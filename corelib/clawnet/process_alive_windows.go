//go:build windows

package clawnet

import (
	"golang.org/x/sys/windows"
)

// isProcessAlive checks whether a process with the given PID is still running
// on Windows by attempting to open a handle with limited query rights.
func isProcessAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)

	var exitCode uint32
	if err := windows.GetExitCodeProcess(h, &exitCode); err != nil {
		return false
	}
	// STILL_ACTIVE (259) means the process has not exited yet.
	return exitCode == 259
}
