//go:build windows

package maclawcapture

// hwndView adapts a native top-level window (the companion player popup) to
// the View interface: Pointer is the window handle the capture pipeline can
// rasterise via PrintWindow(PW_RENDERFULLCONTENT), which reaches
// DirectX-composited WebView2 content that a plain screen BitBlt would miss.

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32Capture      = windows.NewLazySystemDLL("user32.dll")
	procPrintWindow    = user32Capture.NewProc("PrintWindow")
	procGetWindowRectC = user32Capture.NewProc("GetWindowRect")
	procGetDCWin       = user32Capture.NewProc("GetDC")
	procReleaseDCWin   = user32Capture.NewProc("ReleaseDC")
	gdi32Capture       = windows.NewLazySystemDLL("gdi32.dll")
	procCreateCompatDC = gdi32Capture.NewProc("CreateCompatibleDC")
	procCreateCompatBM = gdi32Capture.NewProc("CreateCompatibleBitmap")
	procSelectObj      = gdi32Capture.NewProc("SelectObject")
	procDeleteObj      = gdi32Capture.NewProc("DeleteObject")
	procDeleteDC       = gdi32Capture.NewProc("DeleteDC")
	procGetDIBits      = gdi32Capture.NewProc("GetDIBits")
)

type rectWin struct{ Left, Top, Right, Bottom int32 }

type bitmapinfoheaderWin struct {
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

// HwndView is a View backed by a native window handle.
type HwndView struct {
	Hwnd     uintptr
	IsFullFn func() bool
}

func (v *HwndView) Pointer() uintptr { return v.Hwnd }

func (v *HwndView) ContainsFullScreen() bool {
	if v.IsFullFn != nil {
		return v.IsFullFn()
	}
	return false
}

// CaptureWindowPNG rasterises the window via PrintWindow. The ctx exists for
// signature parity with the async COM CapturePreview path; PrintWindow is a
// single synchronous call.
func (v *HwndView) CaptureWindowPNG(ctx context.Context) ([]byte, error) {
	var r rectWin
	procGetWindowRectC.Call(v.Hwnd, uintptr(unsafe.Pointer(&r)))
	w := int(r.Right - r.Left)
	h := int(r.Bottom - r.Top)
	if w <= 0 || h <= 0 {
		return nil, syscall.EINVAL
	}
	screenDC, _, _ := procGetDCWin.Call(0)
	if screenDC == 0 {
		return nil, syscall.EINVAL
	}
	defer procReleaseDCWin.Call(0, screenDC)
	memDC, _, _ := procCreateCompatDC.Call(screenDC)
	if memDC == 0 {
		return nil, syscall.EINVAL
	}
	defer procDeleteDC.Call(memDC)
	bmp, _, _ := procCreateCompatBM.Call(screenDC, uintptr(w), uintptr(h))
	if bmp == 0 {
		return nil, syscall.EINVAL
	}
	defer procDeleteObj.Call(bmp)
	old, _, _ := procSelectObj.Call(memDC, bmp)
	defer procSelectObj.Call(memDC, old)
	ok, _, _ := procPrintWindow.Call(v.Hwnd, memDC, 2) // PW_RENDERFULLCONTENT
	if ok == 0 {
		return nil, syscall.EINVAL
	}
	header := bitmapinfoheaderWin{
		Size:     40,
		Width:    int32(w),
		Height:   -int32(h),
		Planes:   1,
		BitCount: 32,
	}
	pix := make([]byte, w*h*4)
	procGetDIBits.Call(memDC, bmp, 0, uintptr(h), uintptr(unsafe.Pointer(&pix[0])), uintptr(unsafe.Pointer(&header)), 0)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		src := (h - 1 - y) * w * 4
		dst := y * img.Stride
		for x := 0; x < w; x++ {
			img.Pix[dst+x*4] = pix[src+x*4+2]
			img.Pix[dst+x*4+1] = pix[src+x*4+1]
			img.Pix[dst+x*4+2] = pix[src+x*4]
			img.Pix[dst+x*4+3] = pix[src+x*4+3]
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
