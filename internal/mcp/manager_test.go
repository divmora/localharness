package mcp

import (
	"context"
	"log/slog"
	"os/exec"
	"testing"
	"time"

	"github.com/divmora/localharness/internal/util"
)

func TestManagerCloseKillsProcessGroup(t *testing.T) {
	mgr := NewManager(slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Spawn a long-running sleep command simulating a stdio MCP server
	cmd := exec.CommandContext(ctx, "sleep", "30")
	util.SetProcessGroup(cmd)
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return util.KillProcessGroup(cmd.Process.Pid)
		}
		return nil
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start sleep process: %v", err)
	}

	pid := cmd.Process.Pid

	mgr.mu.Lock()
	mgr.sessions["test-sleep"] = &serverSession{
		name:   "test-sleep",
		cancel: cancel,
		cmd:    cmd,
	}
	mgr.mu.Unlock()

	// Close manager and verify the process is terminated
	mgr.Close()

	// Wait up to 1 second for the process to exit
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-done:
		// Process exited successfully
	case <-time.After(1 * time.Second):
		_ = util.KillProcessGroup(pid)
		t.Fatalf("process %d was not terminated by Manager.Close()", pid)
	}
}
