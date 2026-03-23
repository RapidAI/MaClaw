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

	if isMacOSTahoeOrLater() {
		setupTrayTahoe(app, appOptions)
		return
	}
	setupTrayPreTahoe(app, appOptions)
}

// setupTrayTahoe uses a pure-Cocoa NSStatusItem (no energye/systray) to avoid
// Liquid Glass crashes on macOS 26+.
func setupTrayTahoe(app *App, appOptions *options.App) {
	origStartup := appOptions.OnStartup
	appOptions.OnStartup = func(ctx context.Context) {
		if origStartup != nil {
			origStartup(ctx)
		}

		t := trayTranslations["en"]

		setupTahoeTray(icon, t["title"], t["show"], t["quit"],
			func() {
				// Show
				runtime.WindowShow(app.ctx)
			},
			func() {
				// Quit
				runtime.Quit(app.ctx)
			},
		)

		UpdateTrayMenu = func(lang string) {
			t, ok := trayTranslations[lang]
			if !ok {
				t = trayTranslations["en"]
			}
			updateTahoeTrayMenu(t["title"], t["show"], t["quit"])
		}

		OnConfigChanged = func(cfg AppConfig) {
			runtime.EventsEmit(app.ctx, "config-changed", cfg)
		}

		// ShowNotification / FlashAndBeep not available on Tahoe tray.
		ShowNotification = func(title, message string, iconFlag uint32) {
			_, _, _ = title, message, iconFlag
		}
		FlashAndBeep = func() {}

		if app.CurrentLanguage != "" {
			UpdateTrayMenu(app.CurrentLanguage)
		}
	}
}

// setupTrayPreTahoe uses energye/systray via systray.Run() in a goroutine.
// This is the proven pattern from V3.2.1 that works on Intel Catalina through
// ARM Sequoia.
//
// ⚠️ DO NOT OPTIMIZE / DO NOT REPLACE with RunWithExternalLoop or local fork!
// This uses upstream energye/systray v1.0.2 (via local fork with Windows
// extras) with plain systray.Run() in a goroutine — the exact pattern that
// works reliably on all pre-Tahoe macOS versions. Leave it alone.
func setupTrayPreTahoe(app *App, appOptions *options.App) {
	appOptions.OnStartup = func(ctx context.Context) {
		app.startup(ctx)

		go systray.Run(func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[tray] panic in onReady: %v", r)
				}
			}()

			systray.SetIcon(icon)
			systray.SetTooltip("MaClaw Dashboard")
			systray.CreateMenu()

			mShow := systray.AddMenuItem("Show Main Window", "Show Main Window")
			systray.AddSeparator()
			mQuit := systray.AddMenuItem("Quit", "Quit Application")

			UpdateTrayMenu = func(lang string) {
				t, ok := trayTranslations[lang]
				if !ok {
					t = trayTranslations["en"]
				}
				systray.SetTooltip(t["title"])
				mShow.SetTitle(t["show"])
				mQuit.SetTitle(t["quit"])
			}

			OnConfigChanged = func(cfg AppConfig) {
				runtime.EventsEmit(app.ctx, "config-changed", cfg)
			}

			ShowNotification = func(title, message string, iconFlag uint32) {
				_, _, _ = title, message, iconFlag
			}

			FlashAndBeep = func() {}

			mShow.Click(func() {
				go runtime.WindowShow(app.ctx)
			})

			mQuit.Click(func() {
				go func() {
					systray.Quit()
					time.Sleep(100 * time.Millisecond)
					runtime.Quit(app.ctx)
				}()
			})

			if app.CurrentLanguage != "" {
				UpdateTrayMenu(app.CurrentLanguage)
			}
		}, func() {})
	}
}
