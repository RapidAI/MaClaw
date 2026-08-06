package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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
	logRoot         string
	recovery        []jobs.RecoveryItem
	packageRefsMu   sync.Mutex
	verifiedPackage map[string]verifiedPackage
	writeRequestsMu sync.Mutex
	writeRequests   map[string]*writeRequest
}

type verifiedPackage struct {
	path          string
	boardID       string
	archiveSHA256 string
	issuedAt      time.Time
}

// verifiedPackageTTL limits an in-memory package capability to the user's
// current installation decision. FlashJob still re-verifies the signed
// archive immediately before a write; this expiry additionally prevents an
// old UI state from retaining write authority indefinitely.
const verifiedPackageTTL = 30 * time.Minute

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

func NewApp() *App {
	return &App{
		verifiedPackage: make(map[string]verifiedPackage),
		writeRequests:   make(map[string]*writeRequest),
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
}
func (a *App) shutdown(context.Context) {}

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
	Devices    []AutoDetectedDevice       `json:"devices,omitempty"`
	Reason     string                     `json:"reason,omitempty"`
}

// AutoDetectedDevice describes one independently probed USB/ESP candidate.
// It is intentionally read-only: even when a nonce-bound application identity
// picks one official asset, the caller must still make a physical board-label
// confirmation before a package capability can authorize a write.
type AutoDetectedDevice struct {
	Device   device.Candidate           `json:"device"`
	Probe    *jobs.ProbeResult          `json:"probe,omitempty"`
	Status   string                     `json:"status"`
	Reason   string                     `json:"reason,omitempty"`
	BoardID  string                     `json:"boardId,omitempty"`
	Firmware *catalog.DownloadedRelease `json:"firmware,omitempty"`
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
		item := AutoDetectedDevice{Device: candidate, Status: "probing"}
		probe, probeErr := a.ProbeDevice(candidate.Port)
		item.Probe = &probe
		if probeErr != nil {
			item.Status = "probe_failed"
			item.Reason = probe.ErrorMessage
			if item.Reason == "" {
				item.Reason = probeErr.Error()
			}
			result.Devices = append(result.Devices, item)
			continue
		}
		recognition := probe.BoardRecognition
		if recognition.Status != "probable" || len(recognition.CandidateBoards) != 1 {
			item.Status = "requires_confirmation"
			item.Reason = recognition.Reason
			result.Devices = append(result.Devices, item)
			continue
		}
		item.BoardID = recognition.CandidateBoards[0]
		firmware, downloadErr := a.GetLatestFirmware(item.BoardID)
		if downloadErr != nil {
			item.Status = "firmware_failed"
			item.Reason = downloadErr.Error()
			result.Devices = append(result.Devices, item)
			continue
		}
		item.Firmware = &firmware
		item.Status = "firmware_ready"
		item.Reason = "A signed firmware package was prepared from the device application identity. Confirm the physical board label before flashing."
		result.Devices = append(result.Devices, item)
		readyCount++
	}
	if len(result.Devices) == 1 {
		only := result.Devices[0]
		result.Device = &only.Device
		result.Probe = only.Probe
		result.Firmware = only.Firmware
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

// ListSupportedBoards returns the fixed official Release asset allow-list. USB VID/PID is not a board identity proof.
func (a *App) ListSupportedBoards() []catalog.BoardProfile { return catalog.Profiles() }

// GetLatestFirmware discovers the latest official release and downloads only its allow-listed asset.
func (a *App) GetLatestFirmware(boardID string) (catalog.DownloadedRelease, error) {
	if releaseBuild != "true" {
		return catalog.DownloadedRelease{}, errOfficialBuildRequired
	}
	job, err := jobs.NewDownloadJob(a.logsPath(), a.firmwareCachePath(), boardID, releaseTrustStore(), a.emitLog)
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
func (a *App) ImportFirmwarePackage(boardID string) (catalog.DownloadedRelease, error) {
	if releaseBuild != "true" {
		return catalog.DownloadedRelease{}, errOfficialBuildRequired
	}
	if _, err := catalog.Profile(boardID); err != nil {
		return catalog.DownloadedRelease{}, err
	}
	if a.ctx == nil {
		return catalog.DownloadedRelease{}, fmt.Errorf("native file chooser is unavailable before application startup")
	}
	path, err := wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{Title: "选择已签名的 ClawMate 固件包", Filters: []wailsruntime.FileFilter{{DisplayName: "ClawMate 固件包 (*.clawfw)", Pattern: "*.clawfw"}}})
	if err != nil {
		return catalog.DownloadedRelease{}, fmt.Errorf("choose firmware package: %w", err)
	}
	if path == "" {
		return catalog.DownloadedRelease{}, fmt.Errorf("firmware package selection was cancelled")
	}
	job, err := jobs.NewImportJob(a.logsPath(), boardID, path, releaseTrustStore(), a.emitLog)
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
// download or native-dialog import. The browser cannot name an arbitrary
// local package path. FlashJob repeats signature and hardware checks just
// before the irreversible write.
func (a *App) FlashFirmware(requestID, port, boardID, packageRef string) (jobs.FlashResult, error) {
	if releaseBuild != "true" {
		return jobs.FlashResult{}, errOfficialBuildRequired
	}
	return a.runWriteRequest(requestID, "flash", port, boardID, packageRef, "", func() (jobs.FlashResult, error) {
		if items := a.ListRecoveryRequired(); len(items) != 0 {
			return jobs.FlashResult{}, fmt.Errorf("recovery is required for %d unfinished write job(s); inspect diagnostics and perform a complete ROM recovery before starting a new flash", len(items))
		}
		return a.flashVerifiedFirmware(port, boardID, packageRef, false)
	})
}

// RecoverFirmware performs one explicitly selected full recovery operation for
// a blocked journal. It rejects app-only packages and only removes the
// recovery block after FlashJob has verified boot successfully.
func (a *App) RecoverFirmware(requestID, recoveryJobID, port, boardID, packageRef string) (jobs.FlashResult, error) {
	if releaseBuild != "true" {
		return jobs.FlashResult{}, errOfficialBuildRequired
	}
	return a.runWriteRequest(requestID, "recovery", port, boardID, packageRef, recoveryJobID, func() (jobs.FlashResult, error) {
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
		result, err := a.flashVerifiedFirmware(port, boardID, packageRef, true)
		if err != nil {
			return result, err
		}
		if err := jobs.MarkRecoveryResolved(a.logsPath(), recoveryJobID); err != nil {
			return result, fmt.Errorf("record verified recovery completion: %w", err)
		}
		return result, nil
	})
}

// runWriteRequest makes command retries idempotent during the desktop session.
// A duplicate request ID with identical immutable inputs waits for the first
// operation and returns exactly its result. Reusing an ID with different
// inputs is rejected rather than accidentally directing a completed request
// toward another device or package.
func (a *App) runWriteRequest(requestID, kind, port, boardID, packageRef, recoveryJobID string, run func() (jobs.FlashResult, error)) (jobs.FlashResult, error) {
	if !safeRequestID(requestID) {
		return jobs.FlashResult{}, fmt.Errorf("a valid write request ID is required")
	}
	fingerprint := kind + "\x00" + port + "\x00" + boardID + "\x00" + packageRef + "\x00" + recoveryJobID
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

func (a *App) flashVerifiedFirmware(port, boardID, packageRef string, recovery bool) (jobs.FlashResult, error) {
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
	job, err := jobs.NewFlashJob(a.logsPath(), jobs.FlashRequest{Port: port, PackagePath: verified.path, Trust: releaseTrustStore(), ExpectedChip: "esp32-s3", ExpectedFlashBytes: 16 * 1024 * 1024, BoardID: profile.FirmwareBoardID, Recovery: recovery}, a.emitLog)
	if err != nil {
		return jobs.FlashResult{}, err
	}
	result, err := job.Run(context.Background())
	if err == nil && result.Status == "succeeded" {
		// A completed package reference must not be silently reused for a
		// second write. runWriteRequest retains the completed result, so a
		// transport retry with the same request ID remains idempotent.
		a.revokeVerifiedPackage(packageRef)
	}
	return result, err
}

func (a *App) registerVerifiedPackage(result catalog.DownloadedRelease) (catalog.DownloadedRelease, error) {
	if result.InstallStatus != "verified_ready" || result.Path == "" || result.BoardID == "" {
		return catalog.DownloadedRelease{}, fmt.Errorf("firmware package was not verified and ready")
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
	a.verifiedPackage[ref] = verifiedPackage{path: result.Path, boardID: result.BoardID, archiveSHA256: actualSHA256, issuedAt: time.Now().UTC()}
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

func (a *App) GetJobLogPage(jobID string, after uint64, limit int) (logging.Page, error) {
	if !logging.SafeJobID(jobID) {
		return logging.Page{}, fmt.Errorf("invalid job ID")
	}
	return logging.ReadPage(filepath.Join(a.logsPath(), jobID), after, limit)
}

// ExportJobDiagnostics opens a native directory chooser, then exports a
// fixed, re-redacted bundle. The browser never supplies an arbitrary path.
func (a *App) ExportJobDiagnostics(jobID string) (diagnostics.Bundle, error) {
	if a.ctx == nil {
		return diagnostics.Bundle{}, fmt.Errorf("native directory chooser is unavailable before application startup")
	}
	destination, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{Title: "选择诊断包保存目录"})
	if err != nil {
		return diagnostics.Bundle{}, fmt.Errorf("choose diagnostics directory: %w", err)
	}
	if destination == "" {
		return diagnostics.Bundle{}, fmt.Errorf("diagnostics export was cancelled")
	}
	return diagnostics.ExportJob(a.logsPath(), jobID, destination)
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
