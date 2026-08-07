package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"clawmatemaker/internal/catalog"
	"clawmatemaker/internal/device"
	"clawmatemaker/internal/diagnostics"
	"clawmatemaker/internal/firmware"
	"clawmatemaker/internal/jobs"
	"clawmatemaker/internal/logging"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the narrow Wails boundary. All safety decisions remain in internal packages.
type App struct {
	ctx             context.Context
	watchCancel     context.CancelFunc
	logRoot         string
	recovery        []jobs.RecoveryItem
	packageRefsMu   sync.Mutex
	verifiedPackage map[string]verifiedPackage
	confirmationsMu sync.Mutex
	confirmations   map[string]boardConfirmation
	writeRequestsMu sync.Mutex
	writeRequests   map[string]*writeRequest
	bootVerifyMu    sync.Mutex
	bootVerifies    map[string]*bootVerifyRequest
	activeWritesMu  sync.Mutex
	activeWrites    map[string]activeWrite
	plansMu         sync.Mutex
	plans           map[string]*preparedFlashPlan
}

type verifiedPackage struct {
	path              string
	boardID           string
	archiveSHA256     string
	installPlan       string
	preservesUserData bool
	requiresRecovery  bool
	issuedAt          time.Time
}

// verifiedPackageTTL limits an in-memory package capability to the user's
// current installation decision. FlashJob still re-verifies the signed
// archive immediately before a write; this expiry additionally prevents an
// old UI state from retaining write authority indefinitely.
const verifiedPackageTTL = 30 * time.Minute

// boardConfirmationTTL deliberately makes physical-board confirmation a
// short-lived, session-only capability. It is separate from the longer-lived
// package reference: a downloaded archive may remain valid, but a user must
// reconfirm the actual device before an irreversible write.
const boardConfirmationTTL = 10 * time.Minute

type boardConfirmation struct {
	port          string
	boardID       string
	probeJobID    string
	deviceBinding string
	issuedAt      time.Time
}

// BoardConfirmation is the opaque, session-only evidence that the user
// explicitly confirmed a board label after a successful read-only probe.
// It intentionally does not expose host identity or probe internals.
type BoardConfirmation struct {
	ConfirmationRef string    `json:"confirmationRef"`
	Port            string    `json:"port"`
	BoardID         string    `json:"boardId"`
	ExpiresAt       time.Time `json:"expiresAt" ts_type:"string"`
}

// errOfficialBuildRequired keeps the developer executable genuinely
// probe-only.  A developer build has no embedded release trust root and may
// use a locally installed diagnostic sidecar; it must never look like an
// install-capable distribution just because a caller reaches the backend
// directly instead of going through the UI.
var errOfficialBuildRequired = fmt.Errorf("firmware download, import, and flashing require an official ClawMate Maker release build")

type writeRequest struct {
	fingerprint string
	done        chan struct{}
	result      jobs.FlashResult
	err         error
}

type bootVerifyRequest struct {
	fingerprint string
	done        chan struct{}
	result      jobs.BootVerificationResult
	err         error
}

type activeWrite struct {
	cancel context.CancelFunc
	job    *jobs.FlashJob
}

// FlashPlan is the browser-safe representation of an immutable prepared
// operation. It intentionally contains no archive path, device MAC, or raw
// confirmation secret. StartJob accepts only PlanID + PlanHash and rechecks
// the stored package and confirmation capability before writing.
type FlashPlan struct {
	PlanID            string    `json:"planId"`
	PlanHash          string    `json:"planHash"`
	Port              string    `json:"port"`
	BoardID           string    `json:"boardId"`
	InstallPlan       string    `json:"installPlan"`
	PreservesUserData bool      `json:"preservesUserData"`
	RequiresRecovery  bool      `json:"requiresRecovery"`
	Recovery          bool      `json:"recovery"`
	ExpiresAt         time.Time `json:"expiresAt" ts_type:"string"`
}

type preparedFlashPlan struct {
	FlashPlan
	packageRef      string
	confirmationRef string
	recoveryJobID   string
	requestID       string
	start           *planStart
}

type planStart struct {
	done   chan struct{}
	result jobs.FlashResult
	err    error
}

func NewApp() *App {
	return &App{
		verifiedPackage: make(map[string]verifiedPackage),
		confirmations:   make(map[string]boardConfirmation),
		writeRequests:   make(map[string]*writeRequest),
		bootVerifies:    make(map[string]*bootVerifyRequest),
		activeWrites:    make(map[string]activeWrite),
		plans:           make(map[string]*preparedFlashPlan),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// Do this before the UI starts regular discovery. A previous single-slot
	// write that never reached verified boot must take precedence over a new
	// installation attempt.
	if items, err := jobs.ListRecoveryRequired(a.logsPath()); err == nil {
		a.recovery = items
	}
	watchCtx, cancel := context.WithCancel(ctx)
	a.watchCancel = cancel
	go device.WatchCandidates(watchCtx, device.ListCandidates, device.DefaultWatchPolicy(), func(event device.ChangeEvent) {
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, "device:change", event)
		}
	})
}
func (a *App) shutdown(context.Context) {
	if a.watchCancel != nil {
		a.watchCancel()
	}
	// Shutdown cannot safely leave a child flashing process alive after the
	// desktop process exits. Requesting cancellation here preserves the same
	// durable recovery-journal path used by the explicit UI cancellation.
	a.cancelActiveWrites()
}

// PreventCloseWhileWriting is called by Wails before the native window closes.
// It emits an event for the UI to explain that an irreversible operation is
// still active, and returns true so the close is cancelled. The user must use
// the explicit in-app cancellation control, which records recovery evidence.
func (a *App) PreventCloseWhileWriting(context.Context) bool {
	if !a.hasActiveWrites() {
		return false
	}
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "app:close-blocked", map[string]any{"reason": "flash_active"})
	}
	return true
}

func (a *App) hasActiveWrites() bool {
	a.activeWritesMu.Lock()
	defer a.activeWritesMu.Unlock()
	return len(a.activeWrites) != 0
}

func (a *App) cancelActiveWrites() {
	a.activeWritesMu.Lock()
	active := make([]activeWrite, 0, len(a.activeWrites))
	for _, write := range a.activeWrites {
		active = append(active, write)
	}
	a.activeWritesMu.Unlock()
	for _, write := range active {
		if write.job != nil {
			write.job.RequestCancel()
		}
		if write.cancel != nil {
			write.cancel()
		}
	}
}

type AppInfo struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Platform  string `json:"platform"`
	LogRoot   string `json:"logRoot"`
	ProbeOnly bool   `json:"probeOnly"`
}

