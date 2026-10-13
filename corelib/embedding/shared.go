package embedding

import (
	"os"
	"sync"
	"sync/atomic"
	"time"
)

var (
	sharedEmbMu        sync.Mutex
	sharedEmb          Embedder
	sharedEmbOnce      bool
	sharedEmbTriedPath string
	sharedEmbTriedMod  time.Time
	sharedEmbReady     atomic.Bool
)

// SharedEmbedderReady reports whether the process-wide embedder (Qwen3) is already
// loaded. It does not take the load mutex, so status/admin reads stay non-blocking
// while ReloadSharedEmbedderIfReady is opening a GGUF.
func SharedEmbedderReady() bool {
	return sharedEmbReady.Load()
}

// SharedEmbedder returns the process-wide frozen default embedder. Since the
// single-model switch this is Qwen3-Embedding-0.6B (the only local model). Consumers
// that persist vectors or train on them (llmpool class head) must tag them
// with EmbedderFamily so a model switch cannot mix vector spaces.
//
// The "256" in the name is historical. It named the intended consumer — the
// 256-dim llmpool class head, which truncates incoming vectors to its own
// HeadDim, a valid MRL slice — not the output width. The embedder returns
// DefaultEmbeddingDim components today, so do not read the name as a dimension
// guarantee; call Dim() instead.
//
// Missing model files fall back to NoopEmbedder. If the file later appears
// (admin download / copy into ~/.maclaw/models), the next call retries load.
func SharedEmbedder() Embedder {
	sharedEmbMu.Lock()
	defer sharedEmbMu.Unlock()
	if sharedEmbOnce && !IsNoop(sharedEmb) {
		return sharedEmb
	}
	reloadSharedEmbedderLocked(false)
	sharedEmbOnce = true
	return sharedEmb
}

// ReloadSharedEmbedderIfReady forces a reload when the current embedder is still
// noop. Call after a successful model download so the running process picks up
// the new GGUF without a restart.
func ReloadSharedEmbedderIfReady() Embedder {
	sharedEmbMu.Lock()
	defer sharedEmbMu.Unlock()
	reloadSharedEmbedderLocked(true)
	sharedEmbOnce = true
	return sharedEmb
}

func reloadSharedEmbedderLocked(force bool) {
	if sharedEmb != nil && !IsNoop(sharedEmb) {
		return
	}
	path := sharedModelPath()
	if path == "" {
		if sharedEmb == nil {
			sharedEmb = NoopEmbedder{}
		}
		return
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() || fi.Size() == 0 {
		if sharedEmb == nil {
			sharedEmb = NoopEmbedder{}
		}
		return
	}
	if !force && sharedEmbTriedPath == path && sharedEmbTriedMod.Equal(fi.ModTime()) && sharedEmb != nil {
		return
	}
	next := NewDefaultEmbedder(path)
	sharedEmbTriedPath = path
	sharedEmbTriedMod = fi.ModTime()
	if IsNoop(next) {
		if sharedEmb == nil {
			sharedEmb = next
		}
		return
	}
	sharedEmb = next
	sharedEmbReady.Store(true)
}

func ResetSharedEmbedderForTest() {
	sharedEmbMu.Lock()
	defer sharedEmbMu.Unlock()
	if sharedEmb != nil && !IsNoop(sharedEmb) {
		sharedEmb.Close()
	}
	sharedEmb = nil
	sharedEmbOnce = false
	sharedEmbTriedPath = ""
	sharedEmbTriedMod = time.Time{}
	sharedEmbReady.Store(false)
}

// sharedModelPath is the file the shared embedder loads: the resolved Qwen3
// GGUF (ResolveQwen3ModelPath).
func sharedModelPath() string {
	p, _ := ResolveQwen3ModelPath(DefaultModelPath())
	return p
}
