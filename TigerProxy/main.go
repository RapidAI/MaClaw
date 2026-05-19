package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed frontend/dist
var assets embed.FS

//go:embed assets/maclaw.ico
var trayIcon []byte

func main() {
	app := NewApp()
	appOptions := &options.App{
		Title:                    "TigerProxy",
		Frameless:                true,
		Width:                    920,
		Height:                   680,
		MinWidth:                 780,
		MinHeight:                560,
		EnableDefaultContextMenu: true,
		BackgroundColour:         &options.RGBA{R: 246, G: 248, B: 251, A: 255},
		AssetServer:              &assetserver.Options{Assets: assets},
		OnStartup:                app.startup,
		OnShutdown:               app.shutdown,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "tigerproxy-lock",
			OnSecondInstanceLaunch: func(secondInstanceData options.SecondInstanceData) {
				_ = secondInstanceData
				go app.ShowMainWindow()
			},
		},
		Bind: []interface{}{app},
	}
	setupTray(app, appOptions)
	if err := wails.Run(appOptions); err != nil {
		log.Fatal(err)
	}
}
