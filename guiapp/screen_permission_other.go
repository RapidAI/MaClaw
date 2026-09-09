//go:build !darwin

package guiapp

// HasScreenRecordingPermission always returns true on non-macOS platforms.
func HasScreenRecordingPermission() bool {
	return true
}

// RequestScreenRecordingPermission is a no-op on non-macOS platforms.
func RequestScreenRecordingPermission() {}

// EnsureScreenRecordingPermission always returns true on non-macOS platforms.
func EnsureScreenRecordingPermission() bool {
	return true
}
