//go:build windows

package guiapp

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
	procGetWindowPlacementWA       = workAreaUser32.NewProc("GetWindowPlacement")
	procSetWindowPlacementWA       = workAreaUser32.NewProc("SetWindowPlacement")
	procMonitorFromWindowWA        = workAreaUser32.NewProc("MonitorFromWindow")
	procMonitorFromRectWA          = workAreaUser32.NewProc("MonitorFromRect")
	procGetMonitorInfoWWA          = workAreaUser32.NewProc("GetMonitorInfoW")
	procEnumWindowsWA              = workAreaUser32.NewProc("EnumWindows")
	procGetWindowTextWWA           = workAreaUser32.NewProc("GetWindowTextW")
	procGetWindowThreadProcessIdWA = workAreaUser32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisibleWA          = workAreaUser32.NewProc("IsWindowVisible")
	procGetCurrentProcessIdWA      = workAreaKernel32.NewProc("GetCurrentProcessId")
)

const (
	monitorDefaultToNearest = 2
	swpNoSize               = 0x0001
	swpNoZOrder             = 0x0004
	swpNoActivate           = 0x0010
	gwOwner                 = 4 // GW_OWNER — skip owned popups/tool windows
	swShowMaximized         = 3 // SW_SHOWMAXIMIZED — keep clamp from restoring
)

type workAreaRect struct {
	Left, Top, Right, Bottom int32
}

type winPoint struct {
	X, Y int32
}

// WINDOWPLACEMENT without the Windows 10 rcDevice tail. Length is set at call time.
type windowPlacement struct {
	Length         uint32
	Flags          uint32
	ShowCmd        uint32
	MinPosition    winPoint
	MaxPosition    winPoint
	NormalPosition workAreaRect
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

	savedPlace, havePlace := readWindowPlacement(hwnd)
	if !setWindowRect(hwnd, work) {
		return false
	}
	// Some Win10 builds re-apply maximise frame after SetWindowPos; retry once.
	if hwndOverflowsWorkArea(hwnd, work) {
		time.Sleep(30 * time.Millisecond)
		if !setWindowRect(hwnd, work) {
			return false
		}
	}
	if havePlace {
		preserveClampedNormalPlacement(hwnd, savedPlace.NormalPosition, work)
		// SetWindowPlacement can re-apply the maximize frame past the taskbar.
		if hwndOverflowsWorkArea(hwnd, work) {
			if !setWindowRect(hwnd, work) {
				return false
			}
			preserveClampedNormalPlacement(hwnd, savedPlace.NormalPosition, work)
		}
	}
	return true
}

func readWindowPlacement(hwnd uintptr) (windowPlacement, bool) {
	var place windowPlacement
	place.Length = uint32(unsafe.Sizeof(place))
	r, _, _ := procGetWindowPlacementWA.Call(hwnd, uintptr(unsafe.Pointer(&place)))
	if r == 0 {
		return windowPlacement{}, false
	}
	return place, true
}

func preserveClampedNormalPlacement(hwnd uintptr, savedNormal, work workAreaRect) {
	if !shouldPreserveClampedNormalPlacement(savedNormal, work) {
		return
	}
	cur, ok := readWindowPlacement(hwnd)
	if !ok {
		return
	}
	cur = applyPreservedNormalPlacement(cur, savedNormal)
	procSetWindowPlacementWA.Call(hwnd, uintptr(unsafe.Pointer(&cur)))
}

func applyPreservedNormalPlacement(cur windowPlacement, savedNormal workAreaRect) windowPlacement {
	cur.Length = uint32(unsafe.Sizeof(cur))
	cur.NormalPosition = savedNormal
	// SetWindowPos during clamp can clear WS_MAXIMIZE. Leaving ShowCmd as
	// SW_SHOWNORMAL would restore the window instead of recording the rect.
	cur.ShowCmd = swShowMaximized
	return cur
}

func hwndOverflowsWorkArea(hwnd uintptr, work workAreaRect) bool {
	var cur workAreaRect
	if ok, _, _ := procGetWindowRectWA.Call(hwnd, uintptr(unsafe.Pointer(&cur))); ok == 0 {
		return false
	}
	return windowRectOverflowsWorkArea(cur, work)
}

