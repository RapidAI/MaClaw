//go:build !windows

package main

import "time"

// ClampMaximizedWindowToWorkArea is a no-op outside Windows.
// Win10 frameless overflow under the taskbar is a Windows DWM issue.
func (a *App) ClampMaximizedWindowToWorkArea() {}

func (a *App) scheduleClampMaximizedWindowToWorkArea(delay time.Duration) {}
