//go:build windows

// cursorprobe reproduces the disappearing-cursor bug on a running window.
// It finds the wailsWindow top-level window, dumps its child-window tree,
// maximizes it, moves the real cursor over it with SetCursorPos, and reports
// GetCursor(), WindowFromPoint, and per-window class cursors before/after.
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")

	procEnumWindows              = user32.NewProc("EnumWindows")
	procEnumChildWindows         = user32.NewProc("EnumChildWindows")
	procGetClassNameW            = user32.NewProc("GetClassNameW")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procGetWindowLongPtrW        = user32.NewProc("GetWindowLongPtrW")
	procGetClassLongPtrW         = user32.NewProc("GetClassLongPtrW")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procShowWindow               = user32.NewProc("ShowWindow")
	procSetCursorPos             = user32.NewProc("SetCursorPos")
	procGetCursorPos             = user32.NewProc("GetCursorPos")
	procGetCursor                = user32.NewProc("GetCursor")
	procGetCursorInfo            = user32.NewProc("GetCursorInfo")
	procWindowFromPoint          = user32.NewProc("WindowFromPoint")
	procGetWindow                = user32.NewProc("GetWindow")
	procIsIconic                 = user32.NewProc("IsIconic")
	procIsZoomed                 = user32.NewProc("IsZoomed")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procSendInput                = user32.NewProc("SendInput")
	procSetWindowLongPtrW        = user32.NewProc("SetWindowLongPtrW")
	procSetWindowPos             = user32.NewProc("SetWindowPos")
	procGetSystemMetrics         = user32.NewProc("GetSystemMetrics")
	procPostMessageW             = user32.NewProc("PostMessageW")
)

const (
	gwlStyle   = -16
	gwlExStyle = -20
	gclpHcursor = -12

	wsOverlappedWindow = 0x00CF0000
	wsPopup            = 0x80000000
	wsVisible          = 0x10000000

	hwndTop          = 0
	swpNoZOrder      = 0x4
	swpNoMove        = 0x2
	swpNoSize        = 0x1
	swpNoOwnerZOrder = 0x200
	swpFrameChanged  = 0x20
	gwChild     = 5
	gwHwndNext  = 2
	swMaximize  = 3
	swRestore   = 9
)

type rect struct{ Left, Top, Right, Bottom int32 }
type point struct{ X, Y int32 }

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

func windowRect(hwnd uintptr) rect {
	var r rect
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

func longPtr(hwnd uintptr, idx int32) uintptr {
	r, _, _ := procGetWindowLongPtrW.Call(hwnd, uintptr(idx))
	return r
}

func classCursor(hwnd uintptr) uintptr {
	idx := int32(gclpHcursor)
	r, _, _ := procGetClassLongPtrW.Call(hwnd, uintptr(idx))
	return r
}

func visible(hwnd uintptr) bool {
	r, _, _ := procIsWindowVisible.Call(hwnd)
	return r != 0
}

func styleStr(style uintptr) string {
	var b strings.Builder
	if style&0x80000000 != 0 {
		b.WriteString(" WS_DISABLED")
	}
	if style&0x40000000 != 0 {
		b.WriteString(" WS_CHILD")
	}
	if style&0x10000000 != 0 {
		b.WriteString(" WS_VISIBLE")
	}
	if style&0x02000000 != 0 {
		b.WriteString(" WS_MAXIMIZE")
	}
	if style&0x20000000 != 0 {
		b.WriteString(" WS_MINIMIZE")
	}
	if style&0x8000000 != 0 {
		b.WriteString(" WS_POPUP")
	}
	return b.String()
}

func exStyleStr(ex uintptr) string {
	var b strings.Builder
	if ex&0x20 != 0 {
		b.WriteString(" WS_EX_TRANSPARENT")
	}
	if ex&0x80000 != 0 {
		b.WriteString(" WS_EX_LAYERED")
	}
	if ex&0x08000000 != 0 {
		b.WriteString(" WS_EX_NOACTIVATE")
	}
	if ex&0x80 != 0 {
		b.WriteString(" WS_EX_TOOLWINDOW")
	}
	if ex&0x02000000 != 0 {
		b.WriteString(" WS_EX_NOREDIRECTIONBITMAP")
	}
	return b.String()
}

func enumChildren(parent uintptr) []uintptr {
	var out []uintptr
	cb := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		out = append(out, hwnd)
		return 1
	})
	procEnumChildWindows.Call(parent, cb)
	return out
}

