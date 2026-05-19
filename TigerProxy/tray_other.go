//go:build !windows

package main

import (
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
)

var UpdateTrayVisibility = func(bool) {}

func setupTray(app *App, appOptions *options.App) {
	_ = app
	editMenu := menu.NewMenu()
	editMenu.Append(menu.EditMenu())
	appOptions.Menu = editMenu
}
