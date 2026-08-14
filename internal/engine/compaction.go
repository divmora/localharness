package engine

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/divmora/localharness/internal/errors"
	"github.com/divmora/localharness/internal/llm"
)

const (
	// compactionPrompt instructs the summarizer to produce a high-fidelity summary
	// that preserves structured information critical for the agent to continue.
	compactionPrompt = `You are a conversation compactor for an AI coding agent.
Given a series of conversation messages, produce a concise but thorough summary that preserves:

1. **Decisions & rationale**: All key decisions made, with their reasoning
2. **File paths & operations**: Every file path mentioned, what was done to it (created, edited, viewed, deleted), and key content
3. **Tool call sequence**: All tools invoked, their arguments, and outcomes (especially errors)
4. **User goals**: The user's original request, any refinements, and current progress status
5. **Code context**: Important code snippets, function names, class names, and architectural patterns discussed
6. **Errors & fixes**: All errors encountered, their root causes, and how they were resolved
7. **State & progress**: What has been completed vs. what remains to be done

Format the summary as structured bullet points grouped by topic. Be concise but do NOT omit any file paths, tool results, or decisions.
Output ONLY the summary, no preamble or commentary.`

	// slidingWindowRetryFraction is the fraction of keepRecentMessages to use
	// on a retry when the first compaction didn't reduce enough.
	slidingWindowRetryFraction = 0.5

	// maxCompactionRetries limits sliding window iterations.
	maxCompactionRetries = 3
)

// CompactionConfig holds parameters for context compaction.
type CompactionConfig struct {
	// Threshold is the token count that triggers compaction. 0 = disabled.
	Threshold int

	// KeepRecentMessages is the number of recent messages to preserve.
	// Defaults to config.DefaultKeepRecentMessages (10) if 0.
	KeepRecentMessages int

	// LastRealTokenCount is the most recent real token count from the LLM provider.
	// If > 0, used instead of character-based estimation for the trigger decision.
	LastRealTokenCount int

	// SystemPromptTokens is the estimated token count of the system prompt.
	// Included in the total for threshold comparison.
	SystemPromptTokens int
}

// CompactionResult holds the result of a context compaction.
type CompactionResult struct {
	OriginalTokens  int
	CompactedTokens int
	MessagesRemoved int
	Summary         string
	Retries         int // How many sliding window iterations were needed
}

