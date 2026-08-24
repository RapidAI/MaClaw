package accessibility

import (
	"strconv"
	"strings"
)

// FocusWindow brings a top-level window whose title contains titleSubstring
// to the foreground. Platform implementations live in focus_*.go.
// Returns nil when unsupported or when no matching window is found (best-effort).
func FocusWindow(titleSubstring string) error {
	return focusWindow(titleSubstring)
}

// ForegroundWindowTitle returns the title of the current foreground window,
// or "" when unavailable/unsupported. Best-effort; used for click policy checks.
func ForegroundWindowTitle() string {
	return foregroundWindowTitle()
}

// WindowTitleAtPoint returns the root window title owning screen point (x,y),
// or "" when unavailable. More precise than ForegroundWindowTitle for click
// policy: the clicked window is not always the foreground one.
func WindowTitleAtPoint(x, y int) string {
	return windowTitleAtPoint(x, y)
}

// WindowBounds is a top-level window rectangle in virtual-desktop coordinates.
type WindowBounds struct {
	X, Y, Width, Height int
	Title               string
}

// ForegroundWindowBounds returns the focused top-level window rectangle.
func ForegroundWindowBounds() (WindowBounds, bool) {
	return foregroundWindowBounds()
}

// NamedWindowBounds returns the first visible top-level window whose title
// contains titleSubstring (case-insensitive).
func NamedWindowBounds(titleSubstring string) (WindowBounds, bool) {
	return namedWindowBounds(titleSubstring)
}

func parseCSVWindowBounds(s string) (WindowBounds, bool) {
	parts := strings.SplitN(strings.TrimSpace(s), ",", 5)
	if len(parts) < 4 {
		return WindowBounds{}, false
	}
	x, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	y, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	w, err3 := strconv.Atoi(strings.TrimSpace(parts[2]))
	h, err4 := strconv.Atoi(strings.TrimSpace(parts[3]))
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || w < 64 || h < 64 {
		return WindowBounds{}, false
	}
	title := ""
	if len(parts) >= 5 {
		title = strings.TrimSpace(parts[4])
	}
	return WindowBounds{X: x, Y: y, Width: w, Height: h, Title: title}, true
}
