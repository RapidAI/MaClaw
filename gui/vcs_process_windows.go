//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// hideVCSCommandWindow keeps Git and SVN command-line work inside the app.
// Git for Windows otherwise creates a transient console for each clone, status,
// or checkout even though stdout and stderr are captured by this process.
func hideVCSCommandWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: _CREATE_NO_WINDOW}
}
