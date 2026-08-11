package tools

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/workspace"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// ─── Task Manager Tests ─────────────────────────────────────────────────

func TestTaskManagerStartBackground(t *testing.T) {
	tm := NewTaskManager(testLogger(), 5)
	defer tm.Shutdown()

	ctx := context.Background()
	taskID, _, err := tm.StartBackground(ctx, "echo hello && sleep 0.1", "", nil, 0)
	if err != nil {
		t.Fatalf("StartBackground failed: %v", err)
	}
	if taskID == "" {
		t.Error("expected non-empty task ID")
	}
	if !hasPrefix(taskID, "task-") {
		t.Errorf("task ID should start with 'task-', got %q", taskID)
	}

	// Wait for the task to complete
	time.Sleep(500 * time.Millisecond)

	snap, err := tm.GetTaskStatus(taskID)
	if err != nil {
		t.Fatalf("GetTaskStatus failed: %v", err)
	}
	if snap.Status != TaskCompleted {
		t.Errorf("expected status 'completed', got %q", snap.Status)
	}
	if snap.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", snap.ExitCode)
	}
	if snap.RecentOutput == "" {
		t.Error("expected non-empty recent output")
	}
}

func TestTaskManagerStartBackgroundWithWait(t *testing.T) {
	tm := NewTaskManager(testLogger(), 5)
	defer tm.Shutdown()

	ctx := context.Background()
	// Start a command that outputs quickly then sleeps
	taskID, output, err := tm.StartBackground(ctx, "echo immediate-output && sleep 5", "", nil, 500)
	if err != nil {
		t.Fatalf("StartBackground failed: %v", err)
	}
	if taskID == "" {
		t.Error("expected non-empty task ID")
	}
	if output == "" {
		t.Error("expected initial output from wait period")
	}

	// Task should still be running
	snap, _ := tm.GetTaskStatus(taskID)
	if snap.Status != TaskRunning {
		t.Errorf("expected running, got %q", snap.Status)
	}
}

func TestTaskManagerListTasks(t *testing.T) {
	tm := NewTaskManager(testLogger(), 5)
	defer tm.Shutdown()

	ctx := context.Background()

	// Start multiple tasks
	id1, _, _ := tm.StartBackground(ctx, "sleep 10", "", nil, 0)
	id2, _, _ := tm.StartBackground(ctx, "sleep 10", "", nil, 0)

	tasks := tm.ListTasks()
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}

	ids := map[string]bool{}
	for _, task := range tasks {
		ids[task.ID] = true
	}
	if !ids[id1] || !ids[id2] {
		t.Error("listed tasks should include both task IDs")
	}
}

func TestTaskManagerKillTask(t *testing.T) {
	tm := NewTaskManager(testLogger(), 5)
	defer tm.Shutdown()

	ctx := context.Background()
	taskID, _, _ := tm.StartBackground(ctx, "sleep 30", "", nil, 0)

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	err := tm.KillTask(taskID)
	if err != nil {
		t.Fatalf("KillTask failed: %v", err)
	}

	snap, _ := tm.GetTaskStatus(taskID)
	if snap.Status != TaskKilled {
		t.Errorf("expected status 'killed', got %q", snap.Status)
	}
}

func TestTaskManagerKillUnknownTask(t *testing.T) {
	tm := NewTaskManager(testLogger(), 5)
	defer tm.Shutdown()

	err := tm.KillTask("nonexistent")
	if err == nil {
		t.Error("KillTask should error for unknown task")
	}
}

func TestTaskManagerSendInput(t *testing.T) {
	tm := NewTaskManager(testLogger(), 5)
	defer tm.Shutdown()

	ctx := context.Background()
	// Start cat which reads from stdin
	taskID, _, _ := tm.StartBackground(ctx, "cat", "", nil, 0)

	time.Sleep(100 * time.Millisecond)

	err := tm.SendInput(taskID, "hello from stdin\n")
	if err != nil {
		t.Fatalf("SendInput failed: %v", err)
	}

	// Give time for output
	time.Sleep(200 * time.Millisecond)

	snap, _ := tm.GetTaskStatus(taskID)
	if snap.RecentOutput == "" {
		t.Error("expected output from cat after sending input")
	}
}

