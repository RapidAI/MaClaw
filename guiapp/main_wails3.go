//go:build wails3

package guiapp

// guiapp.Main under the wails3 build tag: the v3 bootstrap mirroring
// main.go's v2 Main. The launch preamble is intentionally duplicated from
// main.go (keep in sync until the v2 path is retired in Phase 5) so this
// file stays an isolated, reversible migration unit.
//
// Not yet wired under wails3 (tracked for later Phase 1 commits):
//   - WebView2 user-data-path / GPU-disable (v3 app-level Windows options
//     need source verification before wiring)
//   - tray construction (tray_*.go still target the v2 menu path)
//   - guarded CSSDragProperty (v3 drag verified in the Phase 0 spike)

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/RapidAI/CodeClaw/corelib/brand"
	wruntime "github.com/RapidAI/CodeClaw/guiapp/wruntime"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// eventsCtxKey mirrors the value key the v2 runtime installs so the
// emitEvent guard (a.ctx.Value("events")) keeps working unchanged.
const eventsCtxKey = "events"

// withEventsCtx reproduces the v2 startup context: the emitEvent funnel
// checks ctx.Value("events") for non-nil before emitting.
func withEventsCtx(ctx context.Context) context.Context {
	return context.WithValue(ctx, eventsCtxKey, struct{}{})
}

// ServiceStartup makes *App a wails3 service, routing into the existing v2
// startup hook unchanged.
func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	a.startup(withEventsCtx(ctx))
	return nil
}

// ServiceShutdown routes into the existing shutdown hook.
func (a *App) ServiceShutdown() {
	if a.ctx == nil {
		return
	}
	a.shutdown(a.ctx)
}

// wails3Enabled is the build-tag dispatch flag read by cmd/maclaw-gui.
const wails3Enabled = true

