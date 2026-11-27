// Command wincap captures a PNG of a process's largest visible window via
// PrintWindow(PW_RENDERFULLCONTENT) — works for WebView2 windows even when
// the desktop session is not being screenshotted by other means.
// Usage: wincap <pid> <out.png>
package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32              = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows     = user32.NewProc("EnumWindows")
	procGetWindowThread = user32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible = user32.NewProc("IsWindowVisible")
	procPrintWindow     = user32.NewProc("PrintWindow")
	procGetWindowRect   = user32.NewProc("GetWindowRect")
)

type rect struct{ Left, Top, Right, Bottom int32 }

func main() {
	if len(os.Args) != 3 {
		fmt.Println("usage: wincap <pid> <out.png>")
		os.Exit(2)
	}
	var target uint32
	fmt.Sscan(os.Args[1], &target)

	var bestHwnd uintptr
	var bestW, bestHt int32
	cb := syscall.NewCallback(func(hwnd, lparam uintptr) uintptr {
		var pid uint32
		procGetWindowThread.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid != target {
			return 1
		}
		vis, _, _ := procIsWindowVisible.Call(hwnd)
		if vis == 0 {
			return 1
		}
		var r rect
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		w, h := r.Right-r.Left, r.Bottom-r.Top
		if w*h > bestW*bestHt {
			bestHwnd, bestW, bestHt = hwnd, w, h
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	if bestHwnd == 0 {
		fmt.Println("no visible window for pid")
		os.Exit(1)
	}
	var r rect
	procGetWindowRect.Call(bestHwnd, uintptr(unsafe.Pointer(&r)))
	fmt.Printf("capturing %dx%d\n", bestW, bestHt)

	img := image.NewRGBA(image.Rect(0, 0, int(bestW), int(bestHt)))
	procGetWindowDC := user32.NewProc("GetWindowDC")
	gdi32 := windows.NewLazySystemDLL("gdi32.dll")
	procCreateCompatibleDC := gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap := gdi32.NewProc("CreateCompatibleBitmap")
	hdc, _, _ := procGetWindowDC.Call(bestHwnd)
	memDC, _, _ := procCreateCompatibleDC.Call(hdc)
	hbm, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(bestW), uintptr(bestHt))
	old, _, _ := gdi32.NewProc("SelectObject").Call(memDC, hbm)
	ret, _, _ := procPrintWindow.Call(bestHwnd, memDC, 2) // PW_RENDERFULLCONTENT
	// copy bitmap bits into the RGBA image (BGRA rows, bottom-up not needed
	// for a top-down DIB created via CreateCompatibleBitmap+GetDIBits-less
	// route: use GetDIBits through syscall for correctness)
	dib := struct {
		Header      bitmapinfoheader
		SpaceUnused [40 * 4]byte
	}{}
	dib.Header.Size = 40
	dib.Header.Planes = 1
	dib.Header.BitCount = 32
	dib.Header.Width = bestW
	dib.Header.Height = -bestHt // top-down
	procGetDIBits := gdi32.NewProc("GetDIBits")
	procGetDIBits.Call(memDC, hbm, 0, uintptr(bestHt), uintptr(unsafe.Pointer(&img.Pix[0])), uintptr(unsafe.Pointer(&dib.Header)), 0)
	user32.NewProc("ReleaseDC").Call(bestHwnd, hdc)
	gdi32.NewProc("DeleteDC").Call(memDC)
	gdi32.NewProc("DeleteObject").Call(old)
	_ = ret

	f, err := os.Create(os.Args[2])
	if err != nil {
		fmt.Println("create:", err)
		os.Exit(1)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		fmt.Println("encode:", err)
		os.Exit(1)
	}
	fmt.Println("saved", os.Args[2])
}

type bitmapinfoheader struct {
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
