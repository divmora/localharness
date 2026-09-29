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

	// PrunedUserContext is the number of historical user messages whose redundant
	// system XML tags (<user_rules>, <user_information>, <subagents>, <skills>, etc.)
	// were pruned.
	PrunedUserContext int

	// TokensSaved is the estimated number of tokens freed by reduction.
	TokensSaved int
}

// ReduceHistory performs zero-cost optimizations on conversation history
// to reduce token usage without losing active information.
//
// Four optimizations (in order):
//  1. Deduplicate re-reads: when the same file is read multiple times with
//     the same or subset range, the older read is replaced with a pointer
//     to the newer one. Only safe when newer range ⊇ older range.
//  2. Collapse command reruns: when the same command is run multiple times,
//     older results are replaced with a pointer to the latest.
//  3. Trim large stale results: view_file results older than freshWindow
//     that exceed 100 lines are trimmed to first 50 + last 50 lines. Large
//     diffs (>40 lines) from edit tools are also trimmed.
//  4. Prune historical user context: intermediate user turns older than
//     freshWindow have redundant XML boilerplate (<user_rules>, <skills>,
//     <user_information>) pruned while keeping Turn 0 and Turn N intact.
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

	// Phase 3: Trim large stale view_file and diff results
	trimmed, tokensSaved := trimLargeResults(result, freshWindow)
	stats.TrimmedResults = trimmed
	stats.TokensSaved += tokensSaved

	// Phase 4: Prune redundant user context from historical turns
	pruned, tokensSaved := pruneHistoricalUserContext(result, freshWindow)
	stats.PrunedUserContext = pruned
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
		minLinesToTrim  = 100 // Only trim results with more lines than this
		keepTopLines    = 50
		keepBottomLines = 50
	)

	trimmed := 0
	tokensSaved := 0

	for i := 0; i < staleEnd; i++ {
		msg := messages[i]
		if msg.ToolResult == nil || msg.ToolResult.IsError {
			continue
		}

		// Handle stale edit diff results (>40 lines)
		if msg.ToolResult.Name == "replace_file_content" || msg.ToolResult.Name == "multi_replace_file_content" {
			content := msg.ToolResult.Content
			if strings.HasPrefix(content, "[... diff lines trimmed") {
				continue
			}
			numLines := lineCount(content)
			if numLines <= 40 {
				continue
			}
			oldTokens := estimateStringTokens(content)
			topLines, bottomLines := splitTopBottomLines(content, 15, 15)
			if bottomLines == "" {
				continue
			}
			trimmedCount := numLines - 30
			if trimmedCount < 1 {
				trimmedCount = 1
			}

			var sb strings.Builder
			sb.WriteString(topLines)
			sb.WriteString(fmt.Sprintf("\n\n[... %d diff lines trimmed — edit already applied in earlier turn ...]\n\n", trimmedCount))
			sb.WriteString(bottomLines)

			newContent := sb.String()
			newTokens := estimateStringTokens(newContent)
			if newTokens >= oldTokens {
				continue
			}

			messages[i].ToolResult = &llm.ToolCallResult{
				CallID:           msg.ToolResult.CallID,
				Name:             msg.ToolResult.Name,
				Content:          newContent,
				IsError:          false,
				ThoughtSignature: msg.ToolResult.ThoughtSignature,
			}

			tokensSaved += oldTokens - newTokens
			trimmed++
			continue
		}

		// Handle stale run_command / exec_command results (>40 lines or >2000 bytes)
		if msg.ToolResult.Name == "run_command" || msg.ToolResult.Name == "exec_command" {
			content := msg.ToolResult.Content
			if strings.HasPrefix(content, "[Command re-run") || strings.Contains(content, "lines of command output trimmed") {
				continue
			}
			numLines := lineCount(content)
			if numLines <= 40 && len(content) <= 2000 {
				continue
			}
			oldTokens := estimateStringTokens(content)
			topLines, bottomLines := splitTopBottomLines(content, 15, 15)
			if bottomLines == "" {
				continue
			}
			trimmedCount := numLines - 30
			if trimmedCount < 1 {
				trimmedCount = 1
			}

			var sb strings.Builder
			sb.WriteString(topLines)
			sb.WriteString(fmt.Sprintf("\n\n[... %d lines of command output trimmed — archived in session log ...]\n\n", trimmedCount))
			sb.WriteString(bottomLines)

			newContent := sb.String()
			newTokens := estimateStringTokens(newContent)
			if newTokens >= oldTokens {
				continue
			}

			messages[i].ToolResult = &llm.ToolCallResult{
				CallID:           msg.ToolResult.CallID,
				Name:             msg.ToolResult.Name,
				Content:          newContent,
				IsError:          msg.ToolResult.IsError,
				ThoughtSignature: msg.ToolResult.ThoughtSignature,
			}

			tokensSaved += oldTokens - newTokens
			trimmed++
			continue
		}

		// Handle stale list_dir, grep_search, and web content results (>60 lines or >3000 bytes)
		if msg.ToolResult.Name == "list_dir" || msg.ToolResult.Name == "grep_search" ||
			msg.ToolResult.Name == "read_url_content" || msg.ToolResult.Name == "web_fetch" {
			content := msg.ToolResult.Content
			if strings.Contains(content, "lines trimmed —") || strings.Contains(content, "trimmed —") {
				continue
			}
			numLines := lineCount(content)
			if numLines <= 60 && len(content) <= 3000 {
				continue
			}
			oldTokens := estimateStringTokens(content)
			topLines, bottomLines := splitTopBottomLines(content, 20, 10)
			if bottomLines == "" {
				continue
			}
			trimmedCount := numLines - 30
			if trimmedCount < 1 {
				trimmedCount = 1
			}

			var label string
			switch msg.ToolResult.Name {
			case "list_dir":
				label = fmt.Sprintf("[... %d directory entries trimmed — re-run tool if needed ...]", trimmedCount)
			case "grep_search":
				label = fmt.Sprintf("[... %d search matches trimmed — re-run tool if needed ...]", trimmedCount)
			default:
				label = fmt.Sprintf("[... %d lines trimmed — re-run tool if needed ...]", trimmedCount)
			}

			var sb strings.Builder
			sb.WriteString(topLines)
			sb.WriteString("\n\n" + label + "\n\n")
			sb.WriteString(bottomLines)

			newContent := sb.String()
			newTokens := estimateStringTokens(newContent)
			if newTokens >= oldTokens {
				continue
			}

			messages[i].ToolResult = &llm.ToolCallResult{
				CallID:           msg.ToolResult.CallID,
				Name:             msg.ToolResult.Name,
				Content:          newContent,
				IsError:          msg.ToolResult.IsError,
				ThoughtSignature: msg.ToolResult.ThoughtSignature,
			}

			tokensSaved += oldTokens - newTokens
			trimmed++
			continue
		}

		if msg.ToolResult.Name != "view_file" {
			continue
		}

		content := msg.ToolResult.Content
		// Skip if already reduced by dedup
		if strings.HasPrefix(content, "[Re-read") || strings.HasPrefix(content, "[Command re-run") || strings.Contains(content, "trimmed —") {
			continue
		}

		numLines := lineCount(content)
		if numLines <= minLinesToTrim && len(content) <= 3000 {
			continue
		}

		// Keep first N + last N lines
		oldTokens := estimateStringTokens(content)
		topLines, bottomLines := splitTopBottomLines(content, keepTopLines, keepBottomLines)
		if bottomLines == "" {
			continue
		}
		trimmedCount := numLines - keepTopLines - keepBottomLines
		if trimmedCount < 1 {
			trimmedCount = 1
		}

		var sb strings.Builder
		sb.WriteString(topLines)
		sb.WriteString(fmt.Sprintf("\n\n[... %d lines trimmed — re-read file if needed ...]\n\n", trimmedCount))
		sb.WriteString(bottomLines)

		newContent := sb.String()
		newTokens := estimateStringTokens(newContent)
		if newTokens >= oldTokens {
			continue
		}

		messages[i].ToolResult = &llm.ToolCallResult{
			CallID:           msg.ToolResult.CallID,
			Name:             msg.ToolResult.Name,
			Content:          newContent,
			IsError:          false,
			ThoughtSignature: msg.ToolResult.ThoughtSignature,
		}

		tokensSaved += oldTokens - newTokens
		trimmed++
	}

	return trimmed, tokensSaved
}

