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
		Title:                    "MaClaw Third API Demo",
		Width:                    1120,
		Height:                   820,
		MinWidth:                 900,
		MinHeight:                640,
		EnableDefaultContextMenu: true,
		BackgroundColour:         &options.RGBA{R: 238, G: 243, B: 248, A: 255},
		AssetServer:              &assetserver.Options{Assets: assets},
		OnStartup:                app.startup,
		Bind:                     []interface{}{app},
	})
	if err != nil {
		log.Fatal(err)
	}
}
