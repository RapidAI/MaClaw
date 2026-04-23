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
		Title:                    "srvdemo",
		Width:                    1480,
		Height:                   980,
		MinWidth:                 1200,
		MinHeight:                760,
		EnableDefaultContextMenu: true,
		BackgroundColour:         &options.RGBA{R: 244, G: 246, B: 240, A: 255},
		AssetServer:              &assetserver.Options{Assets: assets},
		OnStartup:                app.startup,
		Bind:                     []interface{}{app},
	})
	if err != nil {
		log.Fatal(err)
	}
}
