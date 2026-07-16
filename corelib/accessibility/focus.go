package accessibility

// FocusWindow brings a top-level window whose title contains titleSubstring
// to the foreground. Platform implementations live in focus_*.go.
// Returns nil when unsupported or when no matching window is found (best-effort).
func FocusWindow(titleSubstring string) error {
	return focusWindow(titleSubstring)
}
