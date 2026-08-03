package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed frontend/dist
var assets embed.FS

//go:embed assets/maclaw.ico
var trayIcon []byte

// initLogging sets up file-based logging to ~/.tigerproxy/logs/.
// Both stderr and the file receive every application log line. Proxy request
// summaries are deliberately structural and never include credentials.
// Returns a closer function.
func initLogging() func() {
	dir, err := configDir()
	if err != nil {
		return func() {}
	}
	logsDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return func() {}
	}
	fileName := fmt.Sprintf("tigerproxy_%s.log", time.Now().Format("2006-01-02"))
	logPath := filepath.Join(logsDir, fileName)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return func() {}
	}
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	// Clean up log files older than 7 days in background.
	go cleanOldLogs(logsDir, 7)
	return func() { _ = f.Close() }
}

// cleanOldLogs removes .log files in dir that are older than maxDays.
func cleanOldLogs(dir string, maxDays int) {
	cutoff := time.Now().AddDate(0, 0, -maxDays)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".log" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}

func main() {
	closeLog := initLogging()
	defer closeLog()
	log.Printf("[tigerproxy] starting")

	app := NewApp()
	startHidden := hasStartHiddenArg(os.Args[1:])
	app.shown = !startHidden
	appOptions := &options.App{
		Title:                    "TigerProxy",
		Frameless:                true,
		StartHidden:              startHidden,
		Width:                    920,
		Height:                   786,
		MinWidth:                 780,
		MinHeight:                647,
		EnableDefaultContextMenu: true,
		BackgroundColour:         &options.RGBA{R: 246, G: 248, B: 251, A: 255},
		AssetServer:              &assetserver.Options{Assets: assets},
		OnStartup:                app.startup,
		OnShutdown:               app.shutdown,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "tigerproxy-lock",
			OnSecondInstanceLaunch: func(secondInstanceData options.SecondInstanceData) {
				_ = secondInstanceData
				go app.ShowMainWindow()
			},
		},
		Bind: []interface{}{app},
	}
	setupTray(app, appOptions)
	if err := wails.Run(appOptions); err != nil {
		log.Fatal(err)
	}
}

func hasStartHiddenArg(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "--hidden", "-hidden", "/hidden":
			return true
		}
	}
	return false
}
