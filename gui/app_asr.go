package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const asrModelFilename = "moonshine-base-zh.gguf"
const asrModelDefaultURL = "https://github.com/RapidAI/MaClaw/releases/download/Model_Release/moonshine-base-zh.gguf"

var asrDownloadMu sync.Mutex

// GetASREnabled returns whether ASR is enabled in config.
func (a *App) GetASREnabled() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return false
	}
	return cfg.ASREnabled
}

// SetASREnabled enables/disables ASR. Auto-downloads model if enabling.
func (a *App) SetASREnabled(enabled bool) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	cfg.ASREnabled = enabled
	if err := a.SaveConfig(cfg); err != nil {
		return err
	}
	if enabled {
		info := a.CheckASRModel()
		if !info["exists"].(bool) {
			go a.DownloadASRModel()
		}
	}
	return nil
}

// CheckASRModel returns model file status.
func (a *App) CheckASRModel() map[string]interface{} {
	dir, err := embeddingModelsDir() // same dir as embedding model
	if err != nil {
		return map[string]interface{}{"exists": false, "size": 0}
	}
	p := filepath.Join(dir, asrModelFilename)
	fi, err := os.Stat(p)
	if err != nil {
		return map[string]interface{}{"exists": false, "size": 0}
	}
	return map[string]interface{}{"exists": true, "size": fi.Size()}
}

// DownloadASRModel downloads the ASR model (GitHub first, Hub fallback).
func (a *App) DownloadASRModel() error {
	if !asrDownloadMu.TryLock() {
		return nil
	}
	defer asrDownloadMu.Unlock()

	dir, err := embeddingModelsDir()
	if err != nil {
		return fmt.Errorf("create models dir: %w", err)
	}
	destPath := filepath.Join(dir, asrModelFilename)

	// GitHub first (3 retries, silent)
	for attempt := 0; attempt < 3; attempt++ {
		if err := a.downloadASRFrom(asrModelDefaultURL, destPath, false); err == nil {
			a.autoEnableASR()
			a.emitASRProgress(100, 0, 0, "")
			return nil
		}
	}

	// Fallback: Hub
	cfg, err := a.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	hubURL := cfg.RemoteHubURL
	if hubURL == "" {
		a.emitASRProgress(0, 0, 0, "默认下载地址不可用，且 Hub URL 未配置")
		return fmt.Errorf("默认下载地址不可用，且 Hub URL 未配置")
	}
	hubURL = hubURL + "/api/v1/models/" + asrModelFilename
	if err := a.downloadASRFrom(hubURL, destPath, true); err != nil {
		return err
	}
	a.autoEnableASR()
	return nil
}

// autoEnableASR sets ASREnabled=true in config after successful download.
func (a *App) autoEnableASR() {
	cfg, err := a.LoadConfig()
	if err != nil {
		return
	}
	if !cfg.ASREnabled {
		cfg.ASREnabled = true
		a.SaveConfig(cfg)
	}
}

func (a *App) downloadASRFrom(url, destPath string, emitErrors bool) error {
	return a.downloadModelFrom(url, destPath, emitErrors)
}

func (a *App) emitASRProgress(pct int, downloaded, total int64, errMsg string) {
	runtime.EventsEmit(a.ctx, "asr-download-progress", map[string]interface{}{
		"percent":    pct,
		"downloaded": downloaded,
		"total":      total,
		"error":      errMsg,
	})
}

// backgroundPreloadASRModel silently downloads ASR model if not present.
func (a *App) backgroundPreloadASRModel() {
	cfg, err := a.LoadConfig()
	if err != nil {
		return
	}

	dir, err := embeddingModelsDir()
	if err != nil {
		return
	}
	destPath := filepath.Join(dir, asrModelFilename)
	if _, err := os.Stat(destPath); err == nil {
		// Model exists — auto-enable if not already
		if !cfg.ASREnabled {
			cfg.ASREnabled = true
			a.SaveConfig(cfg)
		}
		return
	}

	if !asrDownloadMu.TryLock() {
		return
	}
	defer asrDownloadMu.Unlock()

	fmt.Println("[asr] background preload: starting silent download")

	// GitHub first (3 retries)
	for attempt := 0; attempt < 3; attempt++ {
		if err := a.downloadModelFrom(asrModelDefaultURL, destPath, false); err == nil {
			cfg.ASREnabled = true
			a.SaveConfig(cfg)
			fmt.Println("[asr] background preload: download complete, auto-enabled")
			return
		}
	}

	// Hub fallback
	hubURL := cfg.RemoteHubURL
	if hubURL == "" {
		fmt.Println("[asr] background preload: all sources failed")
		return
	}
	fallbackURL := hubURL + "/api/v1/models/" + asrModelFilename
	if err := a.downloadModelFrom(fallbackURL, destPath, false); err == nil {
		cfg.ASREnabled = true
		a.SaveConfig(cfg)
		fmt.Println("[asr] background preload: hub download complete, auto-enabled")
	}
}
