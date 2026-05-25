//go:build windows

package agent

import (
	"fmt"
	"os/exec"
	"syscall"
)

func hideCommandWindowImpl(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
}

func prepareCommandForTreeKill(_ *exec.Cmd) {}

func terminateCommandTreeImpl(cmd *exec.Cmd) {
	killer := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", cmd.Process.Pid))
	HideCommandWindow(killer)
	_ = killer.Run()
}
