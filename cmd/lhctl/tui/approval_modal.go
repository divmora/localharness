package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ActiveApproval holds state for a pending tool confirmation prompt.
type ActiveApproval struct {
	RequestID   string
	ToolName    string
	Description string
	DiffPreview string
}

// RenderApprovalModal renders an interactive confirmation dialog with unified diff highlighting.
func RenderApprovalModal(app *ActiveApproval, width int) string {
	if app == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(BadgeWaiting.Render() + " " + ToolCallHeaderStyle.Render("Tool Approval Required: "+app.ToolName) + "\n\n")

	if app.Description != "" {
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Action: ") + app.Description + "\n\n")
	}

	if app.DiffPreview != "" {
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorHighlight).Render("Unified Diff Preview:") + "\n")
		lines := strings.Split(app.DiffPreview, "\n")
		maxLines := 20
		if len(lines) > maxLines {
			lines = lines[:maxLines]
			lines = append(lines, lipgloss.NewStyle().Faint(true).Render(fmt.Sprintf("... (%d more lines hidden)", len(strings.Split(app.DiffPreview, "\n"))-maxLines)))
		}

		var diffContent strings.Builder
		for _, l := range lines {
			switch {
			case strings.HasPrefix(l, "+"):
				diffContent.WriteString(DiffAddStyle.Render(l) + "\n")
			case strings.HasPrefix(l, "-"):
				diffContent.WriteString(DiffRemoveStyle.Render(l) + "\n")
			case strings.HasPrefix(l, "@@") || strings.HasPrefix(l, "---") || strings.HasPrefix(l, "+++"):
				diffContent.WriteString(DiffHeaderStyle.Render(l) + "\n")
			default:
				diffContent.WriteString(l + "\n")
			}
		}

		diffBox := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(ColorSubtle).
			Padding(0, 1).
			Width(min(width-8, 90)).
			Render(strings.TrimRight(diffContent.String(), "\n"))

		sb.WriteString(diffBox + "\n\n")
	}

	// Action options
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("Confirm: ") +
		lipgloss.NewStyle().Foreground(ColorSecondary).Bold(true).Render("[y] Approve") + "   " +
		lipgloss.NewStyle().Foreground(ColorError).Bold(true).Render("[n] Deny") + "   " +
		lipgloss.NewStyle().Foreground(ColorWarning).Bold(true).Render("[yolo] Approve & YOLO Mode"))

	return ApprovalModalStyle.Width(min(width-4, 96)).Render(sb.String())
}
