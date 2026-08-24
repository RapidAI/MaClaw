//go:build windows

package remote

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"syscall"
	"unsafe"
)

var (
	user32                     = syscall.NewLazyDLL("user32.dll")
	gdi32                      = syscall.NewLazyDLL("gdi32.dll")
	shcore                     = syscall.NewLazyDLL("shcore.dll")
	procSetProcessDPIAware     = user32.NewProc("SetProcessDPIAware")
	procSetDpiAwarenessCtx     = user32.NewProc("SetProcessDpiAwarenessContext")
	procSetDpiAwareness        = shcore.NewProc("SetProcessDpiAwareness")
	procGetDesktopWindow       = user32.NewProc("GetDesktopWindow")
	procGetWindowDC            = user32.NewProc("GetWindowDC")
	procGetDC                  = user32.NewProc("GetDC")
	procReleaseDC              = user32.NewProc("ReleaseDC")
	procGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procGetDIBits              = gdi32.NewProc("GetDIBits")
)

const (
	smCxScreen        = 0
	smCyScreen        = 1
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79
	srcCopy           = 0x00CC0020
	captureBlt        = 0x40000000
	biRGB             = 0
	dibRGBColors      = 0
)

type bitmapInfoHeader struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

func init() {
	// Try per-monitor DPI awareness V2 (Win10 1703+), then per-monitor
	// awareness (Win8.1+), then fall back to system DPI awareness (Vista+).
	// This ensures GetSystemMetrics, GetWindowRect, CopyFromScreen etc.
	// return physical pixel coordinates, eliminating the white-border
	// artefact caused by logical-vs-physical coordinate mismatch under
	// DPI scaling > 100%.
	const dpiAwarenessContextPerMonitorV2 = ^uintptr(3) // (DPI_AWARENESS_CONTEXT)-4
	if ret, _, _ := procSetDpiAwarenessCtx.Call(dpiAwarenessContextPerMonitorV2); ret != 0 {
		return
	}
	// shcore.dll may not exist on Windows 7; recover from the panic that
	// LazyDLL.Call raises when the DLL cannot be loaded.
	if trySetDpiAwareness() {
		return
	}
	procSetProcessDPIAware.Call()
}

// trySetDpiAwareness attempts SetProcessDpiAwareness(PROCESS_PER_MONITOR_DPI_AWARE).
// Returns true on success. Recovers from panics caused by missing shcore.dll.
func trySetDpiAwareness() (ok bool) {
	defer func() { recover() }()
	const processPerMonitorDpiAware = 2
	ret, _, _ := procSetDpiAwareness.Call(uintptr(processPerMonitorDpiAware))
	return ret == 0
}

