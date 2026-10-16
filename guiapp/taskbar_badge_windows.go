//go:build windows

package guiapp

import (
	"fmt"
	"log"
	"math"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/energye/systray"
)

// Windows taskbar badge: render the given count into a small dark-green disc and
// set it as the ITaskbarList3 overlay icon of the main window's taskbar
// button — the same mechanism behind the QQ unread-count badge. The frontend
// pushes the in-progress task count through App.SetTaskbarBadgeCount whenever
// that count changes; this file owns rendering and the win32 plumbing.
//
// All COM calls run on one dedicated locked OS thread (the worker goroutine)
// because ITaskbarList3 requires an initialized COM apartment.
//
// An explorer.exe restart drops per-window overlays. The tray window hears
// TaskbarCreated and asks this worker to paint the last count again.

const (
	taskbarBadgeCoInitApartmentThreaded = 0x2
	taskbarBadgeClsCtxInProcServer      = 0x1

	// SM_CXSMICON. SetOverlayIcon draws a small overlay; SM_CXICON (11) is the
	// 32px icon and the shell drops or clips it.
	taskbarBadgeSmCxIcon = 49

	// Vtable layout, counted from zero:
	//   IUnknown: QueryInterface, AddRef, Release
	//   ITaskbarList: HrInit, AddTab, DeleteTab, ActivateTab, SetActiveAlt
	//   ITaskbarList2: MarkFullscreenWindow
	//   ITaskbarList3: SetProgressValue, SetProgressState, RegisterTab,
	//     UnregisterTab, SetTabOrder, SetTabActive, ThumbBarAddButtons,
	//     ThumbBarUpdateButtons, ThumbBarSetImageList, then SetOverlayIcon.
	// Index 11 is RegisterTab. Calling it with an icon handle returns E_FAIL
	// and never paints a badge.
	taskbarBadgeVtableRelease        = 2
	taskbarBadgeVtableHrInit         = 3
	taskbarBadgeVtableAddTab         = 4
	taskbarBadgeVtableSetOverlayIcon = 18

	taskbarBadgeBkModeTransparent = 1
	taskbarBadgeFontWeightBold    = 700
	taskbarBadgeQualityAntialiase = 4

	taskbarBadgeDtCenter     = 0x1
	taskbarBadgeDtVcenter    = 0x4
	taskbarBadgeDtSingleLine = 0x20

	// Fast retries while the main HWND is still missing, then a slow poll so
	// a count that arrived before the window is not dropped.
	taskbarBadgeHWNDTries     = 20
	taskbarBadgeHWNDRetryWait = 500 * time.Millisecond
	taskbarBadgeHWNDSlowWait  = 2 * time.Second

	// SetOverlayIcon can fail until the shell creates the taskbar button.
	// Retry quickly, call AddTab once, then keep a slow poll so a count
	// that arrived while the window was hidden still appears later.
	taskbarBadgeOverlayTries = 8

	// Disc fill sampled from the app icon outline, RGB(19, 59, 67).
	taskbarBadgeFillR = 19
	taskbarBadgeFillG = 59
	taskbarBadgeFillB = 67
)

type taskbarBadgeGUID struct {
	data1 uint32
	data2 uint16
	data3 uint16
	data4 [8]byte
}

var (
	clsidTaskbarList = taskbarBadgeGUID{0x56FDF344, 0xFD6D, 0x11D0, [8]byte{0x95, 0x8A, 0x00, 0x60, 0x97, 0xC9, 0xA0, 0x90}}
	iidITaskbarList3 = taskbarBadgeGUID{0xEA1AFB91, 0x9E28, 0x4B86, [8]byte{0x90, 0xE9, 0x9E, 0x9F, 0x8A, 0x5E, 0xEF, 0xAF}}
)

