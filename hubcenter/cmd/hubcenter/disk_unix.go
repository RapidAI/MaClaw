//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// shutdownSignals lists the signals that stop the server gracefully.
// SIGTERM is absent on Windows.
func shutdownSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

func freeBytesPlatform(path string) (uint64, error) {
	dir := path
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		dir = filepath.Dir(path)
		if dir == "" {
			dir = "."
		}
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", dir, err)
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}
