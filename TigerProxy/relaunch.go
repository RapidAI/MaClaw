package main

import (
	"fmt"
	"os"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) scheduleRelaunch() error {
	if os.Getenv("AICODER_SKIP_CODEXPROXY_RELAUNCH") == "1" {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}
	if err := startRelaunchProcess(exe); err != nil {
		return fmt.Errorf("schedule relaunch: %w", err)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		if a.ctx != nil {
			runtime.Quit(a.ctx)
		} else {
			os.Exit(0)
		}
	}()
	return nil
}
