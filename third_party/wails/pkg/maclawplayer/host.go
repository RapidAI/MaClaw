// Package maclawplayer is the companion's top-level page host.
// The playback document is its own WebView in the companion profile, so a
// sign-in cookie belongs to that site and is sent the next time the page is
// requested. A framed page under the app origin is a different top-level site
// and never receives that cookie.
package maclawplayer

import (
	"math"
	"net/url"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/maclawcapture"
)

// Bounds is the player rectangle in the same pixels the WebView controller uses.
type Bounds struct {
	X int32
	Y int32
	W int32
	H int32
}

// Placement is the player the host should be showing.
type Placement struct {
	URL     string
	Bounds  Bounds
	Visible bool
}

// Host paints one top-level page over the player slot.
type Host interface {
	Place(url string, bounds Bounds) error
	Hide()
	// Showing is true while the page should be on screen, including the moment
	// before its WebView exists. Capture must not photograph the companion
	// window during that moment.
	Showing() bool
	// View is the live page while it is showing. Capture uses it instead of
	// the app WebView, because the picture is this page and not a crop of the
	// companion window. It is nil while the player is hidden or not created yet.
	View() maclawcapture.View
}

var (
	mu      sync.RWMutex
	current Host
	// wanted is the page the slot asked for. It is recorded before a host
	// exists, because the document must be requested in that host's profile
	// and not framed while the controller is still being created.
	wanted *Placement
)

// Set installs the process host. Nil clears it. A page requested before the
// host existed is shown then, so the first request is not dropped and later
// framed under the app origin.
func Set(h Host) {
	mu.Lock()
	current = h
	var replay Placement
	replayOK := false
	if h == nil {
		wanted = nil
	} else if wanted != nil && wanted.Visible {
		replay = *wanted
		replayOK = true
	}
	mu.Unlock()
	if replayOK {
		_ = h.Place(replay.URL, replay.Bounds)
	}
}

// Available reports whether this process shows a web page as its own top-level
// document. On a capable platform that is true before the controller exists.
func Available() bool {
	if topLevelCapable {
		return true
	}
	mu.RLock()
	defer mu.RUnlock()
	return current != nil
}

// Place asks the host to show url at bounds. The request is kept when the
// host is not installed yet and applied from Set.
func Place(url string, bounds Bounds) error {
	mu.Lock()
	next := Placement{URL: url, Bounds: bounds, Visible: true}
	wanted = &next
	h := current
	mu.Unlock()
	if h == nil {
		if topLevelCapable {
			return nil
		}
		return errNoHost
	}
	return h.Place(url, bounds)
}

// Hide removes the player page from the window. A request that arrived before
// the host is forgotten, so installing the host does not open a page the slot
// has already left.
func Hide() {
	mu.Lock()
	if wanted != nil {
		wanted.Visible = false
	}
	h := current
	mu.Unlock()
	if h != nil {
		h.Hide()
	}
}

// Showing reports whether the player page is supposed to be on screen.
func Showing() bool {
	mu.RLock()
	h := current
	mu.RUnlock()
	if h == nil {
		return false
	}
	return h.Showing()
}

// View returns the showing player page, or nil.
func View() maclawcapture.View {
	mu.RLock()
	h := current
	mu.RUnlock()
	if h == nil {
		return nil
	}
	return h.View()
}

// BoundsFrom converts device pixels. A rectangle that does not cover a pixel
// is not a player surface.
func BoundsFrom(x, y, w, h float64) (Bounds, bool) {
	if math.IsNaN(x) || math.IsNaN(y) || math.IsNaN(w) || math.IsNaN(h) || math.IsInf(w, 0) || math.IsInf(h, 0) {
		return Bounds{}, false
	}
	b := Bounds{X: roundPx(x), Y: roundPx(y), W: roundPx(w), H: roundPx(h)}
	if b.X < 0 {
		b.W += b.X
		b.X = 0
	}
	if b.Y < 0 {
		b.H += b.Y
		b.Y = 0
	}
	if b.W < 1 || b.H < 1 {
		return Bounds{}, false
	}
	if b.W > 20000 {
		b.W = 20000
	}
	if b.H > 20000 {
		b.H = 20000
	}
	return b, true
}

func roundPx(v float64) int32 {
	r := math.Round(v)
	if r > 20000 {
		return 20000
	}
	if r < -20000 {
		return -20000
	}
	return int32(r)
}

// Next decides whether showing url is a new document request.
// Resizing the slot while the same page is already showing only moves the
// surface. Opening it again, or opening a different address, must request
// the document so the profile's cookies for that site are sent.
func Next(prev Placement, url string, bounds Bounds) (Placement, bool) {
	next := Placement{URL: url, Bounds: bounds, Visible: true}
	return next, !prev.Visible || prev.URL != url
}

// ReloadCurrent reports whether next is already the top-level document.
// Navigating to that address again does not send the profile's cookies.
// Reloading does. A fragment or a default port is still that document.
func ReloadCurrent(current, next string) bool {
	if current == "" || next == "" {
		return false
	}
	if current == next {
		return true
	}
	return sameDocument(current, next)
}

// ShouldUnload reports whether current is a site document. Hiding the slot
// unloads it, so playback stops and the next show is a new request.
func ShouldUnload(current string) bool {
	u, err := url.Parse(current)
	if err != nil || u.Host == "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return true
	default:
		return false
	}
}

func sameDocument(current, next string) bool {
	a, err1 := url.Parse(current)
	b, err2 := url.Parse(next)
	if err1 != nil || err2 != nil || a.Host == "" || b.Host == "" {
		return false
	}
	if !strings.EqualFold(a.Scheme, b.Scheme) || !strings.EqualFold(a.Hostname(), b.Hostname()) {
		return false
	}
	if portOrDefault(a) != portOrDefault(b) || a.EscapedPath() != b.EscapedPath() || a.RawQuery != b.RawQuery {
		return false
	}
	return true
}

func portOrDefault(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	default:
		return ""
	}
}
