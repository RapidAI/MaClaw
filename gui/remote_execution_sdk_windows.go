//go:build windows

package main

import (
	"fmt"
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

func prepareCommandForTreeKill(_ *exec.Cmd) {}

func terminateCommandTreeImpl(cmd *exec.Cmd) {
	killer := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", cmd.Process.Pid))
	hideCommandWindow(killer)
	_ = killer.Run()
}