// AutoDetectionResult is the non-writing result of the startup convenience
// flow.  It is deliberately evidence-first: a download can be prepared from
// a unique, nonce-bound application identity, but it never confirms the
// physical board or authorizes a flash operation.
type AutoDetectionResult struct {
	Status     string                     `json:"status"`
	Candidates []device.Candidate         `json:"candidates,omitempty"`
	Device     *device.Candidate          `json:"device,omitempty"`
	Probe      *jobs.ProbeResult          `json:"probe,omitempty"`
	Firmware   *catalog.DownloadedRelease `json:"firmware,omitempty"`
	Update     catalog.FirmwareUpdate     `json:"update"`
	Devices    []AutoDetectedDevice       `json:"devices,omitempty"`
	Reason     string                     `json:"reason,omitempty"`
}

// AutoDetectedDevice describes one independently probed USB/ESP candidate.
// It is intentionally read-only: even when a nonce-bound application identity
// picks one official asset, the caller must still make a physical board-label
// confirmation before a package capability can authorize a write.
type AutoDetectedDevice struct {
	Device   device.Candidate           `json:"device"`
	Access   device.HostAccess          `json:"access"`
	Probe    *jobs.ProbeResult          `json:"probe,omitempty"`
	Status   string                     `json:"status"`
	Reason   string                     `json:"reason,omitempty"`
	BoardID  string                     `json:"boardId,omitempty"`
	Firmware *catalog.DownloadedRelease `json:"firmware,omitempty"`
	Update   catalog.FirmwareUpdate     `json:"update"`
}

// AutoDetectPortFirmware performs the same non-writing identification flow as
// AutoDetectFirmware, but for one explicitly selected serial port. Selecting a
// port must not require a board or firmware-asset choice: a uniquely reported,
// nonce-bound runtime identity selects the only official asset automatically.
// Ambiguous or unavailable identity remains fail-closed.
func (a *App) AutoDetectPortFirmware(port string) (AutoDetectedDevice, error) {
	port = strings.TrimSpace(port)
	if port == "" {
		return AutoDetectedDevice{}, fmt.Errorf("serial port is required")
	}
	candidates, err := a.ListDevices()
	if err != nil {
		return AutoDetectedDevice{}, err
	}
	for _, candidate := range candidates {
		if candidate.Port != port {
			continue
		}
		if !candidate.IsUSB && !candidate.LikelyEsp {
			return AutoDetectedDevice{Device: candidate, Status: "unsupported_candidate", Reason: "The selected serial port is not an eligible USB or ESP candidate."}, nil
		}
		return a.autoDetectCandidate(candidate)
	}
	return AutoDetectedDevice{}, fmt.Errorf("selected serial port is no longer available")
}
func (a *App) GetAppInfo() AppInfo {
	return AppInfo{Name: "ClawMate Maker", Version: "0.1.0-dev", Platform: goruntime.GOOS + "/" + goruntime.GOARCH, LogRoot: a.logsPath(), ProbeOnly: releaseBuild != "true"}
}

func (a *App) ListDevices() ([]device.Candidate, error) { return device.ListCandidates() }

// AutoDetectFirmware performs the safe, read-only portion of the normal
// installation flow in one backend operation:
//
//  1. enumerate USB/ESP serial candidates;
//  2. probe every candidate sequentially using read-only ROM commands;
//  3. download and verify an allow-listed Release asset for every candidate
//     whose running application reports one known nonce-bound board target.
//
// USB/ROM evidence alone cannot distinguish the three supported products, so
// ambiguous identity is a normal result rather than an error. The caller must
// still have the user confirm each physical board before FlashFirmware.
func (a *App) AutoDetectFirmware() (AutoDetectionResult, error) {
	candidates, err := a.ListDevices()
	if err != nil {
		return AutoDetectionResult{}, err
	}
	eligible := make([]device.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.IsUSB || candidate.LikelyEsp {
			eligible = append(eligible, candidate)
		}
	}
	result := AutoDetectionResult{Candidates: candidates}
	if len(eligible) == 0 {
		result.Status = "no_candidate"
		result.Reason = "No USB or ESP serial candidate was found."
		return result, nil
	}
	result.Devices = make([]AutoDetectedDevice, 0, len(eligible))
	readyCount := 0
	for _, candidate := range eligible {
		item, _ := a.autoDetectCandidate(candidate)
		result.Devices = append(result.Devices, item)
		if item.Status == "firmware_ready" {
			readyCount++
		}
	}
	if len(result.Devices) == 1 {
		only := result.Devices[0]
		result.Device = &only.Device
		result.Probe = only.Probe
		result.Firmware = only.Firmware
		result.Update = only.Update
		result.Status = only.Status
		result.Reason = only.Reason
		return result, nil
	}
	if readyCount > 0 {
		result.Status = "multiple_devices_ready"
		result.Reason = "Multiple devices were independently identified and their signed firmware packages were prepared. Confirm each physical board label before flashing."
		return result, nil
	}
	result.Status = "multiple_candidates"
	result.Reason = "Multiple USB/ESP candidates were probed; inspect each result and select a device after confirming its physical board label."
	return result, nil
}

func (a *App) autoDetectCandidate(candidate device.Candidate) (AutoDetectedDevice, error) {
	item := AutoDetectedDevice{Device: candidate, Access: device.DiagnoseAccess(candidate.Port), Status: "probing"}
	if item.Access.Status != "ready" {
		item.Status = "access_blocked"
		item.Reason = item.Access.Message
		if item.Access.Guide != "" {
			item.Reason += " " + item.Access.Guide
		}
		return item, nil
	}
	probe, probeErr := a.ProbeDevice(candidate.Port)
	item.Probe = &probe
	if probeErr != nil {
		item.Status = "probe_failed"
		item.Reason = probe.ErrorMessage
		if item.Reason == "" {
			item.Reason = probeErr.Error()
		}
		return item, nil
	}
	recognition := probe.BoardRecognition
	if recognition.Status != "probable" || len(recognition.CandidateBoards) != 1 {
		item.Status = "requires_confirmation"
		item.Reason = recognition.Reason
		return item, nil
	}
	item.BoardID = recognition.CandidateBoards[0]
	firmware, downloadErr := a.GetLatestFirmware(item.BoardID)
	if downloadErr != nil {
		item.Status = "firmware_failed"
		item.Reason = downloadErr.Error()
		return item, nil
	}
	item.Firmware = &firmware
	item.Update = catalog.CompareFirmwareVersions(probe.AppIdentity.FirmwareVersion, firmware.FirmwareVersion)
	item.Status = "firmware_ready"
	item.Reason = "A signed firmware package was prepared from the device application identity. Confirm the physical board label before flashing."
	return item, nil
}

