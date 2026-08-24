package accessibility

import "fmt"

// Rect represents a bounding rectangle in screen coordinates.
type Rect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Element represents a UI control in the accessibility tree.
type Element struct {
	Role         string    `json:"role"`               // button, textfield, checkbox, etc.
	Name         string    `json:"name"`               // accessible name
	Value        string    `json:"value"`              // current value
	Bounds       Rect      `json:"bounds"`             // screen coordinates
	Children     []Element `json:"children,omitempty"` // child elements
	Handle       uintptr   `json:"-"`                  // platform-specific handle
	AutomationID string    `json:"automation_id,omitempty"`
	Patterns     []string  `json:"patterns,omitempty"`
}

// ErrUnsupportedPlatform is returned by no-op bridges so callers do not treat
// a missing backend as a successful click or type.
var ErrUnsupportedPlatform = fmt.Errorf("accessibility backend not supported on this platform")

// Bridge provides cross-platform accessibility access.
type Bridge interface {
	// EnumElements returns the accessibility tree for the given window.
	EnumElements(windowTitle string) ([]Element, error)
	// FindElement searches for an element by role and name.
	FindElement(windowTitle, role, name string) (*Element, error)
	// ClickElement performs a click on the element.
	ClickElement(el *Element) error
	// TypeInElement types text into the element.
	TypeInElement(el *Element, text string) error
	// SelectElement selects a list/tab/tree item.
	SelectElement(el *Element) error
	// ExpandElement expands or collapses a disclosure control.
	ExpandElement(el *Element) error
	// ScrollElementIntoView scrolls a container so the element is visible.
	ScrollElementIntoView(el *Element) error
	// FocusElement moves accessibility focus to the element.
	FocusElement(el *Element) error
	// GetValue returns the current value of the element.
	GetValue(el *Element) (string, error)
	// Close releases resources.
	Close()
}

// noopBridge is a no-op implementation that returns empty results.
type noopBridge struct{}

func (b *noopBridge) EnumElements(windowTitle string) ([]Element, error) {
	return nil, nil
}

func (b *noopBridge) FindElement(windowTitle, role, name string) (*Element, error) {
	return nil, nil
}

func (b *noopBridge) ClickElement(el *Element) error {
	if el == nil {
		return nil
	}
	return ErrUnsupportedPlatform
}

func (b *noopBridge) TypeInElement(el *Element, text string) error {
	if el == nil {
		return nil
	}
	return ErrUnsupportedPlatform
}

func (b *noopBridge) SelectElement(el *Element) error {
	if el == nil {
		return nil
	}
	return ErrUnsupportedPlatform
}

func (b *noopBridge) ExpandElement(el *Element) error {
	if el == nil {
		return nil
	}
	return ErrUnsupportedPlatform
}

func (b *noopBridge) ScrollElementIntoView(el *Element) error {
	if el == nil {
		return nil
	}
	return ErrUnsupportedPlatform
}

func (b *noopBridge) FocusElement(el *Element) error {
	if el == nil {
		return nil
	}
	return ErrUnsupportedPlatform
}

func (b *noopBridge) GetValue(el *Element) (string, error) {
	return "", nil
}

func (b *noopBridge) Close() {}

func elementCenter(el *Element) (int, int) {
	if el == nil {
		return 0, 0
	}
	return el.Bounds.X + el.Bounds.Width/2, el.Bounds.Y + el.Bounds.Height/2
}
