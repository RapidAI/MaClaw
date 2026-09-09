//go:build !windows

package guiapp

import "fmt"

// nativeScreenshot is not available on non-Windows platforms.
// Falls back to the command-line approach.
func nativeScreenshot() (string, error) {
	return "", fmt.Errorf("native screenshot not supported on this platform")
}
