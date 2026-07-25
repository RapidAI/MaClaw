package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/diarization"
)

const diarizationModelFilename = diarization.DefaultCAMPlusFilename

// diarizationModelDefaultURL is a variable so download-path tests can use a
// local HTTP server. Production always uses the published MaClaw release.
var diarizationModelDefaultURL = "https://github.com/RapidAI/MaClaw/releases/download/Model_Release/campplus-cn-common.cmpg"

var diarizationDownloadMu sync.Mutex

// GetDiarizationEnabled returns whether speaker diarization is enabled.
func (a *App) GetDiarizationEnabled() bool {
	cfg, err := a.LoadConfig()
	return err == nil && cfg.DiarizationEnabled
}

// SetDiarizationEnabled enables/disables speaker diarization. Enabling starts
// a non-blocking model download when the CAM++ artifact is not cached locally.
func (a *App) SetDiarizationEnabled(enabled bool) error {
	if _, err := a.PatchConfigFields(map[string]interface{}{"diarization_enabled": enabled}); err != nil {
		return err
	}
	if enabled && !modelStatusExists(a.CheckDiarizationModel()) {
		go a.backgroundPreloadDiarizationModel()
	}
	return nil
}

// DownloadDiarizationModel downloads the CAM++ speaker-embedding model.
// It uses the same durable model cache and resume downloader as ASR:
// ~/.maclaw/models/campplus-cn-common.cmpg (or the configured data directory).
func (a *App) DownloadDiarizationModel() error {
	if !diarizationDownloadMu.TryLock() {
		return nil
	}
	defer diarizationDownloadMu.Unlock()

	dir, err := embeddingModelsDir()
	if err != nil {
		return fmt.Errorf("create models dir: %w", err)
	}
	destPath := filepath.Join(dir, diarizationModelFilename)
	if _, err := osStatNonEmpty(destPath); err == nil && diarization.ValidateCAMPlusFile(destPath) == nil {
		a.emitDiarizationProgress(100, 0, 0, "")
		return nil
	}

	// GitHub is the primary source. Keep failures silent while a retry or the
	// configured Hub cache is still available.
	for attempt := 0; attempt < 3; attempt++ {
		if err := a.downloadDiarizationFrom(diarizationModelDefaultURL, destPath, false); err == nil {
			if err := diarization.ValidateCAMPlusFile(destPath); err == nil {
				resetCAMPlusModel(destPath)
				return nil
			}
			_ = os.Remove(destPath)
			resetCAMPlusModel(destPath)
		}
	}

	hubURL := a.PeekRemoteHubURLTrimmed()
	if hubURL == "" {
		msg := "default download URL is unavailable and Hub URL is not configured"
		a.emitDiarizationProgress(0, 0, 0, msg)
		return fmt.Errorf("%s", msg)
	}
	if err := a.downloadDiarizationFrom(hubURL+"/api/v1/models/"+diarizationModelFilename, destPath, true); err != nil {
		return err
	}
	if err := diarization.ValidateCAMPlusFile(destPath); err != nil {
		_ = os.Remove(destPath)
		resetCAMPlusModel(destPath)
		msg := fmt.Sprintf("downloaded CAM++ model is invalid: %v", err)
		a.emitDiarizationProgress(0, 0, 0, msg)
		return fmt.Errorf("%s", msg)
	}
	resetCAMPlusModel(destPath)
	return nil
}

func (a *App) downloadDiarizationFrom(url, destPath string, emitErrors bool) error {
	return a.downloadModelFromWithEvent(url, destPath, emitErrors, "diarization-download-progress")
}

func (a *App) emitDiarizationProgress(pct int, downloaded, total int64, errMsg string) {
	a.emitDownloadProgressNamed("diarization-download-progress", pct, downloaded, total, errMsg)
}

// backgroundPreloadDiarizationModel mirrors ASR startup behavior. It only
// downloads while the feature remains enabled; the shared downloader preserves
// a partial .tmp file for a future Range-resume attempt.
func (a *App) backgroundPreloadDiarizationModel() {
	if !a.GetDiarizationEnabled() || modelStatusExists(a.CheckDiarizationModel()) {
		return
	}
	if err := a.DownloadDiarizationModel(); err != nil {
		fmt.Printf("[diarization] background preload failed: %v\n", err)
	}
}
