package guiapp

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/accessibility"
)

const yoloModelFilename = "omniparser-v2.yolow"

// yoloModelDefaultURL is the primary download source (GitHub Releases).
const yoloModelDefaultURL = "https://github.com/RapidAI/MaClaw/releases/download/Model_Release/omniparser-v2.yolow"

// yoloDownloadMu prevents concurrent YOLO model downloads.
var yoloDownloadMu sync.Mutex

// yolo download state is what the settings panel and the Computer Use banner
// read. A missing file is not a download in progress, and a failed attempt
// must stay failed until the next attempt starts.
var (
	yoloStateMu     sync.Mutex
	yoloRunning     bool
	yoloFailedMsg   string
	yoloUserStopped bool
)

var yoloModelStatusCache struct {
	sync.Mutex
	valid  bool
	path   string
	exists bool
	size   int64
}

func invalidateYOLOModelStatusCache() {
	yoloModelStatusCache.Lock()
	yoloModelStatusCache.valid = false
	yoloModelStatusCache.Unlock()
}

// yoloModelPath returns the full path to the YOLO model file.
// Uses the same active models directory as the embedding model.
func yoloModelPath() string {
	dir, err := embeddingModelsDir() // reuse — same directory
	if err != nil {
		return ""
	}
	return filepath.Join(dir, yoloModelFilename)
}

// GetScreenParsingEnabled returns the current screen parsing (YOLO) toggle state.
func (a *App) GetScreenParsingEnabled() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return true // default: enabled
	}
	// Default to true if not explicitly set
	if cfg.ScreenParsingEnabled == nil {
		return true
	}
	return *cfg.ScreenParsingEnabled
}

// SetScreenParsingEnabled persists the screen parsing toggle.
// When enabling, triggers background download if model is missing.
func (a *App) SetScreenParsingEnabled(enabled bool) error {
	if _, err := a.PatchConfigFields(map[string]interface{}{"screen_parsing_enabled": enabled}); err != nil {
		return err
	}
	invalidateYOLOModelStatusCache()

	yoloStateMu.Lock()
	if enabled {
		yoloUserStopped = false
	} else if yoloRunning {
		yoloUserStopped = true
	}
	yoloStateMu.Unlock()

	if enabled {
		go a.backgroundPreloadYOLOModel()
	}
	return nil
}

// GetComputerUseEnabled returns whether Computer Use tools/playbook injection is on.
// Default true when unset.
func (a *App) GetComputerUseEnabled() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return true
	}
	return computerUseEnabledFromConfig(&cfg)
}

// SetComputerUseEnabled persists the Computer Use product toggle.
func (a *App) SetComputerUseEnabled(enabled bool) error {
	_, err := a.PatchConfigFields(map[string]interface{}{"computer_use_enabled": enabled})
	return err
}

// GetComputerUseStatus returns a snapshot for settings / operator UI.
func (a *App) GetComputerUseStatus() map[string]interface{} {
	enabled := a.GetComputerUseEnabled()
	yoloOn := a.GetScreenParsingEnabled()
	yoloInfo := a.CheckYOLOModel()

	// session_active respects sticky TTL (lazy expiry).
	activated := computerUseSessionActive()
	globalComputerUse.mu.Lock()
	sess := globalComputerUse.session
	globalComputerUse.mu.Unlock()

	steps := 0
	refsValid := false
	elemCount := 0
	if sess != nil {
		if p := sess.Policy(); p != nil {
			steps = p.StepCount()
		}
		refsValid = sess.RefsValid()
		if last := sess.LastObserve(); last != nil {
			elemCount = len(last.Elements)
		}
	}
	paused, stopped := false, false
	if sess != nil {
		paused, stopped = sess.ControlState()
	}
	status := map[string]interface{}{
		"enabled":           enabled,
		"screen_parsing":    yoloOn,
		"yolo_model_exists": yoloInfo["exists"],
		"yolo_model_path":   yoloInfo["path"],
		"session_active":    activated,
		"step_count":        steps,
		"refs_valid":        refsValid,
		"element_count":     elemCount,
		"paused":            paused,
		"stopped":           stopped,
		"playbook":          computerusePlaybookShort(),
	}
	status["uia_sidecar_alive"] = accessibility.UIASidecarAlive()
	status["uia_sidecar_backend"] = accessibility.UIASidecarBackend()
	// Last startup/self-check snapshot (ok/backend/ms) when available.
	if warm := a.GetComputerUseLastWarmup(); len(warm) > 0 {
		status["last_warmup"] = warm
	}
	if m := a.GetComputerUseLastObserveMetrics(); len(m) > 0 {
		status["last_observe"] = m
	}
	if e := a.GetComputerUseLastError(); len(e) > 0 {
		status["last_error"] = e
	}
	status["observe_history_summary"] = a.getComputerUseObserveHistorySummary()
	if e2e := a.GetComputerUseLastE2E(); len(e2e) > 0 {
		status["last_e2e"] = map[string]interface{}{
			"ok":                e2e["ok"],
			"interact":          e2e["interact"],
			"ms":                e2e["ms"],
			"token_found":       e2e["token_found"],
			"type_ok":           e2e["type_ok"],
			"soft_fail":         e2e["soft_fail"],
			"skip_reason":       e2e["skip_reason"],
			"token_unconfirmed": e2e["token_unconfirmed"],
			"focus_retry":       e2e["focus_retry"],
			"diagnostics_path":  e2e["diagnostics_path"],
			"history_csv_path":  e2e["history_csv_path"],
			"at":                e2e["at"],
			"error":             e2e["error"],
		}
	}
	return status
}

