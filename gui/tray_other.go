//go:build !darwin && !windows

package main

import (
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
)

func setupTray(app *App, appOptions *options.App) {
	_ = app

	// Provide a standard Edit menu so that Ctrl+A / Ctrl+C / Ctrl+V / Ctrl+X
	// work correctly inside WebView input fields.
	editMenu := menu.NewMenu()
	editMenu.Append(menu.EditMenu())
	appOptions.Menu = editMenu
}
