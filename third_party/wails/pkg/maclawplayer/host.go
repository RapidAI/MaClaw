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
	"sync/atomic"

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
	// Each Place publishes a new pointer. Hide clears Visible on that pointer.
	wanted *Placement
	// applyMu serializes host Place and Hide. The slot calls arrive on
	// different goroutines, and a host call waits for the UI thread. mu stays
	// free across that wait: Set runs on the UI thread and also locks mu.
	applyMu sync.Mutex
	// testBeforeApply runs after a request is published and before the host
	// call. Tests hide or replace the page in that window. Production leaves it nil.
	testBeforeApply func()
	// skipPlace drops the next Place before it is published. The Windows
	// binding queue sets it when a newer Place or Hide is already waiting.
	// The binding goroutines can run out of receive order, so the queue is
	// what keeps a later leave from losing to an earlier Place.
	skipPlace atomic.Bool
	// hostShown reports whether the companion window is on screen. Nil means
	// there is no window fact, and an empty slot still hides the player.
	hostShownFn func() bool
	// showGen advances when the companion window becomes visible. placedGen
	// records the show generation of a positive Place that was received after
	// that transition. An empty box is the minimized size until those match:
	// the show flag is cleared before the page measures, so that box must not
	// unload the document.
	showGen   atomic.Uint64
	placedGen atomic.Uint64
	// placeShowGen is the generation when the Place now running was received.
	// The call can wait in the queue until after the next show. Sampling
	// showGen when it starts would treat the previous layout as the new slot,
	// and the minimized box that follows would unload the page.
	placeShowGen    atomic.Uint64
	placeShowGenSet atomic.Bool
	// placeSeq counts Place and Hide receipts. placeSeqPin is the receipt of
	// the call now running. A positive Place arms empty-hide only when that
	// pin is still the latest receipt after the host returns. The minimized
	// box is posted while the host call waits on the UI thread.
	placeSeq       atomic.Uint64
	placeSeqPin    atomic.Uint64
	placeSeqPinned atomic.Bool
	// fullscreenHideExit is set when the companion window hides while a
	// fullscreen element is up. The exit callback can run after the window
	// is shown again. That exit is the hide, not the reader leaving the video.
	fullscreenHideExit atomic.Bool
	fullscreenApp      atomic.Bool
	fullscreenPlayer   atomic.Bool
)

// Set installs the process host. Nil clears it. A page requested before the
// host existed is shown then, so the first request is not dropped and later
// framed under the app origin.
// The replay calls the host directly and does not take applyMu. Set runs on
// the UI thread, and a Place goroutine may already hold applyMu while it
// waits for that thread.
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

// SkipNextPlace drops the next Place before it changes the wanted page.
// A newer Place or Hide is already queued behind it.
func SkipNextPlace() { skipPlace.Store(true) }

// ConsumeSkipPlace reports that this Place was dropped, and clears the mark.
func ConsumeSkipPlace() bool { return skipPlace.CompareAndSwap(true, false) }

// ClearSkipPlace clears a mark the dropped call did not consume.
func ClearSkipPlace() { skipPlace.Store(false) }

// SetHostShown installs the window-shown check. Nil clears it. The check is
// read from the binding goroutine that places the player.
func SetHostShown(fn func() bool) {
	mu.Lock()
	hostShownFn = fn
	mu.Unlock()
}

// HostShown is false only when a checker was installed and reports that the
// companion window is hiding, minimized, or not visible. Without a checker
// the window counts as shown, so an empty slot still removes the player.
func HostShown() bool {
	mu.RLock()
	fn := hostShownFn
	mu.RUnlock()
	if fn == nil {
		return true
	}
	return fn()
}

// NoteHostVisible marks the companion window shown. The caller is the window
// procedure, before DefWindowProc delivers the resize. The page's next box
// can still be the minimized size, and that box is not the slot leaving.
func NoteHostVisible() {
	showGen.Add(1)
}

// ShowGeneration is the latest NoteHostVisible. The player queue records it
// when a Place is received, not when that call later starts.
func ShowGeneration() uint64 {
	return showGen.Load()
}

// PinPlaceShowGeneration keeps the generation from when this Place was
// received. The queue sets it for the call it is about to run.
func PinPlaceShowGeneration(gen uint64) {
	placeShowGen.Store(gen)
	placeShowGenSet.Store(true)
}

// PlaceShowGeneration is the generation this Place was received at.
// A call that did not come through the queue uses the current generation.
func PlaceShowGeneration() uint64 {
	if placeShowGenSet.Load() {
		return placeShowGen.Load()
	}
	return showGen.Load()
}

// ClearPlaceShowGeneration drops the receive-time generation after the call.
func ClearPlaceShowGeneration() {
	placeShowGenSet.Store(false)
}

// NextPlaceReceipt records one Place or Hide the page sent.
func NextPlaceReceipt() uint64 {
	return placeSeq.Add(1)
}

// PinPlaceReceipt selects the receipt the call about to run was given.
func PinPlaceReceipt(seq uint64) {
	placeSeqPin.Store(seq)
	placeSeqPinned.Store(true)
}

