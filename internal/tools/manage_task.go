package tools

import (
	"context"
	"fmt"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

func registerManageTask(r *Registry) {
	r.Register("manage_task", executeManageTask, ToolSchema{
		Group: ToolGroupWrite,
		Name:        "manage_task",
		Description: "Manage background tasks. " +
			"Actions: 'list' (list all running tasks), 'kill' (cancel a task), " +
			"'status' (check task status and log file location), 'send_input' (send input to a running task). " +
			"IMPORTANT: Do NOT poll or loop on 'status' to wait for completion. " +
			"The system will automatically notify you when the command finishes.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"list", "status", "kill", "send_input"},
					"description": "The action to perform",
				},
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "Task ID (required for status, kill, send_input)",
				},
				"input": map[string]interface{}{
					"type":        "string",
					"description": "Input to send to task stdin (for send_input action)",
				},
			},
			"required": []string{"action"},
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
		return fmt.Errorf("manage_task: unknown action %q (valid: list, status, kill, send_input)", mt.Action)
	}
}

func handleListTasks(tm *TaskManager, mt *pb.ActionManageTask) error {
	snapshots := tm.ListTasks()
	mt.Tasks = make([]*pb.TaskInfo, 0, len(snapshots))
	for _, s := range snapshots {
		mt.Tasks = append(mt.Tasks, snapshotToProto(s))
	}
	mt.Success = true
	return nil
}

func handleTaskStatus(tm *TaskManager, mt *pb.ActionManageTask) error {
	if mt.TaskId == "" {
		return fmt.Errorf("manage_task: task_id is required for status action")
	}

	snap, err := tm.GetTaskStatus(mt.TaskId)
	if err != nil {
		return fmt.Errorf("manage_task: %w", err)
	}

	mt.Tasks = []*pb.TaskInfo{snapshotToProto(snap)}
	mt.Success = true
	return nil
}

func handleKillTask(tm *TaskManager, mt *pb.ActionManageTask) error {
	if mt.TaskId == "" {
		return fmt.Errorf("manage_task: task_id is required for kill action")
	}

	if err := tm.KillTask(mt.TaskId); err != nil {
		return fmt.Errorf("manage_task: %w", err)
	}

	mt.Success = true
	return nil
}

func handleSendInput(tm *TaskManager, mt *pb.ActionManageTask) error {
	if mt.TaskId == "" {
		return fmt.Errorf("manage_task: task_id is required for send_input action")
	}
	if mt.Input == "" {
		return fmt.Errorf("manage_task: input is required for send_input action")
	}

	if err := tm.SendInput(mt.TaskId, mt.Input); err != nil {
		return fmt.Errorf("manage_task: %w", err)
	}

	mt.Success = true
	return nil
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
	}
	if !s.StartedAt.IsZero() {
		info.StartedAt = s.StartedAt.Format(time.RFC3339)
	}
	if !s.CompletedAt.IsZero() {
		info.CompletedAt = s.CompletedAt.Format(time.RFC3339)
	}
	return info
}
