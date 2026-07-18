//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// hideACPLaunchWindow suppresses console flash for code.cmd / install-extension
// and other Windows console-subsystem children spawned from Launch VS Code.
func hideACPLaunchWindow(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: _CREATE_NO_WINDOW}
}