//go:build windows

package windows

import (
	"errors"
	"log"
	"math"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"github.com/wailsapp/wails/v2/internal/frontend/desktop/windows/winc/w32"
	"github.com/wailsapp/wails/v2/pkg/maclawcapture"
	"github.com/wailsapp/wails/v2/pkg/maclawplayer"
	"golang.org/x/sys/windows"
)

// playerHost is a second WebView2 in the companion environment.
// Its top-level document is the video page, so YouTube (or any other site)
// is the top-level site. Sign-in opens WebView2's default popup, which uses
// this same profile. Cookies set there are sent the next time this document
// is requested. The companion page stays on wails.localhost and does not frame
// the video, so the cookie is not stored under a different top-level site.
//
// Vtable slots match github.com/wailsapp/go-webview2 v1.0.22.
const (
	envCreateControllerSlot             = 3 // ICoreWebView2Environment.CreateCoreWebView2Controller
	controllerGetBoundsSlot             = 5
	controllerPutBoundsSlot             = 6
	controllerCloseSlot                 = 24 // ICoreWebView2Controller.Close
	controllerPutRasterizationScaleSlot = 29 // ICoreWebView2Controller3.put_RasterizationScale
	controllerPutDetectScaleSlot        = 31 // ICoreWebView2Controller3.put_ShouldDetectMonitorScaleChanges
	webviewAddFullscreenSlot            = 52 // ICoreWebView2.AddContainsFullScreenElementChanged
	webviewReloadSlot                   = 31 // ICoreWebView2.Reload
	webviewStopSlot                     = 43 // ICoreWebView2.Stop
)

type rectLTRB struct {
	Left, Top, Right, Bottom int32
}

type playerHost struct {
	mu          sync.Mutex
	win         *Window
	chromium    *edge.Chromium
	env         *edge.ICoreWebView2Environment
	parent      uintptr
	notify      func(bool)
	ctrl        *edge.ICoreWebView2Controller
	wv          *edge.ICoreWebView2
	creating    bool
	epoch       int64
	surface     uintptr
	childBefore map[uintptr]struct{}
	placement   maclawplayer.Placement
	completed   *comHandler
	fullscreen  *comHandler
}

func installCompanionPlayer(f *Frontend) {
	if f == nil || f.playerInstalled || f.chromium == nil || f.mainWindow == nil {
		return
	}
	env := f.chromium.Environment()
	if env == nil {
		return
	}
	comProcs()
	host := &playerHost{
		win:      f.mainWindow,
		chromium: f.chromium,
		env:      env,
		parent:   uintptr(f.mainWindow.Handle()),
		notify: func(on bool) {
			f.notifyCompanionFullscreenFrom(true, on)
		},
	}
	companionPlayer = host
	// A zero slot while this window is hiding must not unload the document.
	// Minimize resizes the webview before the page knows it is hidden.
	maclawplayer.SetHostShown(func() bool {
		if f.mainWindow == nil {
			return true
		}
		return f.mainWindow.contentShown()
	})
	// Set applies a page that was requested before this environment existed.
	maclawplayer.Set(host)
	f.playerInstalled = true
}

// companionPlayer is the window's second controller. Window moves must reach
// it: a sign-in popup is positioned from this WebView, and the app WebView's
// own notification does not move it.
var companionPlayer *playerHost

func notifyCompanionPlayerParentMoved() {
	if companionPlayer == nil {
		return
	}
	companionPlayer.parentMoved()
}

func (h *playerHost) parentMoved() {
	h.mu.Lock()
	ctrl := h.ctrl
	h.mu.Unlock()
	if ctrl != nil {
		_ = ctrl.NotifyParentWindowPositionChanged()
	}
}

func (h *playerHost) Place(url string, bounds maclawplayer.Bounds) error {
	if h == nil {
		return errors.New("player host unavailable")
	}
	if url == "" {
		return errors.New("empty player url")
	}
	ep := h.bump()
	var err error
	if !h.onUI(func() {
		if !h.current(ep) {
			return
		}
		err = h.placeUI(url, bounds)
	}) {
		return errors.New("player ui thread unavailable")
	}
	return err
}

