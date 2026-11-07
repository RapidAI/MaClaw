//go:build windows

package windows

import (
	"unsafe"

	"github.com/wailsapp/wails/v2/internal/frontend/desktop/windows/winc/w32"
)

// captionDragBlocked reports that a title-bar move would leave the fullscreen
// frame. zoomed is WS_MAXIMIZE. Clamp's SetWindowPos can clear that bit while
// the outer rect still matches the monitor work area. GetWindowPlacement's
// showCmd follows the live zoom state, so it reads SW_SHOWNORMAL afterwards
// and is not a signal. A smaller rect may move.
func captionDragBlocked(fullscreen, zoomed, coversWork bool) bool {
	return fullscreen || zoomed || coversWork
}

func rectsNearPx(a, b w32.RECT) bool {
	const slack int32 = 2
	return absInt32(a.Left-b.Left) <= slack && absInt32(a.Top-b.Top) <= slack &&
		absInt32(a.Right-b.Right) <= slack && absInt32(a.Bottom-b.Bottom) <= slack
}

func absInt32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func (w *Window) captionDragBlocked() bool {
	if w == nil {
		return true
	}
	if w.IsFullScreen() || w.IsMaximised() {
		return true
	}
	hwnd := w.Handle()
	if hwnd == 0 {
		return false
	}
	rect := w32.GetWindowRect(hwnd)
	mon := w32.MonitorFromWindow(hwnd, w32.MONITOR_DEFAULTTONEAREST)
	var info w32.MONITORINFO
	info.CbSize = uint32(unsafe.Sizeof(info))
	if rect == nil || mon == 0 || !w32.GetMonitorInfo(mon, &info) {
		return false
	}
	if rect.Right <= rect.Left || rect.Bottom <= rect.Top {
		return false
	}
	return captionDragBlocked(false, false, rectsNearPx(*rect, info.RcWork))
}
