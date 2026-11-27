package guiapp

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
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
		go a.startDeferredUpdateInstalls()
		// Version-qualified installer files accumulate in the downloads folder
		// across versions; drop the ones no live decision can still use.
		go a.cleanupStaleVersionedInstallers()
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
	noticeMu            sync.Mutex
	lastNotifiedVersion string
	// nextNetworkNotBefore is the earliest time a remote fetch may run again.
	// Zero means "allowed immediately".
	scheduleMu           sync.Mutex
	nextNetworkNotBefore time.Time

	silentMu               sync.Mutex
	silentInFlight         bool
	silentTerminalVersions map[string]bool
	silentLatestVersions   map[string]string
}

func (s *backgroundUpdateCheckState) markUpdateNotified(version string) bool {
	version = strings.TrimSpace(version)
	if s == nil || version == "" {
		return false
	}
	s.noticeMu.Lock()
	defer s.noticeMu.Unlock()
	if s.lastNotifiedVersion == version {
		return false
	}
	s.lastNotifiedVersion = version
	return true
}

func (s *backgroundUpdateCheckState) networkFetchNotBefore() time.Time {
	if s == nil {
		return time.Time{}
	}
	s.scheduleMu.Lock()
	defer s.scheduleMu.Unlock()
	return s.nextNetworkNotBefore
}

func (s *backgroundUpdateCheckState) setNetworkFetchNotBefore(at time.Time) {
	if s == nil {
		return
	}
	s.scheduleMu.Lock()
	s.nextNetworkNotBefore = at
	s.scheduleMu.Unlock()
}

func (s *backgroundUpdateCheckState) accelerateNetworkCheck(at time.Time) {
	if s == nil {
		return
	}
	s.scheduleMu.Lock()
	if s.nextNetworkNotBefore.IsZero() || at.Before(s.nextNetworkNotBefore) {
		s.nextNetworkNotBefore = at
	}
	s.scheduleMu.Unlock()
}

func updateChannelKey(channel string) string {
	channel = strings.TrimSpace(strings.ToLower(channel))
	if channel == "" {
		return "stable"
	}
	return channel
}

func (s *backgroundUpdateCheckState) noteSilentUpdateVersion(version, channel string) {
	version = strings.TrimSpace(version)
	if s == nil || version == "" {
		return
	}
	channel = updateChannelKey(channel)
	s.silentMu.Lock()
	defer s.silentMu.Unlock()
	if s.silentLatestVersions == nil {
		s.silentLatestVersions = make(map[string]string)
	}
	latest := s.silentLatestVersions[channel]
	if latest == "" || compareVersions(version, latest) > 0 {
		s.silentLatestVersions[channel] = version
	}
}

func (s *backgroundUpdateCheckState) silentUpdateVersionSuperseded(version, channel string) bool {
	version = strings.TrimSpace(version)
	if s == nil || version == "" {
		return false
	}
	channel = updateChannelKey(channel)
	s.silentMu.Lock()
	latest := s.silentLatestVersions[channel]
	s.silentMu.Unlock()
	return latest != "" && compareVersions(latest, version) > 0
}

func (s *backgroundUpdateCheckState) beginSilentUpdate(version string) bool {
	version = updateVersionKey(version)
	if s == nil || version == "" {
		return false
	}
	s.silentMu.Lock()
	defer s.silentMu.Unlock()
	if s.silentInFlight || s.silentTerminalVersions[version] {
		return false
	}
	s.silentInFlight = true
	return true
}

func (s *backgroundUpdateCheckState) finishSilentUpdate(version string, terminal bool) {
	version = updateVersionKey(version)
	if s == nil || version == "" {
		return
	}
	s.silentMu.Lock()
	defer s.silentMu.Unlock()
	s.silentInFlight = false
	if terminal {
		if s.silentTerminalVersions == nil {
			s.silentTerminalVersions = make(map[string]bool)
		}
		s.silentTerminalVersions[version] = true
	}
}

type silentUpdateOutcome struct {
	terminal bool
	err      error
}

