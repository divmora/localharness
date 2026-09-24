//go:build windows

package util

import (
	"os"
	"os/exec"
)

// SetProcessGroup is a no-op on Windows.
func SetProcessGroup(cmd *exec.Cmd) {
}

// InterruptProcessGroup interrupts process on Windows.
func InterruptProcessGroup(pid int) error {
	return TerminateProcessGroup(pid)
}

// KillProcessGroup kills process on Windows.
func KillProcessGroup(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

// TerminateProcessGroup terminates process on Windows.
func TerminateProcessGroup(pid int) error {
	return KillProcessGroup(pid)
}
