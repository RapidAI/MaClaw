package main

// TigerProxy wails3 bootstrap: mirrors main2 (the v2 path) with the v3
// application objects. Register the App as a single service; startup/shutdown
// route into the existing hooks.

import (
	"context"
	"embed"
	"log"
	"os"

	wruntime "github.com/RapidAI/CodeClaw/guiapp/wruntime"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed frontend/dist
var frontendAssets embed.FS

//go:embed assets/maclaw.ico
var wails3TrayIcon []byte

func main() { main3() }

func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	a.startup(ctx)
	return nil
}

func (a *App) ServiceShutdown() {
	if a.ctx == nil {
		return
	}
	a.shutdown(a.ctx)
}

func main3() {
	closeLog := initLogging()
	defer closeLog()
	log.Printf("[codexproxy] starting (wails3)")

	app := NewApp()
	startHidden := hasStartHiddenArg(os.Args[1:])
	app.shown = !startHidden

	// Preload the tray icon for the reused systray loop.
	trayIcon = wails3TrayIcon

	v3app := application.New(application.Options{
		Name: "CodexProxy",
		Services: []application.Service{
			application.NewService(app),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(frontendAssets),
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "codexproxy-lock",
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				_ = data
				go app.ShowMainWindow()
			},
		},
	})
	wruntime.SetApplication(v3app)

	win := v3app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "CodexProxy",
		Name:             "main",
		Width:            1080,
		Height:           780,
		MinWidth:         900,
		MinHeight:        680,
		Frameless:        true,
		BackgroundColour: application.RGBA{Red: 244, Green: 247, Blue: 251, Alpha: 255},
	})
	wruntime.SetMainWindow(win)
	if startHidden {
		win.Hide()
	} else {
		win.Show()
	}

	setupTray(app)
	if err := v3app.Run(); err != nil {
		log.Fatal(err)
	}
}
