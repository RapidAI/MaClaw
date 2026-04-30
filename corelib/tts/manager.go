// corelib/tts/manager.go — Lazy-load TTS model with auto-unload after idle.
//
// Follows the same pattern as corelib/asr/manager.go.
// Small data files (lexicon, CMU dict, duration caches) are embedded in the binary.
// The GGUF model file (60MB) is loaded from disk (downloaded on demand by GUI).
package tts

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"time"

	ttsembed "github.com/RapidAI/CodeClaw/corelib/tts/embed"
	"github.com/RapidAI/CodeClaw/corelib/modelmanager"
)

const (
	defaultTTSUnloadDelay = 5 * time.Minute
	TTSModelFilename      = "piper-xiao_ya-zh-fp32.gguf"
)

// Manager provides lazy-loaded, auto-unloading TTS.
// Call SynthesizeText; model loads on first use, unloads after idle.
type Manager struct {
	mm *modelmanager.Manager[*PiperModel]
}

// NewManager creates a TTS manager. Model is NOT loaded until first use.
// modelPath is the path to the GGUF model file.
func NewManager(modelPath string) *Manager {
	mm := modelmanager.New(modelmanager.Config[*PiperModel]{
		Name: "tts",
		Load: func() (*PiperModel, error) {
			return loadFullModel(modelPath)
		},
		Close:       func(m *PiperModel) { /* PiperModel has no Close — GC handles it */ },
		UnloadDelay: defaultTTSUnloadDelay,
	})
	return &Manager{mm: mm}
}

// SetUnloadDelay configures idle timeout before model is unloaded.
func (mgr *Manager) SetUnloadDelay(d time.Duration) {
	mgr.mm.SetUnloadDelay(d)
}

// Unload releases the model from memory.
func (mgr *Manager) Unload() {
	mgr.mm.Unload()
}

// Loaded returns true if the model is currently in memory.
func (mgr *Manager) Loaded() bool {
	return mgr.mm.Loaded()
}

// SynthesizeText loads model on demand, synthesizes text to WAV bytes.
func (mgr *Manager) SynthesizeText(text string) ([]byte, error) {
	m, done, err := mgr.mm.Acquire()
	if err != nil {
		return nil, err
	}
	defer done()

	wav, err := m.SynthesizeToWAV(text)
	if err != nil {
		return nil, err
	}
	return wav, nil
}

// SynthesizeAudio loads model on demand, synthesizes text to float32 PCM.
func (mgr *Manager) SynthesizeAudio(text string) ([]float32, int, error) {
	m, done, err := mgr.mm.Acquire()
	if err != nil {
		return nil, 0, err
	}
	defer done()

	audio, err := m.SynthesizeText(text)
	if err != nil {
		return nil, 0, err
	}
	return audio, m.HP.SampleRate, nil
}

// loadFullModel loads the GGUF model and all embedded data files.
func loadFullModel(modelPath string) (*PiperModel, error) {
	// Load lexicon from embedded data
	lexData, err := gunzipBytes(ttsembed.LexiconGz)
	if err != nil {
		return nil, fmt.Errorf("tts: decompress lexicon: %w", err)
	}
	lex, err := LoadPiperLexiconFromReader(bytes.NewReader(lexData))
	if err != nil {
		return nil, fmt.Errorf("tts: load lexicon: %w", err)
	}

	// Load GGUF model
	model, err := NewPiper(modelPath)
	if err != nil {
		return nil, fmt.Errorf("tts: load model: %w", err)
	}
	model.Lex = lex

	// Load CMU dictionary from embedded data
	cmuData, err := gunzipBytes(ttsembed.CMUDictGz)
	if err == nil {
		cmuDict, err := LoadCMUDictFromReader(bytes.NewReader(cmuData))
		if err == nil {
			model.CMUDict = cmuDict
		}
	}

	// Load duration caches from embedded data
	triData, _ := gunzipBytes(ttsembed.DurationTrigramCacheGz)
	biData, _ := gunzipBytes(ttsembed.DurationBigramCacheGz)
	uniData, _ := gunzipBytes(ttsembed.DurationUnigramCacheGz)
	if triData != nil {
		dc, err := LoadDurationCacheFromBytes(triData, biData, uniData)
		if err == nil {
			model.DurCache = dc
		}
	}

	// Load duration MLP from embedded data
	mlpData, _ := gunzipBytes(ttsembed.DurationMLPGz)
	if mlpData != nil {
		w1, b1, w2, b2, err := LoadDurationMLPFromBytes(mlpData)
		if err == nil {
			model.DurMLPW1 = w1
			model.DurMLPB1 = b1
			model.DurMLPW2 = w2
			model.DurMLPB2 = b2
		}
	}

	return model, nil
}

// gunzipBytes decompresses gzipped data.
func gunzipBytes(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty data")
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	return io.ReadAll(gz)
}
