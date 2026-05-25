//go:build !windows

package agent

import (
	"os/exec"
	"syscall"
)

func hideCommandWindowImpl(_ *exec.Cmd) {}

func prepareCommandForTreeKill(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateCommandTreeImpl(cmd *exec.Cmd) {
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
