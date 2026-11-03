//go:build windows

package windows

import "syscall"

// A workstation lock switches to the Winlogon desktop without deactivating
// the app window, so WM_ACTIVATE/WM_SETFOCUS do not fire on unlock. The WTS
// notification is the only reliable unlock signal. It is used to restore a
// cursor display count that WebView2 left below zero; see
// restoreCursorDisplayCount.
const (
	wmWtsSessionChange   = 0x02B1
	wtsSessionUnlock     = 0x8
	notifyForThisSession = 0
)

var (
	wtsapi32                             = syscall.NewLazyDLL("wtsapi32.dll")
	procWTSRegisterSessionNotification   = wtsapi32.NewProc("WTSRegisterSessionNotification")
	procWTSUnRegisterSessionNotification = wtsapi32.NewProc("WTSUnRegisterSessionNotification")
)

// registerSessionNotification asks the WTS service to post WM_WTSSESSION_CHANGE
// to hwnd. A missing wtsapi32 or a refused registration is not fatal: the
// cursor still recovers on the next activation or focus message.
func registerSessionNotification(hwnd uintptr) {
	if hwnd == 0 || procWTSRegisterSessionNotification.Find() != nil {
		return
	}
	procWTSRegisterSessionNotification.Call(hwnd, notifyForThisSession)
}

func unregisterSessionNotification(hwnd uintptr) {
	if hwnd == 0 || procWTSUnRegisterSessionNotification.Find() != nil {
		return
	}
	procWTSUnRegisterSessionNotification.Call(hwnd)
}
