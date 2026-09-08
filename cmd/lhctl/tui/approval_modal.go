package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/divmora/localharness/internal/util"
)

// ActiveApproval holds state for a pending tool confirmation prompt.
type ActiveApproval struct {
	RequestID    string
	ToolName     string
	Description  string
	DiffPreview  string
	ArgsJSON     string
	SubCommands  []string
	SelectedSubs []bool
}

// InitSubcommands initializes the subcommands slice and selection state.
func (app *ActiveApproval) InitSubcommands() {
	if app == nil || app.ToolName != "run_command" {
		return
	}
	targetCmd := app.DisplayTarget()
	if targetCmd != "" && targetCmd != app.ToolName {
		subCmds, _ := util.SplitShellCommands(targetCmd)
		if len(subCmds) > 1 {
			app.SubCommands = subCmds
			app.SelectedSubs = make([]bool, len(subCmds))
			for i := range app.SelectedSubs {
				app.SelectedSubs[i] = true
			}
		}
	}
}

// ToggleSubcommand toggles the selection state of a sub-command by 0-based index.
func (app *ActiveApproval) ToggleSubcommand(idx int) {
	if app == nil || idx < 0 || idx >= len(app.SelectedSubs) {
		return
	}
	app.SelectedSubs[idx] = !app.SelectedSubs[idx]
}

// ApprovedSubcommands returns the list of checked sub-commands.
func (app *ActiveApproval) ApprovedSubcommands() []string {
	if app == nil || len(app.SubCommands) == 0 {
		return nil
	}
	var approved []string
	for i, sub := range app.SubCommands {
		if i < len(app.SelectedSubs) && app.SelectedSubs[i] {
			approved = append(approved, sub)
		}
	}
	return approved
}

// DeniedSubcommands returns the list of unchecked sub-commands.
func (app *ActiveApproval) DeniedSubcommands() []string {
	if app == nil || len(app.SubCommands) == 0 {
		return nil
	}
	var denied []string
	for i, sub := range app.SubCommands {
		if i < len(app.SelectedSubs) && !app.SelectedSubs[i] {
			denied = append(denied, sub)
		}
	}
	return denied
}

// DisplayTarget returns a human-readable target string for status/history messages.
func (app *ActiveApproval) DisplayTarget() string {
	if app == nil {
		return ""
	}
	if app.ToolName == "run_command" && app.ArgsJSON != "" {
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(app.ArgsJSON), &args); err == nil {
			if cmd, ok := args["command"].(string); ok && cmd != "" {
				return cmd
			}
			if cmd, ok := args["CommandLine"].(string); ok && cmd != "" {
				return cmd
			}
		}
	}
	if app.Description != "" {
		return app.Description
	}
	return app.ToolName
}

// RenderApprovalInline renders an inline confirmation prompt with unified diff and scoped approval choices.
func RenderApprovalInline(app *ActiveApproval, width int) string {
	if app == nil {
		return ""
	}

	var sb strings.Builder
	header := BadgeWaiting.Render() + " " +
		lipgloss.NewStyle().Bold(true).Foreground(ColorWarning).Render("Tool Approval Required: ") +
		lipgloss.NewStyle().Bold(true).Foreground(ColorHighlight).Render(app.ToolName)
	sb.WriteString(header + "\n")

	if app.Description != "" {
		sb.WriteString(lipgloss.NewStyle().Foreground(ColorText).Render(app.Description) + "\n")
	}

	// For shell commands: break down compound commands (&&, ||, ;, |, &) into sub-commands with toggles
	if app.ToolName == "run_command" && len(app.SubCommands) > 1 {
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorHighlight).Render(fmt.Sprintf("Chained Sub-commands (press [1-%d] to toggle approval):", len(app.SubCommands))) + "\n")
		for idx, sub := range app.SubCommands {
			selected := true
			if idx < len(app.SelectedSubs) {
				selected = app.SelectedSubs[idx]
			}
			var checkMark string
			if selected {
				checkMark = lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary).Render("[✓] ")
			} else {
				checkMark = lipgloss.NewStyle().Bold(true).Foreground(ColorError).Render("[✗] ")
			}
			numStr := lipgloss.NewStyle().Bold(true).Foreground(ColorHighlight).Render(fmt.Sprintf("%d. ", idx+1))
			cmdStyle := lipgloss.NewStyle()
			if !selected {
				cmdStyle = cmdStyle.Strikethrough(true).Foreground(ColorSubtle)
			} else {
				cmdStyle = cmdStyle.Foreground(ColorText)
			}
			sb.WriteString("  " + checkMark + numStr + cmdStyle.Render(sub) + "\n")
		}
		sb.WriteString("\n")
	}

	if app.DiffPreview != "" {
		lines := strings.Split(app.DiffPreview, "\n")
		maxLines := 14
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
			Width(min(width-4, 90)).
			Render(strings.TrimRight(diffContent.String(), "\n"))

		sb.WriteString(diffBox + "\n")
	}

	// Action options with scoped grants
	sb.WriteString(
		lipgloss.NewStyle().Bold(true).Render("Confirm: ") +
			lipgloss.NewStyle().Foreground(ColorSecondary).Bold(true).Render("[y] Allow selected") + "  " +
			lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Render("[c] Allow in conversation") + "  " +
			lipgloss.NewStyle().Foreground(ColorHighlight).Bold(true).Render("[g] Always allow globally") + "  " +
			lipgloss.NewStyle().Foreground(ColorError).Bold(true).Render("[n] Deny"),
	)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorWarning).
		Padding(0, 1).
		Width(min(width-2, 96)).
		Render(sb.String())
}

// RenderApprovalModal renders an interactive confirmation dialog with unified diff highlighting.
func RenderApprovalModal(app *ActiveApproval, width int) string {
	return RenderApprovalInline(app, width)
}
