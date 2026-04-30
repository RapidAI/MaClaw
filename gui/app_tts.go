package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/tts"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const ttsModelFilename = tts.TTSModelFilename
const ttsModelDefaultURL = "https://github.com/RapidAI/MaClaw/releases/download/Model_Release/" + ttsModelFilename

var (
	ttsDownloadMu sync.Mutex
	ttsSpeakMu    sync.Mutex // prevents concurrent synthesis (only one audio at a time)
)

// GetTTSEnabled returns whether TTS is enabled in config.
func (a *App) GetTTSEnabled() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return false
	}
	return cfg.TTSEnabled
}

// SetTTSEnabled enables/disables TTS. Auto-downloads model if enabling.
func (a *App) SetTTSEnabled(enabled bool) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	cfg.TTSEnabled = enabled
	if err := a.SaveConfig(cfg); err != nil {
		return err
	}
	if enabled {
		info := a.CheckTTSModel()
		if !info["exists"].(bool) {
			go a.DownloadTTSModel()
		}
	}
	return nil
}

// CheckTTSModel returns model file status.
func (a *App) CheckTTSModel() map[string]interface{} {
	dir, err := embeddingModelsDir()
	if err != nil {
		return map[string]interface{}{"exists": false, "size": 0}
	}
	p := filepath.Join(dir, ttsModelFilename)
	fi, err := os.Stat(p)
	if err != nil {
		return map[string]interface{}{"exists": false, "size": 0}
	}
	return map[string]interface{}{"exists": true, "size": fi.Size()}
}

// DownloadTTSModel downloads the TTS model (GitHub first, Hub fallback).
func (a *App) DownloadTTSModel() error {
	if !ttsDownloadMu.TryLock() {
		return nil
	}
	defer ttsDownloadMu.Unlock()

	dir, err := embeddingModelsDir()
	if err != nil {
		return fmt.Errorf("create models dir: %w", err)
	}
	destPath := filepath.Join(dir, ttsModelFilename)

	// GitHub first (3 retries)
	for attempt := 0; attempt < 3; attempt++ {
		if err := a.downloadTTSFrom(ttsModelDefaultURL, destPath, false); err == nil {
			a.autoEnableTTS()
			a.emitTTSProgress(100, 0, 0, "")
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
		a.emitTTSProgress(0, 0, 0, "默认下载地址不可用，且 Hub URL 未配置")
		return fmt.Errorf("默认下载地址不可用，且 Hub URL 未配置")
	}
	hubURL = hubURL + "/api/v1/models/" + ttsModelFilename
	if err := a.downloadTTSFrom(hubURL, destPath, true); err != nil {
		return err
	}
	a.autoEnableTTS()
	return nil
}

func (a *App) autoEnableTTS() {
	cfg, err := a.LoadConfig()
	if err != nil {
		return
	}
	if !cfg.TTSEnabled {
		cfg.TTSEnabled = true
		a.SaveConfig(cfg)
	}
}

func (a *App) downloadTTSFrom(url, destPath string, emitErrors bool) error {
	return a.downloadModelFromWithEvent(url, destPath, emitErrors, "tts-download-progress")
}

func (a *App) emitTTSProgress(pct int, downloaded, total int64, errMsg string) {
	runtime.EventsEmit(a.ctx, "tts-download-progress", map[string]interface{}{
		"percent":    pct,
		"downloaded": downloaded,
		"total":      total,
		"error":      errMsg,
	})
}

// initTTSManager creates the TTS manager if model exists.
func (a *App) initTTSManager() {
	dir, err := embeddingModelsDir()
	if err != nil {
		return
	}
	modelPath := filepath.Join(dir, ttsModelFilename)
	if _, err := os.Stat(modelPath); err != nil {
		return // model not downloaded yet
	}
	a.ttsManager = tts.NewManager(modelPath)
}

// backgroundPreloadTTSModel silently downloads TTS model if not present.
func (a *App) backgroundPreloadTTSModel() {
	cfg, err := a.LoadConfig()
	if err != nil {
		return
	}

	dir, err := embeddingModelsDir()
	if err != nil {
		return
	}
	destPath := filepath.Join(dir, ttsModelFilename)
	if _, err := os.Stat(destPath); err == nil {
		// Model exists — auto-enable if not already
		if !cfg.TTSEnabled {
			cfg.TTSEnabled = true
			a.SaveConfig(cfg)
		}
		return
	}

	if !ttsDownloadMu.TryLock() {
		return
	}
	defer ttsDownloadMu.Unlock()

	fmt.Println("[tts] background preload: starting silent download")

	for attempt := 0; attempt < 3; attempt++ {
		if err := a.downloadModelFromWithEvent(ttsModelDefaultURL, destPath, false, "tts-download-progress"); err == nil {
			cfg.TTSEnabled = true
			a.SaveConfig(cfg)
			fmt.Println("[tts] background preload: download complete, auto-enabled")
			a.initTTSManager()
			return
		}
	}

	// Hub fallback
	hubURL := cfg.RemoteHubURL
	if hubURL == "" {
		fmt.Println("[tts] background preload: all sources failed")
		return
	}
	fallbackURL := hubURL + "/api/v1/models/" + ttsModelFilename
	if err := a.downloadModelFromWithEvent(fallbackURL, destPath, false, "tts-download-progress"); err == nil {
		cfg.TTSEnabled = true
		a.SaveConfig(cfg)
		fmt.Println("[tts] background preload: hub download complete, auto-enabled")
		a.initTTSManager()
	}
}

// SpeakText synthesizes a voice status summary and sends it to the frontend.
// Input is a JSON string: {"userText": "...", "status": "success|error|paused"}
// Runs asynchronously to avoid blocking the Wails binding thread.
func (a *App) SpeakText(input string) {
	if input == "" {
		return
	}
	go a.speakTextAsync(input)
}

func (a *App) speakTextAsync(input string) {
	// Only one synthesis at a time — skip if already speaking
	if !ttsSpeakMu.TryLock() {
		return
	}
	defer ttsSpeakMu.Unlock()

	if a.ttsManager == nil {
		a.initTTSManager()
		if a.ttsManager == nil {
			return
		}
	}
	cfg, err := a.LoadConfig()
	if err != nil || !cfg.TTSEnabled {
		return
	}

	summary := tts.GenerateVoiceSummary(input, 150)
	if summary == "" {
		return
	}

	wav, err := a.ttsManager.SynthesizeText(summary)
	if err != nil {
		fmt.Printf("[tts] synthesize error: %v\n", err)
		return
	}

	b64 := base64EncodeWAV(wav)
	runtime.EventsEmit(a.ctx, "tts:audio", b64)
}

func base64EncodeWAV(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}
