package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

// SemanticAction defines the high-level human-readable action category.
type SemanticAction int

const (
	ActionUnknown SemanticAction = iota
	ActionRead
	ActionSearch
	ActionFind
	ActionWrite
	ActionRun
	ActionBrowse
	ActionDesktop
	ActionAgent
)

func (a SemanticAction) String() string {
	switch a {
	case ActionRead:
		return "Read"
	case ActionSearch:
		return "Search"
	case ActionFind:
		return "Find"
	case ActionWrite:
		return "Write"
	case ActionRun:
		return "Run"
	case ActionBrowse:
		return "Browse"
	case ActionDesktop:
		return "Desktop"
	case ActionAgent:
		return "Agent"
	default:
		return "Tool"
	}
}

func (a SemanticAction) Verb() string {
	switch a {
	case ActionRead:
		return "Reading"
	case ActionSearch:
		return "Searching"
	case ActionFind:
		return "Finding"
	case ActionWrite:
		return "Writing"
	case ActionRun:
		return "Running"
	case ActionBrowse:
		return "Browsing"
	case ActionDesktop:
		return "Desktop"
	case ActionAgent:
		return "Agent"
	default:
		return "Running"
	}
}

func (a SemanticAction) DoneVerb() string {
	switch a {
	case ActionRead:
		return "Read"
	case ActionSearch:
		return "Searched"
	case ActionFind:
		return "Found"
	case ActionWrite:
		return "Wrote"
	case ActionRun:
		return "Ran"
	case ActionBrowse:
		return "Browsed"
	case ActionDesktop:
		return "Desktop"
	case ActionAgent:
		return "Agent"
	default:
		return "Ran"
	}
}

func (a SemanticAction) BadgeStyle() lipgloss.Style {
	switch a {
	case ActionRead:
		return ActionBadgeRead
	case ActionSearch:
		return ActionBadgeSearch
	case ActionFind:
		return ActionBadgeFind
	case ActionWrite:
		return ActionBadgeWrite
	case ActionRun:
		return ActionBadgeRun
	case ActionBrowse:
		return ActionBadgeBrowse
	case ActionDesktop:
		return ActionBadgeDesktop
	case ActionAgent:
		return ActionBadgeAgent
	default:
		return ToolCallHeaderStyle
	}
}

func inferSemanticAction(toolName string) SemanticAction {
	lower := strings.ToLower(toolName)
	switch {
	case lower == "view_file" || lower == "read_url_content" || strings.HasPrefix(lower, "read_"):
		return ActionRead
	case lower == "grep_search" || lower == "search_web" || strings.Contains(lower, "search"):
		return ActionSearch
	case lower == "find_file" || lower == "list_dir" || strings.HasPrefix(lower, "find_") || strings.HasPrefix(lower, "list_"):
		return ActionFind
	case lower == "write_to_file" || lower == "replace_file_content" || lower == "multi_replace_file_content" || strings.HasPrefix(lower, "write_") || strings.HasPrefix(lower, "edit_"):
		return ActionWrite
	case lower == "run_command" || lower == "execute_command" || lower == "bash" || lower == "sh":
		return ActionRun
	case strings.HasPrefix(lower, "browser_") || strings.HasPrefix(lower, "playwright_"):
		return ActionBrowse
	case strings.HasPrefix(lower, "desktop_"):
		return ActionDesktop
	case strings.Contains(lower, "subagent"):
		return ActionAgent
	default:
		return ActionUnknown
	}
}

// ChatItemType defines the kind of chat entry.
type ChatItemType int

const (
	ChatItemUser ChatItemType = iota
	ChatItemAssistant
	ChatItemThinking
	ChatItemToolCall
	ChatItemToolResult
	ChatItemSystem
	ChatItemError
	ChatItemSideQuestion
)

// ChatItem represents a rendered entry in the conversation log.
type ChatItem struct {
	Type           ChatItemType
	Content        string
	Timestamp      time.Time
	ToolName       string
	ToolArgs       string
	Duration       time.Duration
	IsActive       bool
	IsError        bool
	DiffBlock      string
	SemanticAction SemanticAction
	Target         string
	Summary        string
}

