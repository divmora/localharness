package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

// inspectFlags holds parsed flags for the inspect command.
type inspectFlags struct {
	jsonOutput bool
	topN       int
	steps      bool // --steps: show full tool args and error details
	stepN      int  // --step=N: deep-dive into a single step (-1 = disabled)
	errorsOnly bool // --errors: show only error steps
}

func parseInspectFlags(args []string) inspectFlags {
	f := inspectFlags{topN: 3, stepN: -1}
	for _, a := range args {
		switch {
		case a == "--json":
			f.jsonOutput = true
		case a == "--steps":
			f.steps = true
		case a == "--errors":
			f.errorsOnly = true
		case strings.HasPrefix(a, "--top="):
			if n, err := strconv.Atoi(strings.TrimPrefix(a, "--top=")); err == nil && n > 0 {
				f.topN = n
			}
		case strings.HasPrefix(a, "--step="):
			if n, err := strconv.Atoi(strings.TrimPrefix(a, "--step=")); err == nil && n >= 0 {
				f.stepN = n
			}
		}
	}
	return f
}

// messageInfo holds analyzed data for a single conversation message.
type messageInfo struct {
	Index     int    `json:"index"`
	Role      string `json:"role"`
	Size      int    `json:"size_bytes"`
	Cumul     int    `json:"cumulative_bytes"`
	ToolName  string `json:"tool_name,omitempty"`
	ToolInfo  string `json:"tool_info,omitempty"`
	Warning   string `json:"warning,omitempty"`
	ToolArgs  string `json:"tool_args,omitempty"`  // Full args JSON
	FullPath  string `json:"full_path,omitempty"`  // Full file/dir path (not basename)
	ErrorText string `json:"error_text,omitempty"` // Error content when is_error=true
	IsError   bool   `json:"is_error,omitempty"`   // Whether this step errored
	Timestamp string `json:"timestamp,omitempty"`  // Step timestamp
}

// inspectResult holds the full inspection result for JSON output.
type inspectResult struct {
	ConversationID string        `json:"conversation_id"`
	CreatedAt      string        `json:"created_at"`
	UpdatedAt      string        `json:"updated_at"`
	Status         string        `json:"status"`
	Version        string        `json:"harness_version"`
	Model          string        `json:"model,omitempty"`
	MessageCount   int           `json:"message_count"`
	TotalSize      int           `json:"total_size_bytes"`
	EstTokens      int           `json:"estimated_tokens"`
	Compactions    int           `json:"compaction_count"`
	Messages       []messageInfo `json:"messages"`
	TopMessages    []messageInfo `json:"top_messages"`
	Breakdown      breakdown     `json:"context_breakdown"`
	Usage          *usageInfo    `json:"total_usage,omitempty"`

	// Agent lineage (subagent support)
	ParentConversationID string `json:"parent_conversation_id,omitempty"`
	AgentRole            string `json:"agent_role,omitempty"`
	AgentTypeName        string `json:"agent_type_name,omitempty"`
	AgentDepth           int32  `json:"agent_depth,omitempty"`
}

type breakdown struct {
	UserBytes       int `json:"user_bytes"`
	UserCount       int `json:"user_count"`
	ModelBytes      int `json:"model_bytes"`
	ModelCount      int `json:"model_count"`
	ToolCallBytes   int `json:"tool_call_bytes"`
	ToolCallCount   int `json:"tool_call_count"`
	ToolResultBytes int `json:"tool_result_bytes"`
	ToolResultCount int `json:"tool_result_count"`
}

type usageInfo struct {
	PromptTokens     int32 `json:"prompt_tokens"`
	CompletionTokens int32 `json:"completion_tokens"`
	ThinkingTokens   int32 `json:"thinking_tokens"`
	TotalTokens      int32 `json:"total_tokens"`
	CachedTokens     int32 `json:"cached_tokens"`
}

