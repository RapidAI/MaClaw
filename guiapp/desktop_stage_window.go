package guiapp

// Desktop-stage pop-out window: the wails3-native replacement for the
// in-panel h264 iframe (the cross-origin OOPIF that produced the
// 2026-11-24 renderer OOM). The embedded panel stays the default on a normal
// local session; composition-hosted RDP sessions prefer this native window
// because a cross-origin OOPIF is not a reliable presentation surface there.
// The standalone h264.html page drives the same /h264 stream,
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
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

var (
	// desktopStageOpenMu serializes open/close operations themselves. Without
	// it two overlapping frontend probes can both pass the nil-window check and
	// create two native windows, the second overwriting the first in state.
	desktopStageOpenMu sync.Mutex
	desktopStageMu     sync.Mutex
	desktopStageWindow *application.WebviewWindow
	desktopStageURL    string
)

// runningWails3App 由 Main 在 application.New 后安装，供弹出窗口创建使用。
var runningWails3App *application.App

// desktopStageLastBounds remembers the pop-out window's last on-screen
// geometry (DIP, the unit used by Size and window options) so a reopen
// restores the user's resized/placed window instead of snapping back to the
// 1280x800 default. Session-scoped
// by design: a fresh process starts from the default again.
var desktopStageLastW, desktopStageLastH int

// DesktopStageWindowSupported reports whether the running build can open the
// native pop-out stage window. The frontend probe calls this before showing
// the pop-out control; under wails/v2 builds the method is absent and the
// probe fails closed.
func (a *App) DesktopStageWindowSupported() bool {
	return runningWails3App != nil
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

func desktopStagePhysicalWidth(dipWidth int, screen *application.Screen) int {
	scale := float64(1)
	if screen != nil && screen.ScaleFactor > 0 {
		scale = float64(screen.ScaleFactor)
	}
	return int(float64(dipWidth)*scale + 0.5)
}

// DesktopStageWindowWidth returns the width the pop-out will use in physical
// pixels. WebviewWindow.Size is DIP on Windows, so using it directly would
// still upscale on a scaled display; the scale must come from the screen the
// stage window itself is on, not from the main window's devicePixelRatio.
func (a *App) DesktopStageWindowWidth() int {
	desktopStageMu.Lock()
	win := desktopStageWindow
	last := desktopStageLastW
	desktopStageMu.Unlock()
	if win != nil {
		if w, _ := win.Size(); w > 0 {
			screen, _ := win.GetScreen()
			if screen == nil && runningWails3App != nil && runningWails3App.Screen != nil {
				screen = runningWails3App.Screen.GetPrimary()
			}
			return desktopStagePhysicalWidth(w, screen)
		}
	}
	dip := last
	if dip <= 0 {
		dip = 1280
	}
	var screen *application.Screen
	if runningWails3App != nil && runningWails3App.Screen != nil {
		screen = runningWails3App.Screen.GetPrimary()
	}
	return desktopStagePhysicalWidth(dip, screen)
}

// DesktopStageWindowVisible reports whether the pop-out itself is still a
// visible, unminimised desktop surface. The main WebView can be hidden while
// this window remains visible; that must not be treated as an ownerless stream.
func (a *App) DesktopStageWindowVisible() bool {
	desktopStageMu.Lock()
	win := desktopStageWindow
	desktopStageMu.Unlock()
	if win == nil {
		return false
	}
	return win.IsVisible() && !win.IsMinimised()
}

func desktopStageTitle() string {
	title := "MaClaw 桌面画面"
	// Review binaries often coexist with an older review process under the
	// same single-instance lock. A visible version marker is the only quick
	// way to tell which binary actually owns the desktop window; formal
	// releases keep the stable product title.
	if strings.HasPrefix(version, "review-") {
		return title + " [" + version + "]"
	}
	return title
}

func desktopStageValidURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return fmt.Errorf("invalid desktop stage url")
	}
	if !strings.Contains(u.Path, "/desktop-handoff/") || (!strings.HasSuffix(u.Path, "/h264.html") && !strings.HasSuffix(u.Path, "/vnc.html")) {
		return fmt.Errorf("invalid desktop stage player url")
	}
	return nil
}

