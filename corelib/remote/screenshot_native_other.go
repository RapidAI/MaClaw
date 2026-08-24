//go:build !windows

package remote

import "fmt"

// NativeScreenshot is not available on non-Windows platforms.
// Falls back to the command-line approach.
func NativeScreenshot() (string, error) {
	return "", fmt.Errorf("native screenshot not supported on this platform")
}

// NativeScreenshotRect is not available on non-Windows platforms.
func NativeScreenshotRect(x, y, w, h int) (string, error) {
	return "", fmt.Errorf("native screenshot not supported on this platform")
}

// NativeScreenshotVirtual is not available on non-Windows platforms.
func NativeScreenshotVirtual() (string, error) {
	return "", fmt.Errorf("native screenshot not supported on this platform")
}
