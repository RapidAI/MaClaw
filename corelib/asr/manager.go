// corelib/asr/manager.go — Lazy-load ASR model with auto-unload after idle.
package asr

import (
	"fmt"
	"log"
	"sync"
	"time"
)

const defaultUnloadDelay = 5 * time.Minute

// Manager provides lazy-loaded, auto-unloading ASR.
// Call Transcribe/TranscribeWAV; model loads on first use, unloads after idle.
type Manager struct {
	modelPath    string
	unloadDelay  time.Duration
	mu           sync.Mutex
	model        *MoonshineModel
	unloadTimer  *time.Timer
}

// NewManager creates an ASR manager. Model is NOT loaded until first use.
func NewManager(modelPath string) *Manager {
	return &Manager{modelPath: modelPath, unloadDelay: defaultUnloadDelay}
}

// SetUnloadDelay configures idle timeout before model is unloaded.
func (mgr *Manager) SetUnloadDelay(d time.Duration) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	mgr.unloadDelay = d
}

// ensure loads the model if not already loaded, resets unload timer.
func (mgr *Manager) ensure() (*MoonshineModel, error) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if mgr.model == nil {
		log.Printf("[asr] loading model from %s", mgr.modelPath)
		t0 := time.Now()
		m, err := NewMoonshine(mgr.modelPath)
		if err != nil {
			return nil, fmt.Errorf("asr: load model: %w", err)
		}
		mgr.model = m
		log.Printf("[asr] model loaded in %v", time.Since(t0))
	}
	mgr.resetTimer()
	return mgr.model, nil
}

func (mgr *Manager) resetTimer() {
	if mgr.unloadTimer != nil {
		mgr.unloadTimer.Stop()
	}
	mgr.unloadTimer = time.AfterFunc(mgr.unloadDelay, func() {
		mgr.Unload()
	})
}

// Unload releases the model from memory.
func (mgr *Manager) Unload() {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if mgr.model != nil {
		log.Printf("[asr] unloading model (idle timeout)")
		mgr.model = nil // GC will reclaim the ~200MB weights
	}
	if mgr.unloadTimer != nil {
		mgr.unloadTimer.Stop()
		mgr.unloadTimer = nil
	}
}

// Loaded returns true if the model is currently in memory.
func (mgr *Manager) Loaded() bool {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	return mgr.model != nil
}

// Transcribe loads model on demand, transcribes PCM, schedules unload.
func (mgr *Manager) Transcribe(pcm []float32) (string, error) {
	m, err := mgr.ensure()
	if err != nil {
		return "", err
	}
	result, err := m.Transcribe(pcm)
	// Reset unload timer after transcription completes (not just at load time)
	mgr.mu.Lock()
	mgr.resetTimer()
	mgr.mu.Unlock()
	return result, err
}

// TranscribeWAV loads model on demand, reads WAV, transcribes.
func (mgr *Manager) TranscribeWAV(wavData []byte) (string, error) {
	pcm, err := WAVToFloat32(wavData)
	if err != nil {
		return "", err
	}
	return mgr.Transcribe(pcm)
}
