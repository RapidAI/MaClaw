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
		Title:             "iWokerCenter",
		Width:             1480,
		Height:            940,
		MinWidth:          1240,
		MinHeight:         780,
		EnableDefaultContextMenu: true,
		AssetServer: &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 239, G: 245, B: 255, A: 255},
		OnStartup:         app.startup,
		Bind:              []interface{}{app},
	})
	if err != nil {
		log.Fatal(err)
	}
}
