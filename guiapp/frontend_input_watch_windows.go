//go:build windows

package guiapp

import (
	"time"
	"unsafe"
)

var (
	procGetLastInputInfoIW = recoverUser32.NewProc("GetLastInputInfo")
	procGetForegroundWndIW = recoverUser32.NewProc("GetForegroundWindow")
	procSetForegroundWndIW = recoverUser32.NewProc("SetForegroundWindow")
	procGetTickCountIKW    = recoverKernel32.NewProc("GetTickCount")
)

func frontendInputWatchSupported() bool { return true }

type lastInputInfoW struct {
	cbSize uint32
	dwTime uint32
}

// systemInputIdleDuration reports how long since the last system-wide input
// event (mouse/keyboard, any application) via GetLastInputInfo. The uint32
// subtraction wraps correctly across the ~49.7-day GetTickCount rollover.
func systemInputIdleDuration() time.Duration {
	var li lastInputInfoW
	li.cbSize = uint32(unsafe.Sizeof(li))
	if r, _, _ := procGetLastInputInfoIW.Call(uintptr(unsafe.Pointer(&li))); r == 0 {
		return 0
	}
	now, _, _ := procGetTickCountIKW.Call()
	idleMs := uint32(now) - li.dwTime
	return time.Duration(idleMs) * time.Millisecond
}

func isMaclawWindowForeground() bool {
	hwnd, _, _ := procGetForegroundWndIW.Call()
	if hwnd == 0 {
		return false
	}
	return hwnd == findWailsWindowHWND()
}

// nudgeMainWindowInput performs the minimize -> restore -> foreground cycle
// that empirically revives a dead WebView2 input pipeline (verified 2026-10-06).
func nudgeMainWindowInput() {
	hwnd := findWailsWindowHWND()
	if hwnd == 0 {
		return
	}
	procShowWindowRW.Call(hwnd, 6) // SW_MINIMIZE
	time.Sleep(600 * time.Millisecond)
	procShowWindowRW.Call(hwnd, 9) // SW_RESTORE
	time.Sleep(200 * time.Millisecond)
	procSetForegroundWndIW.Call(hwnd)
	bootLog("input watchdog nudge done hwnd=0x%X", hwnd)
}
