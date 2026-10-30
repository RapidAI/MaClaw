//go:build windows

package guiapp

import (
	"log"
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
	// A real handle, then the pseudo-handle. Either call usually does not
	// return. If we are still here, the icon is already gone and this
	// process is still alive, so ExitProcess has to run. Parking instead
	// holds the single-exit gate and leaves that process up.
	err := terminateCurrentProcess(code)
	if err != nil {
		if self, gerr := windows.GetCurrentProcess(); gerr == nil {
			err = windows.TerminateProcess(self, uint32(code))
		}
	}
	// TerminateProcess on this process usually does not come back. When it
	// does, the icon is already gone and the process is still here: a parked
	// goroutine would hold the single-exit gate and never reach ExitProcess.
	if err != nil {
		log.Printf("[exit-lifecycle] TerminateProcess refused: %v; falling back to os.Exit", err)
	} else {
		log.Printf("[exit-lifecycle] TerminateProcess returned; falling back to os.Exit")
	}
	os.Exit(code)
}

func terminateCurrentProcess(code int) error {
	pid := uint32(os.Getpid())
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	return windows.TerminateProcess(handle, uint32(code))
}
