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
