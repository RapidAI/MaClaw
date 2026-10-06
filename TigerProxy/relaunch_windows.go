//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func startRelaunchProcess(exe string) error {
	// Wait until this PID is gone so the new process does not hit the
	// single-instance lock and exit immediately.
	script := fmt.Sprintf(
		`Start-Sleep -Seconds 1; $deadline = (Get-Date).AddSeconds(25); while ((Get-Date) -lt $deadline) { if (-not (Get-Process -Id %d -ErrorAction SilentlyContinue)) { break }; Start-Sleep -Milliseconds 300 }; Start-Process -FilePath %s`,
		os.Getpid(),
		powershellSingleQuote(exe),
	)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-WindowStyle", "Hidden", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
		CreationFlags: 0x00000200 | // CREATE_NEW_PROCESS_GROUP
			0x00000008 | // DETACHED_PROCESS
			0x01000000, // CREATE_BREAKAWAY_FROM_JOB
	}
	return cmd.Start()
}

func powershellSingleQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "''") + "'"
}