// NativeScreenshot captures the entire screen using Windows GDI APIs directly
// from Go, without spawning any external process. Returns base64-encoded PNG.
func NativeScreenshot() (string, error) {
	width, _, _ := procGetSystemMetrics.Call(uintptr(smCxScreen))
	height, _, _ := procGetSystemMetrics.Call(uintptr(smCyScreen))
	if width == 0 || height == 0 {
		return "", fmt.Errorf("failed to get screen dimensions: %dx%d", width, height)
	}

	w := int(width)
	h := int(height)

	hDesktop, _, _ := procGetDesktopWindow.Call()
	hDC, _, _ := procGetWindowDC.Call(hDesktop)
	if hDC == 0 {
		return "", fmt.Errorf("GetWindowDC failed")
	}
	defer procReleaseDC.Call(hDesktop, hDC)

	memDC, _, _ := procCreateCompatibleDC.Call(hDC)
	if memDC == 0 {
		return "", fmt.Errorf("CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(memDC)

	hBitmap, _, _ := procCreateCompatibleBitmap.Call(hDC, uintptr(w), uintptr(h))
	if hBitmap == 0 {
		return "", fmt.Errorf("CreateCompatibleBitmap failed")
	}
	defer procDeleteObject.Call(hBitmap)

	old, _, _ := procSelectObject.Call(memDC, hBitmap)
	defer procSelectObject.Call(memDC, old)

	ret, _, _ := procBitBlt.Call(memDC, 0, 0, uintptr(w), uintptr(h), hDC, 0, 0, srcCopy)
	if ret == 0 {
		return "", fmt.Errorf("BitBlt failed")
	}

	bmi := bitmapInfoHeader{
		BiSize:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		BiWidth:       int32(w),
		BiHeight:      -int32(h),
		BiPlanes:      1,
		BiBitCount:    32,
		BiCompression: biRGB,
	}

	pixelDataSize := w * h * 4
	pixelData := make([]byte, pixelDataSize)

	ret, _, _ = procGetDIBits.Call(
		memDC, hBitmap, 0, uintptr(h),
		uintptr(unsafe.Pointer(&pixelData[0])),
		uintptr(unsafe.Pointer(&bmi)),
		dibRGBColors,
	)
	if ret == 0 {
		return "", fmt.Errorf("GetDIBits failed")
	}

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < pixelDataSize; i += 4 {
		img.Pix[i+0] = pixelData[i+2] // R ← B
		img.Pix[i+1] = pixelData[i+1] // G ← G
		img.Pix[i+2] = pixelData[i+0] // B ← R
		img.Pix[i+3] = 255            // A
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", fmt.Errorf("png encode: %w", err)
	}

	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// NativeScreenshotRect captures a rectangle in virtual-desktop coordinates
// using GDI BitBlt (no PowerShell).
func NativeScreenshotRect(x, y, w, h int) (string, error) {
	if w < 1 || h < 1 {
		return "", fmt.Errorf("invalid screenshot rect %d,%d %dx%d", x, y, w, h)
	}
	if w > 16384 || h > 16384 {
		return "", fmt.Errorf("screenshot rect too large %dx%d", w, h)
	}

	hDC, _, _ := procGetDC.Call(0)
	if hDC == 0 {
		return "", fmt.Errorf("GetDC failed")
	}
	defer procReleaseDC.Call(0, hDC)

	memDC, _, _ := procCreateCompatibleDC.Call(hDC)
	if memDC == 0 {
		return "", fmt.Errorf("CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(memDC)

	hBitmap, _, _ := procCreateCompatibleBitmap.Call(hDC, uintptr(w), uintptr(h))
	if hBitmap == 0 {
		return "", fmt.Errorf("CreateCompatibleBitmap failed")
	}
	defer procDeleteObject.Call(hBitmap)

	old, _, _ := procSelectObject.Call(memDC, hBitmap)
	defer procSelectObject.Call(memDC, old)

	rop := uintptr(srcCopy | captureBlt)
	ret, _, _ := procBitBlt.Call(memDC, 0, 0, uintptr(w), uintptr(h), hDC, uintptr(int32(x)), uintptr(int32(y)), rop)
	if ret == 0 {
		return "", fmt.Errorf("BitBlt failed")
	}

	bmi := bitmapInfoHeader{
		BiSize:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		BiWidth:       int32(w),
		BiHeight:      -int32(h),
		BiPlanes:      1,
		BiBitCount:    32,
		BiCompression: biRGB,
	}
	pixelDataSize := w * h * 4
	pixelData := make([]byte, pixelDataSize)
	ret, _, _ = procGetDIBits.Call(
		memDC, hBitmap, 0, uintptr(h),
		uintptr(unsafe.Pointer(&pixelData[0])),
		uintptr(unsafe.Pointer(&bmi)),
		dibRGBColors,
	)
	if ret == 0 {
		return "", fmt.Errorf("GetDIBits failed")
	}

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < pixelDataSize; i += 4 {
		img.Pix[i+0] = pixelData[i+2]
		img.Pix[i+1] = pixelData[i+1]
		img.Pix[i+2] = pixelData[i+0]
		img.Pix[i+3] = 255
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", fmt.Errorf("png encode: %w", err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// NativeScreenshotVirtual captures the full virtual desktop (all monitors).
func NativeScreenshotVirtual() (string, error) {
	vx, _, _ := procGetSystemMetrics.Call(uintptr(smXVirtualScreen))
	vy, _, _ := procGetSystemMetrics.Call(uintptr(smYVirtualScreen))
	vw, _, _ := procGetSystemMetrics.Call(uintptr(smCXVirtualScreen))
	vh, _, _ := procGetSystemMetrics.Call(uintptr(smCYVirtualScreen))
	if vw == 0 || vh == 0 {
		return NativeScreenshot()
	}
	return NativeScreenshotRect(int(int32(vx)), int(int32(vy)), int(vw), int(vh))
}