// ChatHistory manages the ordered list of chat items and streaming buffer.
type ChatHistory struct {
	items             []ChatItem
	streamingText     strings.Builder
	thinkingText      strings.Builder
	thinkingStartTime time.Time
	activeToolItem    *ChatItem
	toolStartTime     time.Time
	workspaces        []string
	showThinking      bool
}

// NewChatHistory creates a new chat history tracker.
func NewChatHistory() *ChatHistory {
	return &ChatHistory{}
}

// SetWorkspaces sets the current workspace roots for relative path display.
func (h *ChatHistory) SetWorkspaces(ws []string) {
	h.workspaces = ws
}

// SetShowThinking toggles raw thinking visibility.
func (h *ChatHistory) SetShowThinking(show bool) {
	h.showThinking = show
}

// LoadFromState populates chat history from a loaded ConversationState protobuf.
func (h *ChatHistory) LoadFromState(state *pb.ConversationState) {
	if state == nil {
		return
	}
	for _, msg := range state.Messages {
		switch msg.Role {
		case "user":
			content := msg.Content
			if content == "" && len(msg.Parts) > 0 {
				content = strings.Join(msg.Parts, "\n")
			}
			h.items = append(h.items, ChatItem{
				Type:      ChatItemUser,
				Content:   content,
				Timestamp: time.Now(),
			})
		case "model", "assistant":
			if msg.Content != "" {
				h.items = append(h.items, ChatItem{
					Type:      ChatItemAssistant,
					Content:   msg.Content,
					Timestamp: time.Now(),
				})
			}
			for _, tc := range msg.ToolCalls {
				action := inferSemanticAction(tc.Name)
				target := extractTargetFromArgs(tc.Name, tc.ArgsJson)
				h.items = append(h.items, ChatItem{
					Type:           ChatItemToolCall,
					ToolName:       tc.Name,
					ToolArgs:       tc.ArgsJson,
					SemanticAction: action,
					Target:         target,
					Timestamp:      time.Now(),
				})
			}
		case "tool":
			if msg.ToolResult != nil {
				// Attach result cleanly to the matching preceding tool call if available
				if len(h.items) > 0 && h.items[len(h.items)-1].Type == ChatItemToolCall && h.items[len(h.items)-1].ToolName == msg.ToolResult.Name {
					last := &h.items[len(h.items)-1]
					last.IsError = msg.ToolResult.IsError
					if msg.ToolResult.IsError {
						last.Content = msg.ToolResult.Content
						last.Summary = "failed"
					} else {
						lineCount := strings.Count(msg.ToolResult.Content, "\n")
						if lineCount > 0 {
							last.Summary = fmt.Sprintf("%d lines", lineCount)
						} else if len(msg.ToolResult.Content) > 0 && len(msg.ToolResult.Content) < 40 {
							last.Summary = msg.ToolResult.Content
						} else {
							last.Summary = "ok"
						}
					}
				} else {
					itemType := ChatItemToolResult
					if msg.ToolResult.IsError {
						itemType = ChatItemError
					}
					h.items = append(h.items, ChatItem{
						Type:      itemType,
						ToolName:  msg.ToolResult.Name,
						Content:   msg.ToolResult.Content,
						IsError:   msg.ToolResult.IsError,
						Timestamp: time.Now(),
					})
				}
			}
		case "system":
			if msg.Content != "" {
				h.items = append(h.items, ChatItem{
					Type:      ChatItemSystem,
					Content:   msg.Content,
					Timestamp: time.Now(),
				})
			}
		}
	}
}

// ActiveToolItem returns the currently running tool call item, if any.
func (h *ChatHistory) ActiveToolItem() *ChatItem {
	return h.activeToolItem
}

// ToolStartTime returns the start timestamp of the currently running tool.
func (h *ChatHistory) ToolStartTime() time.Time {
	return h.toolStartTime
}

// LastItem returns a pointer to the most recent chat item, if any.
func (h *ChatHistory) LastItem() *ChatItem {
	if len(h.items) == 0 {
		return nil
	}
	return &h.items[len(h.items)-1]
}

// AddUserMessage appends a user prompt and returns the created item.
func (h *ChatHistory) AddUserMessage(content string) ChatItem {
	h.FlushStreaming()
	item := ChatItem{
		Type:      ChatItemUser,
		Content:   content,
		Timestamp: time.Now(),
	}
	h.items = append(h.items, item)
	return item
}

