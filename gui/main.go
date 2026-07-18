package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// version is set at build time via -ldflags "-X main.version=..."
var version = "dev"

func main() {
	args := os.Args
	isTUI := isTUISubcommand(args)

	// --- Log to file: ~/.maclaw/logs/maclaw.log ---
	corelib.SyncLogDetailEnabledFromDefaultConfig()
	initLogFile()
	if isTUI {
		setLogFallback(false, nil)
	}

	// --- Program log for programming tool output ---
	programLogger.Init()

	// Migrate ~/.maclaw/skills → ~/.maclaw/data/skills (one-time).
	skill.MigrateSkillsDir()

	// TUI is a terminal subcommand, not a desktop startup mode. Dispatch it
	// before constructing the desktop App path so it cannot fall through into
	// platform/window initialization.
	if isTUI {
		runTUIMode(nil)
		return
	}

	// Create an instance of the app structure
	app := NewApp()

	// Check for command line arguments
	if len(args) > 1 {
		if args[1] == "remote-smoke" {
			code := runRemoteSmoke(app, args[2:])
			os.Exit(code)
		}
		if args[1] == "generate-mobile-pwa-shell" {
			code := runMobilePWAShellGenerator(app, args[2:])
			os.Exit(code)
		}
		if args[1] == "generate-android-pwa-shell" {
			code := runAndroidPWAShellGenerator(app, args[2:])
			os.Exit(code)
		}
		for _, arg := range args[1:] {
			if arg == "init" {
				app.IsInitMode = true
			}
			if arg == "autostart" {
				app.IsAutoStart = true
			}
		}
	}

	// Platform specific early initialization (like hiding console on Windows)
	app.platformStartup()

	// Clean up stale SingleInstanceLock file from a previous crash.
	// Without this, macOS launches silently exit via os.Exit(0) when the
	// lock file exists but the owning process is gone.
	cleanStaleLock()

	// All platforms use frameless mode; the frontend provides its own title bar
	// with drag region and window controls.
	// Keep WebviewIsTransparent and WindowIsTranslucent false to avoid
	// NSVisualEffectView / Liquid Glass crashes on macOS 15+ and 26+.
	macOpts := &mac.Options{
		TitleBar:             mac.TitleBarDefault(),
		WebviewIsTransparent: false,
		WindowIsTranslucent:  false,
	}
	// Native window background colour.  When the webview is transparent
	// (Windows), this colour is never visible — the CSS border-radius on
	// #App clips to transparency.  When the webview is opaque (macOS/Linux),
	// this colour shows behind the CSS corners for the brief instant before
	// the frontend renders.
	// ── Keep in sync with: App.css  --theme-page-bg  (dark)
	bgColour := &options.RGBA{R: 11, G: 18, B: 32, A: 255} // #0b1220
	frameless := true

	// Windows 11 (build >= 22000) natively rounds frameless window corners
	// via DWM.  We let the OS handle rounding by keeping
	// DisableFramelessWindowDecorations = false.
	//
	// Windows 10 does NOT have native rounded corners, and DWM reserves an
	// invisible border area for decorations that offsets the webview content
	// and clips the custom title bar.  So we disable decorations on Win10
	// and rely on CSS border-radius instead.
	//
	// On macOS/Linux this method returns false, so decorations are always
	// disabled (no-op on those platforms anyway).
	disableFramelessDecorations := !app.IsNativeRoundedCorners()

	// Windows 10: enable transparent webview so the CSS border-radius on
	// #App clips to true transparency — no corner artifacts regardless of
	// theme.  html/body background is set to transparent; #App itself has
	// an opaque background-color, so backdrop-filter inside #App still
	// works correctly (it blurs #App's opaque content, not the desktop).
	//
	// Windows 11: both false — DWM provides native rounded corners;
	// transparent webview is unnecessary and WindowIsTranslucent triggers
	// the slow BlurBehind effect on early Win11 builds (22000–22620).
	//
	// macOS: both false — WebviewIsTransparent causes NSVisualEffectView /
	// Liquid Glass crashes on macOS 15+ and 26+.
	winWebviewTransparent, winWindowTranslucent := app.PlatformTransparencyFlags()
	webviewUserDataPath := defaultWebviewUserDataPath()
	clearWebviewAssetCacheIfNeeded(webviewUserDataPath)

	// Create application with options
	// Start with compact size for environment check, resize to full after check completes.
	envCheckWidth, envCheckHeight := envCheckWindowSize()
	appOptions := &options.App{
		Title:                    brand.Current().WindowTitle,
		Frameless:                frameless,
		Width:                    envCheckWidth,
		Height:                   envCheckHeight,
		EnableDefaultContextMenu: true,
		StartHidden:              app.IsAutoStart,
		OnStartup:                app.startup,
		OnDomReady:               app.domReady,
		OnShutdown:               app.shutdown,
		SingleInstanceLock: &options.SingleInstanceLock{
			// Brand-scoped: TigerClaw/MetaStaff must not share MaClaw's single-instance lock
			// (otherwise launching OEM can activate/hide behind the other product).
			UniqueId: singleInstanceUniqueID(),
			OnSecondInstanceLaunch: func(secondInstanceData options.SecondInstanceData) {
				if app.ctx == nil {
					return
				}

				shouldShowWindow := true

				// Check if init argument was passed to the second instance
				for _, arg := range secondInstanceData.Args {
					if arg == "init" {
						go app.CheckEnvironment(true)
					}
					if arg == "autostart" {
						shouldShowWindow = false
					}
				}

				if !shouldShowWindow {
					return
				}

				go func() {
					runtime.WindowUnminimise(app.ctx)
					runtime.WindowShow(app.ctx)
					runtime.WindowSetAlwaysOnTop(app.ctx, true)
					runtime.WindowSetAlwaysOnTop(app.ctx, false)
				}()
			},
		},
		AssetServer: &assetserver.Options{
			Assets: assets,
			// Binary live-record append (POST /maclaw-record/v1/append) + no-store static assets.
			Middleware: func(next http.Handler) http.Handler {
				return recordAudioAssetMiddleware(app, noStoreAssetMiddleware(next))
			},
		},
		BackgroundColour: bgColour,
		Bind: []interface{}{
			app,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: winWebviewTransparent,
			WindowIsTranslucent:  winWindowTranslucent,
			BackdropType:         windows.None,
			WebviewUserDataPath:  webviewUserDataPath,
			// On Windows 10, disable DWM decorations to prevent the invisible
			// border area from offsetting the webview.  On Windows 11, keep
			// decorations enabled so DWM provides native rounded corners.
			DisableFramelessWindowDecorations: disableFramelessDecorations,
		},
		Mac: macOpts,
		Linux: &linux.Options{
			Icon: icon,
		},
	}

	// Platform specific tray/menu setup
	setupTray(app, appOptions)

	err := wails.Run(appOptions)

	if err != nil {
		println("Error:", err.Error())
	}
}