func runInspect(dataDir, partialID string, extraArgs []string) {
	flags := parseInspectFlags(extraArgs)

	// Resolve partial ID
	convID, err := resolveConversationID(dataDir, partialID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Load conversation state
	pbPath := filepath.Join(dataDir, "conversations", convID+".pb")
	data, err := os.ReadFile(pbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot read %s: %v\n", pbPath, err)
		os.Exit(1)
	}

	state := &pb.ConversationState{}
	if err := proto.Unmarshal(data, state); err != nil {
		fmt.Fprintf(os.Stderr, "Error: corrupt .pb file: %v\n", err)
		os.Exit(1)
	}

	// Analyze messages
	result := analyzeConversation(state)

	// Route to the appropriate output mode
	switch {
	case flags.jsonOutput:
		result.TopMessages = topNMessages(result.Messages, flags.topN)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			fmt.Fprintf(os.Stderr, "Error: JSON encode failed: %v\n", err)
			os.Exit(1)
		}
	case flags.stepN >= 0:
		printHeader(result)
		printStepDetail(result, flags.stepN)
	case flags.errorsOnly:
		printHeader(result)
		printErrorsView(result)
	case flags.steps:
		printHeader(result)
		printStepsView(result)
	default:
		printInspectResult(result, flags.topN)
	}
}

func analyzeConversation(state *pb.ConversationState) inspectResult {
	result := inspectResult{
		ConversationID:       state.ConversationId,
		CreatedAt:            state.CreatedAt,
		UpdatedAt:            state.UpdatedAt,
		Status:               state.Status.String(),
		Version:              state.HarnessVersion,
		Compactions:          int(state.CompactionCount),
		MessageCount:         len(state.Messages),
		ParentConversationID: state.ParentConversationId,
		AgentRole:            state.AgentRole,
		AgentTypeName:        state.AgentTypeName,
		AgentDepth:           state.AgentDepth,
	}

	// Extract model name from config
	if state.Config != nil && state.Config.LitellmEndpoint != "" {
		result.Model = "litellm:" + state.Config.LitellmEndpoint
	}

	// Extract usage
	if state.TotalUsage != nil {
		result.Usage = &usageInfo{
			PromptTokens:     state.TotalUsage.PromptTokens,
			CompletionTokens: state.TotalUsage.CompletionTokens,
			ThinkingTokens:   state.TotalUsage.ThinkingTokens,
			TotalTokens:      state.TotalUsage.TotalTokens,
			CachedTokens:     state.TotalUsage.CachedTokens,
		}
	}

	cumul := 0
	for i, msg := range state.Messages {
		size := messageSize(msg)
		cumul += size

		info := messageInfo{
			Index:     i,
			Role:      classifyRole(msg),
			Size:      size,
			Cumul:     cumul,
			Timestamp: msg.Timestamp,
		}

		// Extract tool info
		if len(msg.ToolCalls) > 0 {
			tc := msg.ToolCalls[0]
			info.ToolName = tc.Name
			info.ToolArgs = tc.ArgsJson
			info.FullPath = extractFullPath(tc)
			info.ToolInfo = fmt.Sprintf("→ %s(%s)", tc.Name, truncatePath(info.FullPath, 50))
			result.Breakdown.ToolCallBytes += size
			result.Breakdown.ToolCallCount++
		} else if msg.ToolResult != nil {
			info.ToolName = msg.ToolResult.Name
			resultContentLen := len(msg.ToolResult.Content)
			info.ToolInfo = fmt.Sprintf("← %s result", msg.ToolResult.Name)
			info.IsError = msg.ToolResult.IsError

			if msg.ToolResult.IsError {
				info.ErrorText = msg.ToolResult.Content
				info.ToolInfo += " [ERROR]"
			}

			// Add warnings for large results
			if resultContentLen > 20_000 {
				info.Warning = "🔴 LARGE"
			} else if resultContentLen > 5_000 {
				info.Warning = "⚠️"
			}

			result.Breakdown.ToolResultBytes += size
			result.Breakdown.ToolResultCount++
		} else {
			switch msg.Role {
			case "user":
				result.Breakdown.UserBytes += size
				result.Breakdown.UserCount++
			case "model":
				result.Breakdown.ModelBytes += size
				result.Breakdown.ModelCount++
			}
		}

		result.Messages = append(result.Messages, info)
	}

	result.TotalSize = cumul
	result.EstTokens = cumul / 4 // rough heuristic

	return result
}