// AppendStreamingText adds a text chunk during LLM generation.
func (h *ChatHistory) AppendStreamingText(delta string) {
	h.streamingText.WriteString(delta)
}

// AppendThinkingText adds thinking/reasoning delta.
func (h *ChatHistory) AppendThinkingText(delta string) {
	if h.thinkingText.Len() == 0 {
		h.thinkingStartTime = time.Now()
	}
	h.thinkingText.WriteString(delta)
}

// FlushStreaming commits any active streaming or thinking buffer into chat items and returns newly created items.
func (h *ChatHistory) FlushStreaming() []ChatItem {
	var created []ChatItem
	if h.thinkingText.Len() > 0 {
		dur := time.Since(h.thinkingStartTime)
		item := ChatItem{
			Type:      ChatItemThinking,
			Content:   h.thinkingText.String(),
			Duration:  dur,
			Timestamp: time.Now(),
		}
		h.items = append(h.items, item)
		created = append(created, item)
		h.thinkingText.Reset()
	}
	if h.streamingText.Len() > 0 {
		item := ChatItem{
			Type:      ChatItemAssistant,
			Content:   h.streamingText.String(),
			Timestamp: time.Now(),
		}
		h.items = append(h.items, item)
		created = append(created, item)
		h.streamingText.Reset()
	}
	return created
}

// StartToolCall registers an active tool execution and returns any flushed streaming items.
func (h *ChatHistory) StartToolCall(name, args string) []ChatItem {
	flushed := h.FlushStreaming()
	action := inferSemanticAction(name)
	target := extractTargetFromArgs(name, args)
	item := ChatItem{
		Type:           ChatItemToolCall,
		ToolName:       name,
		ToolArgs:       args,
		SemanticAction: action,
		Target:         target,
		Timestamp:      time.Now(),
		IsActive:       true,
	}
	h.items = append(h.items, item)
	h.activeToolItem = &h.items[len(h.items)-1]
	h.toolStartTime = time.Now()
	return flushed
}

// FinishToolCall marks the active tool execution as completed and returns the finished item.
func (h *ChatHistory) FinishToolCall(name string, result string, isError bool, diff string) *ChatItem {
	dur := time.Since(h.toolStartTime)
	if h.activeToolItem != nil && h.activeToolItem.ToolName == name {
		h.activeToolItem.IsActive = false
		h.activeToolItem.Duration = dur
		h.activeToolItem.IsError = isError
		h.activeToolItem.Summary = result
		h.activeToolItem.DiffBlock = diff
		if isError {
			h.activeToolItem.Content = result
		}
		res := h.activeToolItem
		h.activeToolItem = nil
		return res
	}

	action := inferSemanticAction(name)
	target := extractTargetFromArgs(name, "")
	item := ChatItem{
		Type:           ChatItemToolCall,
		ToolName:       name,
		SemanticAction: action,
		Target:         target,
		Duration:       dur,
		IsError:        isError,
		Summary:        result,
		DiffBlock:      diff,
		Content:        result,
		Timestamp:      time.Now(),
	}
	h.items = append(h.items, item)
	return &item
}

// AddSystemMessage appends an informational system notification and returns the item.
func (h *ChatHistory) AddSystemMessage(content string) ChatItem {
	h.FlushStreaming()
	item := ChatItem{
		Type:      ChatItemSystem,
		Content:   content,
		Timestamp: time.Now(),
	}
	h.items = append(h.items, item)
	return item
}

// AddSideQuestion appends a side inquiry and response without disrupting the main trajectory and returns the item.
func (h *ChatHistory) AddSideQuestion(question, answer string) ChatItem {
	h.FlushStreaming()
	item := ChatItem{
		Type:      ChatItemSideQuestion,
		ToolName:  question,
		Content:   answer,
		Timestamp: time.Now(),
	}
	h.items = append(h.items, item)
	return item
}

// Clear flushes all history.
func (h *ChatHistory) Clear() {
	h.items = nil
	h.streamingText.Reset()
	h.thinkingText.Reset()
	h.activeToolItem = nil
}

