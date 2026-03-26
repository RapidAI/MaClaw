package main

import (
	"embed"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

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

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// --- Log to file: ~/.maclaw/logs/maclaw.log ---
	initLogFile()

	// Migrate ~/.maclaw/skills → ~/.maclaw/data/skills (one-time).
	skill.MigrateSkillsDir()

	// Create an instance of the app structure
	app := NewApp()

	// Check for command line arguments
	args := os.Args
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
	bgColour := &options.RGBA{R: 255, G: 255, B: 255, A: 255}
	frameless := true

	// Create application with options
	appOptions := &options.App{
		Title:                    brand.Current().WindowTitle,
		Frameless:                frameless,
		Width:                    807,
		Height:                   392,
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
			Assets: assets,
		},
		BackgroundColour: bgColour,
		Bind: []interface{}{
			app,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			BackdropType:         windows.Auto,
		},
		Mac:   macOpts,
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

// initLogFile sets up log output to ~/.maclaw/logs/maclaw.log (with rotation)
// while keeping stderr as a fallback. Logs are rotated when the file exceeds
// 10 MB; the previous log is kept as maclaw.log.1.
func initLogFile() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".maclaw", "logs")
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
	mw := io.MultiWriter(f, os.Stderr)
	log.SetOutput(mw)
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("[maclaw] === started at %s ===", time.Now().Format(time.RFC3339))
	fmt.Fprintf(os.Stderr, "[maclaw] logging to %s\n", logPath)
}