func TestTaskManagerSendInputUnknown(t *testing.T) {
	tm := NewTaskManager(testLogger(), 5)
	defer tm.Shutdown()

	err := tm.SendInput("nonexistent", "data")
	if err == nil {
		t.Error("SendInput should error for unknown task")
	}
}

func TestTaskManagerMaxTasks(t *testing.T) {
	tm := NewTaskManager(testLogger(), 2)
	defer tm.Shutdown()

	ctx := context.Background()
	tm.StartBackground(ctx, "sleep 30", "", nil, 0)
	tm.StartBackground(ctx, "sleep 30", "", nil, 0)

	_, _, err := tm.StartBackground(ctx, "sleep 30", "", nil, 0)
	if err == nil {
		t.Error("expected error when max tasks exceeded")
	}
}

func TestTaskManagerShutdown(t *testing.T) {
	tm := NewTaskManager(testLogger(), 5)

	ctx := context.Background()
	tm.StartBackground(ctx, "sleep 30", "", nil, 0)
	tm.StartBackground(ctx, "sleep 30", "", nil, 0)

	if tm.RunningTaskCount() != 2 {
		t.Errorf("expected 2 running tasks, got %d", tm.RunningTaskCount())
	}

	tm.Shutdown()

	if tm.RunningTaskCount() != 0 {
		t.Errorf("expected 0 running tasks after shutdown, got %d", tm.RunningTaskCount())
	}
}

func TestTaskManagerNonZeroExit(t *testing.T) {
	tm := NewTaskManager(testLogger(), 5)
	defer tm.Shutdown()

	ctx := context.Background()
	taskID, _, _ := tm.StartBackground(ctx, "exit 42", "", nil, 0)

	time.Sleep(500 * time.Millisecond)

	snap, _ := tm.GetTaskStatus(taskID)
	if snap.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", snap.ExitCode)
	}
	if snap.Status != TaskFailed {
		t.Errorf("expected status 'failed', got %q", snap.Status)
	}
}

// ─── Persistent Terminal Tests ──────────────────────────────────────────

func TestPersistentTerminalBasic(t *testing.T) {
	tm := NewTaskManager(testLogger(), 5)
	defer tm.Shutdown()

	ctx := context.Background()
	termID, stdout, exitCode, err := tm.RunInTerminal(ctx, "echo hello-from-terminal", "", "", nil, 5000)
	if err != nil {
		t.Fatalf("RunInTerminal failed: %v", err)
	}
	if termID == "" {
		t.Error("expected non-empty terminal ID")
	}
	if !hasPrefix(termID, "term-") {
		t.Errorf("terminal ID should start with 'term-', got %q", termID)
	}
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
	if stdout == "" {
		t.Error("expected non-empty stdout")
	}
}

func TestPersistentTerminalReuse(t *testing.T) {
	tm := NewTaskManager(testLogger(), 5)
	defer tm.Shutdown()

	ctx := context.Background()

	// Create terminal and set an env var
	termID, _, _, err := tm.RunInTerminal(ctx, "export MY_VAR=test_value_123", "", "", nil, 5000)
	if err != nil {
		t.Fatalf("first command failed: %v", err)
	}

	// Reuse the terminal and read the env var
	termID2, stdout, exitCode, err := tm.RunInTerminal(ctx, "echo $MY_VAR", "", termID, nil, 5000)
	if err != nil {
		t.Fatalf("second command failed: %v", err)
	}
	if termID2 != termID {
		t.Errorf("expected same terminal ID %q, got %q", termID, termID2)
	}
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
	if stdout == "" {
		t.Error("expected non-empty stdout from echo $MY_VAR")
	}
	// The output should contain the variable value
	if !contains(stdout, "test_value_123") {
		t.Errorf("expected stdout to contain 'test_value_123', got %q", stdout)
	}
}

func TestPersistentTerminalUnknown(t *testing.T) {
	tm := NewTaskManager(testLogger(), 5)
	defer tm.Shutdown()

	ctx := context.Background()
	_, _, _, err := tm.RunInTerminal(ctx, "echo test", "", "nonexistent", nil, 5000)
	if err == nil {
		t.Error("expected error for unknown terminal")
	}
}

// ─── Run Command Background Mode Tests ─────────────────────────────────