var (
	taskbarBadgeOle32       = syscall.NewLazyDLL("ole32.dll")
	procBadgeCoInitialize   = taskbarBadgeOle32.NewProc("CoInitializeEx")
	procBadgeCoCreateInst   = taskbarBadgeOle32.NewProc("CoCreateInstance")
	procBadgeCoUninitialize = taskbarBadgeOle32.NewProc("CoUninitialize")

	taskbarBadgeUser32           = syscall.NewLazyDLL("user32.dll")
	procBadgeSystemMetrics       = taskbarBadgeUser32.NewProc("GetSystemMetrics")
	procBadgeIsWindow            = taskbarBadgeUser32.NewProc("IsWindow")
	procBadgeIsWindowVisible     = taskbarBadgeUser32.NewProc("IsWindowVisible")
	procBadgeGetAncestor         = taskbarBadgeUser32.NewProc("GetAncestor")
	procBadgeGetDpiForWindow     = taskbarBadgeUser32.NewProc("GetDpiForWindow")
	procBadgeSystemMetricsForDpi = taskbarBadgeUser32.NewProc("GetSystemMetricsForDpi")
	procBadgeDrawText            = taskbarBadgeUser32.NewProc("DrawTextW")
	procBadgeCreateIconIndirect  = taskbarBadgeUser32.NewProc("CreateIconIndirect")
	procBadgeDestroyIcon         = taskbarBadgeUser32.NewProc("DestroyIcon")

	taskbarBadgeGdi32           = syscall.NewLazyDLL("gdi32.dll")
	procBadgeSetBkMode          = taskbarBadgeGdi32.NewProc("SetBkMode")
	procBadgeSetTextColor       = taskbarBadgeGdi32.NewProc("SetTextColor")
	procBadgeCreateCompatibleDC = taskbarBadgeGdi32.NewProc("CreateCompatibleDC")
	procBadgeDeleteDC           = taskbarBadgeGdi32.NewProc("DeleteDC")
	procBadgeCreateDIBSection   = taskbarBadgeGdi32.NewProc("CreateDIBSection")
	procBadgeSelectObject       = taskbarBadgeGdi32.NewProc("SelectObject")
	procBadgeDeleteObject       = taskbarBadgeGdi32.NewProc("DeleteObject")
	procBadgeCreateFont         = taskbarBadgeGdi32.NewProc("CreateFontW")
	procBadgeCreateBitmap       = taskbarBadgeGdi32.NewProc("CreateBitmap")
)

var (
	taskbarBadgeStartOnce sync.Once
	taskbarBadgeRequests  chan int

	// Serializes the coalescing send below so two concurrent producers can
	// not drain each other's queued request and lose the latest count.
	taskbarBadgeMu sync.Mutex

	taskbarBadgeHWNDCacheMu sync.Mutex
	taskbarBadgeHWNDCache   uintptr

	// Last count the frontend asked for. Explorer drops overlays on restart;
	// TaskbarCreated replays this value.
	taskbarBadgeLast    int
	taskbarBadgeLastSet bool

	// Hide removes the taskbar button after WindowHide returns. The replay
	// waits until that button has gone and come back, so a paint that lands
	// while the old button is still up is not treated as done.
	taskbarBadgeShow      taskbarBadgeShowPhase
	taskbarBadgeShowArmed time.Time
)

// taskbarBadgeShowPhase tracks a hide that still has to be painted again.
type taskbarBadgeShowPhase int

const (
	taskbarBadgeShowIdle taskbarBadgeShowPhase = iota
	taskbarBadgeShowWaitHide
	taskbarBadgeShowWaitShow
)

func init() {
	systray.OnTaskbarCreated = replayTaskbarBadge
}

// notifyTaskbarBadgeCount wakes the worker with the desired count.
// Requests are coalesced so a burst of count changes costs one overlay set.
func notifyTaskbarBadgeCount(count int) {
	taskbarBadgeStartOnce.Do(func() {
		taskbarBadgeRequests = make(chan int, 1)
		go taskbarBadgeWorker(taskbarBadgeRequests)
	})
	taskbarBadgeMu.Lock()
	taskbarBadgeLast = count
	taskbarBadgeLastSet = true
	defer taskbarBadgeMu.Unlock()
	select {
	case taskbarBadgeRequests <- count:
		return
	default:
	}
	// The queued request may carry a stale count; replace it.
	select {
	case <-taskbarBadgeRequests:
	default:
	}
	select {
	case taskbarBadgeRequests <- count:
	default:
	}
}

// replayTaskbarBadge paints the last requested count again. Called on the
// tray window thread when explorer broadcasts TaskbarCreated.
func replayTaskbarBadge() {
	// The HWND survives explorer restart, but the taskbar button does not.
	taskbarBadgeForgetHWND()
	taskbarBadgeMu.Lock()
	count, ok := taskbarBadgeLast, taskbarBadgeLastSet
	taskbarBadgeMu.Unlock()
	if !ok {
		return
	}
	notifyTaskbarBadgeCount(count)
}

func taskbarBadgeDrainTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

func taskbarBadgeWorker(requests <-chan int) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// The badge thread owns its COM apartment for the process lifetime.
	hr, _, _ := procBadgeCoInitialize.Call(0, taskbarBadgeCoInitApartmentThreaded)
	if hr == 0 || hr == 1 { // S_OK / S_FALSE
		defer procBadgeCoUninitialize.Call()
	}

	taskbar, err := taskbarBadgeCreateTaskbarList3()
	if err != nil {
		log.Printf("[taskbar-badge] overlay unsupported: %v", err)
		for range requests {
		}
		return
	}
	defer taskbarBadgeVtableCall(taskbar, taskbarBadgeVtableRelease)

	var liveIcon uintptr
	var pending int
	waiting := false
	misses := 0
	overlayAttempt := 0
	// A stopped timer that never fires. Reset arms it only while a count
	// still needs a window or a taskbar button.
	retry := time.NewTimer(time.Hour)
	taskbarBadgeDrainTimer(retry)

	apply := func(count int) {
		var result taskbarBadgeResult
		liveIcon, result = taskbarBadgeServeCount(taskbar, count, liveIcon, taskbarBadgeRegisterTab(overlayAttempt))
		switch result {
		case taskbarBadgeApplied:
			waiting = false
			misses = 0
			overlayAttempt = 0
		case taskbarBadgeNoWindow:
			// The frame is gone. A later window needs its own plain tries
			// before the one AddTab.
			overlayAttempt = 0
			waiting = true
			misses++
			wait := taskbarBadgeHWNDRetryWait
			if misses > taskbarBadgeHWNDTries {
				wait = taskbarBadgeHWNDSlowWait
			}
			retry.Reset(wait)
		case taskbarBadgeOverlayMiss:
			overlayAttempt++
			waiting = true
			wait := taskbarBadgeHWNDRetryWait
			if overlayAttempt > taskbarBadgeOverlayTries {
				wait = taskbarBadgeHWNDSlowWait
			}
			retry.Reset(wait)
		case taskbarBadgeWindowHidden:
			waiting = true
			retry.Reset(taskbarBadgeHiddenRetryWait)
		}
	}

	for {
		var wake <-chan time.Time
		if waiting {
			wake = retry.C
		}
		select {
		case count, ok := <-requests:
			if !ok {
				return
			}
			pending = count
			taskbarBadgeDrainTimer(retry)
			misses = 0
			overlayAttempt = 0
			apply(count)
		case <-wake:
			if !waiting {
				continue
			}
			apply(pending)
		}
	}
}

// taskbarBadgeRegisterTab reports whether this one overlay attempt may call
// AddTab. Later slow polls only call SetOverlayIcon; repeating AddTab can
// pin an extra taskbar button.
func taskbarBadgeRegisterTab(attempt int) bool {
	return attempt == taskbarBadgeOverlayTries
}

// taskbarBadgeResult is the outcome of one overlay attempt.
type taskbarBadgeResult int

const (
	// taskbarBadgeApplied means the count is on the taskbar. The worker stops.
	taskbarBadgeApplied taskbarBadgeResult = iota
	// taskbarBadgeNoWindow means the top-level window does not exist yet.
	taskbarBadgeNoWindow
	// taskbarBadgeOverlayMiss means the window exists but SetOverlayIcon
	// did not stick. The worker keeps trying.
	taskbarBadgeOverlayMiss
	// taskbarBadgeWindowHidden means the frame is up but has no taskbar
	// button. Hide removes the button and the overlay with it; the count
	// has not changed, so the worker keeps the last count until the window
	// is shown again.
	taskbarBadgeWindowHidden
)

// taskbarBadgeHiddenRetryWait is how often a hidden window is checked.
// The taskbar button appears with the window, so the wait stays short
// instead of falling through to the slow miss poll.
const taskbarBadgeHiddenRetryWait = 250 * time.Millisecond

// taskbarBadgeShowGrace bounds the wait for SW_HIDE. A show that lands
// before the first check is still painted once this elapses.
const taskbarBadgeShowGrace = 500 * time.Millisecond

