//go:build windows

package guiapp

import (
	"os"

	"golang.org/x/sys/windows"
)

// forceTerminateProcess ends the process without running DLL_PROCESS_DETACH
// handlers. os.Exit (ExitProcess) waits on the loader lock and can hang when
// a third-party hooking DLL (e.g. OBS graphics-hook, screen recorders) is
// injected into this process, which made "Quit" leave a window-less process.
// TerminateProcess skips detach handlers entirely and always succeeds on the
// current process.
func forceTerminateProcess(code int) {
	self, err := windows.GetCurrentProcess()
	if err == nil {
		_ = windows.TerminateProcess(self, uint32(code))
	}
	// TerminateProcess on the current process does not return on success.
	// Fall back to the regular runtime exit if it was refused.
	os.Exit(code)
}
