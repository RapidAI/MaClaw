package main

import (
	"os/exec"
	"syscall"
)

// hideConsoleWindow prevents a console window from appearing when running
// external commands on Windows.
func hideConsoleWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
