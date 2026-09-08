package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

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
)

// ChatItem represents a rendered entry in the conversation log.
type ChatItem struct {
	Type      ChatItemType
	Content   string
	Timestamp time.Time
	ToolName  string
	ToolArgs  string
	Duration  time.Duration
	IsActive  bool
	IsError   bool
	DiffBlock string
}

// ChatHistory manages the ordered list of chat items and streaming buffer.
type ChatHistory struct {
	items          []ChatItem
	streamingText  strings.Builder
	thinkingText   strings.Builder
	activeToolItem *ChatItem
	toolStartTime  time.Time
}

// NewChatHistory creates a new chat history tracker.
func NewChatHistory() *ChatHistory {
	return &ChatHistory{}
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
				h.items = append(h.items, ChatItem{
					Type:      ChatItemToolCall,
					ToolName:  tc.Name,
					ToolArgs:  tc.ArgsJson,
					Timestamp: time.Now(),
				})
			}
		case "tool":
			if msg.ToolResult != nil {
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

// AddUserMessage appends a user prompt.
func (h *ChatHistory) AddUserMessage(content string) {
	h.FlushStreaming()
	h.items = append(h.items, ChatItem{
		Type:      ChatItemUser,
		Content:   content,
		Timestamp: time.Now(),
	})
}

// AppendStreamingText adds a text chunk during LLM generation.
func (h *ChatHistory) AppendStreamingText(delta string) {
	h.streamingText.WriteString(delta)
}

// AppendThinkingText adds thinking/reasoning delta.
func (h *ChatHistory) AppendThinkingText(delta string) {
	h.thinkingText.WriteString(delta)
}

// FlushStreaming commits any active streaming or thinking buffer into chat items.
func (h *ChatHistory) FlushStreaming() {
	if h.thinkingText.Len() > 0 {
		h.items = append(h.items, ChatItem{
			Type:      ChatItemThinking,
			Content:   h.thinkingText.String(),
			Timestamp: time.Now(),
		})
		h.thinkingText.Reset()
	}
	if h.streamingText.Len() > 0 {
		h.items = append(h.items, ChatItem{
			Type:      ChatItemAssistant,
			Content:   h.streamingText.String(),
			Timestamp: time.Now(),
		})
		h.streamingText.Reset()
	}
}

// StartToolCall registers an active tool execution.
func (h *ChatHistory) StartToolCall(name, args string) {
	h.FlushStreaming()
	item := ChatItem{
		Type:      ChatItemToolCall,
		ToolName:  name,
		ToolArgs:  args,
		Timestamp: time.Now(),
		IsActive:  true,
	}
	h.items = append(h.items, item)
	h.activeToolItem = &h.items[len(h.items)-1]
	h.toolStartTime = time.Now()
}

// FinishToolCall marks the active tool execution as completed.
func (h *ChatHistory) FinishToolCall(name string, result string, isError bool, diff string) {
	dur := time.Since(h.toolStartTime)
	if h.activeToolItem != nil && h.activeToolItem.ToolName == name {
		h.activeToolItem.IsActive = false
		h.activeToolItem.Duration = dur
		h.activeToolItem.IsError = isError
		h.activeToolItem = nil
	}

	itemType := ChatItemToolResult
	if isError {
		itemType = ChatItemError
	}

	h.items = append(h.items, ChatItem{
		Type:      itemType,
		ToolName:  name,
		Content:   result,
		Duration:  dur,
		IsError:   isError,
		DiffBlock: diff,
		Timestamp: time.Now(),
	})
}

// AddSystemMessage appends an informational system notification.
func (h *ChatHistory) AddSystemMessage(content string) {
	h.FlushStreaming()
	h.items = append(h.items, ChatItem{
		Type:      ChatItemSystem,
		Content:   content,
		Timestamp: time.Now(),
	})
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
		switch item.Type {
		case ChatItemUser:
			sb.WriteString("\n" + UserMsgStyle.Render("🧑 You:") + "\n" + wrapString(item.Content, contentWidth) + "\n")

		case ChatItemAssistant:
			sb.WriteString("\n" + AssistantMsgStyle.Render("🤖 Assistant:") + "\n" + wrapString(item.Content, contentWidth) + "\n")

		case ChatItemThinking:
			sb.WriteString("\n" + ThinkingStyle.Width(contentWidth).Render("💭 Thinking:\n"+item.Content) + "\n")

		case ChatItemToolCall:
			if item.IsActive {
				dur := time.Since(h.toolStartTime).Round(100 * time.Millisecond)
				spinnerView := spin.View()
				argsSummary := summarizeToolArgs(item.ToolArgs, max(20, contentWidth-35))
				line := fmt.Sprintf("%s %s %s [%s]",
					spinnerView,
					ToolCallHeaderStyle.Render("Running "+item.ToolName),
					lipgloss.NewStyle().Faint(true).Render(argsSummary),
					lipgloss.NewStyle().Foreground(ColorWarning).Render(dur.String()),
				)
				sb.WriteString(line + "\n")
			} else {
				icon := "✅"
				if item.IsError {
					icon = "❌"
				}
				argsSummary := summarizeToolArgs(item.ToolArgs, max(20, contentWidth-30))
				line := fmt.Sprintf("  %s %s %s [%s]",
					icon,
					ToolCallHeaderStyle.Render(item.ToolName),
					lipgloss.NewStyle().Faint(true).Render(argsSummary),
					lipgloss.NewStyle().Faint(true).Render(item.Duration.Round(10*time.Millisecond).String()),
				)
				sb.WriteString(line + "\n")
			}

		case ChatItemToolResult:
			if item.DiffBlock != "" {
				sb.WriteString(renderDiffSnippet(item.DiffBlock, contentWidth) + "\n")
			} else if item.Content != "" {
				res := strings.TrimSpace(item.Content)
				if len(res) > 500 {
					res = res[:500] + "..."
				}
				wrapped := wrapString(res, contentWidth-4)
				sb.WriteString("    " + lipgloss.NewStyle().Foreground(ColorMuted).Render(wrapped) + "\n")
			}

		case ChatItemError:
			wrapped := wrapString("Error: "+item.Content, contentWidth-4)
			sb.WriteString("    " + ErrorMsgStyle.Render(wrapped) + "\n")

		case ChatItemSystem:
			wrapped := wrapString("ℹ️  "+item.Content, contentWidth)
			sb.WriteString("\n" + SystemMsgStyle.Render(wrapped) + "\n")
		}
	}

	// Live streaming buffer
	if h.thinkingText.Len() > 0 {
		sb.WriteString("\n" + ThinkingStyle.Width(contentWidth).Render("💭 Thinking:\n"+h.thinkingText.String()) + "\n")
	}
	if h.streamingText.Len() > 0 {
		sb.WriteString("\n" + AssistantMsgStyle.Render("🤖 Assistant:") + "\n" + wrapString(h.streamingText.String(), contentWidth) + "\n")
	}

	return sb.String()
}

func wrapString(text string, width int) string {
	if width <= 0 {
		return text
	}
	return lipgloss.NewStyle().Width(width).Render(text)
}

func summarizeToolArgs(args string, maxLen int) string {
	args = strings.ReplaceAll(args, "\n", " ")
	args = strings.TrimSpace(args)
	if maxLen > 5 && len(args) > maxLen {
		return args[:maxLen-3] + "..."
	}
	return args
}

func renderDiffSnippet(diff string, width int) string {
	lines := strings.Split(diff, "\n")
	maxL := 12
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
