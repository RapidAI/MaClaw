//go:build windows

package main

import (
	"github.com/RapidAI/CodeClaw/corelib"
	"os"
	stdruntime "runtime"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/energye/systray"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func setupTray(app *App, appOptions *options.App) {
	_ = appOptions

	// Provide a standard Edit menu so that Ctrl+A / Ctrl+C / Ctrl+V / Ctrl+X
	// work correctly inside WebView2 input fields.  Without this, Ctrl+A in a
	// modal input can cause the webview to select all page content and
	// inadvertently dismiss the dialog.
	editMenu := menu.NewMenu()
	editMenu.Append(menu.EditMenu())
	appOptions.Menu = editMenu

	// Start the systray immediately (before wails.Run) so the tray icon
	// appears as soon as the process launches, instead of waiting for the
	// Wails WebView to finish initialising.
	go func() {
		// Lock the OS thread for the systray message loop on Windows.
		stdruntime.LockOSThread()

		systray.Run(func() {
			// Wire quitSystray so FloatingAssistantManager.QuitApp can
			// terminate the systray event loop (same as the tray quit handler).
			quitSystray = systray.Quit

			systray.SetIcon(icon)
			systray.SetTitle(brand.Current().DisplayName)
			systray.SetTooltip(brand.Current().TrayTooltip)
			systray.SetOnDClick(func(menu systray.IMenu) {
				_ = menu
				go func() {
					if app.ctx == nil {
						return
					}
					runtime.WindowShow(app.ctx)
					runtime.WindowSetAlwaysOnTop(app.ctx, true)
					runtime.WindowSetAlwaysOnTop(app.ctx, false)
				}()
			})

			mShow := systray.AddMenuItem("Show", "Show Main Window")
			systray.AddSeparator()
			mQuit := systray.AddMenuItem("Quit", "Quit Application")

			isVisible := !app.IsAutoStart

			UpdateTrayMenu = func(lang string) {
				tr := trayTranslations()
				t, ok := tr[lang]
				if !ok {
					t = tr["en"]
				}
				systray.SetTitle(t["title"])
				systray.SetTooltip(t["title"])
				if isVisible {
					mShow.SetTitle(t["hide"])
				} else {
					mShow.SetTitle(t["show"])
				}
				mQuit.SetTitle(t["quit"])
			}

			UpdateTrayVisibility = func(visible bool) {
				isVisible = visible
				UpdateTrayMenu(app.CurrentLanguage)
			}

			OnConfigChanged = func(cfg corelib.AppConfig) {
				if app.ctx == nil {
					return
				}
				app.emitEvent("config-changed", cfg)
			}

			ShowNotification = func(title, message string, iconFlag uint32) {
				_ = systray.ShowBalloonNotification(title, message, iconFlag)
			}

			FlashAndBeep = func() {
				systray.FlashAndBeep()
			}

			mShow.Click(func() {
				go func() {
					if app.ctx == nil {
						return
					}
					if isVisible {
						// Use app.WindowHide() so the desktop pet remains available from the title-bar hide path.
						app.WindowHide()
						isVisible = false
					} else {
						runtime.WindowShow(app.ctx)
						runtime.WindowSetAlwaysOnTop(app.ctx, true)
						runtime.WindowSetAlwaysOnTop(app.ctx, false)
						isVisible = true
					}
					UpdateTrayMenu(app.CurrentLanguage)
				}()
			})

			mQuit.Click(func() {
				go func() {
					if app.ctx == nil {
						os.Exit(0)
						return
					}
					runtime.Quit(app.ctx)
					time.Sleep(500 * time.Millisecond)
					systray.Quit()
				}()
			})

			if app.CurrentLanguage != "" {
				UpdateTrayMenu(app.CurrentLanguage)
			}
		}, func() {
			os.Exit(0)
		})
	}()
}