// singleInstanceUniqueID isolates OEM brands so MaClaw / TigerClaw / MetaStaff
// can run side by side without one process swallowing the other.
func singleInstanceUniqueID() string {
	id := strings.TrimSpace(brand.Current().ID)
	if id == "" {
		id = "maclaw"
	}
	// Keep historical id for default brand so existing Mac installs continue to work.
	if id == "maclaw" {
		return "maclaw-lock"
	}
	return id + "-lock"
}

func defaultWebviewUserDataPath() string {
	if goruntime.GOOS != "windows" {
		return ""
	}
	configDir, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(configDir) == "" {
		return ""
	}
	// Brand-isolated WebView2 profile. Sharing "MaClaw.exe" across OEM builds caused
	// "version is new but welcome/UI looks old" when a newer product's cache fingerprint
	// skipped clearing and WebView reused another brand's chunk cache.
	// Use stable ASCII folder names (not DisplayNameCN) so paths stay portable.
	return filepath.Join(configDir, webviewProfileFolder()+".exe")
}

// webviewProfileFolder returns the Windows WebView2 user-data folder basename.
func webviewProfileFolder() string {
	switch strings.TrimSpace(brand.Current().ID) {
	case "qianxin":
		return "TigerClaw"
	case "metastaff":
		return "MetaStaff"
	default:
		// Historical MaClaw path — keep for upgrade continuity.
		return "MaClaw"
	}
}

func noStoreAssetMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		rw.Header().Set("Pragma", "no-cache")
		rw.Header().Set("Expires", "0")
		next.ServeHTTP(rw, req)
	})
}

func clearWebviewAssetCacheIfNeeded(userDataPath string) {
	clearWebviewAssetCacheForFingerprint(userDataPath, embeddedFrontendFingerprint)
}

func clearWebviewAssetCacheForFingerprint(userDataPath string, fingerprintFunc func() (string, error)) {
	userDataPath = strings.TrimSpace(userDataPath)
	if userDataPath == "" {
		return
	}
	fingerprint, err := fingerprintFunc()
	if err != nil {
		// Fail-open: if we cannot identify the build, clear caches so a broken
		// fingerprint path never leaves users stuck on a stale WebView Code Cache.
		log.Printf("[webview-cache] frontend fingerprint unavailable (%v); clearing caches", err)
		clearWebviewAssetCacheDirs(userDataPath)
		return
	}
	markerPath := filepath.Join(userDataPath, ".maclaw-frontend-build.sha256")
	if existing, err := os.ReadFile(markerPath); err == nil && strings.TrimSpace(string(existing)) == fingerprint {
		return
	}

	clearWebviewAssetCacheDirs(userDataPath)
	if err := os.MkdirAll(userDataPath, 0o755); err != nil {
		log.Printf("[webview-cache] failed to create %s: %v", userDataPath, err)
		return
	}
	if err := os.WriteFile(markerPath, []byte(fingerprint), 0o644); err != nil {
		log.Printf("[webview-cache] failed to write marker %s: %v", markerPath, err)
	}
}

func clearWebviewAssetCacheDirs(userDataPath string) {
	for _, rel := range webviewAssetCacheDirs() {
		target := filepath.Join(userDataPath, rel)
		if err := os.RemoveAll(target); err != nil {
			log.Printf("[webview-cache] failed to remove %s: %v", target, err)
		}
	}
}

