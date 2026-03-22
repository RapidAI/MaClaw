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

	// On macOS 26 (Tahoe) with Liquid Glass, the systray library's
	// NSStatusBar / NSStatusItem rendering crashes shortly after the
	// WebView's first frame.  Until the systray library is updated for
	// Liquid Glass we skip tray creation entirely on Tahoe+.
	// The global callback variables (UpdateTrayMenu, ShowNotification,
	// FlashAndBeep) stay nil; all call-sites already nil-check them.
	if isMacOSTahoeOrLater() {
		log.Println("[tray] macOS Tahoe+ detected – skipping systray to avoid Liquid Glass crash")
		// Still wire OnConfigChanged so config-changed events reach the frontend.
		origDomReady := appOptions.OnDomReady
		appOptions.OnDomReady = func(ctx context.Context) {
			if origDomReady != nil {
				origDomReady(ctx)
			}
			OnConfigChanged = func(cfg AppConfig) {
				runtime.EventsEmit(app.ctx, "config-changed", cfg)
			}
		}
		return
	}

	// On pre-Tahoe macOS we defer systray init to OnDomReady so the
	// WebView and window are fully created before touching NSStatusBar.
	origDomReady := appOptions.OnDomReady
	appOptions.OnDomReady = func(ctx context.Context) {
		if origDomReady != nil {
			origDomReady(ctx)
		}

		// Use RunWithExternalLoop since Wails already owns the NSApplication event loop.
		start, _ := systray.RunWithExternalLoop(func() {
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
				_ = systray.ShowBalloonNotification(title, message, iconFlag)
			}

			FlashAndBeep = func() {
				systray.FlashAndBeep()
			}

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
				go func() {
					time.Sleep(500 * time.Millisecond)
					UpdateTrayMenu(app.CurrentLanguage)
				}()
			}
		}, func() {})

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