func dumpTree(hwnd uintptr, depth int) {
	r := windowRect(hwnd)
	style := longPtr(hwnd, gwlStyle)
	ex := longPtr(hwnd, gwlExStyle)
	fmt.Printf("%*shwnd=%#x class=%q style=%#x%s ex=%#x%s vis=%v rect=[%d,%d-%d,%d] classcur=%#x title=%q\n",
		depth*2, "", hwnd, className(hwnd), style, styleStr(style), ex, exStyleStr(ex),
		visible(hwnd), r.Left, r.Top, r.Right, r.Bottom, classCursor(hwnd), windowText(hwnd))
	child, _, _ := procGetWindow.Call(hwnd, gwChild)
	for child != 0 {
		dumpTree(child, depth+1)
		child, _, _ = procGetWindow.Call(child, gwHwndNext)
	}
}

func findWailsWindows() []uintptr {
	var out []uintptr
	cb := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		if className(hwnd) == "wailsWindow" {
			out = append(out, hwnd)
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return out
}

func getCursor() uintptr {
	r, _, _ := procGetCursor.Call()
	return r
}

func windowFromPoint(x, y int32) uintptr {
	r, _, _ := procWindowFromPoint.Call(uintptr(int64(int64(y))<<32|int64(uint32(x))))
	return r
}

type kbdInput struct {
	Type  uint32
	_     [4]byte // union starts 8-byte aligned on amd64
	Vk    uint16
	Scan  uint16
	Flags uint32
	Time  uint32
	Extra uintptr
	_     [8]byte // union is 32B; Extra lands at 24+8=32, pad to 40 total
}

// sendF11 injects a real F11 key press so the companion page toggles its own
// WindowFullscreen code path.
func sendF11() {
	down := kbdInput{Type: 1, Vk: 0x7A}                    // INPUT_KEYBOARD, VK_F11
	up := kbdInput{Type: 1, Vk: 0x7A, Flags: 0x0002}      // KEYEVENTF_KEYUP
	procSendInput.Call(1, uintptr(unsafe.Pointer(&down)), unsafe.Sizeof(down))
	time.Sleep(60 * time.Millisecond)
	procSendInput.Call(1, uintptr(unsafe.Pointer(&up)), unsafe.Sizeof(up))
}

func pidOf(hwnd uintptr) uint32 {
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

// INPUT: type(4) + pad(4) + 32B union = 40B on amd64. Go already aligns
// Extra (ULONG_PTR) to offset 32, so no trailing pad is needed here.
type mouseInput struct {
	Type  uint32
	_     [4]byte
	Dx    int32
	Dy    int32
	Data  uint32
	Flags uint32
	Time  uint32
	Extra uintptr
}

// injectMouseMove goes through the real input path (RIT) — SetCursorPos only
// teleports the sprite and does not exercise WM_MOUSEMOVE/WM_SETCURSOR.
func injectMouseMove(x, y int32) {
	abs := func(v, max int32) int32 {
		if max <= 1 {
			return v
		}
		return v * 65535 / (max - 1)
	}
	mi := mouseInput{Type: 0, Dx: abs(x, 2560), Dy: abs(y, 1440), Flags: 0x0001 | 0x8000}
	n, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&mi)), unsafe.Sizeof(mi))
	if n == 0 {
		fmt.Printf("  [inject] sizeof(INPUT)=%d (want 40)\n", unsafe.Sizeof(mi))
	}
	var pos point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pos)))
	fmt.Printf("  [inject] -> (%d,%d) sent=%d err=%v actual=(%d,%d)\n", x, y, n, err, pos.X, pos.Y)
}

