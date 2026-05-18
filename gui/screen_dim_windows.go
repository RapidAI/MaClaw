//go:build windows

package main

import (
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	screenDimUser32      = syscall.NewLazyDLL("user32.dll")
	procGetLastInputInfo = screenDimUser32.NewProc("GetLastInputInfo")
	procSendMessageW     = screenDimUser32.NewProc("SendMessageW")
)

func init() {
	platformGetIdleDuration = getIdleDurationWindows
	platformDimDisplay = dimDisplayWindows
}

// LASTINPUTINFO structure for GetLastInputInfo.
type lastInputInfo struct {
	CbSize uint32
	DwTime uint32
}

func getIdleDurationWindows() time.Duration {
	var lii lastInputInfo
	lii.CbSize = uint32(unsafe.Sizeof(lii))
	r, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&lii)))
	if r == 0 {
		return 0
	}
	uptimeMs := uint64(windows.DurationSinceBoot() / time.Millisecond)
	idleMs := idleMillisecondsFromWindowsTicks(uptimeMs, lii.DwTime)
	return time.Duration(idleMs) * time.Millisecond
}

func idleMillisecondsFromWindowsTicks(now64 uint64, lastInput32 uint32) uint32 {
	// GetLastInputInfo returns a 32-bit tick value compatible with GetTickCount,
	// so it wraps roughly every 49.7 days. Compare it against the low 32 bits of
	// GetTickCount64 and let unsigned subtraction handle the wrap. Subtracting the
	// 32-bit value from the full 64-bit uptime makes long-running PCs look idle
	// forever, which turns the display off even while the user is typing.
	return uint32(now64) - lastInput32
}

func dimDisplayWindows() {
	const (
		hwndBroadcast  = 0xFFFF
		wmSyscommand   = 0x0112
		scMonitorpower = 0xF170
		monitorOff     = 2
	)
	procSendMessageW.Call(
		uintptr(hwndBroadcast),
		uintptr(wmSyscommand),
		uintptr(scMonitorpower),
		uintptr(monitorOff),
	)
}
