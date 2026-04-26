// corelib/asr/manager.go — Lazy-load ASR model with auto-unload after idle.
//
// Delegates lifecycle management to modelmanager.Manager[*MoonshineModel].
package asr

import (
	"time"

	"github.com/RapidAI/CodeClaw/corelib/modelmanager"
)

const defaultUnloadDelay = 5 * time.Minute

// Manager provides lazy-loaded, auto-unloading ASR.
// Call Transcribe/TranscribeWAV; model loads on first use, unloads after idle.
type Manager struct {
	mm *modelmanager.Manager[*MoonshineModel]
}

// NewManager creates an ASR manager. Model is NOT loaded until first use.
func NewManager(modelPath string) *Manager {
	mm := modelmanager.New(modelmanager.Config[*MoonshineModel]{
		Name:        "asr",
		Load:        func() (*MoonshineModel, error) { return NewMoonshine(modelPath) },
		Close:       func(m *MoonshineModel) { m.Close() },
		UnloadDelay: defaultUnloadDelay,
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
