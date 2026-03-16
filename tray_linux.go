//go:build linux
// +build linux

package main

import (
	"context"
	"time"

	"github.com/energye/systray"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func setupTray(app *App, appOptions *options.App) {
	appOptions.OnStartup = func(ctx context.Context) {
		app.startup(ctx)

		go func() {
			systray.Run(func() {
				systray.SetIcon(icon)
				systray.SetTitle("MaClaw")
				systray.SetTooltip("MaClaw Dashboard")

				mShow := systray.AddMenuItem("Show", "Show Main Window")
				systray.AddSeparator()
				mQuit := systray.AddMenuItem("Quit", "Quit Application")

				// Register update function
				UpdateTrayMenu = func(lang string) {
					t, ok := trayTranslations[lang]
					if !ok {
						t = trayTranslations["en"]
					}
					systray.SetTitle(t["title"])
					systray.SetTooltip(t["title"])
					mShow.SetTitle(t["show"])
					mQuit.SetTitle(t["quit"])
				}

				// Register config change listener
				OnConfigChanged = func(cfg AppConfig) {
					runtime.EventsEmit(app.ctx, "config-changed", cfg)
				}

				// Handle menu clicks
				mShow.Click(func() {
					go runtime.WindowShow(app.ctx)
				})

				mQuit.Click(func() {
					go func() {
						systray.Quit()
						runtime.Quit(app.ctx)
					}()
				})

				if app.CurrentLanguage != "" {
					go func() {
						time.Sleep(500 * time.Millisecond)
						UpdateTrayMenu(app.CurrentLanguage)
					}()
				}
			}, func() {
				// Cleanup logic on exit
			})
		}()
	}
}
