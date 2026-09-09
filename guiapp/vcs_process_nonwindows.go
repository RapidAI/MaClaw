//go:build !windows

package guiapp

import "os/exec"

func hideVCSCommandWindow(_ *exec.Cmd) {}
