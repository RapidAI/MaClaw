// Command maclaw-gui-v3 is the Wails v3 migration spike (Phase 0 of the
// v2→v3 port plan). It builds a minimal frameless app exercising the
// high-risk checkpoints and writes one "[spike] Cn ..." line per checkpoint
// to stdout and to ~/.maclaw-spike/result.log, so results survive GUI
// process detach.
//
// Run:
//
//	dist/MaClawV3Spike.exe           first instance; opens the spike window
//	dist/MaClawV3Spike.exe --quit    second instance; relays quit to the first
//	dist/MaClawV3Spike.exe --dialog  first instance; also prompts a file dialog
//
// Checkpoints:
//
//	C0 watchdog for the rapid-relaunch WebView2 init flake (WARN + tray fallback)
//	C1 frameless window + HWND access + guarded drag (guiapp 同款逻辑)
//	C2 DWM frameless inset measurement + corner preference + OS build
//	C3 single instance lock + second instance --quit relay
//	C4 asset server middleware (POST route + no-store header)
//	C5 ExecJS timing + page URL scheme (replaces v2 wails.localhost)
//	C6 second WebviewWindow + WebView2 profile cookie sharing
//	C7 system tray with menu (+ optional file dialog via --dialog)
//	C8 Go→JS→Go event round trip (emitEvent 通道的 v3 等价物)
//
// Platform builds (Phase 5 note): wails/v3 desktop targets require
// CGO_ENABLED=1 — plain GOOS=darwin/linux cross-compiles fail in
// internal/buildguard by design. Verified escape hatch: `-tags purego`
// cross-compiles to darwin/arm64 and linux/amd64, but the v3 purego
// backends carry caveats (see v3 PUREGO_LINUX.md / PUREGO_MACOS.md);
// production mac/linux builds should prefer a native CGO toolchain.
package main

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed assets
var spikeAssetsFS embed.FS

var spikeResultFile io.Writer

func setupSpikeResultLog() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".maclaw-spike")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "result.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	spikeResultFile = f
}

func spikeLogf(format string, args ...any) {
	line := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
	fmt.Println(line)
	if spikeResultFile != nil {
		_, _ = fmt.Fprintln(spikeResultFile, line)
	}
}

// SpikeService is the bound service; also hosts the spike orchestration.
type SpikeService struct {
	app             *application.App
	win             *application.WebviewWindow
	win2            *application.WebviewWindow
	dialogRequested bool
	trayOnce        sync.Once
	stopped         atomic.Bool // set once app.Run() has returned
}

// stillRunning guards the delayed callbacks: a manual window close or an
// early --quit relay must not leave the 25s tray fallback (or a late
// RuntimeReady goroutine) calling into a stopped application.
func (s *SpikeService) stillRunning() bool { return !s.stopped.Load() }

// GuardedDragBegin replicates guiapp's guarded frameless drag
// (guiapp/window_drag_windows.go BeginWindowDrag) against the v3 window:
// confirm the left button is still down, release the capture, and post
// WM_NCLBUTTONDOWN/HTCAPTION to the window handle.
func (s *SpikeService) GuardedDragBegin() string {
	if s.win == nil {
		return "no-window"
	}
	h := uintptr(s.win.NativeWindow())
	if h == 0 {
		return "no-hwnd"
	}
	return spikeGuardedDragBegin(h)
}

func (s *SpikeService) onWin1RuntimeReady() {
	if !s.stillRunning() {
		return
	}
	h := uintptr(s.win.NativeWindow())
	spikeLogf("C1 native window handle=0x%x nonzero=%v; guarded drag exposed as service method (manual feel check: top bar drag / button)", h, h != 0)
	for _, v := range spikeWinChecks(h) {
		spikeLogf("C2 %s", v)
	}
	// C5: probe URL scheme + title from inside the page (retries until the
	// runtime module attaches window.Wails).
	s.win.ExecJS(`(function(){var t=0;(function f(){if(window.Wails&&window.Wails.Events){window.Wails.Events.Emit('spike:probe',{href:location.href,title:document.title,from:'execjs'});}else if(t++<25){setTimeout(f,200);}})();})();`)
	// C8: Go→JS ping; page replies spike:pong with the payload echoed.
	s.app.Event.Emit("spike:ping", map[string]any{"n": 42, "msg": "hello-v3"})
	time.AfterFunc(1500*time.Millisecond, s.createSecondWindow)
}

func (s *SpikeService) createSecondWindow() {
	win2 := s.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Spike 2",
		Name:   "spike-second",
		Width:  360,
		Height: 240,
		URL:    "/second.html",
	})
	if win2 == nil {
		spikeLogf("C6 FAIL second window creation returned nil")
		time.AfterFunc(1*time.Second, s.createTray)
		return
	}
	s.win2 = win2
	win2.OnWindowEvent(events.Common.WindowRuntimeReady, func(event *application.WindowEvent) {
		spikeLogf("C6 second window runtime ready")
		win2.ExecJS(`document.cookie='spike2=1; path=/'; window.Wails&&Wails.Events.Emit('spike:cookie-set', document.cookie);`)
		time.AfterFunc(1*time.Second, func() {
			// Read the same cookie from window 1: shared profile → visible.
			s.win.ExecJS(`window.Wails&&Wails.Events.Emit('spike:win1-cookie', document.cookie);`)
		})
	})
	time.AfterFunc(4*time.Second, s.createTray)
}