// Main3 is the wails3 entry point; the default build's Main3 (main_wails2.go)
// forwards to the v2 Main.
func Main3(v string) {
	if v != "" {
		version = v
	}
	args := os.Args
	if handleCLIInfo(args) {
		return
	}
	// `MaClaw mcp` is the stdio proxy for the inbound MCP server. It must
	// exit before the window, the log file, and the dictionary load.
	if isMCPStdioCommand(args) {
		os.Exit(runMCPStdioProxy(os.Stdin, os.Stdout))
	}
	// console-buffer attaches to a client console. It must return before
	// NewApp and ensureHiddenHostConsole, or AttachConsole fails.
	if isConsoleBufferCommand(args) {
		os.Exit(runConsoleBufferCommand(args))
	}
	// Preamble kept in sync with main.go Main().
	app := NewApp()
	app.applyInitialLanguage()
	app.setPendingReferralHandoff(referralHandoffFromArgs(args))
	if launch := cloudWorkspaceShareFromArgs(args); launch.Token != "" {
		app.setPendingCloudWorkspaceShare(launch)
	}
	if gift := creditGiftFromArgs(args); gift.Code != "" {
		app.setPendingCreditGift(gift)
	}
	for _, arg := range args[1:] {
		if arg == "init" {
			app.IsInitMode = true
		}
		if arg == "autostart" {
			app.IsAutoStart = true
		}
	}
	app.platformStartup()
	cleanStaleLock()

	envCheckWidth, envCheckHeight := envCheckWindowSize()
	startMaximised := shouldMaximiseMainWindowForPrimaryScreen()

	// Launch classification mirrors main.go: file-companion launches seed the
	// companion variant and override the window geometry (v2
	// applyDesktopLaunchOptions; HideWindowOnClose has no direct v3 field yet
	// — tracked gap).
	// GPU self-heal parity with v2 (main.go WebviewGpuIsDisabled): the paint
	// watchdog records a marker after a blank window; the next launch disables
	// WebView2 hardware acceleration. v3.1.0 exposes no Windows GPU toggle in
	// application.Options, so use the WebView2 loader's supported env var,
	// which create_core_webview2_environment reads at environment creation.
	gpuDisabled := webviewGPUDisabled(defaultWebviewUserDataPath())
	if gpuDisabled {
		os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", "--disable-gpu")
	}
	bootLog("wails3 gpuDisabled=%v", gpuDisabled)
	launch := classifyDesktopLaunch(args)
	winWidth, winHeight := envCheckWidth, envCheckHeight
	if launch.Mode == desktopLaunchFileCompanion {
		switch {
		case launch.Quit:
			app.seedFileCompanionQuit()
		case launch.Notice != "":
			app.seedFileCompanionNotice(launch.Notice)
		case launch.Library:
			app.seedFileCompanionLibrary()
		case launch.Place:
			app.seedFileCompanionPlace(launch.Paths)
		default:
			app.seedFileCompanionPaths(launch.Paths)
		}
		winWidth, winHeight = 1280, 800
		startMaximised = false
	}

	v3app := application.New(application.Options{
		Name: brand.Current().WindowTitle,

		Services: []application.Service{
			application.NewService(app),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
			Middleware: func(next http.Handler) http.Handler {
				return fileCompanionAssetMiddleware(app, next)
			},
		},
		SingleInstance: &application.SingleInstanceOptions{
			// Brand-scoped: TigerClaw/MetaStaff must not share MaClaw's
			// single-instance lock (see main.go v2 comment).
			UniqueID: singleInstanceUniqueID(),
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				onSecondInstanceLaunch3(app, data.Args[1:])
			},
		},
	})
	wruntime.SetApplication(v3app)

	win := v3app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            brand.Current().WindowTitle,
		Width:            winWidth,
		Height:           winHeight,
		Frameless:        true,
		BackgroundColour: application.RGBA{Red: 243, Green: 245, Blue: 247, Alpha: 255}, // #f3f5f7
		Windows: application.WindowsWindow{
			// Windows 11 keeps native DWM rounding (see main.go v2 comment);
			// Windows 10 disables the decorations it cannot render.
			DisableFramelessWindowDecorations: !app.IsNativeRoundedCorners(),
		},
	})
	wruntime.SetMainWindow(win)

	// Tray: start after the single-instance lock is accepted (wails3 tag
	// dispatches to the reused systray fork loop on Windows).
	bootLog("wails3 window %dx%d frameless=true startMaximised=%v mode=%s", winWidth, winHeight, startMaximised, launch.Mode)
	setupTray3(app, args[1:])

	// v2 OnDomReady equivalent.
	win.OnWindowEvent(events.Common.WindowRuntimeReady, func(event *application.WindowEvent) {
		go app.domReady(app.ctx)
	})

	if app.IsAutoStart {
		win.Hide()
	} else if startMaximised {
		win.Maximise()
	}

	log.Printf("[boot] wails3 event loop starting")
	v3app.Run()
}

// onSecondInstanceLaunch3 ports the v2 OnSecondInstanceLaunch body; args must
// already exclude the executable path (v3 SecondInstanceData.Args[0] is the
// second instance's own exe path).
func onSecondInstanceLaunch3(app *App, args []string) {
	if app.ctx == nil {
		return
	}
	// `MaClaw.exe --quit` (or `quit`) is a tray-independent way to end the
	// app: quit instead of activating the window.
	if secondInstanceRequestsQuit(args) {
		log.Printf("[exit-lifecycle] quit requested by command line")
		requestAppQuit(app.ctx)
		return
	}
	shouldShowWindow := true
	for _, arg := range args {
		if arg == "init" {
			go app.CheckEnvironment(true)
		}
		if arg == "autostart" {
			shouldShowWindow = false
		}
	}
	if handoff := referralHandoffFromArgs(args); handoff.Handoff != "" {
		app.setPendingReferralHandoff(handoff)
	}
	if launch := cloudWorkspaceShareFromArgs(args); launch.Token != "" {
		app.setPendingCloudWorkspaceShare(launch)
	}
	if gift := creditGiftFromArgs(args); gift.Code != "" {
		app.setPendingCreditGift(gift)
	}
	if companionPaperFromArgs(args) {
		shouldShowWindow = false
	}
	if !shouldShowWindow {
		return
	}
	go func() {
		wruntime.WindowUnminimise(app.ctx)
		wruntime.WindowShow(app.ctx)
		wruntime.WindowSetAlwaysOnTop(app.ctx, true)
		wruntime.WindowSetAlwaysOnTop(app.ctx, false)
	}()
}

// Wails3Enabled reports whether this binary was built with the wails3 tag.
func Wails3Enabled() bool { return wails3Enabled }
