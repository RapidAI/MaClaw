package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
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

// ocrModelsZipURL resolves the default (GitHub release) download URL for the
// OCR models zip. Package var so tests can point the download flow at a mock
// server (same pattern as diarizationModelDefaultURL).
var ocrModelsZipURL = ocr.DefaultModelsZipURL

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
// "tier" always reports the configured model tier so the UI can render the
// selector even before any model files exist.
func (a *App) CheckOCRModel() map[string]interface{} {
	tier := a.ocrModelTier()
	dir, err := embeddingModelsDir() // same dir as embedding model
	if err != nil {
		return map[string]interface{}{"exists": false, "size": 0, "tier": tier}
	}
	detPath := filepath.Join(dir, ocr.DetModelFilename(tier))
	recPath := filepath.Join(dir, ocr.RecModelFilename(tier))
	detSize, detOK := ocr.ModelFileStatus(detPath)
	recSize, recOK := ocr.ModelFileStatus(recPath)
	if !detOK || !recOK {
		return map[string]interface{}{"exists": false, "size": 0, "tier": tier}
	}
	return map[string]interface{}{"exists": true, "size": detSize + recSize, "model": "ppocrv6_" + tier, "tier": tier}
}

// SetOCRModelTier persists the PP-OCRv6 model tier ("tiny"/"small"/"medium";
// junk normalizes to the default via PatchConfigFields). When OCR is enabled
// and the new tier's model files are missing, it kicks the same background
// download flow used when enabling OCR.
func (a *App) SetOCRModelTier(tier string) error {
	if _, err := a.PatchConfigFields(map[string]interface{}{"ocr_model_tier": tier}); err != nil {
		return err
	}
	if a.ocrStillConfiguredEnabled() {
		info := a.CheckOCRModel()
		if !info["exists"].(bool) {
			go a.downloadOCRModelIfStillEnabled()
		}
	}
	return nil
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

// DownloadOCRModel downloads the OCR models zip (GitHub first, Hub fallback)
// and extracts every tier's det/rec ONNX files into the models dir.
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
	detPath := filepath.Join(dir, ocr.DetModelFilename(tier))
	recPath := filepath.Join(dir, ocr.RecModelFilename(tier))

	_, detOK := ocr.ModelFileStatus(detPath)
	_, recOK := ocr.ModelFileStatus(recPath)
	if !detOK || !recOK {
		zipPath := filepath.Join(dir, ocr.ModelsZipFilename)
		if err := a.downloadOCRZipWithFallback(zipPath); err != nil {
			return err
		}
		// Extraction validates every tier's files; on failure it removes the
		// partials it extracted. The zip itself is only a transport artifact —
		// drop it either way so a corrupt bundle is re-downloaded next time.
		extractErr := extractOCRModelsZip(zipPath, dir)
		_ = os.Remove(zipPath)
		if extractErr != nil {
			return extractErr
		}
	}

	if autoEnable {
		a.autoEnableOCR()
	} else if !a.ocrStillConfiguredEnabled() {
		return nil
	}
	// Both files for the configured tier are now present; make sure the shared
	// provider uses them (it may have been created for a previous tier).
	sharedNativeOCRProvider().SetModelPaths(detPath, recPath)
	a.emitOCRProgress(100, 0, 0, "")
	return nil
}

// downloadOCRZipWithFallback tries the GitHub release URL (3 silent retries),
// then falls back to the Hub mirror endpoint serving the same zip filename.
func (a *App) downloadOCRZipWithFallback(destPath string) error {
	// GitHub first (3 retries, silent)
	for attempt := 0; attempt < 3; attempt++ {
		if err := a.downloadOCRFrom(ocrModelsZipURL, destPath, false); err == nil {
			return nil
		}
	}

	// Fallback: Hub (lock-free peek)
	hubURL := a.PeekRemoteHubURLTrimmed()
	if hubURL == "" {
		a.emitOCRProgress(0, 0, 0, "默认下载地址不可用，且 Hub URL 未配置")
		return fmt.Errorf("默认下载地址不可用，且 Hub URL 未配置")
	}
	return a.downloadOCRFrom(hubURL+"/api/v1/models/"+ocr.ModelsZipFilename, destPath, true)
}

// expectedOCRModelFilenames returns the det+rec filenames for every published
// PP-OCRv6 tier (tiny/small/medium) bundled in the models zip.
func expectedOCRModelFilenames() []string {
	tiers := []string{"tiny", "small", "medium"}
	out := make([]string, 0, len(tiers)*2)
	for _, tier := range tiers {
		out = append(out, ocr.DetModelFilename(tier), ocr.RecModelFilename(tier))
	}
	return out
}

// extractOCRModelsZip extracts the .onnx entries of the OCR models zip into
// dir, writing each entry to <name>.tmp first and renaming it into place
// (mirroring the download .tmp+rename pattern). Non-.onnx entries are skipped
// and zip-slip paths are rejected. After extraction every expected tier file
// is validated via ocr.ModelFileStatus; on any failure the extracted partials
// are removed and an error is returned.
func extractOCRModelsZip(zipPath, dir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open OCR models zip: %w", err)
	}
	defer r.Close()

	extracted := make([]string, 0, len(r.File))
	fail := func(err error) error {
		for _, path := range extracted {
			_ = os.Remove(path)
		}
		return err
	}

	for _, f := range r.File {
		cleaned := filepath.Clean(f.Name)
		if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			return fail(fmt.Errorf("OCR models zip contains unsafe path %q", f.Name))
		}
		name := filepath.Base(cleaned)
		if !strings.HasSuffix(strings.ToLower(name), ".onnx") {
			continue
		}
		dst := filepath.Join(dir, name)
		if err := extractOCRZipEntry(f, dst); err != nil {
			return fail(err)
		}
		extracted = append(extracted, dst)
	}

	for _, name := range expectedOCRModelFilenames() {
		if _, ok := ocr.ModelFileStatus(filepath.Join(dir, name)); !ok {
			return fail(fmt.Errorf("extracted OCR model %s failed validation", name))
		}
	}
	return nil
}

// extractOCRZipEntry writes a single zip entry to dst via a <dst>.tmp sibling
// followed by an atomic rename.
func extractOCRZipEntry(f *zip.File, dst string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("open zip entry %s: %w", f.Name, err)
	}
	defer rc.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", f.Name, err)
	}
	_, copyErr := io.Copy(out, rc)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("extract %s: %w", f.Name, copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("extract %s: %w", f.Name, closeErr)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("install %s: %w", f.Name, err)
	}
	return nil
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
	if !a.ocrStillConfiguredEnabled() {
		return
	}

	dir, err := embeddingModelsDir()
	if err != nil {
		return
	}
	tier := a.ocrModelTier()
	_, detOK := ocr.ModelFileStatus(filepath.Join(dir, ocr.DetModelFilename(tier)))
	_, recOK := ocr.ModelFileStatus(filepath.Join(dir, ocr.RecModelFilename(tier)))
	if detOK && recOK {
		return
	}

	fmt.Println("[ocr] background preload: starting silent download")
	if err := a.downloadOCRModel(false); err != nil {
		fmt.Printf("[ocr] background preload: %v\n", err)
		return
	}
	fmt.Println("[ocr] background preload: download complete")
}