// CompareFirmwareVersions exposes a non-writing decision based only on the
// immutable integer build version. Display version strings are never parsed.
func (a *App) CompareFirmwareVersions(installedVersion, availableVersion int64) catalog.FirmwareUpdate {
	return catalog.CompareFirmwareVersions(installedVersion, availableVersion)
}

// DiagnoseDeviceAccess runs a non-writing, non-resetting host access check so
// users receive platform guidance before starting an ESP ROM probe.
func (a *App) DiagnoseDeviceAccess(port string) device.HostAccess {
	return device.DiagnoseAccess(port)
}

func (a *App) ListRecoveryRequired() []jobs.RecoveryItem {
	items, err := jobs.ListRecoveryRequired(a.logsPath())
	if err != nil {
		return nil
	}
	a.recovery = items
	return append([]jobs.RecoveryItem(nil), items...)
}

// ListRecentJobSummaries exposes only terminal task metadata. Detailed events
// remain behind the separately validated GetJobLogPage API.
func (a *App) ListRecentJobSummaries() ([]logging.JobSummary, error) {
	return logging.ReadRecentSummaries(a.logsPath(), 20)
}

// ListRecentJobSnapshots restores active and recently terminal jobs after a
// WebView refresh. The UI can render the latest durable state immediately and
// then use GetJobLogPage for incremental logs.
func (a *App) ListRecentJobSnapshots() ([]logging.Snapshot, error) {
	return logging.ReadRecentSnapshots(a.logsPath(), 20)
}

// GetJobSnapshot is the restart-safe state read for one validated job ID. The
// event log remains the detailed source; this compact snapshot lets a WebView
// reconnect without inferring terminal state from old events.
func (a *App) GetJobSnapshot(jobID string) (logging.Snapshot, error) {
	return logging.ReadSnapshot(a.logsPath(), jobID)
}

// GetJobWatchSnapshot is the ordered recovery point for the live job event
// stream. Clients use NextSequence as the durable cursor, then page logs after
// it; a delayed Wails event with an equal or lower sequence is stale.
func (a *App) GetJobWatchSnapshot(jobID string) (logging.WatchSnapshot, error) {
	return logging.ReadWatchSnapshot(a.logsPath(), jobID)
}

// ListSupportedBoards returns the fixed official Release asset allow-list. USB VID/PID is not a board identity proof.
func (a *App) ListSupportedBoards() []catalog.BoardProfile { return catalog.Profiles() }

// GetLatestFirmware discovers the latest official release and downloads only its allow-listed asset.
func (a *App) GetLatestFirmware(boardID string) (catalog.DownloadedRelease, error) {
	return a.GetLatestFirmwareForChannel(boardID, string(catalog.StableChannel))
}

// GetLatestFirmwareForChannel is the explicit UI boundary for firmware
// channels.  Stable is the default, while beta requires a deliberate user
// selection and still traverses the identical package verification path.
func (a *App) GetLatestFirmwareForChannel(boardID, requestedChannel string) (catalog.DownloadedRelease, error) {
	if releaseBuild != "true" {
		return catalog.DownloadedRelease{}, errOfficialBuildRequired
	}
	channel := catalog.ReleaseChannel(requestedChannel)
	job, err := jobs.NewDownloadJobForChannel(a.logsPath(), a.firmwareCachePath(), boardID, channel, releaseTrustStore(), a.emitLog)
	if err != nil {
		return catalog.DownloadedRelease{}, err
	}
	result, err := job.Run(context.Background())
	if err != nil {
		return result, err
	}
	return a.registerVerifiedPackage(result)
}

// ImportFirmwarePackage opens a native file chooser for an offline .clawfw
// package, then performs the same archive/signature/install-plan validation as
// a downloaded release. The chosen path is not accepted from the frontend.
func (a *App) ImportFirmwarePackage(boardID, locale string) (catalog.DownloadedRelease, error) {
	return a.ImportFirmwarePackageForChannel(boardID, locale, string(catalog.StableChannel))
}

// ImportFirmwarePackageForChannel keeps offline signed packages subject to the
// same explicit stable/beta choice as network discovery. A beta archive cannot
// bypass the stable default merely because it was selected from disk.
func (a *App) ImportFirmwarePackageForChannel(boardID, locale, requestedChannel string) (catalog.DownloadedRelease, error) {
	if releaseBuild != "true" {
		return catalog.DownloadedRelease{}, errOfficialBuildRequired
	}
	if _, err := catalog.Profile(boardID); err != nil {
		return catalog.DownloadedRelease{}, err
	}
	if a.ctx == nil {
		return catalog.DownloadedRelease{}, fmt.Errorf("native file chooser is unavailable before application startup")
	}
	copy := dialogCopyFor(locale)
	path, err := wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{Title: copy.importTitle, Filters: []wailsruntime.FileFilter{{DisplayName: copy.firmwareFilter, Pattern: "*.clawfw"}}})
	if err != nil {
		return catalog.DownloadedRelease{}, fmt.Errorf("choose firmware package: %w", err)
	}
	if path == "" {
		return catalog.DownloadedRelease{}, fmt.Errorf("firmware package selection was cancelled")
	}
	job, err := jobs.NewImportJobForChannel(a.logsPath(), boardID, path, catalog.ReleaseChannel(requestedChannel), releaseTrustStore(), a.emitLog)
	if err != nil {
		return catalog.DownloadedRelease{}, err
	}
	result, err := job.Run(context.Background())
	if err != nil {
		return result, err
	}
	return a.registerVerifiedPackage(result)
}

// FlashFirmware accepts only a capability minted after a successful official
// download or native-dialog import and an opaque confirmation minted after a
// successful probe plus a physical board-label confirmation. The browser
// cannot name an arbitrary local package path or bypass board confirmation.
// FlashJob repeats signature and hardware checks just before the irreversible
// write.
func (a *App) FlashFirmware(requestID, port, boardID, packageRef, confirmationRef string) (jobs.FlashResult, error) {
	if releaseBuild != "true" {
		return jobs.FlashResult{}, errOfficialBuildRequired
	}
	return a.runWriteRequest(requestID, "flash", port, boardID, packageRef, confirmationRef, "", func() (jobs.FlashResult, error) {
		return a.startNormalFlash(port, boardID, packageRef, confirmationRef, "")
	})
}

