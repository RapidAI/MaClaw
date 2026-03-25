//go:build darwin

package main

import (
	"context"
	"log"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/brand"
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

		t := trayTranslations()["en"]

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
			tr := trayTranslations()
			t, ok := tr[lang]
			if !ok {
				t = tr["en"]
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

// setupTrayPreTahoe uses energye/systray via RunWithExternalLoop() so that
// the systray status-bar item is created without interfering with Wails'
// NSApplication delegate or Cocoa event loop.
//
// Previous versions used systray.Run() in a goroutine, which called
// registerSystray() → [[NSApplication sharedApplication] setDelegate:owner]
// and then [NSApp run], effectively hijacking the Wails-owned main run loop.
// On macOS 15+ this causes an immediate crash (SIGABRT / SIGSEGV) because
// AppKit enforces stricter thread-safety checks on the application delegate.
//
// RunWithExternalLoop avoids both problems: it does NOT set the NSApp
// delegate and does NOT call [NSApp run]. Instead it returns start/end
// callbacks; calling start() schedules the status-bar creation on the main
// thread via dispatch_async (see nativeStart in systray_darwin.m).
func setupTrayPreTahoe(app *App, appOptions *options.App) {
	appOptions.OnStartup = func(ctx context.Context) {
		app.startup(ctx)

		onReady := func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[tray] panic in onReady: %v", r)
				}
			}()

			systray.SetIcon(icon)
			systray.SetTooltip(brand.Current().TrayTooltip)
			systray.CreateMenu()

			mShow := systray.AddMenuItem("Show Main Window", "Show Main Window")
			systray.AddSeparator()
			mQuit := systray.AddMenuItem("Quit", "Quit Application")

			UpdateTrayMenu = func(lang string) {
				tr := trayTranslations()
				t, ok := tr[lang]
				if !ok {
					t = tr["en"]
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
		}

		start, _ := systray.RunWithExternalLoop(onReady, func() {})
		start()
	}
}
