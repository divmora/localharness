package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// traceEntry represents a parsed trace response file.
type traceEntry struct {
	APICallIndex int              `json:"api_call_index"`
	Timestamp    string           `json:"timestamp"`
	LatencyMs    int              `json:"latency_ms"`
	ContentLen   int              `json:"content_len"`
	ThinkingLen  int              `json:"thinking_len"`
	FinishReason string           `json:"finish_reason"`
	Error        string           `json:"error"`
	ToolCalls    int              `json:"tool_calls"`
	ToolDetails  []traceToolCall  `json:"tool_call_details"`
	Usage        *traceUsage      `json:"usage,omitempty"`
}

type traceToolCall struct {
	ID   string                 `json:"id"`
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

type traceUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	ThinkingTokens   int `json:"thinking_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CachedTokens     int `json:"cached_tokens"`
}

func runTrace(dataDir, partialID string, flags []string) {
	fullID, err := resolveConversationID(dataDir, partialID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	showCommands := contains(flags, "--commands")
	watchMode := contains(flags, "--watch")

	traceDir := filepath.Join(dataDir, "brain", fullID, ".system_generated", "traces")

	if watchMode {
		runTraceWatch(traceDir, fullID, showCommands)
		return
	}

	entries := readTraceEntries(traceDir)
	if len(entries) == 0 {
		fmt.Fprintf(os.Stderr, "No traces found for conversation %s\n", fullID[:8])
		os.Exit(1)
	}

	printTraceHeader(fullID, entries)
	printTraceTable(entries, showCommands)
	printTraceSummary(entries)
}

// readTraceEntries reads all step_*_response.json files from the trace directory.
func readTraceEntries(traceDir string) []traceEntry {
	pattern := filepath.Join(traceDir, "step_*_response.json")
	files, err := filepath.Glob(pattern)
	if err != nil || len(files) == 0 {
		return nil
	}
	sort.Strings(files)

	var entries []traceEntry
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var entry traceEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

func printTraceHeader(fullID string, entries []traceEntry) {
	fmt.Printf("Conversation: %s\n", fullID)

	// Calculate duration from first to last timestamp
	if len(entries) >= 2 {
		first := entries[0].Timestamp
		last := entries[len(entries)-1].Timestamp
		if t1, err1 := time.Parse(time.RFC3339Nano, first); err1 == nil {
			if t2, err2 := time.Parse(time.RFC3339Nano, last); err2 == nil {
				dur := t2.Sub(t1)
				fmt.Printf("Steps: %d | Duration: %s\n", len(entries), formatDuration(dur))
			}
		}
	} else {
		fmt.Printf("Steps: %d\n", len(entries))
	}
	fmt.Println()
}

func printTraceTable(entries []traceEntry, showCommands bool) {
	// Header
	fmt.Printf(" %-5s  %-25s  %-50s  %s\n", "Step", "Tool", "Detail", "Latency")
	fmt.Println(strings.Repeat("─", 100))

	for _, e := range entries {
		if e.Error != "" {
			errMsg := e.Error
			if len(errMsg) > 80 {
				errMsg = errMsg[:80] + "…"
			}
			fmt.Printf(" %5d  %-25s  %-50s  %s\n", e.APICallIndex, "❌ ERROR", errMsg, formatMs(e.LatencyMs))
			continue
		}

		if len(e.ToolDetails) > 0 {
			for _, tc := range e.ToolDetails {
				detail := extractToolDetail(tc, showCommands)
				icon := toolIcon(tc.Name)
				fmt.Printf(" %5d  %s %-23s  %-50s  %s\n", e.APICallIndex, icon, tc.Name, truncate(detail, 50), formatMs(e.LatencyMs))
			}
		} else if e.ContentLen > 0 {
			fmt.Printf(" %5d  💬 %-23s  %-50s  %s\n", e.APICallIndex, "text_response", fmt.Sprintf("(%d bytes, think=%d)", e.ContentLen, e.ThinkingLen), formatMs(e.LatencyMs))
		}
	}
	fmt.Println(strings.Repeat("─", 100))
}

func printTraceSummary(entries []traceEntry) {
	toolCounts := make(map[string]int)
	var totalLatency int
	var errorCount int
	var writeCount int

	writingTools := map[string]bool{
		"replace_file_content":       true,
		"multi_replace_file_content": true,
		"write_to_file":             true,
	}

	for _, e := range entries {
		totalLatency += e.LatencyMs
		if e.Error != "" {
			errorCount++
		}
		for _, tc := range e.ToolDetails {
			toolCounts[tc.Name]++
			if writingTools[tc.Name] {
				writeCount++
			}
		}
	}

	// Sort tools by count descending
	type toolCount struct {
		name  string
		count int
	}
	var sorted []toolCount
	for name, count := range toolCounts {
		sorted = append(sorted, toolCount{name, count})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].count > sorted[j].count })

	fmt.Printf("\n📊 %d API calls", len(entries))
	if errorCount > 0 {
		fmt.Printf(" | %d errors", errorCount)
	}
	fmt.Printf(" | %d writes", writeCount)
	fmt.Printf(" | total latency: %s\n", formatMs(totalLatency))

	// Tool breakdown
	fmt.Print("   ")
	parts := make([]string, 0, len(sorted))
	for _, tc := range sorted {
		parts = append(parts, fmt.Sprintf("%d× %s", tc.count, tc.name))
	}
	fmt.Println(strings.Join(parts, ", "))
}

// extractToolDetail extracts a human-readable detail string from tool args.
func extractToolDetail(tc traceToolCall, showCommands bool) string {
	switch tc.Name {
	case "view_file":
		return shortenPath(getString(tc.Args, "path", "AbsolutePath"))
	case "list_dir":
		return shortenPath(getString(tc.Args, "path", "DirectoryPath"))
	case "grep_search":
		query := getString(tc.Args, "query", "Query")
		path := shortenPath(getString(tc.Args, "path", "SearchPath"))
		return fmt.Sprintf("\"%s\" in %s", query, path)
	case "find_file":
		pattern := getString(tc.Args, "pattern", "Query")
		return fmt.Sprintf("pattern=%s", pattern)
	case "run_command":
		cmd := getString(tc.Args, "command", "CommandLine")
		if !showCommands && len(cmd) > 40 {
			cmd = cmd[:40] + "…"
		}
		return cmd
	case "replace_file_content", "multi_replace_file_content":
		return shortenPath(getString(tc.Args, "TargetFile", "path"))
	case "write_to_file":
		return shortenPath(getString(tc.Args, "TargetFile", "path"))
	case "finish":
		return "✅"
	default:
		// Try common arg names
		if p := getString(tc.Args, "path", "AbsolutePath", "TargetFile"); p != "" {
			return shortenPath(p)
		}
		if q := getString(tc.Args, "query", "Query"); q != "" {
			return q
		}
		return ""
	}
}

// runTraceWatch polls for new trace files and prints them incrementally.
func runTraceWatch(traceDir, fullID string, showCommands bool) {
	fmt.Printf("👁  Watching conversation %s...\n", fullID[:8])
	fmt.Printf(" %-5s  %-25s  %-50s  %s\n", "Step", "Tool", "Detail", "Latency")
	fmt.Println(strings.Repeat("─", 100))

	seen := make(map[string]bool)
	for {
		entries := readTraceEntries(traceDir)
		for _, e := range entries {
			key := fmt.Sprintf("step_%d", e.APICallIndex)
			if seen[key] {
				continue
			}
			seen[key] = true

			if e.Error != "" {
				errMsg := e.Error
				if len(errMsg) > 80 {
					errMsg = errMsg[:80] + "…"
				}
				fmt.Printf(" %5d  %-25s  %-50s  %s\n", e.APICallIndex, "❌ ERROR", errMsg, formatMs(e.LatencyMs))
				continue
			}

			for _, tc := range e.ToolDetails {
				detail := extractToolDetail(tc, showCommands)
				icon := toolIcon(tc.Name)
				fmt.Printf(" %5d  %s %-23s  %-50s  %s\n", e.APICallIndex, icon, tc.Name, truncate(detail, 50), formatMs(e.LatencyMs))
			}

			if len(e.ToolDetails) == 0 && e.ContentLen > 0 {
				fmt.Printf(" %5d  💬 %-23s  %-50s  %s\n", e.APICallIndex, "text_response", fmt.Sprintf("(%d bytes)", e.ContentLen), formatMs(e.LatencyMs))
			}

			// Check for finish
			if e.FinishReason == "stop" && len(e.ToolDetails) == 0 {
				fmt.Println(strings.Repeat("─", 100))
				fmt.Printf("🏁 Conversation finished (reason: %s)\n", e.FinishReason)
				return
			}
		}
		time.Sleep(2 * time.Second)
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────

func toolIcon(name string) string {
	switch name {
	case "view_file":
		return "📄"
	case "list_dir":
		return "📁"
	case "grep_search":
		return "🔍"
	case "find_file":
		return "🔎"
	case "run_command":
		return "⚙️"
	case "replace_file_content", "multi_replace_file_content":
		return "✏️"
	case "write_to_file":
		return "📝"
	case "finish":
		return "✅"
	case "search_web":
		return "🌐"
	case "read_url_content":
		return "🌐"
	default:
		return "🔧"
	}
}

// getString tries multiple keys in a map and returns the first non-empty string found.
func getString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func shortenPath(p string) string {
	if p == "" {
		return ""
	}
	// Show last 2 path components
	parts := strings.Split(p, "/")
	if len(parts) > 3 {
		return "…/" + strings.Join(parts[len(parts)-3:], "/")
	}
	return p
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func formatMs(ms int) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm %ds", m, s)
}
