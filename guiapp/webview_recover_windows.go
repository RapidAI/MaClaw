//go:build windows

package guiapp

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"
)

var (
	recoverUser32          = syscall.NewLazyDLL("user32.dll")
	recoverKernel32        = syscall.NewLazyDLL("kernel32.dll")
	procGetClientRectRW    = recoverUser32.NewProc("GetClientRect")
	procGetWindowRectRW    = recoverUser32.NewProc("GetWindowRect")
	procEnumWindowsRW      = recoverUser32.NewProc("EnumWindows")
	procEnumChildWindowsRW = recoverUser32.NewProc("EnumChildWindows")
	procGetClassNameWRW    = recoverUser32.NewProc("GetClassNameW")
	procGetWindowTextWRW   = recoverUser32.NewProc("GetWindowTextW")
	procIsWindowVisibleRW  = recoverUser32.NewProc("IsWindowVisible")
	procMoveWindowRW       = recoverUser32.NewProc("MoveWindow")
	procCreateWindowExWRW  = recoverUser32.NewProc("CreateWindowExW")
	procSetWindowPosRW     = recoverUser32.NewProc("SetWindowPos")
	procDestroyWindowRW    = recoverUser32.NewProc("DestroyWindow")
	procSetWindowTextWRW   = recoverUser32.NewProc("SetWindowTextW")
	procGetWindowThreadPID = recoverUser32.NewProc("GetWindowThreadProcessId")
	procGetModuleHandleWRW = recoverKernel32.NewProc("GetModuleHandleW")
	procGetCurrentProcess  = recoverKernel32.NewProc("GetCurrentProcessId")
	procGetSystemMetricsRW = recoverUser32.NewProc("GetSystemMetrics")
	procShowWindowRW       = recoverUser32.NewProc("ShowWindow")
	procUpdateWindowRW     = recoverUser32.NewProc("UpdateWindow")
	procInvalidateRectRW   = recoverUser32.NewProc("InvalidateRect")

	nativeSplashMu           sync.Mutex
	nativeSplashHWND         uintptr
	recoverChildMu           sync.Mutex
	recoverChildren          []uintptr
	recoverTopMu             sync.Mutex
	recoverTopWindows        []recoverWinInfo
	recoverChildEnumCallback = syscall.NewCallback(enumRecoverChild)
	recoverTopEnumCallback   = syscall.NewCallback(enumRecoverTop)
)

type recoverRECT struct {
	Left, Top, Right, Bottom int32
}

type recoverWinInfo struct {
	hwnd    uintptr
	visible bool
	title   string
	class   string
	w, h    int
	x, y    int
}

func enumRecoverChild(hwnd, lparam uintptr) uintptr {
	recoverChildMu.Lock()
	recoverChildren = append(recoverChildren, hwnd)
	recoverChildMu.Unlock()
	return 1
}

func enumRecoverTop(hwnd, lparam uintptr) uintptr {
	wantPID := uint32(lparam)
	var wpid uint32
	procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&wpid)))
	if wpid != wantPID {
		return 1
	}
	vis, _, _ := procIsWindowVisibleRW.Call(hwnd)
	var wr recoverRECT
	procGetWindowRectRW.Call(hwnd, uintptr(unsafe.Pointer(&wr)))
	info := recoverWinInfo{
		hwnd:    hwnd,
		visible: vis != 0,
		title:   recoverWindowText(hwnd),
		class:   recoverClassName(hwnd),
		w:       int(wr.Right - wr.Left),
		h:       int(wr.Bottom - wr.Top),
		x:       int(wr.Left),
		y:       int(wr.Top),
	}
	recoverTopMu.Lock()
	recoverTopWindows = append(recoverTopWindows, info)
	recoverTopMu.Unlock()
	return 1
}

