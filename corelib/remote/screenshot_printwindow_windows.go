//go:build windows

package remote

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"sync"
	"syscall"
	"unsafe"
)

// PrintWindow is the capture that still works when the session display
// driver rejects GDI BitBlt and DXGI duplication. Remote Desktop on a WDDM
// indirect display returns ERROR_INVALID_HANDLE / E_ACCESSDENIED for both.
// PW_RENDERFULLCONTENT paints each real top-level window into a memory DC.
const pwRenderFullContent = 2

const (
	wsExToolWindow = 0x00000080
	dwmwaCloaked   = 14
	// EnumWindows is topmost-first. A few hundred candidates fill up before a
	// window on another monitor is seen, and the occlusion filter never gets
	// that window. This bound only stops a runaway z-order. The cell filter
	// decides which of those windows are painted.
	printWindowEnumMax   = 4096
	printWindowMinPx     = 8
	wsExLayered          = 0x00080000
	printWindowCell      = 64
	printWindowBackdrop  = 30
	printWindowMaxEdgePx = 16384
)

var (
	user32Print               = syscall.NewLazyDLL("user32.dll")
	gdi32Print                = syscall.NewLazyDLL("gdi32.dll")
	dwmapiPrint               = syscall.NewLazyDLL("dwmapi.dll")
	procEnumWindowsPrint      = user32Print.NewProc("EnumWindows")
	procIsWindowVisiblePrint  = user32Print.NewProc("IsWindowVisible")
	procIsIconicPrint         = user32Print.NewProc("IsIconic")
	procGetWindowRectPrint    = user32Print.NewProc("GetWindowRect")
	procGetWindowLongPtrPrint = user32Print.NewProc("GetWindowLongPtrW")
	procPrintWindow           = user32Print.NewProc("PrintWindow")
	procDwmGetWindowAttribute = dwmapiPrint.NewProc("DwmGetWindowAttribute")
	procCreateCompatibleDC2   = gdi32Print.NewProc("CreateCompatibleDC")
	procCreateDIBSection      = gdi32Print.NewProc("CreateDIBSection")
	procSelectObject2         = gdi32Print.NewProc("SelectObject")
	procDeleteDC2             = gdi32Print.NewProc("DeleteDC")
	procDeleteObject2         = gdi32Print.NewProc("DeleteObject")

	printEnumOnce sync.Once
	printEnumCB   uintptr
	printEnumMu   sync.Mutex
	printEnumHits []printWindowInfo
	printEnumRect printRect
)

type printRect struct {
	left, top, right, bottom int32
}

type printWindowInfo struct {
	hwnd       uintptr
	x, y, w, h int
	// occludes is false for layered windows. Their bounds must not hide the
	// window underneath, because PrintWindow of a transparent overlay is not
	// the desktop the user sees.
	occludes bool
}

func screenshotUsable(b64 string, err error) bool {
	return err == nil && b64 != "" && !IsBlankImage(b64)
}

// finishScreenshot returns a GDI capture when it has pixels. When the
// display driver refuses BitBlt, it composites visible windows with
// PrintWindow instead of reporting a display-access failure.
func finishScreenshot(x, y, w, h int, b64 string, err error) (string, error) {
	if screenshotUsable(b64, err) {
		return b64, nil
	}
	if w > 0 && h > 0 && w <= printWindowMaxEdgePx && h <= printWindowMaxEdgePx {
		if pw, pwErr := printWindowComposite(x, y, w, h); pwErr == nil && pw != "" {
			return pw, nil
		}
	}
	if err != nil {
		return "", err
	}
	if b64 != "" {
		return "", fmt.Errorf("screenshot is blank — display may be off or locked")
	}
	return "", fmt.Errorf("screenshot failed")
}

