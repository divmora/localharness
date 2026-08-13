package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/util"
	"google.golang.org/protobuf/proto"
)

const (
	// defaultMaxTasks is the maximum number of concurrent background tasks.
	defaultMaxTasks = 20

	// outputBufferSize is the ring buffer size for each task's output (100KB).
	outputBufferSize = 102400

	// recentOutputSize is how many bytes of output to include in status responses.
	recentOutputSize = 10240

	// terminalMarkerPrefix is the delimiter echoed after each command in a persistent terminal.
	terminalMarkerPrefix = "__LH_DONE_"

	// taskPruneAge is how long completed tasks are kept before auto-pruning.
	taskPruneAge = 30 * time.Minute

	// killGracePeriod is how long to wait after SIGTERM before sending SIGKILL.
	killGracePeriod = 5 * time.Second
)

// TaskStatus represents the lifecycle state of a background task.
type TaskStatus string

const (
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskKilled    TaskStatus = "killed"
)

// BackgroundTask represents a command running in the background.
type BackgroundTask struct {
	ID          string
	Command     string
	Cwd         string
	Status      TaskStatus
	ExitCode    int
	StartedAt   time.Time
	CompletedAt time.Time
	TerminalID  string

	cmd    *exec.Cmd
	output *RingBuffer
	stdin  io.WriteCloser
	cancel context.CancelFunc
	done   chan struct{}
}

// PersistentTerminal is a long-lived bash session that preserves environment
// variables across command invocations.
type PersistentTerminal struct {
	ID     string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	output *RingBuffer
	mu     sync.Mutex // serializes command execution
	cancel context.CancelFunc
	done   chan struct{}
}

// TaskManager manages background tasks and persistent terminal sessions.
type TaskManager struct {
	mu        sync.RWMutex
	tasks     map[string]*BackgroundTask
	terminals map[string]*PersistentTerminal
	logger    *slog.Logger
	maxTasks  int
	wsMgr     interface{ ValidatePath(string) (string, error) } // workspace.Manager or nil
	schedMgr  *ScheduleManager
	notifyCh  chan<- SystemMessage // Optional: push task completion notifications
	stepEmitter func(*pb.StepUpdate) // For live output streaming
}

// NewTaskManager creates a new TaskManager.
func NewTaskManager(logger *slog.Logger, maxTasks int) *TaskManager {
	if maxTasks <= 0 {
		maxTasks = defaultMaxTasks
	}
	return &TaskManager{
		tasks:     make(map[string]*BackgroundTask),
		terminals: make(map[string]*PersistentTerminal),
		logger:    logger,
		maxTasks:  maxTasks,
		schedMgr:  NewScheduleManager(logger),
	}
}

// SetStepEmitter registers the emitter for live terminal streaming.
func (tm *TaskManager) SetStepEmitter(emitter func(*pb.StepUpdate)) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.stepEmitter = emitter
}

// SetNotifyChannel sets the channel for task completion notifications.
func (tm *TaskManager) SetNotifyChannel(ch chan<- SystemMessage) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.notifyCh = ch
}

// ScheduleManager returns the task manager's schedule manager.
func (tm *TaskManager) ScheduleManager() *ScheduleManager {
	return tm.schedMgr
}

// shortID generates a short unique ID for tasks and terminals.
// Uses the last 8 chars of UUIDv7 (random portion) to avoid collisions —
// the first 8 chars are the timestamp and are identical within 1ms.
func shortID() string {
	s := util.NewUUID()
	return s[len(s)-8:]
}

// ─── Background Tasks ───────────────────────────────────────────────────

