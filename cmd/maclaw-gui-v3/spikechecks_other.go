//go:build !windows

package main

import (
	"fmt"
	"runtime"
)

// Non-Windows stubs keep the spike cross-compilable; the checkpoints target
// the real Windows machine first.

func spikeWinChecks(hwnd uintptr) []string {
	return []string{fmt.Sprintf("os=%s (non-windows stub)", runtime.GOOS)}
}

func spikeGuardedDragBegin(hwnd uintptr) string {
	return "unsupported on this platform"
}

func spikeOSBuild() string {
	return fmt.Sprintf("os=%s (non-windows stub)", runtime.GOOS)
}