func recoverBlankWebView(reason string) {
	pid, _, _ := procGetCurrentProcess.Call()
	bootLog("recover start reason=%s pid=%d", reason, pid)

	recoverTopMu.Lock()
	recoverTopWindows = recoverTopWindows[:0]
	recoverTopMu.Unlock()
	procEnumWindowsRW.Call(recoverTopEnumCallback, pid)

	recoverTopMu.Lock()
	tops := append([]recoverWinInfo(nil), recoverTopWindows...)
	recoverTopMu.Unlock()
	bootLog("top-level windows count=%d", len(tops))
	var wailsWin recoverWinInfo
	for i, w := range tops {
		bootLog("top[%d] hwnd=0x%X vis=%v class=%q title=%q pos=%d,%d size=%dx%d", i, w.hwnd, w.visible, w.class, w.title, w.x, w.y, w.w, w.h)
		if w.class == "wailsWindow" {
			wailsWin = w
		}
	}

	hwnd := findMainWindowHWND()
	bootLog("findMainWindowHWND=0x%X wailsWindow=0x%X vis=%v %dx%d", hwnd, wailsWin.hwnd, wailsWin.visible, wailsWin.w, wailsWin.h)
	if hwnd == 0 {
		hwnd = wailsWin.hwnd
	}
	if hwnd == 0 {
		bootLog("no wailsWindow to show")
		return
	}
	procShowWindowRW.Call(hwnd, 5)                              // SW_SHOW
	procSetWindowPosRW.Call(hwnd, 0, 0, 0, 0, 0, 0x0003|0x0040) // SWP_NOMOVE|SWP_NOSIZE|SWP_SHOWWINDOW
	bootLog("ShowWindow SW_SHOW wailsWindow hwnd=0x%X vis_was=%v %dx%d", hwnd, wailsWin.visible, wailsWin.w, wailsWin.h)

	var rc recoverRECT
	procGetClientRectRW.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	width := int(rc.Right - rc.Left)
	height := int(rc.Bottom - rc.Top)
	bootLog("client=%dx%d", width, height)

	recoverChildMu.Lock()
	recoverChildren = recoverChildren[:0]
	recoverChildMu.Unlock()
	procEnumChildWindowsRW.Call(hwnd, recoverChildEnumCallback, 0)
	recoverChildMu.Lock()
	children := append([]uintptr(nil), recoverChildren...)
	recoverChildMu.Unlock()
	bootLog("child count=%d", len(children))
	for _, child := range children {
		var crc recoverRECT
		procGetClientRectRW.Call(child, uintptr(unsafe.Pointer(&crc)))
		bootLog("child hwnd=0x%X class=%s size=%dx%d", child, recoverClassName(child), crc.Right-crc.Left, crc.Bottom-crc.Top)
		if width > 0 && height > 0 {
			procMoveWindowRW.Call(child, 0, 0, uintptr(width), uintptr(height), 1)
		}
	}
	hideNativeEnvCheckSplash()
}

func hideNativeEnvCheckSplash() {
	nativeSplashMu.Lock()
	hwnd := nativeSplashHWND
	nativeSplashHWND = 0
	nativeSplashMu.Unlock()
	if hwnd != 0 {
		const wmClose = 0x0010
		procPostMessage := recoverUser32.NewProc("PostMessageW")
		procPostMessage.Call(hwnd, wmClose, 0, 0)
		procDestroyWindowRW.Call(hwnd)
		bootLog("native splash close+destroy hwnd=0x%X", hwnd)
	}
}

