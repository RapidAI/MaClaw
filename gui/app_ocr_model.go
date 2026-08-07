package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/ocr"
)

var ocrDownloadMu sync.Mutex

// ocrDetModelURL/ocrRecModelURL resolve the default (HuggingFace) download
// URLs for a tier. Package vars so tests can point the download flow at a
// mock server (same pattern as diarizationModelDefaultURL).
var (
	ocrDetModelURL = ocr.DetModelURL
	ocrRecModelURL = ocr.RecModelURL
)

// GetOCREnabled returns whether OCR is enabled in config.
func (a *App) GetOCREnabled() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return false
	}
	return cfg.OCREnabled
}

// SetOCREnabled enables/disables OCR. Auto-downloads models if enabling.
func (a *App) SetOCREnabled(enabled bool) error {
	if _, err := a.PatchConfigFields(map[string]interface{}{"ocr_enabled": enabled}); err != nil {
		return err
	}
	if enabled {
		info := a.CheckOCRModel()
		if !info["exists"].(bool) {
			go a.downloadOCRModelIfStillEnabled()
		}
	}
	return nil
}

// CheckOCRModel returns the configured PP-OCRv6 models' file status.
// "exists" is true only when BOTH det and rec files are present and valid.
func (a *App) CheckOCRModel() map[string]interface{} {
	dir, err := embeddingModelsDir() // same dir as embedding model
	if err != nil {
		return map[string]interface{}{"exists": false, "size": 0}
	}
	tier := a.ocrModelTier()
	detPath := filepath.Join(dir, ocr.DetModelFilename(tier))
	recPath := filepath.Join(dir, ocr.RecModelFilename(tier))
	detSize, detOK := ocr.ModelFileStatus(detPath)
	recSize, recOK := ocr.ModelFileStatus(recPath)
	if !detOK || !recOK {
		return map[string]interface{}{"exists": false, "size": 0}
	}
	return map[string]interface{}{"exists": true, "size": detSize + recSize, "model": "ppocrv6_" + tier}
}

// ocrModelTier returns the configured OCR model tier (defaults to "small").
func (a *App) ocrModelTier() string {
	cfg, err := a.LoadConfig()
	if err != nil {
		return corelib.DefaultOCRModelTier
	}
	return corelib.NormalizeOCRModelTier(cfg.OCRModelTier)
}

// ocrConfiguredModelTier resolves the configured OCR model tier without an
// App instance (some tool registration paths have none). It peeks the single
// field from the on-disk config; any failure yields the default tier.
func ocrConfiguredModelTier() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return corelib.DefaultOCRModelTier
	}
	data, err := os.ReadFile(filepath.Join(home, ".maclaw", "config.json"))
	if err != nil {
		return corelib.DefaultOCRModelTier
	}
	var probe struct {
		OCRModelTier string `json:"ocr_model_tier"`
	}
	if json.Unmarshal(data, &probe) != nil {
		return corelib.DefaultOCRModelTier
	}
	return corelib.NormalizeOCRModelTier(probe.OCRModelTier)
}

// ocrModelPaths returns the expected det/rec model paths for the configured
// tier inside the shared models directory.
func ocrModelPaths() (detPath, recPath string, err error) {
	dir, err := embeddingModelsDir()
	if err != nil {
		return "", "", err
	}
	tier := ocrConfiguredModelTier()
	return filepath.Join(dir, ocr.DetModelFilename(tier)),
		filepath.Join(dir, ocr.RecModelFilename(tier)), nil
}

// ── shared native provider ──

var (
	sharedOCROnce     sync.Once
	sharedOCRProvider *browser.NativeOCRProvider
)

