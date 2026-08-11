package main

import (
	"archive/zip"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/tts"
)

const ttsModelFilename = tts.TTSModelFilename
const ttsModelDefaultURL = "https://github.com/RapidAI/MaClaw/releases/download/Model_Release/" + ttsModelFilename
const ttsVoiceZipFilename = tts.TTSVoiceZipFilename
const ttsVoiceZipDefaultURL = "https://github.com/RapidAI/MaClaw/releases/download/Model_Release/" + ttsVoiceZipFilename

var (
	ttsDownloadMu sync.Mutex
	ttsSpeakMu    sync.Mutex // prevents concurrent synthesis (only one audio at a time)
)

func ttsAssetsDir() (string, error) {
	dir, err := embeddingModelsDir()
	if err != nil {
		return "", err
	}
	assetDir := filepath.Join(dir, tts.TTSAssetDirName)
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		return "", err
	}
	return assetDir, nil
}

func ttsModelPath() (string, error) {
	dir, err := ttsAssetsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ttsModelFilename), nil
}

func ttsVoiceZipPath() (string, error) {
	dir, err := ttsAssetsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ttsVoiceZipFilename), nil
}

func ttsVoiceDir() (string, error) {
	dir, err := ttsAssetsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "voices"), nil
}

func normalizeTTSVoiceID(voiceID string) string {
	voiceID = strings.TrimSpace(voiceID)
	if voiceID == "" {
		return tts.DefaultTTSVoiceID
	}
	if tts.IsSupportedTTSVoiceID(voiceID) {
		return voiceID
	}
	return tts.DefaultTTSVoiceID
}

// GetTTSEnabled returns whether TTS is enabled in config.
func (a *App) GetTTSEnabled() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return false
	}
	return cfg.TTSEnabled
}

func (a *App) GetTTSVoiceID() string {
	cfg, err := a.LoadConfig()
	if err != nil {
		return tts.DefaultTTSVoiceID
	}
	return normalizeTTSVoiceID(cfg.TTSVoiceID)
}

func (a *App) SetTTSVoiceID(voiceID string) error {
	voiceID = normalizeTTSVoiceID(voiceID)
	// Publish the new setting and replace the desktop manager as one TTS
	// transition. Hardware synthesis reads both while holding ttsManagerMu.
	ttsSpeakMu.Lock()
	defer ttsSpeakMu.Unlock()
	a.ttsManagerMu.Lock()
	defer a.ttsManagerMu.Unlock()
	if _, err := a.PatchConfigFields(map[string]interface{}{"tts_voice_id": voiceID}); err != nil {
		return err
	}
	if a.ttsManager != nil {
		a.ttsManager.Unload()
		a.ttsManager = nil
	}
	a.unloadHardwareSpeechSynthesizers()
	a.initTTSManagerWithoutLock()
	return nil
}

// SetTTSEnabled enables/disables TTS. Auto-downloads model if enabling.
func (a *App) SetTTSEnabled(enabled bool) error {
	// Keep the enabled setting and manager lifetime coherent for device TTS.
	ttsSpeakMu.Lock()
	defer ttsSpeakMu.Unlock()
	a.ttsManagerMu.Lock()
	defer a.ttsManagerMu.Unlock()
	if _, err := a.PatchConfigFields(map[string]interface{}{"tts_enabled": enabled}); err != nil {
		return err
	}
	if enabled {
		info := a.CheckTTSModel()
		if !info["exists"].(bool) {
			go a.downloadTTSModelIfStillEnabled()
		} else {
			a.initTTSManagerWithoutLock()
		}
	} else {
		if a.ttsManager != nil {
			a.ttsManager.Unload()
			a.ttsManager = nil
		}
		a.unloadHardwareSpeechSynthesizers()
	}
	return nil
}

// CheckTTSModel returns model file status.
func (a *App) CheckTTSModel() map[string]interface{} {
	modelPath, err := ttsModelPath()
	if err != nil {
		return map[string]interface{}{"exists": false, "size": 0}
	}
	voiceDir, _ := ttsVoiceDir()
	zipPath, _ := ttsVoiceZipPath()
	fi, err := os.Stat(modelPath)
	if err != nil {
		return map[string]interface{}{"exists": false, "size": 0, "voices_exists": kokoroVoicesReady(voiceDir), "voice_zip_exists": fileExistsLocal(zipPath), "voice_id": a.GetTTSVoiceID()}
	}
	voicesReady := kokoroVoicesReady(voiceDir)
	return map[string]interface{}{"exists": voicesReady, "model_exists": true, "size": fi.Size(), "voices_exists": voicesReady, "voice_zip_exists": fileExistsLocal(zipPath), "path": modelPath, "voice_id": a.GetTTSVoiceID()}
}

