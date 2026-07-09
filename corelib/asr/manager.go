// corelib/asr/manager.go — Lazy-load ASR model with auto-unload after idle.
//
// Supports both Moonshine and SenseVoice models via the ASRModel interface.
// Model type is auto-detected from the GGUF file metadata.
package asr

import (
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding/gguf"
	"github.com/RapidAI/CodeClaw/corelib/modelmanager"
)

const defaultUnloadDelay = 5 * time.Minute

// ASRModel is the interface for speech recognition models.
type ASRModel interface {
	Transcribe(pcm []float32) (string, error)
	Close()
}

// Manager provides lazy-loaded, auto-unloading ASR.
// Call Transcribe/TranscribeWAV; model loads on first use, unloads after idle.
type Manager struct {
	mm *modelmanager.Manager[ASRModel]
}

// NewManager creates an ASR manager. Model is NOT loaded until first use.
// Auto-detects model type (Moonshine or SenseVoice) from GGUF metadata.
func NewManager(modelPath string) *Manager {
	mm := modelmanager.New(modelmanager.Config[ASRModel]{
		Name: "asr",
		Load: func() (ASRModel, error) {
			return loadASRModel(modelPath)
		},
		Close:       func(m ASRModel) { m.Close() },
		UnloadDelay: defaultUnloadDelay,
	})
	return &Manager{mm: mm}
}

// loadASRModel auto-detects model architecture and loads accordingly.
func loadASRModel(modelPath string) (ASRModel, error) {
	arch := detectASRArch(modelPath)
	switch arch {
	case "sensevoice":
		return NewSenseVoice(modelPath)
	default:
		return NewMoonshine(modelPath)
	}
}

// detectASRArch peeks at GGUF metadata to determine model architecture.
func detectASRArch(modelPath string) string {
	// Quick check: filename heuristic first
	lower := strings.ToLower(modelPath)
	if strings.Contains(lower, "sensevoice") {
		return "sensevoice"
	}

	// Open GGUF and check metadata keys
	mf, err := gguf.OpenMmap(modelPath)
	if err != nil {
		return "moonshine" // default
	}
	defer mf.CloseMmap()

	// SenseVoice has "encoder.num_blocks" and "encoder.tp_blocks" metadata
	if _, ok := mf.Meta["encoder.num_blocks"]; ok {
		if _, ok2 := mf.Meta["encoder.tp_blocks"]; ok2 {
			return "sensevoice"
		}
	}
	// SenseVoice has ctc.ctc_lo.weight tensor
	if _, ok := mf.Tensors["ctc.ctc_lo.weight"]; ok {
		return "sensevoice"
	}

	return "moonshine"
}

// DefaultManager creates a manager that tries SenseVoice first.
func DefaultManager(modelPath string) *Manager {
	return NewManager(modelPath)
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

// Transcribe loads model on demand, transcribes PCM, schedules unload.
func (mgr *Manager) Transcribe(pcm []float32) (string, error) {
	m, done, err := mgr.mm.Acquire()
	if err != nil {
		return "", err
	}
	defer done()
	return m.Transcribe(pcm)
}

// TranscribeWAV loads model on demand, reads WAV, transcribes.
func (mgr *Manager) TranscribeWAV(wavData []byte) (string, error) {
	pcm, err := WAVToFloat32(wavData)
	if err != nil {
		return "", err
	}
	return mgr.Transcribe(pcm)
}

// NewManagerForModel creates a Manager with an explicit model type override.
func NewManagerForModel(modelPath string, modelType string) (*Manager, error) {
	switch modelType {
	case "sensevoice", "moonshine":
		// valid
	default:
		return nil, fmt.Errorf("asr: unknown model type %q (supported: sensevoice, moonshine)", modelType)
	}

	mm := modelmanager.New(modelmanager.Config[ASRModel]{
		Name: "asr",
		Load: func() (ASRModel, error) {
			switch modelType {
			case "sensevoice":
				return NewSenseVoice(modelPath)
			default:
				return NewMoonshine(modelPath)
			}
		},
		Close:       func(m ASRModel) { m.Close() },
		UnloadDelay: defaultUnloadDelay,
	})
	return &Manager{mm: mm}, nil
}
