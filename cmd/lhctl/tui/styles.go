package tui

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	// Base Colors
	ColorPrimary   = lipgloss.Color("#7D56F4") // Purple
	ColorSecondary = lipgloss.Color("#04B575") // Green
	ColorAccent    = lipgloss.Color("#EE6FF8") // Pink
	ColorWarning   = lipgloss.Color("#FFB86C") // Orange/Yellow
	ColorError     = lipgloss.Color("#FF5555") // Red
	ColorMuted     = lipgloss.Color("#6272A4") // Dim Blue/Gray
	ColorSubtle    = lipgloss.Color("#44475A") // Darker Gray
	ColorHighlight = lipgloss.Color("#8BE9FD") // Cyan
	ColorBg        = lipgloss.Color("#282A36") // Dark Background
	ColorText      = lipgloss.Color("#F8F8F2") // Light Text

	// Header / Title styles
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorHighlight).
			Background(ColorSubtle).
			Padding(0, 1)

	// Status Badges
	BadgeYolo = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(ColorError).
			Padding(0, 1).
			SetString("⚡ YOLO MODE")

	BadgeSubagent = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(ColorPrimary).
			Padding(0, 1)

	BadgeWaiting = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#000000")).
			Background(ColorWarning).
			Padding(0, 1).
			SetString("⚠️ WAITING APPROVAL")

	BadgeStreaming = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(ColorSecondary).
			Padding(0, 1).
			SetString("● STREAMING")

	BadgeRunning = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(ColorPrimary).
			Padding(0, 1).
			SetString("● RUNNING")

	BadgeIdle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Background(ColorSubtle).
			Padding(0, 1).
			SetString("● IDLE")

	// Message Styles
	UserMsgStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorHighlight)

	AssistantMsgStyle = lipgloss.NewStyle().
				Foreground(ColorText)

	ThinkingStyle = lipgloss.NewStyle().
			Italic(true).
			Foreground(ColorMuted).
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(ColorMuted).
			PaddingLeft(1)

	ToolCallHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorWarning)

	ToolResultStyle = lipgloss.NewStyle().
			Foreground(ColorText)

	SystemMsgStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	ErrorMsgStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorError)

	// Diff Styles
	DiffAddStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary)

	DiffRemoveStyle = lipgloss.NewStyle().
			Foreground(ColorError)

	DiffHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorHighlight)

	// Modal / Box Styles
	ModalBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorPrimary).
			Padding(1, 2).
			Background(ColorBg)

	ApprovalModalStyle = lipgloss.NewStyle().
				Border(lipgloss.DoubleBorder()).
				BorderForeground(ColorWarning).
				Padding(1, 2).
				Background(ColorBg)

	// Status bar & Help
	StatusBarStyle = lipgloss.NewStyle().
			Background(ColorSubtle).
			Foreground(ColorText).
			Padding(0, 1)

	HelpKeyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorHighlight)

	HelpDescStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	// Autocomplete dropdown
	AutocompleteBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorMuted).
				Background(ColorBg).
				Padding(0, 1)

	AutocompleteActiveItem = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(ColorPrimary)

	AutocompleteItem = lipgloss.NewStyle().
				Foreground(ColorText)
)