func (h *playerHost) Hide() {
	if h == nil {
		return
	}
	ep := h.bump()
	h.onUI(func() {
		if !h.current(ep) {
			return
		}
		h.hideUI()
	})
}

// bump marks this place or hide as the latest request. A callback still
// queued from an older request must not paint after a newer one: the two
// calls can post to the UI thread out of order.
func (h *playerHost) bump() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.epoch++
	return h.epoch
}

func (h *playerHost) current(ep int64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return ep == h.epoch
}

func (h *playerHost) Showing() bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.placement.Visible
}

func (h *playerHost) View() maclawcapture.View {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.placement.Visible || h.wv == nil {
		return nil
	}
	return coreWebView{wv: h.wv}
}

// fullscreenElement reads ContainsFullScreenElement on this document.
// ok is false when the WebView does not exist yet or the COM call fails.
// The caller is the window thread. The last fullscreen callback is not this
// value: that flag stays true until the exit event is delivered.
func (h *playerHost) fullscreenElement() (on, ok bool) {
	if h == nil {
		return false, false
	}
	h.mu.Lock()
	wv := h.wv
	h.mu.Unlock()
	if wv == nil {
		return false, false
	}
	contains, err := wv.GetContainsFullScreenElement()
	if err != nil {
		return false, false
	}
	return contains, true
}

func (h *playerHost) onUI(fn func()) bool {
	if h.win == nil || fn == nil {
		return false
	}
	done := make(chan struct{})
	h.win.Invoke(func() {
		defer close(done)
		fn()
	})
	<-done
	return true
}

func (h *playerHost) placeUI(url string, bounds maclawplayer.Bounds) error {
	h.mu.Lock()
	next, navigate := maclawplayer.Next(h.placement, url, bounds)
	h.placement = next
	wv := h.wv
	h.mu.Unlock()
	if wv == nil {
		h.ensureUI()
		return nil
	}
	h.applyUI(next, navigate)
	return nil
}

func (h *playerHost) hideUI() {
	h.mu.Lock()
	h.placement.Visible = false
	ctrl := h.ctrl
	wv := h.wv
	h.mu.Unlock()
	if ctrl != nil {
		_ = ctrl.PutIsVisible(false)
	}
	// The slot is gone. An iframe would have been destroyed with it. Unload
	// this document so playback stops, and so the next show is a new request
	// that sends the profile's cookies.
	if wv != nil {
		h.unloadDocument(wv)
	}
}

func (h *playerHost) unloadDocument(wv *edge.ICoreWebView2) {
	// Place's Navigate returns before the next document commits. Source can
	// still be the previous page, including about:blank. Reading it first
	// then skips this unload, and the hidden WebView plays the page. Stop
	// cancels that navigation. A document that has already committed is left
	// in place, so an http(s) page still goes to about:blank below.
	_ = callSlot(unsafe.Pointer(wv), webviewStopSlot)
	current, err := wv.GetSource()
	if err != nil || !maclawplayer.ShouldUnload(current) {
		return
	}
	if err := wv.Navigate("about:blank"); err != nil {
		log.Printf("[companion-player] unload: %v", err)
	}
}

func (h *playerHost) ensureUI() {
	h.mu.Lock()
	if h.wv != nil || h.creating || h.env == nil || h.parent == 0 {
		h.mu.Unlock()
		return
	}
	h.creating = true
	env := h.env
	parent := h.parent
	if h.completed == nil {
		h.completed = newComHandler(h, controllerInvokePtr, guidControllerCompleted)
	}
	handler := h.completed
	h.mu.Unlock()
	// The player window does not exist yet. Remember the chrome children so
	// the one created for this controller can be told from the app WebView.
	h.childBefore = chromeChildSet(parent)
	hr := callSlot(unsafe.Pointer(env), envCreateControllerSlot, parent, uintptr(unsafe.Pointer(handler)))
	if int32(hr) < 0 {
		h.mu.Lock()
		h.creating = false
		h.mu.Unlock()
		log.Printf("[companion-player] create controller: 0x%x", uint32(hr))
	}
}

