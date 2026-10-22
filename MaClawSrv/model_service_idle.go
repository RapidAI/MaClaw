package main

import (
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// modelResidency keeps model-serve's footprint small: models are loaded
// lazily by the manager on first use, unloaded after an idle period, and
// (by default) only one model stays resident at a time.
type modelResidency struct {
	m        *srvAIModelManager
	idle     time.Duration // 0 = never unload on idle
	oneModel bool

	mu       sync.Mutex
	lastUsed map[string]time.Time
	unload   func(model string) // test hook; defaults to m.resetRuntime
	loaded   func(model string) bool
}

var modelResidencyAll = []string{srvAIModelEmbedding, srvAIModelASR, srvAIModelTTS, srvAIModelOCR, srvAIModelOmniParser}

func newModelResidencyFromEnv(m *srvAIModelManager) *modelResidency {
	idle := 300 * time.Second
	if v := strings.TrimSpace(os.Getenv("MACLAW_MODEL_IDLE_UNLOAD_SEC")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			idle = time.Duration(n) * time.Second
		}
	}
	one := true
	if v := strings.TrimSpace(os.Getenv("MACLAW_MODEL_SERVICE_ONE_MODEL")); v != "" {
		one = envTruthy("MACLAW_MODEL_SERVICE_ONE_MODEL")
	}
	r := &modelResidency{m: m, idle: idle, oneModel: one, lastUsed: map[string]time.Time{}}
	r.unload = m.resetRuntime
	r.loaded = m.modelLoaded
	return r
}

func (m *srvAIModelManager) modelLoaded(model string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch model {
	case srvAIModelEmbedding:
		return m.embeddingMgr != nil
	case srvAIModelASR:
		return m.asrMgr != nil
	case srvAIModelTTS:
		return m.ttsMgr != nil
	case srvAIModelOCR:
		return m.ocrMgr != nil
	case srvAIModelOmniParser:
		return m.yoloParser != nil
	}
	return false
}

// before is called (under the service's inference lock) before running model.
func (r *modelResidency) before(model string) {
	if !r.oneModel {
		return
	}
	freed := false
	for _, other := range modelResidencyAll {
		if other != model && r.loaded(other) {
			r.unload(other)
			r.mu.Lock()
			delete(r.lastUsed, other)
			r.mu.Unlock()
			freed = true
		}
	}
	if freed {
		releaseModelScratch()
	}
}

// after is called (under the inference lock) once the model has run.
func (r *modelResidency) after(model string) {
	r.mu.Lock()
	r.lastUsed[model] = time.Now()
	r.mu.Unlock()
	releaseModelScratch()
}

// sweep unloads models idle longer than r.idle; returns what it unloaded.
func (r *modelResidency) sweep(now time.Time) []string {
	if r.idle <= 0 {
		return nil
	}
	var out []string
	for _, model := range modelResidencyAll {
		r.mu.Lock()
		t, ok := r.lastUsed[model]
		r.mu.Unlock()
		if !r.loaded(model) {
			continue
		}
		if ok && now.Sub(t) < r.idle {
			continue
		}
		if !ok {
			// loaded but never seen by us (e.g. loaded at startup): start the clock.
			r.mu.Lock()
			r.lastUsed[model] = now
			r.mu.Unlock()
			continue
		}
		r.unload(model)
		r.mu.Lock()
		delete(r.lastUsed, model)
		r.mu.Unlock()
		out = append(out, model)
	}
	if len(out) > 0 {
		releaseModelScratch()
	}
	return out
}

// run sweeps periodically; lock is the service's inference lock so an
// unload never races an inference.
func (r *modelResidency) run(lock sync.Locker) {
	if r.idle <= 0 {
		return
	}
	tick := r.idle / 5
	if tick < 5*time.Second {
		tick = 5 * time.Second
	}
	if tick > 30*time.Second {
		tick = 30 * time.Second
	}
	for range time.Tick(tick) {
		lock.Lock()
		out := r.sweep(time.Now())
		lock.Unlock()
		if len(out) > 0 {
			log.Printf("[model-service] unloaded idle models %v", out)
		}
	}
}
