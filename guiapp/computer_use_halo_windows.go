//go:build windows

package guiapp

import (
	"log"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// Click-through layered frame drawn around the window Computer Use is
// driving. Four border strips leave the interior uncovered, so a maximized
// window is not covered by a full-screen layered bitmap. The strips do not
// activate, do not take clicks, and are excluded from screen capture when
// the OS allows it (Windows 10 2004+). A maximized window has no outer
// pixels, so the rim is drawn inside that window only while every strip is
// excluded from capture.

const (
	haloExTransparent   = 0x00000020
	haloExNoActivate    = 0x08000000
	haloSwpNoMove       = 0x0002
	haloSwpShowWindow   = 0x0040
	wmHaloSync          = 0x8000 + 41
	haloExcludeCapture  = 0x11 // WDA_EXCLUDEFROMCAPTURE
	dwmwaExtendedBounds = 9
	haloTimerID         = 1
	haloTimerMS         = 50
	haloStripCount      = 4
)

var (
	haloUser32 = syscall.NewLazyDLL("user32.dll")
	haloDwm    = syscall.NewLazyDLL("dwmapi.dll")

	procHaloIsWindow     = haloUser32.NewProc("IsWindow")
	procHaloIsIconic     = haloUser32.NewProc("IsIconic")
	procHaloIsZoomed     = haloUser32.NewProc("IsZoomed")
	procHaloSetAffinity  = haloUser32.NewProc("SetWindowDisplayAffinity")
	procHaloSetWindowPos = haloUser32.NewProc("SetWindowPos")
	procHaloInternalText = haloUser32.NewProc("InternalGetWindowText")
	procHaloDwmAttr      = haloDwm.NewProc("DwmGetWindowAttribute")

	haloStartOnce sync.Once
	haloReadyOnce sync.Once
	haloReadyCh   chan struct{}
	haloWndProcCB uintptr
	haloClassName *uint16
	haloStripHwnd [haloStripCount]atomic.Uintptr
	haloArmed     atomic.Bool
	haloFailed    atomic.Bool
	haloExcluded  atomic.Bool
	haloClassOnce sync.Once
	haloClassErr  error
)

func init() {
	haloClassName, _ = syscall.UTF16PtrFromString("MaclawComputerUseHalo")
}

func computerUseHaloHWND() uint64 {
	return uint64(haloStripHwnd[0].Load())
}

func computerUseHaloIsOverlay(id uint64) bool {
	if id == 0 {
		return false
	}
	for i := range haloStripHwnd {
		if uint64(haloStripHwnd[i].Load()) == id {
			return true
		}
	}
	return false
}

func computerUseHaloVisitWindows(fn func(hwnd uintptr)) {
	if fn == nil {
		return
	}
	for i := range haloStripHwnd {
		if hwnd := haloStripHwnd[i].Load(); hwnd != 0 {
			fn(hwnd)
		}
	}
}

func presentComputerUseWindowHalo() {
	if !computerUseHaloLive() {
		return
	}
	if !ensureComputerUseHaloThread() {
		return
	}
	hwnd := haloStripHwnd[0].Load()
	if hwnd == 0 {
		return
	}
	procPostMessageW.Call(hwnd, wmHaloSync, 0, 0)
}

func hideComputerUseWindowHaloPlatform() {
	hwnd := haloStripHwnd[0].Load()
	if hwnd == 0 {
		return
	}
	procPostMessageW.Call(hwnd, wmHaloSync, 0, 0)
}

func ensureComputerUseHaloThread() bool {
	haloStartOnce.Do(func() {
		haloReadyCh = make(chan struct{})
		go computerUseHaloThread()
	})
	// Later actions must not arm a fresh 2s timer. time.After in a select
	// is evaluated on the way in, so the wait stays on this first-create path.
	if haloArmed.Load() {
		return true
	}
	if haloFailed.Load() {
		return false
	}
	select {
	case <-haloReadyCh:
	case <-time.After(2 * time.Second):
		return false
	}
	return haloArmed.Load()
}

func haloSignalReady() {
	haloReadyOnce.Do(func() {
		if haloReadyCh != nil {
			close(haloReadyCh)
		}
	})
}

func computerUseHaloThread() {
	runtime.LockOSThread()
	defer haloSignalReady()

	hwnds, err := createComputerUseHaloWindows()
	if err != nil {
		haloFailed.Store(true)
		log.Printf("[computer-use] halo window: %v", err)
		return
	}
	for i, hwnd := range hwnds {
		haloStripHwnd[i].Store(hwnd)
	}
	if !computerUseHaloSkipCaptureExclusion() {
		excluded := true
		for _, hwnd := range hwnds {
			r, _, _ := procHaloSetAffinity.Call(hwnd, haloExcludeCapture)
			if r == 0 {
				excluded = false
			}
		}
		if !excluded {
			for _, hwnd := range hwnds {
				procHaloSetAffinity.Call(hwnd, 0)
			}
			log.Printf("[computer-use] halo capture exclusion unavailable")
		}
		haloExcluded.Store(excluded)
	}
	// The follow timer starts only once a target exists. An idle desktop
	// must not poll window rectangles. Armed is set after every strip exists
	// so a posted sync cannot run against a partial frame.
	haloArmed.Store(true)
	haloSignalReady()

	var msg msgW
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if ret == 0 || int32(ret) == -1 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func createComputerUseHaloWindows() ([haloStripCount]uintptr, error) {
	var out [haloStripCount]uintptr
	for i := 0; i < haloStripCount; i++ {
		hwnd, err := createComputerUseHaloWindow()
		if err != nil {
			for j := 0; j < i; j++ {
				procDestroyWindowProc.Call(out[j])
			}
			return out, err
		}
		out[i] = hwnd
	}
	return out, nil
}

func createComputerUseHaloWindow() (uintptr, error) {
	haloClassOnce.Do(func() {
		haloWndProcCB = syscall.NewCallback(computerUseHaloWndProc)
		hInst, _, _ := procGetModuleHandleW.Call(0)
		cursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))
		// No CS_HREDRAW/CS_VREDRAW. UpdateLayeredWindow owns the pixels;
		// those class styles discard the composed surface on a resize.
		wcx := wndClassExW{
			LpfnWndProc:   haloWndProcCB,
			HInstance:     syscall.Handle(hInst),
			HCursor:       syscall.Handle(cursor),
			LpszClassName: haloClassName,
		}
		wcx.CbSize = uint32(unsafe.Sizeof(wcx))
		atom, _, callErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wcx)))
		if atom == 0 {
			haloClassErr = callErr
			if haloClassErr == nil {
				haloClassErr = syscall.EINVAL
			}
		}
	})
	if haloClassErr != nil {
		return 0, haloClassErr
	}
	hInst, _, _ := procGetModuleHandleW.Call(0)
	ex := uintptr(wsExTopmost | wsExLayered | wsExToolwindow | haloExTransparent | haloExNoActivate)
	hwnd, _, callErr := procCreateWindowExW.Call(
		ex,
		uintptr(unsafe.Pointer(haloClassName)),
		0,
		uintptr(wsPopup),
		0, 0, 1, 1,
		0, 0, hInst, 0,
	)
	if hwnd == 0 {
		if callErr == nil {
			callErr = syscall.EINVAL
		}
		return 0, callErr
	}
	return hwnd, nil
}

func computerUseHaloWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmTimer, wmHaloSync:
		if haloArmed.Load() && (msg != wmTimer || wParam == haloTimerID) {
			computerUseHaloSync()
		}
		return 0
	case wmDestroy:
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return ret
}

type haloRect struct {
	Left, Top, Right, Bottom int32
}

func haloAlive(hwnd uintptr) bool {
	r, _, _ := procHaloIsWindow.Call(hwnd)
	return r != 0
}

func haloIconic(hwnd uintptr) bool {
	r, _, _ := procHaloIsIconic.Call(hwnd)
	return r != 0
}

func haloZoomed(hwnd uintptr) bool {
	r, _, _ := procHaloIsZoomed.Call(hwnd)
	return r != 0
}

func haloWindowRect(hwnd uintptr) (x, y, w, h int, ok bool) {
	var r haloRect
	ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	if ret == 0 {
		return 0, 0, 0, 0, false
	}
	w = int(r.Right - r.Left)
	h = int(r.Bottom - r.Top)
	if w < 64 || h < 64 {
		return 0, 0, 0, 0, false
	}
	return int(r.Left), int(r.Top), w, h, true
}

func haloDwmRect(hwnd uintptr) (x, y, w, h int, ok bool) {
	var r haloRect
	hr, _, _ := procHaloDwmAttr.Call(hwnd, dwmwaExtendedBounds, uintptr(unsafe.Pointer(&r)), unsafe.Sizeof(r))
	if hr != 0 {
		return 0, 0, 0, 0, false
	}
	w = int(r.Right - r.Left)
	h = int(r.Bottom - r.Top)
	if w < 64 || h < 64 {
		return 0, 0, 0, 0, false
	}
	return int(r.Left), int(r.Top), w, h, true
}

