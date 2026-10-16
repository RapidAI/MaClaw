//go:build !windows

package guiapp

import "os/exec"

func hideACPLaunchWindow(cmd *exec.Cmd) {}
