//go:build darwin && !cgo

package remote

// CheckScreenRecordingPermission returns true when CGo is disabled because
// the TCC API requires CoreGraphics via CGo. The TUI does not take
// screenshots itself, so this is safe.
func CheckScreenRecordingPermission() bool {
	return true
}