// ClearPlaceReceipt drops the running receipt after the call.
func ClearPlaceReceipt() {
	placeSeqPinned.Store(false)
}

// PlaceReceiptStillLatest reports that nothing newer was received after the
// running call. A direct call has no receipt and is the latest.
func PlaceReceiptStillLatest() bool {
	if !placeSeqPinned.Load() {
		return true
	}
	return placeSeqPin.Load() == placeSeq.Load()
}

// NoteSlotPlaced records that a positive Place was received at gen and the
// window was shown when it was applied. A gen from before the latest show
// does not arm empty-hide; that place measured the previous layout.
func NoteSlotPlaced(gen uint64) {
	if gen == showGen.Load() {
		placedGen.Store(gen)
	}
}

// EmptyHideReady reports that an empty slot is the page leaving, not the
// first measurement after a show. Before any show the two generations are
// equal, so an empty slot still removes the player.
func EmptyHideReady() bool {
	return placedGen.Load() == showGen.Load()
}

// ResetShowPlacement restores the generations tests share. Production does
// not call it.
func ResetShowPlacement() {
	showGen.Store(0)
	placedGen.Store(0)
	placeShowGenSet.Store(false)
	placeSeqPinned.Store(false)
	fullscreenHideExit.Store(false)
	fullscreenApp.Store(false)
	fullscreenPlayer.Store(false)
}

// SetFullscreenSource records which WebView currently contains a fullscreen
// element. The app page and the player page are separate documents.
func SetFullscreenSource(player, on bool) {
	if player {
		fullscreenPlayer.Store(on)
		return
	}
	fullscreenApp.Store(on)
}

// FullscreenElementUp reports that a fullscreen element was still up the
// last time either WebView told us. Hide reads it before DefWindowProc.
func FullscreenElementUp() bool {
	return fullscreenApp.Load() || fullscreenPlayer.Load()
}

// NoteFullscreenHidden marks the next fullscreen exit as belonging to this
// hide. The callback can run after the window is already shown.
func NoteFullscreenHidden() {
	fullscreenHideExit.Store(true)
}

// ClearFullscreenHideExit drops that mark. The show path calls it when a
// live read says fullscreen is still up after DefWindowProc, so a later
// exit is the reader's. The last callback flag is not that read.
func ClearFullscreenHideExit() {
	fullscreenHideExit.Store(false)
}

// NoteFullscreenSurvivedShow clears the hide mark when a live
// ContainsFullScreenElement read says an element is still up. ok is false
// when that read failed or no WebView exists. The callback flag stays true
// until the exit event is delivered, and that event can run after the show
// procedure returns, so the flag must not clear the mark. A failed read
// leaves the mark set: stopping the note on a hide is worse than one
// swallowed reader exit.
func NoteFullscreenSurvivedShow(stillUp, ok bool) {
	if ok && stillUp {
		ClearFullscreenHideExit()
	}
}

// FullscreenNotifyVisible reports the windowVisible bit and whether this
// exit belongs to a hide. The caller records this event's source first.
// windowVisible stays the real shown state. Forcing it false after the
// window is shown again makes the page latch that hide, and the player
// then stops following the slot. hideExit is why the note is not stopped.
// A false from the other WebView does not consume the mark. A true does
// not clear it either. The hide's false may still be waiting, and clearing
// on the true makes that false stop the note.
func FullscreenNotifyVisible(on, contentShown bool) (visible, hideExit bool) {
	if on {
		return contentShown, false
	}
	if !FullscreenElementUp() && fullscreenHideExit.CompareAndSwap(true, false) {
		return contentShown, true
	}
	return contentShown, false
}

// Place asks the host to show url at bounds. The request is kept when the
// host is not installed yet and applied from Set.
// A Place that is no longer the wanted page does not call the host. The slot
// can leave, or a newer Place can replace it, while this call is still on
// the way to the host.
func Place(url string, bounds Bounds) error {
	mu.Lock()
	next := &Placement{URL: url, Bounds: bounds, Visible: true}
	wanted = next
	h := current
	mu.Unlock()
	if h == nil {
		if topLevelCapable {
			return nil
		}
		return errNoHost
	}
	if testBeforeApply != nil {
		testBeforeApply()
	}
	applyMu.Lock()
	defer applyMu.Unlock()
	mu.Lock()
	still := wanted == next && next.Visible
	mu.Unlock()
	if !still {
		return nil
	}
	return h.Place(url, bounds)
}

// Hide removes the player page from the window. A request that arrived before
// the host is forgotten, so installing the host does not open a page the slot
// has already left.
// A Hide that lost to a newer Place does not call the host, so it cannot
// cover the page that Place is about to show.
func Hide() {
	mu.Lock()
	if wanted != nil {
		wanted.Visible = false
	}
	h := current
	mu.Unlock()
	if h == nil {
		return
	}
	applyMu.Lock()
	defer applyMu.Unlock()
	mu.Lock()
	show := wanted != nil && wanted.Visible
	mu.Unlock()
	if show {
		return
	}
	h.Hide()
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
