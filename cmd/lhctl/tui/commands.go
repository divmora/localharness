package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Command represents a parsed slash command.
type Command struct {
	Name string
	Args []string
	Raw  string
}

// ParseCommand parses a user input string into a Command.
func ParseCommand(input string) (*Command, bool) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/") {
		return nil, false
	}

	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return nil, false
	}

	cmdName := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	return &Command{
		Name: cmdName,
		Args: parts[1:],
		Raw:  trimmed,
	}, true
}

// RenderHelpView renders the interactive help catalog.
func RenderHelpView(width int) string {
	return RenderHelpViewWithCustom(width, nil)
}

// RenderHelpViewWithCustom renders the help catalog including custom user commands.
func RenderHelpViewWithCustom(width int, customMgr *CustomCommandManager) string {
	commands := []struct {
		Cmd  string
		Desc string
	}{
		{"/help", "Show this help menu and shortcuts"},
		{"/new", "Start a fresh new conversation session"},
		{"/mode [name]", "Switch mode: default, accept-edits, plan"},

		{"/plan [goal]", "Research and create implementation_plan.md"},
		{"/teamwork [goal]", "Coordinate a team of parallel autonomous subagents"},
		{"/pause", "Pause active turn (Ctrl+C)"},
		{"/resume [msg]", "Resume execution with optional instructions"},
		{"/model [name]", "View or switch active LLM model"},
		{"/compact", "Trigger context window compaction"},
		{"/context", "Visualize 2D context usage, tokens & active items"},
		{"/btw <q>", "Ask a side question without interrupting current task"},
		{"/status", "Display daemon state, tokens & subagent metrics"},
		{"/subagents", "View subagent hierarchy & drill-down transcript"},
		{"/tasks", "View background tasks, running commands & timers"},
		{"/add-dir <path>", "Add directory to workspace (alias: /dir add)"},
		{"/remove-dir <path>", "Remove directory from workspace (alias: /rm-dir)"},
		{"/dirs", "List all attached workspaces (alias: /workspaces)"},
		{"/workspace list", "List all currently attached workspaces"},
		{"/workspace add <dir>", "Attach a directory with trust verification"},
		{"/workspace remove <dir>", "Detach a workspace directory"},
		{"/voice [on|off]", "Toggle voice dictation (or press F5 / Ctrl+R)"},
		{"/speak [auto|stop]", "Speak assistant response aloud or toggle auto-TTS"},
		{"/detach", "Detach TUI while background agents continue running"},
		{"/thinking [on|off]", "Toggle reasoning and chain-of-thought (Ctrl+O)"},
		{"/yolo", "Toggle YOLO Mode (bypass all approval queues)"},
		{"/version", "Display client & daemon version and runtime"},
		{"/clear", "Clear chat viewport history"},
		{"/exit, /quit", "Exit the TUI session"},
	}

	shortcuts := []struct {
		Key  string
		Desc string
	}{
		{"F5 / Ctrl+R", "Toggle voice dictation (microphone to prompt)"},
		{"Shift+Tab", "Cycle modes (default → accept-edits → plan)"},
		{"Ctrl+O", "Toggle reasoning and chain-of-thought visibility"},
		{"Ctrl+Y", "Copy last assistant response to clipboard"},
		{"Ctrl+V", "Paste text from clipboard into prompt"},
		{"Alt+Enter", "Insert newline in prompt (multiline mode)"},
		{"@<file>", "Autocompletion dropdown matching files in workspace"},
		{"Enter", "Send message / Confirm approval"},
		{"Ctrl+C", "Interrupt current agent turn / Cancel"},
		{"Ctrl+D", "Detach client from daemon"},
		{"PgUp / PgDn", "Scroll conversation viewport"},
		{"Esc / q", "Close modal / return to chat view"},
	}

	var sb strings.Builder
	sb.WriteString(TitleStyle.Render("⚡ LOCALHARNESS SLASH COMMANDS") + "\n\n")

	for _, c := range commands {
		sb.WriteString(fmt.Sprintf("  %s %s\n",
			HelpKeyStyle.Width(24).Render(c.Cmd),
			HelpDescStyle.Render(c.Desc),
		))
	}

	// Custom Commands section if any are discovered
	if customMgr != nil {
		customList := customMgr.List()
		if len(customList) > 0 {
			sb.WriteString("\n" + TitleStyle.Render("🛠️ CUSTOM COMMANDS (.agents/commands/ & ~/.divmora/commands/)") + "\n\n")
			for _, cc := range customList {
				usage := "/" + cc.Name
				if cc.ArgumentPlaceholder != "" {
					usage += " " + cc.ArgumentPlaceholder
				}
				tag := "[custom]"
				if cc.IsWorkspace {
					tag = "[workspace]"
				}
				sb.WriteString(fmt.Sprintf("  %s %s %s\n",
					HelpKeyStyle.Width(24).Render(usage),
					lipgloss.NewStyle().Foreground(ColorSecondary).Render(tag),
					HelpDescStyle.Render(cc.Description),
				))
			}
		}
	}

	sb.WriteString("\n" + TitleStyle.Render("⌨️ KEYBOARD SHORTCUTS") + "\n\n")
	for _, s := range shortcuts {
		sb.WriteString(fmt.Sprintf("  %s %s\n",
			HelpKeyStyle.Width(24).Render(s.Key),
			HelpDescStyle.Render(s.Desc),
		))
	}

	sb.WriteString("\n" + lipgloss.NewStyle().Faint(true).Render("Press Esc or type any message to return to chat."))

	return ModalBoxStyle.Width(min(width-4, 80)).Render(sb.String())
}