// EmergencyTrimHistory performs aggressive trimming of historical tool results
// when the conversation is approaching the model context ceiling.
// It trims every tool result outside keepRecent down to first 5 + last 5 lines.
func EmergencyTrimHistory(messages []llm.Message, keepRecent int) ([]llm.Message, int) {
	if len(messages) == 0 || keepRecent <= 0 {
		return messages, 0
	}
	cutoff := len(messages) - keepRecent
	if cutoff <= 0 {
		return messages, 0
	}

	tokensSaved := 0

	for i := 0; i < cutoff; i++ {
		msg := messages[i]
		if msg.ToolResult == nil {
			continue
		}
		content := msg.ToolResult.Content
		numLines := lineCount(content)
		if numLines <= 15 && len(content) <= 500 {
			continue
		}
		oldTokens := estimateStringTokens(content)
		topLines, bottomLines := splitTopBottomLines(content, 5, 5)
		trimmedLines := numLines - 10
		if trimmedLines < 1 {
			trimmedLines = 1
		}

		newContent := fmt.Sprintf("%s\n\n[... emergency context trim: %d lines omitted ...]\n\n%s", topLines, trimmedLines, bottomLines)
		messages[i].ToolResult = &llm.ToolCallResult{
			CallID:           msg.ToolResult.CallID,
			Name:             msg.ToolResult.Name,
			Content:          newContent,
			IsError:          msg.ToolResult.IsError,
			ThoughtSignature: msg.ToolResult.ThoughtSignature,
		}
		tokensSaved += oldTokens - estimateStringTokens(newContent)
	}

	return messages, tokensSaved
}