// RenderView renders the entire chat history for viewport display with line wrapping.
func (h *ChatHistory) RenderView(spin spinner.Model, width int) string {
	contentWidth := width - 4
	if contentWidth < 20 {
		contentWidth = 20
	}

	var sb strings.Builder

	for _, item := range h.items {
		if item.IsActive {
			dur := time.Since(h.toolStartTime).Round(100 * time.Millisecond)
			action := item.SemanticAction
			if action == ActionUnknown {
				action = inferSemanticAction(item.ToolName)
			}
			target := item.Target
			if target == "" {
				target = extractTargetFromArgs(item.ToolName, item.ToolArgs)
			}
			target = formatRelativePath(target, h.workspaces)
			maxTargetLen := max(15, contentWidth-35)
			if len(target) > maxTargetLen {
				target = target[:maxTargetLen-3] + "..."
			}

			spinnerView := spin.View()
			verb := action.Verb()
			if action == ActionUnknown {
				verb = "Running " + item.ToolName
			}
			line := fmt.Sprintf("  %s %s %s [%s]",
				spinnerView,
				action.BadgeStyle().Render(verb),
				ActionTargetStyle.Render(target),
				lipgloss.NewStyle().Foreground(ColorWarning).Render(dur.String()),
			)
			sb.WriteString(line + "\n")
		} else {
			rendered := h.RenderItem(item, width)
			if rendered != "" {
				sb.WriteString(rendered)
			}
		}
	}

	// Live streaming buffer
	if h.thinkingText.Len() > 0 {
		if h.showThinking {
			sb.WriteString("\n" + ThinkingStyle.Width(contentWidth).Render("💭 Thinking:\n"+h.thinkingText.String()) + "\n")
		} else {
			dur := time.Since(h.thinkingStartTime).Round(100 * time.Millisecond)
			sb.WriteString("  " + spin.View() + " " + ThinkingCollapsedStyle.Render(fmt.Sprintf("Thinking... [%s]", dur.String())) + "\n")
		}
	}
	if h.streamingText.Len() > 0 {
		sb.WriteString("\n" + AssistantMsgStyle.Render("🤖 Assistant:") + "\n" + wrapString(h.streamingText.String(), contentWidth) + "\n")
	}

	return sb.String()
}

