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

func main() {
	app := NewApp()
	err := wails.Run(&options.App{
		Title:             "DiWorker",
		Width:             1440,
		Height:            920,
		MinWidth:          1200,
		MinHeight:         760,
		EnableDefaultContextMenu: true,
		AssetServer: &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 245, G: 248, B: 252, A: 255},
		OnStartup:         app.startup,
		Bind:              []interface{}{app},
	})
	if err != nil {
		log.Fatal(err)
	}
}
