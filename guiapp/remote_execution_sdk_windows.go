//go:build windows

package guiapp

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

// applyExplorerSelect sets the exact command line Explorer needs to select a file.
func applyExplorerSelect(cmd *exec.Cmd, explorer, path string) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CmdLine = explorerSelectCommandLine(explorer, path)
}