// RetryJob starts a new, fully preflighted write for a job that failed before
// its irreversible-write boundary. It is deliberately not a resume operation:
// callers must supply a fresh board confirmation and a currently verified
// package capability. A recovery-required original job is never retried here;
// it must use either the no-write VerifyBoot path (with complete readback
// evidence) or an explicit full ROM recovery.
//
// requestID makes a lost RPC response safe to retry. The returned JobID always
// identifies the new attempt; originalJobID is retained only in its audit log.
func (a *App) RetryJob(requestID, originalJobID, port, boardID, packageRef, confirmationRef string) (jobs.FlashResult, error) {
	if releaseBuild != "true" {
		return jobs.FlashResult{}, errOfficialBuildRequired
	}
	if !safeRequestID(requestID) || !logging.SafeJobID(originalJobID) || strings.TrimSpace(port) == "" {
		return jobs.FlashResult{}, fmt.Errorf("a valid request ID, original job ID, and serial port are required")
	}
	return a.runWriteRequest(requestID, "retry", port, boardID, packageRef, confirmationRef, originalJobID, func() (jobs.FlashResult, error) {
		if err := a.validateRetryableJob(originalJobID); err != nil {
			return jobs.FlashResult{}, err
		}
		return a.startNormalFlash(port, boardID, packageRef, confirmationRef, originalJobID)
	})
}

// validateRetryableJob fail-closes from the durable snapshot and recovery
// journal. A generic failed snapshot is safe to retry only when no evidence
// shows that the original attempt crossed the irreversible-write boundary.
func (a *App) validateRetryableJob(jobID string) error {
	snapshot, err := logging.ReadSnapshot(a.logsPath(), jobID)
	if err != nil {
		return fmt.Errorf("read original job state before retry: %w", err)
	}
	if snapshot.Status == "recovery_required" {
		journal, journalErr := jobs.ReadJournal(a.logsPath(), jobID)
		if journalErr == nil && jobs.HasCompleteReadbackEvidence(journal) {
			return fmt.Errorf("retry is unavailable: this job has complete ROM readback evidence; use VerifyBoot instead of writing Flash again")
		}
		return fmt.Errorf("retry is unavailable: this job requires an explicit complete ROM recovery")
	}
	if snapshot.Status != "failed" {
		return fmt.Errorf("retry is available only for a failed job that has stopped before writing")
	}
	items, err := jobs.ListRecoveryRequired(a.logsPath())
	if err != nil {
		return fmt.Errorf("inspect recovery state before retry: %w", err)
	}
	for _, item := range items {
		if item.JobID == jobID {
			return fmt.Errorf("retry is unavailable: durable write evidence requires explicit complete ROM recovery")
		}
	}
	return nil
}

// startNormalFlash is shared by a first write and an eligible retry. It is
// intentionally the only route that consumes a board confirmation, preserving
// the same fresh ROM device-binding check for both paths.
func (a *App) startNormalFlash(port, boardID, packageRef, confirmationRef, retryOfJobID string) (jobs.FlashResult, error) {
	if items := a.ListRecoveryRequired(); len(items) != 0 {
		return jobs.FlashResult{}, fmt.Errorf("recovery is required for %d unfinished write job(s); inspect diagnostics and perform a complete ROM recovery before starting a new flash", len(items))
	}
	if _, err := a.lookupVerifiedPackage(packageRef); err != nil {
		return jobs.FlashResult{}, err
	}
	confirmation, err := a.consumeBoardConfirmation(confirmationRef, port, boardID)
	if err != nil {
		return jobs.FlashResult{}, err
	}
	return a.flashVerifiedFirmware(port, boardID, packageRef, confirmation.deviceBinding, false, retryOfJobID)
}

// RecoverFirmware performs one explicitly selected full recovery operation for
// a blocked journal. It rejects app-only packages and only removes the
// recovery block after FlashJob has verified boot successfully.
func (a *App) RecoverFirmware(requestID, recoveryJobID, port, boardID, packageRef, confirmationRef string) (jobs.FlashResult, error) {
	if releaseBuild != "true" {
		return jobs.FlashResult{}, errOfficialBuildRequired
	}
	return a.runWriteRequest(requestID, "recovery", port, boardID, packageRef, confirmationRef, recoveryJobID, func() (jobs.FlashResult, error) {
		confirmation, err := a.consumeBoardConfirmation(confirmationRef, port, boardID)
		if err != nil {
			return jobs.FlashResult{}, err
		}
		if !logging.SafeJobID(recoveryJobID) {
			return jobs.FlashResult{}, fmt.Errorf("invalid recovery job ID")
		}
		items := a.ListRecoveryRequired()
		found := false
		for _, item := range items {
			if item.JobID == recoveryJobID {
				found = true
				break
			}
		}
		if !found {
			return jobs.FlashResult{}, fmt.Errorf("recovery job is no longer pending")
		}
		verified, err := a.lookupVerifiedPackage(packageRef)
		if err != nil || verified.boardID != boardID {
			if err != nil {
				return jobs.FlashResult{}, err
			}
			return jobs.FlashResult{}, fmt.Errorf("verified firmware reference does not match the selected board")
		}
		manifest, err := firmware.VerifyRelease(verified.path, releaseTrustStore())
		if err != nil {
			return jobs.FlashResult{}, fmt.Errorf("verify recovery package: %w", err)
		}
		if manifest.Manifest.Mode != "full" {
			return jobs.FlashResult{}, fmt.Errorf("recovery requires a verified full firmware package; app-only packages are not permitted")
		}
		result, err := a.flashVerifiedFirmware(port, boardID, packageRef, confirmation.deviceBinding, true, "")
		if err != nil {
			return result, err
		}
		if err := jobs.MarkRecoveryResolved(a.logsPath(), recoveryJobID); err != nil {
			return result, fmt.Errorf("record verified recovery completion: %w", err)
		}
		return result, nil
	})
}

