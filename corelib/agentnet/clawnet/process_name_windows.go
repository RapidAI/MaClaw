//go:build windows

package clawnet

import (
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// findProcessByName returns the PID of a running process whose executable
// name matches the given name (case-insensitive). Returns 0 if not found.
// It skips the current process so the caller never matches itself.
func findProcessByName(name string) int {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(snap)

	self := uint32(os.Getpid())
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))

	if err := windows.Process32First(snap, &entry); err != nil {
		return 0
	}
	for {
		exeName := windows.UTF16ToString(entry.ExeFile[:])
		if entry.ProcessID != self && strings.EqualFold(exeName, name) {
			return int(entry.ProcessID)
		}
		if err := windows.Process32Next(snap, &entry); err != nil {
			break
		}
	}
	return 0
}