// embeddedFrontendFingerprint builds a cheap, reliable identity for the embedded
// frontend so WebView2 Code Cache is cleared when the UI actually changes.
//
// We intentionally avoid hashing every asset body (mermaid/katex can be multi-MB):
// Vite content-hashes chunk names, so those names appear in index.html / entry JS.
// Inventory of path+size catches missing/extra files; full body of index.html is
// enough to detect the cascade. Brand + binary version isolate OEM/rebuilds.
func embeddedFrontendFingerprint() (string, error) {
	h := sha256.New()
	indexPath := "frontend/dist/index.html"
	indexHTML, err := assets.ReadFile(indexPath)
	if err != nil {
		// Fallback for test assets stub that only has index.html at root.
		data, readErr := assets.ReadFile("index.html")
		if readErr != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:]), nil
	}
	_, _ = io.WriteString(h, indexPath)
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(indexHTML)
	_, _ = h.Write([]byte{0})

	type inv struct {
		path string
		size int64
	}
	var items []inv
	walkErr := fs.WalkDir(assets, "frontend/dist", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || path == indexPath {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		items = append(items, inv{path: path, size: info.Size()})
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	sort.Slice(items, func(i, j int) bool { return items[i].path < items[j].path })
	for _, it := range items {
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00", it.path, it.size)
	}
	_, _ = io.WriteString(h, "\nbrand=")
	_, _ = io.WriteString(h, brand.Current().ID)
	_, _ = io.WriteString(h, "\nversion=")
	_, _ = io.WriteString(h, version)
	return hex.EncodeToString(h.Sum(nil)), nil
}

func webviewAssetCacheDirs() []string {
	return []string{
		filepath.Join("EBWebView", "Default", "Cache"),
		filepath.Join("EBWebView", "Default", "Code Cache"),
		filepath.Join("EBWebView", "Default", "DawnGraphiteCache"),
		filepath.Join("EBWebView", "Default", "DawnWebGPUCache"),
		filepath.Join("EBWebView", "Default", "GPUCache"),
		filepath.Join("EBWebView", "Default", "Service Worker", "CacheStorage"),
		filepath.Join("EBWebView", "Default", "Service Worker", "ScriptCache"),
		filepath.Join("EBWebView", "Default", "ShaderCache"),
		filepath.Join("EBWebView", "GrShaderCache"),
		filepath.Join("EBWebView", "ShaderCache"),
	}
}

func isTUISubcommand(args []string) bool {
	if len(args) < 2 {
		return false
	}
	subcommand := strings.TrimSpace(strings.ToLower(args[1]))
	return subcommand == "tui" || subcommand == "ui"
}

// initLogFile sets up log output to <MaclawBaseDir>/logs/maclaw.log (with rotation)
// while keeping stderr as a fallback. Logs are rotated when the file exceeds
// 10 MB; the previous log is kept as maclaw.log.1.
func initLogFile() {
	dir := corelib.MaclawLogsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	logPath := filepath.Join(dir, "maclaw.log")

	// Rotate if existing log exceeds 10 MB.
	if info, err := os.Stat(logPath); err == nil && info.Size() > 10*1024*1024 {
		prev := logPath + ".1"
		_ = os.Remove(prev)
		_ = os.Rename(logPath, prev)
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	// Write to both file and stderr so console still works during development.
	mw := &detailAwareLogWriter{file: f, stderr: os.Stderr}
	log.SetOutput(mw)
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("[maclaw] === started at %s ===", time.Now().Format(time.RFC3339))
	fmt.Fprintf(os.Stderr, "[maclaw] logging to %s\n", logPath)
}

type detailAwareLogWriter struct {
	file   io.Writer
	stderr io.Writer
}

func (w *detailAwareLogWriter) Write(p []byte) (int, error) {
	if !corelib.IsLogDetailEnabled() && !isImportantLogLine(string(p)) {
		return len(p), nil
	}
	// Write sinks independently. io.MultiWriter fails the whole write when any
	// sink errors; with -H windowsgui, stderr is often broken and would drop
	// already-successful file writes from the caller's perspective (and can
	// interact badly with concurrent appenders).
	var firstErr error
	if w.file != nil {
		if _, err := w.file.Write(p); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if w.stderr != nil {
		_, _ = w.stderr.Write(p) // best-effort; never fail the log line for console
	}
	if firstErr != nil {
		return 0, firstErr
	}
	return len(p), nil
}

func setLogFallback(desktop bool, stderr io.Writer) {
	if !desktop {
		log.SetOutput(io.Discard)
		return
	}
	if stderr == nil {
		log.SetOutput(io.Discard)
		return
	}
	log.SetOutput(stderr)
}

func isImportantLogLine(line string) bool {
	lower := strings.ToLower(line)
	// Keep download/workdir diagnostics visible even when log_detail_enabled is off:
	// agents land files under working_directory only when these paths are wired correctly.
	keywords := []string{"error", "err=", "failed", "fatal", "panic", "warn", "warning",
		"[skill-runner]", "[skill-scanner]", "[lansenger]",
		"-lifecycle]",
		"[download_file]", "download_file=builtin", "effective_wd=", "configured_wd=",
		"workdir ready", "inject workdir",
		"[startup] loadconfig", "[startup] begin", "[startup] complete", "[startup] workdir"}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
