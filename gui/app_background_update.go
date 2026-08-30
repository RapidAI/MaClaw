package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

const (
	// Wait for startup network/UI settle before the first background check.
	backgroundUpdateCheckInitialDelay = 60 * time.Second
	// Minimum time between successful remote manifest fetches.
	backgroundUpdateCheckInterval = 30 * time.Minute
	// Shorter backoff after a failed fetch so transient network issues recover
	// without waiting a full success interval.
	backgroundUpdateCheckFailureBackoff = 5 * time.Minute
	// How often to re-read check_update_on_startup so mid-session toggles
	// take effect without a restart (network still gated by the backoffs above).
	backgroundUpdateConfigPollInterval = 1 * time.Minute
)

// startBackgroundUpdateChecks launches the non-blocking loop that notifies the
// frontend when a newer application release is available.
//
// The loop always starts so users can enable check_update_on_startup mid-session
// without restarting. Each attempt re-reads config and no-ops when disabled.
// Idempotent: safe if domReady / callers fire more than once.
func (a *App) startBackgroundUpdateChecks() {
	if a == nil || a.ctx == nil {
		// Require the Wails lifecycle context so the loop can stop on shutdown.
		// Falling back to context.Background() would leak a goroutine.
		return
	}
	a.backgroundUpdateOnce.Do(func() {
		go a.backgroundUpdateCheckLoop(
			a.ctx,
			backgroundUpdateCheckInitialDelay,
			backgroundUpdateConfigPollInterval,
			backgroundUpdateCheckInterval,
			backgroundUpdateCheckFailureBackoff,
		)
	})
}

func (a *App) backgroundUpdateCheckLoop(
	ctx context.Context,
	initialDelay, pollInterval, successInterval, failureBackoff time.Duration,
) {
	if a == nil || ctx == nil {
		return
	}
	if initialDelay < 0 {
		initialDelay = 0
	}
	if pollInterval <= 0 {
		pollInterval = backgroundUpdateConfigPollInterval
	}
	if successInterval <= 0 {
		successInterval = backgroundUpdateCheckInterval
	}
	if failureBackoff <= 0 {
		failureBackoff = backgroundUpdateCheckFailureBackoff
	}

	state := &backgroundUpdateCheckState{}
	runBackgroundUpdateCheckLoop(ctx, initialDelay, pollInterval, func() {
		// Isolate panics so a single bad check cannot kill the loop for the
		// rest of the process lifetime.
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[update-check] background check panicked: %v", r)
			}
		}()
		a.performBackgroundUpdateCheck(ctx, state, successInterval, failureBackoff)
	})
}

// backgroundUpdateCheckState holds process-local dedupe / schedule for the loop.
type backgroundUpdateCheckState struct {
	lastNotifiedVersion string
	// nextNetworkNotBefore is the earliest time a remote fetch may run again.
	// Zero means "allowed immediately".
	nextNetworkNotBefore time.Time
}

// runBackgroundUpdateCheckLoop is the pure scheduling skeleton (testable without App).
func runBackgroundUpdateCheckLoop(
	ctx context.Context,
	initialDelay, pollInterval time.Duration,
	tick func(),
) {
	if ctx == nil {
		return
	}

	if initialDelay > 0 {
		timer := time.NewTimer(initialDelay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			stopTimer(timer)
			return
		}
	} else if ctx.Err() != nil {
		return
	}

	if tick != nil {
		tick()
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			// Exit promptly if shutdown raced with the tick delivery.
			if ctx.Err() != nil {
				return
			}
			if tick != nil {
				tick()
			}
		case <-ctx.Done():
			return
		}
	}
}

func stopTimer(timer *time.Timer) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

// shouldFetchAppUpdateNow reports whether a remote manifest fetch is allowed.
func shouldFetchAppUpdateNow(nextNetworkNotBefore, now time.Time) bool {
	if nextNetworkNotBefore.IsZero() {
		return true
	}
	return !now.Before(nextNetworkNotBefore)
}

// nextAppUpdateFetchAt returns the earliest time the next remote fetch may run.
func nextAppUpdateFetchAt(now time.Time, success bool, successInterval, failureBackoff time.Duration) time.Time {
	if success {
		if successInterval <= 0 {
			return now
		}
		return now.Add(successInterval)
	}
	if failureBackoff <= 0 {
		return now
	}
	return now.Add(failureBackoff)
}

// performBackgroundUpdateCheck loads config, optionally fetches the latest
// release, and emits EventAppUpdateAvailable when a new version is found.
func (a *App) performBackgroundUpdateCheck(
	ctx context.Context,
	state *backgroundUpdateCheckState,
	successInterval, failureBackoff time.Duration,
) {
	if a == nil || state == nil {
		return
	}
	if ctx != nil && ctx.Err() != nil {
		return
	}

	cfg, err := a.LoadConfig()
	// Fail open when config cannot be read (prefer checking over never checking).
	if err == nil && !cfg.CheckUpdateOnStartup {
		return
	}

	now := time.Now()
	if !shouldFetchAppUpdateNow(state.nextNetworkNotBefore, now) {
		return
	}
	currentVersion, hasVersion := linkedReleaseVersion()
	// A development binary has no linker-injected release version.  Do not
	// compare a real remote release against the placeholder "dev" value: that
	// would make every release look newer and produce a false notification.
	if !hasVersion {
		a.log("[update-check] skipping background check: local release version is unavailable")
		return
	}
	// Provisional failure backoff before the network call so a panic mid-flight
	// still throttles retries (recover in the tick wrapper keeps the loop alive).
	state.nextNetworkNotBefore = nextAppUpdateFetchAt(now, false, successInterval, failureBackoff)

	var result UpdateResult
	if cfg.PreferBetaChannel {
		result, err = a.CheckUpdateBeta(currentVersion)
	} else {
		result, err = a.CheckUpdate(currentVersion)
	}

	// Finalize schedule: success uses the longer interval; errors keep failure backoff.
	success := err == nil
	state.nextNetworkNotBefore = nextAppUpdateFetchAt(now, success, successInterval, failureBackoff)

	if err != nil {
		a.log(fmt.Sprintf("[update-check] background check failed: %v", err))
		return
	}
	// Do not surface UI notices after shutdown has begun.
	if ctx != nil && ctx.Err() != nil {
		return
	}

	version, ok := appUpdateNotifyVersion(result, state.lastNotifiedVersion)
	if !ok {
		return
	}
	state.lastNotifiedVersion = version
	// Normalize before emit so frontend dismiss keys match process-local dedupe.
	result.LatestVersion = version
	a.emitEvent(EventAppUpdateAvailable, result)
}

// appUpdateNotifyVersion returns the normalized version to record when a check
// result should surface a UI notice. Same version is only notified once per
// process lifetime (frontend also persists a per-version dismiss in localStorage).
func appUpdateNotifyVersion(result UpdateResult, lastNotifiedVersion string) (string, bool) {
	if !result.HasUpdate {
		return "", false
	}
	version := strings.TrimSpace(result.LatestVersion)
	if version == "" {
		return "", false
	}
	if version == strings.TrimSpace(lastNotifiedVersion) {
		return "", false
	}
	return version, true
}