func TestRunCommandBackground(t *testing.T) {
	cfg := &pb.BuiltinToolsConfig{RunCommand: true}
	reg, wsDir := testRegistryWithConfig(t, cfg)
	ctx := context.Background()

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_RunCommand{
			RunCommand: &pb.ActionRunCommand{
				Command:    "sleep 10",
				Cwd:        wsDir,
				Background: true,
			},
		},
	}

	err := reg.Execute(ctx, "run_command", step)
	if err != nil {
		t.Fatalf("run_command background failed: %v", err)
	}

	rc := step.GetRunCommand()
	if rc.TaskId == "" {
		t.Error("expected non-empty task_id for background task")
	}

	// Clean up
	reg.Shutdown()
}

func TestRunCommandBackgroundWithWait(t *testing.T) {
	cfg := &pb.BuiltinToolsConfig{RunCommand: true}
	reg, wsDir := testRegistryWithConfig(t, cfg)
	ctx := context.Background()

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_RunCommand{
			RunCommand: &pb.ActionRunCommand{
				Command:            "echo quick-output && sleep 10",
				Cwd:                wsDir,
				Background:         true,
				WaitMsBeforeAsync:  500,
			},
		},
	}

	err := reg.Execute(ctx, "run_command", step)
	if err != nil {
		t.Fatalf("run_command background+wait failed: %v", err)
	}

	rc := step.GetRunCommand()
	if rc.TaskId == "" {
		t.Error("expected task_id since command is still running")
	}
	if rc.Stdout == "" {
		t.Error("expected initial stdout captured during wait period")
	}

	reg.Shutdown()
}

func TestRunCommandPersistent(t *testing.T) {
	cfg := &pb.BuiltinToolsConfig{RunCommand: true}
	reg, wsDir := testRegistryWithConfig(t, cfg)
	ctx := context.Background()

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_RunCommand{
			RunCommand: &pb.ActionRunCommand{
				Command:    "echo persistent-test",
				Cwd:        wsDir,
				Persistent: true,
			},
		},
	}

	err := reg.Execute(ctx, "run_command", step)
	if err != nil {
		t.Fatalf("run_command persistent failed: %v", err)
	}

	rc := step.GetRunCommand()
	if rc.AssignedTerminalId == "" {
		t.Error("expected non-empty assigned_terminal_id")
	}
	if rc.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", rc.ExitCode)
	}

	reg.Shutdown()
}

func TestRunCommandPersistentReuse(t *testing.T) {
	cfg := &pb.BuiltinToolsConfig{RunCommand: true}
	reg, wsDir := testRegistryWithConfig(t, cfg)
	ctx := context.Background()

	// First command: create a persistent terminal and set env var
	step1 := &pb.StepUpdate{
		Action: &pb.StepUpdate_RunCommand{
			RunCommand: &pb.ActionRunCommand{
				Command:    "export REUSE_TEST=yes",
				Cwd:        wsDir,
				Persistent: true,
			},
		},
	}

	if err := reg.Execute(ctx, "run_command", step1); err != nil {
		t.Fatalf("first persistent command failed: %v", err)
	}
	termID := step1.GetRunCommand().AssignedTerminalId

	// Second command: reuse the terminal
	step2 := &pb.StepUpdate{
		Action: &pb.StepUpdate_RunCommand{
			RunCommand: &pb.ActionRunCommand{
				Command:    "echo $REUSE_TEST",
				Cwd:        wsDir,
				Persistent: true,
				TerminalId: termID,
			},
		},
	}

	if err := reg.Execute(ctx, "run_command", step2); err != nil {
		t.Fatalf("second persistent command failed: %v", err)
	}

	rc2 := step2.GetRunCommand()
	if rc2.AssignedTerminalId != termID {
		t.Errorf("expected same terminal %q, got %q", termID, rc2.AssignedTerminalId)
	}
	if !contains(rc2.Stdout, "yes") {
		t.Errorf("expected stdout to contain 'yes', got %q", rc2.Stdout)
	}

	reg.Shutdown()
}

// ─── Manage Task Tests ──────────────────────────────────────────────────

