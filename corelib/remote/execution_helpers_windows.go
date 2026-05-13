//go:build windows

package remote

import (
	"os/exec"
	"syscall"
)

// HideCommandWindow sets SysProcAttr to prevent a visible console window
// from appearing when the process is started on Windows.
// Uses CREATE_NO_WINDOW to suppress console creation for the entire process tree.
func HideCommandWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
}
