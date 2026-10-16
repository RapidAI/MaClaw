//go:build windows

package guiapp

import (
	"sync"
	"syscall"
	"unsafe"
)

var (
	procAllowSetForegroundWindow = recoverUser32.NewProc("AllowSetForegroundWindow")
	procShowWindowAsyncReveal    = recoverUser32.NewProc("ShowWindowAsync")

	companionEnumOnce  sync.Once
	companionEnumCB    uintptr
	companionEnumGate  sync.Mutex
	companionEnumMu    sync.Mutex
	companionEnumPID   uintptr
	companionEnumFound uintptr
)

func allowCompanionForeground() {
	// ASFW_ANY. The button click is in the foreground process.
	procAllowSetForegroundWindow.Call(^uintptr(0))
}

// revealHiddenCompanionWindow shows this process's companion window and brings
// it forward. It returns false when the window handle is not available yet.
// The show and z-order calls are posted. The single-instance procedure may
// still be on the UI thread, and a sent message would deadlock that thread.
func revealHiddenCompanionWindow() bool {
	hwnd := companionWindowHWND()
	if hwnd == 0 {
		return false
	}
	iconic, _, _ := procIsIconicWA.Call(hwnd)
	visible, _, _ := procIsWindowVisibleRW.Call(hwnd)
	fg, _, _ := procGetForegroundWndIW.Call()
	if iconic == 0 && visible != 0 && fg == hwnd {
		return true
	}
	procShowWindowAsyncReveal.Call(hwnd, uintptr(companionShowCommand(iconic != 0)))
	flags := companionRaiseFlags()
	topmost := ^uintptr(0)    // HWND_TOPMOST
	notTopmost := ^uintptr(1) // HWND_NOTOPMOST
	procSetWindowPosRW.Call(hwnd, topmost, 0, 0, 0, 0, flags)
	procSetWindowPosRW.Call(hwnd, notTopmost, 0, 0, 0, 0, flags)
	return true
}

// companionWindowHWND finds the Wails window without GetWindowText.
// GetWindowText sends to the UI thread and deadlocks a single-instance callback.
func companionWindowHWND() uintptr {
	companionEnumOnce.Do(func() {
		companionEnumCB = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
			companionEnumMu.Lock()
			pid := companionEnumPID
			companionEnumMu.Unlock()
			var wpid uint32
			procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&wpid)))
			if uintptr(wpid) != pid {
				return 1
			}
			if recoverClassName(hwnd) != "wailsWindow" {
				return 1
			}
			companionEnumMu.Lock()
			companionEnumFound = hwnd
			companionEnumMu.Unlock()
			return 0
		})
	})

	companionEnumGate.Lock()
	defer companionEnumGate.Unlock()
	pid, _, _ := procGetCurrentProcess.Call()
	companionEnumMu.Lock()
	companionEnumPID = pid
	companionEnumFound = 0
	companionEnumMu.Unlock()
	procEnumWindowsRW.Call(companionEnumCB, 0)
	companionEnumMu.Lock()
	hwnd := companionEnumFound
	companionEnumMu.Unlock()
	return hwnd
}