func printWindowComposite(originX, originY, width, height int) (string, error) {
	windows := listPrintWindows(originX, originY, width, height)
	if len(windows) == 0 {
		return "", fmt.Errorf("printwindow: no visible windows")
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.RGBA{R: printWindowBackdrop, G: printWindowBackdrop, B: printWindowBackdrop, A: 255}}, image.Point{}, draw.Src)
	// Topmost first. A cell is reserved only after it has ink, so a blank
	// PrintWindow does not hide the window behind it.
	painted := paintOrderedWindows(dst, windows, originX, originY, capturePrintWindowRaster)
	if painted == 0 {
		return "", fmt.Errorf("printwindow: windows did not paint")
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return "", fmt.Errorf("printwindow png: %w", err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func listPrintWindows(originX, originY, width, height int) []printWindowInfo {
	printEnumMu.Lock()
	defer printEnumMu.Unlock()
	printEnumOnce.Do(func() {
		printEnumCB = syscall.NewCallback(printEnumProc)
	})
	printEnumHits = printEnumHits[:0]
	printEnumRect = printRect{
		left:   int32(originX),
		top:    int32(originY),
		right:  int32(originX + width),
		bottom: int32(originY + height),
	}
	procEnumWindowsPrint.Call(printEnumCB, 0)
	out := make([]printWindowInfo, len(printEnumHits))
	copy(out, printEnumHits)
	if len(out) > printWindowEnumMax {
		out = out[:printWindowEnumMax]
	}
	return out
}

// windowsForPrint applies the enumeration bound, then drops windows that sit
// fully behind an opaque one. The bound is not a paint budget.
func windowsForPrint(hits []printWindowInfo, originX, originY, width, height int) []printWindowInfo {
	if len(hits) > printWindowEnumMax {
		hits = hits[:printWindowEnumMax]
	}
	return filterVisiblePrintWindows(hits, originX, originY, width, height)
}

// filterVisiblePrintWindows keeps every window that still shows at least one
// cell of the capture. Enumeration is topmost-first, so a window fully behind
// an opaque one is not painted. A hard count used to stop after the top 48
// and leave the rest of a second monitor as empty backdrop.
func filterVisiblePrintWindows(hits []printWindowInfo, originX, originY, width, height int) []printWindowInfo {
	if len(hits) == 0 || width <= 0 || height <= 0 {
		return nil
	}
	cols := (width + printWindowCell - 1) / printWindowCell
	rows := (height + printWindowCell - 1) / printWindowCell
	if cols <= 0 || rows <= 0 || cols > 4096 || rows > 4096 {
		return hits
	}
	covered := make([]bool, cols*rows)
	kept := make([]printWindowInfo, 0, len(hits))
	for _, win := range hits {
		if !printWindowClaimsCell(covered, cols, win.x-originX, win.y-originY, win.w, win.h, false) {
			continue
		}
		kept = append(kept, win)
		if win.occludes {
			printWindowClaimsCell(covered, cols, win.x-originX, win.y-originY, win.w, win.h, true)
		}
	}
	return kept
}

func printWindowClaimsCell(covered []bool, cols, x, y, w, h int, mark bool) bool {
	if w <= 0 || h <= 0 || cols <= 0 || x+w <= 0 || y+h <= 0 {
		return false
	}
	c0 := x / printWindowCell
	r0 := y / printWindowCell
	c1 := (x + w - 1) / printWindowCell
	r1 := (y + h - 1) / printWindowCell
	if c1 < 0 || r1 < 0 {
		return false
	}
	rows := len(covered) / cols
	if c0 >= cols || r0 >= rows {
		return false
	}
	if c0 < 0 {
		c0 = 0
	}
	if r0 < 0 {
		r0 = 0
	}
	if c1 >= cols {
		c1 = cols - 1
	}
	if r1 >= rows {
		r1 = rows - 1
	}
	hit := false
	for r := r0; r <= r1; r++ {
		for c := c0; c <= c1; c++ {
			idx := r*cols + c
			if idx < 0 || idx >= len(covered) {
				continue
			}
			if !covered[idx] {
				hit = true
				if mark {
					covered[idx] = true
				}
			} else if mark {
				covered[idx] = true
			}
		}
	}
	return hit
}

func printEnumProc(hwnd, _ uintptr) uintptr {
	if len(printEnumHits) >= printWindowEnumMax {
		return 0
	}
	if info, ok := printWindowCandidate(hwnd, printEnumRect); ok {
		printEnumHits = append(printEnumHits, info)
	}
	return 1
}

func printWindowCandidate(hwnd uintptr, target printRect) (printWindowInfo, bool) {
	if hwnd == 0 {
		return printWindowInfo{}, false
	}
	visible, _, _ := procIsWindowVisiblePrint.Call(hwnd)
	if visible == 0 {
		return printWindowInfo{}, false
	}
	iconic, _, _ := procIsIconicPrint.Call(hwnd)
	if iconic != 0 {
		return printWindowInfo{}, false
	}
	gwlExStyle := int32(-20) // GWL_EXSTYLE
	ex, _, _ := procGetWindowLongPtrPrint.Call(hwnd, uintptr(gwlExStyle))
	if ex&wsExToolWindow != 0 {
		return printWindowInfo{}, false
	}
	occludes := ex&wsExLayered == 0
	var cloaked uint32
	hr, _, _ := procDwmGetWindowAttribute.Call(hwnd, dwmwaCloaked, uintptr(unsafe.Pointer(&cloaked)), unsafe.Sizeof(cloaked))
	if hr == 0 && cloaked != 0 {
		return printWindowInfo{}, false
	}
	var wr printRect
	ret, _, _ := procGetWindowRectPrint.Call(hwnd, uintptr(unsafe.Pointer(&wr)))
	if ret == 0 || wr.right <= wr.left || wr.bottom <= wr.top {
		return printWindowInfo{}, false
	}
	w := int(wr.right - wr.left)
	h := int(wr.bottom - wr.top)
	if w < printWindowMinPx || h < printWindowMinPx || w > printWindowMaxEdgePx || h > printWindowMaxEdgePx {
		return printWindowInfo{}, false
	}
	if wr.right <= target.left || wr.left >= target.right || wr.bottom <= target.top || wr.top >= target.bottom {
		return printWindowInfo{}, false
	}
	return printWindowInfo{hwnd: hwnd, x: int(wr.left), y: int(wr.top), w: w, h: h, occludes: occludes}, true
}

// paintOrderedWindows draws topmost-first. Pixels land only in cells that
// are still free, and a cell stays free until the bitmap there has ink.
func paintOrderedWindows(dst *image.RGBA, windows []printWindowInfo, originX, originY int, raster func(printWindowInfo) ([]byte, bool)) int {
	if dst == nil {
		return 0
	}
	width, height := dst.Bounds().Dx(), dst.Bounds().Dy()
	cols := (width + printWindowCell - 1) / printWindowCell
	rows := (height + printWindowCell - 1) / printWindowCell
	useCells := cols > 0 && rows > 0 && cols <= 4096 && rows <= 4096
	var covered []bool
	if useCells {
		covered = make([]bool, cols*rows)
	}
	painted := 0
	if !useCells {
		for i := len(windows) - 1; i >= 0; i-- {
			win := windows[i]
			src, ok := raster(win)
			if !ok || !bgraHasInk(src, win.w, win.h) {
				continue
			}
			blitBGRA(dst, src, win.w, win.h, win.x-originX, win.y-originY)
			painted++
		}
		return painted
	}
	for _, win := range windows {
		if !printWindowClaimsCell(covered, cols, win.x-originX, win.y-originY, win.w, win.h, false) {
			continue
		}
		src, ok := raster(win)
		if !ok {
			continue
		}
		if blitBGRAFreeCells(dst, src, win.w, win.h, win.x-originX, win.y-originY, covered, cols) {
			painted++
		}
	}
	return painted
}

func capturePrintWindowRaster(win printWindowInfo) ([]byte, bool) {
	bmi := bitmapInfoHeader{
		BiSize:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		BiWidth:       int32(win.w),
		BiHeight:      -int32(win.h),
		BiPlanes:      1,
		BiBitCount:    32,
		BiCompression: biRGB,
		BiSizeImage:   uint32(win.w * win.h * 4),
	}
	var bits unsafe.Pointer
	hbmp, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bmi)), dibRGBColors, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hbmp == 0 || bits == nil {
		if hbmp != 0 {
			procDeleteObject2.Call(hbmp)
		}
		return nil, false
	}
	memDC, _, _ := procCreateCompatibleDC2.Call(0)
	if memDC == 0 {
		procDeleteObject2.Call(hbmp)
		return nil, false
	}
	old, _, _ := procSelectObject2.Call(memDC, hbmp)
	ret, _, _ := procPrintWindow.Call(win.hwnd, memDC, pwRenderFullContent)
	n := win.w * win.h * 4
	var src []byte
	if ret != 0 && bits != nil && n > 0 {
		src = make([]byte, n)
		copy(src, unsafe.Slice((*byte)(bits), n))
	}
	if old != 0 {
		procSelectObject2.Call(memDC, old)
	}
	procDeleteDC2.Call(memDC)
	procDeleteObject2.Call(hbmp)
	return src, src != nil
}

