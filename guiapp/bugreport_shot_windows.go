//go:build windows

package guiapp

import (
	"encoding/base64"
	"fmt"
	"syscall"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// DWMWA_EXTENDED_FRAME_BOUNDS. GetWindowRect includes the invisible resize
// border of a maximized window; this attribute is the visible frame.
const dwmwaExtendedFrameBounds = 9

var procDwmGetWindowAttributeBug = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmGetWindowAttribute")

// captureBugReportWindowPNG copies the pixels of the main GUI window. BitBlt of
// the window rectangle includes the WebView, which PrintWindow of the top
// frame often leaves blank.
func captureBugReportWindowPNG() ([]byte, error) {
	hwnd := findMainWindowHWND()
	if hwnd == 0 {
		return nil, fmt.Errorf("main window is not available")
	}
	var bounds workAreaRect
	callOK, _, _ := procGetWindowRectWA.Call(hwnd, uintptr(unsafe.Pointer(&bounds)))
	if callOK == 0 {
		return nil, fmt.Errorf("main window bounds are unavailable")
	}
	visible, visibleOK := bugReportVisibleFrame(hwnd)
	rect, ok := guiBugReportCaptureRect(guiBugReportRect(bounds), visible, visibleOK)
	if !ok {
		return nil, fmt.Errorf("main window has no visible area")
	}
	// Widen before subtracting. The fields are int32, and a negative
	// virtual-desktop origin can wrap the difference.
	width := int(rect.Right) - int(rect.Left)
	height := int(rect.Bottom) - int(rect.Top)
	if width < 1 || height < 1 {
		return nil, fmt.Errorf("main window has no visible area")
	}
	encoded, err := remote.NativeScreenshotRect(int(rect.Left), int(rect.Top), width, height)
	if err != nil {
		return nil, err
	}
	pngBytes, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode main window screenshot: %w", err)
	}
	return pngBytes, nil
}

// bugReportVisibleFrame reads the DWM visible frame. A failed call leaves
// the caller on GetWindowRect. S_OK is zero.
func bugReportVisibleFrame(hwnd uintptr) (guiBugReportRect, bool) {
	var rect workAreaRect
	hr, _, _ := procDwmGetWindowAttributeBug.Call(
		hwnd,
		uintptr(dwmwaExtendedFrameBounds),
		uintptr(unsafe.Pointer(&rect)),
		unsafe.Sizeof(rect),
	)
	if hr != 0 {
		return guiBugReportRect{}, false
	}
	return guiBugReportRect(rect), true
}
