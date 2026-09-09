//go:build !windows

package guiapp

import "os/exec"

// hideCommandWindow is a no-op on non-Windows platforms.
func hideCommandWindow(_ *exec.Cmd) {}