var silentUpdateDownloadHook func(a *App, url, fileName, expectedSHA256 string) (string, error)
var silentUpdateInstallHook func(a *App, installerPath, targetVersion string) error

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

	cfg, configErr := a.LoadConfig()
	// Fail open when config cannot be read (prefer checking over never checking).
	if configErr == nil && !cfg.CheckUpdateOnStartup {
		return
	}

	now := time.Now()
	if !shouldFetchAppUpdateNow(state.networkFetchNotBefore(), now) {
		return
	}
	currentVersion, hasVersion := linkedReleaseVersion()
	// A development binary has no release version injected via cmd/maclaw-gui (-X main.version) and passed to guiapp.Main.  Do not
	// compare a real remote release against the placeholder "dev" value: that
	// would make every release look newer and produce a false notification.
	if !hasVersion {
		a.log("[update-check] skipping background check: local release version is unavailable")
		return
	}
	// Provisional failure backoff before the network call so a panic mid-flight
	// still throttles retries (recover in the tick wrapper keeps the loop alive).
	state.setNetworkFetchNotBefore(nextAppUpdateFetchAt(now, false, successInterval, failureBackoff))

	var result UpdateResult
	var checkErr error
	if cfg.PreferBetaChannel {
		result, checkErr = a.CheckUpdateBeta(currentVersion)
	} else {
		result, checkErr = a.CheckUpdate(currentVersion)
	}

	// Finalize schedule: success uses the longer interval; errors keep failure backoff.
	success := checkErr == nil
	state.setNetworkFetchNotBefore(nextAppUpdateFetchAt(now, success, successInterval, failureBackoff))

	if checkErr != nil {
		a.log(fmt.Sprintf("[update-check] background check failed: %v", checkErr))
		return
	}
	// Do not surface UI notices after shutdown has begun.
	if ctx != nil && ctx.Err() != nil {
		return
	}

	silentEnabled := configErr == nil && cfg.SilentUpdateEnabled
	a.handleBackgroundUpdateResult(ctx, state, result, silentEnabled, failureBackoff)
}

func (a *App) handleBackgroundUpdateResult(ctx context.Context, state *backgroundUpdateCheckState, result UpdateResult, silentEnabled bool, retryBackoffs ...time.Duration) {
	version, ok := appUpdateVersion(result)
	if !ok {
		return
	}
	result.LatestVersion = version
	// Even an ignored newer release supersedes an older in-flight download:
	// installing a deliberately skipped obsolete build helps nobody.
	state.noteSilentUpdateVersion(version, result.Channel)
	if a.isUpdateVersionIgnored(version) {
		return
	}
	// A deferred or next-startup install for this same version already
	// schedules its own check-download-install at the chosen time. Treating
	// it like a fresh discovery here would re-download the same installer
	// (versioned file names make the bytes identical) and reopen the dialog
	// over a decision the user already made — most visibly after a restart,
	// when the durable record is the only thing remembering that choice.
	if record, ok := a.readDeferredUpdateInstall(); ok && sameUpdateVersion(record.TargetVersion, version) {
		return
	}
	canSilentDownload := silentEnabled && !result.DownloadUnavailable && strings.TrimSpace(result.DownloadUrl) != ""
	if canSilentDownload {
		if !state.beginSilentUpdate(version) {
			return
		}
		go func() {
			outcome := a.runSilentUpdateSafely(ctx, state, result)
			state.finishSilentUpdate(version, outcome.terminal)
			if outcome.err != nil {
				a.log(fmt.Sprintf("[update-check] silent update %s failed: %v", version, outcome.err))
				if !outcome.terminal {
					retryBackoff := backgroundUpdateCheckFailureBackoff
					if len(retryBackoffs) > 0 && retryBackoffs[0] > 0 {
						retryBackoff = retryBackoffs[0]
					}
					state.accelerateNetworkCheck(time.Now().Add(retryBackoff))
				}
			}
		}()
		return
	}
	if !state.markUpdateNotified(version) {
		return
	}
	// Normalize before emit so frontend dismiss keys match process-local dedupe.
	a.emitEvent(EventAppUpdateAvailable, result)
}

func (a *App) runSilentUpdateSafely(ctx context.Context, state *backgroundUpdateCheckState, result UpdateResult) (outcome silentUpdateOutcome) {
	defer func() {
		if r := recover(); r != nil {
			outcome = silentUpdateOutcome{err: fmt.Errorf("silent update panicked: %v", r)}
		}
	}()
	return a.runSilentUpdate(ctx, state, result)
}

