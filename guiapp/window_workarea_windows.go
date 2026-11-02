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
	procShowWindowRestore          = workAreaUser32.NewProc("ShowWindow")
	procGetWindowLongWRestore      = workAreaUser32.NewProc("GetWindowLongW")
	procSetWindowLongWRestore      = workAreaUser32.NewProc("SetWindowLongW")
	procSendMessageWRestore        = workAreaUser32.NewProc("SendMessageW")
	procGetClientRectRestore       = workAreaUser32.NewProc("GetClientRect")
)

const (
	monitorDefaultToNearest = 2
	swpNoSize               = 0x0001
	swpNoZOrder             = 0x0004
	swpNoActivate           = 0x0010
	gwOwner                 = 4 // GW_OWNER — skip owned popups/tool windows
	swShowMaximized         = 3 // SW_SHOWMAXIMIZED — keep clamp from restoring
	swShowNormal            = 1 // SW_SHOWNORMAL
	swRestore               = 9 // SW_RESTORE — leaves WS_MAXIMIZE before SetWindowPos
	swpFrameChanged         = 0x0020
	wsMaximizeStyle         = 0x01000000
	wmSizeRestore           = 0x0005
	sizeRestored            = 0
	spiGetWorkArea          = 0x0030 // primary monitor work origin; workspace coords use it
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

	// windowGeometryMu keeps clamp and restore from applying two rects at once.
	// The 80ms/250ms reassert otherwise overlaps the first restore.
	windowGeometryMu sync.Mutex

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
	// Same lock as restore, and acquired before findMainWindowHWND, so the
	// two calls cannot lock the enum mutex in opposite orders.
	windowGeometryMu.Lock()
	defer windowGeometryMu.Unlock()

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

// scaleLogicalToPhysical converts a Wails WindowGetSize value (96-DPI logical
// pixels) into the physical pixels SetWindowPos expects.
func scaleLogicalToPhysical(px int, dpi uint32) int {
	if px <= 0 {
		return 0
	}
	if dpi == 0 {
		dpi = defaultWindowsDPI
	}
	return (px * int(dpi)) / int(defaultWindowsDPI)
}

// planRestoredPlacement rewrites a placement that is still SW_SHOWMAXIMIZED.
// Any other show command, including minimized, is left alone. normal and
// target are both workspace coordinates. A rect already within 2px is kept
// so a rounding difference does not replace the OS workspace rect. Any other
// position or size is replaced so SetWindowPlacement cannot flash the
// work-area frame.
func planRestoredPlacement(current uint32, normal, target workAreaRect) (workAreaRect, uint32, bool) {
	if current != swShowMaximized {
		return normal, current, false
	}
	tw := int(target.Right - target.Left)
	th := int(target.Bottom - target.Top)
	if tw <= 0 || th <= 0 {
		return normal, current, false
	}
	if rectsNear(normal, target) {
		return normal, swShowNormal, true
	}
	return target, swShowNormal, true
}

func rectsNear(a, b workAreaRect) bool {
	return abs32(a.Left-b.Left) <= 2 && abs32(a.Top-b.Top) <= 2 &&
		abs32(a.Right-b.Right) <= 2 && abs32(a.Bottom-b.Bottom) <= 2
}

// RestoreMainWindowGeometry leaves the maximized state and applies the
// pre-maximize screen rect, then asks WebView2 to fill that client area.
//
// WindowSetSize/MoveWindow while WS_MAXIMIZE is still set does not shrink the
// visible frame. WebView2 then keeps a restored-size view anchored to the
// bottom-right, and the rest of the frameless window paints the native
// background (#f3f5f7). width and height are logical pixels, the same values
// WindowGetSize returns. x and y are screen pixels from WindowGetPosition.
func (a *App) RestoreMainWindowGeometry(x, y, width, height int) {
	if !restoreMainWindowGeometry(x, y, width, height) {
		log.Printf("[window] restore %d,%d %dx%d failed", x, y, width, height)
	}
}

func restoreMainWindowGeometry(x, y, width, height int) bool {
	if width < 160 || height < 120 {
		return false
	}
	windowGeometryMu.Lock()
	defer windowGeometryMu.Unlock()
	hwnd := findMainWindowHWND()
	if hwnd == 0 {
		return false
	}
	// A reassert at 80ms/250ms must not pull a window the user just minimized
	// back onto the desktop.
	if iconic, _, _ := procIsIconicWA.Call(hwnd); iconic != 0 {
		return true
	}
	// WindowGetSize was captured on this monitor. Read its DPI before
	// SW_RESTORE, which can jump to the placement monitor and report another scale.
	dpi := windowDPI(hwnd)
	pw := scaleLogicalToPhysical(width, dpi)
	ph := scaleLogicalToPhysical(height, dpi)
	if pw < 160 || ph < 120 {
		return false
	}
	if info, found := monitorInfoForRect(rectFromOrigin(x, y, pw, ph)); found {
		x, y = placeOriginInWorkArea(x, y, pw, ph, info.RcMonitor, info.RcWork)
	}
	zoomed, _, _ := procIsZoomedWA.Call(hwnd)
	if zoomed == 0 && windowOuterNear(hwnd, x, y, pw, ph) {
		// Already restored. ShowWindow would steal focus. Still clear a
		// leftover SW_SHOWMAXIMIZED placement, then put the screen rect back
		// because SetWindowPlacement positions from workspace coordinates.
		if markPlacementShownNormal(hwnd, x, y, pw, ph) {
			if !setWindowScreenRect(hwnd, x, y, pw, ph) || !windowOuterNear(hwnd, x, y, pw, ph) {
				return false
			}
			refitWebViewAfterRestore(hwnd)
		}
		return true
	}
	leaveMaximizedForRestore(hwnd)
	// Measure after SW_RESTORE. That call already generated WM_SIZE, so a
	// later SetWindowPos to the same outer size will not. The WebView can
	// still be sitting in the bottom-right until something refits it.
	beforeW, beforeH := windowOuterSize(hwnd)
	if !setWindowScreenRect(hwnd, x, y, pw, ph) {
		return false
	}
	// SetWindowPlacement applies rcNormalPosition and can undo the screen
	// point above. Put the target rect back when the show command changes.
	if markPlacementShownNormal(hwnd, x, y, pw, ph) && !setWindowScreenRect(hwnd, x, y, pw, ph) {
		return false
	}
	if zoomed, _, _ := procIsZoomedWA.Call(hwnd); zoomed != 0 {
		return false
	}
	// SetWindowPos can report success and leave the maximized frame in place.
	// Refitting then reads that client rect and pins the page to its corner.
	if !windowOuterNear(hwnd, x, y, pw, ph) {
		return false
	}
	afterW, afterH := windowOuterSize(hwnd)
	// A real size change already went through WM_SIZE. Refit only when that
	// message was skipped, or when the before-size could not be measured.
	if beforeW <= 0 || beforeH <= 0 || (beforeW == afterW && beforeH == afterH) {
		refitWebViewAfterRestore(hwnd)
	}
	log.Printf("[window] restored normal frame to %d,%d %dx%d (logical %dx%d dpi %d)", x, y, pw, ph, width, height, dpi)
	return true
}

func windowOuterNear(hwnd uintptr, x, y, width, height int) bool {
	var rc workAreaRect
	if ok, _, _ := procGetWindowRectWA.Call(hwnd, uintptr(unsafe.Pointer(&rc))); ok == 0 {
		return false
	}
	return abs32(rc.Left-int32(x)) <= 2 && abs32(rc.Top-int32(y)) <= 2 &&
		abs32((rc.Right-rc.Left)-int32(width)) <= 2 && abs32((rc.Bottom-rc.Top)-int32(height)) <= 2
}

func windowOuterSize(hwnd uintptr) (int, int) {
	var rc workAreaRect
	if ok, _, _ := procGetWindowRectWA.Call(hwnd, uintptr(unsafe.Pointer(&rc))); ok == 0 {
		return 0, 0
	}
	return int(rc.Right - rc.Left), int(rc.Bottom - rc.Top)
}

func setWindowScreenRect(hwnd uintptr, x, y, width, height int) bool {
	px, py := screenPositionParams(x, y)
	flags := uintptr(swpNoZOrder | swpNoActivate | swpFrameChanged)
	ret, _, _ := procSetWindowPosWA.Call(hwnd, 0, px, py, uintptr(width), uintptr(height), flags)
	return ret != 0
}

func windowDPI(hwnd uintptr) uint32 {
	if hwnd != 0 && procGetDpiForWindowInset.Find() == nil {
		if dpi, _, _ := procGetDpiForWindowInset.Call(hwnd); dpi != 0 {
			return uint32(dpi)
		}
	}
	return defaultWindowsDPI
}

func leaveMaximizedForRestore(hwnd uintptr) {
	zoomed, _, _ := procIsZoomedWA.Call(hwnd)
	if zoomed == 0 {
		// Already normal. SW_RESTORE would activate the window again on the
		// 80ms/250ms reassert and steal focus.
		return
	}
	procShowWindowRestore.Call(hwnd, swRestore)
	zoomed, _, _ = procIsZoomedWA.Call(hwnd)
	if zoomed == 0 {
		return
	}
	// SW_RESTORE can no-op when clamp stored ShowCmd=SW_SHOWMAXIMIZED.
	// Clear the style so the following SetWindowPos is allowed to shrink.
	// GWL_STYLE is -16. Sign-extend it; a bare negative constant does not fit in uintptr.
	gwlStyle := ^uintptr(15)
	style, _, _ := procGetWindowLongWRestore.Call(hwnd, gwlStyle)
	if style&wsMaximizeStyle == 0 {
		return
	}
	procSetWindowLongWRestore.Call(hwnd, gwlStyle, style&^uintptr(wsMaximizeStyle))
}

func markPlacementShownNormal(hwnd uintptr, x, y, targetW, targetH int) bool {
	place, ok := readWindowPlacement(hwnd)
	if !ok {
		return false
	}
	// rcNormalPosition is workspace coordinates. Screen points differ by the
	// primary work-area origin (taskbar on the top or left).
	ox, oy := primaryWorkOrigin()
	target := screenRectToWorkspace(rectFromOrigin(x, y, targetW, targetH), ox, oy)
	nextRect, nextCmd, update := planRestoredPlacement(place.ShowCmd, place.NormalPosition, target)
	if !update {
		return false
	}
	place.NormalPosition = nextRect
	place.ShowCmd = nextCmd
	place.Length = uint32(unsafe.Sizeof(place))
	ret, _, _ := procSetWindowPlacementWA.Call(hwnd, uintptr(unsafe.Pointer(&place)))
	return ret != 0
}

func refitWebViewAfterRestore(hwnd uintptr) {
	// chromium.Resize reads the client rect itself. This message only has to
	// reach the size handler when SetWindowPos did not change the outer size
	// (the WebView is already stuck in the bottom-right of a matching frame).
	var client workAreaRect
	if ok, _, _ := procGetClientRectRestore.Call(hwnd, uintptr(unsafe.Pointer(&client))); ok == 0 {
		return
	}
	w := client.Right - client.Left
	h := client.Bottom - client.Top
	if w <= 0 || h <= 0 {
		return
	}
	// WM_SIZE packs the client size into two 16-bit words. A side that does not
	// fit is left at 0; the refit reads the client rect itself.
	var lparam uintptr
	if w > 0 && h > 0 && w <= 0xFFFF && h <= 0xFFFF {
		lparam = uintptr(uint32(w) | uint32(h)<<16)
	}
	procSendMessageWRestore.Call(hwnd, wmSizeRestore, sizeRestored, lparam)
}

func primaryWorkOrigin() (int32, int32) {
	var rc workAreaRect
	r, _, _ := procSystemParametersInfoW.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&rc)), 0)
	if r == 0 {
		return 0, 0
	}
	return rc.Left, rc.Top
}

// screenRectToWorkspace converts a screen rect into the workspace coordinates
// WINDOWPLACEMENT stores. origin is the primary monitor work-area origin.
func screenRectToWorkspace(screen workAreaRect, originX, originY int32) workAreaRect {
	screen.Left -= originX
	screen.Top -= originY
	screen.Right -= originX
	screen.Bottom -= originY
	return screen
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