// RenderItem formats a single completed chat item for inline terminal output.
func (h *ChatHistory) RenderItem(item ChatItem, width int) string {
	contentWidth := width - 4
	if contentWidth < 20 {
		contentWidth = 20
	}

	switch item.Type {
	case ChatItemUser:
		return "\n" + UserMsgStyle.Render("🧑 You:") + "\n" + wrapString(item.Content, contentWidth) + "\n"

	case ChatItemAssistant:
		return "\n" + AssistantMsgStyle.Render("🤖 Assistant:") + "\n" + wrapString(item.Content, contentWidth) + "\n"

	case ChatItemThinking:
		if h.showThinking {
			return "\n" + ThinkingStyle.Width(contentWidth).Render("💭 Thinking:\n"+item.Content) + "\n"
		}
		thoughtDur := item.Duration
		durStr := ""
		if thoughtDur > 0 {
			durStr = fmt.Sprintf(" for %s", thoughtDur.Round(100*time.Millisecond).String())
		}
		return "  " + ThinkingCollapsedStyle.Render(fmt.Sprintf("💭 Thought%s", durStr)) + "\n"

	case ChatItemToolCall:
		action := item.SemanticAction
		if action == ActionUnknown {
			action = inferSemanticAction(item.ToolName)
		}
		target := item.Target
		if target == "" {
			target = extractTargetFromArgs(item.ToolName, item.ToolArgs)
		}
		target = formatRelativePath(target, h.workspaces)
		maxTargetLen := max(15, contentWidth-35)
		if len(target) > maxTargetLen {
			target = target[:maxTargetLen-3] + "..."
		}

		if item.IsError {
			durStr := ""
			if item.Duration > 0 {
				durStr = fmt.Sprintf(" [%s]", item.Duration.Round(10*time.Millisecond).String())
			}
			line := fmt.Sprintf("  %s %s %s%s",
				lipgloss.NewStyle().Bold(true).Foreground(ColorError).Render("✗"),
				lipgloss.NewStyle().Bold(true).Foreground(ColorError).Render("Failed: "+action.String()),
				ActionTargetStyle.Render(target),
				ActionDurStyle.Render(durStr),
			)
			out := line + "\n"
			if item.Content != "" {
				errStr := strings.TrimSpace(item.Content)
				if len(errStr) > 400 {
					errStr = errStr[:400] + "..."
				}
				out += "    " + ErrorMsgStyle.Render(wrapString(errStr, contentWidth-6)) + "\n"
			}
			return out
		}

		summary := item.Summary
		if summary != "" {
			if len(summary) > 60 || strings.Contains(summary, "\n") {
				summary = ""
			} else {
				summary = "(" + summary + ")"
			}
		}
		verb := action.DoneVerb()
		if action == ActionUnknown {
			verb = item.ToolName
		}
		durStr := ""
		if item.Duration > 0 {
			durStr = "· " + item.Duration.Round(10*time.Millisecond).String()
		}

		var line string
		if summary != "" {
			line = fmt.Sprintf("  %s %s %s %s %s",
				action.BadgeStyle().Render("●"),
				action.BadgeStyle().Render(verb),
				ActionTargetStyle.Render(target),
				ActionMetricStyle.Render(summary),
				ActionDurStyle.Render(durStr),
			)
		} else {
			line = fmt.Sprintf("  %s %s %s %s",
				action.BadgeStyle().Render("●"),
				action.BadgeStyle().Render(verb),
				ActionTargetStyle.Render(target),
				ActionDurStyle.Render(durStr),
			)
		}
		out := strings.TrimRight(line, " ") + "\n"
		if item.DiffBlock != "" {
			out += renderDiffSnippet(item.DiffBlock, contentWidth) + "\n"
		}
		return out

	case ChatItemToolResult:
		if item.DiffBlock != "" {
			return renderDiffSnippet(item.DiffBlock, contentWidth) + "\n"
		} else if item.Content != "" {
			res := strings.TrimSpace(item.Content)
			if len(res) > 200 {
				res = res[:200] + "..."
			}
			wrapped := wrapString(res, contentWidth-4)
			return "    " + lipgloss.NewStyle().Foreground(ColorMuted).Render(wrapped) + "\n"
		}
		return ""

	case ChatItemError:
		wrapped := wrapString("Error: "+item.Content, contentWidth-4)
		return "    " + ErrorMsgStyle.Render(wrapped) + "\n"

	case ChatItemSystem:
		wrapped := wrapString("ℹ️  "+item.Content, contentWidth)
		return "\n" + SystemMsgStyle.Render(wrapped) + "\n"

	case ChatItemSideQuestion:
		sideBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorHighlight).
			Padding(0, 1).
			Width(contentWidth)
		title := lipgloss.NewStyle().Bold(true).Foreground(ColorHighlight).Render("💬 Side Question (/btw): ") + item.ToolName
		body := wrapString(item.Content, contentWidth-4)
		return "\n" + sideBox.Render(title+"\n\n"+body) + "\n"
	}

	return ""
}

