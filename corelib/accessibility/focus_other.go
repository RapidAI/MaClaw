//go:build !windows && !darwin && !linux

package accessibility

import "fmt"

func focusWindow(titleSubstring string) error {
	return fmt.Errorf("FocusWindow not supported on this platform")
}

func foregroundWindowTitle() string {
	return ""
}

func windowTitleAtPoint(x, y int) string {
	return ""
}

func foregroundWindowBounds() (WindowBounds, bool) {
	return WindowBounds{}, false
}

func namedWindowBounds(titleSubstring string) (WindowBounds, bool) {
	return WindowBounds{}, false
}
