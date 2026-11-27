package guiapp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	deferredUpdateInstallMaxAge       = 7 * 24 * time.Hour
	deferredUpdateInstallRetryDelay   = 30 * time.Minute
	deferredUpdateInstallStartupDelay = 20 * time.Second
)

// downloadedUpdateReady is the verified installer waiting for the user's
// install decision in this process. It is memory-only because it is also the
// launch authorization for that installer.
type downloadedUpdateReady struct {
	Result        UpdateResult
	InstallerPath string
	DownloadedAt  time.Time
}

// AppUpdateDownloadedEvent is emitted after a silent background download. The
// install decision belongs to the user; the backend never launches solely
// because this download completed.
type AppUpdateDownloadedEvent struct {
	UpdateResult
	InstallerPath string `json:"installer_path"`
}

// deferredUpdateInstall is the durable representation of a choice the user
// already made. It deliberately stores no download URL or hash: both are
// fetched fresh before execution so a writable state file cannot become an
// elevated-installer authorization.
type deferredUpdateInstall struct {
	TargetVersion string    `json:"target_version"`
	InstallerPath string    `json:"installer_path"`
	Channel       string    `json:"channel,omitempty"`
	DueAt         time.Time `json:"due_at,omitempty"`
	NextStartup   bool      `json:"next_startup,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

func updateVersionKey(version string) string {
	version = strings.TrimSpace(strings.ToLower(version))
	return strings.TrimPrefix(version, "v")
}

func sameUpdateVersion(left, right string) bool {
	leftKey := updateVersionKey(left)
	return leftKey != "" && leftKey == updateVersionKey(right)
}

func normalizeReadyUpdateResult(result UpdateResult) UpdateResult {
	result.LatestVersion = strings.TrimSpace(result.LatestVersion)
	return result
}

func (a *App) storeDownloadedUpdate(result UpdateResult, installerPath string) {
	if a == nil {
		return
	}
	result = normalizeReadyUpdateResult(result)
	ready := &downloadedUpdateReady{
		Result:        result,
		InstallerPath: strings.TrimSpace(installerPath),
		DownloadedAt:  time.Now(),
	}
	a.downloadedUpdateMu.Lock()
	if a.downloadedUpdates == nil {
		a.downloadedUpdates = make(map[string]*downloadedUpdateReady)
	}
	a.downloadedUpdates[updateVersionKey(result.LatestVersion)] = ready
	a.downloadedUpdateMu.Unlock()
	a.emitEvent(EventAppUpdateDownloaded, AppUpdateDownloadedEvent{
		UpdateResult:  result,
		InstallerPath: ready.InstallerPath,
	})
}

func (a *App) downloadedUpdateForVersion(targetVersion string) *downloadedUpdateReady {
	if a == nil {
		return nil
	}
	a.downloadedUpdateMu.Lock()
	defer a.downloadedUpdateMu.Unlock()
	ready := a.downloadedUpdates[updateVersionKey(targetVersion)]
	if ready == nil || !sameUpdateVersion(ready.Result.LatestVersion, targetVersion) {
		return nil
	}
	copyReady := *ready
	return &copyReady
}

func (a *App) clearDownloadedUpdateForVersion(targetVersion string) {
	if a == nil {
		return
	}
	a.downloadedUpdateMu.Lock()
	delete(a.downloadedUpdates, updateVersionKey(targetVersion))
	a.downloadedUpdateMu.Unlock()
}

func (a *App) deferredUpdateInstallPath() string {
	if a == nil {
		return ""
	}
	base := strings.TrimSpace(a.getMaclawBaseDir())
	if base == "" {
		return ""
	}
	return filepath.Join(base, "update", "deferred-install.json")
}

func writeDeferredUpdateInstall(path string, record deferredUpdateInstall) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("deferred update state path is empty")
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	// This file carries a user authorization decision. Never leave a torn
	// half-write behind if the app or machine dies while persisting it.
	return atomicWriteFile(path, data)
}

func (a *App) readDeferredUpdateInstall() (deferredUpdateInstall, bool) {
	var empty deferredUpdateInstall
	if a == nil {
		return empty, false
	}
	path := a.deferredUpdateInstallPath()
	if path == "" {
		return empty, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return empty, false
	}
	var record deferredUpdateInstall
	if err := json.Unmarshal(data, &record); err != nil || strings.TrimSpace(record.TargetVersion) == "" {
		a.log(fmt.Sprintf("[update-install] discarding unusable deferred-install record: %v", err))
		_ = os.Remove(path)
		return empty, false
	}
	// A timed record without a due time would otherwise become an immediate
	// install after restart, and a record without a creation time could never
	// expire. Treat both as corrupt rather than acting on a partial choice.
	if record.CreatedAt.IsZero() || (!record.NextStartup && record.DueAt.IsZero()) {
		a.log(fmt.Sprintf("[update-install] discarding malformed deferred-install record for %s", record.TargetVersion))
		_ = os.Remove(path)
		return empty, false
	}
	if time.Since(record.CreatedAt) > deferredUpdateInstallMaxAge {
		a.log(fmt.Sprintf("[update-install] deferred install for %s expired", record.TargetVersion))
		_ = os.Remove(path)
		return empty, false
	}
	if current, ok := linkedReleaseVersion(); ok && updateRunningVersionReachesTarget(current, record.TargetVersion) {
		a.log(fmt.Sprintf("[update-install] deferred update %s already installed (running %s); cleared", record.TargetVersion, current))
		_ = os.Remove(path)
		return empty, false
	}
	return record, true
}

func (a *App) stopDeferredUpdateInstallTimer() {
	if a == nil {
		return
	}
	a.downloadedUpdateMu.Lock()
	if a.downloadedUpdateTimer != nil {
		a.downloadedUpdateTimer.Stop()
		a.downloadedUpdateTimer = nil
	}
	a.downloadedUpdateMu.Unlock()
}

func (a *App) clearDeferredUpdateInstall() error {
	if a == nil {
		return nil
	}
	a.stopDeferredUpdateInstallTimer()
	path := a.deferredUpdateInstallPath()
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (a *App) scheduleDeferredUpdateInstall(record deferredUpdateInstall, startup bool) {
	if a == nil || strings.TrimSpace(record.TargetVersion) == "" {
		return
	}
	var delay time.Duration
	if record.NextStartup {
		if !startup {
			return
		}
		delay = deferredUpdateInstallStartupDelay
	} else {
		delay = time.Until(record.DueAt)
		if delay < 0 {
			delay = 0
		}
	}
	// A timed defer can become overdue while the app is closed. Do not fire
	// that catch-up install in the middle of process startup; give the UI the
	// same short settling window as an explicit next-startup choice.
	if startup && delay < deferredUpdateInstallStartupDelay {
		delay = deferredUpdateInstallStartupDelay
	}
	targetVersion := strings.TrimSpace(record.TargetVersion)
	a.downloadedUpdateMu.Lock()
	if a.downloadedUpdateTimer != nil {
		a.downloadedUpdateTimer.Stop()
	}
	a.downloadedUpdateTimer = time.AfterFunc(delay, func() {
		if a.ctx != nil && a.ctx.Err() != nil {
			return
		}
		go a.executeDeferredUpdateInstall(targetVersion, startup)
	})
	a.downloadedUpdateMu.Unlock()
}

func (a *App) startDeferredUpdateInstalls() {
	record, ok := a.readDeferredUpdateInstall()
	if !ok {
		return
	}
	if a.isUpdateVersionIgnored(record.TargetVersion) {
		a.log(fmt.Sprintf("[update-install] deferred update %s is ignored; cleared", record.TargetVersion))
		_ = a.clearDeferredUpdateInstall()
		return
	}
	a.scheduleDeferredUpdateInstall(record, true)
}

func (a *App) markDownloadedUpdateActionRunning() bool {
	a.downloadedUpdateMu.Lock()
	defer a.downloadedUpdateMu.Unlock()
	if a.downloadedUpdateActionRunning {
		return false
	}
	a.downloadedUpdateActionRunning = true
	return true
}

func (a *App) unmarkDownloadedUpdateActionRunning() {
	a.downloadedUpdateMu.Lock()
	a.downloadedUpdateActionRunning = false
	a.downloadedUpdateMu.Unlock()
}

func (a *App) executeDeferredUpdateInstall(targetVersion string, startupExecution bool) {
	if a == nil {
		return
	}
	// The decision lock is deliberately NOT held across this function: the
	// check + download phases below can take minutes on a slow link, and a
	// user install/defer/ignore click during that window would otherwise be
	// refused outright. Only the final launch decision takes the lock, with a
	// record-equality re-check so a choice the user made mid-download wins.
	record, ok := a.readDeferredUpdateInstall()
	if !ok || !sameUpdateVersion(record.TargetVersion, targetVersion) {
		return
	}
	if a.isUpdateVersionIgnored(record.TargetVersion) {
		_ = a.clearDeferredUpdateInstall()
		return
	}
	// A stale countdown callback can fire just after the user moves the install
	// to next startup. That record is only executable by the startup path;
	// keeping it intact here is what lets the next launch honor the choice.
	if record.NextStartup && !startupExecution {
		return
	}
	if !record.NextStartup && time.Now().Before(record.DueAt) {
		a.scheduleDeferredUpdateInstall(record, false)
		return
	}

	currentVersion, hasVersion := linkedReleaseVersion()
	if !hasVersion {
		a.log("[update-install] deferred install skipped: local release version is unavailable")
		return
	}
	var result UpdateResult
	var err error
	if deferredUpdateCheckHook != nil {
		result, err = deferredUpdateCheckHook(a, currentVersion, record.Channel)
	} else if strings.EqualFold(record.Channel, deferredRollbackChannel) {
		result, err = a.checkDeferredRollbackTarget(currentVersion, record.TargetVersion)
	} else if strings.EqualFold(record.Channel, "beta") {
		result, err = a.CheckUpdateBeta(currentVersion)
	} else {
		result, err = a.CheckUpdate(currentVersion)
	}
	if err != nil {
		a.log(fmt.Sprintf("[update-install] deferred update check failed for %s: %v", record.TargetVersion, err))
		a.retryDeferredUpdateInstall(record)
		return
	}
	if !result.HasUpdate || !sameUpdateVersion(result.LatestVersion, record.TargetVersion) {
		a.log(fmt.Sprintf("[update-install] deferred update %s is no longer the latest target; cleared", record.TargetVersion))
		_ = a.clearDeferredUpdateInstall()
		return
	}

	installerPath := ""
	if ready := a.downloadedUpdateForVersion(record.TargetVersion); ready != nil {
		// A same-session file still has to match the freshly fetched manifest
		// when that manifest carries a hash; otherwise a re-signed replacement
		// release would silently install the older bytes.
		if result.SHA256 == "" || verifySHA256File(ready.InstallerPath, result.SHA256) == nil {
			if result.SHA256 != "" {
				// The original same-session download may predate the fresh manifest
				// hash. Rebind launch authorization to that hash so the final
				// pre-launch check verifies content rather than only file identity.
				a.rememberVerifiedInstaller(ready.InstallerPath, result.SHA256)
			}
			installerPath = ready.InstallerPath
		}
	} else if result.SHA256 != "" && record.InstallerPath != "" {
		// Reuse the already downloaded file only after verifying it against the
		// freshly fetched manifest hash. The persisted path itself is not trusted.
		if verifySHA256File(record.InstallerPath, result.SHA256) == nil {
			a.rememberVerifiedInstaller(record.InstallerPath, result.SHA256)
			installerPath = record.InstallerPath
		}
	}
	if installerPath == "" {
		var downloadErr error
		fileName := versionedInstallerFileName(updateTargetFileName(), result.LatestVersion)
		if silentUpdateDownloadHook != nil {
			installerPath, downloadErr = silentUpdateDownloadHook(a, result.DownloadUrl, fileName, result.SHA256)
		} else {
			installerPath, downloadErr = a.downloadUpdateWithSHA256(result.DownloadUrl, fileName, result.SHA256, false)
		}
		if downloadErr != nil {
			a.log(fmt.Sprintf("[update-install] deferred update download failed for %s: %v", record.TargetVersion, downloadErr))
			a.retryDeferredUpdateInstall(record)
			return
		}
	}

	// A re-download lands on the versioned destination, which can differ from
	// the path the original deferred record stored (e.g. a record written
	// before versioned naming). Carry the fresh path into any retry record so
	// a later attempt can verify and reuse these bytes instead of downloading
	// the same installer again after every transient launch failure.
	if installerPath != "" && installerPath != record.InstallerPath {
		record.InstallerPath = installerPath
	}
	// A timer that wins the shutdown race must not turn quitting into a fresh
	// installer launch. The durable record remains, so the next startup can
	// still honor the user's deferred choice.
	if a.ctx != nil && a.ctx.Err() != nil {
		return
	}
	if !a.markDownloadedUpdateActionRunning() {
		// A user decision (install now / a different defer / ignore) owns the
		// record right now and supersedes this execution. The durable record
		// — or its replacement — keeps the choice alive either way.
		a.log(fmt.Sprintf("[update-install] deferred install for %s yielded to a concurrent update decision", record.TargetVersion))
		return
	}
	// A user decision made while the check/download ran (install now, ignore,
	// or a different defer) rewrote or removed the durable record. Anything
	// that no longer matches the record this execution started with drives
	// its own path, so this stale run must stop before launching.
	latest, stillRecorded := a.readDeferredUpdateInstall()
	if !stillRecorded || !deferredUpdateRecordsEquivalent(record, latest) {
		a.unmarkDownloadedUpdateActionRunning()
		return
	}
	// The user may have ignored this release while a retry/check/download was
	// in flight. Re-check immediately before the launch decision is consumed.
	if a.isUpdateVersionIgnored(record.TargetVersion) {
		a.unmarkDownloadedUpdateActionRunning()
		_ = a.clearDeferredUpdateInstall()
		return
	}
	var launchErr error
	if silentUpdateInstallHook != nil {
		launchErr = silentUpdateInstallHook(a, installerPath, result.LatestVersion)
	} else {
		launchErr = a.LaunchInstallerAndExit(installerPath, result.LatestVersion)
	}
	a.unmarkDownloadedUpdateActionRunning()
	if launchErr != nil {
		if errors.Is(launchErr, errInstallerElevationCancelled) {
			// A declined elevation prompt must not turn into repeated UAC
			// prompts. Drop the preauthorization; the normal update flow can
			// offer it again.
			a.log(fmt.Sprintf("[update-install] deferred installer elevation declined for %s", record.TargetVersion))
			_ = a.clearDeferredUpdateInstall()
			return
		}
		// Other launch failures are usually transient file/shell errors and
		// happen before a consent prompt. Keep the user's deferred choice and
		// try later instead of silently dropping it.
		a.log(fmt.Sprintf("[update-install] deferred installer launch failed for %s: %v", record.TargetVersion, launchErr))
		a.retryDeferredUpdateInstall(record)
		return
	}
	_ = a.clearDeferredUpdateInstall()
}

// retryDeferredUpdateInstall keeps an already authorized deferred install
// alive after a failure that occurred before any user-visible consent choice.
// Timed retries persist their revised due time too: a restart must not turn a
// 30-minute retry backoff into an immediate repeated attempt. Next-startup
// choices instead remain tied to the session boundary they asked for.
func (a *App) retryDeferredUpdateInstall(record deferredUpdateInstall) {
	if record.NextStartup {
		// "Install on next startup" is a session-boundary choice. A network or
		// launch failure during that startup attempt must not convert it into a
		// 30-minute-then-interrupt-this-session install; leave the durable record
		// unchanged so the next launch tries again.
		a.log(fmt.Sprintf("[update-install] next-startup update install for %s will retry on a later launch", record.TargetVersion))
		return
	}
	// The retry rewrites the durable record, so it needs the same decision
	// lock as the user-facing choices. A busy lock means a user decision is
	// writing the record right now; that choice supersedes this retry and
	// must not be clobbered by it.
	if !a.markDownloadedUpdateActionRunning() {
		return
	}
	defer a.unmarkDownloadedUpdateActionRunning()
	retry := record
	retry.DueAt = time.Now().Add(deferredUpdateInstallRetryDelay)
	if err := writeDeferredUpdateInstall(a.deferredUpdateInstallPath(), retry); err != nil {
		a.log(fmt.Sprintf("[update-install] deferred update retry state was not persisted for %s: %v", record.TargetVersion, err))
	}
	a.scheduleDeferredUpdateInstall(retry, false)
}

// deferredUpdateRecordsEquivalent reports whether the durable record still
// describes the same user choice an execution started from. The decision
// fields are compared; InstallerPath is deliberately excluded because the
// execution itself may have rebound it to a freshly downloaded file.
func deferredUpdateRecordsEquivalent(started, current deferredUpdateInstall) bool {
	return started.TargetVersion == current.TargetVersion &&
		started.Channel == current.Channel &&
		started.NextStartup == current.NextStartup &&
		started.DueAt.Equal(current.DueAt) &&
		started.CreatedAt.Equal(current.CreatedAt)
}

// deferredUpdateCheckHook is a test seam for executeDeferredUpdateInstall.
// Production always uses the signed release-channel checks above.
var deferredUpdateCheckHook func(a *App, currentVersion, channel string) (UpdateResult, error)

func validDeferredInstallHours(hours int) bool {
	switch hours {
	case 1, 2, 5, 10:
		return true
	default:
		return false
	}
}

// deferredRollbackChannel marks a deferred install whose target is a rollback
// build rather than a channel release. The distinction matters at execution
// time: a rollback is deliberately NOT the latest release, so validating it
// against the latest-version check would always clear the user's choice
// instead of installing it.
const deferredRollbackChannel = "rollback"

// deferredRollbackCatalogueHook is a test seam for checkDeferredRollbackTarget.
// Production always reads the signed server catalogue via ListRollbackReleases.
var deferredRollbackCatalogueHook func(a *App) ([]RollbackRelease, error)

// checkDeferredRollbackTarget revalidates a deferred rollback install against
// the server-published rollback catalogue. The catalogue is the same signed
// source the original pick came from: the download URL and hash are fetched
// fresh rather than trusted from the durable record.
func (a *App) checkDeferredRollbackTarget(currentVersion, targetBuild string) (UpdateResult, error) {
	var releases []RollbackRelease
	var err error
	if deferredRollbackCatalogueHook != nil {
		releases, err = deferredRollbackCatalogueHook(a)
	} else {
		releases, err = a.ListRollbackReleases()
	}
	if err != nil {
		return UpdateResult{}, err
	}
	return resolveDeferredRollbackTarget(releases, currentVersion, targetBuild), nil
}

// resolveDeferredRollbackTarget is the pure decision behind
// checkDeferredRollbackTarget. HasUpdate stays false when the build is no
// longer catalogued or installable, so the caller drops a choice that can no
// longer be honored safely; it is also false when the running build already
// reached the target, because then there is nothing left to install.
func resolveDeferredRollbackTarget(releases []RollbackRelease, currentVersion, targetBuild string) UpdateResult {
	base := UpdateResult{HasUpdate: false, LatestVersion: strings.TrimSpace(targetBuild), Channel: deferredRollbackChannel}
	for _, release := range releases {
		if !sameUpdateVersion(release.Build, targetBuild) {
			continue
		}
		if release.DownloadUnavailable || strings.TrimSpace(release.DownloadUrl) == "" || strings.TrimSpace(release.SHA256) == "" {
			return base
		}
		if updateRunningVersionReachesTarget(currentVersion, targetBuild) {
			return base
		}
		return UpdateResult{
			HasUpdate:     true,
			LatestVersion: strings.TrimSpace(release.Build),
			DownloadUrl:   strings.TrimSpace(release.DownloadUrl),
			SHA256:        strings.TrimSpace(release.SHA256),
			Channel:       deferredRollbackChannel,
		}
	}
	return base
}

func (a *App) readyDownloadedUpdate(targetVersion string) (*downloadedUpdateReady, error) {
	ready := a.downloadedUpdateForVersion(targetVersion)
	if ready == nil {
		return nil, fmt.Errorf("downloaded update %s is no longer available in this session", strings.TrimSpace(targetVersion))
	}
	if strings.TrimSpace(ready.InstallerPath) == "" {
		return nil, fmt.Errorf("downloaded update installer path is empty")
	}
	return ready, nil
}

// RegisterDownloadedUpdate remembers an installer that was downloaded through
// the manual update dialog in this process. Silent downloads already call
// storeDownloadedUpdate with the full check result; manual downloads use this
// minimal registration so the same explicit install/defer decisions apply.
func (a *App) RegisterDownloadedUpdate(targetVersion, installerPath, channel string) error {
	targetVersion = strings.TrimSpace(targetVersion)
	installerPath = strings.TrimSpace(installerPath)
	if targetVersion == "" {
		return fmt.Errorf("downloaded update version is empty")
	}
	if installerPath == "" {
		return fmt.Errorf("downloaded update installer path is empty")
	}
	ready := &downloadedUpdateReady{
		Result: UpdateResult{
			HasUpdate:     true,
			LatestVersion: targetVersion,
			Channel:       strings.TrimSpace(channel),
		},
		InstallerPath: installerPath,
		DownloadedAt:  time.Now(),
	}
	a.downloadedUpdateMu.Lock()
	if a.downloadedUpdates == nil {
		a.downloadedUpdates = make(map[string]*downloadedUpdateReady)
	}
	a.downloadedUpdates[updateVersionKey(targetVersion)] = ready
	a.downloadedUpdateMu.Unlock()
	return nil
}

// InstallDownloadedUpdate launches the already downloaded installer only after
// an explicit user choice.
func (a *App) InstallDownloadedUpdate(targetVersion string) error {
	ready, err := a.readyDownloadedUpdate(targetVersion)
	if err != nil {
		return err
	}
	if !a.markDownloadedUpdateActionRunning() {
		return fmt.Errorf("another update install decision is already in progress")
	}
	defer a.unmarkDownloadedUpdateActionRunning()
	// Installing now supersedes any earlier defer choice, including when the
	// elevation prompt is declined: a later surprise UAC prompt would be worse.
	_ = a.clearDeferredUpdateInstall()
	if silentUpdateInstallHook != nil {
		return silentUpdateInstallHook(a, ready.InstallerPath, ready.Result.LatestVersion)
	}
	return a.LaunchInstallerAndExit(ready.InstallerPath, ready.Result.LatestVersion)
}

// DeferDownloadedUpdateInstall persists the user's choice to install after
// 1, 2, 5, or 10 hours.
func (a *App) DeferDownloadedUpdateInstall(targetVersion string, hours int) error {
	if !validDeferredInstallHours(hours) {
		return fmt.Errorf("unsupported update install delay: %d hours", hours)
	}
	if !a.markDownloadedUpdateActionRunning() {
		return fmt.Errorf("another update install decision is already in progress")
	}
	defer a.unmarkDownloadedUpdateActionRunning()
	ready, err := a.readyDownloadedUpdate(targetVersion)
	if err != nil {
		return err
	}
	record := deferredUpdateInstall{
		TargetVersion: strings.TrimSpace(ready.Result.LatestVersion),
		InstallerPath: ready.InstallerPath,
		Channel:       strings.TrimSpace(ready.Result.Channel),
		DueAt:         time.Now().Add(time.Duration(hours) * time.Hour),
		CreatedAt:     time.Now(),
	}
	if err := writeDeferredUpdateInstall(a.deferredUpdateInstallPath(), record); err != nil {
		return fmt.Errorf("save deferred update install: %w", err)
	}
	a.scheduleDeferredUpdateInstall(record, false)
	return nil
}

// DeferDownloadedUpdateInstallToNextStartup persists the user's choice to
// install this already downloaded update on the next application start.
func (a *App) DeferDownloadedUpdateInstallToNextStartup(targetVersion string) error {
	if !a.markDownloadedUpdateActionRunning() {
		return fmt.Errorf("another update install decision is already in progress")
	}
	defer a.unmarkDownloadedUpdateActionRunning()
	ready, err := a.readyDownloadedUpdate(targetVersion)
	if err != nil {
		return err
	}
	record := deferredUpdateInstall{
		TargetVersion: strings.TrimSpace(ready.Result.LatestVersion),
		InstallerPath: ready.InstallerPath,
		Channel:       strings.TrimSpace(ready.Result.Channel),
		NextStartup:   true,
		CreatedAt:     time.Now(),
	}
	if err := writeDeferredUpdateInstall(a.deferredUpdateInstallPath(), record); err != nil {
		return fmt.Errorf("save next-startup update install: %w", err)
	}
	// A next-startup choice replaces any timed countdown in this process. The
	// timer callback re-reads the durable record, so leaving it active would
	// install during this session even though the user moved the install to
	// the next launch.
	a.stopDeferredUpdateInstallTimer()
	return nil
}

type ignoredUpdateVersionsFile struct {
	Versions map[string]time.Time `json:"versions"`
}

func (a *App) ignoredUpdateVersionsPath() string {
	if a == nil {
		return ""
	}
	base := strings.TrimSpace(a.getMaclawBaseDir())
	if base == "" {
		return ""
	}
	return filepath.Join(base, "update", "ignored-versions.json")
}

func readIgnoredUpdateVersions(path string) ignoredUpdateVersionsFile {
	var result ignoredUpdateVersionsFile
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &result) != nil || result.Versions == nil {
		result.Versions = make(map[string]time.Time)
	}
	return result
}

func (a *App) isUpdateVersionIgnored(version string) bool {
	if a == nil {
		return false
	}
	key := updateVersionKey(version)
	if key == "" {
		return false
	}
	a.updateIgnoreMu.Lock()
	defer a.updateIgnoreMu.Unlock()
	ignored := readIgnoredUpdateVersions(a.ignoredUpdateVersionsPath())
	_, ok := ignored.Versions[key]
	return ok
}

func (a *App) addIgnoredUpdateVersion(version string) error {
	key := updateVersionKey(version)
	if key == "" {
		return fmt.Errorf("update version is empty")
	}
	path := a.ignoredUpdateVersionsPath()
	if path == "" {
		return fmt.Errorf("ignored-update state path is empty")
	}
	a.updateIgnoreMu.Lock()
	defer a.updateIgnoreMu.Unlock()
	ignored := readIgnoredUpdateVersions(path)
	ignored.Versions[key] = time.Now()
	data, err := json.Marshal(ignored)
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data)
}

// IgnoreUpdateVersion persists the user's choice to skip exactly this release.
// A newer release is a different version key and can still be offered.
func (a *App) IgnoreUpdateVersion(targetVersion string) error {
	targetVersion = strings.TrimSpace(targetVersion)
	if err := a.addIgnoredUpdateVersion(targetVersion); err != nil {
		return err
	}
	a.clearDownloadedUpdateForVersion(targetVersion)
	if record, ok := a.readDeferredUpdateInstall(); ok && sameUpdateVersion(record.TargetVersion, targetVersion) {
		_ = a.clearDeferredUpdateInstall()
	}
	return nil
}
