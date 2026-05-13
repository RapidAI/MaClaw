//go:build windows

package tool

import (
	"context"
	"os/exec"
	"syscall"
)

// HideCommandWindow prevents a visible console window from appearing
// when the process is started on Windows.
func HideCommandWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

// Command creates an exec.Cmd with the console window hidden on Windows.
// Use this instead of exec.Command to avoid forgetting HideCommandWindow.
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

// CommandContext creates an exec.Cmd with context and the console window
// hidden on Windows.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}