// CompactIfNeeded checks if the conversation history exceeds the token threshold
// and compacts it by summarizing older messages.
//
// Uses a hybrid approach for token counting:
//   - If cfg.LastRealTokenCount > 0, uses the real count from the LLM provider
//   - Otherwise falls back to improved character-based estimation
//
// If the first compaction pass doesn't reduce tokens enough (still > 80% of threshold),
// it retries with fewer preserved messages (sliding window).
func CompactIfNeeded(
	ctx context.Context,
	provider llm.Provider,
	messages []llm.Message,
	cfg CompactionConfig,
	logger *slog.Logger,
) ([]llm.Message, *CompactionResult, error) {
	if cfg.Threshold <= 0 {
		return messages, nil, nil
	}

	keepCount := cfg.KeepRecentMessages
	if keepCount <= 0 {
		keepCount = 10 // Safety default
	}

	// Determine current token count — prefer real counts from provider
	tokenEstimate := cfg.LastRealTokenCount
	if tokenEstimate <= 0 {
		tokenEstimate = EstimateTokens(messages) + cfg.SystemPromptTokens
	}

	if tokenEstimate < cfg.Threshold {
		return messages, nil, nil
	}

	logger.Info("context compaction triggered",
		"estimated_tokens", tokenEstimate,
		"threshold", cfg.Threshold,
		"messages", len(messages),
		"using_real_count", cfg.LastRealTokenCount > 0,
	)

	// Sliding window: try compacting, if still too large, keep fewer messages
	var lastResult *CompactionResult
	currentMessages := messages
	retries := 0

	for retries <= maxCompactionRetries {
		if keepCount >= len(currentMessages) {
			// Not enough messages to compact
			if retries == 0 {
				return messages, nil, nil
			}
			break
		}

		compacted, result, err := doCompaction(ctx, provider, currentMessages, keepCount, logger)
		if err != nil {
			logger.Error("compaction failed", "error", err, "retry", retries)
			if retries == 0 {
				// First attempt failed — return original messages
				return messages, nil, fmt.Errorf("compaction summary failed: %w", err)
			}
			// Retry failed — return last successful result
			break
		}

		if result == nil {
			break
		}

		result.Retries = retries
		lastResult = result
		currentMessages = compacted

		// Check if we've reduced enough (below 80% of threshold)
		newEstimate := EstimateTokens(currentMessages) + cfg.SystemPromptTokens
		if newEstimate < int(float64(cfg.Threshold)*0.8) {
			break
		}

		// Need more aggressive compaction — reduce keepCount
		retries++
		keepCount = int(float64(keepCount) * slidingWindowRetryFraction)
		if keepCount < 2 {
			keepCount = 2 // Always keep at least 2 messages (summary + ack don't count)
		}

		logger.Info("compaction insufficient, retrying with fewer kept messages",
			"new_estimate", newEstimate,
			"threshold", cfg.Threshold,
			"new_keep_count", keepCount,
			"retry", retries,
		)
	}

	if lastResult == nil {
		return messages, nil, nil
	}

	logger.Info("context compacted",
		"original_tokens", lastResult.OriginalTokens,
		"compacted_tokens", lastResult.CompactedTokens,
		"messages_removed", lastResult.MessagesRemoved,
		"messages_kept", len(currentMessages),
		"retries", lastResult.Retries,
		"reduction_pct", fmt.Sprintf("%.0f%%",
			float64(lastResult.OriginalTokens-lastResult.CompactedTokens)/float64(lastResult.OriginalTokens)*100),
	)

	return currentMessages, lastResult, nil
}

// doCompaction performs a single compaction pass: summarize old messages, keep recent ones.
func doCompaction(
	ctx context.Context,
	provider llm.Provider,
	messages []llm.Message,
	keepCount int,
	logger *slog.Logger,
) ([]llm.Message, *CompactionResult, error) {
	if keepCount >= len(messages) {
		return messages, nil, nil
	}

	originalEstimate := EstimateTokens(messages)

	// Split into old (to summarize) and recent (to keep)
	oldMessages := messages[:len(messages)-keepCount]
	recentMessages := messages[len(messages)-keepCount:]

	// Build summary request with structured content
	conversationText := formatMessagesForSummary(oldMessages)

	summaryReq := &llm.GenerateRequest{
		SystemPrompt: compactionPrompt,
		Messages: []llm.Message{
			{
				Role:    "user",
				Content: fmt.Sprintf("Summarize this conversation history (%d messages, ~%d tokens):\n\n%s",
					len(oldMessages), EstimateTokens(oldMessages), conversationText),
			},
		},
		// No tools — pure text generation
	}

	summaryResp, err := provider.Generate(ctx, summaryReq)
	if err != nil {
		return nil, nil, errors.Wrap(err, errors.ErrCodeLLMProvider,
			"compaction LLM call failed").
			WithContext("operation", "compaction").
			WithComponent("engine")
	}

	summary := summaryResp.Content
	if summary == "" {
		logger.Warn("compaction produced empty summary, skipping")
		return messages, nil, nil
	}

	// Build new message list: [summary] + [model ack] + recent messages
	compactedMessages := make([]llm.Message, 0, 2+len(recentMessages))
	compactedMessages = append(compactedMessages, llm.Message{
		Role:    "user",
		Content: fmt.Sprintf("[Conversation Summary — %d messages compacted]\n%s", len(oldMessages), summary),
	})
	// Add a model ack so history alternates correctly (user → model → user → ...)
	compactedMessages = append(compactedMessages, llm.Message{
		Role:    "model",
		Content: "Understood. I have the full conversation context from the summary above. Continuing from where we left off.",
	})
	compactedMessages = append(compactedMessages, recentMessages...)

	newEstimate := EstimateTokens(compactedMessages)

	return compactedMessages, &CompactionResult{
		OriginalTokens:  originalEstimate,
		CompactedTokens: newEstimate,
		MessagesRemoved: len(oldMessages),
		Summary:         summary,
	}, nil
}

