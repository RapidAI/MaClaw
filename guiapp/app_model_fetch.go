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
// user pointed MACLAW_EMBED_MODEL_PATH at a custom file (no spec to verify
// against: load it as-is, never download over it).
func (a *App) localModelFetcher() *embedding.ModelFetcher {
	localModelFetcherOnce.Do(func() {
		if strings.TrimSpace(os.Getenv(embedding.EmbedModelPathEnv)) != "" {
			return
		}
		slot := embedding.DefaultModelPath()
		if slot == "" {
			return
		}
		qp := embedding.PreferredQwen3Path(slot)
		spec := embedding.Qwen3ModelSpec
		spec.Filename = filepath.Base(qp)
		// GitHub release Model_Release first, then the hub mirror — the same
		// order as the ASR/YOLO/OCR downloads.
		spec = spec.WithHubFallback(a.modelHubURL())
		f := embedding.NewModelFetcher(filepath.Dir(qp), spec)
		f.Logf = log.Printf
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
		return map[string]interface{}{"state": "custom", "status": "local model: custom path (" + embedding.EmbedModelPathEnv + ")"}
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
// (custom MACLAW_EMBED_MODEL_PATH) so the caller loads that file as-is.
func (a *App) startLocalModelFetch() bool {
	f := a.localModelFetcher()
	if f == nil {
		return false
	}
	a.startHeadAssetFetch()
	f.Start(context.Background(), func(path string) {
		if !a.vectorSearchConfiguredEnabled() {
			a.removeLegacyGemmaModel()
			log.Printf("[embedding] local model ready at %s (vector search disabled: not loading)", path)
			return
		}
		log.Printf("[embedding] local model verified (%s) — hot-loading, no restart", path)
		embedding.ResetQwen3LoadBackoff() // a failure before the download must not delay the hot-load
		a.removeLegacyGemmaModel()        // migrated users: drop the stale EmbeddingGemma cache file
		a.verifyAndEnableEmbedding(embedding.DefaultModelPath())
	})
	return true
}

func (a *App) modelHubURL() string {
	if cfg, err := a.LoadConfig(); err == nil {
		return strings.TrimRight(cfg.RemoteHubURL, "/")
	}
	return ""
}

var headAssetFetchOnce sync.Once

// startHeadAssetFetch downloads the intent / tool heads named in
// embedding.ReleaseAssets into <models>/intent_heads and <models>/tool_heads
// (GitHub release, then hub; size+sha verified). Existing local heads are
// kept. When a head lands after the embedder is already live, it is attached
// immediately (attachLocalL3 / attachToolHead are idempotent).
func (a *App) startHeadAssetFetch() {
	headAssetFetchOnce.Do(func() {
		dir := embedding.DefaultModelsDir()
		if dir == "" {
			return
		}
		hub := a.modelHubURL()
		for _, asset := range embedding.ReleaseAssets() {
			if asset.Kind == "model" {
				continue
			}
			f := embedding.NewReleaseAssetFetcher(dir, asset, hub)
			f.Logf = log.Printf
			f.Start(context.Background(), func(path string) {
				if emb := a.activeInterruptEmbedder(); emb != nil && !embedding.IsNoop(emb) {
					a.attachLocalL3(emb)
					a.attachToolHead(emb)
				}
			})
		}
	})
}