func showNativeEnvCheckSplash(parent uintptr, width, height int) {
	if width <= 0 {
		width = 520
	}
	if height <= 0 {
		height = 460
	}
	x, y := 200, 200
	if parent != 0 {
		var wr recoverRECT
		procGetWindowRectRW.Call(parent, uintptr(unsafe.Pointer(&wr)))
		if wr.Right > wr.Left && wr.Bottom > wr.Top {
			x, y = int(wr.Left), int(wr.Top)
			width, height = int(wr.Right-wr.Left), int(wr.Bottom-wr.Top)
		}
	} else {
		sw, _, _ := procGetSystemMetricsRW.Call(0) // SM_CXSCREEN
		sh, _, _ := procGetSystemMetricsRW.Call(1) // SM_CYSCREEN
		x = (int(sw) - width) / 2
		y = (int(sh) - height) / 2
		bootLog("no parent hwnd, centering overlay on screen %dx%d at %d,%d", sw, sh, x, y)
	}
	const (
		wsExTopmost    = 0x00000008
		wsExToolwindow = 0x00000080
		wsPopup        = 0x80000000
		wsVisible      = 0x10000000
		wsBorder       = 0x00800000
		hwndTopmost    = ^uintptr(0) // HWND_TOPMOST == -1
		swpShow        = 0x0040
		swShow         = 5
	)
	className, _ := syscall.UTF16PtrFromString("STATIC")
	title, _ := syscall.UTF16PtrFromString("正在准备环境\r\n正在准备运行环境，请稍候")
	mod, _, _ := procGetModuleHandleWRW.Call(0)
	hwnd, _, err := procCreateWindowExWRW.Call(
		uintptr(wsExTopmost|wsExToolwindow),
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		uintptr(wsPopup|wsVisible|wsBorder),
		uintptr(x), uintptr(y), uintptr(width), uintptr(height),
		0, 0, mod, 0,
	)
	if hwnd == 0 {
		bootLog("CreateWindowEx overlay failed err=%v", err)
		return
	}
	nativeSplashMu.Lock()
	old := nativeSplashHWND
	nativeSplashHWND = hwnd
	nativeSplashMu.Unlock()
	if old != 0 {
		procDestroyWindowRW.Call(old)
	}
	procSetWindowPosRW.Call(hwnd, hwndTopmost, uintptr(x), uintptr(y), uintptr(width), uintptr(height), swpShow)
	procShowWindowRW.Call(hwnd, swShow)
	procSetWindowTextWRW.Call(hwnd, uintptr(unsafe.Pointer(title)))
	procInvalidateRectRW.Call(hwnd, 0, 1)
	procUpdateWindowRW.Call(hwnd)
	bootLog("native overlay hwnd=0x%X pos=%d,%d size=%dx%d", hwnd, x, y, width, height)
}

func recoverClassName(hwnd uintptr) string {
	buf := make([]uint16, 256)
	n, _, _ := procGetClassNameWRW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 256)
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

func recoverWindowText(hwnd uintptr) string {
	buf := make([]uint16, 512)
	n, _, _ := procGetWindowTextWRW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 512)
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

func findWailsWindowHWND() uintptr {
	pid, _, _ := procGetCurrentProcess.Call()
	recoverTopMu.Lock()
	recoverTopWindows = recoverTopWindows[:0]
	recoverTopMu.Unlock()
	procEnumWindowsRW.Call(recoverTopEnumCallback, pid)
	recoverTopMu.Lock()
	defer recoverTopMu.Unlock()
	for _, w := range recoverTopWindows {
		if w.class == "wailsWindow" {
			return w.hwnd
		}
	}
	return 0
}

func syncWebViewClientSize() {
	hwnd := findWailsWindowHWND()
	if hwnd == 0 {
		bootLog("syncWebViewClientSize no hwnd")
		return
	}
	var rc recoverRECT
	procGetClientRectRW.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	width := int(rc.Right - rc.Left)
	height := int(rc.Bottom - rc.Top)
	bootLog("syncWebViewClientSize hwnd=0x%X client=%dx%d", hwnd, width, height)
	if width <= 0 || height <= 0 {
		return
	}
	recoverChildMu.Lock()
	recoverChildren = recoverChildren[:0]
	recoverChildMu.Unlock()
	procEnumChildWindowsRW.Call(hwnd, recoverChildEnumCallback, 0)
	recoverChildMu.Lock()
	children := append([]uintptr(nil), recoverChildren...)
	recoverChildMu.Unlock()
	for _, child := range children {
		procMoveWindowRW.Call(child, 0, 0, uintptr(width), uintptr(height), 1)
	}
}

