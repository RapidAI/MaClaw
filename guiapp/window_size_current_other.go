//go:build !windows

package guiapp

// adaptiveWindowSizeForCurrentWindow keeps non-Windows platforms on the
// full-display path, which applies their existing taskbar/dock reserve.
func adaptiveWindowSizeForCurrentWindow() (width, height int) {
	return adaptiveWindowSize()
}
