package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const yoloModelFilename = "omniparser-v2.yolow"

// yoloModelDefaultURL is the primary download source (GitHub Releases).
const yoloModelDefaultURL = "https://github.com/RapidAI/MaClaw/releases/download/Model_Release/omniparser-v2.yolow"

// yoloDownloadMu prevents concurrent YOLO model downloads.
var yoloDownloadMu sync.Mutex

// yoloModelPath returns the full path to the YOLO model file.
// Uses the same ~/.maclaw/models directory as the embedding model.
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
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	cfg.ScreenParsingEnabled = &enabled
	if err := a.SaveConfig(cfg); err != nil {
		return err
	}

	if enabled {
		go a.backgroundPreloadYOLOModel()
	}
	return nil
}

// CheckYOLOModel checks if the YOLO model file exists locally.
func (a *App) CheckYOLOModel() map[string]interface{} {
	p := yoloModelPath()
	if p == "" {
		return map[string]interface{}{"exists": false, "path": "", "size": int64(0)}
	}
	fi, err := os.Stat(p)
	if err != nil {
		return map[string]interface{}{"exists": false, "path": p, "size": int64(0)}
	}
	return map[string]interface{}{"exists": true, "path": p, "size": fi.Size()}
}

// DownloadYOLOModel downloads the YOLO model for screen parsing.
// Tries GitHub Releases first, then falls back to Hub URL.
// Progress is emitted via Wails event "yolo-download-progress".
func (a *App) DownloadYOLOModel() error {
	if !yoloDownloadMu.TryLock() {
		return nil // already downloading
	}
	defer yoloDownloadMu.Unlock()

	dir, err := embeddingModelsDir()
	if err != nil {
		return fmt.Errorf("create models dir: %w", err)
	}
	destPath := filepath.Join(dir, yoloModelFilename)

	// 1) Try GitHub URL first (silent)
	if err := a.downloadModelFromWithEvent(yoloModelDefaultURL, destPath, false, "yolo-download-progress"); err == nil {
		return nil
	}

	// 2) Fallback: Hub URL
	cfg, err := a.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	hubURL := strings.TrimRight(cfg.RemoteHubURL, "/")
	if hubURL == "" {
		return fmt.Errorf("GitHub download failed and Hub URL not configured")
	}
	fallbackURL := hubURL + "/api/v1/models/" + yoloModelFilename
	return a.downloadModelFromWithEvent(fallbackURL, destPath, true, "yolo-download-progress")
}

// backgroundPreloadYOLOModel silently downloads the YOLO model in the background
// when screen parsing is enabled and the model file is missing.
// Reuses the same downloadModelFrom infrastructure as the embedding model.
func (a *App) backgroundPreloadYOLOModel() {
	// Check if enabled
	if !a.GetScreenParsingEnabled() {
		return
	}

	destPath := yoloModelPath()
	if destPath == "" {
		return
	}

	// Already exists?
	if _, err := os.Stat(destPath); err == nil {
		log.Printf("[yolo] model already exists: %s", destPath)
		return
	}

	// Acquire download lock
	if !yoloDownloadMu.TryLock() {
		return
	}
	defer yoloDownloadMu.Unlock()

	log.Println("[yolo] background preload: starting silent download")

	// Try GitHub first
	if err := a.downloadModelFromWithEvent(yoloModelDefaultURL, destPath, false, "yolo-download-progress"); err != nil {
		// Fallback: Hub URL
		cfg, _ := a.LoadConfig()
		hubURL := strings.TrimRight(cfg.RemoteHubURL, "/")
		if hubURL == "" {
			log.Printf("[yolo] background preload: all sources failed: %v", err)
			return
		}
		fallbackURL := hubURL + "/api/v1/models/" + yoloModelFilename
		if err := a.downloadModelFromWithEvent(fallbackURL, destPath, false, "yolo-download-progress"); err != nil {
			log.Printf("[yolo] background preload: fallback failed: %v", err)
			return
		}
	}

	log.Printf("[yolo] background preload: model downloaded to %s", destPath)
}
