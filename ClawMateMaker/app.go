package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
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
	ctx      context.Context
	logRoot  string
	recovery []jobs.RecoveryItem
}

func NewApp() *App { return &App{} }

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

func (a *App) GetAppInfo() AppInfo {
	return AppInfo{Name: "ClawMate Maker", Version: "0.1.0-dev", Platform: goruntime.GOOS + "/" + goruntime.GOARCH, LogRoot: a.logsPath(), ProbeOnly: false}
}

func (a *App) ListDevices() ([]device.Candidate, error) { return device.ListCandidates() }

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

// ListSupportedBoards returns the fixed official Release asset allow-list. USB VID/PID is not a board identity proof.
func (a *App) ListSupportedBoards() []catalog.BoardProfile { return catalog.Profiles() }

// GetLatestFirmware discovers the latest official release and downloads only its allow-listed asset.
func (a *App) GetLatestFirmware(boardID string) (catalog.DownloadedRelease, error) {
	job, err := jobs.NewDownloadJob(a.logsPath(), a.firmwareCachePath(), boardID, releaseTrustStore(), a.emitLog)
	if err != nil {
		return catalog.DownloadedRelease{}, err
	}
	return job.Run(context.Background())
}

// ImportFirmwarePackage opens a native file chooser for an offline .clawfw
// package, then performs the same archive/signature/install-plan validation as
// a downloaded release. The chosen path is not accepted from the frontend.
func (a *App) ImportFirmwarePackage(boardID string) (catalog.DownloadedRelease, error) {
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
	return job.Run(context.Background())
}

// FlashFirmware keeps user-controlled strings out of the flash command line.
// The downloaded package has already been checked, and FlashJob repeats every
// compatibility and signature check immediately before the irreversible write.
func (a *App) FlashFirmware(port, boardID, packagePath string) (jobs.FlashResult, error) {
	if items := a.ListRecoveryRequired(); len(items) != 0 {
		return jobs.FlashResult{}, fmt.Errorf("recovery is required for %d unfinished write job(s); inspect diagnostics and perform a complete ROM recovery before starting a new flash", len(items))
	}
	profile, err := catalog.Profile(boardID)
	if err != nil {
		return jobs.FlashResult{}, err
	}
	if port == "" || packagePath == "" {
		return jobs.FlashResult{}, fmt.Errorf("port and package path are required")
	}
	job, err := jobs.NewFlashJob(a.logsPath(), jobs.FlashRequest{Port: port, PackagePath: packagePath, Trust: releaseTrustStore(), ExpectedChip: "esp32-s3", ExpectedFlashBytes: 16 * 1024 * 1024, BoardID: profile.FirmwareBoardID}, a.emitLog)
	if err != nil {
		return jobs.FlashResult{}, err
	}
	return job.Run(context.Background())
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
	return logging.ReadPage(filepath.Join(a.logsPath(), jobID), after, limit)
}

// ExportJobDiagnostics writes a fixed, re-redacted support bundle to a
// user-chosen directory. The front end cannot name arbitrary source files.
func (a *App) ExportJobDiagnostics(jobID, destinationDir string) (diagnostics.Bundle, error) {
	return diagnostics.ExportJob(a.logsPath(), jobID, destinationDir)
}

// DefaultDiagnosticsDirectory returns an existing, user-owned directory for
// support bundles. The UI can use it without requesting arbitrary source
// paths; the actual archive still contains only a fixed allow-list of
// re-redacted files from the selected job.
func (a *App) DefaultDiagnosticsDirectory() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	return filepath.Dir(a.logsPath())
}

// VerifyFirmwarePackage reports archive integrity. Official release signature
// keys are intentionally injected by a signed application build, not supplied
// by the front end, so this development shell exposes no install command yet.
func (a *App) VerifyFirmwarePackage(packagePath string) (firmware.Verified, error) {
	if packagePath == "" {
		return firmware.Verified{}, fmt.Errorf("package path is required")
	}
	return firmware.Verify(packagePath)
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
