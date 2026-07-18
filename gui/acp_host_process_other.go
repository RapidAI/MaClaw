//go:build !windows

package main

import (
	"os"
	"syscall"
)

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 checks existence without killing.
	err = p.Signal(syscall.Signal(0))
	return err == nil
}
