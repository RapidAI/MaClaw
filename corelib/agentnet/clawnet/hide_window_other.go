//go:build !windows

package clawnet

import "os/exec"

// hideCommandWindow is a no-op on non-Windows platforms.
func hideCommandWindow(_ *exec.Cmd) {}