func bgraHasInk(src []byte, w, h int) bool {
	n := w * h
	if n == 0 || len(src) < n*4 {
		return false
	}
	step := 1
	if n > 10000 {
		step = int(isqrt(uint64(n / 10000)))
		if step < 1 {
			step = 1
		}
	}
	var total, count uint64
	for i := 0; i < n; i += step {
		o := i * 4
		total += (uint64(src[o]) + uint64(src[o+1]) + uint64(src[o+2])) / 3
		count++
	}
	return count > 0 && total/count > uint64(blankImageThreshold)
}

func blitBGRA(dst *image.RGBA, src []byte, srcW, srcH, destX, destY int) {
	if dst == nil || srcW <= 0 || srcH <= 0 || len(src) < srcW*srcH*4 {
		return
	}
	bounds := dst.Bounds()
	x0, y0 := destX, destY
	x1, y1 := destX+srcW, destY+srcH
	if x0 < bounds.Min.X {
		x0 = bounds.Min.X
	}
	if y0 < bounds.Min.Y {
		y0 = bounds.Min.Y
	}
	if x1 > bounds.Max.X {
		x1 = bounds.Max.X
	}
	if y1 > bounds.Max.Y {
		y1 = bounds.Max.Y
	}
	for y := y0; y < y1; y++ {
		sy := y - destY
		row := sy * srcW
		di := dst.PixOffset(x0, y)
		for x := x0; x < x1; x++ {
			si := (row + (x - destX)) * 4
			dst.Pix[di] = src[si+2]
			dst.Pix[di+1] = src[si+1]
			dst.Pix[di+2] = src[si]
			dst.Pix[di+3] = 255
			di += 4
		}
	}
}

