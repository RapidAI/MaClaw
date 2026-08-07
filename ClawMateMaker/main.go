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

// assets is deliberately embedded from the built static surface. The desktop
// program never loads UI code from a network location, which keeps the local
// firmware-write authority behind the same reviewed application bundle.
//
//go:embed all:frontend/dist
var assets embed.FS

// releaseBuild is injected only by the protected release workflow. A shipped
// executable therefore refuses a PATH-provided esptool and accepts only its
// signed sidecar, while developer builds remain probe-only by default.
var releaseBuild = "false"

// buildVersion is display metadata injected alongside the release trust root.
// Keeping the development default explicit makes locally-built probe-only
// executables distinguishable from an official release in the UI and in
// exported diagnostics.
var buildVersion = "0.1.0-dev"

// These values are public release metadata, not private signing material.
// Official packaging injects them with -ldflags.
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
	err = wails.Run(&options.App{
		Title:            "ClawMate Maker",
		Width:            1180,
		Height:           820,
		MinWidth:         960,
		MinHeight:        680,
		DisableResize:    false,
		BackgroundColour: &options.RGBA{R: 244, G: 247, B: 251, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		OnBeforeClose:    app.PreventCloseWhileWriting,
		Bind:             []interface{}{app},
	})
	if err != nil {
		log.Fatal(err)
	}
}
