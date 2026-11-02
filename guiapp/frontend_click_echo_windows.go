//go:build windows

package guiapp

import (
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Native click-echo monitor for the dead WebView2 input pipeline
// (frontend_input_watch.go). In that failure mode the OS still delivers mouse
// messages to our HWND — WebView2 drops them afterwards — so the page cannot
// detect the freeze itself. A WH_MOUSE_LL hook sees every click before any
// window consumes it; comparing clicks targeted at the main window against
// page-reported input detects the freeze within seconds instead of the
// multi-minute tenure the idle-based watchdog needs.
//
// The hook runs on a dedicated locked OS thread with its own GetMessage pump:
// WH_MOUSE_LL callbacks are delivered to the installing thread's message loop,
// and Go goroutines migrate threads, so installation from an ordinary goroutine
// would silently never fire.

const (
	whMouseLL        = 14
	wmLLButtonDown   = 0x0201
	wmRButtonDown    = 0x0204
	wmMButtonDown    = 0x0207
	gaRoot           = 2
	clickEchoBufSize = 64
)

var (
	clickEchoUser32 = windows.NewLazySystemDLL("user32.dll")
	procSetHookCE   = clickEchoUser32.NewProc("SetWindowsHookExW")
	procUnhookCE    = clickEchoUser32.NewProc("UnhookWindowsHookEx")
	procNextHookCE  = clickEchoUser32.NewProc("CallNextHookEx")
	procGetMsgCE    = clickEchoUser32.NewProc("GetMessageW")
	procWinFromPtCE = clickEchoUser32.NewProc("WindowFromPoint")
	procGetAncestor = clickEchoUser32.NewProc("GetAncestor")

	clickEchoHookHandle uintptr
	clickEchoRunning    atomic.Bool
	clickEchoCallback   uintptr // keeps the syscall callback alive
	// clickEchoMainHWND is the resolved main-window HWND the callback compares
	// against. Resolved OUTSIDE the callback: findMainWindowHWND can fall back
	// to EnumWindows (its cache is invalidated while the window is minimized,
	// e.g. during a nudge), and a slow hook callback risks Windows disabling
	// the low-level hook.
	clickEchoMainHWND   atomic.Uintptr
	clickEchoResolveDue atomic.Int64

	clickEchoMu       sync.Mutex
	clickEchoStamps   [clickEchoBufSize]time.Time
	clickEchoCount    int
	clickEchoNextSlot int
)

type clickEchoPoint struct {
	X, Y int32
}

// clickEchoHookMsg mirrors MSLLHOOKSTRUCT up to the fields we read.
type clickEchoHookMsg struct {
	Pt          clickEchoPoint
	MouseData   uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

// clickEchoHookProc is the WH_MOUSE_LL callback. Keep it cheap: a hit-test
// (WindowFromPoint/GetAncestor) plus one atomic load and a ring-buffer append.
// No window enumeration here — slow low-level hook callbacks get disabled by
// Windows.
func clickEchoHookProc(nCode int32, wParam uintptr, lParam uintptr) uintptr {
	if nCode >= 0 && clickEchoRunning.Load() {
		switch wParam {
		case wmLLButtonDown, wmRButtonDown, wmMButtonDown:
			msg := (*clickEchoHookMsg)(unsafe.Pointer(lParam))
			hwnd, _, _ := procWinFromPtCE.Call(
				uintptr(unsafe.Pointer(&clickEchoPoint{X: msg.Pt.X, Y: msg.Pt.Y})))
			if hwnd != 0 {
				root, _, _ := procGetAncestor.Call(hwnd, gaRoot)
				if root != 0 && root == clickEchoMainHWND.Load() {
					clickEchoRecord(time.Now())
				}
			}
		}
	}
	ret, _, _ := procNextHookCE.Call(0, uintptr(nCode), wParam, lParam)
	return ret
}

// clickEchoEnsureHWND refreshes the callback's comparison HWND at most once
// per 30s from the watchdog sampler (never from the hook callback). When the
// main window cannot be resolved yet (early startup) the callback simply
// records nothing until it appears.
func clickEchoEnsureHWND() {
	now := time.Now().Unix()
	if clickEchoMainHWND.Load() != 0 && now < clickEchoResolveDue.Load() {
		return
	}
	if hwnd := findMainWindowHWND(); hwnd != 0 {
		clickEchoMainHWND.Store(hwnd)
	}
	clickEchoResolveDue.Store(now + 30)
}

func clickEchoRecord(now time.Time) {
	clickEchoMu.Lock()
	clickEchoStamps[clickEchoNextSlot] = now
	clickEchoNextSlot = (clickEchoNextSlot + 1) % clickEchoBufSize
	if clickEchoCount < clickEchoBufSize {
		clickEchoCount++
	}
	clickEchoMu.Unlock()
}

// startClickEchoMonitor installs the low-level mouse hook and pumps messages on
// a dedicated locked thread. Safe to call once at startup; a failure leaves the
// monitor disabled and the watchdog falls back to idle-based detection only.
func startClickEchoMonitor() {
	if clickEchoRunning.Load() {
		return
	}
	clickEchoCallback = windows.NewCallback(clickEchoHookProc)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		hook, _, err := procSetHookCE.Call(whMouseLL, clickEchoCallback, 0, 0)
		if hook == 0 {
			bootLog("click-echo monitor hook install failed err=%v", err)
			return
		}
		clickEchoHookHandle = hook
		clickEchoRunning.Store(true)
		bootLog("click-echo monitor hook installed hwnd-callback ready")
		defer func() {
			clickEchoRunning.Store(false)
			procUnhookCE.Call(hook)
		}()
		var msg struct {
			Hwnd    uintptr
			Message uint32
			WParam  uintptr
			LParam  uintptr
			Time    uint32
			Pt      clickEchoPoint
		}
		for {
			r, _, _ := procGetMsgCE.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if r == 0 || int32(r) == -1 { // WM_QUIT or error
				return
			}
			// WH_MOUSE_LL needs no TranslateMessage/DispatchMessage: the hook
			// callback fires during the pump itself.
		}
	}()
}

func clickEchoMonitorRunning() bool { return clickEchoRunning.Load() }

// windowClickStamps returns recorded click timestamps within the last window,
// oldest first. Shared by the echo watchdog and the latency matcher.
func windowClickStamps(window time.Duration) []time.Time {
	cutoff := time.Now().Add(-window)
	clickEchoMu.Lock()
	out := make([]time.Time, 0, clickEchoCount)
	for i := 0; i < clickEchoCount; i++ {
		if !clickEchoStamps[i].Before(cutoff) {
			out = append(out, clickEchoStamps[i])
		}
	}
	clickEchoMu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

// recentWindowClicks counts recorded clicks on the main window within the last
// window duration.
func recentWindowClicks(window time.Duration) int {
	return len(windowClickStamps(window))
}

// oldestWindowClickAge returns the age of the oldest recorded click still
// inside the window, or 0 when the buffer has no click in the window. Callers
// compare it against pageIdle: page silence older than the click evidence
// means the clicks never reached the DOM.
func oldestWindowClickAge(window time.Duration) time.Duration {
	stamps := windowClickStamps(window)
	if len(stamps) == 0 {
		return 0
	}
	return time.Since(stamps[0])
}
