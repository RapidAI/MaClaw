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

// ForegroundWindowID returns the stable id of the foreground top-level window.
// Windows reports the HWND. Other platforms return 0, and callers then compare
// titles. The id survives a title change on that same window.
func ForegroundWindowID() uint64 {
	return foregroundWindowID()
}

// WindowIDAtPoint returns the stable id of the top-level window owning (x, y).
func WindowIDAtPoint(x, y int) uint64 {
	return windowIDAtPoint(x, y)
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

// NamedWindowBounds returns the best top-level application frame for
// titleSubstring. A hidden frame is used when no visible frame matches, so a
// tray app can still be named. Tool windows are not frames.
func NamedWindowBounds(titleSubstring string) (WindowBounds, bool) {
	return namedWindowBounds(titleSubstring)
}

// NamedWindowID returns the platform id of the best visible top-level window
// for titleSubstring. Windows returns the HWND. Other platforms return 0.
func NamedWindowID(titleSubstring string) uint64 {
	return namedWindowID(titleSubstring)
}

// WindowOwnedBySelf reports whether id belongs to this process.
// A zero id does not.
func WindowOwnedBySelf(id uint64) bool {
	return windowOwnedBySelf(id)
}

// WindowIsHung reports the OS's not-responding verdict for a top-level window.
// Unsupported platforms and a zero id return false.
func WindowIsHung(id uint64) bool {
	return windowIsHung(id)
}

// KeyboardTextTarget is the foreground thread's keyboard focus.
// Focus and Caret are platform window ids. On Windows they are HWNDs.
type KeyboardTextTarget struct {
	Foreground uint64
	Focus      uint64
	Caret      uint64
	FocusClass string
}

// AcceptsText reports whether typed characters will be inserted.
// A Win32 caret accepts text. A focused window accepts text when its class
// is not a shell that never inserts characters: the browser frame
// Chrome_WidgetWin (page text goes to Chrome_RenderWidgetHostHWND), a
// button, a static label, the desktop, or a menu. A toolkit that keeps
// keyboard focus on its own top-level window, such as Qt, still accepts
// text. No focus window and no caret does not.
func (t KeyboardTextTarget) AcceptsText() bool {
	if t.Caret != 0 {
		return true
	}
	if t.Focus == 0 {
		return false
	}
	return !focusClassRejectsText(t.FocusClass)
}

// focusClassRejectsText reports classes that never insert typed characters.
// An empty class is not a rejection: the class could not be read, and a
// focused window of an unknown toolkit still receives the keystrokes.
func focusClassRejectsText(class string) bool {
	switch strings.TrimSpace(class) {
	case "#32768", "#32769", "Button", "Static", "ToolbarWindow32", "Shell_TrayWnd", "Progman", "WorkerW":
		return true
	}
	return strings.HasPrefix(strings.TrimSpace(class), "Chrome_WidgetWin")
}

// ForegroundAcceptsText reports whether the foreground thread will insert
// typed characters. Windows reads that thread. Other platforms have no
// equivalent probe and return true so typing is not refused there.
func ForegroundAcceptsText() bool {
	return foregroundAcceptsText()
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
