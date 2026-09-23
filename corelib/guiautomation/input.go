package guiautomation

import "fmt"

// InputSimulator provides cross-platform input event simulation.
type InputSimulator interface {
	// Click performs a left mouse click at the given screen coordinates.
	Click(x, y int) error
	// RightClick performs a right mouse click at the given screen coordinates.
	RightClick(x, y int) error
	// DoubleClick performs a double left-click at the given screen coordinates.
	DoubleClick(x, y int) error
	// Type simulates typing the given text string character by character.
	Type(text string) error
	// KeyCombo simulates a keyboard shortcut, e.g. KeyCombo("ctrl", "c").
	KeyCombo(keys ...string) error
	// Scroll simulates mouse wheel scrolling at the given screen coordinates.
	Scroll(x, y, deltaX, deltaY int) error
	// DragDrop simulates a mouse drag from (fromX, fromY) to (toX, toY).
	DragDrop(fromX, fromY, toX, toY int) error
}

// ErrUnsupportedPlatform is returned when no input backend exists so callers
// do not treat a missing simulator as a successful click or type.
var ErrUnsupportedPlatform = fmt.Errorf("input simulation not supported on this platform")

// clampToVirtual keeps a click inside the virtual desktop.
// Origins may be negative when a monitor sits left of or above the primary.
func clampToVirtual(x, y, originX, originY, width, height int) (int, int) {
	if width <= 0 || height <= 0 {
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		return x, y
	}
	if x < originX {
		x = originX
	}
	if y < originY {
		y = originY
	}
	if x >= originX+width {
		x = originX + width - 1
	}
	if y >= originY+height {
		y = originY + height - 1
	}
	return x, y
}

// typeUsesClipboard reports whether text should be pasted instead of injected
// as Unicode keystrokes. Chinese IM, Office, and Electron apps often drop
// KEYEVENTF_UNICODE, while Ctrl+V still lands in the focused control.
func typeUsesClipboard(text string) bool {
	for _, r := range text {
		if r > 127 {
			return true
		}
	}
	return false
}

// noopInputSimulator is a no-op fallback for unsupported platforms.
type noopInputSimulator struct{}

func (n *noopInputSimulator) Click(x, y int) error {
	return ErrUnsupportedPlatform
}
func (n *noopInputSimulator) RightClick(x, y int) error {
	return ErrUnsupportedPlatform
}
func (n *noopInputSimulator) DoubleClick(x, y int) error {
	return ErrUnsupportedPlatform
}
func (n *noopInputSimulator) Type(text string) error {
	return ErrUnsupportedPlatform
}
func (n *noopInputSimulator) KeyCombo(keys ...string) error {
	return ErrUnsupportedPlatform
}
func (n *noopInputSimulator) Scroll(x, y, deltaX, deltaY int) error {
	return ErrUnsupportedPlatform
}
func (n *noopInputSimulator) DragDrop(fromX, fromY, toX, toY int) error {
	return ErrUnsupportedPlatform
}
