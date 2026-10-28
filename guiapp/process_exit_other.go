//go:build !windows

package guiapp

import "os"

// forceTerminateProcess ends the process. Unix signal-based shutdown and
// Darwin's Cocoa teardown have no equivalent of Windows' loader-lock hang, so
// the regular runtime exit is sufficient.
func forceTerminateProcess(code int) {
	os.Exit(code)
}