// findRenderWidget returns the topmost Chrome_RenderWidgetHostHWND under hwnd.
func findRenderWidget(hwnd uintptr) uintptr {
	var found uintptr
	cb := syscall.NewCallback(func(child, param uintptr) uintptr {
		if className(child) == "Chrome_RenderWidgetHostHWND" && found == 0 {
			found = child
		}
		return 1
	})
	procEnumChildWindows.Call(hwnd, cb, 0)
	return found
}

// postF11 delivers WM_KEYDOWN/UP directly to the render widget — the page's
// F11 handler then runs the same WindowFullscreen path as the 全屏 button.
func postF11(hwnd uintptr) {
	widget := findRenderWidget(hwnd)
	fmt.Printf("render widget=%#x\n", widget)
	if widget == 0 {
		return
	}
	const wmKeyDown, wmKeyUp, vkF11 = 0x100, 0x101, 0x7A
	procPostMessageW.Call(widget, wmKeyDown, vkF11, 0x003B0001)
	time.Sleep(80 * time.Millisecond)
	procPostMessageW.Call(widget, wmKeyUp, vkF11, 0xC03B0001)
}

func raise(hwnd uintptr) {
	// HWND_TOPMOST keeps the target above Edge/IDE windows so injected input
	// actually reaches the app under test.
	procSetWindowPos.Call(hwnd, ^uintptr(0), 0, 0, 0, 0, swpNoMove|swpNoSize)
	procSetForegroundWindow.Call(hwnd)
}

func lower(hwnd uintptr) {
	// HWND_NOTOPMOST = -2
	procSetWindowPos.Call(hwnd, ^uintptr(1), 0, 0, 0, 0, swpNoMove|swpNoSize)
}

type cursorInfo struct {
	Size   uint32
	Flags  uint32
	Cursor uintptr
	Pos    point
}

// cursorShowing reports the system-wide sprite state — the truth about
// visibility regardless of which thread owns the display count.
func cursorShowing() (bool, uintptr) {
	var ci cursorInfo
	ci.Size = uint32(unsafe.Sizeof(ci))
	r, _, _ := procGetCursorInfo.Call(uintptr(unsafe.Pointer(&ci)))
	if r == 0 {
		return true, 0
	}
	return ci.Flags&1 != 0, ci.Cursor // CURSOR_SHOWING
}

func probe(tag string, hwnd uintptr, px, py int32) {
	injectMouseMove(px, py)
	time.Sleep(350 * time.Millisecond)
	cur := getCursor()
	showing, shownCur := cursorShowing()
	under := windowFromPoint(px, py)
	ours := "other-app"
	if pidOf(under) == pidOf(hwnd) {
		ours = "ours"
	}
	fmt.Printf("[%s] point=(%d,%d) GetCursor=%#x SHOWING=%v shown=%#x WindowFromPoint=%#x class=%q classcur=%#x hit=%s\n",
		tag, px, py, cur, showing, shownCur, under, className(under), classCursor(under), ours)
	r, _, _ := procIsIconic.Call(hwnd)
	z, _, _ := procIsZoomed.Call(hwnd)
	fmt.Printf("[%s] hwnd=%#x iconic=%d zoomed=%d fg=%#x\n", tag, hwnd, r, z, func() uintptr { f, _, _ := procGetForegroundWindow.Call(); return f }())
}