// VerifyBoot repeats only the nonce-bound application BOOT_STATUS check for a
// recovery-required job whose entire signed image plan already passed ROM
// readback verification. It cannot open ROM download mode, erase, write, or
// reset Flash. A successful result clears the original recovery block; any
// failure leaves it in place and continues to require complete ROM recovery.
func (a *App) VerifyBoot(requestID, recoveryJobID, port, packageRef string) (jobs.BootVerificationResult, error) {
	if releaseBuild != "true" {
		return jobs.BootVerificationResult{}, errOfficialBuildRequired
	}
	if !safeRequestID(requestID) || !logging.SafeJobID(recoveryJobID) || strings.TrimSpace(port) == "" {
		return jobs.BootVerificationResult{}, fmt.Errorf("a valid request ID, recovery job ID, and serial port are required")
	}
	// Check the idempotency record before reading recovery state or the package
	// capability. A successful first call clears the recovery lock and may
	// revoke/expire UI capabilities before its RPC response reaches the
	// renderer; retrying that exact request must still return the same result.
	fingerprint := recoveryJobID + "\x00" + port + "\x00" + packageRef
	a.bootVerifyMu.Lock()
	if a.bootVerifies == nil {
		a.bootVerifies = make(map[string]*bootVerifyRequest)
	}
	if existing, ok := a.bootVerifies[requestID]; ok {
		if existing.fingerprint != fingerprint {
			a.bootVerifyMu.Unlock()
			return jobs.BootVerificationResult{}, fmt.Errorf("boot verification request ID was already used for a different operation")
		}
		done := existing.done
		a.bootVerifyMu.Unlock()
		<-done
		return existing.result, existing.err
	}
	a.bootVerifyMu.Unlock()
	if found := func() bool {
		for _, item := range a.ListRecoveryRequired() {
			if item.JobID == recoveryJobID {
				return true
			}
		}
		return false
	}(); !found {
		return jobs.BootVerificationResult{}, fmt.Errorf("recovery job is no longer pending")
	}
	verified, err := a.lookupVerifiedPackage(packageRef)
	if err != nil {
		return jobs.BootVerificationResult{}, err
	}
	a.bootVerifyMu.Lock()
	// A concurrent caller can only create the same request after the first
	// preflight above, so repeat the exact lookup under the mutex.
	if existing, ok := a.bootVerifies[requestID]; ok {
		if existing.fingerprint != fingerprint {
			a.bootVerifyMu.Unlock()
			return jobs.BootVerificationResult{}, fmt.Errorf("boot verification request ID was already used for a different operation")
		}
		done := existing.done
		a.bootVerifyMu.Unlock()
		<-done
		return existing.result, existing.err
	}
	request := &bootVerifyRequest{fingerprint: fingerprint, done: make(chan struct{})}
	a.bootVerifies[requestID] = request
	a.bootVerifyMu.Unlock()

	result, runErr := jobs.VerifyBootRetry(context.Background(), a.logsPath(), recoveryJobID, port, verified.path, releaseTrustStore(), a.emitLog)
	a.bootVerifyMu.Lock()
	request.result, request.err = result, runErr
	close(request.done)
	a.bootVerifyMu.Unlock()
	return result, runErr
}

// PrepareJob freezes the user-confirmed write inputs into a short-lived,
// single-use plan. It performs only capability checks and never opens a serial
// port or writes a device. StartJob must receive the exact returned hash.
func (a *App) PrepareJob(requestID, port, boardID, packageRef, confirmationRef, recoveryJobID string) (FlashPlan, error) {
	if releaseBuild != "true" {
		return FlashPlan{}, errOfficialBuildRequired
	}
	if !safeRequestID(requestID) || strings.TrimSpace(port) == "" {
		return FlashPlan{}, fmt.Errorf("a valid request ID and serial port are required")
	}
	if _, err := catalog.Profile(boardID); err != nil {
		return FlashPlan{}, err
	}
	verified, err := a.lookupVerifiedPackage(packageRef)
	if err != nil {
		return FlashPlan{}, err
	}
	if verified.boardID != boardID {
		return FlashPlan{}, fmt.Errorf("verified firmware reference does not match the selected board")
	}
	if _, err := a.validateBoardConfirmation(confirmationRef, port, boardID); err != nil {
		return FlashPlan{}, err
	}
	recovery := recoveryJobID != ""
	if recovery {
		if !logging.SafeJobID(recoveryJobID) {
			return FlashPlan{}, fmt.Errorf("invalid recovery job ID")
		}
		found := false
		for _, item := range a.ListRecoveryRequired() {
			if item.JobID == recoveryJobID {
				found = true
				break
			}
		}
		if !found {
			return FlashPlan{}, fmt.Errorf("recovery job is no longer pending")
		}
		manifest, verifyErr := firmware.VerifyRelease(verified.path, releaseTrustStore())
		if verifyErr != nil || manifest.Manifest.Mode != firmware.ModeFull {
			return FlashPlan{}, fmt.Errorf("recovery requires a verified full firmware package")
		}
	} else if items := a.ListRecoveryRequired(); len(items) != 0 {
		return FlashPlan{}, fmt.Errorf("recovery is required for %d unfinished write job(s)", len(items))
	}
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return FlashPlan{}, fmt.Errorf("create flash plan: %w", err)
	}
	planID := "plan-" + hex.EncodeToString(bytes)
	expiresAt := time.Now().UTC().Add(boardConfirmationTTL)
	planHashInput := strings.Join([]string{planID, requestID, port, boardID, packageRef, recoveryJobID, expiresAt.Format(time.RFC3339Nano)}, "\x00")
	planHash := sha256.Sum256([]byte(planHashInput))
	plan := &preparedFlashPlan{FlashPlan: FlashPlan{
		PlanID:            planID,
		PlanHash:          hex.EncodeToString(planHash[:]),
		Port:              port,
		BoardID:           boardID,
		InstallPlan:       verified.installPlan,
		PreservesUserData: verified.preservesUserData,
		RequiresRecovery:  verified.requiresRecovery,
		Recovery:          recovery,
		ExpiresAt:         expiresAt,
	}, packageRef: packageRef, confirmationRef: confirmationRef, recoveryJobID: recoveryJobID, requestID: requestID}
	a.plansMu.Lock()
	for id, existing := range a.plans {
		if time.Now().After(existing.ExpiresAt) {
			delete(a.plans, id)
		}
	}
	a.plans[planID] = plan
	a.plansMu.Unlock()
	return plan.FlashPlan, nil
}

// StartJob is idempotent for the same PlanID + PlanHash. A renderer may retry
// when its original RPC response was lost; concurrent callers wait for the
// one controlled write rather than consuming the physical confirmation twice.
func (a *App) StartJob(planID, planHash string) (jobs.FlashResult, error) {
	a.plansMu.Lock()
	plan, ok := a.plans[planID]
	if ok && plan.PlanHash != planHash {
		a.plansMu.Unlock()
		return jobs.FlashResult{}, fmt.Errorf("flash plan hash does not match")
	}
	if ok && plan.start != nil {
		start := plan.start
		a.plansMu.Unlock()
		<-start.done
		return start.result, start.err
	}
	if ok && time.Now().After(plan.ExpiresAt) {
		delete(a.plans, planID)
		ok = false
	}
	if ok {
		plan.start = &planStart{done: make(chan struct{})}
	}
	a.plansMu.Unlock()
	if !ok {
		return jobs.FlashResult{}, fmt.Errorf("flash plan is unknown, expired, or changed; prepare it again")
	}
	result, err := a.runWriteRequest(plan.requestID, map[bool]string{true: "recovery", false: "flash"}[plan.Recovery], plan.Port, plan.BoardID, plan.packageRef, plan.confirmationRef, plan.recoveryJobID, func() (jobs.FlashResult, error) {
		confirmation, err := a.consumeBoardConfirmation(plan.confirmationRef, plan.Port, plan.BoardID)
		if err != nil {
			return jobs.FlashResult{}, err
		}
		result, err := a.flashVerifiedFirmware(plan.Port, plan.BoardID, plan.packageRef, confirmation.deviceBinding, plan.Recovery, "")
		if err != nil || !plan.Recovery {
			return result, err
		}
		if markErr := jobs.MarkRecoveryResolved(a.logsPath(), plan.recoveryJobID); markErr != nil {
			return result, fmt.Errorf("record verified recovery completion: %w", markErr)
		}
		return result, nil
	})
	a.plansMu.Lock()
	plan.start.result, plan.start.err = result, err
	close(plan.start.done)
	a.plansMu.Unlock()
	return result, err
}

