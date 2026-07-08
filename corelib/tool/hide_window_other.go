//go:build !windows

package tool

import (
	"context"
	"os/exec"
)

// HideCommandWindow is a no-op on non-Windows platforms.
func HideCommandWindow(_ *exec.Cmd) {}

// ResolveCmdExe returns "cmd" on non-Windows platforms (no-op fallback).
func ResolveCmdExe() string {
	return "cmd"
}

// Command creates an exec.Cmd. On Windows this hides the console window.
func Command(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

// CommandContext creates an exec.Cmd with context.
// On Windows this hides the console window.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}
