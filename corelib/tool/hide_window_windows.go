//go:build windows

package tool

import (
	"context"
	"os/exec"
	"syscall"
)

// _CREATE_NO_WINDOW prevents the creation of a console window for the child
// process and all its descendants. Unlike HideWindow (STARTUPINFO.wShowWindow),
// this CreationFlag affects the entire process tree.
const _CREATE_NO_WINDOW = 0x08000000

// HideCommandWindow prevents a visible console window from appearing
// when the process is started on Windows.
func HideCommandWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: _CREATE_NO_WINDOW}
}

// Command creates an exec.Cmd with the console window hidden on Windows.
// Use this instead of exec.Command to avoid forgetting HideCommandWindow.
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: _CREATE_NO_WINDOW}
	return cmd
}

// CommandContext creates an exec.Cmd with context and the console window
// hidden on Windows.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: _CREATE_NO_WINDOW}
	return cmd
}
