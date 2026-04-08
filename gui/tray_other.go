//go:build !darwin

package main

import "github.com/wailsapp/wails/v2/pkg/options"

func setupTray(app *App, appOptions *options.App) {
	_ = app
	_ = appOptions
}
