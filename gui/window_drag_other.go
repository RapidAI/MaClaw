//go:build !windows

package main

// BeginWindowDrag is a no-op off Windows: macOS/Linux keep using Wails'
// built-in --wails-draggable handling, which performs the drag natively and
// synchronously on mousedown and is not affected by the Windows race guarded
// against in window_drag_windows.go.
func (a *App) BeginWindowDrag() {}

// cssDragPropertyOverride keeps Wails' default --wails-draggable property on
// non-Windows platforms.
func cssDragPropertyOverride() string {
	return ""
}
