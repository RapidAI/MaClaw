package main

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/asr"
)

const asrModelFilename = asr.DefaultModelFilename
const asrModelDefaultURL = asr.DefaultModelDownloadURL

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
	if _, err := a.PatchConfigFields(map[string]interface{}{"asr_enabled": enabled}); err != nil {
		return err
	}
	if enabled {
		info := a.CheckASRModel()
		if !info["exists"].(bool) {
			go a.downloadASRModelIfStillEnabled()
		}
	}
	return nil
}

// CheckASRModel returns the configured SenseVoice model's file status.
func (a *App) CheckASRModel() map[string]interface{} {
	dir, err := embeddingModelsDir() // same dir as embedding model
	if err != nil {
		return map[string]interface{}{"exists": false, "size": 0}
	}
	p := filepath.Join(dir, asrModelFilename)
	size, ok := asr.ModelFileStatus(p)
	if !ok {
		return map[string]interface{}{"exists": false, "size": 0}
	}
	return map[string]interface{}{"exists": true, "size": size, "model": "sensevoice"}
}

// DownloadASRModel downloads the ASR model (GitHub first, Hub fallback).
func (a *App) DownloadASRModel() error {
	return a.downloadASRModel(true)
}

func (a *App) downloadASRModelIfStillEnabled() error {
	return a.downloadASRModel(false)
}

func (a *App) downloadASRModel(autoEnable bool) error {
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
			if autoEnable {
				a.autoEnableASR()
			} else if !a.asrStillConfiguredEnabled() {
				return nil
			}
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
	if autoEnable {
		a.autoEnableASR()
	} else if !a.asrStillConfiguredEnabled() {
		return nil
	}
	return nil
}

// autoEnableASR sets ASREnabled=true in config after successful download.
func (a *App) autoEnableASR() {
	_, _ = a.PatchConfigFields(map[string]interface{}{"asr_enabled": true})
}

func (a *App) downloadASRFrom(url, destPath string, emitErrors bool) error {
	return a.downloadModelFromWithEvent(url, destPath, emitErrors, "asr-download-progress")
}

func (a *App) emitASRProgress(pct int, downloaded, total int64, errMsg string) {
	a.emitEvent("asr-download-progress", map[string]interface{}{
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
	if !cfg.ASREnabled {
		return
	}

	dir, err := embeddingModelsDir()
	if err != nil {
		return
	}
	destPath := filepath.Join(dir, asrModelFilename)
	if _, ok := asr.ModelFileStatus(destPath); ok {
		return
	}

	if !asrDownloadMu.TryLock() {
		return
	}
	defer asrDownloadMu.Unlock()

	fmt.Println("[asr] background preload: starting silent download")

	// GitHub first (3 retries)
	for attempt := 0; attempt < 3; attempt++ {
		if err := a.downloadModelFromWithEvent(asrModelDefaultURL, destPath, false, "asr-download-progress"); err == nil {
			if !a.asrStillConfiguredEnabled() {
				return
			}
			fmt.Println("[asr] background preload: download complete")
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
	if err := a.downloadModelFromWithEvent(fallbackURL, destPath, false, "asr-download-progress"); err == nil {
		if !a.asrStillConfiguredEnabled() {
			return
		}
		fmt.Println("[asr] background preload: hub download complete")
	}
}