func main() {
	waitSec := 3
	if len(os.Args) > 1 {
		if v, err := strconv.Atoi(os.Args[1]); err == nil {
			waitSec = v
		}
	}
	// Save the real cursor position to restore it at the end.
	var orig point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&orig)))
	defer procSetCursorPos.Call(uintptr(orig.X), uintptr(orig.Y))

	wins := findWailsWindows()
	fmt.Printf("wailsWindow count=%d\n", len(wins))
	for _, h := range wins {
		fmt.Printf("  hwnd=%#x pid=%d title=%q\n", h, pidOf(h), windowText(h))
	}
	if len(wins) == 0 {
		fmt.Println("no wailsWindow found; start the companion first")
		return
	}
	hwnd := wins[0]
	defer lower(hwnd)
	raise(hwnd)
	time.Sleep(400 * time.Millisecond)
	fmt.Println("=== tree before ===")
	dumpTree(hwnd, 0)
	fmt.Printf("GetCursor=%#x before any move\n", getCursor())

	// Move over the window while normal: baseline.
	r := windowRect(hwnd)
	probe("normal", hwnd, r.Left+(r.Right-r.Left)/2, r.Top+(r.Bottom-r.Top)/3)

	procShowWindow.Call(hwnd, swMaximize)
	time.Sleep(800 * time.Millisecond)
	fmt.Println("=== tree after maximize ===")
	dumpTree(hwnd, 0)

	r = windowRect(hwnd)
	for i := 0; i < 3; i++ {
		probe(fmt.Sprintf("max-%d", i), hwnd, r.Left+(r.Right-r.Left)/2+int32(i)*40, r.Top+(r.Bottom-r.Top)/3)
	}

	// Emulate Form.Fullscreen(): strip WS_OVERLAPPEDWINDOW, add
	// WS_POPUP|WS_VISIBLE, then cover the monitor. This reproduces the
	// companion's 全屏 button path (runtime WindowFullscreen) without
	// needing the page's F11 handler.
	styleIdx := int32(gwlStyle)
	savedStyle := longPtr(hwnd, gwlStyle)
	procShowWindow.Call(hwnd, swRestore)
	time.Sleep(500 * time.Millisecond)
	popup := (savedStyle &^ wsOverlappedWindow) | wsPopup | wsVisible
	procSetWindowLongPtrW.Call(hwnd, uintptr(styleIdx), popup)
	cx, _, _ := procGetSystemMetrics.Call(0)
	cy, _, _ := procGetSystemMetrics.Call(1)
	procSetWindowPos.Call(hwnd, hwndTop, 0, 0, cx, cy, swpNoOwnerZOrder|swpFrameChanged)
	time.Sleep(900 * time.Millisecond)
	fmt.Println("=== tree after emulated WS_POPUP fullscreen ===")
	dumpTree(hwnd, 0)

	r = windowRect(hwnd)
	for i := 0; i < 4; i++ {
		probe(fmt.Sprintf("fs-%d", i), hwnd, r.Left+(r.Right-r.Left)/2+int32(i)*40, r.Top+(r.Bottom-r.Top)/3)
	}
	// The player's second render widget owns the top-left region.
	probe("player-0", hwnd, 400, 250)
	probe("player-1", hwnd, 600, 400)
	probe("player-edge", hwnd, 799, 499)
	probe("cross-main", hwnd, 1280, 529)
	probe("player-again", hwnd, 400, 250)

	// Undo the fullscreen emulation.
	procSetWindowLongPtrW.Call(hwnd, uintptr(styleIdx), savedStyle)
	procSetWindowPos.Call(hwnd, 0, 480, 196, 1600, 1000, swpNoZOrder|swpFrameChanged)
	time.Sleep(800 * time.Millisecond)
	r = windowRect(hwnd)
	probe("after-fs-restore", hwnd, r.Left+(r.Right-r.Left)/2, r.Top+(r.Bottom-r.Top)/3)

	// Real path: the page's own F11 -> toggleReadingFrame -> WindowFullscreen.
	postF11(hwnd)
	time.Sleep(1200 * time.Millisecond)
	fmt.Println("=== tree after page F11 ===")
	dumpTree(hwnd, 0)
	r = windowRect(hwnd)
	for i := 0; i < 4; i++ {
		probe(fmt.Sprintf("realfs-%d", i), hwnd, r.Left+(r.Right-r.Left)/2+int32(i)*40, r.Top+(r.Bottom-r.Top)/3)
	}
	postF11(hwnd)
	time.Sleep(800 * time.Millisecond)
	r = windowRect(hwnd)
	probe("realfs-exit", hwnd, r.Left+(r.Right-r.Left)/2, r.Top+(r.Bottom-r.Top)/3)

	time.Sleep(time.Duration(waitSec) * time.Second)
}
