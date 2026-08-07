package accessibility

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
