//go:build wails3

package guiapp

// Desktop-stage pop-out window: the wails3-native replacement for the
// in-panel h264 iframe (the cross-origin OOPIF that produced the
// 2026-11-24 renderer OOM). Additive by design — the embedded panel stays
// the default; this window is only opened when the frontend asks for the
// pop-out. The standalone h264.html page drives the same /h264 stream,
// takeover heartbeats and viewer_takeover gate as the iframe, and shares
// the WebView2 profile so cookies/ Takeover state carry over (spike C6).
//
// v2 builds: this file is excluded by the build tag, so the bindings do not
// exist and the frontend probe (DesktopStageWindowSupported) reports
// unsupported — the panel keeps its iframe.

import (
	"fmt"
	"log"

	wruntime "github.com/RapidAI/CodeClaw/guiapp/wruntime"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

var desktopStageWindow *application.WebviewWindow

// DesktopStageWindowSupported reports whether the running build can open the
// native pop-out stage window. The frontend probe calls this before showing
// the pop-out control; under wails/v2 builds the method is absent and the
// probe fails closed.
func (a *App) DesktopStageWindowSupported() bool {
	return wails3Enabled
}

// OpenDesktopStageWindow opens (or focuses) the standalone desktop-stage
// window streaming h264.html with the given encoder width and fps. width is
// clamped by h264.html's own bounds handling; fps 0 keeps the server
// default.
func (a *App) OpenDesktopStageWindow(width int, fps int) {
	if desktopStageWindow != nil {
		desktopStageWindow.Show()
		desktopStageWindow.Focus()
		return
	}
	url := "/h264.html"
	if width > 0 || fps > 0 {
		url = fmt.Sprintf("/h264.html?w=%d&fps=%d", width, fps)
	}
	win := wruntime.Application().Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "MaClaw 桌面画面",
		Name:             "desktop-stage",
		Width:            1280,
		Height:           800,
		Frameless:        false,
		BackgroundColour: application.RGBA{Red: 12, Green: 12, Blue: 12, Alpha: 255},
		URL:              url,
	})
	if win == nil {
		log.Printf("[desktop-stage] window creation returned nil")
		return
	}
	desktopStageWindow = win
	win.OnWindowEvent(events.Common.WindowClosing, func(event *application.WindowEvent) {
		desktopStageWindow = nil
	})
	win.Show()
	log.Printf("[desktop-stage] pop-out window opened url=%s", url)
}

// CloseDesktopStageWindow closes the pop-out window if open. The panel
// remounts its iframe afterwards.
func (a *App) CloseDesktopStageWindow() {
	if desktopStageWindow == nil {
		return
	}
	win := desktopStageWindow
	desktopStageWindow = nil
	win.Close()
	log.Printf("[desktop-stage] pop-out window closed")
}