func (a *App) runSilentUpdate(ctx context.Context, state *backgroundUpdateCheckState, result UpdateResult) silentUpdateOutcome {
	if ctx != nil && ctx.Err() != nil {
		return silentUpdateOutcome{err: ctx.Err()}
	}
	// The destination is qualified with the target version: a fixed shared
	// name let this silent download truncate the bytes of a different target
	// (for example a manual rollback) while the user was still deciding on it.
	fileName := versionedInstallerFileName(updateTargetFileName(), result.LatestVersion)
	if a.updateDownloadInProgress(fileName) {
		return silentUpdateOutcome{err: fmt.Errorf("another installer download is already in progress")}
	}
	var path string
	var err error
	if silentUpdateDownloadHook != nil {
		path, err = silentUpdateDownloadHook(a, result.DownloadUrl, fileName, result.SHA256)
	} else {
		path, err = a.downloadUpdateWithSHA256(result.DownloadUrl, fileName, result.SHA256, false)
	}
	if err != nil {
		return silentUpdateOutcome{err: err}
	}
	if ctx != nil && ctx.Err() != nil {
		return silentUpdateOutcome{err: ctx.Err()}
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return silentUpdateOutcome{err: fmt.Errorf("reload config after silent download: %w", err)}
	}
	if !cfg.SilentUpdateEnabled {
		if version, ok := appUpdateVersion(result); ok && state != nil && state.markUpdateNotified(version) {
			a.emitEvent(EventAppUpdateAvailable, result)
		}
		return silentUpdateOutcome{err: fmt.Errorf("silent updates were disabled after the background download")}
	}
	// Channel preference can change while a large installer download is in
	// flight. A beta artifact is not a valid offering on the stable channel;
	// drop it and let the next check use the newly selected channel. (A stable
	// result remains valid when beta is preferred: beta mode deliberately falls
	// back to a newer formal release.)
	if !cfg.PreferBetaChannel && updateChannelKey(result.Channel) == "beta" {
		return silentUpdateOutcome{err: fmt.Errorf("beta update %s was downloaded after the beta channel was disabled", result.LatestVersion)}
	}
	if a.isUpdateVersionIgnored(result.LatestVersion) {
		return silentUpdateOutcome{terminal: true}
	}
	if state != nil && state.silentUpdateVersionSuperseded(result.LatestVersion, result.Channel) {
		return silentUpdateOutcome{err: fmt.Errorf("update %s was superseded before installation", result.LatestVersion)}
	}
	// Download completion is not install approval. Hand the verified installer
	// to the UI and wait for InstallDownloadedUpdate or a defer/ignore choice.
	a.storeDownloadedUpdate(result, path)
	return silentUpdateOutcome{terminal: true}
}

func (a *App) updateDownloadInProgress(fileName string) bool {
	if a == nil || strings.TrimSpace(fileName) == "" {
		return false
	}
	a.downloadMutex.Lock()
	defer a.downloadMutex.Unlock()
	_, ok := a.downloadCancelers[downloadRegistryKey(fileName)]
	return ok
}

// downloadRegistryKey normalizes the downloadCancelers map key. The key is a
// bare file name joined with the downloads folder, so on a case-insensitive
// filesystem differently-cased names for the same installer (a URL-carried
// asset name vs the brand-derived fallback) name the same physical file:
// without normalization two "different" downloads could hold the destination
// open concurrently and corrupt each other's bytes.
func downloadRegistryKey(fileName string) string {
	if pathCaseInsensitive() {
		return strings.ToLower(strings.TrimSpace(fileName))
	}
	return strings.TrimSpace(fileName)
}

// appUpdateVersion returns the normalized release version carried by a result.
func appUpdateVersion(result UpdateResult) (string, bool) {
	if !result.HasUpdate {
		return "", false
	}
	version := strings.TrimSpace(result.LatestVersion)
	if version == "" {
		return "", false
	}
	return version, true
}

// appUpdateNotifyVersion returns the normalized version to record when a check
// result should surface a UI notice. Same version is only notified once per
// process lifetime (frontend also persists a per-version dismiss in localStorage).
func appUpdateNotifyVersion(result UpdateResult, lastNotifiedVersion string) (string, bool) {
	version, ok := appUpdateVersion(result)
	if !ok || version == strings.TrimSpace(lastNotifiedVersion) {
		return "", false
	}
	return version, true
}
