//go:build !windows

package guiapp

// Windows applies the tray asset directly to the native window. Other
// platforms use their existing bundle/window icon configuration.
func setMainWindowIconFromTray() {}
