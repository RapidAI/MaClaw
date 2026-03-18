package main

import (
	"embed"
	"os"

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

	// Create application with options
	appOptions := &options.App{
		Title:       "MaClaw",
		Frameless:   true,
		Width:       612,
		Height:      311,
		StartHidden: app.IsAutoStart,
		OnStartup:   app.startup,
		OnDomReady:  app.domReady,
		OnShutdown:  app.shutdown,
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
		BackgroundColour: &options.RGBA{R: 255, G: 255, B: 255, A: 0},
		Bind: []interface{}{
			app,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			BackdropType:         windows.Auto,
		},
		Mac: &mac.Options{
			TitleBar:             mac.TitleBarHidden(),
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
		},
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
