package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

func registerManageTask(r *Registry) {
	r.Register("manage_task", executeManageTask, ToolSchema{
		Group: ToolGroupWrite,
		Name:  "manage_task",
		Description: "Manage background tasks. Use this tool to list running tasks or interact with tasks that were sent to the background.\n\n" +
			"Actions:\n" +
			"- 'list': List all currently running background tasks\n" +
			"- 'kill': Cancel the task's execution\n" +
			"- 'status': Check the task's current status and log file location\n" +
			"- 'send_input': Send input to a running task\n\n" +
			"When mentioning tasks to the user, avoid using full task IDs and start timestamps; keep them human-readable.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"Action": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"list", "kill", "status", "send_input"},
					"description": "The action to perform: 'list' (list all running tasks), 'kill' (cancel the task), 'status' (check the task status and log URI), 'send_input' (send input to a running task).",
				},
				"Input": map[string]interface{}{
					"type":        "string",
					"description": "The input to send to the task. Required when Action is 'send_input'.",
				},
				"TaskId": map[string]interface{}{
					"type":        "string",
					"description": "The task ID to manage. Required when Action is 'kill', 'status', or 'send_input'.",
				},
				"ToolAction": map[string]interface{}{
					"type":        "string",
					"description": "Brief 2-5 word phrase in -ing form describing the specific action. Capitalize like a sentence. Some examples: 'Analyzing directory', 'Searching the web', 'Checking git status', 'Running tests', 'Searching code'.",
				},
				"ToolSummary": map[string]interface{}{
					"type":        "string",
					"description": "Brief 2-5 word noun phrase describing the specific task. Capitalize like a sentence. Some examples: 'Directory analysis', 'Web search', 'Git status check', 'Test execution', 'Code search'.",
				},
			},
			"required": []string{"Action", "ToolSummary", "ToolAction"},
		},
	})
}

func executeManageTask(ctx context.Context, step *pb.StepUpdate, r *Registry) error {
	mt := step.GetManageTask()
	if mt == nil {
		return fmt.Errorf("manage_task: missing action")
	}

	if r.taskMgr == nil {
		return fmt.Errorf("manage_task: task manager not initialized")
	}

	switch mt.Action {
	case "list":
		return handleListTasks(r.taskMgr, mt)
	case "status":
		return handleTaskStatus(r.taskMgr, mt)
	case "kill":
		return handleKillTask(r.taskMgr, mt)
	case "send_input":
		return handleSendInput(r.taskMgr, mt)
	default:
		return fmt.Errorf("manage_task: unknown action %q (valid: list, kill, status, send_input)", mt.Action)
	}
}

func handleListTasks(tm *TaskManager, mt *pb.ActionManageTask) error {
	snapshots := tm.ListTasks()
	mt.Tasks = make([]*pb.TaskInfo, 0, len(snapshots))
	for _, s := range snapshots {
		mt.Tasks = append(mt.Tasks, snapshotToProto(s))
	}
	mt.Success = true
	mt.FormattedOutput = formatListTasks(mt.Tasks)
	return nil
}

func handleTaskStatus(tm *TaskManager, mt *pb.ActionManageTask) error {
	if mt.TaskId == "" {
		return fmt.Errorf("manage_task: TaskId is required for status action")
	}

	snap, err := tm.GetTaskStatus(mt.TaskId)
	if err != nil {
		return fmt.Errorf("manage_task: %w", err)
	}

	taskProto := snapshotToProto(snap)
	mt.Tasks = []*pb.TaskInfo{taskProto}
	mt.Success = true
	mt.FormattedOutput = formatTaskStatus(taskProto)
	return nil
}

func handleKillTask(tm *TaskManager, mt *pb.ActionManageTask) error {
	if mt.TaskId == "" {
		return fmt.Errorf("manage_task: TaskId is required for kill action")
	}

	if err := tm.KillTask(mt.TaskId); err != nil {
		return fmt.Errorf("manage_task: %w", err)
	}

	mt.Success = true
	mt.FormattedOutput = fmt.Sprintf("Task %q cancelled.", mt.TaskId)
	return nil
}

func handleSendInput(tm *TaskManager, mt *pb.ActionManageTask) error {
	if mt.TaskId == "" {
		return fmt.Errorf("manage_task: TaskId is required for send_input action")
	}
	if mt.Input == "" {
		return fmt.Errorf("manage_task: Input is required for send_input action")
	}

	if err := tm.SendInput(mt.TaskId, mt.Input); err != nil {
		return fmt.Errorf("manage_task: %w", err)
	}

	mt.Success = true
	mt.FormattedOutput = fmt.Sprintf("Input sent to task %q.", mt.TaskId)
	return nil
}

func formatListTasks(tasks []*pb.TaskInfo) string {
	if len(tasks) == 0 {
		return "No background tasks currently running."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Currently running background tasks (%d):\n", len(tasks)))
	for _, t := range tasks {
		sb.WriteString(fmt.Sprintf("- Task ID: %s\n  Command: %s\n  Status: %s\n", t.TaskId, t.Command, t.Status))
		if t.StartedAt != "" {
			sb.WriteString(fmt.Sprintf("  Started At: %s\n", t.StartedAt))
		}
		if t.LogUri != "" {
			sb.WriteString(fmt.Sprintf("  Log URI: %s\n", t.LogUri))
		} else if t.LogPath != "" {
			sb.WriteString(fmt.Sprintf("  Log Path: %s\n", t.LogPath))
		}
	}
	return sb.String()
}

func formatTaskStatus(t *pb.TaskInfo) string {
	var sb strings.Builder
	if t.StartedAt != "" {
		sb.WriteString(fmt.Sprintf("Created At: %s\n", t.StartedAt))
	}
	if t.CompletedAt != "" {
		sb.WriteString(fmt.Sprintf("Completed At: %s\n", t.CompletedAt))
	}
	sb.WriteString(fmt.Sprintf("Task: %s\n", t.TaskId))
	sb.WriteString(fmt.Sprintf("Command: %s\n", t.Command))
	sb.WriteString(fmt.Sprintf("Status: %s\n", t.Status))
	if t.ExitCode != 0 || t.Status == "exit" || t.Status == "done" || t.Status == "completed" {
		sb.WriteString(fmt.Sprintf("Exit Code: %d\n", t.ExitCode))
	}
	if t.LogUri != "" {
		sb.WriteString(fmt.Sprintf("Log URI: %s\n", t.LogUri))
	} else if t.LogPath != "" {
		sb.WriteString(fmt.Sprintf("Log: %s\n", t.LogPath))
	}
	if t.RecentOutput != "" {
		sb.WriteString("Log output:\n")
		sb.WriteString(t.RecentOutput)
		if !strings.HasSuffix(t.RecentOutput, "\n") {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func snapshotToProto(s TaskSnapshot) *pb.TaskInfo {
	info := &pb.TaskInfo{
		TaskId:       s.ID,
		Command:      s.Command,
		Cwd:          s.Cwd,
		Status:       string(s.Status),
		ExitCode:     int32(s.ExitCode),
		RecentOutput: s.RecentOutput,
		TerminalId:   s.TerminalID,
		LogPath:      s.LogPath,
		LogUri:       s.LogURI,
	}
	if !s.StartedAt.IsZero() {
		info.StartedAt = s.StartedAt.Format(time.RFC3339)
	}
	if !s.CompletedAt.IsZero() {
		info.CompletedAt = s.CompletedAt.Format(time.RFC3339)
	}
	return info
}
