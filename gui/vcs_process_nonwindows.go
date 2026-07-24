//go:build !windows

package main

import "os/exec"

func hideVCSCommandWindow(_ *exec.Cmd) {}
