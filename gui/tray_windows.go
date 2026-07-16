//go:build windows

package main

import (
	"os"
	stdruntime "runtime"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
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
			// Computer Use operator controls (desktop automation safety).
			mCU := systray.AddMenuItem("Computer Use", "Desktop Computer Use")
			mCUStatus := mCU.AddSubMenuItem("Status: idle", "Computer Use status")
			mCUStatus.Disable()
			mCUPause := mCU.AddSubMenuItem("Pause desktop actions", "Pause click/type")
			mCUResume := mCU.AddSubMenuItem("Resume desktop actions", "Resume after pause")
			mCUStop := mCU.AddSubMenuItem("Stop desktop control", "Hard stop + cancel agent")
			mCUReset := mCU.AddSubMenuItem("Reset control state", "Clear stop/pause")
			systray.AddSeparator()
			mQuit := systray.AddMenuItem("Quit", "Quit Application")

			isVisible := !app.IsAutoStart

			refreshCUTray := func() {
				menuTitle, statusLabel, pause, resume, stop, reset, pe, re, se, xe := computerUseTrayLabels(app)
				mCU.SetTitle(menuTitle)
				mCUStatus.SetTitle(statusLabel)
				mCUPause.SetTitle(pause)
				mCUResume.SetTitle(resume)
				mCUStop.SetTitle(stop)
				mCUReset.SetTitle(reset)
				if pe {
					mCUPause.Enable()
				} else {
					mCUPause.Disable()
				}
				if re {
					mCUResume.Enable()
				} else {
					mCUResume.Disable()
				}
				if se {
					mCUStop.Enable()
				} else {
					mCUStop.Disable()
				}
				if xe {
					mCUReset.Enable()
				} else {
					mCUReset.Disable()
				}
			}

			UpdateComputerUseTray = refreshCUTray

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
				refreshCUTray()
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

			mCUPause.Click(func() {
				go func() {
					_ = app.ComputerUsePause()
					refreshCUTray()
				}()
			})
			mCUResume.Click(func() {
				go func() {
					_ = app.ComputerUseResume()
					refreshCUTray()
				}()
			})
			mCUStop.Click(func() {
				go func() {
					_ = app.ComputerUseStop()
					refreshCUTray()
				}()
			})
			mCUReset.Click(func() {
				go func() {
					_ = app.ComputerUseReset()
					refreshCUTray()
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
			} else {
				refreshCUTray()
			}
		}, func() {
			os.Exit(0)
		})
	}()
}