// sharedNativeOCRProvider returns the process-wide OCR provider wrapping the
// native PP-OCRv6 engine. A single shared provider means a single model
// manager — the det/rec graphs stay in memory only once for the whole app
// (computer use, GUI automation, browser tasks and the ocr_recognize tool).
func sharedNativeOCRProvider() *browser.NativeOCRProvider {
	sharedOCROnce.Do(func() {
		detPath, recPath, err := ocrModelPaths()
		if err != nil {
			log.Printf("[ocr] cannot resolve model paths: %v", err)
		}
		sharedOCRProvider = browser.NewNativeOCRProvider(detPath, recPath, func(msg string) {
			log.Printf("[ocr] %s", msg)
		})
	})
	return sharedOCRProvider
}

// ensureOCRModelFiles returns the det/rec model paths when both files are
// present and valid. Otherwise it kicks off the background download and
// returns ok=false so callers can ask the user to retry shortly.
func (a *App) ensureOCRModelFiles() (detPath, recPath string, ok bool) {
	dir, err := embeddingModelsDir()
	if err != nil {
		return "", "", false
	}
	tier := a.ocrModelTier()
	detPath = filepath.Join(dir, ocr.DetModelFilename(tier))
	recPath = filepath.Join(dir, ocr.RecModelFilename(tier))
	if _, detOK := ocr.ModelFileStatus(detPath); !detOK {
		go a.backgroundPreloadOCRModel()
		return detPath, recPath, false
	}
	if _, recOK := ocr.ModelFileStatus(recPath); !recOK {
		go a.backgroundPreloadOCRModel()
		return detPath, recPath, false
	}
	// The shared singleton resolves its paths once at creation; if the
	// configured tier changed since then, point it at the now-present files.
	sharedNativeOCRProvider().SetModelPaths(detPath, recPath)
	return detPath, recPath, true
}

// DownloadOCRModel downloads the OCR models (HuggingFace first, Hub fallback).
func (a *App) DownloadOCRModel() error {
	return a.downloadOCRModel(true)
}

func (a *App) downloadOCRModelIfStillEnabled() error {
	return a.downloadOCRModel(false)
}

func (a *App) downloadOCRModel(autoEnable bool) error {
	if !ocrDownloadMu.TryLock() {
		return nil
	}
	defer ocrDownloadMu.Unlock()

	dir, err := embeddingModelsDir()
	if err != nil {
		return fmt.Errorf("create models dir: %w", err)
	}
	tier := a.ocrModelTier()
	files := []struct {
		filename string
		url      string
	}{
		{ocr.DetModelFilename(tier), ocrDetModelURL(tier)},
		{ocr.RecModelFilename(tier), ocrRecModelURL(tier)},
	}

	hubURL := ""
	for _, f := range files {
		destPath := filepath.Join(dir, f.filename)
		if _, ok := ocr.ModelFileStatus(destPath); ok {
			continue
		}
		if err := a.downloadOCRFileWithFallback(f.url, f.filename, destPath, &hubURL); err != nil {
			return err
		}
		// Validate the downloaded file; delete garbage (e.g. an HTML error page).
		if _, ok := ocr.ModelFileStatus(destPath); !ok {
			_ = os.Remove(destPath)
			return fmt.Errorf("downloaded OCR model %s failed validation", f.filename)
		}
	}

	if autoEnable {
		a.autoEnableOCR()
	} else if !a.ocrStillConfiguredEnabled() {
		return nil
	}
	// Both files for the configured tier are now present; make sure the shared
	// provider uses them (it may have been created for a previous tier).
	sharedNativeOCRProvider().SetModelPaths(
		filepath.Join(dir, ocr.DetModelFilename(tier)),
		filepath.Join(dir, ocr.RecModelFilename(tier)),
	)
	a.emitOCRProgress(100, 0, 0, "")
	return nil
}