// haloTitle reads the caption without sending WM_GETTEXT.
// GetWindowText blocks until the target thread answers, so a hung app would
// freeze this overlay thread and the rim would stop following.
func haloTitle(hwnd uintptr) string {
	if procHaloInternalText.Find() != nil {
		return ""
	}
	buf := make([]uint16, 181)
	n, _, _ := procHaloInternalText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	runtime.KeepAlive(buf)
	if n == 0 || int(n) > len(buf) {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

type haloStripSurface struct {
	memDC uintptr
	bmp   uintptr
	old   uintptr
	bits  unsafe.Pointer
	w, h  int
}

type haloPlacement struct {
	x, y, w, h int
	inset      bool
	alphaQ     byte
	id         uint64
}

var (
	haloStrips     [haloStripCount]haloStripSurface
	haloCacheW     int
	haloCacheH     int
	haloCacheInset bool
	haloCacheOK    bool
	haloPlace      haloPlacement
	haloVisible    bool
	haloFollowing  bool
	haloLoggedID   uint64
	haloBlitFail   uint64
	haloZTick      int
)

func computerUseHaloSync() {
	// A preview test starts this thread, then later tests in the same
	// process must not paint. Production is always live.
	follow := haloStripHwnd[0].Load()
	if !computerUseHaloLive() {
		computerUseHaloHideWindow()
		haloStopFollow(follow)
		return
	}
	id := computerUseHaloTarget()
	target := uintptr(id)
	if id == 0 || computerUseHaloIsOverlay(id) || !haloAlive(target) {
		if id != 0 && !computerUseHaloIsOverlay(id) && !haloAlive(target) {
			computerUseHaloForgetTarget(id)
		}
		computerUseHaloHideWindow()
		// A replacement note may already have stored a new id. Stop the
		// timer only when nothing is left to follow. A minimized window
		// still has a live HWND and is followed until it is restored.
		if computerUseHaloTarget() == 0 {
			haloStopFollow(follow)
		}
		return
	}
	haloEnsureFollow(follow)
	if haloIconic(target) {
		computerUseHaloHideWindow()
		return
	}
	zoomed := haloZoomed(target)
	var x, y, w, h int
	var ok, inset bool
	if zoomed && haloExcluded.Load() {
		if rx, ry, rw, rh, rok := haloDwmRect(target); rok {
			x, y, w, h, ok, inset = rx, ry, rw, rh, true, true
		} else if rx, ry, rw, rh, rok := haloWindowRect(target); rok {
			x, y, w, h, ok, inset = rx, ry, rw, rh, true, true
		}
	} else if zoomed {
		// An inner rim would be part of the screenshot. Leave maximized
		// windows unmarked when capture exclusion is unavailable.
		computerUseHaloHideWindow()
		return
	} else {
		x, y, w, h, ok = haloWindowRect(target)
	}
	if !ok {
		computerUseHaloHideWindow()
		return
	}
	computerUseHaloShowFrame(id, x, y, w, h, inset)
}

func computerUseHaloHideWindow() {
	if !haloVisible {
		return
	}
	for i := range haloStripHwnd {
		if hwnd := haloStripHwnd[i].Load(); hwnd != 0 {
			procShowWindowFloat.Call(hwnd, swHide)
		}
	}
	haloVisible = false
	haloPlace = haloPlacement{}
	haloBlitFail = 0
	if haloLoggedID != 0 {
		log.Printf("[computer-use] halo hidden")
		haloLoggedID = 0
	}
}

func haloEnsureFollow(hwnd uintptr) {
	if haloFollowing || hwnd == 0 {
		return
	}
	if r, _, _ := procSetTimer.Call(hwnd, haloTimerID, haloTimerMS, 0); r != 0 {
		haloFollowing = true
	}
}

func haloStopFollow(hwnd uintptr) {
	if !haloFollowing || hwnd == 0 {
		return
	}
	procKillTimer.Call(hwnd, haloTimerID)
	haloFollowing = false
}

func computerUseHaloPulse() byte {
	ms := time.Now().UnixMilli() % 1800
	phase := float64(ms) / 1800 * 2 * math.Pi
	s := 0.78 + 0.22*math.Sin(phase)
	return byte(s * 255)
}

func computerUseHaloShowFrame(id uint64, x, y, w, h int, inset bool) {
	alpha := computerUseHaloPulse()
	place := haloPlacement{x: x, y: y, w: w, h: h, inset: inset, alphaQ: alpha / 16, id: id}
	if place == haloPlace && haloVisible {
		haloZTick++
		if haloZTick%8 == 0 {
			haloRaise()
		}
		return
	}
	strips, ok := computerUseHaloStrips(w, h, inset)
	if !ok || !haloInstall(w, h, inset) {
		computerUseHaloHideWindow()
		return
	}
	margin := computerUseHaloMargin(inset)
	// Place each strip at its final size before the upload. The windows are
	// created at 1x1. Resizing them inside UpdateLayeredWindow leaves the
	// composed surface empty: the call succeeds and GetWindowRect matches,
	// but the screen stays clear. Show at the final size first, then upload.
	// A failed upload does not commit the placement and does not hide, so
	// the next tick retries without flashing an empty frame.
	haloVisible = true
	for i, strip := range strips {
		hwnd := haloStripHwnd[i].Load()
		dst := point{X: int32(x - margin + strip.x), Y: int32(y - margin + strip.y)}
		sz := sizeW{CX: int32(haloStrips[i].w), CY: int32(haloStrips[i].h)}
		if hwnd == 0 {
			if haloBlitFail != id {
				log.Printf("[computer-use] halo frame update failed for window 0x%X", id)
				haloBlitFail = id
			}
			return
		}
		procHaloSetWindowPos.Call(
			hwnd, ^uintptr(0),
			uintptr(int32(dst.X)), uintptr(int32(dst.Y)),
			uintptr(sz.CX), uintptr(sz.CY),
			uintptr(swpNoActivate|haloSwpShowWindow),
		)
		if !haloBlit(hwnd, i, dst, sz, alpha) {
			if haloBlitFail != id {
				log.Printf("[computer-use] halo frame update failed for window 0x%X", id)
				haloBlitFail = id
			}
			return
		}
	}
	haloPlace = place
	haloBlitFail = 0
	haloRaise()
	if haloLoggedID != id {
		log.Printf("[computer-use] halo on window 0x%X %q", id, haloTitle(uintptr(id)))
		haloLoggedID = id
	}
}

func haloBlit(hwnd uintptr, strip int, dst point, sz sizeW, alpha byte) bool {
	if hwnd == 0 || strip < 0 || strip >= len(haloStrips) || haloStrips[strip].memDC == 0 {
		return false
	}
	src := point{}
	blend := blendFunction{acSrcOver, 0, alpha, acSrcAlpha}
	memDC := haloStrips[strip].memDC
	call := func() uintptr {
		ret, _, _ := procUpdateLayeredWindow.Call(
			hwnd, 0,
			uintptr(unsafe.Pointer(&dst)),
			uintptr(unsafe.Pointer(&sz)),
			memDC,
			uintptr(unsafe.Pointer(&src)),
			0,
			uintptr(unsafe.Pointer(&blend)),
			uintptr(ulwAlpha),
		)
		return ret
	}
	ok := call() != 0 || call() != 0
	runtime.KeepAlive(&dst)
	runtime.KeepAlive(&sz)
	runtime.KeepAlive(&src)
	runtime.KeepAlive(&blend)
	return ok
}

func haloRaise() {
	flags := uintptr(swpNoSize | haloSwpNoMove | swpNoActivate)
	for i := range haloStripHwnd {
		if hwnd := haloStripHwnd[i].Load(); hwnd != 0 {
			procHaloSetWindowPos.Call(hwnd, ^uintptr(0), 0, 0, 0, 0, flags)
		}
	}
}

func haloInstall(winW, winH int, inset bool) bool {
	if haloCacheOK && haloCacheW == winW && haloCacheH == winH && haloCacheInset == inset {
		return true
	}
	strips, ok := computerUseHaloStrips(winW, winH, inset)
	if !ok {
		return false
	}
	ow, oh, dimOK := computerUseHaloDims(winW, winH, inset)
	if !dimOK {
		return false
	}
	haloReleaseSurface()
	var made [haloStripCount]haloStripSurface
	for i, strip := range strips {
		surf, allocOK := haloAllocStrip(strip.w, strip.h)
		if !allocOK {
			for j := 0; j <= i; j++ {
				haloFreeStrip(&made[j])
			}
			return false
		}
		made[i] = surf
	}
	haloStrips = made
	haloPaintStrips(ow, oh, computerUseHaloLimit(inset), winW, winH, inset)
	haloCacheW, haloCacheH, haloCacheInset, haloCacheOK = winW, winH, inset, true
	return true
}

func haloAllocStrip(w, h int) (haloStripSurface, bool) {
	if w <= 0 || h <= 0 {
		return haloStripSurface{}, false
	}
	memDC, _, _ := procCreateCompatibleDC.Call(0)
	if memDC == 0 {
		return haloStripSurface{}, false
	}
	bmi := bitmapInfo{BmiHeader: bitmapInfoHeader{
		BiSize:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		BiWidth:       int32(w),
		BiHeight:      -int32(h),
		BiPlanes:      1,
		BiBitCount:    32,
		BiCompression: biRgb,
	}}
	var bits unsafe.Pointer
	hbmp, _, _ := procCreateDIBSection.Call(
		memDC, uintptr(unsafe.Pointer(&bmi)), 0,
		uintptr(unsafe.Pointer(&bits)), 0, 0,
	)
	if hbmp == 0 || bits == nil {
		procScreenshotDeleteDC.Call(memDC)
		return haloStripSurface{}, false
	}
	old, _, _ := procScreenshotSelectObject.Call(memDC, hbmp)
	buf := unsafe.Slice((*byte)(bits), w*h*4)
	clear(buf)
	runtime.KeepAlive(buf)
	return haloStripSurface{memDC: memDC, bmp: hbmp, old: old, bits: bits, w: w, h: h}, true
}

func haloPaintStrips(ow, oh, limit, winW, winH int, inset bool) {
	computerUseHaloPaint(winW, winH, inset, func(x, y, n int, r, g, b, a uint8) {
		a16 := uint16(a)
		pb := byte(uint16(b) * a16 / 255)
		pg := byte(uint16(g) * a16 / 255)
		pr := byte(uint16(r) * a16 / 255)
		computerUseHaloProject(x, y, n, ow, oh, limit, func(strip, lx, ly, take int) {
			s := &haloStrips[strip]
			if s.bits == nil || lx < 0 || ly < 0 || take <= 0 || lx+take > s.w || ly >= s.h {
				return
			}
			buf := unsafe.Slice((*byte)(s.bits), s.w*s.h*4)
			off := ly*s.w*4 + lx*4
			haloFill4(buf[off:off+take*4], take, pb, pg, pr, a)
		})
	})
}

func haloReleaseSurface() {
	for i := range haloStrips {
		haloFreeStrip(&haloStrips[i])
	}
	haloCacheOK = false
}

func haloFreeStrip(s *haloStripSurface) {
	if s == nil || s.memDC == 0 {
		return
	}
	if s.old != 0 {
		procScreenshotSelectObject.Call(s.memDC, s.old)
	}
	if s.bmp != 0 {
		procScreenshotDeleteObject.Call(s.bmp)
	}
	procScreenshotDeleteDC.Call(s.memDC)
	*s = haloStripSurface{}
}