// taskbarBadgeShowGate decides whether this attempt may paint. A positive
// count on a hidden window waits. After WindowHide, a still-visible window
// also waits until the hide is observed, or until the grace elapses because
// the window was shown again first.
func taskbarBadgeShowGate(count int, visible bool, phase taskbarBadgeShowPhase, armed, now time.Time) (taskbarBadgeShowPhase, taskbarBadgeResult, bool) {
	if count <= 0 {
		return taskbarBadgeShowIdle, 0, false
	}
	switch phase {
	case taskbarBadgeShowWaitHide:
		if !visible {
			return taskbarBadgeShowWaitShow, taskbarBadgeWindowHidden, true
		}
		if !armed.IsZero() && !now.Before(armed.Add(taskbarBadgeShowGrace)) {
			return taskbarBadgeShowIdle, 0, false
		}
		return phase, taskbarBadgeWindowHidden, true
	case taskbarBadgeShowWaitShow:
		if !visible {
			return phase, taskbarBadgeWindowHidden, true
		}
		return taskbarBadgeShowIdle, 0, false
	default:
		if !visible {
			return phase, taskbarBadgeWindowHidden, true
		}
		return phase, 0, false
	}
}

func taskbarBadgeHoldShow(count int, visible bool) (taskbarBadgeResult, bool) {
	taskbarBadgeMu.Lock()
	defer taskbarBadgeMu.Unlock()
	next, result, hold := taskbarBadgeShowGate(count, visible, taskbarBadgeShow, taskbarBadgeShowArmed, time.Now())
	taskbarBadgeShow = next
	if next == taskbarBadgeShowIdle {
		taskbarBadgeShowArmed = time.Time{}
	}
	return result, hold
}

// taskbarBadgeServeCount renders and applies one badge on the worker thread.
// NoWindow means the frame is not up yet. OverlayMiss means the button
// rejected the icon and the worker should try again. A panic is swallowed
// so a badge cannot take the process down.
func taskbarBadgeServeCount(taskbar unsafe.Pointer, count int, liveIcon uintptr, allowAddTab bool) (nextLive uintptr, result taskbarBadgeResult) {
	var staged uintptr
	committed := false
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[taskbar-badge] panic suppressed: %v", r)
			if !committed && staged != 0 {
				procBadgeDestroyIcon.Call(staged)
			}
			if !committed {
				nextLive = liveIcon
				result = taskbarBadgeApplied
			}
		}
	}()
	cached := taskbarBadgeCachedHWND()
	var candidates []uintptr
	if cached == 0 {
		candidates = taskbarBadgeFreshCandidates()
		if len(candidates) == 0 {
			return taskbarBadgeNoWindowResult(count, liveIcon)
		}
	}
	hwnd := cached
	if hwnd == 0 && len(candidates) > 0 {
		hwnd = candidates[0]
	}
	if result, hold := taskbarBadgeHoldShow(count, taskbarBadgeHWNDVisible(hwnd)); hold {
		// Keep the frame we already found. The hidden retry would otherwise
		// enumerate every top-level window several times a second.
		if hwnd != 0 {
			taskbarBadgeRememberHWND(hwnd)
		}
		return liveIcon, result
	}
	staged = taskbarBadgeRenderIconFor(count, hwnd)
	if staged == 0 && count > 0 {
		// Render failed. Leave the current overlay in place and try again.
		return liveIcon, taskbarBadgeOverlayMiss
	}
	desc, _ := syscall.UTF16PtrFromString(taskbarBadgeDescriptionText())
	keep := func(hwnd uintptr) {
		taskbarBadgeRememberHWND(hwnd)
		nextLive = staged
		result = taskbarBadgeApplied
		committed = true
		taken := staged
		staged = 0
		if liveIcon != 0 && liveIcon != taken {
			procBadgeDestroyIcon.Call(liveIcon)
		}
	}
	try := func(hwnd uintptr) bool {
		if taskbarBadgeSetOverlay(taskbar, hwnd, staged, desc) != 0 {
			return false
		}
		keep(hwnd)
		return true
	}
	drop := func() {
		if staged == 0 {
			return
		}
		icon := staged
		staged = 0
		procBadgeDestroyIcon.Call(icon)
	}
	if cached != 0 && try(cached) {
		return nextLive, result
	}
	if cached != 0 {
		// The cached handle is still a window, but it is not the taskbar
		// button. Drop it and resolve the real top-level windows once.
		taskbarBadgeForgetHWND()
		candidates = taskbarBadgeFreshCandidates()
		if len(candidates) == 0 {
			drop()
			return taskbarBadgeNoWindowResult(count, liveIcon)
		}
	}
	// The cached call just failed. Calling SetOverlayIcon again on that
	// same handle in this attempt fails the same way.
	if candidates[0] != cached && try(candidates[0]) {
		return nextLive, result
	}
	if allowAddTab {
		hr := taskbarBadgeAddTabAndOverlay(taskbar, candidates[0], staged, desc)
		if hr == 0 {
			keep(candidates[0])
			return nextLive, result
		}
		log.Printf("[taskbar-badge] SetOverlayIcon failed: hr=0x%08x", uint32(hr))
		for _, hwnd := range candidates[1:] {
			if try(hwnd) {
				return nextLive, result
			}
		}
	}
	taskbarBadgeForgetHWND()
	drop()
	return liveIcon, taskbarBadgeOverlayMiss
}

