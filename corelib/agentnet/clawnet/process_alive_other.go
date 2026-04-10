//go:build !windows

package clawnet

import (
	"os"
	"syscall"
)

// isProcessAlive checks whether a process with the given PID is still running
// on Unix-like systems by sending signal 0.
func isProcessAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
