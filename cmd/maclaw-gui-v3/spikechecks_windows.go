//go:build windows

package main

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modUser32 = windows.NewLazySystemDLL("user32.dll")
	modDwmapi = windows.NewLazySystemDLL("dwmapi.dll")
	modNtdll  = windows.NewLazySystemDLL("ntdll.dll")

	procGetDpiForWindow        = modUser32.NewProc("GetDpiForWindow")
	procGetSystemMetricsForDpi = modUser32.NewProc("GetSystemMetricsForDpi")
	procGetWindowRect          = modUser32.NewProc("GetWindowRect")
	procClientToScreen         = modUser32.NewProc("ClientToScreen")
	procGetAsyncKeyState       = modUser32.NewProc("GetAsyncKeyState")
	procReleaseCapture         = modUser32.NewProc("ReleaseCapture")
	procPostMessageW           = modUser32.NewProc("PostMessageW")
	procGetForegroundWindow    = modUser32.NewProc("GetForegroundWindow")
	procDwmGetWindowAttribute  = modDwmapi.NewProc("DwmGetWindowAttribute")
	procRtlGetNtVersionNumbers = modNtdll.NewProc("RtlGetNtVersionNumbers")
)

const (
	smCyFrame        = 32 // SM_CYFRAME
	smCxPaddedBorder = 92 // SM_CXPADDEDBORDER

	// DWMWA_WINDOW_CORNER_PREFERENCE is Windows 11 only; on Windows 10 the
	// call fails, which is itself the answer for checkpoint C2.
	dwmwaCornerPref = 33
	dwmcpRound      = 2

	wmNcLButtonDown = 0x00A1
	htCaption       = 2
	vkLButton       = 0x01
)

type spikePoint struct{ X, Y int32 }

type spikeRect struct{ Left, Top, Right, Bottom int32 }

func spikeNtVersion() (major, minor, build uint32) {
	procRtlGetNtVersionNumbers.Call(
		uintptr(unsafe.Pointer(&major)),
		uintptr(unsafe.Pointer(&minor)),
		uintptr(unsafe.Pointer(&build)))
	return major, minor, build & 0xFFFF
}

// spikeWinChecks mirrors guiapp/platform_windows.go GetFramelessTopInset and
// the Win11 branch of IsNativeRoundedCorners against the v3 window.
func spikeWinChecks(hwnd uintptr) []string {
	major, minor, build := spikeNtVersion()
	out := []string{fmt.Sprintf("os=win %d.%d build %d", major, minor, build)}
	if hwnd == 0 {
		return append(out, "hwnd=0")
	}
	dpi, _, _ := procGetDpiForWindow.Call(hwnd)
	frame, _, _ := procGetSystemMetricsForDpi.Call(smCyFrame, dpi)
	pad, _, _ := procGetSystemMetricsForDpi.Call(smCxPaddedBorder, dpi)
	var r spikeRect
	_, _, _ = procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	var pt spikePoint
	_, _, _ = procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
	inset := int32(pt.Y) - r.Top
	out = append(out, fmt.Sprintf("dpi=%d topInset=%dpx (SM_CYFRAME=%d SM_CXPADDEDBORDER=%d)", dpi, inset, frame, pad))

	var pref uint32
	hr, _, _ := procDwmGetWindowAttribute.Call(hwnd, dwmwaCornerPref, uintptr(unsafe.Pointer(&pref)), 4)
	if hr == 0 {
		out = append(out, fmt.Sprintf("cornerPreference=%d (%s)", pref, spikeCornerName(pref)))
	} else {
		out = append(out, fmt.Sprintf("cornerPreference=unsupported hr=0x%08x (Windows 10 预期如此)", hr))
	}
	fg, _, _ := procGetForegroundWindow.Call()
	out = append(out, fmt.Sprintf("isForeground=%v", fg == hwnd))
	return out
}

func spikeCornerName(pref uint32) string {
	if pref == dwmcpRound {
		return "round"
	}
	return fmt.Sprintf("raw%d", pref)
}

// spikeGuardedDragBegin is the v3 port of guiapp's BeginWindowDrag guard:
// refuse when the left button already came up (the async invoke race that
// wedged v2 windows in the native move loop), then release capture and post
// WM_NCLBUTTONDOWN/HTCAPTION directly.
func spikeGuardedDragBegin(hwnd uintptr) string {
	if hwnd == 0 {
		return "no-hwnd"
	}
	state, _, _ := procGetAsyncKeyState.Call(vkLButton)
	if state&0x8000 == 0 {
		return "mouse-up-before-drag (guard 正确拒绝)"
	}
	_, _, _ = procReleaseCapture.Call()
	_, _, _ = procPostMessageW.Call(hwnd, wmNcLButtonDown, htCaption, 0)
	return "posted WM_NCLBUTTONDOWN/HTCAPTION"
}

func spikeOSBuild() string {
	major, minor, build := spikeNtVersion()
	return fmt.Sprintf("os=win %d.%d build %d", major, minor, build)
}
