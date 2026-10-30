//go:build windows

package guiapp

import (
	"context"
	"log"
	"os"
	stdruntime "runtime"
	"sync"
	"sync/atomic"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/energye/systray"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func setupTray(app *App, appOptions *options.App) {
	// Provide a standard Edit menu so that Ctrl+A / Ctrl+C / Ctrl+V / Ctrl+X
	// work correctly inside WebView2 input fields.  Without this, Ctrl+A in a
	// modal input can cause the webview to select all page content and
	// inadvertently dismiss the dialog.
	editMenu := menu.NewMenu()
	editMenu.Append(menu.EditMenu())
	appOptions.Menu = editMenu

	// A --quit launch never needs a tray: either another instance receives the
	// handoff, or startup exits immediately. Skipping the tray start avoids a
	// notification-icon flash on the way out.
	if secondInstanceRequestsQuit(os.Args[1:]) {
		return
	}

	// Start the systray from OnStartup rather than before wails.Run. Wails
	// performs its single-instance check inside wails.Run, before OnStartup,
	// and a rejected second instance exits straight through os.Exit. Starting
	// the tray earlier therefore created a notification icon that was never
	// removed (os.Exit skips NIM_DELETE on the icon), leaving a stale tray icon
	// whose menu no longer belongs to any live process. Clicking "Quit" there
	// does nothing and the real instance keeps running.
	origStartup := appOptions.OnStartup
	appOptions.OnStartup = func(ctx context.Context) {
		startSystray(app)
		if origStartup != nil {
			origStartup(ctx)
		}
	}
}

// trayStartOnce makes the tray start idempotent even if a startup hook is
// invoked more than once.
var trayStartOnce sync.Once

// startSystray runs the tray event loop. It is only reached by the primary
// instance, after Wails accepted the single-instance lock.
func startSystray(app *App) {
	trayStartOnce.Do(func() {
		go func() {
			// Lock the OS thread for the systray message loop on Windows.
			stdruntime.LockOSThread()
			// Windows 10+ turns a tray balloon into a toast and drops it unless
			// this process has an explicit AppUserModelID. Set it before the icon exists.
			id := brand.Current().ID
			if id == "" {
				id = "maclaw"
			}
			systray.SetAppUserModelID(id + ".Desktop")

			systray.Run(func() {
				// Shell_NotifyIcon(NIM_DELETE) runs on the exit goroutine, in the
				// same step as process termination. systray.Quit only posts
				// WM_CLOSE; the window procedure would drop the icon and
				// return while the process kept running.
				appExit.setTrayQuit(systray.RemoveIcon)
				systray.OnSessionEnd = func() {
					log.Printf("[exit-lifecycle] Windows session ending; terminating")
					appExit.exitNow()
				}

				systray.SetIcon(icon)
				systray.SetTitle(brand.Current().DisplayName)
				systray.SetTooltip(brand.Current().TrayTooltip)

				var isVisible atomic.Bool
				isVisible.Store(!app.IsAutoStart)

				// Log every menu open: when a "Quit does nothing" report arrives,
				// this distinguishes "the click never reached this process"
				// (stale icon) from "the menu opened but the handler was skipped".
				systray.SetOnRClick(func(menu systray.IMenu) {
					log.Printf("[exit-lifecycle] tray menu opened pid=%d", os.Getpid())
					if menu != nil {
						_ = menu.ShowMenu()
					}
				})

				systray.SetOnDClick(func(menu systray.IMenu) {
					_ = menu
					go func() {
						if app.ctx == nil {
							return
						}
						runtime.WindowShow(app.ctx)
						runtime.WindowSetAlwaysOnTop(app.ctx, true)
						runtime.WindowSetAlwaysOnTop(app.ctx, false)
						isVisible.Store(true)
						UpdateTrayMenu(app.CurrentLanguage)
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
					t, ok := tr[string(normalizeAppLanguageKind(lang))]
					if !ok {
						t = tr["en"]
					}
					systray.SetTitle(t["title"])
					systray.SetTooltip(t["title"])
					if isVisible.Load() {
						mShow.SetTitle(t["hide"])
					} else {
						mShow.SetTitle(t["show"])
					}
					mQuit.SetTitle(t["quit"])
					refreshCUTray()
				}

				UpdateTrayVisibility = func(visible bool) {
					isVisible.Store(visible)
					UpdateTrayMenu(app.CurrentLanguage)
				}

				IsMainWindowVisible = func() bool {
					return isVisible.Load()
				}

				OnConfigChanged = func(cfg corelib.AppConfig) {
					if app.ctx == nil {
						return
					}
					app.emitEvent("config-changed", cfg)
				}

				ShowNotification = func(title, message string, iconFlag uint32) {
					if err := systray.ShowBalloonNotification(title, message, iconFlag); err != nil {
						log.Printf("[tray] balloon: %v", err)
					}
				}

				FlashAndBeep = func() {
					systray.FlashAndBeep()
				}

				mShow.Click(func() {
					go func() {
						if app.ctx == nil {
							return
						}
						if isVisible.Load() {
							// Use app.WindowHide() so the desktop pet remains available from the title-bar hide path.
							app.WindowHide()
							isVisible.Store(false)
						} else {
							runtime.WindowShow(app.ctx)
							runtime.WindowSetAlwaysOnTop(app.ctx, true)
							runtime.WindowSetAlwaysOnTop(app.ctx, false)
							isVisible.Store(true)
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
					log.Printf("[exit-lifecycle] tray quit menu item selected")
					// Stay off the tray message-loop thread. The icon is removed
					// in the same step that terminates the process, after
					// shutdown or the short deadline — not from this click.
					go requestAppQuit(app.ctx)
				})

				if app.CurrentLanguage != "" {
					UpdateTrayMenu(app.CurrentLanguage)
				} else {
					refreshCUTray()
				}
				// Only after every item and handler is wired: this line means a
				// tray click will reach the app, which is what distinguishes
				// "quit did nothing" reports from a stale icon left by an older
				// process.
				log.Printf("[exit-lifecycle] tray ready pid=%d", os.Getpid())
			}, func() {
				log.Printf("[exit-lifecycle] tray event loop stopped")
				// The icon is already gone. Do not terminate on this stack:
				// the callback runs inside the tray window procedure, and
				// TerminateProcess/ExitProcess from there returns without
				// killing the process while holding the single-exit gate.
				// finishDesktopExit and the quit watchdog terminate from a
				// normal goroutine. Logoff uses OnSessionEnd instead.
			})
		}()
	})
}
