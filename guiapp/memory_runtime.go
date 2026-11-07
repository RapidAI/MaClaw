package guiapp

import (
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

type memoryRuntimeState struct {
	Store       *memory.Store
	Maintenance *memory.Maintenance
	DataDir     string
	Generation  uint64

	mu       sync.Mutex
	active   int
	closing  bool
	idle     chan struct{}
	stopped  chan struct{}
	stopOnce sync.Once
}

func newMemoryRuntimeState(store *memory.Store, maintenance *memory.Maintenance, dataDir string, generation uint64) *memoryRuntimeState {
	return &memoryRuntimeState{
		Store:       store,
		Maintenance: maintenance,
		DataDir:     dataDir,
		Generation:  generation,
		idle:        make(chan struct{}),
		stopped:     make(chan struct{}),
	}
}

func (r *memoryRuntimeState) acquire() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closing || r.Store == nil {
		return false
	}
	r.active++
	return true
}

func (r *memoryRuntimeState) release() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.active > 0 {
		r.active--
	}
	if r.active == 0 && r.closing {
		select {
		case <-r.idle:
		default:
			close(r.idle)
		}
	}
	r.mu.Unlock()
}

func (r *memoryRuntimeState) isClosing() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closing
}

func (r *memoryRuntimeState) stopWhenIdle() {
	if r == nil {
		return
	}
	r.stopOnce.Do(func() {
		r.mu.Lock()
		r.closing = true
		if r.active == 0 {
			select {
			case <-r.idle:
			default:
				close(r.idle)
			}
		}
		r.mu.Unlock()
		go func() {
			<-r.idle
			if r.Store != nil {
				r.Store.Stop()
			}
			close(r.stopped)
		}()
	})
}

func (r *memoryRuntimeState) waitStopped() {
	if r == nil {
		return
	}
	<-r.stopped
}

func (a *App) currentSharedMemoryStore() *memory.Store {
	if a == nil {
		return nil
	}
	if runtime := a.memRuntime.Load(); runtime != nil {
		return runtime.Store
	}
	a.memoryStoreMu.RLock()
	defer a.memoryStoreMu.RUnlock()
	return a.memoryStore
}

func (a *App) currentMemoryRuntime() *memoryRuntimeState {
	if a == nil {
		return nil
	}
	if runtime := a.memRuntime.Load(); runtime != nil {
		return runtime
	}
	a.memoryStoreMu.Lock()
	defer a.memoryStoreMu.Unlock()
	if runtime := a.memRuntime.Load(); runtime != nil {
		return runtime
	}
	if a.memoryStore == nil {
		return nil
	}
	runtime := newMemoryRuntimeState(a.memoryStore, a.memoryMaintenance, a.getMaclawBaseDir(), a.memRuntimeGeneration.Add(1))
	if a.memRuntime.CompareAndSwap(nil, runtime) {
		a.subscribeMemoryMutations(runtime)
		return runtime
	}
	return a.memRuntime.Load()
}

func (a *App) installMemoryRuntime(store *memory.Store, maintenance *memory.Maintenance, dataDir string) *memoryRuntimeState {
	if a == nil || store == nil {
		return nil
	}
	runtime := newMemoryRuntimeState(store, maintenance, dataDir, a.memRuntimeGeneration.Add(1))
	a.memRuntime.Store(runtime)
	a.subscribeMemoryMutations(runtime)
	return runtime
}

func (a *App) detachMemoryRuntime() *memoryRuntimeState {
	if a == nil {
		return nil
	}
	return a.memRuntime.Swap(nil)
}

type memoryRuntimeBinding struct {
	mu      sync.Mutex
	runtime *memoryRuntimeState
	active  int
	idle    chan struct{}
	private bool
}

func (h *IMMessageHandler) acquireMemoryRuntimeForTurn() func() {
	if h == nil {
		return func() {}
	}
	h.memoryRuntimeBinding.mu.Lock()
	private := h.memoryRuntimeBinding.private
	h.memoryRuntimeBinding.mu.Unlock()
	if private || h.app == nil {
		return func() {}
	}
	if h.app.currentMemoryRuntime() == nil {
		h.app.ensureMemoryStore()
	}
	for {
		current := h.app.currentMemoryRuntime()
		if current == nil || !current.acquire() {
			h.clearUnavailableMemoryRuntime()
			return func() {}
		}

		h.memoryRuntimeBinding.mu.Lock()
		if h.memoryRuntimeBinding.runtime == nil {
			h.memoryRuntimeBinding.runtime = current
		}
		if h.memoryRuntimeBinding.runtime == current {
			h.memoryRuntimeBinding.active++
			if h.currentMemoryStore() != current.Store {
				h.SetMemoryStore(current.Store)
				h.RefreshAllMemorySnapshots()
			}
			h.memoryRuntimeBinding.mu.Unlock()
			return func() { h.releaseMemoryRuntimeForTurn(current) }
		}
		if h.memoryRuntimeBinding.active == 0 {
			previous := h.memoryRuntimeBinding.runtime
			h.memoryRuntimeBinding.runtime = current
			h.memoryRuntimeBinding.active = 1
			h.SetMemoryStore(current.Store)
			h.RefreshAllMemorySnapshots()
			h.memoryRuntimeBinding.mu.Unlock()
			if previous != nil {
				previous.stopWhenIdle()
			}
			return func() { h.releaseMemoryRuntimeForTurn(current) }
		}
		idle := h.memoryRuntimeBinding.idle
		if idle == nil {
			idle = make(chan struct{})
			h.memoryRuntimeBinding.idle = idle
		}
		h.memoryRuntimeBinding.mu.Unlock()
		current.release()
		<-idle
	}
}

func (h *IMMessageHandler) clearUnavailableMemoryRuntime() {
	for {
		h.memoryRuntimeBinding.mu.Lock()
		if h.memoryRuntimeBinding.active == 0 {
			h.memoryRuntimeBinding.runtime = nil
			h.SetMemoryStore(nil)
			h.RefreshAllMemorySnapshots()
			h.memoryRuntimeBinding.mu.Unlock()
			return
		}
		idle := h.memoryRuntimeBinding.idle
		if idle == nil {
			idle = make(chan struct{})
			h.memoryRuntimeBinding.idle = idle
		}
		h.memoryRuntimeBinding.mu.Unlock()
		<-idle
	}
}

func (h *IMMessageHandler) releaseMemoryRuntimeForTurn(runtime *memoryRuntimeState) {
	h.memoryRuntimeBinding.mu.Lock()
	if h.memoryRuntimeBinding.active > 0 {
		h.memoryRuntimeBinding.active--
	}
	if h.memoryRuntimeBinding.active == 0 && h.memoryRuntimeBinding.idle != nil {
		close(h.memoryRuntimeBinding.idle)
		h.memoryRuntimeBinding.idle = nil
	}
	h.memoryRuntimeBinding.mu.Unlock()
	runtime.release()
}

func (h *IMMessageHandler) memoryRuntimeClosing() bool {
	if h == nil {
		return false
	}
	h.memoryRuntimeBinding.mu.Lock()
	runtime := h.memoryRuntimeBinding.runtime
	h.memoryRuntimeBinding.mu.Unlock()
	return runtime != nil && runtime.isClosing()
}

func (h *IMMessageHandler) SetPrivateMemoryStore(store *memory.Store) {
	if h == nil {
		return
	}
	h.memoryRuntimeBinding.mu.Lock()
	h.memoryRuntimeBinding.private = true
	h.memoryRuntimeBinding.mu.Unlock()
	h.SetMemoryStore(store)
}