func computerusePlaybookShort() string {
	// Avoid importing cycle issues — re-export from computeruse package via tools file.
	return computerUsePlaybookOneLiner()
}

// CheckYOLOModel checks if the YOLO model file exists locally.
func (a *App) CheckYOLOModel() map[string]interface{} {
	p := yoloModelPath()
	if p == "" {
		return yoloStatusMap(false, "", 0)
	}
	yoloModelStatusCache.Lock()
	if yoloModelStatusCache.valid && yoloModelStatusCache.path == p {
		exists := yoloModelStatusCache.exists
		size := yoloModelStatusCache.size
		yoloModelStatusCache.Unlock()
		// File presence is cached. Download progress is not: a hit that
		// omitted it kept a failed download looking in-progress.
		return yoloStatusMap(exists, p, size)
	}
	yoloModelStatusCache.Unlock()
	exists, size := false, int64(0)
	if fi, err := os.Stat(p); err == nil {
		exists = true
		size = fi.Size()
	}
	// A miss must not be cached. The file appears when the download renames
	// it, and a cached miss would keep saying the weights are absent.
	yoloRememberFile(p, exists, size)
	return yoloStatusMap(exists, p, size)
}

func yoloRememberFile(path string, exists bool, size int64) {
	yoloModelStatusCache.Lock()
	defer yoloModelStatusCache.Unlock()
	if !exists {
		if yoloModelStatusCache.path == path {
			yoloModelStatusCache.valid = false
		}
		return
	}
	yoloModelStatusCache.path = path
	yoloModelStatusCache.exists = true
	yoloModelStatusCache.size = size
	yoloModelStatusCache.valid = true
}

func yoloStatusMap(exists bool, path string, size int64) map[string]interface{} {
	running, failed := yoloDownloadStatus()
	return map[string]interface{}{
		"exists":      exists,
		"path":        path,
		"size":        size,
		"downloading": running,
		"error":       failed,
	}
}

func yoloDownloadStatus() (running bool, failed string) {
	yoloStateMu.Lock()
	defer yoloStateMu.Unlock()
	return yoloRunning, yoloFailedMsg
}

func yoloNoteDownloadStarted() {
	yoloStateMu.Lock()
	yoloRunning = true
	yoloFailedMsg = ""
	yoloStateMu.Unlock()
}

func yoloNoteDownloadFailed(err error) {
	yoloStateMu.Lock()
	yoloRunning = false
	if err != nil {
		yoloFailedMsg = err.Error()
	} else {
		yoloFailedMsg = "download failed"
	}
	yoloStateMu.Unlock()
}

func yoloNoteDownloadFinished() {
	yoloStateMu.Lock()
	yoloRunning = false
	yoloFailedMsg = ""
	yoloStateMu.Unlock()
}

func yoloAllowEnableAfterDownload() bool {
	yoloStateMu.Lock()
	defer yoloStateMu.Unlock()
	return !yoloUserStopped
}

// DownloadYOLOModel downloads the YOLO model for screen parsing.
// Tries GitHub Releases first, then falls back to Hub URL.
// Progress is emitted via Wails event "yolo-download-progress".
func (a *App) DownloadYOLOModel() error {
	if !yoloDownloadMu.TryLock() {
		return nil // already downloading
	}
	defer yoloDownloadMu.Unlock()
	invalidateYOLOModelStatusCache()
	defer invalidateYOLOModelStatusCache()

	dir, err := embeddingModelsDir()
	if err != nil {
		return fmt.Errorf("create models dir: %w", err)
	}
	return a.downloadYOLOFile(filepath.Join(dir, yoloModelFilename))
}

