package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// AgentMode represents the interactive operation mode.
type AgentMode string

const (
	ModeDefault     AgentMode = "default"      // Standard safe mode: asks for file edits & commands
	ModeAcceptEdits AgentMode = "accept-edits" // Auto-accepts file edits; asks for shell commands
	ModePlan        AgentMode = "plan"         // Plan-before-act mode: forces research & planning first
)

// Next cycles to the next mode in sequence: default -> accept-edits -> plan -> default.
func (m AgentMode) Next() AgentMode {
	switch m {
	case ModeDefault:
		return ModeAcceptEdits
	case ModeAcceptEdits:
		return ModePlan
	case ModePlan:
		return ModeDefault
	default:
		return ModeDefault
	}
}

// StatusBarState holds data for the bottom status bar.
type StatusBarState struct {
	Status           string // IDLE, RUNNING, STREAMING, BLOCKED, WAITING
	Mode             AgentMode
	ModelName        string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	RunningSubagents int
	TotalSubagents   int
	YoloMode         bool
	WorkspaceCount   int
}

// RenderStatusBar formats and renders the full-width status bar.
func RenderStatusBar(state StatusBarState, width int) string {
	var leftParts []string

	// Status badge
	switch state.Status {
	case "STREAMING":
		leftParts = append(leftParts, BadgeStreaming.Render())
	case "RUNNING":
		leftParts = append(leftParts, BadgeRunning.Render())
	case "BLOCKED", "WAITING":
		leftParts = append(leftParts, BadgeWaiting.Render())
	default:
		leftParts = append(leftParts, BadgeIdle.Render())
	}

	// Mode badge (Default, Accept-Edits, Plan)
	switch state.Mode {
	case ModeAcceptEdits:
		leftParts = append(leftParts, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(ColorSecondary).Padding(0, 1).Render("⚡ ACCEPT-EDITS"))
	case ModePlan:
		leftParts = append(leftParts, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(ColorPrimary).Padding(0, 1).Render("📋 PLAN"))
	default:
		leftParts = append(leftParts, lipgloss.NewStyle().Foreground(ColorMuted).Background(ColorSubtle).Padding(0, 1).Render("🛡️ DEFAULT"))
	}

	// Model badge
	if state.ModelName != "" {
		leftParts = append(leftParts, lipgloss.NewStyle().Foreground(ColorHighlight).Render("🤖 "+state.ModelName))
	}

	// YOLO badge
	if state.YoloMode {
		leftParts = append(leftParts, BadgeYolo.Render())
	}

	// Subagent badge
	if state.RunningSubagents > 0 {
		subBadge := fmt.Sprintf("🤖 Subagents: %d running", state.RunningSubagents)
		leftParts = append(leftParts, BadgeSubagent.Render(subBadge))
	}

	// Token usage
	if state.TotalTokens > 0 {
		tokStr := fmt.Sprintf("Tokens: %s", formatTokens(state.TotalTokens))
		leftParts = append(leftParts, lipgloss.NewStyle().Foreground(ColorMuted).Render(tokStr))
	}

	// Workspaces count
	if state.WorkspaceCount > 0 {
		wsStr := fmt.Sprintf("📂 %d ws", state.WorkspaceCount)
		leftParts = append(leftParts, lipgloss.NewStyle().Foreground(ColorMuted).Render(wsStr))
	}

	left := strings.Join(leftParts, "  ")

	// Right side hints
	right := lipgloss.NewStyle().Foreground(ColorMuted).Render("Shift+Tab Mode │ ^D Detach │ ^C Stop │ /help")

	// Calculate spacing
	gap := width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}

	content := left + strings.Repeat(" ", gap) + right
	return StatusBarStyle.Width(width).Render(content)
}

func formatTokens(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}
