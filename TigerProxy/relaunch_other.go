//go:build !windows

package main

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

func startRelaunchProcess(exe string) error {
	script := "pid=" + strconv.Itoa(os.Getpid()) + `; i=0; while kill -0 "$pid" 2>/dev/null && [ "$i" -lt 50 ]; do sleep 0.3; i=$((i+1)); done; exec ` + strconv.Quote(exe)
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Env = os.Environ()
	return cmd.Start()
}