// DownloadTTSModel downloads the TTS model (GitHub first, Hub fallback).
func (a *App) DownloadTTSModel() error {
	return a.downloadTTSModel(true)
}

func (a *App) downloadTTSModelIfStillEnabled() error {
	return a.downloadTTSModel(false)
}

func (a *App) downloadTTSModel(autoEnable bool) error {
	if !ttsDownloadMu.TryLock() {
		return nil
	}
	defer ttsDownloadMu.Unlock()

	cfg, err := a.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := a.ensureTTSAssets(cfg.RemoteHubURL, true); err != nil {
		return err
	}
	if autoEnable {
		a.autoEnableTTS()
	} else if !a.ttsStillConfiguredEnabled() {
		return nil
	}
	a.initTTSManager()
	return nil
}
func (a *App) autoEnableTTS() {
	_, _ = a.PatchConfigFields(map[string]interface{}{"tts_enabled": true})
}

func (a *App) downloadTTSFrom(url, destPath string, emitErrors bool) error {
	return a.downloadModelFromWithEvent(url, destPath, emitErrors, "tts-download-progress")
}

func (a *App) ensureTTSAssetsForUse(hubBaseURL string, emitErrors bool) error {
	info := a.CheckTTSModel()
	if exists, _ := info["exists"].(bool); exists {
		return nil
	}
	ttsDownloadMu.Lock()
	defer ttsDownloadMu.Unlock()
	info = a.CheckTTSModel()
	if exists, _ := info["exists"].(bool); exists {
		return nil
	}
	return a.ensureTTSAssets(hubBaseURL, emitErrors)
}

func (a *App) ensureTTSAssets(hubBaseURL string, emitErrors bool) error {
	modelPath, err := ttsModelPath()
	if err != nil {
		return fmt.Errorf("create tts asset dir: %w", err)
	}
	zipPath, err := ttsVoiceZipPath()
	if err != nil {
		return fmt.Errorf("create tts voice zip path: %w", err)
	}
	voiceDir, err := ttsVoiceDir()
	if err != nil {
		return fmt.Errorf("create tts voice dir: %w", err)
	}

	if !fileExistsLocal(modelPath) {
		if err := a.downloadTTSAssetWithFallback(ttsModelFilename, ttsModelDefaultURL, hubBaseURL, modelPath, emitErrors); err != nil {
			return err
		}
	}
	if !fileExistsLocal(zipPath) {
		if err := a.downloadTTSAssetWithFallback(ttsVoiceZipFilename, ttsVoiceZipDefaultURL, hubBaseURL, zipPath, emitErrors); err != nil {
			return err
		}
	}
	if !kokoroVoicesReady(voiceDir) {
		if err := unzipKokoroVoices(zipPath, voiceDir); err != nil {
			return err
		}
	}
	a.emitTTSProgress(100, 0, 0, "")
	return nil
}

func (a *App) downloadTTSAssetWithFallback(filename, primaryURL, hubBaseURL, destPath string, emitErrors bool) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if err := a.downloadTTSFrom(primaryURL, destPath, false); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	hubBaseURL = strings.TrimRight(strings.TrimSpace(hubBaseURL), "/")
	if hubBaseURL == "" {
		if emitErrors {
			a.emitTTSProgress(0, 0, 0, "default TTS download failed and Hub URL is not configured")
		}
		return fmt.Errorf("download %s from GitHub failed and Hub URL is not configured: %w", filename, lastErr)
	}
	return a.downloadTTSFrom(hubBaseURL+"/api/v1/models/"+filename, destPath, emitErrors)
}

func kokoroVoicesReady(voiceDir string) bool {
	if voiceDir == "" {
		return false
	}
	for _, voiceID := range tts.RequiredTTSVoiceIDs {
		if !fileExistsLocal(filepath.Join(voiceDir, voiceID+".koro")) {
			return false
		}
	}
	return true
}

func ensureKokoroWelcomeVoice(voiceDir, voiceID string) (string, error) {
	if strings.TrimSpace(voiceDir) == "" {
		return "", fmt.Errorf("TTS voice directory is unavailable")
	}
	var err error
	voiceID, err = normalizeHardwareWelcomeVoiceID(voiceID)
	if err != nil {
		return "", err
	}
	path := filepath.Join(voiceDir, voiceID+".koro")
	if !fileExistsLocal(path) {
		return "", fmt.Errorf("TTS voice %s is not installed; update or re-download the TTS voice pack", voiceID)
	}
	return voiceID, nil
}