// StartBackground starts a command as a background task.
// It returns immediately after spawning, populating task_id in the result fields.
// If waitMs > 0, it waits that many milliseconds for initial output before returning.
func (tm *TaskManager) StartBackground(ctx context.Context, command, cwd string, env map[string]string, waitMs int, step *pb.StepUpdate) (taskID, stdout string, err error) {
	tm.mu.Lock()

	// Check task limit
	running := 0
	for _, t := range tm.tasks {
		if t.Status == TaskRunning {
			running++
		}
	}
	if running >= tm.maxTasks {
		tm.mu.Unlock()
		return "", "", fmt.Errorf("maximum concurrent background tasks reached (%d)", tm.maxTasks)
	}

	taskID = "task-" + shortID()
	taskCtx, cancel := context.WithCancel(ctx)

	cmd := exec.CommandContext(taskCtx, "bash", "-c", command)
	if cwd != "" {
		cmd.Dir = cwd
	}

	// Build environment
	cmdEnv := cmd.Environ()
	cmdEnv = append(cmdEnv, "PAGER=cat", "GIT_TERMINAL_PROMPT=0")
	for k, v := range env {
		k = strings.ReplaceAll(k, "\n", "")
		v = strings.ReplaceAll(v, "\n", "")
		cmdEnv = append(cmdEnv, k+"="+v)
	}
	cmd.Env = cmdEnv

	// Set up output capture
	output := NewRingBuffer(outputBufferSize)
	
	streamWriter := &taskStreamWriter{
		buf: output,
		step: step,
		tm: tm,
	}
	cmd.Stdout = streamWriter
	cmd.Stderr = streamWriter

	// Set up stdin pipe
	stdinPipe, pipeErr := cmd.StdinPipe()
	if pipeErr != nil {
		cancel()
		tm.mu.Unlock()
		return "", "", fmt.Errorf("task_manager: stdin pipe: %w", pipeErr)
	}

	// Use process group so we can kill the entire tree (on supported platforms)
	setProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		cancel()
		tm.mu.Unlock()
		return "", "", fmt.Errorf("task_manager: start: %w", err)
	}

	task := &BackgroundTask{
		ID:        taskID,
		Command:   command,
		Cwd:       cwd,
		Status:    TaskRunning,
		StartedAt: time.Now(),
		cmd:       cmd,
		output:    output,
		stdin:     stdinPipe,
		cancel:    cancel,
		done:      make(chan struct{}),
	}

	tm.tasks[taskID] = task
	tm.mu.Unlock()

	tm.logger.Info("started background task", "task_id", taskID, "command", command)

	// Monitor completion in a goroutine
	go tm.monitorTask(task)

	// If waitMs > 0, wait for initial output or completion
	if waitMs > 0 {
		timer := time.NewTimer(time.Duration(waitMs) * time.Millisecond)
		defer timer.Stop()

		select {
		case <-task.done:
			// Task completed within wait window
		case <-timer.C:
			// Wait period expired, task still running
		}
		stdout = output.Last(recentOutputSize)
	}

	return taskID, stdout, nil
}

// monitorTask waits for a background task to complete and updates its status.
func (tm *TaskManager) monitorTask(task *BackgroundTask) {
	err := task.cmd.Wait()

	tm.mu.Lock()
	defer tm.mu.Unlock()

	task.CompletedAt = time.Now()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			task.ExitCode = exitErr.ExitCode()
			// Check if killed by signal
			if exitErr.ExitCode() == -1 {
				task.Status = TaskKilled
			} else {
				task.Status = TaskFailed
			}
		} else {
			task.Status = TaskFailed
			task.ExitCode = -1
		}
	} else {
		task.Status = TaskCompleted
		task.ExitCode = 0
	}

	close(task.done)
	tm.logger.Info("background task completed",
		"task_id", task.ID,
		"status", task.Status,
		"exit_code", task.ExitCode,
	)

	// Push completion notification to the engine
	if tm.notifyCh != nil {
		output := task.output.Last(1024) // Last 1KB for context
		content := fmt.Sprintf("Command: %s\nStatus: %s\nExit code: %d", task.Command, task.Status, task.ExitCode)
		if output != "" {
			content += fmt.Sprintf("\nOutput (last 1KB):\n%s", output)
		}

		select {
		case tm.notifyCh <- SystemMessage{
			Source:  string(task.Status),
			TaskID:  task.ID,
			Content: content,
			FiredAt: task.CompletedAt,
		}:
		default:
			tm.logger.Warn("notification channel full, dropping task completion", "task_id", task.ID)
		}
	}

	// Schedule auto-prune of old completed tasks
	go tm.pruneOldTasks()
}

// pruneOldTasks removes completed tasks older than taskPruneAge.
func (tm *TaskManager) pruneOldTasks() {
	time.Sleep(taskPruneAge)

	tm.mu.Lock()
	defer tm.mu.Unlock()

	cutoff := time.Now().Add(-taskPruneAge)
	for id, task := range tm.tasks {
		if task.Status != TaskRunning && !task.CompletedAt.IsZero() && task.CompletedAt.Before(cutoff) {
			delete(tm.tasks, id)
			tm.logger.Debug("pruned old task", "task_id", id)
		}
	}
}

