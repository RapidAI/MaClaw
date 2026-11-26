//go:build windows

// cursorverify checks the live cursor watch on the test companion window:
// it maximizes the wailsWindow owned by the given pid, parks the real cursor
// on the corner resize band (where edge tracking paints a sizing cursor in a
// maximized window), and polls the cursor handle. The watch should detect
// sizing+maximized and log "[cursor] abnormal cursor" to maclaw.log.
package main

import (
	"fmt"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")

	procEnumWindows                = user32.NewProc("EnumWindows")
	procGetWindowTextW             = user32.NewProc("GetWindowTextW")
	procGetClassNameW              = user32.NewProc("GetClassNameW")
	procGetWindowRect              = user32.NewProc("GetWindowRect")
	procShowWindow                 = user32.NewProc("ShowWindow")
	procSetCursorPos               = user32.NewProc("SetCursorPos")
	procGetCursorPos               = user32.NewProc("GetCursorPos")
	procGetCursorInfo              = user32.NewProc("GetCursorInfo")
	procIsZoomed                   = user32.NewProc("IsZoomed")
	procSetForegroundWind          = user32.NewProc("SetForegroundWindow")
	procGetWindowThreadProcessId   = user32.NewProc("GetWindowThreadProcessId")
)

type rect struct{ Left, Top, Right, Bottom int32 }
type point struct{ X, Y int32 }
type cursorInfo struct {
	Size     uint32
	Flags    uint32
	HCursor  uintptr
	PtScreen point
}

func className(hwnd uintptr) string {
	var buf [128]uint16
	r, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if r == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:r])
}

func windowText(hwnd uintptr) string {
	var buf [256]uint16
	r, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if r == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:r])
}

func wailsWindowsForPid(pid uint32) []uintptr {
	var out []uintptr
	cb := windows.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if className(hwnd) != "wailsWindow" {
			return 1
		}
		var wp uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&wp)))
		if wp == pid {
			out = append(out, hwnd)
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return out
}

func windowRect(hwnd uintptr) (r rect) {
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return
}

func cursorSnapshot() (uintptr, uint32) {
	info := cursorInfo{Size: uint32(unsafe.Sizeof(cursorInfo{}))}
	r, _, _ := procGetCursorInfo.Call(uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 0, 0
	}
	return info.HCursor, info.Flags
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: cursorverify <pid>")
		return
	}
	var pid uint32
	fmt.Sscan(os.Args[1], &pid)
	wins := wailsWindowsForPid(pid)
	fmt.Printf("wailsWindow count for pid %d: %d\n", pid, len(wins))
	if len(wins) == 0 {
		return
	}
	target := wins[0]
	fmt.Printf("target hwnd=%#x title=%q\n", target, windowText(target))
	run(target)
}

func run(hwnd uintptr) {
	var orig point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&orig)))
	defer procSetCursorPos.Call(uintptr(orig.X), uintptr(orig.Y))

	procSetForegroundWind.Call(hwnd)
	procShowWindow.Call(hwnd, 3) // SW_MAXIMIZE
	time.Sleep(1200 * time.Millisecond)
	zoomed, _, _ := procIsZoomed.Call(hwnd)
	fmt.Printf("maximized IsZoomed=%v\n", zoomed != 0)

	r := windowRect(hwnd)
	fmt.Printf("rect=%d,%d-%d,%d\n", r.Left, r.Top, r.Right, r.Bottom)

	// Park on the bottom-right resize band: 3px inside the corner.
	cx, cy := r.Right-3, r.Bottom-3
	fmt.Printf("parking cursor at %d,%d (corner band)\n", cx, cy)
	procSetCursorPos.Call(uintptr(cx), uintptr(cy))

	// Nudge twice so the renderer definitely sees the move.
	for i := 0; i < 2; i++ {
		time.Sleep(150 * time.Millisecond)
		procSetCursorPos.Call(uintptr(cx-1), uintptr(cy))
		time.Sleep(150 * time.Millisecond)
		procSetCursorPos.Call(uintptr(cx), uintptr(cy))
	}

	fmt.Println("polling cursor for 6s (sizing handle expected; detection should log to maclaw.log):")
	start := time.Now()
	var last uintptr
	for time.Since(start) < 6*time.Second {
		h, flags := cursorSnapshot()
		if h != last {
			fmt.Printf("  t=%5.1fs cursor=%#x flags=%#x\n", time.Since(start).Seconds(), h, flags)
			last = h
		}
		time.Sleep(120 * time.Millisecond)
	}
	fmt.Printf("  final cursor=%#x\n", last)

	// Move to the middle: the edge cursor should clear back to arrow.
	mid := windowRect(hwnd)
	procSetCursorPos.Call(uintptr((mid.Left+mid.Right)/2), uintptr((mid.Top+mid.Bottom)/2))
	time.Sleep(700 * time.Millisecond)
	h, flags := cursorSnapshot()
	fmt.Printf("after center move cursor=%#x flags=%#x (0x10003=arrow expected)\n", h, flags)
}
