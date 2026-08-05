package main

import (
	"context"
	"fmt"
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
	ctx     context.Context
	logRoot string
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) { a.ctx = ctx }
func (a *App) shutdown(context.Context)    {}

type AppInfo struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Platform  string `json:"platform"`
	LogRoot   string `json:"logRoot"`
	ProbeOnly bool   `json:"probeOnly"`
}

func (a *App) GetAppInfo() AppInfo {
	return AppInfo{Name: "ClawMate Maker", Version: "0.1.0-dev", Platform: goruntime.GOOS + "/" + goruntime.GOARCH, LogRoot: a.logsPath(), ProbeOnly: true}
}

func (a *App) ListDevices() ([]device.Candidate, error) { return device.ListCandidates() }

// ListSupportedBoards returns the fixed official Release asset allow-list. USB VID/PID is not a board identity proof.
func (a *App) ListSupportedBoards() []catalog.BoardProfile { return catalog.Profiles() }

// GetLatestFirmware discovers the latest official release and downloads only its allow-listed asset.
func (a *App) GetLatestFirmware(boardID string) (catalog.DownloadedRelease, error) {
	return catalog.NewClient(a.firmwareCachePath()).DownloadLatest(context.Background(), boardID)
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
	result.BoardRecognition = catalog.RecognizeProbe(result.Chip, result.Flash)
	return result, err
}

func (a *App) GetJobLogPage(jobID string, after uint64, limit int) (logging.Page, error) {
	return logging.ReadPage(filepath.Join(a.logsPath(), jobID), after, limit)
}

// ExportJobDiagnostics writes a fixed, re-redacted support bundle to a
// user-chosen directory. The front end cannot name arbitrary source files.
func (a *App) ExportJobDiagnostics(jobID, destinationDir string) (diagnostics.Export, error) {
	return diagnostics.ExportJob(a.logsPath(), jobID, destinationDir)
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