func testRegistryWithTasks(t *testing.T) (*Registry, string) {
	t.Helper()
	wsDir := t.TempDir()

	wsMgr, err := workspace.NewManager([]string{wsDir})
	if err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	reg := NewRegistry(wsMgr, logger)
	RegisterBuiltinTools(reg, &pb.BuiltinToolsConfig{
		RunCommand: true,
		ManageTask: true,
	})

	return reg, wsDir
}

func TestManageTaskList(t *testing.T) {
	reg, wsDir := testRegistryWithTasks(t)
	defer reg.Shutdown()
	ctx := context.Background()

	// Start a background task
	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_RunCommand{
			RunCommand: &pb.ActionRunCommand{
				Command:    "sleep 10",
				Cwd:        wsDir,
				Background: true,
			},
		},
	}
	reg.Execute(ctx, "run_command", step)

	// List tasks
	listStep := &pb.StepUpdate{
		Action: &pb.StepUpdate_ManageTask{
			ManageTask: &pb.ActionManageTask{
				Action: "list",
			},
		},
	}

	err := reg.Execute(ctx, "manage_task", listStep)
	if err != nil {
		t.Fatalf("manage_task list failed: %v", err)
	}

	mt := listStep.GetManageTask()
	if !mt.Success {
		t.Error("expected success=true")
	}
	if len(mt.Tasks) < 1 {
		t.Errorf("expected at least 1 task, got %d", len(mt.Tasks))
	}
}

func TestManageTaskStatus(t *testing.T) {
	reg, wsDir := testRegistryWithTasks(t)
	defer reg.Shutdown()
	ctx := context.Background()

	// Start a background task
	runStep := &pb.StepUpdate{
		Action: &pb.StepUpdate_RunCommand{
			RunCommand: &pb.ActionRunCommand{
				Command:    "echo status-check && sleep 10",
				Cwd:        wsDir,
				Background: true,
				WaitMsBeforeAsync: 200,
			},
		},
	}
	reg.Execute(ctx, "run_command", runStep)
	taskID := runStep.GetRunCommand().TaskId

	// Get status
	statusStep := &pb.StepUpdate{
		Action: &pb.StepUpdate_ManageTask{
			ManageTask: &pb.ActionManageTask{
				Action: "status",
				TaskId: taskID,
			},
		},
	}

	err := reg.Execute(ctx, "manage_task", statusStep)
	if err != nil {
		t.Fatalf("manage_task status failed: %v", err)
	}

	mt := statusStep.GetManageTask()
	if !mt.Success {
		t.Error("expected success=true")
	}
	if len(mt.Tasks) != 1 {
		t.Fatalf("expected 1 task info, got %d", len(mt.Tasks))
	}
	if mt.Tasks[0].Status != "running" {
		t.Errorf("expected status 'running', got %q", mt.Tasks[0].Status)
	}
}

func TestManageTaskKill(t *testing.T) {
	reg, wsDir := testRegistryWithTasks(t)
	defer reg.Shutdown()
	ctx := context.Background()

	// Start a background task
	runStep := &pb.StepUpdate{
		Action: &pb.StepUpdate_RunCommand{
			RunCommand: &pb.ActionRunCommand{
				Command:    "sleep 30",
				Cwd:        wsDir,
				Background: true,
			},
		},
	}
	reg.Execute(ctx, "run_command", runStep)
	taskID := runStep.GetRunCommand().TaskId

	time.Sleep(100 * time.Millisecond)

	// Kill the task
	killStep := &pb.StepUpdate{
		Action: &pb.StepUpdate_ManageTask{
			ManageTask: &pb.ActionManageTask{
				Action: "kill",
				TaskId: taskID,
			},
		},
	}

	err := reg.Execute(ctx, "manage_task", killStep)
	if err != nil {
		t.Fatalf("manage_task kill failed: %v", err)
	}

	if !killStep.GetManageTask().Success {
		t.Error("expected success=true for kill")
	}
}