// downloadYOLOFile fetches the weights and records running or failed for the
// UI. GitHub is tried first. The Hub attempt is the same file, so a GitHub
// error is not shown while the fallback is still going. The final failure is
// one event, otherwise a silent preload leaves the panel spinning.
func (a *App) downloadYOLOFile(destPath string) error {
	yoloNoteDownloadStarted()
	err := a.downloadModelFromWithEvent(yoloModelDefaultURL, destPath, false, "yolo-download-progress")
	if err != nil {
		cfg, cfgErr := a.LoadConfig()
		hubURL := ""
		if cfgErr == nil {
			hubURL = strings.TrimRight(cfg.RemoteHubURL, "/")
		}
		if hubURL == "" {
			if cfgErr != nil {
				err = fmt.Errorf("load config: %w", cfgErr)
			} else {
				err = fmt.Errorf("GitHub download failed and Hub URL not configured: %v", err)
			}
			yoloNoteDownloadFailed(err)
			a.emitDownloadProgressNamed("yolo-download-progress", 0, 0, 0, err.Error())
			return err
		}
		fallbackURL := hubURL + "/api/v1/models/" + yoloModelFilename
		if hubErr := a.downloadModelFromWithEvent(fallbackURL, destPath, false, "yolo-download-progress"); hubErr != nil {
			yoloNoteDownloadFailed(hubErr)
			a.emitDownloadProgressNamed("yolo-download-progress", 0, 0, 0, hubErr.Error())
			return hubErr
		}
	}
	yoloNoteDownloadFinished()
	log.Printf("[yolo] model downloaded to %s", destPath)
	if yoloAllowEnableAfterDownload() {
		a.enableScreenParsingAfterYOLODownload(destPath)
	} else {
		log.Printf("[yolo] screen parsing stays off; it was turned off during the download")
	}
	// The byte-stream event already reported 100% before this decision.
	// Publish the toggle only after it, so a mid-download off stays off.
	a.emitYOLOScreenParsingSettled()
	return nil
}

func yoloSettledProgress(enabled bool) map[string]interface{} {
	status := downloadProgressNamedStatus(100, "")
	return map[string]interface{}{
		"percent":                100,
		"downloaded":             int64(0),
		"total":                  int64(0),
		"error":                  "",
		"status":                 status,
		"runtime_status":         string(status.RuntimeStatusValue()),
		"screen_parsing_enabled": enabled,
	}
}

func (a *App) emitYOLOScreenParsingSettled() {
	if a == nil || a.ctx == nil {
		return
	}
	a.emitEvent("yolo-download-progress", yoloSettledProgress(a.GetScreenParsingEnabled()))
}

// yoloAutoDownloadPolicy is the missing-weights rule. A file that is not on
// disk is downloaded without asking, and a successful download turns screen
// parsing on. A file that is already present stays as the user left it: an
// explicit off is not switched back on.
func yoloAutoDownloadPolicy(fileExists, screenParsingEnabled bool) (download, enableOnSuccess bool) {
	if fileExists {
		return false, false
	}
	return true, !screenParsingEnabled
}

// enableScreenParsingAfterYOLODownload persists screen parsing once the
// weights are on disk. It does not start another preload: the file is
// already there, and SetScreenParsingEnabled would only queue a no-op.
func (a *App) enableScreenParsingAfterYOLODownload(destPath string) {
	if destPath == "" {
		return
	}
	if _, err := os.Stat(destPath); err != nil {
		return
	}
	if a.GetScreenParsingEnabled() {
		return
	}
	if _, err := a.PatchConfigFields(map[string]interface{}{"screen_parsing_enabled": true}); err != nil {
		log.Printf("[yolo] enable after download: %v", err)
		return
	}
	invalidateYOLOModelStatusCache()
	log.Printf("[yolo] screen parsing enabled after model download")
}

// backgroundPreloadYOLOModel silently downloads the YOLO model in the background
// when the weights file is missing, whether or not screen parsing is already on.
// Reuses the same downloadModelFrom infrastructure as the embedding model.
func (a *App) backgroundPreloadYOLOModel() {
	destPath := yoloModelPath()
	if destPath == "" {
		return
	}

	_, statErr := os.Stat(destPath)
	download, _ := yoloAutoDownloadPolicy(statErr == nil, a.GetScreenParsingEnabled())
	if !download {
		if statErr == nil {
			log.Printf("[yolo] model already exists: %s", destPath)
		}
		return
	}

	// Acquire download lock
	if !yoloDownloadMu.TryLock() {
		return
	}
	defer yoloDownloadMu.Unlock()
	invalidateYOLOModelStatusCache()
	defer invalidateYOLOModelStatusCache()

	log.Println("[yolo] background preload: starting silent download")
	if err := a.downloadYOLOFile(destPath); err != nil {
		log.Printf("[yolo] background preload: %v", err)
	}
}
