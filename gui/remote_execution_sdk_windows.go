//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// hideCommandWindow hides the process being created. Grandchildren still need
// a hidden host console (ensureHiddenHostConsole) or they will flash.
func hideCommandWindow(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= _CREATE_NO_WINDOW
}
