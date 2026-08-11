package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	pb "github.com/divmora/localharness/gen/go/localharness/v1"
)

func registerRunCommand(r *Registry) {
	r.Register("run_command", executeRunCommand, ToolSchema{
		Group: ToolGroupWrite,
		Name:        "run_command",
		Description: "Execute a shell command. Only use this when no purpose-built tool covers the task " +
			"(e.g., running builds, tests, git commands, package managers, linters, or custom scripts). " +
			"Do NOT use run_command for operations that have dedicated tools: " +
			"use view_file instead of cat/head/tail, grep_search instead of grep/rg, " +
			"list_dir instead of ls, find_file instead of find, " +
			"read_url_content instead of curl/wget, write_to_file instead of echo/cat >. " +
			"The command runs in bash with PAGER=cat set by default. Returns stdout, stderr, and exit code. " +
			"Commands are subject to a timeout (default 30s). Set background=true to run as a background task (returns task_id). " +
			"Set persistent=true to run in a persistent terminal that preserves environment across invocations. " +
			"Prefer non-destructive operations. Avoid rm -rf or similar destructive commands unless the user explicitly instructs it.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command":               map[string]interface{}{"type": "string", "description": "The shell command to execute"},
				"cwd":                   map[string]interface{}{"type": "string", "description": "Working directory for the command"},
				"timeout_ms":            map[string]interface{}{"type": "integer", "description": "Timeout in milliseconds (default: 30000)"},
				"env":                   map[string]interface{}{"type": "object", "description": "Additional environment variables"},
				"background":            map[string]interface{}{"type": "boolean", "description": "If true, start as background task and return task_id"},
				"persistent":            map[string]interface{}{"type": "boolean", "description": "If true, run in a persistent terminal session that preserves env vars"},
				"terminal_id":           map[string]interface{}{"type": "string", "description": "Reuse an existing persistent terminal by ID"},
				"wait_ms_before_async":  map[string]interface{}{"type": "integer", "description": "Wait this many ms for initial output before promoting to background (used with background=true)"},
			},
			"required": []string{"command"},
		},
	})
}

func executeRunCommand(ctx context.Context, step *pb.StepUpdate, r *Registry) error {
	rc := step.GetRunCommand()
	if rc == nil {
		return fmt.Errorf("run_command: missing action")
	}

	if rc.Command == "" {
		return fmt.Errorf("run_command: command is required")
	}

	// Validate working directory
	cwd := rc.Cwd
	if cwd != "" {
		validCwd, err := r.ValidatePath(cwd)
		if err != nil {
			return fmt.Errorf("run_command: invalid cwd: %w", err)
		}
		cwd = validCwd
	}

	// ── Persistent terminal mode ──
	if rc.Persistent {
		if r.taskMgr == nil {
			return fmt.Errorf("run_command: task manager not available for persistent mode")
		}

		termID, stdout, exitCode, err := r.taskMgr.RunInTerminal(
			ctx, rc.Command, cwd, rc.TerminalId, rc.Env, int(rc.TimeoutMs),
		)
		if err != nil {
			return fmt.Errorf("run_command: persistent: %w", err)
		}

		rc.Stdout = truncateOutput(stdout, 100000)
		rc.ExitCode = int32(exitCode)
		rc.AssignedTerminalId = termID
		return nil
	}

	// ── Background mode ──
	if rc.Background {
		if r.taskMgr == nil {
			return fmt.Errorf("run_command: task manager not available for background mode")
		}

		taskID, stdout, err := r.taskMgr.StartBackground(
			ctx, rc.Command, cwd, rc.Env, int(rc.WaitMsBeforeAsync),
		)
		if err != nil {
			return fmt.Errorf("run_command: background: %w", err)
		}

		rc.TaskId = taskID
		rc.Stdout = truncateOutput(stdout, 100000)
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

	// Build environment: inherit current env + add PAGER=cat + user env
	env := cmd.Environ()
	env = append(env, "PAGER=cat")

	// Ensure GIT_TERMINAL_PROMPT=0 to prevent interactive git prompts
	env = append(env, "GIT_TERMINAL_PROMPT=0")

	// Add user-specified env vars
	for k, v := range rc.Env {
		// Sanitize: no newlines in env vars
		k = strings.ReplaceAll(k, "\n", "")
		v = strings.ReplaceAll(v, "\n", "")
		env = append(env, k+"="+v)
	}
	cmd.Env = env

	// Capture stdout and stderr separately
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// Populate results
	rc.Stdout = truncateOutput(stdout.String(), 100000)
	rc.Stderr = truncateOutput(stderr.String(), 50000)

	if cmdCtx.Err() == context.DeadlineExceeded {
		rc.TimedOut = true
		rc.ExitCode = -1
		return nil // Timeout is not a tool error, it's a result
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			rc.ExitCode = int32(exitErr.ExitCode())
		} else {
			return fmt.Errorf("run_command: %w", err)
		}
	} else {
		rc.ExitCode = 0
	}

	return nil
}

// truncateOutput caps output length to prevent huge payloads.
func truncateOutput(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + fmt.Sprintf("\n\n... [output truncated, showing %d/%d bytes]", maxLen, len(s))
}
