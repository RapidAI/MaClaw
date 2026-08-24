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
