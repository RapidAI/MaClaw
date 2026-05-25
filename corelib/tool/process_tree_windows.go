//go:build windows

package tool

import (
	"fmt"
	"os/exec"
	"syscall"
)

func prepareCommandForTreeKill(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= _CREATE_NO_WINDOW
}

func terminateCommandTree(cmd *exec.Cmd) {
	killer := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", cmd.Process.Pid))
	HideCommandWindow(killer)
	_ = killer.Run()
}
