//go:build darwin

package guiapp

import (
	"context"
	"os"
	"os/exec"
	"strconv"
)

// setWorkstationMode on macOS uses caffeinate -i (prevent idle sleep, allow display sleep)
// and disables the "ask for password after sleep/screen saver" setting to prevent lock.
func (a *App) setWorkstationMode(enabled bool, screenDimMin int) {
	a.powerStateMutex.Lock()
	defer a.powerStateMutex.Unlock()

	// Clean up previous state.
	if a.workstationCancel != nil {
		a.workstationCancel()
		a.workstationCancel = nil
	}

	if !enabled {
		// Restore lock-on-wake.
		_ = exec.Command("defaults", "write", "com.apple.screensaver", "askForPassword", "-int", "1").Run()
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.workstationCancel = cancel

	// caffeinate -i: prevent idle sleep but allow display sleep.
	cmd := exec.Command("caffeinate", "-i", "-w", strconv.Itoa(os.Getpid()))
	if err := cmd.Start(); err != nil {
		a.log("workstation-mode: failed to start caffeinate: " + err.Error())
		cancel()
		a.workstationCancel = nil
		return
	}

	// Disable "ask for password" after screen saver / display sleep.
	_ = exec.Command("defaults", "write", "com.apple.screensaver", "askForPassword", "-int", "0").Run()

	// Wait for cancel in background, then clean up caffeinate.
	go func() {
		<-ctx.Done()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}()
}