func captureMainWindowPNG(tag string) {
	hwnd := findWailsWindowHWND()
	if hwnd == 0 {
		bootLog("captureMainWindowPNG tag=%s no hwnd", tag)
		return
	}
	var wr recoverRECT
	procGetWindowRectRW.Call(hwnd, uintptr(unsafe.Pointer(&wr)))
	w := int(wr.Right - wr.Left)
	h := int(wr.Bottom - wr.Top)
	if w < 8 || h < 8 {
		bootLog("captureMainWindowPNG tag=%s bad size %dx%d", tag, w, h)
		return
	}
	gdi := syscall.NewLazyDLL("gdi32.dll")
	procGetDC := recoverUser32.NewProc("GetDC")
	procReleaseDC := recoverUser32.NewProc("ReleaseDC")
	procCreateCompatibleDC := gdi.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap := gdi.NewProc("CreateCompatibleBitmap")
	procSelectObject := gdi.NewProc("SelectObject")
	procDeleteObject := gdi.NewProc("DeleteObject")
	procDeleteDC := gdi.NewProc("DeleteDC")
	procGetDIBits := gdi.NewProc("GetDIBits")
	procPrintWindow := recoverUser32.NewProc("PrintWindow")

	hdc, _, _ := procGetDC.Call(hwnd)
	if hdc == 0 {
		bootLog("captureMainWindowPNG tag=%s GetDC failed", tag)
		return
	}
	defer procReleaseDC.Call(hwnd, hdc)
	memDC, _, _ := procCreateCompatibleDC.Call(hdc)
	if memDC == 0 {
		bootLog("captureMainWindowPNG tag=%s CreateCompatibleDC failed", tag)
		return
	}
	defer procDeleteDC.Call(memDC)
	bmp, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(w), uintptr(h))
	if bmp == 0 {
		bootLog("captureMainWindowPNG tag=%s CreateCompatibleBitmap failed", tag)
		return
	}
	defer procDeleteObject.Call(bmp)
	old, _, _ := procSelectObject.Call(memDC, bmp)
	ok, _, _ := procPrintWindow.Call(hwnd, memDC, 2)
	if ok == 0 {
		bootLog("captureMainWindowPNG tag=%s PrintWindow failed, trying without flags", tag)
		procPrintWindow.Call(hwnd, memDC, 0)
	}
	type bmih struct {
		Size          uint32
		Width         int32
		Height        int32
		Planes        uint16
		BitCount      uint16
		Compression   uint32
		SizeImage     uint32
		XPelsPerMeter int32
		YPelsPerMeter int32
		ClrUsed       uint32
		ClrImportant  uint32
	}
	hdr := bmih{Size: 40, Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	pixels := make([]byte, w*h*4)
	n, _, _ := procGetDIBits.Call(memDC, bmp, 0, uintptr(h), uintptr(unsafe.Pointer(&pixels[0])), uintptr(unsafe.Pointer(&hdr)), 0)
	procSelectObject.Call(memDC, old)
	if n == 0 {
		bootLog("captureMainWindowPNG tag=%s GetDIBits failed", tag)
		return
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			img.SetNRGBA(x, y, color.NRGBA{R: pixels[i+2], G: pixels[i+1], B: pixels[i+0], A: 255})
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		bootLog("captureMainWindowPNG tag=%s home err=%v", tag, err)
		return
	}
	dir := filepath.Join(home, ".maclaw", "logs")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "maclaw-ui-"+tag+".png")
	f, err := os.Create(path)
	if err != nil {
		bootLog("captureMainWindowPNG tag=%s create err=%v", tag, err)
		return
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		bootLog("captureMainWindowPNG tag=%s png err=%v", tag, err)
		return
	}
	bootLog("captureMainWindowPNG tag=%s path=%s size=%dx%d", tag, path, w, h)
}
