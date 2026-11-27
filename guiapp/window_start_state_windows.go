//go:build windows

package guiapp

func shouldMaximiseMainWindowForPrimaryScreen() bool {
	sw, sh := getPrimaryScreenSize()
	return shouldPreserveMaximisedWindowAfterEnvironmentCheck(sw, sh)
}

// shouldMaximiseMainWindow reports whether the available logical desktop is
// too constrained for the main workbench to remain a comfortably resizable
// window. Maximising keeps the taskbar and custom title-bar controls
// available while ensuring no edge of the application is outside the usable
// desktop.
func shouldMaximiseMainWindow(sw, sh int) bool {
	return sw > 0 && sh > 0 && (sw <= 1366 || sh <= 768)
}

// shouldPreserveMaximisedWindowAfterEnvironmentCheck keeps startup and the
// post-check resize path aligned. It is kept separate so the policy can be
// exercised without depending on the Wails runtime.
func shouldPreserveMaximisedWindowAfterEnvironmentCheck(sw, sh int) bool {
	return shouldMaximiseMainWindow(sw, sh)
}