// taskbarBadgeNoWindowResult handles a count that arrived with no frame.
// A positive count keeps waiting. A clear with nothing on the taskbar stops,
// and releases the icon we still hold.
func taskbarBadgeNoWindowResult(count int, liveIcon uintptr) (uintptr, taskbarBadgeResult) {
	if count > 0 {
		return liveIcon, taskbarBadgeNoWindow
	}
	if liveIcon != 0 {
		procBadgeDestroyIcon.Call(liveIcon)
	}
	return 0, taskbarBadgeApplied
}

func taskbarBadgeDescriptionText() string {
	name := strings.TrimSpace(brand.Current().DisplayName)
	if name == "" {
		return "MaClaw"
	}
	return name
}

// taskbarBadgeCreateTaskbarList3 creates and initializes the shell taskbar
// list object on the calling (COM-owning) thread.
func taskbarBadgeCreateTaskbarList3() (unsafe.Pointer, error) {
	var taskbar unsafe.Pointer
	hr, _, _ := procBadgeCoCreateInst.Call(
		uintptr(unsafe.Pointer(&clsidTaskbarList)),
		0,
		taskbarBadgeClsCtxInProcServer,
		uintptr(unsafe.Pointer(&iidITaskbarList3)),
		uintptr(unsafe.Pointer(&taskbar)),
	)
	if hr != 0 || taskbar == nil {
		return nil, fmt.Errorf("CoCreateInstance(ITaskbarList3) hr=0x%08x", uint32(hr))
	}
	if hr := taskbarBadgeVtableCall(taskbar, taskbarBadgeVtableHrInit); hr != 0 {
		taskbarBadgeVtableCall(taskbar, taskbarBadgeVtableRelease)
		return nil, fmt.Errorf("ITaskbarList3::HrInit hr=0x%08x", uint32(hr))
	}
	return taskbar, nil
}

// taskbarBadgeVtableProc resolves a COM interface method address: the
// interface pointer's first field is the vtable pointer, whose entries are
// plain function pointers.
func taskbarBadgeVtableProc(itf unsafe.Pointer, index int) uintptr {
	vtable := *(*unsafe.Pointer)(itf)
	return uintptr(*(*unsafe.Pointer)(unsafe.Add(vtable, uintptr(index)*unsafe.Sizeof(uintptr(0)))))
}

// taskbarBadgeVtableCall invokes a COM interface method through its vtable.
func taskbarBadgeVtableCall(itf unsafe.Pointer, index int, args ...uintptr) uintptr {
	callArgs := make([]uintptr, 0, len(args)+1)
	callArgs = append(callArgs, uintptr(itf))
	callArgs = append(callArgs, args...)
	hr, _, _ := syscall.SyscallN(taskbarBadgeVtableProc(itf, index), callArgs...)
	return hr
}

func taskbarBadgeHWNDVisible(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	vis, _, _ := procBadgeIsWindowVisible.Call(hwnd)
	return vis != 0
}

// noteTaskbarBadgeWindowHidden starts a replay of the last count. Hiding the
// main window destroys its taskbar button, and the overlay is not restored
// when the button comes back unless it is painted again.
func noteTaskbarBadgeWindowHidden() {
	taskbarBadgeMu.Lock()
	taskbarBadgeShow = taskbarBadgeShowWaitHide
	taskbarBadgeShowArmed = time.Now()
	taskbarBadgeMu.Unlock()
	replayTaskbarBadge()
}