func (h *playerHost) attach(ctrl *edge.ICoreWebView2Controller, wv *edge.ICoreWebView2) {
	h.useHostScale(ctrl)
	_ = ctrl.PutIsVisible(false)
	h.mu.Lock()
	h.ctrl = ctrl
	h.wv = wv
	h.creating = false
	if h.fullscreen == nil {
		h.fullscreen = newComHandler(h, fullscreenInvokePtr, guidFullscreenChanged)
	}
	handler := h.fullscreen
	placement := h.placement
	h.mu.Unlock()
	var token int64
	hr := callSlot(unsafe.Pointer(wv), webviewAddFullscreenSlot, uintptr(unsafe.Pointer(handler)), uintptr(unsafe.Pointer(&token)))
	if int32(hr) < 0 {
		log.Printf("[companion-player] fullscreen event: 0x%x", uint32(hr))
	}
	if placement.Visible {
		h.applyUI(placement, true)
	}
}

func (h *playerHost) applyUI(p maclawplayer.Placement, navigate bool) {
	h.mu.Lock()
	ctrl := h.ctrl
	wv := h.wv
	h.mu.Unlock()
	if ctrl == nil || wv == nil {
		return
	}
	h.paint(ctrl, p.Bounds)
	if err := ctrl.PutIsVisible(true); err != nil {
		log.Printf("[companion-player] show: %v", err)
	}
	if !navigate {
		return
	}
	h.requestDocument(wv, p.URL)
}

// requestDocument loads url. Navigating to the address already shown is not a
// new request, so cookies stored by the sign-in window are not sent. Reload
// is that request. Stop first so an unload that has not committed cannot
// replace this request with about:blank.
func (h *playerHost) requestDocument(wv *edge.ICoreWebView2, url string) {
	_ = callSlot(unsafe.Pointer(wv), webviewStopSlot)
	current, err := wv.GetSource()
	if err != nil {
		current = ""
	}
	if maclawplayer.ReloadCurrent(current, url) {
		hr := callSlot(unsafe.Pointer(wv), webviewReloadSlot)
		if int32(hr) < 0 {
			log.Printf("[companion-player] reload %s: 0x%x", url, uint32(hr))
			return
		}
		log.Printf("[companion-player] top-level %s", url)
		return
	}
	if err := wv.Navigate(url); err != nil {
		log.Printf("[companion-player] navigate %s: %v", url, err)
		return
	}
	log.Printf("[companion-player] top-level %s", url)
}

// useHostScale makes the player page use the same pixels as the slot.
// The slot is measured in the app WebView. Bounds are raw pixels, and the
// page's CSS pixel is those pixels divided by the rasterization scale, so
// this scale has to be that WebView's scale.
//
// put_RasterizationScale takes the float by value. The Windows syscall copies
// each argument into an XMM register, so the bits of the float belong in that
// argument. The generated binding passes the address instead, and that address
// as a float is about 1e-311, which is still greater than zero and would be
// stored as the scale. put_ShouldDetectMonitorScaleChanges takes a BOOL.
// Passing its address makes the BOOL non-zero, so the app WebView's attempt
// to turn detection off leaves it on. This page keeps detection on as well,
// and devicePixelRatio follows the same monitor on both.
func (h *playerHost) useHostScale(ctrl *edge.ICoreWebView2Controller) {
	c3 := ctrl.GetICoreWebView2Controller3()
	if c3 == nil {
		return
	}
	if err := c3.PutBoundsMode(edge.COREWEBVIEW2_BOUNDS_MODE_USE_RAW_PIXELS); err != nil {
		log.Printf("[companion-player] bounds mode: %v", err)
	}
	if h.chromium == nil {
		return
	}
	main := h.chromium.GetController()
	if main == nil {
		return
	}
	main3 := main.GetICoreWebView2Controller3()
	if main3 == nil {
		return
	}
	scale, err := main3.GetRasterizationScale()
	if err != nil || scale <= 0 {
		return
	}
	hr := callSlot(unsafe.Pointer(c3), controllerPutRasterizationScaleSlot, uintptr(math.Float64bits(scale)))
	if int32(hr) < 0 {
		log.Printf("[companion-player] rasterization scale %v: 0x%x", scale, uint32(hr))
		return
	}
	hr = callSlot(unsafe.Pointer(c3), controllerPutDetectScaleSlot, 1)
	if int32(hr) < 0 {
		log.Printf("[companion-player] scale detection: 0x%x", uint32(hr))
	}
}

