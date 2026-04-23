//go:build !windows

package agent

import "os/exec"

func hideCommandWindowImpl(_ *exec.Cmd) {}