// ListTasks returns info about all tasks.
func (tm *TaskManager) ListTasks() []TaskSnapshot {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	var result []TaskSnapshot
	for _, task := range tm.tasks {
		result = append(result, tm.snapshotTask(task))
	}
	return result
}

// GetTaskStatus returns info about a specific task.
func (tm *TaskManager) GetTaskStatus(taskID string) (TaskSnapshot, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	task, ok := tm.tasks[taskID]
	if !ok {
		return TaskSnapshot{}, fmt.Errorf("unknown task: %s", taskID)
	}
	return tm.snapshotTask(task), nil
}

// KillTask kills a running background task with SIGTERM, then SIGKILL if needed.
// Also handles schedule entries (sched-*, cron-*).
func (tm *TaskManager) KillTask(taskID string) error {
	// Check if this is a schedule entry
	if strings.HasPrefix(taskID, "sched-") || strings.HasPrefix(taskID, "cron-") {
		if tm.schedMgr != nil {
			return tm.schedMgr.Cancel(taskID)
		}
		return fmt.Errorf("unknown task: %s", taskID)
	}

	tm.mu.RLock()
	task, ok := tm.tasks[taskID]
	if !ok {
		tm.mu.RUnlock()
		return fmt.Errorf("unknown task: %s", taskID)
	}
	status := task.Status
	tm.mu.RUnlock()

	if status != TaskRunning {
		return fmt.Errorf("task %s is not running (status: %s)", taskID, status)
	}

	tm.logger.Info("killing background task", "task_id", taskID)

	// Try SIGTERM to the process group first (or Kill on Windows)
	if task.cmd.Process != nil {
		_ = terminateProcessGroup(task.cmd.Process.Pid)
	}

	// Wait for graceful exit
	select {
	case <-task.done:
		tm.mu.Lock()
		task.Status = TaskKilled
		tm.mu.Unlock()
		return nil
	case <-time.After(killGracePeriod):
	}

	// Force kill
	if task.cmd.Process != nil {
		_ = killProcessGroup(task.cmd.Process.Pid)
	}
	task.cancel()

	// Wait for goroutine to finish
	<-task.done

	tm.mu.Lock()
	task.Status = TaskKilled
	tm.mu.Unlock()

	return nil
}

// SendInput writes data to a running task's stdin.
func (tm *TaskManager) SendInput(taskID, input string) error {
	tm.mu.RLock()
	task, ok := tm.tasks[taskID]
	if !ok {
		tm.mu.RUnlock()
		return fmt.Errorf("unknown task: %s", taskID)
	}
	status := task.Status
	stdinPipe := task.stdin
	tm.mu.RUnlock()

	if status != TaskRunning {
		return fmt.Errorf("task %s is not running (status: %s)", taskID, status)
	}

	if stdinPipe == nil {
		return fmt.Errorf("task %s has no stdin pipe", taskID)
	}

	_, err := stdinPipe.Write([]byte(input))
	if err != nil {
		return fmt.Errorf("task_manager: send_input: %w", err)
	}
	return nil
}

// TaskSnapshot is an immutable snapshot of a task's state for serialization.
type TaskSnapshot struct {
	ID           string
	Command      string
	Cwd          string
	Status       TaskStatus
	ExitCode     int
	StartedAt    time.Time
	CompletedAt  time.Time
	RecentOutput string
	TerminalID   string
}

func (tm *TaskManager) snapshotTask(task *BackgroundTask) TaskSnapshot {
	return TaskSnapshot{
		ID:           task.ID,
		Command:      task.Command,
		Cwd:          task.Cwd,
		Status:       task.Status,
		ExitCode:     task.ExitCode,
		StartedAt:    task.StartedAt,
		CompletedAt:  task.CompletedAt,
		RecentOutput: task.output.Last(recentOutputSize),
		TerminalID:   task.TerminalID,
	}
}

// ─── Persistent Terminals ───────────────────────────────────────────────

// RunInTerminal executes a command in a persistent terminal session.
// If terminalID is empty, a new terminal is created.
// Returns the terminal ID and command output.
func (tm *TaskManager) RunInTerminal(ctx context.Context, command, cwd, terminalID string, env map[string]string, timeoutMs int, step *pb.StepUpdate) (assignedTerminalID, stdout string, exitCode int, err error) {
	if terminalID != "" {
		// Reuse existing terminal
		tm.mu.RLock()
		term, ok := tm.terminals[terminalID]
		tm.mu.RUnlock()

		if !ok {
			return "", "", -1, fmt.Errorf("unknown terminal: %s", terminalID)
		}
		return tm.execInTerminal(ctx, term, command, timeoutMs, step)
	}

	// Create new terminal
	term, err := tm.createTerminal(cwd, env)
	if err != nil {
		return "", "", -1, err
	}
	return tm.execInTerminal(ctx, term, command, timeoutMs, step)
}

