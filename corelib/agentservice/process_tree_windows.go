//go:build windows

package agentservice

import (
	"fmt"
	"os/exec"
	"syscall"
)

func prepareCommandForTreeKill(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
}

func terminateCommandTreeImpl(cmd *exec.Cmd) {
	killer := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", cmd.Process.Pid))
	killer.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	_ = killer.Run()
}
