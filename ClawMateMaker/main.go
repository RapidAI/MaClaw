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

// Release metadata is injected by the protected workflow. It is intentionally
// separate from the firmware signing key: it lets support staff distinguish a
// development build (which trusts no public releases) from a signed release
// binary without exposing any signing material.
var releaseKeyID = "clawmate-release-v1"
var releasePublicKeyBase64 = ""

func main() {
	executable, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	flash.ConfigureSidecar(executable, releaseBuild == "true")
	flash.ConfigureSidecarTrust(releaseKeyID, releasePublicKeyBase64)
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
