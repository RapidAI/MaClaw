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

func foregroundWindowID() uint64 { return 0 }

func windowIDAtPoint(x, y int) uint64 { return 0 }

func foregroundWindowBounds() (WindowBounds, bool) {
	return WindowBounds{}, false
}

func namedWindowBounds(titleSubstring string) (WindowBounds, bool) {
	return WindowBounds{}, false
}

func namedWindowID(titleSubstring string) uint64 { return 0 }

func windowOwnedBySelf(id uint64) bool { return false }
