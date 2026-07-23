//go:build !windows

package main

// This policy addresses the Windows 10 frameless-window clipping path. Other
// platforms retain their existing adaptive sizing behavior.
func shouldMaximiseMainWindowForPrimaryScreen() bool {
	return false
}
