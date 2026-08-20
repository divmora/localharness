package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

// TaskItemState tracks an individual background task/command or timer.
type TaskItemState struct {
	TaskID       string
	Command      string
	Cwd          string
	Status       string // RUNNING, COMPLETED, FAILED, KILLED
	ExitCode     int
	StartedAt    time.Time
	CompletedAt  time.Time
	RecentOutput string
	TerminalID   string
	IsSchedule   bool
}

// TasksViewManager manages background tasks state and rendering.
type TasksViewManager struct {
	tasks         map[string]*TaskItemState
	order         []string // ordered list of task IDs
	selectedIndex int
}

// NewTasksViewManager creates a tasks view manager.
func NewTasksViewManager() *TasksViewManager {
	return &TasksViewManager{
		tasks: make(map[string]*TaskItemState),
	}
}

// AddOrUpdate registers or updates a background task.
func (m *TasksViewManager) AddOrUpdate(task *TaskItemState) {
	if task == nil || task.TaskID == "" {
		return
	}
	if _, exists := m.tasks[task.TaskID]; !exists {
		m.order = append(m.order, task.TaskID)
	}
	m.tasks[task.TaskID] = task
}

// UpdateFromProto updates tasks state from a protobuf TaskInfo list.
func (m *TasksViewManager) UpdateFromProto(protoTasks []*pb.TaskInfo) {
	for _, pt := range protoTasks {
		if pt == nil || pt.TaskId == "" {
			continue
		}
		started, _ := time.Parse(time.RFC3339, pt.StartedAt)
		completed, _ := time.Parse(time.RFC3339, pt.CompletedAt)
		if started.IsZero() {
			started = time.Now()
		}

		m.AddOrUpdate(&TaskItemState{
			TaskID:       pt.TaskId,
			Command:      pt.Command,
			Cwd:          pt.Cwd,
			Status:       strings.ToUpper(pt.Status),
			ExitCode:     int(pt.ExitCode),
			StartedAt:    started,
			CompletedAt:  completed,
			RecentOutput: pt.RecentOutput,
			TerminalID:   pt.TerminalId,
		})
	}
}

// RunningCount returns the number of active running background tasks.
func (m *TasksViewManager) RunningCount() int {
	count := 0
	for _, t := range m.tasks {
		if t.Status == "RUNNING" {
			count++
		}
	}
	return count
}

// TotalCount returns the total number of tracked tasks.
func (m *TasksViewManager) TotalCount() int {
	return len(m.tasks)
}

// SelectedTask returns the currently highlighted task.
func (m *TasksViewManager) SelectedTask() *TaskItemState {
	if m.selectedIndex >= 0 && m.selectedIndex < len(m.order) {
		return m.tasks[m.order[m.selectedIndex]]
	}
	return nil
}

// NavigateUp moves the task cursor up.
func (m *TasksViewManager) NavigateUp() {
	if m.selectedIndex > 0 {
		m.selectedIndex--
	}
}

// NavigateDown moves the task cursor down.
func (m *TasksViewManager) NavigateDown() {
	if m.selectedIndex < len(m.order)-1 {
		m.selectedIndex++
	}
}

// Render formats and renders the background tasks dashboard modal.
func (m *TasksViewManager) Render(width, height int) string {
	var sb strings.Builder

	title := lipgloss.NewStyle().Bold(true).Foreground(ColorHighlight).Render("⚙️ BACKGROUND TASKS & COMMANDS")
	sb.WriteString(title + "\n\n")

	if len(m.order) == 0 {
		sb.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("  No background tasks or commands running.\n  (Commands run via run_command with wait_ms_before_async or schedules appear here)\n\n"))
		sb.WriteString(lipgloss.NewStyle().Faint(true).Render("Press Esc or q to close."))
		return ModalBoxStyle.Width(min(width-4, 86)).Render(sb.String())
	}

	// Table header
	header := fmt.Sprintf("  %-10s %-12s %-10s %s", "TASK ID", "STATUS", "DURATION", "COMMAND / DESCRIPTION")
	sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary).Render(header) + "\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(ColorSubtle).Render("  " + strings.Repeat("─", min(width-10, 80)) + "\n"))

	for i, taskID := range m.order {
		t := m.tasks[taskID]
		isSel := i == m.selectedIndex

		cursor := "  "
		if isSel {
			cursor = lipgloss.NewStyle().Bold(true).Foreground(ColorHighlight).Render("❯ ")
		}

		// Status Badge
		var statusStr string
		switch t.Status {
		case "RUNNING":
			statusStr = lipgloss.NewStyle().Foreground(ColorSecondary).Bold(true).Render("● RUNNING")
		case "COMPLETED":
			statusStr = lipgloss.NewStyle().Foreground(ColorMuted).Render("✓ DONE")
		case "FAILED":
			statusStr = lipgloss.NewStyle().Foreground(ColorError).Bold(true).Render("✗ FAILED")
		case "KILLED":
			statusStr = lipgloss.NewStyle().Foreground(ColorWarning).Render("⊘ KILLED")
		default:
			statusStr = t.Status
		}

		// Duration
		dur := "..."
		if !t.StartedAt.IsZero() {
			if !t.CompletedAt.IsZero() {
				dur = t.CompletedAt.Sub(t.StartedAt).Round(time.Second).String()
			} else {
				dur = time.Since(t.StartedAt).Round(time.Second).String()
			}
		}

		cmdSummary := t.Command
		if len(cmdSummary) > 42 {
			cmdSummary = cmdSummary[:39] + "..."
		}

		line := fmt.Sprintf("%s%-10s %-20s %-10s %s\n",
			cursor,
			lipgloss.NewStyle().Bold(isSel).Render(t.TaskID),
			statusStr,
			dur,
			cmdSummary,
		)
		sb.WriteString(line)
	}

	// Show selected task details and output preview
	if sel := m.SelectedTask(); sel != nil {
		sb.WriteString("\n" + lipgloss.NewStyle().Foreground(ColorSubtle).Render("  " + strings.Repeat("─", min(width-10, 80)) + "\n"))
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("  Details for %s:\n", sel.TaskID)))
		sb.WriteString(fmt.Sprintf("  Command: %s\n", lipgloss.NewStyle().Foreground(ColorText).Render(sel.Command)))
		if sel.Cwd != "" {
			sb.WriteString(fmt.Sprintf("  Working Dir: %s\n", lipgloss.NewStyle().Foreground(ColorMuted).Render(sel.Cwd)))
		}
		if sel.RecentOutput != "" {
			sb.WriteString("\n  Recent Output:\n")
			lines := strings.Split(strings.TrimSpace(sel.RecentOutput), "\n")
			start := max(0, len(lines)-6)
			for _, l := range lines[start:] {
				sb.WriteString(fmt.Sprintf("    %s\n", lipgloss.NewStyle().Foreground(ColorMuted).Render(l)))
			}
		}
	}

	sb.WriteString("\n" + lipgloss.NewStyle().Faint(true).Render("Keys: [↑/↓] Navigate  [k] Kill Task  [Esc/q] Close"))

	return ModalBoxStyle.Width(min(width-4, 88)).Render(sb.String())
}
