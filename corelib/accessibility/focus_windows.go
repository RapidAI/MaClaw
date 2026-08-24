//go:build windows

package accessibility

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

var (
	user32Enum                   = syscall.NewLazyDLL("user32.dll")
	procEnumWindows              = user32Enum.NewProc("EnumWindows")
	procGetWindowTextW           = user32Enum.NewProc("GetWindowTextW")
	procGetWindowTextLengthW     = user32Enum.NewProc("GetWindowTextLengthW")
	procIsWindowVisible          = user32Enum.NewProc("IsWindowVisible")
	procSetForegroundWindow      = user32Enum.NewProc("SetForegroundWindow")
	procShowWindow               = user32Enum.NewProc("ShowWindow")
	procGetForegroundWindow      = user32Enum.NewProc("GetForegroundWindow")
	procGetWindowRect            = user32Enum.NewProc("GetWindowRect")
	procWindowFromPoint          = user32Enum.NewProc("WindowFromPoint")
	procGetAncestor              = user32Enum.NewProc("GetAncestor")
	procAttachThreadInput        = user32Enum.NewProc("AttachThreadInput")
	procGetWindowThreadProcessId = user32Enum.NewProc("GetWindowThreadProcessId")
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procGetCurrentThreadId       = kernel32.NewProc("GetCurrentThreadId")
)

const swRestore = 9

func focusWindow(titleSubstring string) error {
	titleSubstring = strings.TrimSpace(titleSubstring)
	if titleSubstring == "" {
		return fmt.Errorf("window title required")
	}
	want := strings.ToLower(titleSubstring)
	var found uintptr

	cb := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		vis, _, _ := procIsWindowVisible.Call(hwnd)
		if vis == 0 {
			return 1
		}
		n, _, _ := procGetWindowTextLengthW.Call(hwnd)
		if n == 0 {
			return 1
		}
		buf := make([]uint16, n+1)
		procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(n+1))
		title := strings.ToLower(syscall.UTF16ToString(buf))
		if strings.Contains(title, want) {
			found = hwnd
			return 0 // stop
		}
		return 1
	})

	procEnumWindows.Call(cb, 0)
	if found == 0 {
		return fmt.Errorf("no visible window matching %q", titleSubstring)
	}

	// Restore if minimized, then force foreground (AttachThreadInput trick).
	procShowWindow.Call(found, swRestore)

	fg, _, _ := procGetForegroundWindow.Call()
	if fg == found {
		return nil
	}
	var fgTID, targetTID uintptr
	if fg != 0 {
		fgTID, _, _ = procGetWindowThreadProcessId.Call(fg, 0)
	}
	targetTID, _, _ = procGetWindowThreadProcessId.Call(found, 0)
	curTID, _, _ := procGetCurrentThreadId.Call()

	if fgTID != 0 && fgTID != curTID {
		procAttachThreadInput.Call(curTID, fgTID, 1)
	}
	if targetTID != 0 && targetTID != curTID {
		procAttachThreadInput.Call(curTID, targetTID, 1)
	}
	r, _, err := procSetForegroundWindow.Call(found)
	if fgTID != 0 && fgTID != curTID {
		procAttachThreadInput.Call(curTID, fgTID, 0)
	}
	if targetTID != 0 && targetTID != curTID {
		procAttachThreadInput.Call(curTID, targetTID, 0)
	}
	if r == 0 {
		return fmt.Errorf("SetForegroundWindow: %v", err)
	}
	return nil
}

// foregroundWindowTitle returns the foreground window title ("" when none).
func foregroundWindowTitle() string {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return ""
	}
	return windowText(hwnd)
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
	want := strings.ToLower(titleSubstring)
	var found WindowBounds
	var okFound bool
	cb := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		vis, _, _ := procIsWindowVisible.Call(hwnd)
		if vis == 0 {
			return 1
		}
		title := strings.ToLower(windowText(hwnd))
		if title == "" || !strings.Contains(title, want) {
			return 1
		}
		if b, ok := hwndWindowBounds(hwnd); ok {
			found = b
			okFound = true
			return 0
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return found, okFound
}

// windowTitleAtPoint resolves the top-level window owning screen point (x,y)
// and returns its title ("" when none).
func windowTitleAtPoint(x, y int) string {
	// POINT by value: two int32 packed into one uintptr on x64.
	hwnd, _, _ := procWindowFromPoint.Call(uintptr(uint32(x)) | uintptr(uint32(y))<<32)
	if hwnd == 0 {
		return ""
	}
	if root, _, _ := procGetAncestor.Call(hwnd, 2 /*GA_ROOT*/); root != 0 {
		hwnd = root
	}
	return windowText(hwnd)
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
