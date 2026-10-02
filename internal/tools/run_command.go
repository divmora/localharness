package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
	"github.com/divmora/localharness/internal/errors"
	"google.golang.org/protobuf/proto"
)

func getRunCommandDescription() string {
	osName := "mac"
	if runtime.GOOS == "linux" {
		osName = "linux"
	} else if runtime.GOOS == "windows" {
		osName = "windows"
	}
	shellName := "zsh"
	if runtime.GOOS != "darwin" {
		shellName = "bash"
	}
	return fmt.Sprintf("PROPOSE a command to run on behalf of the user. Operating System: %s. Shell: %s.\n"+
		"**NEVER PROPOSE A cd COMMAND**.\n"+
		"If you have this tool, note that you DO have the ability to run commands directly on the USER's system.\n"+
		"Make sure to specify CommandLine exactly as it should be run in the shell.\n"+
		"If the step doesn't return the command output, it means that the command was sent to the background as a task. You will receive messages with the command's output as it runs. To interact with a running command, use the manage_task tool. Use `send_input` to send stdin, `kill` to terminate the command, and `status` to check current status. IMPORTANT: Do NOT poll or loop on `status` to wait for completion. The system will automatically notify you with a message when the command finishes. Simply proceed with other work or stop calling tools after launching a command.\n"+
		"Commands will be run with PAGER=cat. You may want to limit the length of output for commands that usually rely on paging and may contain very long output (e.g. git log, use git log -n <N>).\n"+
		"IMPORTANT: The Cwd (working directory) MUST be within the user's workspace. Do NOT use /tmp, /home, or any path outside the workspace. If you need a temporary directory, use the scratch/ directory in your artifact directory.",
		osName, shellName)
}

func registerRunCommand(r *Registry) {
	r.Register("run_command", executeRunCommand, ToolSchema{
		Group:       ToolGroupWrite,
		Name:        "run_command",
		Description: getRunCommandDescription(),
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"CommandLine": map[string]interface{}{
					"type":        "string",
					"description": "The exact command line string to execute.",
				},
				"Cwd": map[string]interface{}{
					"type":        "string",
					"description": "The current working directory for the command",
				},
				"IsDaemon": map[string]interface{}{
					"type":        "boolean",
					"description": "Set to true for long-running support processes that are meant to keep running in the background indefinitely and are not expected to finish on their own (e.g., dev servers, file watchers, tunnels). Leave false (the default) for normal commands that are expected to terminate.",
				},
				"RequestedTerminalID": map[string]interface{}{
					"type":        "string",
					"description": "Optional ID of a persistent terminal to reuse. Specify a TerminalID returned from a previous persistent run_command to share its variables. Can only be used when RunPersistent is true. Leave this empty with RunPersistent set to true to create a new persistent terminal.",
				},
				"RunPersistent": map[string]interface{}{
					"type":        "boolean",
					"description": "Set to true to run this command in a persistent terminal that preserves environment and shell variables between invocations. Returns a TerminalID that can be specified in future run_command calls to share the environment. Note: persistent terminals share variables but are separate bash -c invocations; shell state like working directory, aliases, and functions are not shared.",
				},
				"WaitMsBeforeAsync": map[string]interface{}{
					"type":        "integer",
					"description": "This specifies the number of milliseconds to wait after starting the command before sending it to the background. If you want the command to complete execution synchronously, set this to a large enough value that you expect the command to complete in that time under ordinary circumstances. If you're starting an interactive or long-running command, set it to a large enough value that it would cause possible failure cases to execute synchronously (e.g. 500ms). Keep the value as small as possible, with a maximum of 10000ms.",
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
			"required": []string{"Cwd", "WaitMsBeforeAsync", "CommandLine", "ToolSummary", "ToolAction"},
		},
	})
}

