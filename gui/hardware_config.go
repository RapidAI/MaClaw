package main

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/audioconv"
	"github.com/RapidAI/CodeClaw/corelib/tts"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const hardwareWelcomeMaxWAVBytes = 96 * 1024
const hardwareWelcomeMinPeak = 32
const hardwareWelcomeSpeechSpeed float32 = 0.82

var hardwareWelcomeVoiceIDs = map[string]struct{}{
	tts.DefaultEnglishTTSVoiceID: {},
	tts.EnglishFemaleTTSVoiceID:  {},
}

func normalizeHardwareWelcomeVoiceID(voiceID string) (string, error) {
	voiceID = strings.TrimSpace(voiceID)
	if voiceID == "" {
		return tts.EnglishFemaleTTSVoiceID, nil
	}
	if _, ok := hardwareWelcomeVoiceIDs[voiceID]; !ok {
		return "", fmt.Errorf("unsupported English welcome voice %q", voiceID)
	}
	return voiceID, nil
}

func hardwareWelcomePCM16Peak(wav []byte) (int, error) {
	if len(wav) < 44 || string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		return 0, fmt.Errorf("welcome audio is not a PCM WAV")
	}
	for offset := 12; offset+8 <= len(wav); {
		size := int(binary.LittleEndian.Uint32(wav[offset+4 : offset+8]))
		start := offset + 8
		end := start + size
		if size < 0 || end < start || end > len(wav) {
			return 0, fmt.Errorf("welcome audio contains a malformed WAV chunk")
		}
		if string(wav[offset:offset+4]) == "data" {
			if size == 0 || size%2 != 0 {
				return 0, fmt.Errorf("welcome audio contains no playable PCM samples")
			}
			peak := 0
			for i := start; i < end; i += 2 {
				sample := int(int16(binary.LittleEndian.Uint16(wav[i : i+2])))
				if sample < 0 {
					sample = -sample
				}
				if sample > peak {
					peak = sample
				}
			}
			return peak, nil
		}
		offset = end + (size & 1)
	}
	return 0, fmt.Errorf("welcome audio contains no PCM data chunk")
}

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

// ensureDefaultHardwareWelcome installs the embedded greeting only when the
// config has no selected/generated audio. Existing user choices are never
// overwritten. The returned bool tells the caller whether config persistence
// is needed.
func (a *App) ensureDefaultHardwareWelcome(cfg *corelib.AppConfig) (bool, error) {
	if cfg == nil || strings.TrimSpace(cfg.HardwareWelcomeAudioPath) != "" {
		return false, nil
	}
	wav, err := prepareHardwareWelcomeWAV(defaultHardwareWelcomeWAV)
	if err != nil {
		return false, fmt.Errorf("prepare embedded welcome audio: %w", err)
	}
	path := a.hardwareWelcomePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, wav, 0o600); err != nil {
			return false, err
		}
	} else if err != nil {
		return false, err
	}
	cfg.HardwareWelcomeAudioPath = path
	return true, nil
}

// prepareHardwareWelcomeWAV validates the complete RIFF structure and
// normalizes legacy files to the exact format the ESP32 accepts. Checking only
// the RIFF/WAVE magic allows truncated chunks and incompatible sample formats
// to be persisted, which then makes the device reject or repeatedly retry the
// greeting.
func prepareHardwareWelcomeWAV(wav []byte) ([]byte, error) {
	if len(wav) == 0 {
		return nil, fmt.Errorf("welcome audio is empty")
	}
	normalized, err := audioconv.ToWAV(wav, audioconv.FormatWAV)
	if err != nil {
		return nil, fmt.Errorf("welcome audio is invalid: %w", err)
	}
	if len(normalized) <= 44 {
		return nil, fmt.Errorf("welcome audio contains no playable samples")
	}
	if len(normalized) > hardwareWelcomeMaxWAVBytes {
		return nil, fmt.Errorf("welcome audio is too long after conversion (%d KB; maximum %d KB)", len(normalized)/1024, hardwareWelcomeMaxWAVBytes/1024)
	}
	peak, err := hardwareWelcomePCM16Peak(normalized)
	if err != nil {
		return nil, err
	}
	if peak < hardwareWelcomeMinPeak {
		return nil, fmt.Errorf("welcome audio is silent or too quiet to play (PCM peak %d)", peak)
	}
	return normalized, nil
}

