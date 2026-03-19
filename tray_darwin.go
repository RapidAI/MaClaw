//go:build darwin

package main

import (
	"context"
	"log"
	"time"

	"github.com/energye/systray"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func setupTray(app *App, appOptions *options.App) {
	// We still use a basic Application Menu for macOS to support standard shortcuts
	appMenu := menu.NewMenu()
	appMenu.Append(menu.AppMenu())
	appMenu.Append(menu.EditMenu())
	appOptions.Menu = appMenu

	appOptions.OnStartup = func(ctx context.Context) {
		app.startup(ctx)

		// Use RunWithExternalLoop since Wails already owns the NSApplication event loop.
		// systray.Run() would call setInternalLoop(true) and override Wails' AppDelegate,
		// causing the app to crash on launch.
		start, _ := systray.RunWithExternalLoop(func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[tray] panic in onReady: %v", r)
				}
			}()

			systray.SetIcon(icon)
			// Do not set title for macOS as requested
			systray.SetTooltip("MaClaw Dashboard")

			// Ensure clicking the icon shows the menu immediately on macOS
			systray.CreateMenu()

			mShow := systray.AddMenuItem("Show Main Window", "Show Main Window")
			systray.AddSeparator()
			mQuit := systray.AddMenuItem("Quit", "Quit Application")

			// Register update function
			UpdateTrayMenu = func(lang string) {
				t, ok := trayTranslations[lang]
				if !ok {
					t = trayTranslations["en"]
				}
				systray.SetTooltip(t["title"])
				mShow.SetTitle(t["show"])
				mQuit.SetTitle(t["quit"])
			}

			// Register config change listener
			OnConfigChanged = func(cfg AppConfig) {
				runtime.EventsEmit(app.ctx, "config-changed", cfg)
			}

			// Register system notification function
			ShowNotification = func(title, message string, iconFlag uint32) {
				_ = systray.ShowBalloonNotification(title, message, iconFlag)
			}

			// Register flash + sound function
			FlashAndBeep = func() {
				systray.FlashAndBeep()
			}

			// Handle menu clicks
			mShow.Click(func() {
				go runtime.WindowShow(app.ctx)
			})

			mQuit.Click(func() {
				go func() {
					systray.Quit()
					time.Sleep(100 * time.Millisecond) // let status item removal complete
					runtime.Quit(app.ctx)
				}()
			})

			// Initial language sync
			if app.CurrentLanguage != "" {
				go func() {
					time.Sleep(500 * time.Millisecond)
					UpdateTrayMenu(app.CurrentLanguage)
				}()
			}
		}, func() {})

		// Start the systray native integration (without taking over the event loop).
		// nativeStart uses dispatch_after with a 1.5s delay to schedule status bar
		// creation on the main thread, avoiding races with Liquid Glass rendering
		// on macOS Tahoe.  It returns immediately.
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[tray] panic in nativeStart: %v", r)
				}
			}()
			start()
		}()
	}
}