// downloadOCRFileWithFallback tries the HuggingFace URL (3 silent retries),
// then falls back to the Hub mirror endpoint. hubURL is resolved lazily and
// cached across the det/rec downloads via the pointer.
func (a *App) downloadOCRFileWithFallback(url, filename, destPath string, hubURL *string) error {
	// HuggingFace first (3 retries, silent)
	for attempt := 0; attempt < 3; attempt++ {
		if err := a.downloadOCRFrom(url, destPath, false); err == nil {
			return nil
		}
	}

	// Fallback: Hub (lock-free peek)
	if *hubURL == "" {
		*hubURL = a.PeekRemoteHubURLTrimmed()
	}
	if *hubURL == "" {
		a.emitOCRProgress(0, 0, 0, "默认下载地址不可用，且 Hub URL 未配置")
		return fmt.Errorf("默认下载地址不可用，且 Hub URL 未配置")
	}
	return a.downloadOCRFrom(*hubURL+"/api/v1/models/"+filename, destPath, true)
}

// autoEnableOCR sets OCREnabled=true in config after successful download.
func (a *App) autoEnableOCR() {
	_, _ = a.PatchConfigFields(map[string]interface{}{"ocr_enabled": true})
}

func (a *App) downloadOCRFrom(url, destPath string, emitErrors bool) error {
	return a.downloadModelFromWithEvent(url, destPath, emitErrors, "ocr-download-progress")
}

func (a *App) emitOCRProgress(pct int, downloaded, total int64, errMsg string) {
	a.emitEvent("ocr-download-progress", map[string]interface{}{
		"percent":    pct,
		"downloaded": downloaded,
		"total":      total,
		"error":      errMsg,
	})
}

func (a *App) ocrStillConfiguredEnabled() bool {
	cfg, err := a.LoadConfig()
	return err == nil && cfg.OCREnabled
}

// backgroundPreloadOCRModel silently downloads OCR models if not present.
func (a *App) backgroundPreloadOCRModel() {
	cfg, err := a.LoadConfig()
	if err != nil {
		return
	}
	if !cfg.OCREnabled {
		return
	}

	dir, err := embeddingModelsDir()
	if err != nil {
		return
	}
	tier := corelib.NormalizeOCRModelTier(cfg.OCRModelTier)
	files := []struct {
		filename string
		url      string
	}{
		{ocr.DetModelFilename(tier), ocrDetModelURL(tier)},
		{ocr.RecModelFilename(tier), ocrRecModelURL(tier)},
	}

	missing := false
	for _, f := range files {
		if _, ok := ocr.ModelFileStatus(filepath.Join(dir, f.filename)); !ok {
			missing = true
			break
		}
	}
	if !missing {
		return
	}

	if !ocrDownloadMu.TryLock() {
		return
	}
	defer ocrDownloadMu.Unlock()

	fmt.Println("[ocr] background preload: starting silent download")

	hubURL := strings.TrimRight(cfg.RemoteHubURL, "/")
	for _, f := range files {
		destPath := filepath.Join(dir, f.filename)
		if _, ok := ocr.ModelFileStatus(destPath); ok {
			continue
		}
		downloaded := false
		// HuggingFace first (3 retries)
		for attempt := 0; attempt < 3; attempt++ {
			if err := a.downloadModelFromWithEvent(f.url, destPath, false, "ocr-download-progress"); err == nil {
				downloaded = true
				break
			}
		}
		// Hub fallback
		if !downloaded && hubURL != "" {
			fallbackURL := hubURL + "/api/v1/models/" + f.filename
			if err := a.downloadModelFromWithEvent(fallbackURL, destPath, false, "ocr-download-progress"); err == nil {
				downloaded = true
			}
		}
		if !downloaded {
			fmt.Printf("[ocr] background preload: failed to download %s\n", f.filename)
			return
		}
		if _, ok := ocr.ModelFileStatus(destPath); !ok {
			_ = os.Remove(destPath)
			fmt.Printf("[ocr] background preload: %s failed validation\n", f.filename)
			return
		}
		if !a.ocrStillConfiguredEnabled() {
			return
		}
	}
	// Switch the shared provider to the downloaded tier's files (it may have
	// been created with paths of a previously configured tier).
	sharedNativeOCRProvider().SetModelPaths(
		filepath.Join(dir, ocr.DetModelFilename(tier)),
		filepath.Join(dir, ocr.RecModelFilename(tier)),
	)
	fmt.Println("[ocr] background preload: download complete")
}
