//go:build darwin && !cgo

package remote

// CheckScreenRecordingPermission returns true when CGo is disabled because
// the TCC API requires CoreGraphics via CGo. The TUI does not take
// screenshots itself, so this is safe.
func CheckScreenRecordingPermission() bool {
	return true
}

// IsScreenRecordingStale always returns false when CGo is disabled.
func IsScreenRecordingStale() bool {
	return false
}

// IsMacOS26OrLater always returns false when CGo is disabled.
func IsMacOS26OrLater() bool {
	return false
}
