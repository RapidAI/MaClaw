//go:build windows

package agent

import (
	"os/exec"
	"syscall"
)

func hideCommandWindowImpl(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