func shouldPreserveClampedNormalPlacement(savedNormal, work workAreaRect) bool {
	w := savedNormal.Right - savedNormal.Left
	h := savedNormal.Bottom - savedNormal.Top
	if w < 160 || h < 120 {
		return false
	}
	ww := work.Right - work.Left
	hh := work.Bottom - work.Top
	if ww <= 0 || hh <= 0 {
		return false
	}
	// Keep a restore rect that is smaller on either axis and still on this
	// monitor. An off-screen rect is already corrupted; writing it back is what
	// makes unmaximize slide the window off a display left of the primary.
	if !rectIntersects(savedNormal, work) {
		return false
	}
	return w < ww-16 || h < hh-16
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

// SetMainWindowScreenPosition moves the main window to a screen point from
// WindowGetPosition / GetWindowRect.
//
// Wails WindowSetPosition on Windows adds the current monitor work-area origin.
// WindowGetPosition is already an absolute screen point, so on a second display
// that origin is applied twice and the restored window leaves the virtual desktop.
// A display left of the primary has a negative origin, which is the slide off
// the left edge.
func (a *App) SetMainWindowScreenPosition(x, y int) {
	if a == nil {
		return
	}
	setMainWindowScreenPosition(x, y)
}

func setMainWindowScreenPosition(x, y int) bool {
	hwnd := findMainWindowHWND()
	if hwnd == 0 {
		log.Printf("[window] screen position %d,%d skipped: main window not found", x, y)
		return false
	}
	tx, ty := x, y
	var cur workAreaRect
	if ok, _, _ := procGetWindowRectWA.Call(hwnd, uintptr(unsafe.Pointer(&cur))); ok != 0 {
		w := int(cur.Right - cur.Left)
		h := int(cur.Bottom - cur.Top)
		if info, found := monitorInfoForRect(rectFromOrigin(x, y, w, h)); found {
			tx, ty = placeOriginInWorkArea(x, y, w, h, info.RcMonitor, info.RcWork)
		}
		if abs32(cur.Left-int32(tx)) <= 1 && abs32(cur.Top-int32(ty)) <= 1 {
			return true
		}
		if tx != x || ty != y {
			log.Printf("[window] restore position %d,%d misses every display; using %d,%d", x, y, tx, ty)
		}
	}
	if setWindowScreenPos(hwnd, tx, ty) {
		return true
	}
	log.Printf("[window] screen position %d,%d failed", tx, ty)
	return false
}

func setWindowScreenPos(hwnd uintptr, x, y int) bool {
	px, py := screenPositionParams(x, y)
	flags := uintptr(swpNoSize | swpNoZOrder | swpNoActivate)
	ret, _, _ := procSetWindowPosWA.Call(hwnd, 0, px, py, 0, 0, flags)
	return ret != 0
}

func monitorInfoForRect(r workAreaRect) (workAreaMonitorInfo, bool) {
	hmon, _, _ := procMonitorFromRectWA.Call(uintptr(unsafe.Pointer(&r)), monitorDefaultToNearest)
	if hmon == 0 {
		return workAreaMonitorInfo{}, false
	}
	var mi workAreaMonitorInfo
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	if ok, _, _ := procGetMonitorInfoWWA.Call(hmon, uintptr(unsafe.Pointer(&mi))); ok == 0 {
		return workAreaMonitorInfo{}, false
	}
	return mi, true
}

func rectFromOrigin(x, y, width, height int) workAreaRect {
	return workAreaRect{
		Left: int32(x), Top: int32(y),
		Right: int32(x + width), Bottom: int32(y + height),
	}
}

func rectIntersects(a, b workAreaRect) bool {
	return a.Left < b.Right && a.Right > b.Left && a.Top < b.Bottom && a.Bottom > b.Top
}

// placeOriginInWorkArea keeps a restore point that already intersects the
// target monitor. A point that misses it is shifted into the work area.
func placeOriginInWorkArea(x, y, width, height int, monitor, work workAreaRect) (int, int) {
	if width <= 0 || height <= 0 || rectIntersects(rectFromOrigin(x, y, width, height), monitor) {
		return x, y
	}
	workLeft, workTop := int(work.Left), int(work.Top)
	workRight, workBottom := int(work.Right), int(work.Bottom)
	if x+width > workRight {
		x -= (x + width) - workRight
	}
	if x < workLeft {
		x = workLeft
	}
	if y+height > workBottom {
		y -= (y + height) - workBottom
	}
	if y < workTop {
		y = workTop
	}
	return x, y
}

// screenPositionParams keeps a negative screen origin sign-extended for
// SetWindowPos. A second display left of the primary has a negative X.
func screenPositionParams(x, y int) (uintptr, uintptr) {
	return uintptr(int32(x)), uintptr(int32(y))
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
		strings.Contains(lower, "qagent") ||
		strings.Contains(lower, "metastaff") ||
		strings.Contains(lower, "码卡龙")
}