// createTerminal starts a new persistent bash session.
func (tm *TaskManager) createTerminal(cwd string, env map[string]string) (*PersistentTerminal, error) {
	termID := "term-" + shortID()

	termCtx, cancel := context.WithCancel(context.Background())
	// Use non-interactive bash to avoid prompt noise
	cmd := exec.CommandContext(termCtx, "bash", "--norc", "--noprofile")
	if cwd != "" {
		cmd.Dir = cwd
	}

	// Build environment
	cmdEnv := cmd.Environ()
	cmdEnv = append(cmdEnv, "PAGER=cat", "GIT_TERMINAL_PROMPT=0")
	for k, v := range env {
		k = strings.ReplaceAll(k, "\n", "")
		v = strings.ReplaceAll(v, "\n", "")
		cmdEnv = append(cmdEnv, k+"="+v)
	}
	cmd.Env = cmdEnv

	output := NewRingBuffer(outputBufferSize)
	cmd.Stdout = output
	cmd.Stderr = output

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("task_manager: terminal stdin: %w", err)
	}

	setProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("task_manager: terminal start: %w", err)
	}

	term := &PersistentTerminal{
		ID:     termID,
		cmd:    cmd,
		stdin:  stdinPipe,
		output: output,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	// Monitor terminal process
	go func() {
		cmd.Wait()
		close(term.done)
	}()

	tm.mu.Lock()
	tm.terminals[termID] = term
	tm.mu.Unlock()

	tm.logger.Info("created persistent terminal", "terminal_id", termID, "cwd", cwd)

	// Give bash a moment to initialize
	time.Sleep(100 * time.Millisecond)

	return term, nil
}

// execInTerminal runs a command in an existing persistent terminal and captures output.
func (tm *TaskManager) execInTerminal(ctx context.Context, term *PersistentTerminal, command string, timeoutMs int, step *pb.StepUpdate) (terminalID, stdout string, exitCode int, err error) {
	term.mu.Lock()
	defer term.mu.Unlock()

	// Check terminal is still alive
	select {
	case <-term.done:
		return term.ID, "", -1, fmt.Errorf("terminal %s has exited", term.ID)
	default:
	}

	// Use a unique marker to delimit command output.
	// We use a BEGIN marker and an END marker to reliably extract just the command output.
	markerID := util.NewUUID()
	beginMarker := terminalMarkerPrefix + "BEGIN_" + markerID + "__"
	endMarker := terminalMarkerPrefix + "END_" + markerID + "__"

	// Write the command sequence:
	// 1. Echo BEGIN marker
	// 2. Run the command
	// 3. Capture exit code
	// 4. Echo END marker with exit code
	cmdLine := fmt.Sprintf("echo '%s'\n%s\n__LH_EC__=$?\necho '%s'\"$__LH_EC__\"\n", beginMarker, command, endMarker)
	if _, err := term.stdin.Write([]byte(cmdLine)); err != nil {
		return term.ID, "", -1, fmt.Errorf("task_manager: write to terminal: %w", err)
	}

	// Set up timeout
	if timeoutMs <= 0 {
		timeoutMs = 30000
	}
	deadline := time.After(time.Duration(timeoutMs) * time.Millisecond)

	// Poll for the end marker in output
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	
	var lastEmit time.Time

	for {
		select {
		case <-ctx.Done():
			return term.ID, "", -1, ctx.Err()
		case <-term.done:
			// Terminal died — return whatever output we have
			allOutput := term.output.String()
			cmdOutput := extractBetweenMarkers(allOutput, beginMarker, endMarker)
			return term.ID, cmdOutput, -1, fmt.Errorf("terminal exited unexpectedly")
		case <-deadline:
			// Timed out waiting for end marker
			allOutput := term.output.String()
			cmdOutput := extractBetweenMarkers(allOutput, beginMarker, endMarker)
			return term.ID, cmdOutput, -1, nil
		case <-ticker.C:
			allOutput := term.output.String()
			cmdOutput := extractBetweenMarkers(allOutput, beginMarker, endMarker)
			
			if tm.stepEmitter != nil && step != nil && time.Since(lastEmit) > 200*time.Millisecond {
				lastEmit = time.Now()
				clonedStep := proto.Clone(step).(*pb.StepUpdate)
				if rc, ok := clonedStep.Action.(*pb.StepUpdate_RunCommand); ok {
					rc.RunCommand.Stdout = truncateOutput(cmdOutput, 100000)
					clonedStep.State = pb.StepUpdate_STATE_STREAMING
					tm.stepEmitter(clonedStep)
				}
			}

			if endIdx := strings.Index(allOutput, endMarker); endIdx >= 0 {
				// Found end marker — extract command output and exit code
				code := parseExitCodeAfterMarker(allOutput, endMarker)
				return term.ID, cmdOutput, code, nil
			}
		}
	}
}

