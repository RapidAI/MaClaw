// Package modelmanager provides a generic lazy-load + auto-unload lifecycle
// manager for AI models (embedding, ASR, YOLO, etc.).
//
// The manager holds a model instance of type T. The model is loaded on first
// use (Acquire) and automatically unloaded after an idle timeout. Subsequent
// Acquire calls after unload transparently reload the model.
//
// Usage:
//
//	mgr := modelmanager.New(modelmanager.Config[*MyModel]{
//	    Name:        "my-model",
//	    Load:        func() (*MyModel, error) { return LoadMyModel(path) },
//	    Close:       func(m *MyModel) { m.Close() }, // optional
//	    UnloadDelay: 5 * time.Minute,
//	})
//	defer mgr.Shutdown()
//
//	model, done, err := mgr.Acquire()
//	if err != nil { ... }
//	defer done() // schedules auto-unload timer
//	model.DoWork()
package modelmanager

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// Config configures a ModelManager instance.
type Config[T any] struct {
	// Name is a human-readable label for log messages (e.g. "asr", "yolo").
	Name string

	// Load creates a new model instance. Called lazily on first Acquire().
	// Must be safe to call multiple times (after Unload, the model may be reloaded).
	Load func() (T, error)

	// Close releases resources held by the model (e.g. mmap, GPU memory).
	// Optional — nil means no cleanup is needed (GC handles it).
	Close func(T)

	// UnloadDelay is the idle timeout before the model is automatically unloaded.
	// Zero or negative means no auto-unload (model stays loaded until Shutdown).
	UnloadDelay time.Duration
}

// Manager manages the lifecycle of a single model instance of type T.
// It is safe for concurrent use.
type Manager[T any] struct {
	name        string // resolved once at construction
	load        func() (T, error)
	close       func(T)
	mu          sync.Mutex
	model       T
	loaded      bool
	unloadDelay time.Duration
	unloadTimer *time.Timer
}

// New creates a ModelManager. The model is NOT loaded until first Acquire().
func New[T any](cfg Config[T]) *Manager[T] {
	name := cfg.Name
	if name == "" {
		name = "model"
	}
	return &Manager[T]{
		name:        name,
		load:        cfg.Load,
		close:       cfg.Close,
		unloadDelay: cfg.UnloadDelay,
	}
}

// Acquire returns the model, loading it if necessary.
// The returned done function MUST be called when the caller is finished using
// the model — it schedules the auto-unload timer. The model pointer is only
// guaranteed valid between Acquire and done; do not store it beyond that scope.
//
//	model, done, err := mgr.Acquire()
//	if err != nil { return err }
//	defer done()
//	// use model — do not store model beyond this scope
func (m *Manager[T]) Acquire() (model T, done func(), err error) {
	m.mu.Lock()
	// Stop any pending unload while the model is in use.
	if m.unloadTimer != nil {
		m.unloadTimer.Stop()
		m.unloadTimer = nil
	}
	if !m.loaded {
		log.Printf("[%s] loading model", m.name)
		t0 := time.Now()
		loaded, loadErr := m.load()
		if loadErr != nil {
			m.mu.Unlock()
			var zero T
			return zero, func() {}, fmt.Errorf("%s: load: %w", m.name, loadErr)
		}
		m.model = loaded
		m.loaded = true
		log.Printf("[%s] model loaded in %v", m.name, time.Since(t0))
	}
	model = m.model
	m.mu.Unlock()

	done = func() {
		m.mu.Lock()
		m.scheduleUnload()
		m.mu.Unlock()
	}
	return model, done, nil
}

// scheduleUnload starts (or restarts) the idle unload timer. Must be called with mu held.
func (m *Manager[T]) scheduleUnload() {
	if m.unloadDelay <= 0 {
		return // no auto-unload configured
	}
	if m.unloadTimer != nil {
		m.unloadTimer.Stop()
	}
	m.unloadTimer = time.AfterFunc(m.unloadDelay, func() {
		m.Unload()
	})
}

// Unload releases the model from memory. Safe to call even if not loaded.
// The model will be reloaded on next Acquire().
func (m *Manager[T]) Unload() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unloadLocked()
}

func (m *Manager[T]) unloadLocked() {
	if !m.loaded {
		return
	}
	log.Printf("[%s] unloading model (idle timeout)", m.name)
	if m.close != nil {
		m.close(m.model)
	}
	var zero T
	m.model = zero
	m.loaded = false
	if m.unloadTimer != nil {
		m.unloadTimer.Stop()
		m.unloadTimer = nil
	}
}

// Loaded returns true if the model is currently in memory.
func (m *Manager[T]) Loaded() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loaded
}

// SetUnloadDelay changes the idle timeout. Takes effect on the next done() call.
func (m *Manager[T]) SetUnloadDelay(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unloadDelay = d
}

// Shutdown unloads the model. After Shutdown, Acquire will still work
// (it will reload the model), but Shutdown signals intent to stop using
// the manager and is a good place to ensure cleanup before process exit.
func (m *Manager[T]) Shutdown() {
	m.Unload()
}
