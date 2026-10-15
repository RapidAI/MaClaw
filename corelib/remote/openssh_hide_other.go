//go:build !windows

package remote

import "os/exec"

func hideCommandWindow(cmd *exec.Cmd) {}
