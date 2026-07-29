//go:build windows

package main

import (
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/brand"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Win10 frameless + translucent maximise often paints past the monitor work area
// (under the taskbar / off-screen edges). Clamp the maximized HWND to the
// monitor's rcWork so chrome stays visible.

var (
	workAreaUser32                 = syscall.NewLazyDLL("user32.dll")
	workAreaKernel32               = syscall.NewLazyDLL("kernel32.dll")
	procIsZoomedWA                 = workAreaUser32.NewProc("IsZoomed")
	procIsIconicWA                 = workAreaUser32.NewProc("IsIconic")
	procGetWindowWA                = workAreaUser32.NewProc("GetWindow")
	procGetWindowRectWA            = workAreaUser32.NewProc("GetWindowRect")
	procSetWindowPosWA             = workAreaUser32.NewProc("SetWindowPos")
	procMonitorFromWindowWA        = workAreaUser32.NewProc("MonitorFromWindow")
	procGetMonitorInfoWWA          = workAreaUser32.NewProc("GetMonitorInfoW")
	procEnumWindowsWA              = workAreaUser32.NewProc("EnumWindows")
	procGetWindowTextWWA           = workAreaUser32.NewProc("GetWindowTextW")
	procGetWindowThreadProcessIdWA = workAreaUser32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisibleWA          = workAreaUser32.NewProc("IsWindowVisible")
	procGetCurrentProcessIdWA      = workAreaKernel32.NewProc("GetCurrentProcessId")
)

const (
	monitorDefaultToNearest = 2
	swpNoZOrder             = 0x0004
	swpNoActivate           = 0x0010
	gwOwner                 = 4 // GW_OWNER — skip owned popups/tool windows
)

type workAreaRect struct {
	Left, Top, Right, Bottom int32
}

type workAreaMonitorInfo struct {
	CbSize    uint32
	RcMonitor workAreaRect
	RcWork    workAreaRect
	DwFlags   uint32
}

var (
	mainHWNDCacheMu sync.Mutex
	mainHWNDCache   uintptr

	// findMainWindowMu serialises EnumWindows searches (shared enumSearch state).
	findMainWindowMu sync.Mutex

	// clampScheduleGen drops superseded delayed clamps (domReady + toggle + resize).
	clampScheduleGen atomic.Uint64

	// clampRunMu + lastClampAt throttle concurrent immediate clamps (FE resize + BE schedule).
	clampRunMu  sync.Mutex
	lastClampAt time.Time
	clampMinGap = 40 * time.Millisecond

	// enumSearch holds state for the single EnumWindows callback.
	// Go's syscall.NewCallback pins forever — create it once only.
	enumSearch struct {
		mu          sync.Mutex
		pid         uintptr
		wantTitle   string
		found       uintptr
		foundZoomed uintptr
	}
	enumWindowsCBOnce sync.Once
	enumWindowsCB     uintptr
)

func ensureEnumWindowsCallback() uintptr {
	enumWindowsCBOnce.Do(func() {
		enumWindowsCB = syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
			enumSearch.mu.Lock()
			pid := enumSearch.pid
			want := enumSearch.wantTitle
			enumSearch.mu.Unlock()

			var wpid uint32
			procGetWindowThreadProcessIdWA.Call(hwnd, uintptr(unsafe.Pointer(&wpid)))
			if uintptr(wpid) != pid {
				return 1
			}
			vis, _, _ := procIsWindowVisibleWA.Call(hwnd)
			if vis == 0 {
				return 1
			}
			// Skip owned windows (dialogs / floating helpers), not the main shell.
			owner, _, _ := procGetWindowWA.Call(hwnd, gwOwner)
			if owner != 0 {
				return 1
			}
			iconic, _, _ := procIsIconicWA.Call(hwnd)
			if iconic != 0 {
				return 1
			}
			buf := make([]uint16, 512)
			n, _, _ := procGetWindowTextWWA.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
			if n == 0 {
				return 1
			}
			name := syscall.UTF16ToString(buf)
			if !windowTitleMatchesMain(name, want) {
				return 1
			}
			enumSearch.mu.Lock()
			enumSearch.found = hwnd
			z, _, _ := procIsZoomedWA.Call(hwnd)
			if z != 0 {
				enumSearch.foundZoomed = hwnd
				enumSearch.mu.Unlock()
				return 0 // stop: prefer maximised main window
			}
			enumSearch.mu.Unlock()
			return 1
		})
	})
	return enumWindowsCB
}

