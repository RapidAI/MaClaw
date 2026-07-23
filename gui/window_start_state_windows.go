//go:build windows

package main

func shouldMaximiseMainWindowForPrimaryScreen() bool {
	sw, sh := getPrimaryScreenSize()
	return shouldPreserveMaximisedWindowAfterEnvironmentCheck(sw, sh)
}