// desktopStageLogURL keeps enough of the player URL to identify the viewer
// mode in logs without persisting the handoff capability embedded in its
// path. Query strings are dropped entirely: handoff implementations have
// carried credentials there as well as in path segments.
func desktopStageLogURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Scheme == "" || u.Host == "" {
		return "<invalid>"
	}
	path := u.Path
	const handoff = "/desktop-handoff/"
	if idx := strings.Index(path, handoff); idx >= 0 {
		rest := strings.TrimPrefix(path[idx+len(handoff):], "/")
		parts := strings.Split(rest, "/")
		if len(parts) > 1 && parts[0] != "" {
			parts[0] = "<handoff>"
			path = path[:idx] + handoff + strings.Join(parts, "/")
		}
	}
	return u.Scheme + "://" + u.Host + path
}

// OpenDesktopStageWindow opens the standalone desktop-stage window streaming
// the player's full remote URL, or navigates the existing window to it.
// Passing the complete URL is essential: the player lives on the hub handoff path and carries the
// handoff token, viewer mode, width and fps in its query. Loading a local
// "/h264.html" would show the app's missing-asset page, not the desktop.
func (a *App) OpenDesktopStageWindow(rawURL string) error {
	if runningWails3App == nil {
		return fmt.Errorf("wails3 application is not initialized")
	}
	if err := desktopStageValidURL(rawURL); err != nil {
		return err
	}
	desktopStageOpenMu.Lock()
	defer desktopStageOpenMu.Unlock()
	desktopStageMu.Lock()
	if desktopStageWindow != nil {
		win := desktopStageWindow
		navigate := desktopStageURL != rawURL
		if navigate {
			desktopStageURL = rawURL
		}
		desktopStageMu.Unlock()
		if navigate {
			win.SetURL(rawURL)
			log.Printf("[desktop-stage] pop-out window navigated url=%s", desktopStageLogURL(rawURL))
		}
		// Do not Show/Focus an existing window on every player-URL refresh:
		// stream-width renegotiation and watch/live generation changes are
		// background maintenance, and on Windows Focus also unminimizes and
		// steals foreground. Creation below is the only focus-taking open.
		return nil
	}
	w, h := 1280, 800
	if desktopStageLastW > 0 && desktopStageLastH > 0 {
		w, h = desktopStageLastW, desktopStageLastH
	}
	lastW, lastH := desktopStageLastW, desktopStageLastH
	desktopStageMu.Unlock()
	win := runningWails3App.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            desktopStageTitle(),
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
	win.OnWindowEvent(events.Common.WindowDidResize, func(event *application.WindowEvent) {
		sw, sh := win.Size()
		desktopStageMu.Lock()
		if sw > 200 && sh > 200 {
			desktopStageLastW, desktopStageLastH = sw, sh
		}
		desktopStageMu.Unlock()
	})
	win.OnWindowEvent(events.Common.WindowClosing, func(event *application.WindowEvent) {
		desktopStageMu.Lock()
		manual := desktopStageWindow == win
		if manual {
			desktopStageWindow = nil
			desktopStageURL = ""
		}
		desktopStageMu.Unlock()
		if manual {
			log.Printf("[desktop-stage] pop-out window closed by user")
			a.emitEvent(EventDesktopStageClosed)
		}
	})
	win.Show()
	win.Focus()
	log.Printf("[desktop-stage] pop-out window opened url=%s size=%dx%d (last=%dx%d)", desktopStageLogURL(rawURL), w, h, lastW, lastH)
	return nil
}

// CloseDesktopStageWindow closes the pop-out window if open. The panel
// remounts its iframe afterwards.
func (a *App) CloseDesktopStageWindow() {
	desktopStageOpenMu.Lock()
	defer desktopStageOpenMu.Unlock()
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