func executeRunCommand(ctx context.Context, step *pb.StepUpdate, r *Registry) error {
	rc := step.GetRunCommand()
	if rc == nil {
		return errors.New(errors.ErrCodeToolValidation,
			"run_command tool missing action").
			WithContext("component", "run_command")
	}

	if rc.Command == "" {
		return errors.New(errors.ErrCodeToolValidation,
			"run_command command is required").
			WithContext("component", "run_command")
	}

	// Validate working directory
	cwd := rc.Cwd
	if cwd != "" {
		validCwd, err := r.ValidatePathContext(ctx, cwd)
		if err != nil {
			return errors.Wrap(err, errors.ErrCodeWorkspaceValidation,
				"invalid working directory").
				WithContext("cwd", cwd).
				WithContext("command", rc.Command).
				WithContext("operation", "run_command").
				WithComponent("run_command")
		}
		cwd = validCwd
	} else if primary := r.PrimaryWorkspace(); primary != "" {
		cwd = primary
	}
	rc.Cwd = cwd

	// Merge context-provided env vars (from subagent/engine) with user-specified env vars
	mergedEnv := mergeContextEnv(ctx, rc.Env)

	startedAt := time.Now().Format("2006-01-02T15:04:05-07:00")

	// ── Persistent terminal mode ──
	if rc.Persistent {
		if r.taskMgr == nil {
			return errors.New(errors.ErrCodeToolExecution,
				"task manager not available for persistent mode").
				WithContext("component", "run_command")
		}

		termID, stdout, exitCode, err := r.taskMgr.RunInTerminal(
			ctx, rc.Command, cwd, rc.TerminalId, mergedEnv, int(rc.TimeoutMs), step,
		)
		if err != nil {
			return errors.Wrap(err, errors.ErrCodeToolExecution,
				"persistent terminal execution failed").
				WithContext("command", rc.Command).
				WithContext("cwd", cwd).
				WithContext("operation", "run_command").
				WithComponent("run_command")
		}

		rc.Stdout = truncateOutput(stdout, 100000)
		rc.ExitCode = int32(exitCode)
		rc.AssignedTerminalId = termID
		if exitCode == -1 {
			rc.TimedOut = true
		}
		completedAt := time.Now().Format("2006-01-02T15:04:05-07:00")
		rc.FormattedOutput = formatRunCommandOutput(startedAt, completedAt, rc.ExitCode, rc.Stdout, "")
		return nil
	}

	// ── Background / Async mode ──
	if rc.Background || rc.IsDaemon || (rc.WaitMsBeforeAsync > 0 && r.taskMgr != nil) {
		if r.taskMgr == nil {
			return errors.New(errors.ErrCodeToolExecution,
				"task manager not available for background mode").
				WithContext("component", "run_command")
		}

		waitMs := int(rc.WaitMsBeforeAsync)
		if waitMs <= 0 && (rc.Background || rc.IsDaemon) {
			waitMs = 500
		}

		taskID, stdout, logPath, logURI, err := r.taskMgr.StartBackground(
			ctx, rc.Command, cwd, mergedEnv, waitMs, step,
		)
		if err != nil {
			return errors.Wrap(err, errors.ErrCodeToolExecution,
				"background task start failed").
				WithContext("command", rc.Command).
				WithContext("cwd", cwd).
				WithContext("operation", "run_command").
				WithComponent("run_command")
		}

		completedAt := time.Now().Format("2006-01-02T15:04:05-07:00")
		snap, err := r.taskMgr.GetTaskStatus(taskID)
		if err == nil && snap.Status != TaskRunning {
			// Finished within wait window
			rc.Stdout = truncateOutput(snap.RecentOutput, 100000)
			rc.ExitCode = int32(snap.ExitCode)
			rc.LogPath = logPath
			rc.LogUri = logURI
			rc.FormattedOutput = formatRunCommandOutput(startedAt, completedAt, rc.ExitCode, rc.Stdout, "")
			return nil
		}

		// Still running as a background task
		rc.TaskId = taskID
		rc.Stdout = truncateOutput(stdout, 100000)
		rc.LogPath = logPath
		rc.LogUri = logURI
		rc.FormattedOutput = formatBackgroundTaskOutput(taskID, logURI)
		return nil
	}

	// ── Synchronous mode (default, existing behavior) ──
	timeoutMs := int(rc.TimeoutMs)
	if timeoutMs <= 0 {
		timeoutMs = 30000 // 30 second default
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Run via bash -c for proper shell interpretation
	cmd := exec.CommandContext(cmdCtx, "bash", "-c", rc.Command)

	// Set working directory
	if cwd != "" {
		cmd.Dir = cwd
	}

	// Build environment: inherit current env + add PAGER=cat + context & user env
	env := cmd.Environ()
	env = append(env, "PAGER=cat")

	// Ensure GIT_TERMINAL_PROMPT=0 to prevent interactive git prompts
	env = append(env, "GIT_TERMINAL_PROMPT=0")

	// Add context and user-specified env vars
	for k, v := range mergedEnv {
		// Sanitize: no newlines in env vars
		k = strings.ReplaceAll(k, "\n", "")
		v = strings.ReplaceAll(v, "\n", "")
		env = append(env, k+"="+v)
	}
	cmd.Env = env

	// Capture stdout and stderr separately
	var stdout, stderr bytes.Buffer

	stdoutWriter := &streamWriter{
		buf:      &stdout,
		isStderr: false,
		step:     step,
		rc:       rc,
		r:        r,
	}
	stderrWriter := &streamWriter{
		buf:      &stderr,
		isStderr: true,
		step:     step,
		rc:       rc,
		r:        r,
	}

	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter

	err := cmd.Run()

	// Populate results
	rc.Stdout = truncateOutput(stdout.String(), 100000)
	rc.Stderr = truncateOutput(stderr.String(), 50000)

	if cmdCtx.Err() == context.DeadlineExceeded {
		rc.TimedOut = true
		rc.ExitCode = -1
	} else if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			rc.ExitCode = int32(exitErr.ExitCode())
		} else {
			return errors.Wrap(err, errors.ErrCodeToolExecution,
				"command execution failed").
				WithContext("command", rc.Command).
				WithContext("cwd", cwd).
				WithContext("operation", "run_command").
				WithComponent("run_command")
		}
	} else {
		rc.ExitCode = 0
	}

	completedAt := time.Now().Format("2006-01-02T15:04:05-07:00")
	rc.FormattedOutput = formatRunCommandOutput(startedAt, completedAt, rc.ExitCode, rc.Stdout, rc.Stderr)
	return nil
}