func (a *App) saveHardwareWelcomeWAV(wav []byte) (string, error) {
	var err error
	wav, err = prepareHardwareWelcomeWAV(wav)
	if err != nil {
		return "", err
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

// ResetHardwareWelcomeAudio restores the recording embedded in the executable.
// Unlike first-run installation, reset intentionally replaces the stable WAV
// even when the user previously generated or imported another recording.
func (a *App) ResetHardwareWelcomeAudio() (string, error) {
	path, err := a.saveHardwareWelcomeWAV(defaultHardwareWelcomeWAV)
	if err != nil {
		return "", fmt.Errorf("restore embedded welcome audio: %w", err)
	}
	if _, err = a.PatchConfigFields(map[string]interface{}{
		"hardware_welcome_text":       corelib.AppConfigDefaults().HardwareWelcomeText,
		"hardware_welcome_voice_id":   corelib.AppConfigDefaults().HardwareWelcomeVoiceID,
		"hardware_welcome_audio_path": path,
	}); err != nil {
		return "", err
	}
	// The destination path is stable, so explicitly synchronize the replaced
	// bytes instead of relying on config-path change detection.
	if err := a.SyncHardwareWelcome(); err != nil {
		return "", err
	}
	return path, nil
}

// GenerateHardwareWelcomeAudio converts the configured welcome copy into the
// 16 kHz mono PCM WAV format accepted by the ESP32.
func (a *App) GenerateHardwareWelcomeAudio(text, requestedVoiceID string) (string, error) {
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
	modelPath, err := ttsModelPath()
	if err != nil {
		return "", err
	}
	voiceDir, err := ttsVoiceDir()
	if err != nil {
		return "", err
	}
	voiceID, err := normalizeHardwareWelcomeVoiceID(requestedVoiceID)
	if err != nil {
		return "", err
	}
	voiceID, err = ensureKokoroEnglishVoice(voiceDir, voiceID)
	if err != nil {
		return "", err
	}
	// The normal application TTS voice is Mandarin. Welcome is a short English
	// product signature, so synthesize it with a native English voice instead of
	// asking the Mandarin speaker to imitate English phonemes.
	welcomeTTS := tts.NewKokoroManager(modelPath, voiceDir, voiceID)
	defer welcomeTTS.Unload()
	// A measured pace helps this short product signature survive the ESP32's
	// 16 kHz conversion without changing the text stored and shown in settings.
	wav, err := welcomeTTS.SynthesizeTextAtSpeed(text, hardwareWelcomeSpeechSpeed)
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
	if _, err = a.PatchConfigFields(map[string]interface{}{"hardware_welcome_text": text, "hardware_welcome_voice_id": voiceID, "hardware_welcome_audio_path": path}); err != nil {
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
	// Welcome audio may be prepared while hardware is off. Only its remote
	// transport is gated; enabling hardware later performs the normal Hub sync.
	if !cfg.HardwareEnabled {
		return nil
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
		wav, err = prepareHardwareWelcomeWAV(wav)
		if err != nil {
			return err
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
	if !cfg.HardwareEnabled {
		return nil
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

func (a *App) loadHardwareWelcomeWAV() ([]byte, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	path := strings.TrimSpace(cfg.HardwareWelcomeAudioPath)
	if path == "" {
		return nil, fmt.Errorf("generate or select a welcome audio file first")
	}
	wav, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read welcome audio: %w", err)
	}
	wav, err = prepareHardwareWelcomeWAV(wav)
	if err != nil {
		return nil, err
	}
	return wav, nil
}

func (a *App) hardwareWelcomePreviewPayload() (map[string]any, error) {
	wav, err := a.loadHardwareWelcomeWAV()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"reply_type": "audio",
		"type":       "audio",
		"mime_type":  "audio/wav",
		"file_data":  base64.StdEncoding.EncodeToString(wav),
		// Preview is an explicit device-control action. It must be allowed to
		// play even if the ESP32 is still showing the result of a prior command.
		"extra": map[string]any{"hardware_audio_preview": true},
	}, nil
}

// GetHardwareWelcomeAudioDataURL returns the validated WAV for playback by the
// desktop GUI's own audio element. This path intentionally ignores ESP32
// speaker volume because it is a quality preview on the computer speakers.
func (a *App) GetHardwareWelcomeAudioDataURL() (string, error) {
	wav, err := a.loadHardwareWelcomeWAV()
	if err != nil {
		return "", err
	}
	return "data:audio/wav;base64," + base64.StdEncoding.EncodeToString(wav), nil
}

// SendHardwareWelcomeAudioRemote plays the configured welcome WAV on one
// specific Hub-bound ESP32. Keeping the client ID explicit prevents a manual
// preview from being broadcast to every paired device.
func (a *App) SendHardwareWelcomeAudioRemote(clientID string) error {
	a.imGatewaySyncMu.Lock()
	if _, err := a.requireHardwareEnabled(); err != nil {
		a.imGatewaySyncMu.Unlock()
		return err
	}
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		a.imGatewaySyncMu.Unlock()
		return fmt.Errorf("hardware client ID is required")
	}
	payload, err := a.hardwareWelcomePreviewPayload()
	if err != nil {
		a.imGatewaySyncMu.Unlock()
		return err
	}
	hub := a.hubClient()
	if hub == nil || !hub.IsConnected() {
		a.imGatewaySyncMu.Unlock()
		return fmt.Errorf("Hub is not connected; connect MaClaw to Hub and ensure the selected remote ESP32 is online")
	}
	// Physical playback confirmation can take fifteen seconds. This operation
	// has an explicit client ID, so waiting must not freeze controls for other
	// independently bound devices.
	a.imGatewaySyncMu.Unlock()
	return hub.SendDeviceGatewayHardwareReplyConfirmed(clientID, payload)
}

// SendHardwareWelcomeAudio preserves the existing mode-aware API for older
// frontends while the settings UI exposes explicit local and remote previews.
func (a *App) SendHardwareWelcomeAudio() error {
	return fmt.Errorf("select a bound ESP32 and use its remote playback button")
}
