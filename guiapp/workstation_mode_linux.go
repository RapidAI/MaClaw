//go:build linux

package guiapp

import (
	"context"
	"os"
	"os/exec"
	"strconv"
)

// setWorkstationMode on Linux uses systemd-inhibit to prevent sleep
// and disables the screen lock via gsettings.
func (a *App) setWorkstationMode(enabled bool, screenDimMin int) {
	a.powerStateMutex.Lock()
	defer a.powerStateMutex.Unlock()

	// Clean up previous state.
	if a.workstationCancel != nil {
		a.workstationCancel()
		a.workstationCancel = nil
	}

	if !enabled {
		// Restore screen lock.
		_ = exec.Command("gsettings", "set", "org.gnome.desktop.screensaver", "lock-enabled", "true").Run()
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.workstationCancel = cancel

	// Inhibit sleep via systemd-inhibit.
	var cmd *exec.Cmd
	if _, err := exec.LookPath("systemd-inhibit"); err == nil {
		cmd = exec.Command(
			"systemd-inhibit",
			"--what=sleep:idle",
			"--why=Workstation mode active",
			"sh", "-c",
			"while kill -0 "+strconv.Itoa(os.Getpid())+" 2>/dev/null; do sleep 60; done",
		)
		if err := cmd.Start(); err != nil {
			a.log("workstation-mode: failed to start systemd-inhibit: " + err.Error())
		}
	} else {
		a.log("workstation-mode: systemd-inhibit not found, sleep inhibit unavailable")
	}

	// Disable screen lock via gsettings (GNOME).
	_ = exec.Command("gsettings", "set", "org.gnome.desktop.screensaver", "lock-enabled", "false").Run()

	// Wait for cancel in background, then clean up.
	go func() {
		<-ctx.Done()
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}()
}