// extractBetweenMarkers extracts text between begin and end markers.
func extractBetweenMarkers(allOutput, beginMarker, endMarker string) string {
	beginIdx := strings.Index(allOutput, beginMarker)
	if beginIdx < 0 {
		return ""
	}
	// Skip past the begin marker and its trailing newline
	start := beginIdx + len(beginMarker)
	if start < len(allOutput) && allOutput[start] == '\n' {
		start++
	}

	endIdx := strings.Index(allOutput[start:], endMarker)
	if endIdx < 0 {
		// End marker not found yet — return everything after begin
		return strings.TrimSpace(allOutput[start:])
	}

	return strings.TrimSpace(allOutput[start : start+endIdx])
}

// parseExitCodeAfterMarker parses the exit code appended after the end marker.
func parseExitCodeAfterMarker(allOutput, endMarker string) int {
	idx := strings.Index(allOutput, endMarker)
	if idx < 0 {
		return -1
	}
	afterMarker := allOutput[idx+len(endMarker):]
	exitCode := 0
	if afterMarker != "" {
		line := strings.SplitN(afterMarker, "\n", 2)[0]
		line = strings.TrimSpace(line)
		if _, err := fmt.Sscanf(line, "%d", &exitCode); err != nil {
			exitCode = -1
		}
	}
	return exitCode
}

// extractTerminalOutput extracts the new output added since beforeLen, up to the marker.
func extractTerminalOutput(allOutput string, beforeLen int, marker string) string {
	if beforeLen >= len(allOutput) {
		return ""
	}
	newOutput := allOutput[beforeLen:]

	// Remove the marker line if present
	if idx := strings.Index(newOutput, marker); idx >= 0 {
		newOutput = newOutput[:idx]
	}

	return strings.TrimSpace(newOutput)
}

// parseTerminalResult extracts command output and exit code from terminal output
// containing the completion marker.
func parseTerminalResult(allOutput string, beforeLen int, marker string) (string, int) {
	if beforeLen >= len(allOutput) {
		return "", -1
	}
	newOutput := allOutput[beforeLen:]

	idx := strings.Index(newOutput, marker)
	if idx < 0 {
		return strings.TrimSpace(newOutput), -1
	}

	// Output before marker
	cmdOutput := strings.TrimSpace(newOutput[:idx])

	// Parse exit code after marker
	afterMarker := newOutput[idx+len(marker):]
	exitCode := 0
	if afterMarker != "" {
		// Read until newline
		line := strings.SplitN(afterMarker, "\n", 2)[0]
		line = strings.TrimSpace(line)
		if _, err := fmt.Sscanf(line, "%d", &exitCode); err != nil {
			exitCode = -1
		}
	}

	return cmdOutput, exitCode
}

// ─── Lifecycle ──────────────────────────────────────────────────────────

// Shutdown kills all running tasks and terminals. Called when the session disconnects.
func (tm *TaskManager) Shutdown() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.logger.Info("shutting down task manager",
		"running_tasks", len(tm.tasks),
		"terminals", len(tm.terminals),
	)

	// Kill all running tasks
	for _, task := range tm.tasks {
		if task.Status == TaskRunning {
			if task.cmd.Process != nil {
				_ = killProcessGroup(task.cmd.Process.Pid)
			}
			task.cancel()
		}
	}

	// Kill all terminals
	for _, term := range tm.terminals {
		if term.cmd.Process != nil {
			_ = killProcessGroup(term.cmd.Process.Pid)
		}
		term.cancel()
	}

	// Cancel all scheduled timers/cron
	if tm.schedMgr != nil {
		tm.schedMgr.Shutdown()
	}

	// Wait briefly for cleanup
	time.Sleep(100 * time.Millisecond)

	// Clear maps
	tm.tasks = make(map[string]*BackgroundTask)
	tm.terminals = make(map[string]*PersistentTerminal)
}