func (h *playerHost) paint(ctrl *edge.ICoreWebView2Controller, slot maclawplayer.Bounds) {
	// The slot is the left preview column. A fullscreen element fills this
	// WebView, so it stays in that column. Growing to the companion window
	// covers the chat.
	h.putBounds(ctrl, slot)
}

func (h *playerHost) putBounds(ctrl *edge.ICoreWebView2Controller, b maclawplayer.Bounds) {
	ox, oy := h.mainOrigin()
	h.putRect(ctrl, rectLTRB{
		Left:   ox + b.X,
		Top:    oy + b.Y,
		Right:  ox + b.X + b.W,
		Bottom: oy + b.Y + b.H,
	})
}

func (h *playerHost) putRect(ctrl *edge.ICoreWebView2Controller, r rectLTRB) {
	hr := callSlot(unsafe.Pointer(ctrl), controllerPutBoundsSlot, uintptr(unsafe.Pointer(&r)))
	if int32(hr) < 0 {
		log.Printf("[companion-player] bounds: 0x%x", uint32(hr))
	} else {
		// Popups, including the sign-in window, are placed from this screen origin.
		_ = ctrl.NotifyParentWindowPositionChanged()
	}
	// PutBounds does not change z-order when the rectangle is unchanged.
	// Resizing the app WebView raises that window, which then covers the player.
	h.raise()
}

func (h *playerHost) raise() {
	if h.surface == 0 {
		return
	}
	w32.SetWindowPos(w32.HWND(h.surface), w32.HWND_TOP, 0, 0, 0, 0, w32.SWP_NOSIZE|w32.SWP_NOMOVE|w32.SWP_NOACTIVATE)
}

func (h *playerHost) mainOrigin() (int32, int32) {
	r, ok := h.mainRect()
	if !ok {
		return 0, 0
	}
	return r.Left, r.Top
}

func (h *playerHost) mainRect() (rectLTRB, bool) {
	if h.chromium == nil {
		return rectLTRB{}, false
	}
	main := h.chromium.GetController()
	if main == nil {
		return rectLTRB{}, false
	}
	var r rectLTRB
	hr := callSlot(unsafe.Pointer(main), controllerGetBoundsSlot, uintptr(unsafe.Pointer(&r)))
	if int32(hr) < 0 || r.Right <= r.Left || r.Bottom <= r.Top {
		return rectLTRB{}, false
	}
	return r, true
}

func restackCompanionPlayer() {
	if companionPlayer == nil {
		return
	}
	companionPlayer.restack()
}

func (h *playerHost) restack() {
	h.mu.Lock()
	ctrl := h.ctrl
	visible := h.placement.Visible
	bounds := h.placement.Bounds
	h.mu.Unlock()
	if ctrl == nil || !visible {
		return
	}
	h.paint(ctrl, bounds)
}

func closeController(ctrl *edge.ICoreWebView2Controller) {
	if ctrl == nil {
		return
	}
	_ = callSlot(unsafe.Pointer(ctrl), controllerCloseSlot)
	ctrl.Release()
}

// callSlot invokes one COM method. A pointer passed as uintptr is still the
// caller's memory: PutBounds reads the rectangle, GetBounds and the event
// token write into it. Growing the stack on the way into the syscall would
// leave that address pointing at the old frame.
//
//go:uintptrescapes
func callSlot(obj unsafe.Pointer, slot int, args ...uintptr) uintptr {
	if obj == nil {
		return 0x80004003
	}
	vtbl := *(*unsafe.Pointer)(obj)
	if vtbl == nil {
		return 0x80004003
	}
	fn := *(*uintptr)(unsafe.Add(vtbl, slot*int(unsafe.Sizeof(uintptr(0)))))
	call := make([]uintptr, 0, 1+len(args))
	call = append(call, uintptr(obj))
	call = append(call, args...)
	ret, _, _ := syscall.SyscallN(fn, call...)
	return ret
}

