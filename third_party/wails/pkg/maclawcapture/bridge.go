// Package maclawcapture is the host WebView handle used by companion notes.
// The desktop frontend binds the live view after it is created. Tests leave
// it unset, so capture refuses without opening a sound device.
package maclawcapture

import "sync"

// View is the live Wails WebView. Pointer is the platform object
// (ICoreWebView2, WKWebView, or WebKitWebView). It is zero when unbound.
type View interface {
	Pointer() uintptr
	ContainsFullScreen() bool
}

// PointerView is a WebView that does not report Windows fullscreen.
// macOS and Linux use the page's fullscreenElement for that signal.
type PointerView struct {
	P uintptr
}

func (v PointerView) Pointer() uintptr { return v.P }

func (v PointerView) ContainsFullScreen() bool { return false }

var (
	mu       sync.RWMutex
	current  View
	uiRunner func(func())
)

// Bind stores the live view. A nil view clears it.
func Bind(v View) {
	mu.Lock()
	current = v
	mu.Unlock()
}

// Current returns the bound view, or nil when the window has not created one.
func Current() View {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// SetUIRunner posts work onto the WebView UI thread.
// CapturePreview and the GTK/AppKit snapshot must run there.
func SetUIRunner(fn func(func())) {
	mu.Lock()
	uiRunner = fn
	mu.Unlock()
}

// HasUIRunner reports whether a UI thread pump is registered.
func HasUIRunner() bool {
	mu.RLock()
	defer mu.RUnlock()
	return uiRunner != nil
}

// PostUI runs fn on the UI thread and waits until fn returns.
// It does not wait for an async completion the fn starts.
func PostUI(fn func()) bool {
	mu.RLock()
	run := uiRunner
	mu.RUnlock()
	if run == nil || fn == nil {
		return false
	}
	done := make(chan struct{})
	run(func() {
		defer close(done)
		fn()
	})
	<-done
	return true
}