// RunningTaskCount returns the number of currently running background tasks.
func (tm *TaskManager) RunningTaskCount() int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	count := 0
	for _, task := range tm.tasks {
		if task.Status == TaskRunning {
			count++
		}
	}
	return count
}

// ─── Synchronous with wait-before-async ─────────────────────────────────

// RunWithWait runs a command synchronously but promotes it to a background task
// if it doesn't complete within waitMs milliseconds. Returns either the completed
// result or a task_id for the background task.
func (tm *TaskManager) RunWithWait(ctx context.Context, command, cwd string, env map[string]string, timeoutMs, waitMs int) (taskID, stdout, stderr string, exitCode int, timedOut bool, err error) {
	if waitMs <= 0 {
		// Pure synchronous execution — use the original approach
		return tm.runSync(ctx, command, cwd, env, timeoutMs)
	}

	// Start as background, then check if it completes within waitMs
	taskID, initialOutput, err := tm.StartBackground(ctx, command, cwd, env, waitMs, nil)
	if err != nil {
		return "", "", "", -1, false, err
	}

	// Check if the task completed during the wait
	tm.mu.RLock()
	task, ok := tm.tasks[taskID]
	var taskStatus TaskStatus
	var taskExitCode int
	var taskOutput string
	if ok {
		taskStatus = task.Status
		taskExitCode = task.ExitCode
		taskOutput = task.output.String()
	}
	tm.mu.RUnlock()

	if ok && taskStatus != TaskRunning {
		// Completed within wait period — return synchronous result
		return "", taskOutput, "", taskExitCode, false, nil
	}

	// Still running — return as background task
	return taskID, initialOutput, "", 0, false, nil
}

// runSync runs a command synchronously (used when no background or persistent mode).
func (tm *TaskManager) runSync(ctx context.Context, command, cwd string, env map[string]string, timeoutMs int) (taskID, stdout, stderr string, exitCode int, timedOut bool, err error) {
	if timeoutMs <= 0 {
		timeoutMs = 30000
	}
	timeout := time.Duration(timeoutMs) * time.Millisecond
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "bash", "-c", command)
	if cwd != "" {
		cmd.Dir = cwd
	}

	cmdEnv := cmd.Environ()
	cmdEnv = append(cmdEnv, "PAGER=cat", "GIT_TERMINAL_PROMPT=0")
	for k, v := range env {
		k = strings.ReplaceAll(k, "\n", "")
		v = strings.ReplaceAll(v, "\n", "")
		cmdEnv = append(cmdEnv, k+"="+v)
	}
	cmd.Env = cmdEnv

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	runErr := cmd.Run()

	stdoutStr := truncateOutput(stdoutBuf.String(), 100000)
	stderrStr := truncateOutput(stderrBuf.String(), 50000)

	if cmdCtx.Err() == context.DeadlineExceeded {
		return "", stdoutStr, stderrStr, -1, true, nil
	}

	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			return "", stdoutStr, stderrStr, exitErr.ExitCode(), false, nil
		}
		return "", stdoutStr, stderrStr, -1, false, runErr
	}

	return "", stdoutStr, stderrStr, 0, false, nil
}

// taskStreamWriter wraps a RingBuffer to emit streaming step updates for background tasks.
type taskStreamWriter struct {
	buf       *RingBuffer
	mu        sync.Mutex
	isStderr  bool
	lastEmit  time.Time
	step      *pb.StepUpdate
	tm        *TaskManager
}

func (w *taskStreamWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	n, err = w.buf.Write(p)
	
	if time.Since(w.lastEmit) > 500*time.Millisecond {
		w.doEmitLocked()
	}
	return
}

func (w *taskStreamWriter) doEmitLocked() {
	if w.tm.stepEmitter == nil || w.step == nil {
		return
	}
	w.lastEmit = time.Now()

	clonedStep := proto.Clone(w.step).(*pb.StepUpdate)
	rc, ok := clonedStep.Action.(*pb.StepUpdate_RunCommand)
	if !ok {
		return
	}

	// For background tasks, stdout and stderr are often combined in the same RingBuffer
	// but we'll try to put it in stdout.
	rc.RunCommand.Stdout = truncateOutput(w.buf.String(), 100000)
	
	clonedStep.State = pb.StepUpdate_STATE_STREAMING
	w.tm.stepEmitter(clonedStep)
}
