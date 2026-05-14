//go:build windows

package main

import (
	"os"
	"syscall"
)

const attachParentProcess = ^uintptr(0)

func prepareConsoleForMode(mode string) {
	if windowsGUI != "true" || (mode != "tui" && mode != "cli") {
		return
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	attachConsole := kernel32.NewProc("AttachConsole")
	attachConsole.Call(attachParentProcess)
	if out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout = out
		os.Stderr = out
	}
	if in, err := os.OpenFile("CONIN$", os.O_RDONLY, 0); err == nil {
		os.Stdin = in
	}
}
