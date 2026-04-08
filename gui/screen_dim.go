package main

import (
	"context"
	"log"
	"time"
)

// updateScreenDimTimer starts or stops the screen-dim goroutine based on
// the power optimization state and the configured timeout.
// When enabled with timeout > 0, a background goroutine periodically checks
// user idle time and dims the display after the configured inactivity period.
// The display wakes automatically on any user input.
func (a *App) updateScreenDimTimer(powerEnabled bool, timeoutMin int) {
	lockStart := time.Now()
	a.powerStateMutex.Lock()
	lockWait := time.Since(lockStart)
	if lockWait > 50*time.Millisecond {
		log.Printf("[power] updateScreenDimTimer:lock_wait=%s enabled=%t timeout_min=%d", lockWait, powerEnabled, timeoutMin)
	}
	defer a.powerStateMutex.Unlock()

	// Stop any existing dim timer.
	if a.screenDimCancel != nil {
		a.screenDimCancel()
		a.screenDimCancel = nil
	}

	if !powerEnabled || timeoutMin <= 0 {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.screenDimCancel = cancel

	timeout := time.Duration(timeoutMin) * time.Minute

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		dimmed := false

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				idle := getIdleDuration()
				if idle >= timeout && !dimmed {
					dimDisplay()
					dimmed = true
					a.log("[screen-dim] display dimmed after idle " + idle.String())
				} else if idle < 10*time.Second && dimmed {
					// User activity detected — display wakes automatically,
					// just reset our state.
					dimmed = false
				}
			}
		}
	}()
}

// getIdleDuration returns the duration since the last user input event.
// Platform-specific implementations are in screen_dim_*.go files.
// Falls back to 0 on unsupported platforms (never dims).
// Implemented via platformGetIdleDuration variable set in platform files.
func getIdleDuration() time.Duration {
	if platformGetIdleDuration != nil {
		return platformGetIdleDuration()
	}
	return 0
}

// dimDisplay turns off the display to save power.
// Platform-specific implementations are in screen_dim_*.go files.
// Implemented via platformDimDisplay variable set in platform files.
func dimDisplay() {
	if platformDimDisplay != nil {
		platformDimDisplay()
	}
}

// Platform hooks — set by screen_dim_windows.go / screen_dim_darwin.go / etc.
var platformGetIdleDuration func() time.Duration
var platformDimDisplay func()
