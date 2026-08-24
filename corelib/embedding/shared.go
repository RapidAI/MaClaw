package embedding

import (
	"os"
	"sync"
	"sync/atomic"
	"time"
)

var (
	sharedGemmaMu        sync.Mutex
	sharedGemma          Embedder
	sharedGemmaOnce      bool
	sharedGemmaTriedPath string
	sharedGemmaTriedMod  time.Time
	sharedGemmaReady     atomic.Bool
)

// SharedGemmaReady reports whether the process-wide Gemma embedder is already
// loaded. It does not take the load mutex, so status/admin reads stay non-blocking
// while ReloadSharedGemmaIfReady is opening a GGUF.
func SharedGemmaReady() bool {
	return sharedGemmaReady.Load()
}

// SharedGemma256 returns the process-wide frozen Gemma embedder (256-dim).
// Missing model files fall back to NoopEmbedder. If the file later appears
// (admin download / copy into ~/.maclaw/models), the next call retries load.
// Tests should not rely on Gemma.
func SharedGemma256() Embedder {
	sharedGemmaMu.Lock()
	defer sharedGemmaMu.Unlock()
	if sharedGemmaOnce && !IsNoop(sharedGemma) {
		return sharedGemma
	}
	reloadSharedGemmaLocked(false)
	sharedGemmaOnce = true
	return sharedGemma
}

// ReloadSharedGemmaIfReady forces a reload when the current embedder is still
// noop. Call after a successful model download so the running process picks up
// the new GGUF without a restart.
func ReloadSharedGemmaIfReady() Embedder {
	sharedGemmaMu.Lock()
	defer sharedGemmaMu.Unlock()
	reloadSharedGemmaLocked(true)
	sharedGemmaOnce = true
	return sharedGemma
}

func reloadSharedGemmaLocked(force bool) {
	if sharedGemma != nil && !IsNoop(sharedGemma) {
		return
	}
	path := DefaultModelPath()
	if path == "" {
		if sharedGemma == nil {
			sharedGemma = NoopEmbedder{}
		}
		return
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		if sharedGemma == nil {
			sharedGemma = NoopEmbedder{}
		}
		return
	}
	if !force && sharedGemmaTriedPath == path && sharedGemmaTriedMod.Equal(fi.ModTime()) && sharedGemma != nil {
		return
	}
	next := NewDefaultEmbedder(path)
	sharedGemmaTriedPath = path
	sharedGemmaTriedMod = fi.ModTime()
	if IsNoop(next) {
		if sharedGemma == nil {
			sharedGemma = next
		}
		return
	}
	sharedGemma = next
	sharedGemmaReady.Store(true)
}

func ResetSharedGemmaForTest() {
	sharedGemmaMu.Lock()
	defer sharedGemmaMu.Unlock()
	if sharedGemma != nil && !IsNoop(sharedGemma) {
		sharedGemma.Close()
	}
	sharedGemma = nil
	sharedGemmaOnce = false
	sharedGemmaTriedPath = ""
	sharedGemmaTriedMod = time.Time{}
	sharedGemmaReady.Store(false)
}
