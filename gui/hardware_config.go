package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/audioconv"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const hardwareWelcomeMaxWAVBytes = 96 * 1024

// hardwareWelcomeConfigChanged reports whether Hub's durable boot greeting
// needs to be refreshed. The text is intentionally excluded: only the enabled
// state and converted WAV affect what the paired ESP32 receives.
func hardwareWelcomeConfigChanged(oldConfig, newConfig corelib.AppConfig) bool {
	return oldConfig.HardwareWelcomeEnabled != newConfig.HardwareWelcomeEnabled ||
		strings.TrimSpace(oldConfig.HardwareWelcomeAudioPath) != strings.TrimSpace(newConfig.HardwareWelcomeAudioPath)
}

func hardwareVolumeChanged(oldConfig, newConfig corelib.AppConfig) bool {
	return oldConfig.HardwareVolume != newConfig.HardwareVolume
}

func (a *App) hardwareWelcomePath() string {
	return filepath.Join(a.GetDataDir(), "hardware", "welcome.wav")
}

func (a *App) saveHardwareWelcomeWAV(wav []byte) (string, error) {
	if len(wav) == 0 {
		return "", fmt.Errorf("welcome audio is empty")
	}
	if len(wav) > hardwareWelcomeMaxWAVBytes {
		return "", fmt.Errorf("welcome audio is too long after conversion (%d KB; maximum %d KB)", len(wav)/1024, hardwareWelcomeMaxWAVBytes/1024)
	}
	path := a.hardwareWelcomePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, wav, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// GenerateHardwareWelcomeAudio converts the configured welcome copy into the
// 16 kHz mono PCM WAV format accepted by the ESP32.
func (a *App) GenerateHardwareWelcomeAudio(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("welcome text cannot be empty")
	}
	if utf8.RuneCountInString(text) > 80 {
		return "", fmt.Errorf("welcome text must be at most 80 characters")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return "", err
	}
	if err := a.ensureTTSAssetsForUse(cfg.RemoteHubURL, true); err != nil {
		return "", fmt.Errorf("prepare TTS: %w", err)
	}
	if a.ttsManager == nil {
		a.initTTSManager()
	}
	if a.ttsManager == nil {
		return "", fmt.Errorf("TTS is unavailable; enable TTS and download its model first")
	}
	wav, err := a.ttsManager.SynthesizeText(text)
	if err != nil {
		return "", err
	}
	wav, err = audioconv.ToWAV(wav, audioconv.FormatWAV)
	if err != nil {
		return "", fmt.Errorf("convert generated audio: %w", err)
	}
	path, err := a.saveHardwareWelcomeWAV(wav)
	if err != nil {
		return "", err
	}
	if _, err = a.PatchConfigFields(map[string]interface{}{"hardware_welcome_text": text, "hardware_welcome_audio_path": path}); err != nil {
		return "", err
	}
	// The generated file always has the same stable path. Synchronize directly
	// so Hub receives a regenerated WAV even when no config field changed.
	if err := a.SyncHardwareWelcome(); err != nil {
		return "", err
	}
	return path, nil
}

// SelectHardwareWelcomeAudio imports a supported audio file and turns it into
// a bounded ESP-playable WAV. Native conversion supports WAV, MP3 and Ogg/Opus.
func (a *App) SelectHardwareWelcomeAudio() (string, error) {
	if a == nil || a.ctx == nil {
		return "", fmt.Errorf("desktop runtime is unavailable")
	}
	source, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "选择欢迎音频", Filters: []runtime.FileFilter{
		{DisplayName: "音频文件 (*.wav;*.mp3;*.ogg;*.opus)", Pattern: "*.wav;*.mp3;*.ogg;*.opus"},
	}})
	if err != nil || strings.TrimSpace(source) == "" {
		return "", err
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return "", err
	}
	wav, err := audioconv.ToWAV(data, audioconv.FormatFromPath(source))
	if err != nil {
		return "", fmt.Errorf("convert welcome audio: %w", err)
	}
	path, err := a.saveHardwareWelcomeWAV(wav)
	if err != nil {
		return "", err
	}
	if _, err = a.PatchConfigFields(map[string]interface{}{"hardware_welcome_text": "", "hardware_welcome_audio_path": path}); err != nil {
		return "", err
	}
	// Importing replaces the WAV in place, so its path alone cannot signal a
	// changed recording to Hub.
	if err := a.SyncHardwareWelcome(); err != nil {
		return "", err
	}
	return path, nil
}

// SyncHardwareWelcome makes the boot-time greeting durable in Hub mode. The
// local gateway reads the same desktop config at the next hardware boot, so it
// does not need a second relay.
func (a *App) SyncHardwareWelcome() error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	hub := a.hubClient()
	if hub == nil || !hub.IsConnected() {
		return nil
	}
	payload := map[string]any{
		"reply_type":      "hardware_welcome_config",
		"welcome_enabled": cfg.HardwareWelcomeEnabled,
		// Treat the desktop configuration as the source of truth. Without this
		// explicit replacement marker an empty local audio path would leave an
		// earlier Hub-hosted WAV in place, which could play unexpectedly if the
		// switch is enabled again later.
		"replace_audio": true,
	}
	if path := strings.TrimSpace(cfg.HardwareWelcomeAudioPath); path != "" {
		wav, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read welcome audio: %w", err)
		}
		if len(wav) == 0 || len(wav) > hardwareWelcomeMaxWAVBytes {
			return fmt.Errorf("welcome audio exceeds the hardware limit")
		}
		payload["file_data"] = base64.StdEncoding.EncodeToString(wav)
		payload["mime_type"] = "audio/wav"
	}
	return hub.SendDeviceGatewayHardwareReply(payload)
}

// SyncHardwareVolume makes the selected speaker level durable in Hub mode.
// Local devices read it from desktop configuration during their handshake;
// Hub devices need this explicit authenticated relay for later reconnects.
func (a *App) SyncHardwareVolume() error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	if cfg.HardwareVolume < 0 || cfg.HardwareVolume > 100 {
		return fmt.Errorf("hardware volume must be between 0 and 100")
	}
	hub := a.hubClient()
	if hub == nil || !hub.IsConnected() {
		return nil
	}
	return hub.SendDeviceGatewayHardwareConfig(map[string]any{"volume": cfg.HardwareVolume})
}

// SendHardwareWelcomeAudio delivers the stored welcome WAV for immediate
// preview. The ESP honours its normal audio capability and queue limit.
func (a *App) SendHardwareWelcomeAudio() error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	path := strings.TrimSpace(cfg.HardwareWelcomeAudioPath)
	if path == "" {
		return fmt.Errorf("generate or select a welcome audio file first")
	}
	wav, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read welcome audio: %w", err)
	}
	if len(wav) > hardwareWelcomeMaxWAVBytes {
		return fmt.Errorf("welcome audio exceeds the hardware limit")
	}
	payload := map[string]any{"reply_type": "audio", "type": "audio", "mime_type": "audio/wav", "file_data": base64.StdEncoding.EncodeToString(wav)}
	if a.thirdPartyGateway != nil {
		a.thirdPartyGateway.broadcastHardwareAudio(payload)
	}
	if hub := a.hubClient(); hub != nil && hub.IsConnected() {
		return hub.SendDeviceGatewayHardwareReply(payload)
	}
	return nil
}
