// corelib/tts/manager.go - Lazy-load TTS model with auto-unload after idle.
//
// Follows the same pattern as corelib/asr/manager.go.
// Kokoro model and voice files are loaded from disk on demand.
package tts

import (
	"bytes"
	"fmt"
	"path/filepath"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/modelmanager"
	"github.com/RapidAI/CodeClaw/corelib/tts/kokoro"
)

const (
	defaultTTSUnloadDelay = 5 * time.Minute
	TTSModelFilename      = "kokoro-v1_0.koro"
	// Keep the voice pack filename versioned. Existing installations can retain
	// the original four-voice ZIP, so reusing that filename would make the
	// downloader treat an incomplete cached pack as current.
	TTSVoiceZipFilename      = "kokoro_82m_selected_voices_koro_v2.zip"
	TTSAssetDirName          = "kokoro_82m"
	DefaultTTSVoiceID        = "zf_xiaoyi"
	DefaultEnglishTTSVoiceID = "am_adam"
	EnglishFemaleTTSVoiceID  = "af_heart"
)

var SupportedTTSVoiceIDs = []string{"zm_yunxi", "zm_yunyang", "zf_xiaoxiao", "zf_xiaoyi", DefaultEnglishTTSVoiceID, EnglishFemaleTTSVoiceID}

// RequiredTTSVoiceIDs is the complete voice pack needed by the application.
var RequiredTTSVoiceIDs = append([]string{}, SupportedTTSVoiceIDs...)

// Manager provides lazy-loaded, auto-unloading TTS.
// Call SynthesizeText; model loads on first use, unloads after idle.
type Manager struct {
	kokoro *modelmanager.Manager[*KokoroRuntime]
}

type KokoroRuntime struct {
	Model    *kokoro.Model
	Voice    *kokoro.TensorFile
	VoiceDir string
	VoiceID  string
}

// NewManager creates a Kokoro TTS manager. Model is NOT loaded until first use.
// modelPath is the path to the Kokoro model file.
func NewManager(modelPath string) *Manager {
	voiceDir := filepath.Join(filepath.Dir(modelPath), "voices")
	return NewKokoroManager(modelPath, voiceDir, DefaultTTSVoiceID)
}

func NewKokoroManager(modelPath, voiceDir, voiceID string) *Manager {
	if voiceID == "" {
		voiceID = DefaultTTSVoiceID
	}
	mm := modelmanager.New(modelmanager.Config[*KokoroRuntime]{
		Name: "tts-kokoro",
		Load: func() (*KokoroRuntime, error) {
			model, err := kokoro.LoadModel(kokoro.Assets{WeightsPath: modelPath})
			if err != nil {
				return nil, err
			}
			voice, err := model.LoadVoice(voiceDir, voiceID)
			if err != nil {
				return nil, err
			}
			return &KokoroRuntime{Model: model, Voice: voice, VoiceDir: voiceDir, VoiceID: voiceID}, nil
		},
		Close:       func(m *KokoroRuntime) {},
		UnloadDelay: defaultTTSUnloadDelay,
	})
	return &Manager{kokoro: mm}
}

// SetUnloadDelay configures idle timeout before model is unloaded.
func (mgr *Manager) SetUnloadDelay(d time.Duration) {
	if mgr == nil {
		return
	}
	if mgr.kokoro != nil {
		mgr.kokoro.SetUnloadDelay(d)
	}
}

// Unload releases the model from memory.
func (mgr *Manager) Unload() {
	if mgr == nil {
		return
	}
	if mgr.kokoro != nil {
		mgr.kokoro.Unload()
	}
}

// Loaded returns true if the model is currently in memory.
func (mgr *Manager) Loaded() bool {
	if mgr == nil {
		return false
	}
	if mgr.kokoro != nil {
		return mgr.kokoro.Loaded()
	}
	return false
}

// SynthesizeText loads model on demand, synthesizes text to WAV bytes.
func (mgr *Manager) SynthesizeText(text string) ([]byte, error) {
	return mgr.SynthesizeTextAtSpeed(text, 1)
}

// SynthesizeTextAtSpeed is SynthesizeText with an explicit Kokoro duration
// multiplier. Values below 1 speak more slowly and are useful for short device
// prompts where intelligibility matters more than throughput.
func (mgr *Manager) SynthesizeTextAtSpeed(text string, speed float32) ([]byte, error) {
	if mgr == nil {
		return nil, fmt.Errorf("tts: manager not available")
	}
	pcm, sampleRate, err := mgr.SynthesizeAudioAtSpeed(text, speed)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := kokoro.WriteWAVTo(&buf, pcm, sampleRate); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// SynthesizeAudio loads model on demand, synthesizes text to float32 PCM.
func (mgr *Manager) SynthesizeAudio(text string) ([]float32, int, error) {
	return mgr.SynthesizeAudioAtSpeed(text, 1)
}

// SynthesizeAudioAtSpeed synthesizes text using an explicit Kokoro speed.
func (mgr *Manager) SynthesizeAudioAtSpeed(text string, speed float32) ([]float32, int, error) {
	if mgr == nil {
		return nil, 0, fmt.Errorf("tts: manager not available")
	}
	phonemes := KokoroTextToPhonemes(text)
	if phonemes == "" {
		return nil, 0, fmt.Errorf("tts: text produced no Kokoro phonemes")
	}
	return mgr.SynthesizeKokoroPhonemes(phonemes, speed)
}

func (mgr *Manager) SynthesizeKokoroPhonemes(phonemes string, speed float32) ([]float32, int, error) {
	if mgr == nil || mgr.kokoro == nil {
		return nil, 0, fmt.Errorf("tts: Kokoro manager not available")
	}
	rt, done, err := mgr.kokoro.Acquire()
	if err != nil {
		return nil, 0, err
	}
	defer done()
	if speed <= 0 {
		speed = 1
	}
	pcm, err := rt.Model.SynthesizePhonemes(phonemes, rt.Voice, speed)
	if err != nil {
		return nil, 0, err
	}
	return pcm, kokoro.DefaultSampleRate, nil
}
