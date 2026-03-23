//go:build windows

package main

import (
	"context"
	stdruntime "runtime"

	"github.com/energye/systray"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func setupTray(app *App, appOptions *options.App) {
	appOptions.OnStartup = func(ctx context.Context) {
		app.startup(ctx)

		go func() {
			// Lock the OS thread for the systray message loop on Windows
			stdruntime.LockOSThread()

			systray.Run(func() {
				systray.SetIcon(icon)
				systray.SetTitle("MaClaw")
				systray.SetTooltip("MaClaw Dashboard")
				systray.SetOnDClick(func(menu systray.IMenu) {
					go func() {
						runtime.WindowShow(app.ctx)
						runtime.WindowSetAlwaysOnTop(app.ctx, true)
						runtime.WindowSetAlwaysOnTop(app.ctx, false)
					}()
				})

				mShow := systray.AddMenuItem("Show", "Show Main Window")
				systray.AddSeparator()
				mQuit := systray.AddMenuItem("Quit", "Quit Application")

				var isVisible bool = true

				// Register update function
				UpdateTrayMenu = func(lang string) {
					t, ok := trayTranslations[lang]
					if !ok {
						t = trayTranslations["en"]
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

				// Register config change listener
				OnConfigChanged = func(cfg AppConfig) {
					runtime.EventsEmit(app.ctx, "config-changed", cfg)
				}

				// System notification (not available in upstream energye/systray)
				ShowNotification = func(title, message string, iconFlag uint32) {
					_, _, _ = title, message, iconFlag
				}

				// Flash + beep (not available in upstream energye/systray)
				FlashAndBeep = func() {
				}

				// Handle menu clicks
				mShow.Click(func() {
					go func() {
						if isVisible {
							runtime.WindowHide(app.ctx)
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
						systray.Quit()
						runtime.Quit(app.ctx)
					}()
				})

				if app.CurrentLanguage != "" {
					UpdateTrayMenu(app.CurrentLanguage)
				}
			}, func() {
				// Cleanup logic on exit
			})
		}()
	}
}