// FormatInitialHistory formats all pre-existing chat items for emission into terminal scrollback on startup.
func (h *ChatHistory) FormatInitialHistory(width int) string {
	var sb strings.Builder
	for _, item := range h.items {
		rendered := strings.TrimSpace(h.RenderItem(item, width))
		if rendered != "" {
			sb.WriteString(rendered + "\n")
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

func wrapString(text string, width int) string {
	if width <= 0 {
		return text
	}
	return lipgloss.NewStyle().Width(width).Render(text)
}

func extractTargetFromArgs(name, args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return ""
	}
	if strings.HasPrefix(args, "{") && strings.HasSuffix(args, "}") {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(args), &m); err == nil {
			for _, k := range []string{
				"TargetFile", "TargetDirectory", "path", "Path", "file", "File",
				"DirectoryPath", "SearchDirectory", "SearchPath", "query", "Query",
				"pattern", "Pattern", "command", "Command", "CommandLine",
				"url", "Url", "URL", "TaskName", "Task",
			} {
				if v, ok := m[k].(string); ok && v != "" {
					return v
				}
			}
		}
	}
	for _, prefix := range []string{"path: ", "Path: ", "TargetFile: ", "Query: ", "Command: ", "command: "} {
		if strings.HasPrefix(args, prefix) {
			return strings.TrimPrefix(args, prefix)
		}
	}
	return args
}

func formatRelativePath(path string, workspaces []string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	// Do not touch URLs or shell commands with spaces/flags
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") || strings.Contains(path, " ") {
		return path
	}

	clean := filepath.Clean(path)
	for _, ws := range workspaces {
		absWS, err := filepath.Abs(ws)
		if err != nil {
			absWS = ws
		}
		if clean == absWS {
			return "."
		}
		if strings.HasPrefix(clean, absWS+string(filepath.Separator)) {
			rel, err := filepath.Rel(absWS, clean)
			if err == nil && !strings.HasPrefix(rel, "..") {
				return rel
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(clean, home+string(filepath.Separator)) {
		rel, err := filepath.Rel(home, clean)
		if err == nil {
			return "~/" + rel
		}
	}
	return clean
}

func renderDiffSnippet(diff string, width int) string {
	lines := strings.Split(diff, "\n")
	maxL := 10
	if len(lines) > maxL {
		lines = lines[:maxL]
		lines = append(lines, lipgloss.NewStyle().Faint(true).Render("..."))
	}

	maxLineW := max(20, width-8)
	var sb strings.Builder
	for _, l := range lines {
		if len(l) > maxLineW {
			l = l[:maxLineW-3] + "..."
		}
		switch {
		case strings.HasPrefix(l, "+"):
			sb.WriteString("    " + DiffAddStyle.Render(l) + "\n")
		case strings.HasPrefix(l, "-"):
			sb.WriteString("    " + DiffRemoveStyle.Render(l) + "\n")
		case strings.HasPrefix(l, "@@"):
			sb.WriteString("    " + DiffHeaderStyle.Render(l) + "\n")
		default:
			sb.WriteString("    " + lipgloss.NewStyle().Faint(true).Render(l) + "\n")
		}
	}
	return sb.String()
}

// LastAssistantResponse returns the content of the most recent assistant message.
func (h *ChatHistory) LastAssistantResponse() string {
	for i := len(h.items) - 1; i >= 0; i-- {
		if h.items[i].Type == ChatItemAssistant && strings.TrimSpace(h.items[i].Content) != "" {
			return strings.TrimSpace(h.items[i].Content)
		}
	}
	return ""
}

// LastCodeBlock returns the content of the most recent markdown fenced code block from assistant messages.
func (h *ChatHistory) LastCodeBlock() string {
	for i := len(h.items) - 1; i >= 0; i-- {
		if h.items[i].Type == ChatItemAssistant {
			code := extractLastCodeBlock(h.items[i].Content)
			if code != "" {
				return code
			}
		}
	}
	return ""
}

// FullTranscript returns a clean plaintext transcript of the conversation history.
func (h *ChatHistory) FullTranscript() string {
	var sb strings.Builder
	for _, item := range h.items {
		switch item.Type {
		case ChatItemUser:
			sb.WriteString(fmt.Sprintf("User: %s\n\n", item.Content))
		case ChatItemAssistant:
			sb.WriteString(fmt.Sprintf("Assistant: %s\n\n", item.Content))
		case ChatItemToolCall:
			if item.Target != "" {
				sb.WriteString(fmt.Sprintf("[%s] %s\n\n", item.ToolName, item.Target))
			} else {
				sb.WriteString(fmt.Sprintf("[%s]\n\n", item.ToolName))
			}
		case ChatItemSystem:
			sb.WriteString(fmt.Sprintf("System: %s\n\n", item.Content))
		}
	}
	return strings.TrimSpace(sb.String())
}

// extractLastCodeBlock extracts the last fenced code block (```...```) from content.
func extractLastCodeBlock(content string) string {
	parts := strings.Split(content, "```")
	if len(parts) < 3 {
		return ""
	}
	// Parts with odd index are between ``` and ```
	for i := len(parts) - 2; i >= 1; i -= 2 {
		block := parts[i]
		if newlineIdx := strings.Index(block, "\n"); newlineIdx != -1 {
			block = block[newlineIdx+1:]
		}
		trimmed := strings.TrimRight(block, "\r\n")
		if strings.TrimSpace(trimmed) != "" {
			return trimmed
		}
	}
	return ""
}