// scheduleClampMaximizedWindowToWorkArea runs clamp after delay so the OS can
// finish ShowWindow(SW_MAXIMIZE) geometry first. Later schedules cancel earlier ones.
func (a *App) scheduleClampMaximizedWindowToWorkArea(delay time.Duration) {
	if a == nil {
		return
	}
	gen := clampScheduleGen.Add(1)
	go func() {
		if delay > 0 {
			time.Sleep(delay)
		}
		if clampScheduleGen.Load() != gen {
			return
		}
		a.ClampMaximizedWindowToWorkArea()
	}()
}

// ClampMaximizedWindowToWorkArea fits a maximised main window into the monitor
// work area (excludes taskbar). Safe no-op when not maximised / HWND not found.
// Bound for the frontend after maximise toggles.
func (a *App) ClampMaximizedWindowToWorkArea() {
	if a == nil || a.ctx == nil {
		return
	}

	hwnd := findMainWindowHWND()
	if hwnd == 0 {
		return
	}
	// Never fight a minimised window.
	if iconic, _, _ := procIsIconicWA.Call(hwnd); iconic != 0 {
		return
	}

	maximised := wailsruntime.WindowIsMaximised(a.ctx)
	if !maximised {
		z, _, _ := procIsZoomedWA.Call(hwnd)
		if z == 0 {
			return
		}
	}

	// Throttle only after we know work is needed (max path), so a no-op call
	// does not suppress a real maximise clamp arriving a few ms later.
	clampRunMu.Lock()
	if !lastClampAt.IsZero() && time.Since(lastClampAt) < clampMinGap {
		clampRunMu.Unlock()
		return
	}
	lastClampAt = time.Now()
	clampRunMu.Unlock()

	if clampHWNDToWorkArea(hwnd) {
		log.Printf("[window] clamped maximised window to monitor work area")
	}
}

func clampHWNDToWorkArea(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	work, ok := monitorWorkAreaForHWND(hwnd)
	if !ok {
		return false
	}
	w := work.Right - work.Left
	h := work.Bottom - work.Top
	if w <= 0 || h <= 0 {
		return false
	}

	var cur workAreaRect
	if ok, _, _ := procGetWindowRectWA.Call(hwnd, uintptr(unsafe.Pointer(&cur))); ok == 0 {
		invalidateMainHWNDCache(hwnd)
		return false
	}
	if !windowRectOverflowsWorkArea(cur, work) {
		return false
	}

	if !setWindowRect(hwnd, work) {
		return false
	}
	// Some Win10 builds re-apply maximise frame after SetWindowPos; retry once.
	var after workAreaRect
	if ok, _, _ := procGetWindowRectWA.Call(hwnd, uintptr(unsafe.Pointer(&after))); ok != 0 {
		if windowRectOverflowsWorkArea(after, work) {
			time.Sleep(30 * time.Millisecond)
			return setWindowRect(hwnd, work)
		}
	}
	return true
}

func monitorWorkAreaForHWND(hwnd uintptr) (workAreaRect, bool) {
	hmon, _, _ := procMonitorFromWindowWA.Call(hwnd, monitorDefaultToNearest)
	if hmon == 0 {
		return workAreaRect{}, false
	}
	var mi workAreaMonitorInfo
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	r, _, _ := procGetMonitorInfoWWA.Call(hmon, uintptr(unsafe.Pointer(&mi)))
	if r == 0 {
		return workAreaRect{}, false
	}
	return mi.RcWork, true
}

func windowRectOverflowsWorkArea(cur, work workAreaRect) bool {
	// Already matches work area (or within 1px noise).
	if abs32(cur.Left-work.Left) <= 1 && abs32(cur.Top-work.Top) <= 1 &&
		abs32(cur.Right-work.Right) <= 1 && abs32(cur.Bottom-work.Bottom) <= 1 {
		return false
	}
	// Overflow under taskbar / off-screen edges.
	return cur.Left < work.Left-1 || cur.Top < work.Top-1 ||
		cur.Right > work.Right+1 || cur.Bottom > work.Bottom+1
}

