package main

import (
	"embed"
	"log"
	"os"

	"clawmatemaker/internal/flash"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

// releaseBuild is set only by the protected release workflow. It keeps a
// developer build usable with local ESP-IDF tools while a shipped executable
// refuses to run an unverified PATH-provided esptool.
var releaseBuild = "false"

func main() {
	executable, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	flash.ConfigureSidecar(executable, releaseBuild == "true")
	app := NewApp()
	if err := wails.Run(&options.App{
		Title:     "ClawMate Maker",
		Width:     1180,
		Height:    780,
		MinWidth:  960,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind:       []interface{}{app},
	}); err != nil {
		log.Fatal(err)
	}
}
