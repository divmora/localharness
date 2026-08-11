package engine

import (
	"fmt"
	"strings"

	"github.com/divmora/localharness/internal/llm"
)

// ReductionResult holds metrics from a ReduceHistory pass.
type ReductionResult struct {
	// DeduplicatedFiles is the number of view_file results that were superseded
	// by a later read of the same file with a superset line range.
	DeduplicatedFiles int

	// CollapsedCommands is the number of run_command results replaced because
	// the same command was re-run later.
	CollapsedCommands int

	// TrimmedResults is the number of large, stale view_file results that had
	// their content truncated to first/last N lines.
	TrimmedResults int

	// TokensSaved is the estimated number of tokens freed by reduction.
	TokensSaved int
}

// viewFileKey uniquely identifies a view_file call for deduplication.
type viewFileKey struct {
	path      string
	startLine int
	endLine   int
}

// ReduceHistory performs zero-cost optimizations on conversation history
// to reduce token usage without losing active information.
//
// Three optimizations (in order):
//  1. Deduplicate re-reads: when the same file is read multiple times with
//     the same or subset range, the older read is replaced with a pointer
//     to the newer one. Only safe when newer range ⊇ older range.
//  2. Collapse command reruns: when the same command is run multiple times,
//     older results are replaced with a pointer to the latest.
//  3. Trim large stale results: view_file results older than freshWindow
//     that exceed 100 lines are trimmed to first 50 + last 50 lines.
//
// The freshWindow parameter controls how many recent messages are never
// touched (default: 8). Messages within the fresh window are always
// preserved exactly as-is.
//
// Returns the modified messages (a shallow copy — original not mutated)
// and a ReductionResult with metrics.
func ReduceHistory(messages []llm.Message, freshWindow int) ([]llm.Message, ReductionResult) {
	if len(messages) == 0 || freshWindow <= 0 {
		return messages, ReductionResult{}
	}

	// Work on a shallow copy to avoid mutating the original
	result := make([]llm.Message, len(messages))
	copy(result, messages)

	var stats ReductionResult

	// Phase 1: Deduplicate view_file re-reads
	stats.DeduplicatedFiles, stats.TokensSaved = deduplicateViewFiles(result, freshWindow)

	// Phase 2: Collapse run_command reruns
	collapsed, tokensSaved := collapseCommandReruns(result, freshWindow)
	stats.CollapsedCommands = collapsed
	stats.TokensSaved += tokensSaved

	// Phase 3: Trim large stale view_file results
	trimmed, tokensSaved := trimLargeResults(result, freshWindow)
	stats.TrimmedResults = trimmed
	stats.TokensSaved += tokensSaved

	return result, stats
}

// deduplicateViewFiles replaces older view_file tool results when a newer read
// of the same file has a superset line range.
//
// A newer read supersedes an older one when:
//   - Same file path
//   - Newer startLine <= older startLine AND newer endLine >= older endLine
//
// The older result's content is replaced with a short pointer message.
func deduplicateViewFiles(messages []llm.Message, freshWindow int) (int, int) {
	staleEnd := len(messages) - freshWindow
	if staleEnd <= 0 {
		return 0, 0
	}

	// Build an index of ALL view_file tool calls (model messages with ToolCalls)
	// and their corresponding results (tool messages with ToolResult).
	// We scan the entire history to find the latest read of each file.
	type viewEntry struct {
		msgIdx    int // index of the tool result message
		path      string
		startLine int
		endLine   int
	}

	// Collect all view_file results with their line ranges.
	// We need both the tool call (for args) and the tool result (for content).
	var entries []viewEntry
	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		if msg.ToolResult == nil || msg.ToolResult.Name != "view_file" || msg.ToolResult.IsError {
			continue
		}
		// Find the corresponding tool call to extract path and line range.
		// The tool call is in the preceding model message.
		path, startLine, endLine := extractViewFileArgs(messages, i)
		if path == "" {
			continue
		}
		entries = append(entries, viewEntry{
			msgIdx:    i,
			path:      path,
			startLine: startLine,
			endLine:   endLine,
		})
	}

	if len(entries) < 2 {
		return 0, 0
	}

	deduped := 0
	tokensSaved := 0

	// For each stale entry, check if a newer entry supersedes it.
	for i := 0; i < len(entries); i++ {
		older := entries[i]
		if older.msgIdx >= staleEnd {
			continue // In fresh window, skip
		}

		for j := i + 1; j < len(entries); j++ {
			newer := entries[j]
			if newer.path != older.path {
				continue
			}
			// Check if newer ⊇ older (superset range)
			if newer.startLine <= older.startLine && newer.endLine >= older.endLine {
				// Superseded — replace older content
				oldContent := messages[older.msgIdx].ToolResult.Content
				oldTokens := estimateStringTokens(oldContent)
				replacement := fmt.Sprintf("[Re-read in later turn — see latest view_file of %s below]", older.path)
				messages[older.msgIdx].ToolResult = &llm.ToolCallResult{
					CallID:           messages[older.msgIdx].ToolResult.CallID,
					Name:             messages[older.msgIdx].ToolResult.Name,
					Content:          replacement,
					IsError:          false,
					ThoughtSignature: messages[older.msgIdx].ToolResult.ThoughtSignature,
				}
				tokensSaved += oldTokens - estimateStringTokens(replacement)
				deduped++
				break // This older entry is handled, move to next
			}
		}
	}

	return deduped, tokensSaved
}

