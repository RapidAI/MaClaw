//go:build windows

package main

import (
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Window dragging on Windows is handled by the frontend via data-window-drag
// regions calling BeginWindowDrag, NOT via Wails' built-in --wails-draggable
// CSS handling (disabled through CSSDragProperty override in main.go).
//
// Why: Wails' built-in drag posts WM_NCLBUTTONDOWN/HTCAPTION unconditionally
// when the async "drag" invoke arrives from the WebView. If the user pressed
// the mouse on a drag region and released it quickly (e.g. accidentally
// starting a text selection on the title bar), the invoke can be processed
// AFTER the button was released. Windows then enters the modal move loop with
// no button held and never receives the exit condition: the UI keeps
// rendering (the loop still pumps WM_PAINT) but every mouse message is
// swallowed — the window looks normal yet cannot be clicked or dragged until
// the process is killed.
//
// The guard below closes that race: we re-check the physical left-button
// state at the moment we would post the message, and refuse to enter the
// move loop when the button is already up.

const (
	vkLeftButton    = 0x01
	wmNclButtonDown = 0x00A1
	htCaptionHit    = 2
)

var procGetAsyncKeyState = floatUser32.NewProc("GetAsyncKeyState")

// BeginWindowDrag starts a native window move for the main window. Called by
// the frontend after the pointer has actually travelled past a small
// threshold on a data-window-drag region.
func (a *App) BeginWindowDrag() {
	if a.ctx == nil || runtime.WindowIsFullscreen(a.ctx) {
		return
	}
	// Key race guard: only enter the native modal move loop while the left
	// button is still physically held.
	state, _, _ := procGetAsyncKeyState.Call(vkLeftButton)
	if state&0x8000 == 0 {
		return
	}
	// findMainWindowHWND (window_workarea_windows.go) resolves and caches the
	// main window handle, filtered by this process's PID.
	hwnd := findMainWindowHWND()
	if hwnd == 0 {
		return
	}
	procReleaseCapture.Call()
	procPostMessageW.Call(hwnd, wmNclButtonDown, htCaptionHit, 0)
}

// cssDragPropertyOverride disables Wails' built-in CSS drag handling on
// Windows by pointing it at a property no element ever sets; dragging is
// driven by BeginWindowDrag instead. Returning "" keeps the default
// --wails-draggable behaviour (used on other platforms).
func cssDragPropertyOverride() string {
	return "--maclaw-drag-disabled"
}
