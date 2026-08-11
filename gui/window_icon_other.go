//go:build !windows

package main

// Windows applies the tray asset directly to the native window. Other
// platforms use their existing bundle/window icon configuration.
func setMainWindowIconFromTray() {}