func setWindowRect(hwnd uintptr, work workAreaRect) bool {
	w := work.Right - work.Left
	h := work.Bottom - work.Top
	if w <= 0 || h <= 0 {
		return false
	}
	flags := uintptr(swpNoZOrder | swpNoActivate)
	ret, _, _ := procSetWindowPosWA.Call(
		hwnd, 0,
		uintptr(work.Left), uintptr(work.Top),
		uintptr(w), uintptr(h),
		flags,
	)
	return ret != 0
}

func invalidateMainHWNDCache(hwnd uintptr) {
	mainHWNDCacheMu.Lock()
	if hwnd == 0 || mainHWNDCache == hwnd {
		mainHWNDCache = 0
	}
	mainHWNDCacheMu.Unlock()
}

func findMainWindowHWND() uintptr {
	// Fast path: cached HWND still valid for this process + visible.
	mainHWNDCacheMu.Lock()
	cached := mainHWNDCache
	mainHWNDCacheMu.Unlock()
	if cached != 0 && hwndStillUsable(cached) {
		return cached
	}
	if cached != 0 {
		invalidateMainHWNDCache(cached)
	}

	// One EnumWindows at a time — callback state is package-global.
	findMainWindowMu.Lock()
	defer findMainWindowMu.Unlock()

	// Re-check cache after waiting: another finder may have filled it.
	mainHWNDCacheMu.Lock()
	cached = mainHWNDCache
	mainHWNDCacheMu.Unlock()
	if cached != 0 && hwndStillUsable(cached) {
		return cached
	}

	pid, _, _ := procGetCurrentProcessIdWA.Call()
	wantTitle := strings.TrimSpace(brand.Current().WindowTitle)

	enumSearch.mu.Lock()
	enumSearch.pid = pid
	enumSearch.wantTitle = wantTitle
	enumSearch.found = 0
	enumSearch.foundZoomed = 0
	enumSearch.mu.Unlock()

	procEnumWindowsWA.Call(ensureEnumWindowsCallback(), 0)

	enumSearch.mu.Lock()
	result := enumSearch.found
	if enumSearch.foundZoomed != 0 {
		result = enumSearch.foundZoomed
	}
	enumSearch.mu.Unlock()

	if result != 0 {
		mainHWNDCacheMu.Lock()
		mainHWNDCache = result
		mainHWNDCacheMu.Unlock()
	}
	return result
}

func hwndStillUsable(hwnd uintptr) bool {
	if !hwndBelongsToCurrentProcess(hwnd) {
		return false
	}
	vis, _, _ := procIsWindowVisibleWA.Call(hwnd)
	if vis == 0 {
		return false
	}
	if iconic, _, _ := procIsIconicWA.Call(hwnd); iconic != 0 {
		// Minimised: treat as unusable for clamp cache (force re-find when restored).
		return false
	}
	// Reject dead HWNDs: GetWindowRect fails when the window is gone.
	var cur workAreaRect
	ok, _, _ := procGetWindowRectWA.Call(hwnd, uintptr(unsafe.Pointer(&cur)))
	return ok != 0
}

func hwndBelongsToCurrentProcess(hwnd uintptr) bool {
	pid, _, _ := procGetCurrentProcessIdWA.Call()
	var wpid uint32
	procGetWindowThreadProcessIdWA.Call(hwnd, uintptr(unsafe.Pointer(&wpid)))
	return uintptr(wpid) == pid
}

func windowTitleMatchesMain(actual, want string) bool {
	actual = strings.TrimSpace(actual)
	if actual == "" {
		return false
	}
	if want != "" {
		if actual == want || strings.HasPrefix(actual, want) {
			return true
		}
		// Title may be "Brand — path" or path in title bar.
		if strings.Contains(actual, want) && len(want) >= 4 {
			return true
		}
	}
	// OEM / fallback brands still share MaClaw-family titles in some builds.
	lower := strings.ToLower(actual)
	return strings.Contains(lower, "maclaw") ||
		strings.Contains(lower, "tigerclaw") ||
		strings.Contains(lower, "metastaff") ||
		strings.Contains(lower, "码卡龙")
}
