//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// hideCommandWindow sets SysProcAttr to prevent a visible console window
// from appearing when the process is started on Windows.
func hideCommandWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
