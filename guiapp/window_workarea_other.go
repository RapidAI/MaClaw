//go:build !windows

package guiapp

import (
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ClampMaximizedWindowToWorkArea is a no-op outside Windows.
// Win10 frameless overflow under the taskbar is a Windows DWM issue.
func (a *App) ClampMaximizedWindowToWorkArea() {}

func (a *App) scheduleClampMaximizedWindowToWorkArea(delay time.Duration) {}

// SetMainWindowScreenPosition places the window at a screen point. Non-Windows
// Wails runtimes already take screen coordinates for WindowSetPosition.
func (a *App) SetMainWindowScreenPosition(x, y int) {
	if a == nil || a.ctx == nil {
		return
	}
	wailsruntime.WindowSetPosition(a.ctx, x, y)
}
