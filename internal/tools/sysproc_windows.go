//go:build windows

package tools

import (
	"os"
	"os/exec"
)

func setProcessGroup(cmd *exec.Cmd) {
	// Not supported in the same way on Windows.
	// We could use CREATE_NEW_PROCESS_GROUP via SysProcAttr on Windows, but standard process killing works fine for now.
}

func killProcessGroup(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

func terminateProcessGroup(pid int) error {
	// Windows doesn't have SIGTERM cleanly, just use Kill
	return killProcessGroup(pid)
}