// formatMessagesForSummary converts messages into a structured text representation
// suitable for the compaction summarizer. Includes tool call details, results, and
// truncates very long content to keep the summary request manageable.
func formatMessagesForSummary(messages []llm.Message) string {
	var parts []string

	for _, msg := range messages {
		var lines []string
		prefix := msg.Role

		// Main content (handles both single-part and multi-part messages)
		content := msg.TextContent()
		if content != "" {
			if len(content) > 3000 {
				content = content[:3000] + "\n... [truncated, " + fmt.Sprintf("%d", len(msg.TextContent())) + " chars total]"
			}
			lines = append(lines, content)
		}

		// Tool calls
		if len(msg.ToolCalls) > 0 {
			var tcParts []string
			for _, tc := range msg.ToolCalls {
				argSummary := summarizeArgs(tc.Args)
				tcParts = append(tcParts, fmt.Sprintf("  → %s(%s)", tc.Name, argSummary))
			}
			lines = append(lines, "[Tool calls]\n"+strings.Join(tcParts, "\n"))
		}

		// Tool result
		if msg.ToolResult != nil {
			resultContent := msg.ToolResult.Content
			if len(resultContent) > 2000 {
				resultContent = resultContent[:2000] + "\n... [truncated]"
			}
			status := "OK"
			if msg.ToolResult.IsError {
				status = "ERROR"
			}
			lines = append(lines, fmt.Sprintf("[Tool result: %s → %s]\n%s", msg.ToolResult.Name, status, resultContent))
		}

		if len(lines) > 0 {
			parts = append(parts, fmt.Sprintf("[%s]:\n%s", prefix, strings.Join(lines, "\n")))
		}
	}

	return strings.Join(parts, "\n\n---\n\n")
}

// summarizeArgs creates a compact string representation of tool call arguments.
// Truncates long values (e.g., file content) to keep the summary readable.
func summarizeArgs(args map[string]interface{}) string {
	if len(args) == 0 {
		return ""
	}

	var parts []string
	for k, v := range args {
		s := fmt.Sprintf("%v", v)
		if len(s) > 100 {
			s = s[:100] + "..."
		}
		parts = append(parts, fmt.Sprintf("%s=%s", k, s))
	}
	return strings.Join(parts, ", ")
}

// EstimateTokens provides a token count estimate for a message list.
// Uses improved heuristics: ~4 characters per token for English text,
// with overhead for message structure, role tokens, and separators.
func EstimateTokens(messages []llm.Message) int {
	total := 0
	for _, msg := range messages {
		// Base overhead per message (role token, separators)
		total += 4

		// Content tokens (handles both single-part and multi-part messages)
		total += estimateStringTokens(msg.TextContent())

		// Tool result tokens
		if msg.ToolResult != nil {
			total += 4 // function call overhead
			total += estimateStringTokens(msg.ToolResult.Name)
			total += estimateStringTokens(msg.ToolResult.Content)
		}

		// Tool call tokens
		for _, tc := range msg.ToolCalls {
			total += 4 // function call overhead
			total += estimateStringTokens(tc.Name)
			for _, v := range tc.Args {
				total += estimateStringTokens(fmt.Sprintf("%v", v))
			}
		}
	}
	return total
}

// EstimateStringTokens estimates token count for a single string.
// Uses ~4 characters per token with a minimum of 1 for non-empty strings.
func estimateStringTokens(s string) int {
	if s == "" {
		return 0
	}
	tokens := len(s) / 4
	if tokens == 0 {
		tokens = 1
	}
	return tokens
}