func taskbarBadgeCachedHWND() uintptr {
	taskbarBadgeHWNDCacheMu.Lock()
	hwnd := taskbarBadgeHWNDCache
	taskbarBadgeHWNDCacheMu.Unlock()
	if hwnd == 0 {
		return 0
	}
	if alive, _, _ := procBadgeIsWindow.Call(hwnd); alive != 0 {
		return hwnd
	}
	taskbarBadgeForgetHWND()
	return 0
}

func taskbarBadgeRememberHWND(hwnd uintptr) {
	taskbarBadgeHWNDCacheMu.Lock()
	taskbarBadgeHWNDCache = hwnd
	taskbarBadgeHWNDCacheMu.Unlock()
}

func taskbarBadgeForgetHWND() {
	taskbarBadgeHWNDCacheMu.Lock()
	taskbarBadgeHWNDCache = 0
	taskbarBadgeHWNDCacheMu.Unlock()
}

// taskbarBadgeFreshCandidates lists taskbar-button windows, Wails frame
// first. The title scan can land on another visible top-level window, so it
// is only the fallback. Each handle is walked to its unowned root first.
func taskbarBadgeFreshCandidates() []uintptr {
	return taskbarBadgeCandidateOrder(
		taskbarBadgeRootOwner(findWailsWindowHWND()),
		taskbarBadgeRootOwner(findMainWindowHWND()),
	)
}

// taskbarBadgeCandidateOrder keeps the Wails frame ahead of the title-scan
// window and drops zeros and duplicates.
func taskbarBadgeCandidateOrder(wails, main uintptr) []uintptr {
	out := make([]uintptr, 0, 2)
	push := func(hwnd uintptr) {
		if hwnd == 0 {
			return
		}
		for _, have := range out {
			if have == hwnd {
				return
			}
		}
		out = append(out, hwnd)
	}
	push(wails)
	push(main)
	return out
}

// taskbarBadgeRootOwner walks to the unowned top-level window. Overlay icons
// attach to that taskbar button, not to an owned dialog.
func taskbarBadgeRootOwner(hwnd uintptr) uintptr {
	if hwnd == 0 {
		return 0
	}
	root, _, _ := procBadgeGetAncestor.Call(hwnd, 3) // GA_ROOTOWNER
	if root != 0 {
		return root
	}
	return hwnd
}

func taskbarBadgeSetOverlay(taskbar unsafe.Pointer, hwnd, icon uintptr, desc *uint16) uintptr {
	return taskbarBadgeVtableCall(taskbar, taskbarBadgeVtableSetOverlayIcon, hwnd, icon, uintptr(unsafe.Pointer(desc)))
}

// taskbarBadgeAddTabAndOverlay registers the window with the taskbar and
// paints the overlay. Used only after a plain SetOverlayIcon failed, and
// only for the preferred top-level window.
func taskbarBadgeAddTabAndOverlay(taskbar unsafe.Pointer, hwnd, icon uintptr, desc *uint16) uintptr {
	_ = taskbarBadgeVtableCall(taskbar, taskbarBadgeVtableAddTab, hwnd)
	return taskbarBadgeSetOverlay(taskbar, hwnd, icon, desc)
}

type taskbarBadgeBitmapInfoHeader struct {
	size          uint32
	width         int32
	height        int32
	planes        uint16
	bitCount      uint16
	compression   uint32
	sizeImage     uint32
	xPelsPerMeter int32
	yPelsPerMeter int32
	clrUsed       uint32
	clrImportant  uint32
}

type taskbarBadgeBitmapInfo struct {
	header taskbarBadgeBitmapInfoHeader
	colors [1]uint32
}

type taskbarBadgeRect struct {
	left, top, right, bottom int32
}

type taskbarBadgeIconInfo struct {
	fIcon    int32
	xHotspot uint32
	yHotspot uint32
	hbmMask  uintptr
	hbmColor uintptr
}

// taskbarBadgeRenderIcon draws the count as a dark-green disc with a bold
// white number and returns an ARGB HICON, or 0 for "no badge" (count <= 0).
func taskbarBadgeRenderIcon(count int) uintptr {
	return taskbarBadgeRenderIconFor(count, 0)
}

