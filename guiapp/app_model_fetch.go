package guiapp

// First-run local model (Qwen3-Embedding-0.6B Q8_0) download + hot-load.
//
// Startup never waits for the model: backgroundPreloadEmbeddingModel starts a
// single-flight embedding.ModelFetcher (resumable .part + HTTP Range, size +
// SHA-256 verified before install). Until it is ready the app runs exactly as
// with no embedder: intent = L1 rules + L2 none + remote L3 (or rules only when
// offline / MACLAW_INTENT_LOCAL_L3=strict), tools = BM25 Track A + intent scope,
// no local L3 head / tool head attached. When the verified file lands,
// verifyAndEnableEmbedding loads it through the normal factory and
// activateEmbedderAsync attaches LocalTreeFunc + the tool head and warms the
// ModelID-keyed tool vector cache. locall3.Runtime swaps atomically, so
// in-flight turns finish on whichever L3 they started with.

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

var (
	localModelFetcherOnce sync.Once
	localModelFetcherInst *embedding.ModelFetcher
)

// localModelFetcher returns the process-wide Qwen3 fetcher, or nil when the
// user pinned Gemma or pointed MACLAW_EMBED_MODEL_PATH at a custom file (no
// spec to verify against: load it as-is, never download over it).
func (a *App) localModelFetcher() *embedding.ModelFetcher {
	localModelFetcherOnce.Do(func() {
		if embedding.GemmaPinned() || strings.TrimSpace(os.Getenv(embedding.EmbedModelPathEnv)) != "" {
			return
		}
		gemmaSlot := embedding.DefaultModelPath()
		if gemmaSlot == "" {
			return
		}
		qp := embedding.PreferredQwen3Path(gemmaSlot)
		spec := embedding.Qwen3ModelSpec
		spec.Filename = filepath.Base(qp)
		f := embedding.NewModelFetcher(filepath.Dir(qp), spec)
		f.OnProgress = func(s embedding.FetchStatus) {
			pct := s.Percent()
			if pct < 0 {
				pct = 0
			}
			a.emitDownloadProgressNamed("embedding-download-progress", pct, s.Downloaded, s.Total, s.Err)
			if s.State != embedding.FetchDownloading || pct%10 == 0 {
				log.Printf("[embedding] %s", s.String())
			}
		}
		localModelFetcherInst = f
	})
	return localModelFetcherInst
}

// GetLocalModelStatus is the UI/status-bar view of the local model
// ("local model downloading 42%", "local model ready", ...).
func (a *App) GetLocalModelStatus() map[string]interface{} {
	f := a.localModelFetcher()
	if f == nil {
		return map[string]interface{}{"state": "custom", "status": "local model: custom path / Gemma pinned"}
	}
	s := f.Status()
	return map[string]interface{}{
		"state":      s.State.String(),
		"status":     s.String(),
		"percent":    s.Percent(),
		"downloaded": s.Downloaded,
		"total":      s.Total,
		"path":       s.Path,
		"error":      s.Err,
		"attempt":    s.Attempt,
	}
}

// startLocalModelFetch kicks off (or joins) the background Qwen3 download and
// hot-loads it when verified. Returns false when the fetcher does not apply
// (Gemma pinned / custom path) so the caller keeps its legacy behaviour.
func (a *App) startLocalModelFetch() bool {
	f := a.localModelFetcher()
	if f == nil {
		return false
	}
	f.Start(context.Background(), func(path string) {
		if !a.vectorSearchConfiguredEnabled() {
			log.Printf("[embedding] local model ready at %s (vector search disabled: not loading)", path)
			return
		}
		log.Printf("[embedding] local model verified (%s) — hot-loading, no restart", path)
		// Gemma-slot path keeps sharedEmbeddingEmbedder's cache key stable;
		// the factory picks the Qwen3 file that now exists next to it.
		a.verifyAndEnableEmbedding(embedding.DefaultModelPath())
	})
	return true
}

// qwen3UpgradeAvailable reports whether the cached embedder is the Gemma
// fallback while a verified Qwen3 file is now present (first-run download
// finished): sharedEmbeddingEmbedder then swaps instead of returning Gemma.
func qwen3UpgradeAvailable(cur embedding.Embedder, gemmaSlot string) bool {
	if cur == nil || embedding.IsNoop(cur) || embedding.GemmaPinned() {
		return false
	}
	if embedding.EmbedderFamily(cur) != "gemma" {
		return false
	}
	qp := embedding.PreferredQwen3Path(gemmaSlot)
	if qp == "" {
		return false
	}
	if strings.TrimSpace(os.Getenv(embedding.EmbedModelPathEnv)) != "" {
		_, err := os.Stat(qp)
		return err == nil
	}
	spec := embedding.Qwen3ModelSpec
	spec.Filename = filepath.Base(qp)
	return embedding.VerifyModelFile(qp, spec) == nil
}
