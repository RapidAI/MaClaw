package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
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
			UniqueId: "maclaw-lock",
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
			Assets:     assets,
			Middleware: noStoreAssetMiddleware,
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

func defaultWebviewUserDataPath() string {
	if goruntime.GOOS != "windows" {
		return ""
	}
	configDir, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(configDir) == "" {
		return ""
	}
	return filepath.Join(configDir, "MaClaw.exe")
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
		log.Printf("[webview-cache] frontend fingerprint unavailable: %v", err)
		return
	}
	markerPath := filepath.Join(userDataPath, ".maclaw-frontend-build.sha256")
	if existing, err := os.ReadFile(markerPath); err == nil && strings.TrimSpace(string(existing)) == fingerprint {
		return
	}

	for _, rel := range webviewAssetCacheDirs() {
		target := filepath.Join(userDataPath, rel)
		if err := os.RemoveAll(target); err != nil {
			log.Printf("[webview-cache] failed to remove %s: %v", target, err)
		}
	}
	if err := os.MkdirAll(userDataPath, 0o755); err != nil {
		log.Printf("[webview-cache] failed to create %s: %v", userDataPath, err)
		return
	}
	if err := os.WriteFile(markerPath, []byte(fingerprint), 0o644); err != nil {
		log.Printf("[webview-cache] failed to write marker %s: %v", markerPath, err)
	}
}

func embeddedFrontendFingerprint() (string, error) {
	indexHTML, err := assets.ReadFile("frontend/dist/index.html")
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(indexHTML)
	return hex.EncodeToString(sum[:]), nil
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
	if corelib.IsLogDetailEnabled() || isImportantLogLine(string(p)) {
		writers := make([]io.Writer, 0, 2)
		if w.file != nil {
			writers = append(writers, w.file)
		}
		if w.stderr != nil {
			writers = append(writers, w.stderr)
		}
		if len(writers) == 0 {
			return len(p), nil
		}
		_, err := io.MultiWriter(writers...).Write(p)
		if err != nil {
			return 0, err
		}
		return len(p), nil
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
	keywords := []string{"error", "err=", "failed", "fatal", "panic", "warn", "warning",
		"[skill-runner]", "[skill-scanner]", "[lansenger]",
		"-lifecycle]"}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