func taskbarBadgeRenderIconFor(count int, hwnd uintptr) uintptr {
	if count <= 0 {
		return 0
	}
	label := strconv.Itoa(count)
	if count > 99 {
		label = "99+"
	}
	size := taskbarBadgeIconSize(hwnd)

	var bits *byte
	bmi := taskbarBadgeBitmapInfo{header: taskbarBadgeBitmapInfoHeader{
		size:     uint32(unsafe.Sizeof(taskbarBadgeBitmapInfoHeader{})),
		width:    int32(size),
		height:   -int32(size), // negative: top-down rows, pixel 0 = top-left
		planes:   1,
		bitCount: 32,
	}}

	hdc, _, _ := procBadgeCreateCompatibleDC.Call(0)
	if hdc == 0 {
		return 0
	}
	defer procBadgeDeleteDC.Call(hdc)

	hbm, _, _ := procBadgeCreateDIBSection.Call(
		hdc,
		uintptr(unsafe.Pointer(&bmi)),
		0, // DIB_RGB_COLORS
		uintptr(unsafe.Pointer(&bits)),
		0, 0,
	)
	if hbm == 0 || bits == nil {
		if hbm != 0 {
			procBadgeDeleteObject.Call(hbm)
		}
		return 0
	}
	oldBmp, _, _ := procBadgeSelectObject.Call(hdc, hbm)

	// Fill the disc in the bitmap. GDI Ellipse antialiases against the cleared
	// black pixels and leaves alpha at 0; a later "any non-black pixel is
	// opaque" pass then turns that fringe into a black ring. A dark fill sits
	// close enough to black for the ring to eat the edge. Coverage alpha keeps
	// the outline color and a soft rim.
	taskbarBadgeFillDisc(bits, size)

	fontHeight := size * 11 / 16
	switch {
	case len(label) >= 3:
		fontHeight = size * 7 / 16
	case len(label) == 2:
		fontHeight = size * 9 / 16
	}
	face, _ := syscall.UTF16FromString("Segoe UI")
	font, _, _ := procBadgeCreateFont.Call(
		uintptr(uint32(int32(-fontHeight))),
		0, 0, 0,
		taskbarBadgeFontWeightBold,
		0, 0, 0,
		1, // DEFAULT_CHARSET
		0, 0,
		taskbarBadgeQualityAntialiase,
		0,
		uintptr(unsafe.Pointer(&face[0])),
	)
	oldFont, _, _ := procBadgeSelectObject.Call(hdc, font)
	procBadgeSetBkMode.Call(hdc, taskbarBadgeBkModeTransparent)
	procBadgeSetTextColor.Call(hdc, 0xFFFFFF)
	labelUTF16, _ := syscall.UTF16FromString(label)
	rect := taskbarBadgeRect{0, 0, int32(size), int32(size)}
	procBadgeDrawText.Call(
		hdc,
		uintptr(unsafe.Pointer(&labelUTF16[0])),
		uintptr(len(labelUTF16)-1),
		uintptr(unsafe.Pointer(&rect)),
		taskbarBadgeDtCenter|taskbarBadgeDtVcenter|taskbarBadgeDtSingleLine,
	)
	procBadgeSelectObject.Call(hdc, oldFont)
	procBadgeDeleteObject.Call(font)

	// DrawText blends the white digit into the disc and writes alpha 0 on
	// the pixels it touches. Restore the disc's own coverage everywhere the
	// color did not change, and make the glyph opaque. Forcing every
	// non-black pixel opaque would turn the dark rim into a black ring.
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			pixel := (*uint32)(unsafe.Add(unsafe.Pointer(bits), uintptr((y*size+x)*4)))
			*pixel = taskbarBadgeComposePixel(taskbarBadgeDiscPixel(x, y, size), *pixel)
		}
	}

	procBadgeSelectObject.Call(hdc, oldBmp)

	// CreateBitmap is bottom-up, so the mask's first row is the bottom of
	// the icon. A zero bit is opaque; a one bit is transparent. Filling only
	// the clear pixels keeps the disc visible when the shell ignores alpha.
	stride := ((size + 15) / 16) * 2
	maskBits := make([]byte, stride*size)
	taskbarBadgeFillMask(maskBits, size, func(x, y int) bool {
		pixel := *(*uint32)(unsafe.Add(unsafe.Pointer(bits), uintptr((y*size+x)*4)))
		return pixel>>24 >= 128
	})
	mask, _, _ := procBadgeCreateBitmap.Call(
		uintptr(size), uintptr(size), 1, 1,
		uintptr(unsafe.Pointer(&maskBits[0])),
	)
	if mask == 0 {
		procBadgeDeleteObject.Call(hbm)
		return 0
	}
	defer procBadgeDeleteObject.Call(mask)
	iconInfo := taskbarBadgeIconInfo{fIcon: 1, hbmMask: mask, hbmColor: hbm}
	icon, _, _ := procBadgeCreateIconIndirect.Call(uintptr(unsafe.Pointer(&iconInfo)))
	procBadgeDeleteObject.Call(hbm)
	return icon
}

