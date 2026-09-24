//go:build windows

package tools

import (
	"os/exec"

	"github.com/divmora/localharness/internal/util"
)

func setProcessGroup(cmd *exec.Cmd) {
	util.SetProcessGroup(cmd)
}

func interruptProcessGroup(pid int) error {
	return util.InterruptProcessGroup(pid)
}

func killProcessGroup(pid int) error {
	return util.KillProcessGroup(pid)
}

func terminateProcessGroup(pid int) error {
	return util.TerminateProcessGroup(pid)
}