// collapseCommandReruns replaces older run_command tool results when the same
// command was run again later.
func collapseCommandReruns(messages []llm.Message, freshWindow int) (int, int) {
	staleEnd := len(messages) - freshWindow
	if staleEnd <= 0 {
		return 0, 0
	}

	type cmdEntry struct {
		msgIdx  int
		command string
	}

	// Collect all run_command results
	var entries []cmdEntry
	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		if msg.ToolResult == nil || msg.ToolResult.Name != "run_command" {
			continue
		}
		cmd := extractCommandString(messages, i)
		if cmd == "" {
			continue
		}
		entries = append(entries, cmdEntry{msgIdx: i, command: cmd})
	}

	if len(entries) < 2 {
		return 0, 0
	}

	collapsed := 0
	tokensSaved := 0

	// For each stale entry, check if the same command was run later
	for i := 0; i < len(entries); i++ {
		older := entries[i]
		if older.msgIdx >= staleEnd {
			continue
		}
		// Don't collapse error results — errors might have different failure modes
		if messages[older.msgIdx].ToolResult.IsError {
			continue
		}

		for j := i + 1; j < len(entries); j++ {
			newer := entries[j]
			if newer.command == older.command {
				oldContent := messages[older.msgIdx].ToolResult.Content
				oldTokens := estimateStringTokens(oldContent)
				replacement := fmt.Sprintf("[Command re-run in later turn — see latest `%s` result below]",
					truncateString(older.command, 80))
				messages[older.msgIdx].ToolResult = &llm.ToolCallResult{
					CallID:           messages[older.msgIdx].ToolResult.CallID,
					Name:             messages[older.msgIdx].ToolResult.Name,
					Content:          replacement,
					IsError:          false,
					ThoughtSignature: messages[older.msgIdx].ToolResult.ThoughtSignature,
				}
				tokensSaved += oldTokens - estimateStringTokens(replacement)
				collapsed++
				break
			}
		}
	}

	return collapsed, tokensSaved
}

// trimLargeResults truncates stale view_file results that are very large
// (>100 lines) to first 50 + last 50 lines with a trimmed marker.
func trimLargeResults(messages []llm.Message, freshWindow int) (int, int) {
	staleEnd := len(messages) - freshWindow
	if staleEnd <= 0 {
		return 0, 0
	}

	const (
		minLinesToTrim = 100 // Only trim results with more lines than this
		keepTopLines   = 50
		keepBottomLines = 50
	)

	trimmed := 0
	tokensSaved := 0

	for i := 0; i < staleEnd; i++ {
		msg := messages[i]
		if msg.ToolResult == nil || msg.ToolResult.Name != "view_file" || msg.ToolResult.IsError {
			continue
		}

		content := msg.ToolResult.Content
		// Skip if already reduced by dedup
		if strings.HasPrefix(content, "[Re-read") || strings.HasPrefix(content, "[Command re-run") {
			continue
		}

		lines := strings.Split(content, "\n")
		if len(lines) <= minLinesToTrim {
			continue
		}

		// Keep first N + last N lines
		oldTokens := estimateStringTokens(content)
		topLines := lines[:keepTopLines]
		bottomLines := lines[len(lines)-keepBottomLines:]
		trimmedCount := len(lines) - keepTopLines - keepBottomLines

		var sb strings.Builder
		sb.WriteString(strings.Join(topLines, "\n"))
		sb.WriteString(fmt.Sprintf("\n\n[... %d lines trimmed — re-read file if needed ...]\n\n", trimmedCount))
		sb.WriteString(strings.Join(bottomLines, "\n"))

		newContent := sb.String()
		messages[i].ToolResult = &llm.ToolCallResult{
			CallID:           msg.ToolResult.CallID,
			Name:             msg.ToolResult.Name,
			Content:          newContent,
			IsError:          false,
			ThoughtSignature: msg.ToolResult.ThoughtSignature,
		}

		tokensSaved += oldTokens - estimateStringTokens(newContent)
		trimmed++
	}

	return trimmed, tokensSaved
}

// extractViewFileArgs finds the view_file tool call arguments (path, start_line, end_line)
// for a tool result at the given index. It searches backward for the model message
// that initiated this tool call.
func extractViewFileArgs(messages []llm.Message, resultIdx int) (path string, startLine, endLine int) {
	tr := messages[resultIdx].ToolResult
	if tr == nil {
		return "", 0, 0
	}

	// Search backward for the model message with matching tool call
	for i := resultIdx - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != "model" {
			continue
		}
		for _, tc := range msg.ToolCalls {
			if tc.ID == tr.CallID && tc.Name == "view_file" {
				path, _ = tc.Args["path"].(string)
				startLine = toInt(tc.Args["start_line"])
				endLine = toInt(tc.Args["end_line"])
				// Normalize: 0 means "full file" — use max range
				if startLine <= 0 {
					startLine = 1
				}
				if endLine <= 0 {
					endLine = 999999 // Effectively "to end of file"
				}
				return path, startLine, endLine
			}
		}
		// Only search one model message back — tool results immediately follow
		if msg.Role == "model" {
			break
		}
	}
	return "", 0, 0
}

// extractCommandString finds the command string for a run_command tool result.
func extractCommandString(messages []llm.Message, resultIdx int) string {
	tr := messages[resultIdx].ToolResult
	if tr == nil {
		return ""
	}

	for i := resultIdx - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != "model" {
			continue
		}
		for _, tc := range msg.ToolCalls {
			if tc.ID == tr.CallID && tc.Name == "run_command" {
				cmd, _ := tc.Args["command"].(string)
				return cmd
			}
		}
		if msg.Role == "model" {
			break
		}
	}
	return ""
}

// toInt converts an interface{} to int, handling float64 (from JSON) and int.
func toInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	default:
		return 0
	}
}

// truncateString truncates a string to maxLen characters with "..." suffix.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
