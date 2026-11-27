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
	"net/url"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

var (
	desktopStageMu     sync.Mutex
	desktopStageWindow *application.WebviewWindow
	desktopStageURL    string
)

// runningWails3App 由 Main 在 application.New 后安装，供弹出窗口创建使用。
var runningWails3App *application.App

// desktopStageLastBounds remembers the pop-out window's last on-screen
// geometry (physical px) so a reopen restores the user's resized/placed
// window instead of snapping back to the 1280x800 default. Session-scoped
// by design: a fresh process starts from the default again.
var desktopStageLastW, desktopStageLastH int

// DesktopStageWindowSupported reports whether the running build can open the
// native pop-out stage window. The frontend probe calls this before showing
// the pop-out control; under wails/v2 builds the method is absent and the
// probe fails closed.
func (a *App) DesktopStageWindowSupported() bool {
	return true
}

// DesktopStageWindowPreferred reports whether this host should prefer the
// native window over an embedded cross-origin iframe. Composition hosting is
// required for the main WebView to present over RDP, but a cross-origin OOPIF
// inside that composited WebView is not a reliable presentation surface; the
// same stream then arrives and decodes while the iframe stays black. A
// top-level composition-hosted WebView avoids that renderer boundary.
func (a *App) DesktopStageWindowPreferred() bool {
	return perfSessionIsRemote()
}

// DesktopStageWindowOpen reports whether the pop-out window still exists.
func (a *App) DesktopStageWindowOpen() bool {
	desktopStageMu.Lock()
	defer desktopStageMu.Unlock()
	return desktopStageWindow != nil
}

func desktopStageValidURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return fmt.Errorf("invalid desktop stage url")
	}
	return nil
}

// OpenDesktopStageWindow opens (or focuses) the standalone desktop-stage
// window streaming the player's full remote URL. Passing the complete URL is
// essential: the player lives on the hub handoff path and carries the
// handoff token, viewer mode, width and fps in its query. Loading a local
// "/h264.html" would show the app's missing-asset page, not the desktop.
func (a *App) OpenDesktopStageWindow(rawURL string) error {
	if err := desktopStageValidURL(rawURL); err != nil {
		return err
	}
	desktopStageMu.Lock()
	if desktopStageWindow != nil {
		win := desktopStageWindow
		if desktopStageURL != rawURL {
			desktopStageURL = rawURL
			win.SetURL(rawURL)
		}
		desktopStageMu.Unlock()
		win.Show()
		win.Focus()
		return nil
	}
	desktopStageMu.Unlock()
	w, h := 1280, 800
	if desktopStageLastW > 0 && desktopStageLastH > 0 {
		w, h = desktopStageLastW, desktopStageLastH
	}
	win := runningWails3App.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "MaClaw 桌面画面",
		Name:             "desktop-stage",
		Width:            w,
		Height:           h,
		Frameless:        false,
		BackgroundColour: application.RGBA{Red: 12, Green: 12, Blue: 12, Alpha: 255},
		URL:              rawURL,
		// 远控观看用户恰好在 RDP 会话里：窗口托管在 RDP 下不呈现
		// （与主窗口同一白屏根因），组合托管是官方修复路径。
		Windows: application.WindowsWindow{
			WebView2CompositionHosting: useVisualHostingWails3(),
		},
	})
	if win == nil {
		log.Printf("[desktop-stage] window creation returned nil")
		return fmt.Errorf("desktop stage window creation failed")
	}
	desktopStageMu.Lock()
	desktopStageWindow = win
	desktopStageURL = rawURL
	desktopStageMu.Unlock()
	win.OnWindowEvent(events.Common.WindowClosing, func(event *application.WindowEvent) {
		// 记录关闭前的尺寸，下次弹出时恢复用户调整过的几何。
		if sw, sh := win.Size(); sw > 200 && sh > 200 {
			desktopStageLastW, desktopStageLastH = sw, sh
		}
		desktopStageMu.Lock()
		if desktopStageWindow == win {
			desktopStageWindow = nil
			desktopStageURL = ""
		}
		desktopStageMu.Unlock()
	})
	win.Show()
	log.Printf("[desktop-stage] pop-out window opened url=%s size=%dx%d (last=%dx%d)", rawURL, w, h, desktopStageLastW, desktopStageLastH)
	return nil
}

// CloseDesktopStageWindow closes the pop-out window if open. The panel
// remounts its iframe afterwards.
func (a *App) CloseDesktopStageWindow() {
	desktopStageMu.Lock()
	if desktopStageWindow == nil {
		desktopStageMu.Unlock()
		return
	}
	win := desktopStageWindow
	desktopStageWindow = nil
	desktopStageURL = ""
	desktopStageMu.Unlock()
	win.Close()
	log.Printf("[desktop-stage] pop-out window closed")
}
