//go:build windows

package accessibility

import (
	"fmt"
	"log"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/computeruse"
)

var (
	user32Enum                   = syscall.NewLazyDLL("user32.dll")
	procEnumWindows              = user32Enum.NewProc("EnumWindows")
	procGetWindowTextW           = user32Enum.NewProc("GetWindowTextW")
	procGetWindowTextLengthW     = user32Enum.NewProc("GetWindowTextLengthW")
	procIsWindowVisible          = user32Enum.NewProc("IsWindowVisible")
	procSetForegroundWindow      = user32Enum.NewProc("SetForegroundWindow")
	procAllowSetForegroundWindow = user32Enum.NewProc("AllowSetForegroundWindow")
	procShowWindow               = user32Enum.NewProc("ShowWindow")
	procGetForegroundWindow      = user32Enum.NewProc("GetForegroundWindow")
	procKeybdEvent               = user32Enum.NewProc("keybd_event")
	procIsIconic                 = user32Enum.NewProc("IsIconic")
	procGetWindowPlacement       = user32Enum.NewProc("GetWindowPlacement")
	procSetWindowPos             = user32Enum.NewProc("SetWindowPos")
	procGetWindowRect            = user32Enum.NewProc("GetWindowRect")
	procWindowFromPoint          = user32Enum.NewProc("WindowFromPoint")
	procGetAncestor              = user32Enum.NewProc("GetAncestor")
	procAttachThreadInput        = user32Enum.NewProc("AttachThreadInput")
	procGetWindowThreadProcessId = user32Enum.NewProc("GetWindowThreadProcessId")
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procGetCurrentThreadId       = kernel32.NewProc("GetCurrentThreadId")
)

const (
	swMaximize = 3
	// swShow activates a visible window and keeps its current size.
	// swRestore on a maximized window turns it into a normal rectangle.
	swShow    = 5
	swRestore = 9
	// wpfRestoreToMaximized is WINDOWPLACEMENT.Flags, not a ShowWindow command.
	wpfRestoreToMaximized = 0x0002
	swpNoMove             = 0x0002
	swpNoSize             = 0x0001
	swpShowWindow         = 0x0040
	vkMenu                = 0x12
	keyeventfKeyup        = 0x0002
)

type visibleWindow struct {
	hwnd  uintptr
	title string
}

func listVisibleWindows() []visibleWindow {
	var hits []visibleWindow
	cb := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		vis, _, _ := procIsWindowVisible.Call(hwnd)
		if vis == 0 {
			return 1
		}
		title := windowText(hwnd)
		if title == "" {
			return 1
		}
		hits = append(hits, visibleWindow{hwnd: hwnd, title: title})
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return hits
}

// pickBestVisible chooses the window whose title best answers hint.
// EnumWindows is topmost-first, and BestWindowTitle keeps that order on ties.
func pickBestVisible(hint string) (visibleWindow, bool) {
	hits := listVisibleWindows()
	if len(hits) == 0 {
		return visibleWindow{}, false
	}
	titles := make([]string, len(hits))
	for i, hit := range hits {
		titles[i] = hit.title
	}
	best, ok := computeruse.BestWindowTitle(hint, titles)
	if !ok {
		return visibleWindow{}, false
	}
	want := computeruse.NormalizeWindowTitle(best)
	for _, hit := range hits {
		if computeruse.NormalizeWindowTitle(hit.title) == want {
			return hit, true
		}
	}
	return visibleWindow{}, false
}

