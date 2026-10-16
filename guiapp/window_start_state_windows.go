//go:build windows

package guiapp

func shouldMaximiseMainWindowForPrimaryScreen() bool {
	sw, sh := getPrimaryScreenSize()
	return shouldPreserveMaximisedWindowAfterEnvironmentCheck(sw, sh)
}