// comHandler is an IUnknown plus Invoke. The vtable is the first field so the
// COM this pointer is the struct.
type comHandler struct {
	vtbl *comVtbl
	host *playerHost
	refs uintptr
	iid  windows.GUID
}

type comVtbl struct {
	query, add, release, invoke uintptr
}

var (
	comOnce             sync.Once
	comQueryPtr         uintptr
	comAddPtr           uintptr
	comReleasePtr       uintptr
	controllerInvokePtr uintptr
	fullscreenInvokePtr uintptr
	enumChildPtr        uintptr
	procGetParent       = windows.NewLazySystemDLL("user32.dll").NewProc("GetParent")
)

func comProcs() {
	comOnce.Do(func() {
		comQueryPtr = syscall.NewCallback(comQuery)
		comAddPtr = syscall.NewCallback(comAdd)
		comReleasePtr = syscall.NewCallback(comRelease)
		controllerInvokePtr = syscall.NewCallback(controllerInvoke)
		fullscreenInvokePtr = syscall.NewCallback(fullscreenInvoke)
		enumChildPtr = syscall.NewCallback(enumChild)
	})
}

func newComHandler(host *playerHost, invoke uintptr, iid windows.GUID) *comHandler {
	comProcs()
	return &comHandler{
		vtbl: &comVtbl{query: comQueryPtr, add: comAddPtr, release: comReleasePtr, invoke: invoke},
		host: host,
		refs: 1,
		iid:  iid,
	}
}

// Accept IUnknown and this handler only. Returning this object for every IID
// would claim IAgileObject, and WebView2 could then call Invoke off the UI thread.
func comQuery(this, riid, object uintptr) uintptr {
	if this == 0 || riid == 0 || object == 0 {
		return 0x80004003
	}
	*(*uintptr)(unsafe.Pointer(object)) = 0
	id := *(*windows.GUID)(unsafe.Pointer(riid))
	h := (*comHandler)(unsafe.Pointer(this))
	if id != guidIUnknown && id != h.iid {
		return 0x80004002
	}
	*(*uintptr)(unsafe.Pointer(object)) = this
	comAdd(this)
	return 0
}

func comAdd(this uintptr) uintptr {
	if this == 0 {
		return 1
	}
	return atomic.AddUintptr(&(*comHandler)(unsafe.Pointer(this)).refs, 1)
}

func comRelease(this uintptr) uintptr {
	if this == 0 {
		return 0
	}
	refs := atomic.AddUintptr(&(*comHandler)(unsafe.Pointer(this)).refs, ^uintptr(0))
	return refs
}

func controllerInvoke(this, errorCode, controller uintptr) uintptr {
	if this == 0 {
		return 0
	}
	h := (*comHandler)(unsafe.Pointer(this)).host
	if h == nil {
		return 0
	}
	if controller == 0 || int32(errorCode) < 0 {
		log.Printf("[companion-player] controller completed 0x%x", uint32(errorCode))
		h.mu.Lock()
		h.creating = false
		h.mu.Unlock()
		return 0
	}
	ctrl := (*edge.ICoreWebView2Controller)(unsafe.Pointer(controller))
	// IsVisible defaults to true and the bounds default to the parent, so the
	// new page would cover the companion until the slot is applied.
	_ = ctrl.PutIsVisible(false)
	ctrl.AddRef()
	wv, err := ctrl.GetCoreWebView2()
	if err != nil || wv == nil {
		log.Printf("[companion-player] webview: %v", err)
		closeController(ctrl)
		h.mu.Lock()
		h.creating = false
		h.mu.Unlock()
		return 0
	}
	wv.AddRef()
	h.noteSurface()
	h.attach(ctrl, wv)
	return 0
}

