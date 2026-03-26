//go:build !darwin

package main

import "fmt"

// nativeCaptureScreenshot is not available on non-macOS platforms.
// Callers should fall back to the shell command approach.
func nativeCaptureScreenshot() (string, error) {
	return "", fmt.Errorf("native screenshot capture is only available on macOS")
}
