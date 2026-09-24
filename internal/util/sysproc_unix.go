//go:build !windows

package util

import (
	"os/exec"
	"syscall"
)

// SetProcessGroup sets PGID so the command and all its children belong to a separate process group.
func SetProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// InterruptProcessGroup sends SIGINT to the entire process group.
func InterruptProcessGroup(pid int) error {
	return syscall.Kill(-pid, syscall.SIGINT)
}

// KillProcessGroup sends SIGKILL to the entire process group.
func KillProcessGroup(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}

// TerminateProcessGroup sends SIGTERM to the entire process group.
func TerminateProcessGroup(pid int) error {
	return syscall.Kill(-pid, syscall.SIGTERM)
}