// runWriteRequest makes command retries idempotent during the desktop session.
// A duplicate request ID with identical immutable inputs waits for the first
// operation and returns exactly its result. Reusing an ID with different
// inputs is rejected rather than accidentally directing a completed request
// toward another device or package.
func (a *App) runWriteRequest(requestID, kind, port, boardID, packageRef, confirmationRef, recoveryJobID string, run func() (jobs.FlashResult, error)) (jobs.FlashResult, error) {
	if !safeRequestID(requestID) {
		return jobs.FlashResult{}, fmt.Errorf("a valid write request ID is required")
	}
	fingerprint := kind + "\x00" + port + "\x00" + boardID + "\x00" + packageRef + "\x00" + confirmationRef + "\x00" + recoveryJobID
	a.writeRequestsMu.Lock()
	if a.writeRequests == nil {
		a.writeRequests = make(map[string]*writeRequest)
	}
	if existing, ok := a.writeRequests[requestID]; ok {
		if existing.fingerprint != fingerprint {
			a.writeRequestsMu.Unlock()
			return jobs.FlashResult{}, fmt.Errorf("write request ID was already used for a different operation")
		}
		done := existing.done
		a.writeRequestsMu.Unlock()
		<-done
		return existing.result, existing.err
	}
	request := &writeRequest{fingerprint: fingerprint, done: make(chan struct{})}
	a.writeRequests[requestID] = request
	a.writeRequestsMu.Unlock()

	result, err := run()
	a.writeRequestsMu.Lock()
	request.result, request.err = result, err
	close(request.done)
	a.writeRequestsMu.Unlock()
	return result, err
}

func safeRequestID(value string) bool {
	if len(value) < 16 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func (a *App) flashVerifiedFirmware(port, boardID, packageRef, expectedDeviceBinding string, recovery bool, retryOfJobID string) (jobs.FlashResult, error) {
	profile, err := catalog.Profile(boardID)
	if err != nil {
		return jobs.FlashResult{}, err
	}
	if port == "" || packageRef == "" {
		return jobs.FlashResult{}, fmt.Errorf("port and verified firmware reference are required")
	}
	verified, err := a.lookupVerifiedPackage(packageRef)
	if err != nil {
		return jobs.FlashResult{}, err
	}
	if verified.boardID != boardID {
		return jobs.FlashResult{}, fmt.Errorf("verified firmware reference does not match the selected board")
	}
	job, err := jobs.NewFlashJob(a.logsPath(), jobs.FlashRequest{Port: port, PackagePath: verified.path, Trust: releaseTrustStore(), ExpectedChip: "esp32-s3", ExpectedFlashBytes: 16 * 1024 * 1024, ExpectedDeviceBinding: expectedDeviceBinding, BoardID: profile.FirmwareBoardID, Recovery: recovery, RetryOfJobID: retryOfJobID}, a.emitLog)
	if err != nil {
		return jobs.FlashResult{}, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.activeWritesMu.Lock()
	if a.activeWrites == nil {
		a.activeWrites = make(map[string]activeWrite)
	}
	a.activeWrites[job.JobID()] = activeWrite{cancel: cancel, job: job}
	a.activeWritesMu.Unlock()
	defer func() {
		cancel()
		a.activeWritesMu.Lock()
		delete(a.activeWrites, job.JobID())
		a.activeWritesMu.Unlock()
	}()
	result, err := job.Run(ctx)
	if err == nil && result.Status == "succeeded" {
		// A completed package reference must not be silently reused for a
		// second write. runWriteRequest retains the completed result, so a
		// transport retry with the same request ID remains idempotent.
		a.revokeVerifiedPackage(packageRef)
	}
	return result, err
}

// CancelJob requests cancellation of the active in-process operation. The
// job's context owns the sidecar process tree, so this is not a best-effort UI
// flag: esptool is stopped before the call returns. If writing has begun,
// FlashJob persists the recovery journal and normal flashing remains blocked.
func (a *App) CancelJob(jobID string) error {
	if !logging.SafeJobID(jobID) {
		return fmt.Errorf("invalid job ID")
	}
	a.activeWritesMu.Lock()
	active, ok := a.activeWrites[jobID]
	a.activeWritesMu.Unlock()
	if !ok {
		// A cancellation response may be lost after the sidecar has already
		// stopped and the active entry has been removed. Treat a durable
		// terminal snapshot as an accepted no-op so callers can retry the
		// exact CancelJob RPC without being told that its effect is unknown.
		if snapshot, err := logging.ReadSnapshot(a.logsPath(), jobID); err == nil && snapshot.Status != "running" {
			return nil
		}
		return fmt.Errorf("job is not active or has already finished")
	}
	if active.job != nil {
		active.job.RequestCancel()
	}
	active.cancel()
	return nil
}

func (a *App) registerVerifiedPackage(result catalog.DownloadedRelease) (catalog.DownloadedRelease, error) {
	if result.InstallStatus != "verified_ready" || result.Path == "" || result.BoardID == "" {
		return catalog.DownloadedRelease{}, fmt.Errorf("firmware package was not verified and ready")
	}
	if strings.TrimSpace(result.Channel) == "" {
		return catalog.DownloadedRelease{}, fmt.Errorf("verified firmware package is missing a supported channel")
	}
	if _, err := catalog.NormalizeReleaseChannel(catalog.ReleaseChannel(result.Channel)); err != nil {
		return catalog.DownloadedRelease{}, fmt.Errorf("verified firmware package is missing a supported channel: %w", err)
	}
	if _, err := catalog.Profile(result.BoardID); err != nil {
		return catalog.DownloadedRelease{}, err
	}
	expectedSHA256, err := normalizedSHA256(result.SHA256)
	if err != nil {
		return catalog.DownloadedRelease{}, fmt.Errorf("verified firmware package is missing a valid SHA-256: %w", err)
	}
	actualSHA256, err := archiveSHA256(result.Path)
	if err != nil {
		return catalog.DownloadedRelease{}, fmt.Errorf("hash verified firmware package: %w", err)
	}
	if actualSHA256 != expectedSHA256 {
		return catalog.DownloadedRelease{}, fmt.Errorf("verified firmware package changed before registration")
	}
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return catalog.DownloadedRelease{}, fmt.Errorf("create verified firmware reference: %w", err)
	}
	ref := "fwref-" + hex.EncodeToString(bytes)
	a.packageRefsMu.Lock()
	if a.verifiedPackage == nil {
		a.verifiedPackage = make(map[string]verifiedPackage)
	}
	a.verifiedPackage[ref] = verifiedPackage{
		path:              result.Path,
		boardID:           result.BoardID,
		archiveSHA256:     actualSHA256,
		installPlan:       result.InstallPlan,
		preservesUserData: result.PreservesUserData,
		requiresRecovery:  result.RequiresRecovery,
		issuedAt:          time.Now().UTC(),
	}
	a.packageRefsMu.Unlock()
	result.PackageRef = ref
	return result, nil
}

func (a *App) lookupVerifiedPackage(ref string) (verifiedPackage, error) {
	a.packageRefsMu.Lock()
	result, ok := a.verifiedPackage[ref]
	a.packageRefsMu.Unlock()
	if !ok {
		return verifiedPackage{}, fmt.Errorf("firmware reference is unknown, expired, or was not verified in this application session")
	}
	if time.Since(result.issuedAt) > verifiedPackageTTL {
		a.revokeVerifiedPackage(ref)
		return verifiedPackage{}, fmt.Errorf("firmware reference expired; download or import the verified package again")
	}
	actualSHA256, err := archiveSHA256(result.path)
	if err != nil {
		a.revokeVerifiedPackage(ref)
		return verifiedPackage{}, fmt.Errorf("verified firmware package is no longer readable: %w", err)
	}
	if actualSHA256 != result.archiveSHA256 {
		a.revokeVerifiedPackage(ref)
		return verifiedPackage{}, fmt.Errorf("verified firmware package changed after validation; download or import it again")
	}
	return result, nil
}

func (a *App) revokeVerifiedPackage(ref string) {
	a.packageRefsMu.Lock()
	delete(a.verifiedPackage, ref)
	a.packageRefsMu.Unlock()
}

func normalizedSHA256(value string) (string, error) {
	value = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "sha256:")
	if len(value) != sha256.Size*2 {
		return "", fmt.Errorf("expected %d hexadecimal characters", sha256.Size*2)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", fmt.Errorf("invalid hexadecimal digest")
	}
	return value, nil
}

func archiveSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ProbeDevice only executes read-only ROM commands. It never erases, writes or resets into a write mode.
func (a *App) ProbeDevice(port string) (jobs.ProbeResult, error) {
	if port == "" {
		return jobs.ProbeResult{}, fmt.Errorf("port is required")
	}
	job, err := jobs.NewProbeJob(a.logsPath(), port, a.emitLog)
	if err != nil {
		return jobs.ProbeResult{}, err
	}
	result, err := job.Run(context.Background())
	if result.BoardRecognition.Status == "" {
		result.BoardRecognition = catalog.RecognizeProbe(result.Chip, result.Flash)
	}
	return result, err
}

// ConfirmBoard mints the final write-authority input only after a successful
// fresh probe of the same port. The confirmation ref is opaque, short lived,
// single-use, and remains bound to the selected board and serial endpoint.
// Probe evidence informs the choice but never replaces the user's physical
// board-label confirmation.
func (a *App) ConfirmBoard(port, boardID, probeJobID string) (BoardConfirmation, error) {
	if releaseBuild != "true" {
		return BoardConfirmation{}, errOfficialBuildRequired
	}
	if strings.TrimSpace(port) == "" || strings.TrimSpace(probeJobID) == "" {
		return BoardConfirmation{}, fmt.Errorf("port and successful probe job ID are required to confirm a board")
	}
	if _, err := catalog.Profile(boardID); err != nil {
		return BoardConfirmation{}, err
	}
	if !logging.SafeJobID(probeJobID) {
		return BoardConfirmation{}, fmt.Errorf("invalid probe job ID")
	}
	probe, err := a.readSuccessfulProbe(probeJobID)
	if err != nil {
		return BoardConfirmation{}, err
	}
	if probe.Port != port {
		return BoardConfirmation{}, fmt.Errorf("probe evidence belongs to a different serial port")
	}
	if time.Since(probe.FinishedAt) > boardConfirmationTTL {
		return BoardConfirmation{}, fmt.Errorf("probe evidence expired; run the read-only check again before confirming the board")
	}
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return BoardConfirmation{}, fmt.Errorf("create board confirmation: %w", err)
	}
	ref := "boardref-" + hex.EncodeToString(bytes)
	issuedAt := time.Now().UTC()
	a.confirmationsMu.Lock()
	if a.confirmations == nil {
		a.confirmations = make(map[string]boardConfirmation)
	}
	for key, confirmation := range a.confirmations {
		if time.Since(confirmation.issuedAt) > boardConfirmationTTL {
			delete(a.confirmations, key)
		}
	}
	a.confirmations[ref] = boardConfirmation{port: port, boardID: boardID, probeJobID: probeJobID, deviceBinding: probe.DeviceBinding, issuedAt: issuedAt}
	a.confirmationsMu.Unlock()
	return BoardConfirmation{ConfirmationRef: ref, Port: port, BoardID: boardID, ExpiresAt: issuedAt.Add(boardConfirmationTTL)}, nil
}

