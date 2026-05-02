//go:build windows

package main

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
	procGetDesktopWindow       = user32.NewProc("GetDesktopWindow")
	procGetWindowDC            = user32.NewProc("GetWindowDC")
	procReleaseDC              = user32.NewProc("ReleaseDC")
	procGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procScreenshotSelectObject = gdi32.NewProc("SelectObject")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procScreenshotDeleteDC     = gdi32.NewProc("DeleteDC")
	procScreenshotDeleteObject = gdi32.NewProc("DeleteObject")
	procGetDIBits              = gdi32.NewProc("GetDIBits")
)

const (
	smCxScreen   = 0 // SM_CXSCREEN
	smCyScreen   = 1 // SM_CYSCREEN
	srcCopy      = 0x00CC0020
	biRGB        = 0
	dibRGBColors = 0
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

// nativeScreenshot captures the entire screen using Windows GDI APIs directly
// from Go, without spawning any external process. Returns base64-encoded PNG.
func nativeScreenshot() (string, error) {
	// Note: DPI awareness is NOT set here. Wails sets Per-Monitor DPI Aware V2
	// during startup, which already ensures GetSystemMetrics returns physical
	// pixel dimensions. The legacy SetProcessDPIAware() was previously called
	// in init() but that conflicted with Wails' DPI initialization, causing
	// frameless window content to be offset/clipped on Windows 10.
	// Per-Monitor V2 is a superset of System DPI Aware, so screenshot capture
	// at full resolution works correctly without any additional DPI calls.
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
	defer procScreenshotDeleteDC.Call(memDC)

	hBitmap, _, _ := procCreateCompatibleBitmap.Call(hDC, uintptr(w), uintptr(h))
	if hBitmap == 0 {
		return "", fmt.Errorf("CreateCompatibleBitmap failed")
	}
	defer procScreenshotDeleteObject.Call(hBitmap)

	old, _, _ := procScreenshotSelectObject.Call(memDC, hBitmap)
	defer procScreenshotSelectObject.Call(memDC, old)

	ret, _, _ := procBitBlt.Call(memDC, 0, 0, uintptr(w), uintptr(h), hDC, 0, 0, srcCopy)
	if ret == 0 {
		return "", fmt.Errorf("BitBlt failed")
	}

	// Read pixel data via GetDIBits.
	bmi := bitmapInfoHeader{
		BiSize:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		BiWidth:       int32(w),
		BiHeight:      -int32(h), // top-down
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

	// Convert BGRA → RGBA and build image.Image.
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
