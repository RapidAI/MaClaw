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

// SharedGemma256 returns the process-wide frozen default embedder. Since the
// single-model switch this is Qwen3-Embedding-0.6B (EmbeddingGemma only when
// MACLAW_EMBED_MODEL=gemma); the name is kept for API stability. Consumers
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
	path := sharedModelPath()
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

// sharedModelPath is the file the shared embedder loads: the resolved Qwen3
// GGUF (ResolveQwen3ModelPath).
func sharedModelPath() string {
	p, _ := ResolveQwen3ModelPath(DefaultModelPath())
	return p
}