// ConfirmDetectedBoard turns a unique, nonce-bound application identity from a
// fresh probe into the same short-lived write capability used by the legacy
// manual path. The frontend still shows the detected board in the final flash
// confirmation; this method simply removes the error-prone board-picker step.
func (a *App) ConfirmDetectedBoard(port, probeJobID string) (BoardConfirmation, error) {
	if releaseBuild != "true" {
		return BoardConfirmation{}, errOfficialBuildRequired
	}
	probe, err := a.readSuccessfulProbe(probeJobID)
	if err != nil {
		return BoardConfirmation{}, err
	}
	if probe.Port != port {
		return BoardConfirmation{}, fmt.Errorf("probe evidence belongs to a different serial port")
	}
	recognition := probe.BoardRecognition
	if recognition.Status != "probable" && probe.AppIdentity.FirmwareTargetBoardID != "" {
		recognition = catalog.RecognizeApplicationIdentityEvidence(probe.AppIdentity)
	}
	if recognition.Status != "probable" || len(recognition.CandidateBoards) != 1 {
		return BoardConfirmation{}, fmt.Errorf("the selected device did not report one uniquely supported board identity")
	}
	return a.ConfirmBoard(port, recognition.CandidateBoards[0], probeJobID)
}
func (a *App) readSuccessfulProbe(jobID string) (jobs.ProbeResult, error) {
	jobRoot := filepath.Join(a.logsPath(), jobID)
	path := filepath.Join(jobRoot, "summary.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return jobs.ProbeResult{}, fmt.Errorf("read probe evidence: %w", err)
	}
	var probe jobs.ProbeResult
	if err := json.Unmarshal(raw, &probe); err != nil {
		return jobs.ProbeResult{}, fmt.Errorf("parse probe evidence: %w", err)
	}
	if probe.JobID != jobID || probe.Status != "succeeded" || probe.Port == "" || probe.FinishedAt.IsZero() {
		return jobs.ProbeResult{}, fmt.Errorf("probe evidence is not a successful completed read-only probe")
	}
	if strings.TrimSpace(probe.DeviceBinding) == "" {
		return jobs.ProbeResult{}, fmt.Errorf("probe evidence has no ROM device identity")
	}
	return probe, nil
}

func (a *App) consumeBoardConfirmation(ref, port, boardID string) (boardConfirmation, error) {
	if _, err := a.validateBoardConfirmation(ref, port, boardID); err != nil {
		return boardConfirmation{}, err
	}
	a.confirmationsMu.Lock()
	defer a.confirmationsMu.Unlock()
	confirmation := a.confirmations[ref]
	delete(a.confirmations, ref)
	return confirmation, nil
}

// validateBoardConfirmation checks a confirmation without consuming it. A
// prepared plan is allowed to expire; only StartJob spends the single-use
// physical-board confirmation.
func (a *App) validateBoardConfirmation(ref, port, boardID string) (boardConfirmation, error) {
	if strings.TrimSpace(ref) == "" {
		return boardConfirmation{}, fmt.Errorf("a fresh physical board confirmation is required before flashing")
	}
	a.confirmationsMu.Lock()
	defer a.confirmationsMu.Unlock()
	confirmation, ok := a.confirmations[ref]
	if !ok {
		return boardConfirmation{}, fmt.Errorf("board confirmation is unknown, expired, or already used; confirm the physical board again")
	}
	if time.Since(confirmation.issuedAt) > boardConfirmationTTL {
		delete(a.confirmations, ref)
		return boardConfirmation{}, fmt.Errorf("board confirmation expired; run the read-only check and confirm the physical board again")
	}
	if confirmation.port != port || confirmation.boardID != boardID {
		return boardConfirmation{}, fmt.Errorf("board confirmation does not match the selected port and board")
	}
	return confirmation, nil
}

func (a *App) GetJobLogPage(jobID string, after uint64, limit int) (logging.Page, error) {
	return a.GetJobLogPageFiltered(jobID, after, limit, logging.Filter{})
}

// GetJobLogPageFiltered exposes only a fixed structured filter for the
// already-authorized job. It deliberately does not accept paths, regexes, or
// raw-file controls from the WebView.
func (a *App) GetJobLogPageFiltered(jobID string, after uint64, limit int, filter logging.Filter) (logging.Page, error) {
	if !logging.SafeJobID(jobID) {
		return logging.Page{}, fmt.Errorf("invalid job ID")
	}
	if err := filter.Validate(); err != nil {
		return logging.Page{}, err
	}
	return logging.ReadPageFiltered(filepath.Join(a.logsPath(), jobID), after, limit, filter)
}

// ExportJobDiagnostics opens a native directory chooser, then exports a
// fixed, re-redacted bundle. The browser never supplies an arbitrary path.
func (a *App) ExportJobDiagnostics(jobID, locale string) (diagnostics.Bundle, error) {
	if a.ctx == nil {
		return diagnostics.Bundle{}, fmt.Errorf("native directory chooser is unavailable before application startup")
	}
	destination, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{Title: dialogCopyFor(locale).diagnosticsDirectoryTitle})
	if err != nil {
		return diagnostics.Bundle{}, fmt.Errorf("choose diagnostics directory: %w", err)
	}
	if destination == "" {
		return diagnostics.Bundle{}, fmt.Errorf("diagnostics export was cancelled")
	}
	return diagnostics.ExportJob(a.logsPath(), jobID, destination)
}

// dialogCopy is deliberately a small closed set. The WebView may choose the
// display locale, but it must not control native-dialog titles or filters.
// Keeping these strings at the backend boundary ensures system UI follows the
// chosen language without accepting arbitrary text across the Wails bridge.
type dialogCopy struct {
	importTitle               string
	firmwareFilter            string
	diagnosticsDirectoryTitle string
}

func dialogCopyFor(locale string) dialogCopy {
	switch strings.ToLower(strings.TrimSpace(locale)) {
	case "zh-tw", "zh-hant", "zh-hk":
		return dialogCopy{
			importTitle:               "選擇已簽署的 ClawMate 韌體套件",
			firmwareFilter:            "ClawMate 韌體套件 (*.clawfw)",
			diagnosticsDirectoryTitle: "選擇診斷套件儲存資料夾",
		}
	case "en", "en-us", "en-gb":
		return dialogCopy{
			importTitle:               "Choose a signed ClawMate firmware package",
			firmwareFilter:            "ClawMate firmware package (*.clawfw)",
			diagnosticsDirectoryTitle: "Choose a folder for the diagnostic bundle",
		}
	default:
		return dialogCopy{
			importTitle:               "选择已签名的 ClawMate 固件包",
			firmwareFilter:            "ClawMate 固件包 (*.clawfw)",
			diagnosticsDirectoryTitle: "选择诊断包保存文件夹",
		}
	}
}

func (a *App) logsPath() string {
	if a.logRoot != "" {
		return a.logRoot
	}
	base, err := logging.DefaultRoot()
	if err != nil {
		return filepath.Join(".", "logs", "jobs")
	}
	a.logRoot = filepath.Join(base, "jobs")
	return a.logRoot
}

func (a *App) firmwareCachePath() string {
	base, err := logging.DefaultRoot()
	if err != nil {
		return filepath.Join(".", "cache", "firmware")
	}
	return filepath.Join(filepath.Dir(base), "firmware")
}

func (a *App) emitLog(event logging.Event) {
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "job:log", event)
	}
}

// Keep time linked in release builds until job history is added to the UI.
var _ = time.Second