// taskbarBadgeDiscPixel is one premultiplied BGRA pixel of the badge disc.
// The circle is inscribed in the size×size box. Coverage ramps across one
// pixel at the rim so the edge stays the icon green instead of a black fringe.
func taskbarBadgeDiscPixel(x, y, size int) uint32 {
	if size <= 0 || x < 0 || y < 0 || x >= size || y >= size {
		return 0
	}
	cx := float64(size) / 2
	dx := float64(x) + 0.5 - cx
	dy := float64(y) + 0.5 - cx
	coverage := cx - math.Hypot(dx, dy) + 0.5
	if coverage <= 0 {
		return 0
	}
	if coverage > 1 {
		coverage = 1
	}
	alpha := uint32(coverage*255 + 0.5)
	if alpha == 0 {
		return 0
	}
	if alpha > 255 {
		alpha = 255
	}
	red := taskbarBadgeFillR * alpha / 255
	green := taskbarBadgeFillG * alpha / 255
	blue := taskbarBadgeFillB * alpha / 255
	return alpha<<24 | red<<16 | green<<8 | blue
}

// taskbarBadgeComposePixel keeps the disc pixel when text did not change
// its color, including when GDI only cleared the alpha byte. A glyph pixel
// is made opaque.
func taskbarBadgeComposePixel(disc, drawn uint32) uint32 {
	if drawn == disc || drawn&0xFFFFFF == 0 || drawn&0xFFFFFF == disc&0xFFFFFF {
		return disc
	}
	return drawn | 0xFF000000
}

func taskbarBadgeFillDisc(bits *byte, size int) {
	if bits == nil || size <= 0 {
		return
	}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			px := taskbarBadgeDiscPixel(x, y, size)
			if px == 0 {
				continue
			}
			*(*uint32)(unsafe.Add(unsafe.Pointer(bits), uintptr((y*size+x)*4))) = px
		}
	}
}

// taskbarBadgeFillMask writes a bottom-up monochrome AND mask. Opaque pixels
// stay 0. Transparent pixels get a 1 in MSB-first order, which is what
// CreateBitmap expects.
func taskbarBadgeFillMask(mask []byte, size int, opaque func(x, y int) bool) {
	if size <= 0 || opaque == nil {
		return
	}
	stride := ((size + 15) / 16) * 2
	if len(mask) < stride*size {
		return
	}
	for y := 0; y < size; y++ {
		srcY := size - 1 - y
		row := mask[y*stride : y*stride+stride]
		for x := 0; x < size; x++ {
			if opaque(x, srcY) {
				continue
			}
			row[x/8] |= 0x80 >> (uint(x) % 8)
		}
	}
}

func taskbarBadgeIconSize(hwnd uintptr) int {
	const max = 128
	if hwnd != 0 && procBadgeGetDpiForWindow.Find() == nil && procBadgeSystemMetricsForDpi.Find() == nil {
		dpi, _, _ := procBadgeGetDpiForWindow.Call(hwnd)
		if dpi != 0 {
			if v, _, _ := procBadgeSystemMetricsForDpi.Call(uintptr(taskbarBadgeSmCxIcon), dpi); v > 0 && int(v) <= max {
				return int(v)
			}
		}
	}
	size := int(taskbarBadgeSystemMetrics(taskbarBadgeSmCxIcon))
	if size <= 0 || size > max {
		return 32
	}
	return size
}

func taskbarBadgeSystemMetrics(index int) int32 {
	v, _, _ := procBadgeSystemMetrics.Call(uintptr(index))
	return int32(v)
}
