//go:build !windows

package guiapp

// setupTray3 is a no-op on non-Windows wails3 builds for now: the darwin
// tray path (setupTrayNative + v2 AppMenu) needs the v3 menu migration and is
// scheduled with the macOS leg of Phase 1.
func setupTray3(app *App, args []string) {}