// messageSize returns the approximate content size of a conversation message.
func messageSize(msg *pb.ConversationMessage) int {
	size := len(msg.Content)
	for _, p := range msg.Parts {
		size += len(p)
	}
	for _, tc := range msg.ToolCalls {
		size += len(tc.Name) + len(tc.ArgsJson)
	}
	if msg.ToolResult != nil {
		size += len(msg.ToolResult.Name) + len(msg.ToolResult.Content)
	}
	return size
}

// classifyRole returns a display-friendly role string.
func classifyRole(msg *pb.ConversationMessage) string {
	if len(msg.ToolCalls) > 0 {
		return "model"
	}
	if msg.ToolResult != nil {
		return "tool"
	}
	return msg.Role
}

// extractFullPath extracts the full file/directory path from a tool call's args.
// Returns the path as-is (no basename truncation) for debugging.
func extractFullPath(tc *pb.ToolCallRecord) string {
	if tc.ArgsJson == "" {
		return ""
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(tc.ArgsJson), &args); err != nil {
		return ""
	}

	for _, key := range []string{
		"AbsolutePath", "path", "TargetFile",
		"SearchPath", "DirectoryPath",
		"CommandLine", "command",
		"Query", "query",
		"Url", "url",
	} {
		if v, ok := args[key]; ok {
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

// extractKeyArg extracts the most useful argument from a tool call for display.
// Shows the full path (truncated at maxLen) for debugging visibility.
func extractKeyArg(tc *pb.ToolCallRecord) string {
	path := extractFullPath(tc)
	if path == "" {
		return ""
	}
	return truncatePath(path, 40)
}

// truncatePath shortens a path for display, keeping the end visible.
func truncatePath(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Keep the last maxLen-3 chars with "..." prefix
	return "..." + s[len(s)-maxLen+3:]
}

// topNMessages returns the N largest messages by size.
func topNMessages(msgs []messageInfo, n int) []messageInfo {
	sorted := make([]messageInfo, len(msgs))
	copy(sorted, msgs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Size > sorted[j].Size
	})
	if n > len(sorted) {
		n = len(sorted)
	}
	return sorted[:n]
}

// ---------- Output renderers ----------

// printHeader prints the conversation summary header (shared by all views).
func printHeader(r inspectResult) {
	fmt.Printf("Conversation: %s\n", r.ConversationID)
	fmt.Printf("Created:      %s\n", r.CreatedAt)
	fmt.Printf("Updated:      %s\n", r.UpdatedAt)
	fmt.Printf("Status:       %s\n", r.Status)
	if r.Version != "" {
		fmt.Printf("Version:      %s\n", r.Version)
	}
	if r.Model != "" {
		fmt.Printf("Model:        %s\n", r.Model)
	}

	// Agent lineage (shown for subagents)
	if r.ParentConversationID != "" {
		fmt.Println()
		fmt.Printf("🔗 Agent Lineage:\n")
		fmt.Printf("  Parent:     %s\n", r.ParentConversationID)
		if r.AgentRole != "" {
			fmt.Printf("  Role:       %s\n", r.AgentRole)
		}
		if r.AgentTypeName != "" {
			fmt.Printf("  Type:       %s\n", r.AgentTypeName)
		}
		fmt.Printf("  Depth:      %d\n", r.AgentDepth)
	}

	fmt.Println()
	fmt.Printf("Messages:     %d\n", r.MessageCount)
	fmt.Printf("Total Size:   %s (~%s tokens)\n", formatBytes(r.TotalSize), formatCount(r.EstTokens))
	fmt.Printf("Compactions:  %d\n", r.Compactions)
	if r.Usage != nil {
		fmt.Printf("Total Usage:  %s tokens (prompt: %s, completion: %s, cached: %s)\n",
			formatCount(int(r.Usage.TotalTokens)),
			formatCount(int(r.Usage.PromptTokens)),
			formatCount(int(r.Usage.CompletionTokens)),
			formatCount(int(r.Usage.CachedTokens)))
	}
	fmt.Println()
}

// printStepsView renders the --steps output: full tool args and error details.
func printStepsView(r inspectResult) {
	fmt.Printf(" %-4s %-15s %-55s %8s  %s\n", "#", "Tool", "Path/Args", "Size", "Status")
	fmt.Println(strings.Repeat("─", 100))

	for _, m := range r.Messages {
		status := ""
		tool := m.ToolName
		pathOrArgs := ""

		switch m.Role {
		case "user":
			tool = "(user prompt)"
		case "model":
			if m.ToolName != "" {
				pathOrArgs = m.FullPath
				if pathOrArgs == "" && m.ToolArgs != "" {
					pathOrArgs = truncateStr(m.ToolArgs, 55)
				}
			}
		case "tool":
			if m.IsError {
				status = "❌ ERROR"
			} else {
				status = "✅"
			}
			tool = "  └─ result"
			if m.Warning != "" {
				status += " " + m.Warning
			}
		}

		// Truncate path/args for table display
		displayPath := truncateStr(pathOrArgs, 55)

		fmt.Printf(" %-4d %-15s %-55s %8s  %s\n",
			m.Index, tool, displayPath, formatBytes(m.Size), status)

		// Show error details on a separate line
		if m.IsError && m.ErrorText != "" {
			errLine := truncateStr(m.ErrorText, 90)
			fmt.Printf("      %s%s\n", strings.Repeat(" ", 16), "Error: "+errLine)
		}
	}
	fmt.Println(strings.Repeat("─", 100))
}

// printStepDetail renders the --step=N output: full dump of a single step.
func printStepDetail(r inspectResult, stepN int) {
	if stepN >= len(r.Messages) {
		fmt.Fprintf(os.Stderr, "Error: step %d does not exist (conversation has %d messages)\n", stepN, len(r.Messages))
		os.Exit(1)
	}

	m := r.Messages[stepN]

	// Determine display type
	var direction string
	switch m.Role {
	case "user":
		direction = "user message"
	case "model":
		if m.ToolName != "" {
			direction = "model → " + m.ToolName
		} else {
			direction = "model response"
		}
	case "tool":
		direction = m.ToolName + " → result"
	}

	fmt.Printf("Step %d — %s\n", stepN, direction)
	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf("  Role:      %s\n", m.Role)
	fmt.Printf("  Size:      %s\n", formatBytes(m.Size))
	if m.Timestamp != "" {
		fmt.Printf("  Timestamp: %s\n", m.Timestamp)
	}

	if m.ToolName != "" {
		fmt.Printf("  Tool:      %s\n", m.ToolName)
	}

	if m.IsError {
		fmt.Printf("  Status:    ❌ ERROR\n")
	} else if m.Role == "tool" {
		fmt.Printf("  Status:    ✅ OK\n")
	}

	if m.FullPath != "" {
		fmt.Printf("  Path:      %s\n", m.FullPath)
	}

	// Tool args (pretty-printed JSON)
	if m.ToolArgs != "" {
		fmt.Printf("\n  Args:\n")
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(m.ToolArgs), &args); err == nil {
			for k, v := range args {
				val := fmt.Sprintf("%v", v)
				if len(val) > 100 {
					val = val[:97] + "..."
				}
				fmt.Printf("    %-20s %s\n", k+":", val)
			}
		} else {
			fmt.Printf("    %s\n", truncateStr(m.ToolArgs, 200))
		}
	}

	// Error text
	if m.IsError && m.ErrorText != "" {
		fmt.Printf("\n  Error:\n")
		// Show full error, wrapping at 80 chars
		for _, line := range strings.Split(m.ErrorText, "\n") {
			fmt.Printf("    %s\n", line)
		}
	}
}

// printErrorsView renders the --errors output: only steps that errored.
func printErrorsView(r inspectResult) {
	errors := 0
	fmt.Printf(" %-4s %-15s %-50s  %s\n", "#", "Tool", "Path", "Error")
	fmt.Println(strings.Repeat("─", 100))

	for _, m := range r.Messages {
		if !m.IsError {
			continue
		}
		errors++

		// Find the preceding model call to get the path
		path := ""
		if m.Index > 0 && m.Index-1 < len(r.Messages) {
			prev := r.Messages[m.Index-1]
			path = prev.FullPath
		}

		errText := truncateStr(m.ErrorText, 50)
		fmt.Printf(" %-4d %-15s %-50s  %s\n",
			m.Index, m.ToolName, truncateStr(path, 50), errText)
	}

	fmt.Println(strings.Repeat("─", 100))
	if errors == 0 {
		fmt.Println("  No errors found ✅")
	} else {
		fmt.Printf("\n  %d error(s) found\n", errors)
	}
}

// printInspectResult renders the default overview output.
func printInspectResult(r inspectResult, topN int) {
	printHeader(r)

	// Message table
	fmt.Printf(" %-4s %-7s %8s %9s   %s\n", "#", "Role", "Size", "Cumul", "Tool/Info")
	fmt.Println(strings.Repeat("─", 78))

	for _, m := range r.Messages {
		info := m.ToolInfo
		if info == "" && m.Role == "user" {
			info = "-"
		}
		if m.Warning != "" {
			info += "  " + m.Warning
		}
		fmt.Printf(" %-4d %-7s %8s %9s   %s\n",
			m.Index, m.Role, formatBytes(m.Size), formatBytes(m.Cumul), info)
	}
	fmt.Println(strings.Repeat("─", 78))

	// Top N largest
	top := topNMessages(r.Messages, topN)
	if len(top) > 0 {
		fmt.Printf("\n📊 Top %d Largest Messages:\n", len(top))
		for i, m := range top {
			info := m.ToolName
			if info == "" {
				info = m.Role
			}
			fmt.Printf("  #%d  msg[%d]  %-6s %8s  %s\n",
				i+1, m.Index, m.Role, formatBytes(m.Size), info)
		}
	}

	// Context breakdown
	fmt.Printf("\n📈 Context Breakdown:\n")
	fmt.Printf("  User messages:   %8s (%d messages)\n", formatBytes(r.Breakdown.UserBytes), r.Breakdown.UserCount)
	fmt.Printf("  Model messages:  %8s (%d messages)\n", formatBytes(r.Breakdown.ModelBytes), r.Breakdown.ModelCount)
	fmt.Printf("  Tool calls:      %8s (%d calls)\n", formatBytes(r.Breakdown.ToolCallBytes), r.Breakdown.ToolCallCount)
	fmt.Printf("  Tool results:    %8s (%d results)", formatBytes(r.Breakdown.ToolResultBytes), r.Breakdown.ToolResultCount)

	if r.TotalSize > 0 {
		pct := float64(r.Breakdown.ToolResultBytes) / float64(r.TotalSize) * 100
		if pct > 50 {
			fmt.Printf("  ← %.0f%% of total context", pct)
		}
	}
	fmt.Println()
}

// ---------- Formatting helpers ----------

// truncateStr truncates a string at maxLen with "..." suffix.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// formatBytes formats a byte count with comma separators and B suffix.
func formatBytes(b int) string {
	if b < 1000 {
		return fmt.Sprintf("%dB", b)
	}
	if b < 1_000_000 {
		return fmt.Sprintf("%s B", formatCount(b))
	}
	return fmt.Sprintf("%.1fMB", float64(b)/1_000_000)
}

// formatCount formats an integer with comma separators.
func formatCount(n int) string {
	if n < 0 {
		return "-" + formatCount(-n)
	}
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}

	var result strings.Builder
	remainder := len(s) % 3
	if remainder > 0 {
		result.WriteString(s[:remainder])
	}
	for i := remainder; i < len(s); i += 3 {
		if result.Len() > 0 {
			result.WriteByte(',')
		}
		result.WriteString(s[i : i+3])
	}
	return result.String()
}
