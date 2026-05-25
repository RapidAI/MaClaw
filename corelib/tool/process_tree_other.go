//go:build !windows

package tool

import (
	"os/exec"
	"syscall"
)

func prepareCommandForTreeKill(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func terminateCommandTree(cmd *exec.Cmd) {
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
