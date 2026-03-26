//go:build !darwin

package remote

// CheckScreenRecordingPermission always returns true on non-macOS platforms.
func CheckScreenRecordingPermission() bool {
	return true
}

// IsScreenRecordingStale always returns false on non-macOS platforms.
func IsScreenRecordingStale() bool {
	return false
}

// IsMacOS26OrLater always returns false on non-macOS platforms.
func IsMacOS26OrLater() bool {
	return false
}
