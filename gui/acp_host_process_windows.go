//go:build windows

package main

import (
	"os"
	"syscall"
)

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	const stillActive = 259
	h, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		// Fallback: signal 0 via FindProcess
		p, err := os.FindProcess(pid)
		if err != nil {
			return false
		}
		// On Windows FindProcess always succeeds; OpenProcess is the real check.
		_ = p
		return false
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}
