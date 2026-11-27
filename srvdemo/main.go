package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed frontend/dist
var assets embed.FS

func main() {
	app := NewApp()
	v3app := application.New(application.Options{
		Name: "srvdemo",
		Services: []application.Service{
			application.NewService(app),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
	})
	win := v3app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "srvdemo",
		Width:            1480,
		Height:           980,
		MinWidth:         1200,
		MinHeight:        760,
		BackgroundColour: application.RGBA{Red: 244, Green: 246, Blue: 240, Alpha: 255},
	})
	_ = win
	if err := v3app.Run(); err != nil {
		log.Fatal(err)
	}
}
