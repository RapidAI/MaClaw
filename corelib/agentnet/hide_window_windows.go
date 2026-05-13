package agentnet

import (
	"os/exec"
	"syscall"
)

// hideCommandWindow prevents a visible console window on Windows.
// Uses CREATE_NO_WINDOW to suppress console creation for the entire process tree.
func hideCommandWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
}
