package main

import (
	"fmt"
	"os"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// relaunchGrace is how long the process stays alive after a relaunch is
// scheduled. The login call that triggered it is still unwinding — it has to
// finish persisting settings and return its result to the frontend — so quitting
// sooner tears the window down mid-request and the user sees the app vanish at
// the last step instead of the confirmed result.
const relaunchGrace = 1500 * time.Millisecond

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
		time.Sleep(relaunchGrace)
		if a.ctx != nil {
			runtime.Quit(a.ctx)
		} else {
			os.Exit(0)
		}
	}()
	return nil
}
