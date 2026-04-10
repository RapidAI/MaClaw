package clawnet

import (
	"os/exec"
	"syscall"
)

// hideCommandWindow prevents a visible console window on Windows.
func hideCommandWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
