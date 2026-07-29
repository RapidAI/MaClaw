//go:build !windows

package main

import "os/exec"

func hideACPLaunchWindow(cmd *exec.Cmd) {}
