//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// getPrimaryScreenSize returns the primary monitor resolution in Wails' 96-DPI
// logical units.
//
// Wails calls SetProcessDPIAware and its WindowSetSize implementation scales
// logical inputs to the current window DPI. GetSystemMetrics therefore reports
// physical dimensions here and must be normalized before applying the preset
// table. Example: 1920x1080 at 125% becomes 1536x864 logical pixels.
func getPrimaryScreenSize() (width, height int) {
	user32 := syscall.NewLazyDLL("user32.dll")
	getSystemMetrics := user32.NewProc("GetSystemMetrics")
	getDpiForSystem := user32.NewProc("GetDpiForSystem")
	if getSystemMetrics.Find() != nil {
		return 0, 0
	}

	const (
		smCXScreen = 0 // SM_CXSCREEN — primary screen width (logical px)
		smCYScreen = 1 // SM_CYSCREEN — primary screen height (logical px)
	)

	w, _, _ := getSystemMetrics.Call(uintptr(smCXScreen))
	h, _, _ := getSystemMetrics.Call(uintptr(smCYScreen))
	// GetDpiForSystem is unavailable on pre-1607 Windows 10. The screen
	// metrics still need normalising there, so fall back to the process/system
	// DPI exposed through a desktop device context.
	dpi := systemDPIForWindowSizing(getDpiForSystem)

	return normalizeScreenSizeForWindowDPI(int(w), int(h), dpi)
}

// getCurrentWindowWorkArea returns the logical working-area dimensions of the
// monitor containing the main window. It is used only after the Wails frontend
// has mounted; startup still calls getPrimaryScreenSize before an HWND exists.
//
// WindowSetSize scales its logical arguments using the window's current DPI.
// Using the primary monitor here would make a primary@100% -> current@125%
// move request an oversized physical window after the environment check.
// A false result means no HWND/work area is available yet; callers must then
// use the full-screen sizing path, which includes its normal taskbar reserve.
func getCurrentWindowWorkArea() (width, height int, ok bool) {
	hwnd := findMainWindowHWND()
	if hwnd == 0 {
		return 0, 0, false
	}
	hmon, _, _ := procMonitorFromWindowWA.Call(hwnd, monitorDefaultToNearest)
	if hmon == 0 {
		return 0, 0, false
	}
	var mi workAreaMonitorInfo
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	if ok, _, _ := procGetMonitorInfoWWA.Call(hmon, uintptr(unsafe.Pointer(&mi))); ok == 0 {
		return 0, 0, false
	}
	// RcWork already excludes a taskbar on any edge. Passing monitor bounds into
	// the generic fallback would assume an 80px bottom taskbar, which is wrong
	// for top/left/right taskbars and can still place the resized window under a
	// side taskbar.
	width = int(mi.RcWork.Right - mi.RcWork.Left)
	height = int(mi.RcWork.Bottom - mi.RcWork.Top)
	if width <= 0 || height <= 0 {
		return 0, 0, false
	}
	width, height = normalizeScreenSizeForWindowDPI(width, height, windowDPIForSizing(hwnd))
	return width, height, width > 0 && height > 0
}

// adaptiveWindowSizeForCurrentWindow uses the actual Windows work area after
// the main HWND exists. This must stay Windows-specific: other platforms only
// provide full-display metrics and still need adaptiveWindowSize's normal
// taskbar/dock reserve.
func adaptiveWindowSizeForCurrentWindow() (width, height int) {
	if workWidth, workHeight, ok := getCurrentWindowWorkArea(); ok {
		return adaptiveWindowSizeForWorkArea(workWidth, workHeight)
	}
	return adaptiveWindowSize()
}

func windowDPIForSizing(hwnd uintptr) int {
	if hwnd != 0 && procGetDpiForWindowInset.Find() == nil {
		if dpi, _, _ := procGetDpiForWindowInset.Call(hwnd); dpi != 0 {
			return int(dpi)
		}
	}
	return systemDPIForWindowSizing(syscall.NewLazyDLL("user32.dll").NewProc("GetDpiForSystem"))
}

func systemDPIForWindowSizing(getDpiForSystem *syscall.LazyProc) int {
	if getDpiForSystem != nil && getDpiForSystem.Find() == nil {
		if dpi, _, _ := getDpiForSystem.Call(); dpi != 0 {
			return int(dpi)
		}
	}

	user32 := syscall.NewLazyDLL("user32.dll")
	gdi32 := syscall.NewLazyDLL("gdi32.dll")
	getDC := user32.NewProc("GetDC")
	releaseDC := user32.NewProc("ReleaseDC")
	getDeviceCaps := gdi32.NewProc("GetDeviceCaps")
	if getDC.Find() != nil || releaseDC.Find() != nil || getDeviceCaps.Find() != nil {
		return 96
	}

	const logPixelsX = 88 // LOGPIXELSX
	dc, _, _ := getDC.Call(0)
	if dc == 0 {
		return 96
	}
	defer releaseDC.Call(0, dc)
	if dpi, _, _ := getDeviceCaps.Call(dc, uintptr(logPixelsX)); dpi != 0 {
		return int(dpi)
	}
	return 96
}
