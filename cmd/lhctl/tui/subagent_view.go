package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// SubagentState tracks an individual subagent's lifecycle and transcript.
type SubagentState struct {
	ConversationID string
	ParentID       string
	TypeName       string
	Role           string
	State          string // RUNNING, IDLE, ERROR, COMPLETED
	Depth          int
	StepsExecuted  int
	Transcript     []string
}

// SubagentViewManager manages subagents state and rendering.
type SubagentViewManager struct {
	subagents       map[string]*SubagentState
	order           []string // List of conversation IDs
	selectedIndex   int
	drillDownID     string // If non-empty, viewing specific subagent transcript
	drillDownScroll int
}

// NewSubagentViewManager creates a subagent view manager.
func NewSubagentViewManager() *SubagentViewManager {
	return &SubagentViewManager{
		subagents: make(map[string]*SubagentState),
	}
}

// AddOrUpdate registers or updates a subagent.
func (m *SubagentViewManager) AddOrUpdate(sub *SubagentState) {
	if _, exists := m.subagents[sub.ConversationID]; !exists {
		m.order = append(m.order, sub.ConversationID)
	}
	m.subagents[sub.ConversationID] = sub
}

// AppendTranscript adds a step/message line to a subagent transcript.
func (m *SubagentViewManager) AppendTranscript(convID, line string) {
	if sub, exists := m.subagents[convID]; exists {
		sub.Transcript = append(sub.Transcript, line)
	}
}

// RunningCount returns the number of active running subagents.
func (m *SubagentViewManager) RunningCount() int {
	count := 0
	for _, sub := range m.subagents {
		if sub.State == "RUNNING" || sub.State == "TRAJ_RUNNING" {
			count++
		}
	}
	return count
}

// TotalCount returns total subagents registered.
func (m *SubagentViewManager) TotalCount() int {
	return len(m.subagents)
}

// NavigateUp moves the tree cursor up.
func (m *SubagentViewManager) NavigateUp() {
	if m.drillDownID != "" {
		if m.drillDownScroll > 0 {
			m.drillDownScroll--
		}
		return
	}
	if m.selectedIndex > 0 {
		m.selectedIndex--
	}
}

// NavigateDown moves the tree cursor down.
func (m *SubagentViewManager) NavigateDown() {
	if m.drillDownID != "" {
		if sub, ok := m.subagents[m.drillDownID]; ok && m.drillDownScroll < len(sub.Transcript)-1 {
			m.drillDownScroll++
		}
		return
	}
	if m.selectedIndex < len(m.order)-1 {
		m.selectedIndex++
	}
}

// SelectDrillDown enters transcript drill-down for currently selected subagent.
func (m *SubagentViewManager) SelectDrillDown() {
	if m.selectedIndex >= 0 && m.selectedIndex < len(m.order) {
		m.drillDownID = m.order[m.selectedIndex]
		m.drillDownScroll = 0
	}
}

// ExitDrillDown returns to tree list.
func (m *SubagentViewManager) ExitDrillDown() {
	m.drillDownID = ""
	m.drillDownScroll = 0
}

// IsDrillDown returns true if viewing transcript.
func (m *SubagentViewManager) IsDrillDown() bool {
	return m.drillDownID != ""
}

// Render renders the subagent tree or drill-down transcript view.
func (m *SubagentViewManager) Render(width, height int) string {
	if m.drillDownID != "" {
		return m.renderTranscript(width, height)
	}
	return m.renderTree(width, height)
}

func (m *SubagentViewManager) renderTree(width, height int) string {
	var sb strings.Builder
	sb.WriteString(TitleStyle.Render("🌳 SUBAGENT HIERARCHY TREE") + "\n\n")

	if len(m.order) == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("No subagents launched yet in this session.\n\nSubagents are spawned via invoke_subagent or /subagents.\n"))
		sb.WriteString(lipgloss.NewStyle().Faint(true).Render("Press Esc or q to return to chat."))
		return ModalBoxStyle.Width(min(width-4, 90)).Render(sb.String())
	}

	for i, id := range m.order {
		sub := m.subagents[id]
		indent := strings.Repeat("  │ ", sub.Depth)
		branch := "├── "
		if i == len(m.order)-1 || (i < len(m.order)-1 && m.subagents[m.order[i+1]].Depth < sub.Depth) {
			branch = "└── "
		}

		cursor := "  "
		itemStyle := lipgloss.NewStyle().Foreground(ColorText)
		if i == m.selectedIndex {
			cursor = "▶ "
			itemStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorHighlight)
		}

		statusColor := ColorSecondary
		statusIcon := "✅"
		if sub.State == "RUNNING" || sub.State == "TRAJ_RUNNING" {
			statusColor = ColorWarning
			statusIcon = "⏳"
		} else if sub.State == "ERROR" || sub.State == "TRAJ_ERROR" {
			statusColor = ColorError
			statusIcon = "❌"
		}

		role := sub.Role
		if role == "" {
			role = sub.TypeName
		}
		if role == "" {
			role = "Subagent"
		}

		line := fmt.Sprintf("%s%s%s🤖 %s [%s] (%s %s, %d steps)",
			cursor, indent, branch,
			itemStyle.Render(id[:min(8, len(id))]),
			role,
			statusIcon,
			lipgloss.NewStyle().Foreground(statusColor).Render(sub.State),
			sub.StepsExecuted,
		)
		sb.WriteString(line + "\n")
	}

	sb.WriteString("\n" + lipgloss.NewStyle().Faint(true).Render("Use ↑/↓ to navigate • Enter to view transcript • Esc/q to exit"))
	return ModalBoxStyle.Width(min(width-4, 90)).Render(sb.String())
}

func (m *SubagentViewManager) renderTranscript(width, height int) string {
	sub, ok := m.subagents[m.drillDownID]
	if !ok {
		return "Subagent not found"
	}

	var sb strings.Builder
	title := fmt.Sprintf("📜 Transcript: %s (%s)", sub.ConversationID[:min(8, len(sub.ConversationID))], sub.Role)
	sb.WriteString(TitleStyle.Render(title) + "\n\n")

	if len(sub.Transcript) == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("No transcript events recorded yet.\n"))
	} else {
		start := m.drillDownScroll
		if start < 0 {
			start = 0
		}
		maxLines := height - 8
		if maxLines < 5 {
			maxLines = 15
		}

		boxWidth := min(width-4, 96)
		contentWidth := max(20, boxWidth-6)

		for i := start; i < len(sub.Transcript) && i < start+maxLines; i++ {
			wrapped := lipgloss.NewStyle().Width(contentWidth).Render(sub.Transcript[i])
			sb.WriteString(wrapped + "\n")
		}
	}

	sb.WriteString("\n" + lipgloss.NewStyle().Faint(true).Render("Use ↑/↓ to scroll • Esc/q to return to tree"))
	return ModalBoxStyle.Width(min(width-4, 96)).Render(sb.String())
}


func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
