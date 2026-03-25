//go:build darwin

package main

import (
	"context"
	"os/exec"
	"strings"

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

	// All macOS versions now use the pure-Cocoa NSStatusItem tray.
	// The previous energye/systray path (setupTrayPreTahoe) caused crashes
	// on macOS 15+ due to AppKit thread-safety enforcement and race
	// conditions between systray's deferred init and Wails' event loop.
	setupTrayNative(app, appOptions)
}

// setupTrayNative uses a pure-Cocoa NSStatusItem (no energye/systray) for
// all macOS versions. This avoids the thread-safety crashes that
// energye/systray triggers on macOS 15+ and the Liquid Glass issues on
// macOS 26+.
func setupTrayNative(app *App, appOptions *options.App) {
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

		ShowNotification = func(title, message string, iconFlag uint32) {
			// Use osascript to display a native macOS notification.
			// Escape backslashes first, then double quotes for AppleScript strings.
			// Strip newlines — AppleScript string literals don't support them.
			r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ", "\r", "")
			safeTitle := r.Replace(title)
			safeMsg := r.Replace(message)
			script := `display notification "` + safeMsg + `" with title "` + safeTitle + `" sound name "default"`
			_ = exec.Command("osascript", "-e", script).Start()
		}

		FlashAndBeep = func() {
			// Dock bounce is handled via the Tahoe ObjC helper.
			tahoeDockBounce()
		}

		if app.CurrentLanguage != "" {
			UpdateTrayMenu(app.CurrentLanguage)
		}
	}
}
