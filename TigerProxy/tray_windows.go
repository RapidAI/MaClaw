//go:build windows

package main

import (
	"os"
	stdruntime "runtime"
	"sync"
	"time"

	"github.com/energye/systray"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var (
	trayVisibilityMu   sync.Mutex
	trayVisibilityFunc func(bool)
)

var UpdateTrayVisibility = func(v bool) {
	trayVisibilityMu.Lock()
	fn := trayVisibilityFunc
	trayVisibilityMu.Unlock()
	if fn != nil {
		fn(v)
	}
}

func setupTray(app *App, appOptions *options.App) {
	editMenu := menu.NewMenu()
	editMenu.Append(menu.EditMenu())
	appOptions.Menu = editMenu

	go func() {
		stdruntime.LockOSThread()
		systray.Run(func() {
			systray.SetIcon(trayIcon)
			systray.SetTitle("TigerProxy")
			systray.SetTooltip("TigerProxy")

			mShowHide := systray.AddMenuItem("隐藏", "显示/隐藏主界面")
			mQuit := systray.AddMenuItem("退出", "退出 TigerProxy")

			// visible is guarded by trayVisibilityMu to prevent data race
			// between tray goroutine (toggle) and main goroutine (UpdateTrayVisibility).
			visible := app.isShown()

			update := func() {
				// caller must hold trayVisibilityMu
				if visible {
					mShowHide.SetTitle("隐藏")
				} else {
					mShowHide.SetTitle("显示")
				}
			}
			trayVisibilityMu.Lock()
			trayVisibilityFunc = func(v bool) {
				visible = v
				update()
			}
			update()
			trayVisibilityMu.Unlock()

			toggle := func() {
				if app.ctx == nil {
					return
				}
				trayVisibilityMu.Lock()
				if visible {
					runtime.WindowHide(app.ctx)
					visible = false
				} else {
					runtime.WindowShow(app.ctx)
					runtime.WindowSetAlwaysOnTop(app.ctx, true)
					runtime.WindowSetAlwaysOnTop(app.ctx, false)
					visible = true
				}
				update()
				v := visible
				trayVisibilityMu.Unlock()
				// setShown calls UpdateTrayVisibility which re-acquires the mutex,
				// but since we already updated `visible` above, the func is a no-op
				// (it sets visible to the same value). We still call it to keep
				// app.shown in sync.
				app.mu.Lock()
				app.shown = v
				app.mu.Unlock()
			}

			systray.SetOnDClick(func(menu systray.IMenu) { go toggle() })
			mShowHide.Click(func() { go toggle() })
			mQuit.Click(func() {
				go func() {
					if app.ctx != nil {
						runtime.Quit(app.ctx)
						time.Sleep(500 * time.Millisecond)
					}
					systray.Quit()
				}()
			})
		}, func() { os.Exit(0) })
	}()
}