func unzipKokoroVoices(zipPath, voiceDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open voice zip: %w", err)
	}
	defer r.Close()
	if err := os.MkdirAll(voiceDir, 0o755); err != nil {
		return err
	}
	for _, f := range r.File {
		name := filepath.Base(f.Name)
		if name == "." || name == string(filepath.Separator) || !strings.HasSuffix(strings.ToLower(name), ".koro") {
			continue
		}
		dst := filepath.Join(voiceDir, name)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(dst)
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		closeErr := out.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func fileExistsLocal(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (a *App) emitTTSProgress(pct int, downloaded, total int64, errMsg string) {
	a.emitEvent("tts-download-progress", map[string]interface{}{
		"percent":    pct,
		"downloaded": downloaded,
		"total":      total,
		"error":      errMsg,
	})
}

// initTTSManager creates the TTS manager if model exists.
func (a *App) initTTSManager() {
	if a == nil {
		return
	}
	a.initTTSManagerLocked()
}

// initTTSManagerLocked creates the default manager. The name is retained for
// callers that already hold ttsSpeakMu; it takes the dedicated manager lock so
// background initialization cannot race a voice replacement.
func (a *App) initTTSManagerLocked() {
	a.ttsManagerMu.Lock()
	defer a.ttsManagerMu.Unlock()
	a.initTTSManagerWithoutLock()
}

// initTTSManagerWithoutLock creates the default manager with ttsManagerMu
// already held.
func (a *App) initTTSManagerWithoutLock() {
	if a.ttsManager != nil {
		return
	}
	cfg, err := a.LoadConfig()
	if err != nil || !cfg.TTSEnabled {
		return
	}
	modelPath, err := ttsModelPath()
	if err != nil || !fileExistsLocal(modelPath) {
		return // model not downloaded yet
	}
	voiceDir, err := ttsVoiceDir()
	if err != nil || !kokoroVoicesReady(voiceDir) {
		return
	}
	voiceID := tts.DefaultTTSVoiceID
	voiceID = normalizeTTSVoiceID(cfg.TTSVoiceID)
	a.ttsManager = tts.NewKokoroManager(modelPath, voiceDir, voiceID)
}

// ttsManagerForSynthesis returns a stable manager pointer for one synthesis.
// Manager.Unload is safe while a synthesis is active; the mutex only protects
// replacing the App-owned pointer during a voice change, disable, or shutdown.
func (a *App) ttsManagerForSynthesis() *tts.Manager {
	if a == nil {
		return nil
	}
	a.ttsManagerMu.Lock()
	manager := a.ttsManager
	a.ttsManagerMu.Unlock()
	return manager
}

// ensureTTSManagerForSynthesis lazily creates the default manager and returns
// a stable pointer. Callers must prepare TTS assets before using it.
func (a *App) ensureTTSManagerForSynthesis() *tts.Manager {
	if a == nil {
		return nil
	}
	a.ttsManagerMu.Lock()
	if a.ttsManager == nil {
		a.initTTSManagerWithoutLock()
	}
	manager := a.ttsManager
	a.ttsManagerMu.Unlock()
	return manager
}

// hardwareSpeechSynthesizer resolves one physical device's reply voice without
// mutating the process-wide manager. Non-default voices are cached separately:
// a device does not reload the full Kokoro model for every reply, and no device
// can change another device's voice mid-response.
func (a *App) hardwareSpeechSynthesizer(clientID string) (tts.TextSynthesizer, func(), bool) {
	if a == nil {
		return nil, func() {}, false
	}
	// Read the configuration only after holding the manager lock. A global
	// voice change writes config before it replaces the desktop manager; taking
	// the config snapshot beforehand could compare an old global voice against
	// a newly-created manager and make one hardware reply speak with the wrong
	// voice. This lock gives every selection a coherent config/manager view.
	a.ttsManagerMu.Lock()
	defer a.ttsManagerMu.Unlock()
	cfg, err := a.LoadConfig()
	if err != nil || !cfg.TTSEnabled {
		return nil, func() {}, false
	}
	if a.ttsManager == nil {
		a.initTTSManagerWithoutLock()
		if a.ttsManager == nil {
			return nil, func() {}, false
		}
	}
	voiceID := effectiveHardwareReplyVoiceID(cfg.HardwareAgentBindings[normalizeThirdPartyID(clientID)])
	if voiceID == normalizeTTSVoiceID(cfg.TTSVoiceID) {
		return a.ttsManager, func() {}, true
	}
	modelPath, err := ttsModelPath()
	if err != nil || !fileExistsLocal(modelPath) {
		return nil, func() {}, false
	}
	voiceDir, err := ttsVoiceDir()
	if err != nil || !fileExistsLocal(filepath.Join(voiceDir, voiceID+".koro")) {
		return nil, func() {}, false
	}
	a.hardwareTTSMu.Lock()
	defer a.hardwareTTSMu.Unlock()
	if a.hardwareTTSManagers == nil {
		a.hardwareTTSManagers = make(map[string]*tts.Manager)
	}
	if manager := a.hardwareTTSManagers[voiceID]; manager != nil {
		return manager, func() {}, true
	}
	manager := tts.NewKokoroManager(modelPath, voiceDir, voiceID)
	a.hardwareTTSManagers[voiceID] = manager
	return manager, func() {}, true
}

func (a *App) unloadHardwareSpeechSynthesizers() {
	if a == nil {
		return
	}
	a.hardwareTTSMu.Lock()
	managers := a.hardwareTTSManagers
	a.hardwareTTSManagers = nil
	a.hardwareTTSMu.Unlock()
	for _, manager := range managers {
		if manager != nil {
			manager.Unload()
		}
	}
}

// backgroundPreloadTTSModel silently downloads TTS model if not present.
func (a *App) backgroundPreloadTTSModel() {
	if !ttsDownloadMu.TryLock() {
		return
	}
	defer ttsDownloadMu.Unlock()

	cfg, err := a.LoadConfig()
	if err != nil {
		return
	}
	if !cfg.TTSEnabled {
		return
	}
	if err := a.ensureTTSAssets(cfg.RemoteHubURL, false); err != nil {
		fmt.Printf("[tts] background preload: all sources failed: %v\n", err)
		return
	}
	if !a.ttsStillConfiguredEnabled() {
		return
	}
	fmt.Println("[tts] background preload: download complete")
	a.initTTSManager()
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

// SpeakPlainText synthesizes a short sentence directly without task-summary wrapping.
func (a *App) SpeakPlainText(input string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return
	}
	go a.speakPlainTextAsync(input)
}

func (a *App) speakPlainTextAsync(input string) {
	if !ttsSpeakMu.TryLock() {
		return
	}
	defer ttsSpeakMu.Unlock()

	cfg, err := a.LoadConfig()
	if err != nil || !cfg.TTSEnabled {
		return
	}
	if err := a.ensureTTSAssetsForUse(cfg.RemoteHubURL, false); err != nil {
		fmt.Printf("[tts] on-demand asset preparation failed: %v\n", err)
		return
	}
	// Recreate lazily if another settings path unloaded the selected voice.
	manager := a.ensureTTSManagerForSynthesis()
	if manager == nil {
		fmt.Println("[tts] manager unavailable after asset preparation")
		return
	}

	if len([]rune(input)) > 80 {
		input = string([]rune(input)[:80])
	}
	wav, err := manager.SynthesizeText(input)
	if err != nil {
		fmt.Printf("[tts] synthesize error: %v\n", err)
		return
	}
	a.emitEvent("tts:audio", base64EncodeWAV(wav))
}

// SynthesizeTTSPreview generates a bounded WAV for settings-quality checks.
// It uses the currently selected voice, including the native English voice.
func (a *App) SynthesizeTTSPreview(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("preview text cannot be empty")
	}
	if len([]rune(text)) > 200 {
		return "", fmt.Errorf("preview text must be at most 200 characters")
	}
	ttsSpeakMu.Lock()
	defer ttsSpeakMu.Unlock()
	cfg, err := a.LoadConfig()
	if err != nil {
		return "", err
	}
	if err := a.ensureTTSAssetsForUse(cfg.RemoteHubURL, true); err != nil {
		return "", err
	}
	manager := a.ensureTTSManagerForSynthesis()
	if manager == nil {
		return "", fmt.Errorf("TTS is unavailable after preparing its assets")
	}
	wav, err := manager.SynthesizeText(text)
	if err != nil {
		return "", err
	}
	return "data:audio/wav;base64," + base64.StdEncoding.EncodeToString(wav), nil
}

func (a *App) speakTextAsync(input string) {
	// Only one synthesis at a time — skip if already speaking
	if !ttsSpeakMu.TryLock() {
		return
	}
	defer ttsSpeakMu.Unlock()

	cfg, err := a.LoadConfig()
	if err != nil || !cfg.TTSEnabled {
		return
	}
	if err := a.ensureTTSAssetsForUse(cfg.RemoteHubURL, false); err != nil {
		fmt.Printf("[tts] on-demand asset preparation failed: %v\n", err)
		return
	}
	manager := a.ensureTTSManagerForSynthesis()
	if manager == nil {
		fmt.Println("[tts] manager unavailable after asset preparation")
		return
	}

	summary := tts.GenerateVoiceSummary(input, 150)
	if summary == "" {
		return
	}

	wav, err := manager.SynthesizeText(summary)
	if err != nil {
		fmt.Printf("[tts] synthesize error: %v\n", err)
		return
	}

	b64 := base64EncodeWAV(wav)
	a.emitEvent("tts:audio", b64)
}

func base64EncodeWAV(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}
