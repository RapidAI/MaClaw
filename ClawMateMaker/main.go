package main

import (
	"embed"
	"log"
	"os"

	"clawmatemaker/internal/flash"
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
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
	v3app := application.New(application.Options{
		Name: "ClawMate Maker",
		Services: []application.Service{
			application.NewService(app),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		// v2 OnBeforeClose semantics: block quit while a flash write is
		// active (the UI is notified via the app:close-blocked event).
		ShouldQuit: func() bool {
			return !app.PreventCloseWhileWriting(context.Background())
		},
	})
	wails3App = v3app
	win := v3app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "ClawMate Maker",
		Width:            1180,
		Height:           820,
		MinWidth:         960,
		MinHeight:        680,
		BackgroundColour: application.RGBA{Red: 244, Green: 247, Blue: 251, Alpha: 255},
	})
	_ = win
	if err := v3app.Run(); err != nil {
		log.Fatal(err)
	}
}
