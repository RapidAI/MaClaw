//go:build windows

package main

import (
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	processElevatedOnce sync.Once
	processElevated     bool
)

// isProcessElevated returns true if the current process is running with
// administrator (elevated) privileges on Windows. The result is cached
// because elevation status cannot change during the process lifetime.
func isProcessElevated() bool {
	processElevatedOnce.Do(func() {
		processElevated = checkProcessElevated()
	})
	return processElevated
}

func checkProcessElevated() bool {
	var token windows.Token
	proc := windows.CurrentProcess()
	err := windows.OpenProcessToken(proc, windows.TOKEN_QUERY, &token)
	if err != nil {
		return false
	}
	defer token.Close()

	var elevation struct {
		TokenIsElevated uint32
	}
	var retLen uint32
	err = windows.GetTokenInformation(
		token,
		windows.TokenElevation,
		(*byte)(unsafe.Pointer(&elevation)),
		uint32(unsafe.Sizeof(elevation)),
		&retLen,
	)
	if err != nil {
		return false
	}
	return elevation.TokenIsElevated != 0
}
