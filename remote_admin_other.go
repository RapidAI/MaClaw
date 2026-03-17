//go:build !windows

package main

import (
	"os"
	"sync"
)

var (
	processElevatedOnce sync.Once
	processElevated     bool
)

// isProcessElevated returns true if the current process is running as
// root (uid 0) on Unix-like systems. The result is cached because
// effective UID does not change during normal operation.
func isProcessElevated() bool {
	processElevatedOnce.Do(func() {
		processElevated = os.Getuid() == 0
	})
	return processElevated
}