func focusWindow(titleSubstring string) error {
	titleSubstring = strings.TrimSpace(titleSubstring)
	if titleSubstring == "" {
		return fmt.Errorf("window title required")
	}
	picked, ok := pickBestVisible(titleSubstring)
	if !ok {
		return fmt.Errorf("no visible window matching %q", titleSubstring)
	}
	found := picked.hwnd

	// A visible window is activated with SW_SHOW and keeps its size, including
	// maximized. SW_RESTORE is only for a minimized window, and one that was
	// maximized comes back maximized.
	if foregroundIs(found) {
		return nil
	}
	iconic := windowIconic(found)
	if cmd, ok := focusShowCommand(iconic, iconic && windowRestoreToMaximized(found)); ok {
		procShowWindow.Call(found, cmd)
		if foregroundIs(found) {
			return nil
		}
	}
	// SetForegroundWindow can return success while the previous window stays
	// in front, so each attempt is checked against GetForegroundWindow.
	// Z-order uses HWND_TOP with SWP_NOMOVE|SWP_NOSIZE. A TOPMOST toggle can
	// leave the window always on top, and SWP_SHOWWINDOW restores a minimized
	// window to its normal rectangle.
	requestForeground(found, false)
	if waitUntilForeground(found) {
		return nil
	}
	requestForeground(found, true)
	if waitUntilForeground(found) {
		log.Printf("[computer-use] focus %q raised without moving", titleSubstring)
		return nil
	}
	return fmt.Errorf("window stayed in the background")
}

// focusShowCommand chooses a ShowWindow command that does not turn a
// maximized window into a restored rectangle. A visible window uses SW_SHOW,
// which activates it in its current size. A minimized window uses SW_MAXIMIZE
// when it was maximized, otherwise SW_RESTORE.
func focusShowCommand(iconic, restoreToMaximized bool) (uintptr, bool) {
	if !iconic {
		return swShow, true
	}
	if restoreToMaximized {
		return swMaximize, true
	}
	return swRestore, true
}

func focusRaiseFlags() uintptr {
	return swpNoMove | swpNoSize
}

// focusRaiseInsertAfter is HWND_TOP. HWND_TOPMOST then HWND_NOTOPMOST is not
// used: the second call can drop the window behind another one, and a failed
// second call leaves it always on top.
func focusRaiseInsertAfter() uintptr {
	return 0
}

func windowIconic(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	r, _, _ := procIsIconic.Call(hwnd)
	return r != 0
}

func windowRestoreToMaximized(hwnd uintptr) bool {
	var place focusPlacement
	place.Length = uint32(unsafe.Sizeof(place))
	r, _, _ := procGetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&place)))
	if r == 0 {
		return false
	}
	return place.Flags&wpfRestoreToMaximized != 0
}

func raiseWindowInPlace(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	procSetWindowPos.Call(hwnd, focusRaiseInsertAfter(), 0, 0, 0, 0, uintptr(focusRaiseFlags()))
}

type focusPlacement struct {
	Length         uint32
	Flags          uint32
	ShowCmd        uint32
	MinPosition    focusPoint
	MaxPosition    focusPoint
	NormalPosition focusRect
}

type focusPoint struct {
	X, Y int32
}

type focusRect struct {
	Left, Top, Right, Bottom int32
}

func foregroundIs(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	fg, _, _ := procGetForegroundWindow.Call()
	return fg == hwnd
}

func waitUntilForeground(hwnd uintptr) bool {
	if foregroundIs(hwnd) {
		return true
	}
	for i := 0; i < 3; i++ {
		time.Sleep(20 * time.Millisecond)
		if foregroundIs(hwnd) {
			return true
		}
	}
	return false
}

func requestForeground(found uintptr, tapAlt bool) {
	fg, _, _ := procGetForegroundWindow.Call()
	var fgTID, targetTID uintptr
	if fg != 0 && fg != found {
		fgTID, _, _ = procGetWindowThreadProcessId.Call(fg, 0)
	}
	targetTID, _, _ = procGetWindowThreadProcessId.Call(found, 0)
	curTID, _, _ := procGetCurrentThreadId.Call()
	if fgTID != 0 && fgTID != curTID {
		procAttachThreadInput.Call(curTID, fgTID, 1)
		defer procAttachThreadInput.Call(curTID, fgTID, 0)
	}
	if targetTID != 0 && targetTID != curTID && targetTID != fgTID {
		procAttachThreadInput.Call(curTID, targetTID, 1)
		defer procAttachThreadInput.Call(curTID, targetTID, 0)
	}
	procAllowSetForegroundWindow.Call(^uintptr(0))
	if tapAlt {
		// Finish the tap before activation. Holding Alt down across
		// SetForegroundWindow delivers it to the app, and the next Enter
		// hits the menu instead of the text box.
		focusAltTap()
	}
	// Z-order while the threads are attached. After detach, SetForegroundWindow is ignored.
	raiseWindowInPlace(found)
	procSetForegroundWindow.Call(found)
}