// noteSurface records the window created for this controller. The app WebView
// raises itself on resize. This window is what has to be ordered above it.
func (h *playerHost) noteSurface() {
	found := chromeChildren(h.parent, h.childBefore)
	h.childBefore = nil
	if len(found) == 0 {
		log.Printf("[companion-player] player window was not found")
		return
	}
	h.surface = found[len(found)-1]
}

type childEnum struct {
	parent uintptr
	found  []uintptr
}

func chromeChildSet(parent uintptr) map[uintptr]struct{} {
	found := directChromeWidgets(parent)
	set := make(map[uintptr]struct{}, len(found))
	for _, hwnd := range found {
		set[hwnd] = struct{}{}
	}
	return set
}

func chromeChildren(parent uintptr, skip map[uintptr]struct{}) []uintptr {
	found := directChromeWidgets(parent)
	if len(skip) == 0 {
		return found
	}
	out := make([]uintptr, 0, len(found))
	for _, hwnd := range found {
		if _, old := skip[hwnd]; !old {
			out = append(out, hwnd)
		}
	}
	return out
}

func directChromeWidgets(parent uintptr) []uintptr {
	comProcs()
	state := &childEnum{parent: parent}
	var pin runtime.Pinner
	pin.Pin(state)
	defer pin.Unpin()
	windows.EnumChildWindows(windows.HWND(parent), enumChildPtr, unsafe.Pointer(state))
	return state.found
}

func enumChild(hwnd, param uintptr) uintptr {
	if hwnd == 0 || param == 0 {
		return 1
	}
	state := (*childEnum)(unsafe.Pointer(param))
	if parentOf(hwnd) != state.parent {
		return 1
	}
	if chromeWidget(hwnd) {
		state.found = append(state.found, hwnd)
	}
	return 1
}

func parentOf(hwnd uintptr) uintptr {
	r, _, _ := procGetParent.Call(hwnd)
	return r
}

func chromeWidget(hwnd uintptr) bool {
	buf := make([]uint16, 64)
	n, _ := windows.GetClassName(windows.HWND(hwnd), &buf[0], int32(len(buf)))
	if n <= 0 {
		return false
	}
	return strings.HasPrefix(windows.UTF16ToString(buf[:n]), "Chrome_WidgetWin")
}

var (
	guidIUnknown = windows.GUID{
		Data1: 0x00000000, Data2: 0x0000, Data3: 0x0000,
		Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46},
	}
	// ICoreWebView2CreateCoreWebView2ControllerCompletedHandler
	guidControllerCompleted = windows.GUID{
		Data1: 0x6C4819F3, Data2: 0xC9B7, Data3: 0x4260,
		Data4: [8]byte{0x81, 0x27, 0xC9, 0xF5, 0xBD, 0xE7, 0xF6, 0x8C},
	}
	// ICoreWebView2ContainsFullScreenElementChangedEventHandler
	guidFullscreenChanged = windows.GUID{
		Data1: 0xE45D98B1, Data2: 0xAFEF, Data3: 0x45BE,
		Data4: [8]byte{0x8B, 0xAF, 0x6C, 0x77, 0x28, 0x86, 0x7F, 0x73},
	}
)

func fullscreenInvoke(this, _, _ uintptr) uintptr {
	if this == 0 {
		return 0
	}
	h := (*comHandler)(unsafe.Pointer(this)).host
	if h == nil {
		return 0
	}
	h.mu.Lock()
	wv := h.wv
	h.mu.Unlock()
	if wv == nil {
		return 0
	}
	on, err := wv.GetContainsFullScreenElement()
	if err != nil {
		return 0
	}
	// The element fills this WebView. Keep the WebView on the left preview
	// slot so the chat stays visible. The signal is still the page's.
	h.mu.Lock()
	ctrl := h.ctrl
	bounds := h.placement.Bounds
	visible := h.placement.Visible
	h.mu.Unlock()
	if ctrl != nil && visible {
		h.paint(ctrl, bounds)
	}
	if h.notify != nil {
		h.notify(on)
	}
	return 0
}
