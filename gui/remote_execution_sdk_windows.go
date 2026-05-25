//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// hideCommandWindow prevents a visible console window from appearing when the
// process is started on Windows. Uses CREATE_NO_WINDOW (0x08000000) which
// suppresses console creation for the entire process tree, including child
// cmd.exe interpreters spawned by .cmd/.bat scripts (e.g. npm's FOR /F).
func hideCommandWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: _CREATE_NO_WINDOW,
	}
}