// focusAltTapEvents is keybd_event flags for one Alt press. The release is
// in the list so the key is up before the window is activated.
func focusAltTapEvents() []uintptr {
	return []uintptr{0, keyeventfKeyup}
}

func focusAltTap() {
	for _, flags := range focusAltTapEvents() {
		procKeybdEvent.Call(uintptr(vkMenu), 0, flags, 0)
	}
}

// foregroundWindowTitle returns the foreground window title ("" when none).
func foregroundWindowTitle() string {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return ""
	}
	return windowText(hwnd)
}

func foregroundWindowID() uint64 {
	hwnd, _, _ := procGetForegroundWindow.Call()
	return uint64(hwnd)
}

type winRect struct {
	Left, Top, Right, Bottom int32
}

func hwndWindowBounds(hwnd uintptr) (WindowBounds, bool) {
	if hwnd == 0 {
		return WindowBounds{}, false
	}
	var r winRect
	ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	if ok == 0 {
		return WindowBounds{}, false
	}
	w := int(r.Right - r.Left)
	h := int(r.Bottom - r.Top)
	if w < 64 || h < 64 {
		return WindowBounds{}, false
	}
	return WindowBounds{
		X:      int(r.Left),
		Y:      int(r.Top),
		Width:  w,
		Height: h,
		Title:  windowText(hwnd),
	}, true
}

func foregroundWindowBounds() (WindowBounds, bool) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	return hwndWindowBounds(hwnd)
}

func namedWindowBounds(titleSubstring string) (WindowBounds, bool) {
	titleSubstring = strings.TrimSpace(titleSubstring)
	if titleSubstring == "" {
		return WindowBounds{}, false
	}
	picked, ok := pickBestVisible(titleSubstring)
	if !ok {
		return WindowBounds{}, false
	}
	return hwndWindowBounds(picked.hwnd)
}

func namedWindowID(titleSubstring string) uint64 {
	titleSubstring = strings.TrimSpace(titleSubstring)
	if titleSubstring == "" {
		return 0
	}
	picked, ok := pickBestVisible(titleSubstring)
	if !ok {
		return 0
	}
	return uint64(picked.hwnd)
}

func windowOwnedBySelf(id uint64) bool {
	if id == 0 {
		return false
	}
	var pid uint32
	procGetWindowThreadProcessId.Call(uintptr(id), uintptr(unsafe.Pointer(&pid)))
	return pid != 0 && int(pid) == os.Getpid()
}

// windowTitleAtPoint resolves the top-level window owning screen point (x,y)
// and returns its title ("" when none).
func rootWindowAtPoint(x, y int) uintptr {
	// POINT by value: two int32 packed into one uintptr on x64.
	hwnd, _, _ := procWindowFromPoint.Call(uintptr(uint32(x)) | uintptr(uint32(y))<<32)
	if hwnd == 0 {
		return 0
	}
	if root, _, _ := procGetAncestor.Call(hwnd, 2 /*GA_ROOT*/); root != 0 {
		hwnd = root
	}
	return hwnd
}

func windowTitleAtPoint(x, y int) string {
	hwnd := rootWindowAtPoint(x, y)
	if hwnd == 0 {
		return ""
	}
	return windowText(hwnd)
}

func windowIDAtPoint(x, y int) uint64 {
	return uint64(rootWindowAtPoint(x, y))
}

func windowText(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(n+1))
	return syscall.UTF16ToString(buf)
}