func TestManageTaskSendInput(t *testing.T) {
	reg, wsDir := testRegistryWithTasks(t)
	defer reg.Shutdown()
	ctx := context.Background()

	// Start cat in background
	runStep := &pb.StepUpdate{
		Action: &pb.StepUpdate_RunCommand{
			RunCommand: &pb.ActionRunCommand{
				Command:    "cat",
				Cwd:        wsDir,
				Background: true,
			},
		},
	}
	reg.Execute(ctx, "run_command", runStep)
	taskID := runStep.GetRunCommand().TaskId

	time.Sleep(100 * time.Millisecond)

	// Send input
	inputStep := &pb.StepUpdate{
		Action: &pb.StepUpdate_ManageTask{
			ManageTask: &pb.ActionManageTask{
				Action: "send_input",
				TaskId: taskID,
				Input:  "test input\n",
			},
		},
	}

	err := reg.Execute(ctx, "manage_task", inputStep)
	if err != nil {
		t.Fatalf("manage_task send_input failed: %v", err)
	}

	if !inputStep.GetManageTask().Success {
		t.Error("expected success=true for send_input")
	}
}

func TestManageTaskStatusMissingID(t *testing.T) {
	reg, _ := testRegistryWithTasks(t)
	defer reg.Shutdown()
	ctx := context.Background()

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ManageTask{
			ManageTask: &pb.ActionManageTask{
				Action: "status",
			},
		},
	}

	err := reg.Execute(ctx, "manage_task", step)
	if err == nil {
		t.Error("manage_task status should error without task_id")
	}
}

func TestManageTaskUnknownAction(t *testing.T) {
	reg, _ := testRegistryWithTasks(t)
	defer reg.Shutdown()
	ctx := context.Background()

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ManageTask{
			ManageTask: &pb.ActionManageTask{
				Action: "invalid",
			},
		},
	}

	err := reg.Execute(ctx, "manage_task", step)
	if err == nil {
		t.Error("manage_task should error for unknown action")
	}
}

// ─── Tool Registration Tests ────────────────────────────────────────────

func TestRegisterManageTask(t *testing.T) {
	reg, _ := testRegistryWithTasks(t)
	defer reg.Shutdown()

	if !reg.HasTool("manage_task") {
		t.Error("manage_task should be registered")
	}
	if !reg.HasTool("run_command") {
		t.Error("run_command should be registered")
	}
}

func TestGetToolNameManageTask(t *testing.T) {
	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_ManageTask{
			ManageTask: &pb.ActionManageTask{},
		},
	}

	name := GetToolName(step)
	if name != "manage_task" {
		t.Errorf("expected 'manage_task', got %q", name)
	}
}

func TestRegistryShutdown(t *testing.T) {
	wsDir := t.TempDir()
	wsMgr, _ := workspace.NewManager([]string{wsDir})
	logger := testLogger()
	reg := NewRegistry(wsMgr, logger)

	// Should not panic even without tasks
	reg.Shutdown()
}

func TestRunCommandSynchronousUnchanged(t *testing.T) {
	// Verify synchronous mode still works as before
	cfg := &pb.BuiltinToolsConfig{RunCommand: true}
	reg, wsDir := testRegistryWithConfig(t, cfg)
	ctx := context.Background()

	step := &pb.StepUpdate{
		Action: &pb.StepUpdate_RunCommand{
			RunCommand: &pb.ActionRunCommand{
				Command: "echo sync-test",
				Cwd:     wsDir,
			},
		},
	}

	err := reg.Execute(ctx, "run_command", step)
	if err != nil {
		t.Fatalf("sync run_command failed: %v", err)
	}

	rc := step.GetRunCommand()
	if rc.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", rc.ExitCode)
	}
	if rc.Stdout != "sync-test\n" {
		t.Errorf("expected stdout 'sync-test\\n', got %q", rc.Stdout)
	}
	// Sync mode should NOT set task_id or terminal_id
	if rc.TaskId != "" {
		t.Error("sync mode should not set task_id")
	}
	if rc.AssignedTerminalId != "" {
		t.Error("sync mode should not set assigned_terminal_id")
	}

	reg.Shutdown()
}

// ─── Helpers ────────────────────────────────────────────────────────────

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// testRegistryForBackground creates a Registry that includes a temporary workspace path.
func testRegistryForBackground(t *testing.T) (*Registry, string) {
	t.Helper()
	return testRegistryWithConfig(t, &pb.BuiltinToolsConfig{
		RunCommand: true,
	})
}

// helper to create a test file within the workspace
func createTestFile(t *testing.T, wsDir, name, content string) string {
	t.Helper()
	path := filepath.Join(wsDir, name)
	os.WriteFile(path, []byte(content), 0644)
	return path
}
