//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// shutdownSignals covers the console control events Go maps to os.Interrupt;
// SIGTERM does not exist on Windows.
func shutdownSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

func freeBytesPlatform(path string) (uint64, error) {
	root := filepath.VolumeName(path)
	if root == "" {
		root = path
	}
	if len(root) == 2 && root[1] == ':' {
		root += `\`
	}
	var available, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(windows.StringToUTF16Ptr(root), &available, &total, &free); err != nil {
		return 0, fmt.Errorf("free space %s: %w", root, err)
	}
	return available, nil
}