func (s *SpikeService) createTray() {
	s.trayOnce.Do(func() {
		if !s.stillRunning() {
			return
		}
		tray := s.app.SystemTray.New()
		icon := spikeTrayIconBytes()
		tray.SetIcon(icon)
		tray.SetLabel("MaClaw spike")
		menu := s.app.NewMenu()
		menu.Add("退出 spike").OnClick(func(*application.Context) { s.app.Quit() })
		tray.SetMenu(menu)
		spikeLogf("C7 tray created (icon=%dB, menu=1 item) — manual check: tray visible, 退出 spike works", len(icon))
		if s.dialogRequested {
			dlg := s.app.Dialog.OpenFile().SetTitle("spike file dialog")
			path, err := dlg.PromptForSingleSelection()
			spikeLogf("C7b dialog path=%q err=%v", path, err)
		}
		spikeLogf("=== all automated checkpoints fired; waiting for --quit relay or manual close ===")
	})
}

func main() {
	setupSpikeResultLog()
	spikeLogf("=== maclaw wails3 spike start pid=%d args=%v ===", os.Getpid(), os.Args[1:])
	spikeLogf("ENV %s", spikeOSBuild())

	svc := &SpikeService{dialogRequested: slices.Contains(os.Args[1:], "--dialog")}

	app := application.New(application.Options{
		Name: "MaClawV3Spike",
		Services: []application.Service{
			application.NewService(svc),
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "maclaw-spike-wails3",
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				spikeLogf("C3 second-instance launch args=%v workdir=%s", data.Args, data.WorkingDir)
				// 移植提示：Windows 转发完整命令行，data.Args[0] 是第二实例
				// 的 exe 路径。guiapp 移植时参数判定必须从 Args[1:] 开始
				// （spike 这里用全量 Contains 是刻意的稳健写法）。
				if slices.Contains(data.Args, "--quit") {
					spikeLogf("C3 PASS second instance relayed --quit")
					svc.app.Quit()
				}
			},
		},
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(spikeAssetFS()),
			Middleware: spikeAssetMiddleware(),
		},
	})
	svc.app = app

	// Event sinks (C4/C5/C6/C8 verdicts arrive as CustomEvents from the pages).
	svc.bindEventSinks(app)

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "MaClaw Wails3 Spike",
		Name:             "spike-main",
		Width:            900,
		Height:           620,
		Frameless:        true,
		BackgroundColour: application.RGBA{Red: 243, Green: 245, Blue: 247, Alpha: 255},
		Windows: application.WindowsWindow{
			// Match the production Win11 choice: keep DWM frame decorations so
			// native rounded corners stay on (guiapp/main.go disableFramelessDecorations).
			DisableFramelessWindowDecorations: false,
		},
	})
	if win == nil {
		spikeLogf("FAIL main window creation returned nil")
		os.Exit(1)
	}
	svc.win = win
	var readyFired atomic.Bool
	win.OnWindowEvent(events.Common.WindowRuntimeReady, func(event *application.WindowEvent) {
		readyFired.Store(true)
		go svc.onWin1RuntimeReady()
	})
	// Watchdogs for the rapid-relaunch flake observed during Phase 0: an
	// immediate relaunch can stall WebView2 environment creation for seconds
	// and WindowRuntimeReady may not fire within the normal ~5s budget.
	time.AfterFunc(12*time.Second, func() {
		if !svc.stillRunning() {
			return
		}
		if !readyFired.Load() {
			spikeLogf("C0 WARN runtime not ready after 12s (rapid-relaunch env contention); checkpoints will not fire this run")
		}
	})
	// Tray normally fires from the win2 chain (ready+4s); this fallback keeps
	// C7 verifiable even when the webview chain stalls.
	time.AfterFunc(25*time.Second, func() { svc.createTray() })

	spikeLogf("main window created; running event loop")
	app.Run()
	svc.stopped.Store(true)
	spikeLogf("=== app exited ===")
}

func spikeAssetFS() fs.FS {
	sub, err := fs.Sub(spikeAssetsFS, "assets")
	if err != nil {
		return spikeAssetsFS
	}
	return sub
}

// spikeAssetMiddleware mirrors the shape of guiapp's
// fileCompanionAssetMiddleware: a POST route handled before the default
// handler, plus a Cache-Control override for HTML documents.
func spikeAssetMiddleware() application.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/maclaw-spike/append" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"ok":true}`)
				return
			}
			if strings.HasSuffix(r.URL.Path, ".html") {
				w.Header().Set("Cache-Control", "no-store")
			}
			next.ServeHTTP(w, r)
		})
	}
}