// lineCount returns the number of lines in s without allocating a string slice.
func lineCount(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

// splitTopBottomLines extracts the first topN lines and last bottomN lines from s
// without allocating a slice of all intermediate lines.
func splitTopBottomLines(s string, topN, bottomN int) (string, string) {
	// Find topN-th newline
	topEnd := 0
	for i := 0; i < topN && topEnd < len(s); i++ {
		next := strings.IndexByte(s[topEnd:], '\n')
		if next == -1 {
			topEnd = len(s)
			break
		}
		topEnd += next + 1
	}

	// Find bottomN-th newline from end
	searchEnd := len(s)
	if searchEnd > 0 && s[searchEnd-1] == '\n' {
		searchEnd--
	}
	bottomStart := len(s)
	count := 0
	for count < bottomN && searchEnd > 0 {
		prev := strings.LastIndexByte(s[:searchEnd], '\n')
		count++
		if prev == -1 {
			bottomStart = 0
			break
		}
		bottomStart = prev + 1
		searchEnd = prev
	}

	// If top and bottom overlap or cross, line-based splitting cannot trim a middle section.
	if topEnd >= bottomStart {
		// If string is large (e.g. single-line JSON or minified content > 2000 bytes),
		// split by byte budget: first 1000 bytes and last 1000 bytes.
		if len(s) > 2000 {
			return s[:1000], s[len(s)-1000:]
		}
		return s, ""
	}

	topPart := strings.TrimSuffix(s[:topEnd], "\n")
	bottomPart := strings.TrimSuffix(s[bottomStart:], "\n")

	return topPart, bottomPart
}

// pruneHistoricalUserContext removes redundant system XML boilerplate
// (<user_rules>, <user_information>, <skills>, <plugins>, <subagents>,
// <knowledge_items>, <slash_commands>, <ADDITIONAL_METADATA>) from historical
// user messages.
//
// Preserves:
//   - Turn 0 (the first user message) — keeps full initial rules and context.
//   - Turn N (the latest user message) — keeps current reinforcement.
//   - Messages within freshWindow — keeps immediate recent turns intact.
//
// For intermediate turns outside freshWindow, redundant boilerplate is
// replaced with a concise pointer.
func pruneHistoricalUserContext(messages []llm.Message, freshWindow int) (int, int) {
	staleEnd := len(messages) - freshWindow
	if staleEnd <= 0 {
		return 0, 0
	}

	var userIndices []int
	for i, m := range messages {
		if m.Role == "user" {
			userIndices = append(userIndices, i)
		}
	}

	// Need at least 3 user turns to have an intermediate turn (0, 1..N-1, N)
	if len(userIndices) < 3 {
		return 0, 0
	}

	prunedCount := 0
	tokensSaved := 0

	boilerplateTags := []string{
		"user_information",
		"user_rules",
		"skills",
		"plugins",
		"knowledge_items",
		"slash_commands",
		"subagents",
		"ADDITIONAL_METADATA",
	}

	isBoilerplatePart := func(s string) bool {
		trimmed := strings.TrimSpace(s)
		for _, tag := range boilerplateTags {
			if strings.HasPrefix(trimmed, "<"+tag+">") || strings.HasPrefix(trimmed, "<"+tag+" ") {
				return true
			}
		}
		return false
	}

	// Iterate intermediate user turns: exclude index 0 and latest user turn
	for k := 1; k < len(userIndices)-1; k++ {
		idx := userIndices[k]
		if idx >= staleEnd {
			continue // Within fresh window
		}

		msg := messages[idx]

		if len(msg.Parts) > 0 {
			oldTokens := estimateStringTokens(msg.TextContent())
			var newParts []string
			hasPruned := false

			for _, p := range msg.Parts {
				if isBoilerplatePart(p) {
					hasPruned = true
				} else {
					newParts = append(newParts, p)
				}
			}

			if hasPruned {
				placeholder := "[Historical system & user rules omitted — see initial turn]"
				newParts = append([]string{placeholder}, newParts...)
				messages[idx].Parts = newParts
				newTokens := estimateStringTokens(messages[idx].TextContent())
				if oldTokens > newTokens {
					tokensSaved += oldTokens - newTokens
				}
				prunedCount++
			}
		} else if msg.Content != "" {
			oldTokens := estimateStringTokens(msg.Content)
			content := msg.Content
			modified := false

			for _, tag := range boilerplateTags {
				startTag := "<" + tag
				endTag := "</" + tag + ">"
				for {
					start := strings.Index(content, startTag)
					if start == -1 {
						break
					}
					// Find closing > of the opening tag
					openClose := strings.Index(content[start:], ">")
					if openClose == -1 {
						break
					}
					end := strings.Index(content[start:], endTag)
					if end == -1 {
						break
					}
					endPos := start + end + len(endTag)
					if endPos < len(content) && content[endPos] == '\n' {
						endPos++
					}
					content = content[:start] + content[endPos:]
					modified = true
				}
			}

			if modified {
				placeholder := "[Historical system & user rules omitted — see initial turn]\n"
				content = placeholder + strings.TrimSpace(content)
				messages[idx].Content = content
				newTokens := estimateStringTokens(content)
				if oldTokens > newTokens {
					tokensSaved += oldTokens - newTokens
				}
				prunedCount++
			}
		}
	}

	return prunedCount, tokensSaved
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