// blitBGRAFreeCells copies only cells that are still free and that contain
// ink. Those cells are then reserved. A black PrintWindow cell stays open
// for the window behind it.
func blitBGRAFreeCells(dst *image.RGBA, src []byte, srcW, srcH, destX, destY int, covered []bool, cols int) bool {
	if dst == nil || srcW <= 0 || srcH <= 0 || cols <= 0 || len(covered) == 0 || len(src) < srcW*srcH*4 {
		return false
	}
	bounds := dst.Bounds()
	x0, y0 := destX, destY
	x1, y1 := destX+srcW, destY+srcH
	if x0 < bounds.Min.X {
		x0 = bounds.Min.X
	}
	if y0 < bounds.Min.Y {
		y0 = bounds.Min.Y
	}
	if x1 > bounds.Max.X {
		x1 = bounds.Max.X
	}
	if y1 > bounds.Max.Y {
		y1 = bounds.Max.Y
	}
	if x0 >= x1 || y0 >= y1 {
		return false
	}
	rows := len(covered) / cols
	wrote := false
	for cy := y0 / printWindowCell; cy <= (y1-1)/printWindowCell; cy++ {
		for cx := x0 / printWindowCell; cx <= (x1-1)/printWindowCell; cx++ {
			if cx < 0 || cy < 0 || cx >= cols || cy >= rows {
				continue
			}
			idx := cy*cols + cx
			if idx < 0 || idx >= len(covered) || covered[idx] {
				continue
			}
			ix0 := cx * printWindowCell
			iy0 := cy * printWindowCell
			ix1 := ix0 + printWindowCell
			iy1 := iy0 + printWindowCell
			if ix0 < x0 {
				ix0 = x0
			}
			if iy0 < y0 {
				iy0 = y0
			}
			if ix1 > x1 {
				ix1 = x1
			}
			if iy1 > y1 {
				iy1 = y1
			}
			if !rectHasInk(src, srcW, srcH, destX, destY, ix0, iy0, ix1, iy1) {
				continue
			}
			blitBGRARect(dst, src, srcW, srcH, destX, destY, ix0, iy0, ix1, iy1)
			covered[idx] = true
			wrote = true
		}
	}
	return wrote
}

func blitBGRARect(dst *image.RGBA, src []byte, srcW, srcH, destX, destY, x0, y0, x1, y1 int) {
	for y := y0; y < y1; y++ {
		sy := y - destY
		if sy < 0 || sy >= srcH {
			continue
		}
		di := dst.PixOffset(x0, y)
		for x := x0; x < x1; x++ {
			sx := x - destX
			if sx < 0 || sx >= srcW {
				di += 4
				continue
			}
			si := (sy*srcW + sx) * 4
			dst.Pix[di] = src[si+2]
			dst.Pix[di+1] = src[si+1]
			dst.Pix[di+2] = src[si]
			dst.Pix[di+3] = 255
			di += 4
		}
	}
}

func rectHasInk(src []byte, srcW, srcH, destX, destY, x0, y0, x1, y1 int) bool {
	var total, count uint64
	for y := y0; y < y1; y++ {
		sy := y - destY
		if sy < 0 || sy >= srcH {
			continue
		}
		for x := x0; x < x1; x++ {
			sx := x - destX
			if sx < 0 || sx >= srcW {
				continue
			}
			o := (sy*srcW + sx) * 4
			if o+2 >= len(src) {
				continue
			}
			total += (uint64(src[o]) + uint64(src[o+1]) + uint64(src[o+2])) / 3
			count++
		}
	}
	return count > 0 && total/count > uint64(blankImageThreshold)
}
