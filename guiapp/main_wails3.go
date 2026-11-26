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
	"slices"
	"time"

	goruntime "runtime"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/RapidAI/CodeClaw/corelib/skill"
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

// Main is the application entry point (wails3 runtime).
func Main(v string) {
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
	// Preamble mirrors main.go's v2 Main line-for-line (the v2 path is gone;
	// keep them from drifting if anything is ever re-ported).
	isTUI := isTUISubcommand(args)

	// --- Log to file: ~/.maclaw/logs/maclaw.log ---
	corelib.SyncLogDetailEnabledFromDefaultConfig()
	initLogFile()
	if isTUI {
		setLogFallback(false, nil)
	}

	// --- Program log for programming tool output ---
	programLogger.Init()

	if isCompanionPaperHostArgv(args) {
		os.Exit(runCompanionPaperHost(args))
	}
	if goruntime.GOOS != "darwin" && companionPaperFromArgs(args) {
		os.Exit(0)
	}
	bm25.PrewarmDict()
	skill.MigrateSkillsDir()

	if isTUI {
		runTUIMode(nil)
		return
	}
	app := NewApp()
	if len(args) > 1 {
		if args[1] == "remote-smoke" {
			os.Exit(runRemoteSmoke(app, args[2:]))
		}
		if args[1] == "generate-mobile-pwa-shell" {
			os.Exit(runMobilePWAShellGenerator(app, args[2:]))
		}
		if args[1] == "generate-android-pwa-shell" {
			os.Exit(runAndroidPWAShellGenerator(app, args[2:]))
		}
	}
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
	visualHosting := useVisualHostingWails3()
	bootLog("wails3 gpuDisabled=%v visualHosting=%v remote=%v", gpuDisabled, visualHosting, perfSessionIsRemote())
	launch := classifyDesktopLaunch(args)
	winWidth, winHeight := envCheckWidth, envCheckHeight
	hideOnClose := false
	if launch.Mode == desktopLaunchFileCompanion {
		// v2 HideWindowOnClose parity (main.go companion comment): Alt+F4
		// must hide the companion so later open-file launches can re-show
		// it — a destroyed window would strand the reopen relay.
		hideOnClose = true
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
		// Visual hosting over RDP (official wails3 RDP fix); no-op off RDP.

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
	runningWails3App = v3app

	// v2 EditMenu parity: without an application EditMenu, Ctrl+A in a
	// WebView2 input selects the whole page instead of the input text. The
	// Windows application menu is not rendered as a visible bar; it exists
	// for the editing accelerators (same effect as the v2 setupTray attach).
	editMenu := v3app.NewMenu()
	editMenu.AddRole(application.EditMenu)
	v3app.Menu.SetApplicationMenu(editMenu)

	win := v3app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            brand.Current().WindowTitle,
		Width:            winWidth,
		Height:           winHeight,
		Frameless:        true,
		HideOnClose:      hideOnClose,
		BackgroundColour: application.RGBA{Red: 243, Green: 245, Blue: 247, Alpha: 255}, // #f3f5f7
		Windows: application.WindowsWindow{
			// Windows 11 keeps native DWM rounding (see main.go v2 comment);
			// Windows 10 disables the decorations it cannot render.
			DisableFramelessWindowDecorations: !app.IsNativeRoundedCorners(),
			// THE fix for the RDP white screen: v3.1.0's app-level
			// UseVisualHosting option is a no-op (its env var has no consumer
			// in this version — verified against the vendored source). The
			// live switch is per-window composition hosting
			// (ICoreWebView2CompositionController + DirectComposition).
			WebView2CompositionHosting: visualHosting,
		},
	})
	wruntime.SetMainWindow(win)

	// Tray: start after the single-instance lock is accepted (wails3 tag
	// dispatches to the reused systray fork loop on Windows).
	bootLog("wails3 window %dx%d frameless=true startMaximised=%v mode=%s", winWidth, winHeight, startMaximised, launch.Mode)
	setupTray3(app, args[1:])

	// --desktop-stage: open the standalone desktop-stream window at launch
	// (also serves as the programmatic verification of the pop-out binding).
	if slices.Contains(args, "--desktop-stage") {
		go func() {
			time.Sleep(5 * time.Second) // let the main window settle first
			app.OpenDesktopStageWindow(1280, 10)
		}()
	}

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

	// v2 Main 同款收尾：companion 退役 + 确定性退出（注入钩子下
	// ExitProcess 挂死防护，见 finishDesktopExit 注释）。
	app.signalFileCompanionQuit()
	finishDesktopExit()
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
