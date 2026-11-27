//go:build windows

package guiapp

// wails3 tray: the vendored systray fork is wails-version-independent, so the
// v2 startSystray event loop is reused as-is. Only the EditMenu attachment
// (v2 appOptions.Menu, needed for WebView2 Ctrl+A/C/V/X) has no v3 equivalent
// wired yet — tracked as a known gap in main_wails3.go.

// setupTray3 starts the systray after application.New has accepted the
// single-instance lock (the v2 stale-icon constraint: never start the tray
// in an instance that is about to os.Exit through the lock rejection).
func setupTray3(app *App, args []string) {
	// A --quit launch never needs a tray (same rationale as v2 setupTray).
	if secondInstanceRequestsQuit(args) {
		return
	}
	launch := classifyDesktopLaunch(args)
	if launch.Mode != desktopLaunchFileCompanion {
		startSystray(app)
	} else {
		bootLog("file-companion skip tray")
	}
}
