//go:build windows

package maclawplayer

// v3-native companion player host: a borderless popup window (owned by the
// companion main window, hidden from the taskbar) whose WebView2 loads the
// playback document as its own top-level page. This replaces the wails/v2
// fork's in-window second-WebView2 composition surface (player_host.go,
// raw-vtable implementation) — the cookie semantics are identical because
// the popup shares the companion process profile and the video site is the
// top-level document. Over RDP the popup enables WebView2 composition
// hosting (windowed hosting does not present in a disconnected/reattached
// session).
//
// The popup tracks the player slot: the caller reports the slot rectangle in
// companion-window client pixels on every Place, and the host repositions the
// popup whenever the main window moves or resizes.

import (
	"sync"

	"github.com/RapidAI/CodeClaw/guiapp/internal/maclawcapture"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"golang.org/x/sys/windows"
)

const smRemoteSessionPlayer = 0x1000

func sessionIsRemotePlayer() bool {
	user32 := windows.NewLazySystemDLL("user32.dll")
	ret, _, _ := user32.NewProc("GetSystemMetrics").Call(uintptr(smRemoteSessionPlayer))
	return ret != 0
}

// setWindowOwner marks the popup as owned by the main window: an owned
// window stays above its owner, hides with it, and stays out of Alt+Tab.
// GWLP_HWNDPARENT is -8.
func setWindowOwner(popup, main uintptr) {
	const gwlpHwndParent = ^uintptr(7)
	user32Owner.NewProc("SetWindowLongPtrW").Call(popup, gwlpHwndParent, main)
}

var user32Owner = windows.NewLazySystemDLL("user32.dll")

// v3PlayerHost implements maclawplayer.Host over a wails3 WebviewWindow.
type v3PlayerHost struct {
	mu        sync.Mutex
	app       *application.App
	mainWin   *application.WebviewWindow
	win       *application.WebviewWindow
	winGone   bool
	url       string
	placement Placement
}

func (h *v3PlayerHost) ensureWindow(url string) *application.WebviewWindow {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.winGone {
		return nil
	}
	if h.win != nil {
		return h.win
	}
	if h.app == nil || h.mainWin == nil {
		return nil
	}
	w, ht := 960, 640
	if h.placement.Bounds.W > 0 && h.placement.Bounds.H > 0 {
		w, ht = int(h.placement.Bounds.W), int(h.placement.Bounds.H)
	}
	win := h.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            " ",
		Name:             "companion-player",
		URL:              url,
		Frameless:        true,
		Width:            w,
		Height:           ht,
		BackgroundColour: application.RGBA{Red: 12, Green: 12, Blue: 12, Alpha: 255},
		Windows: application.WindowsWindow{
			WebView2CompositionHosting: sessionIsRemotePlayer(),
		},
	})
	if win == nil {
		return nil
	}
	h.win = win
	h.url = url
	// Owned by the companion main window: stays above it, hides with it,
	// kept out of Alt+Tab (WS_EX_TOOLWINDOW).
	if main := uintptr(h.mainWin.NativeWindow()); main != 0 {
		if popup := uintptr(win.NativeWindow()); popup != 0 {
			setWindowOwner(popup, main)
		}
	}
	return win
}

// syncToSlot moves the popup over the slot rectangle. The caller-supplied
// bounds are companion-window client pixels (device px), so the popup's
// physical position is the main window's physical top-left plus the slot
// offset.
func (h *v3PlayerHost) syncToSlot() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.winGone || !h.placement.Visible {
		return
	}
	pb := h.mainWin.PhysicalBounds()
	h.win.SetPhysicalBounds(application.Rect{
		X:      pb.X + int(h.placement.Bounds.X),
		Y:      pb.Y + int(h.placement.Bounds.Y),
		Width:  int(h.placement.Bounds.W),
		Height: int(h.placement.Bounds.H),
	})
}

// InitCompanionPlayerHost installs the v3 host and its main-window tracking.
// Call once after the companion main window exists. mainWin is the companion
// window the player slot lives in; shown reports the real on-screen state.
func InitCompanionPlayerHost(app *application.App, mainWin *application.WebviewWindow, shown func() bool) {
	h := &v3PlayerHost{app: app, mainWin: mainWin}
	Set(h)
	if shown != nil {
		SetHostShown(shown)
	}
	resync := func(*application.WindowEvent) {
		h.mu.Lock()
		b := h.placement
		h.mu.Unlock()
		if b.Visible && !h.winGone {
			h.syncToSlot()
		}
	}
	mainWin.OnWindowEvent(events.Windows.WindowDidMove, resync)
	mainWin.OnWindowEvent(events.Windows.WindowDidResize, resync)
}

// Place shows url over the slot rectangle, creating the popup on first use.
func (h *v3PlayerHost) Place(url string, bounds Bounds) error {
	h.mu.Lock()
	h.placement = Placement{URL: url, Bounds: bounds, Visible: true}
	h.mu.Unlock()
	win := h.ensureWindow(url)
	if win == nil {
		return errNoHost
	}
	h.mu.Lock()
	h.url = url
	h.mu.Unlock()
	if h.placement.URL != url {
		win.SetURL(url)
	}
	h.syncToSlot()
	win.Show()
	return nil
}

// Hide removes the popup from the screen. The document stays loaded: the
// next Place for the same URL resumes playback (matching the v2 host).
func (h *v3PlayerHost) Hide() {
	h.mu.Lock()
	h.placement.Visible = false
	h.mu.Unlock()
	if h.win == nil || h.winGone {
		return
	}
	h.win.Hide()
}

// Showing reports the popup's on-screen state.
func (h *v3PlayerHost) Showing() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.win == nil || h.winGone {
		return false
	}
	return h.win.IsVisible()
}

// View returns the popup window as the capture view (PrintWindow raster).
func (h *v3PlayerHost) View() maclawcapture.View {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.win == nil || h.winGone {
		return nil
	}
	hwnd := uintptr(h.win.NativeWindow())
	return &maclawcapture.HwndView{Hwnd: hwnd, IsFullFn: func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.win != nil && !h.winGone && h.win.IsFullscreen()
	}}
}

// MarkGone records that the popup window was destroyed (Alt+F4 on the
// popup). The next Place recreates it — the slot self-heals.
func (h *v3PlayerHost) MarkGone() {
	h.mu.Lock()
	h.win = nil
	h.winGone = true
	h.mu.Unlock()
}