func formatRunCommandOutput(startedAt, completedAt string, exitCode int32, stdout, stderr string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Created At: %s\nCompleted At: %s\n\nThe command exited with code %d.\n", startedAt, completedAt, exitCode))
	if stderr != "" && exitCode != 0 {
		sb.WriteString("Error:\n")
		sb.WriteString(stderr)
		if !strings.HasSuffix(stderr, "\n") {
			sb.WriteByte('\n')
		}
	}
	if stdout != "" {
		sb.WriteString("Output:\n")
		sb.WriteString(stdout)
		if !strings.HasSuffix(stdout, "\n") {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func formatBackgroundTaskOutput(taskID, logURI string) string {
	return fmt.Sprintf("Tool is running as a background task with task id: %s\n"+
		"Task logs are available at: %s\n"+
		"YOU MUST TAKE ONE OF THE FOLLOWING TWO ACTIONS: A) either proceed to other relevant work (if any) or, B) simply update the user with a short message (that you have launched the command and will wait for it to finish) and end the turn.\n"+
		" DO NOTHING ELSE.\n", taskID, logURI)
}

// truncateOutput caps output length to prevent huge payloads.
func truncateOutput(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + fmt.Sprintf("\n\n... [output truncated, showing %d/%d bytes]", maxLen, len(s))
}

type streamWriter struct {
	buf      *bytes.Buffer
	mu       sync.Mutex
	isStderr bool
	lastEmit time.Time
	step     *pb.StepUpdate
	rc       *pb.ActionRunCommand
	r        *Registry
}

func (w *streamWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	n, err = w.buf.Write(p)

	// Emit a streaming step at most every 200ms
	if time.Since(w.lastEmit) > 200*time.Millisecond {
		w.doEmitLocked()
	}
	return
}

func (w *streamWriter) doEmitLocked() {
	if w.r.stepEmitter == nil {
		return
	}
	w.lastEmit = time.Now()

	// Clone the StepUpdate to avoid race conditions
	clonedStep := proto.Clone(w.step).(*pb.StepUpdate)

	// Get a reference to the cloned ActionRunCommand
	rc, ok := clonedStep.Action.(*pb.StepUpdate_RunCommand)
	if !ok {
		return
	}

	// Update the strings
	if w.isStderr {
		rc.RunCommand.Stderr = truncateOutput(w.buf.String(), 50000)
	} else {
		rc.RunCommand.Stdout = truncateOutput(w.buf.String(), 100000)
	}

	clonedStep.State = pb.StepUpdate_STATE_STREAMING
	w.r.EmitStep(clonedStep)
}

func mergeContextEnv(ctx context.Context, explicit map[string]string) map[string]string {
	ctxEnv := EnvironmentFromContext(ctx)
	if len(ctxEnv) == 0 && len(explicit) == 0 {
		return nil
	}
	merged := make(map[string]string, len(ctxEnv)+len(explicit))
	for k, v := range ctxEnv {
		merged[k] = v
	}
	for k, v := range explicit {
		merged[k] = v
	}
	return merged
}
