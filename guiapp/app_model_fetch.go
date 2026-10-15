package guiapp

// First-run local model (Qwen3-Embedding-0.6B Q8_0) download + hot-load.
//
// Startup never waits for the model: backgroundPreloadEmbeddingModel starts a
// single-flight embedding.ModelFetcher (resumable .part + HTTP Range, size +
// SHA-256 verified before install). Until it is ready the app runs exactly as
// with no embedder: intent = L1 rules + L2 none + remote L3 (or rules only when
// offline / MACLAW_INTENT_LOCAL_L3=strict), tools = BM25 Track A + intent scope,
// no local L3 head / tool head attached. Head JSON is fetched on its own
// (startHeadAssetFetch), including when this GGUF is already installed.
// When the verified file lands,
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

// skipHeadAssetFetch reports that this process must not download release heads
// into the developer's ~/.maclaw (unit tests, or an App bound to a temp home).
func (a *App) skipHeadAssetFetch() bool {
	if a == nil || a.disableBackgroundEmbeddingForTest {
		return true
	}
	if strings.TrimSpace(a.testHomeDir) != "" {
		return true
	}
	base := strings.ToLower(filepath.Base(os.Args[0]))
	return strings.Contains(base, ".test")
}

// startHeadAssetFetch downloads the intent / tool / interrupt heads named in
// embedding.ReleaseAssets into <models>/<subdir> (GitHub release, then hub;
// size+sha verified). It is independent of whether the Qwen3 GGUF is already
// installed. Existing local heads are kept. When a head lands after the
// embedder is already live, that head is attached immediately
// (attachLocalL3 / attachToolHead are idempotent). The interrupt head is
// loaded lazily on the next relevance call.
func (a *App) startHeadAssetFetch() {
	if a.skipHeadAssetFetch() {
		return
	}
	dir := embedding.DefaultModelsDir()
	if dir == "" {
		log.Printf("[embedding] head asset fetch failed: models dir unavailable")
		return // leave the once unconsumed so a later call can retry
	}
	headAssetFetchOnce.Do(func() {
		a.removeLegacyHeadFiles(dir)
		hub := a.modelHubURL()
		for _, asset := range embedding.ReleaseAssets() {
			if asset.Kind == "model" {
				continue
			}
			asset := asset
			dest := filepath.Join(dir, asset.Subdir, asset.Filename)
			_, statErr := os.Stat(dest)
			existed := statErr == nil
			if !existed {
				log.Printf("[embedding] head asset fetch started kind=%s file=%s", asset.Kind, asset.Filename)
			}
			f := embedding.NewReleaseAssetFetcher(dir, asset, hub)
			f.Logf = log.Printf
			kind, name := asset.Kind, asset.Filename
			f.OnProgress = func(s embedding.FetchStatus) {
				switch s.State {
				case embedding.FetchFailed:
					log.Printf("[embedding] head asset fetch failed kind=%s file=%s err=%s", kind, name, s.Err)
				case embedding.FetchDownloading:
					// 135MB intent head otherwise sits between "started" and
					// "ready" with log_detail off. Emit each 10% once; the
					// fetcher already suppresses repeat percents.
					if p := s.Percent(); p > 0 && p%10 == 0 {
						log.Printf("[embedding] head asset fetch kind=%s file=%s %d%%", kind, name, p)
					}
				}
			}
			f.Start(context.Background(), func(path string) {
				a.removeLegacyHeadFiles(dir) // a stale copy may have landed meanwhile; never loaded either way
				emb := a.activeInterruptEmbedder()
				live := emb != nil && !embedding.IsNoop(emb)
				log.Printf("[embedding] head asset ready: kind=%s file=%s path=%s embedder_live=%v", kind, name, path, live)
				if live {
					switch kind {
					case "intent_head":
						a.attachLocalL3(emb)
					case "tool_head":
						a.attachToolHead(emb)
					}
				}
				a.logHeadStatus("head-asset-ready:" + name)
			})
		}
	})
}

// removeLegacyHeadFiles deletes the intent / tool head files earlier releases
// shipped (embedding.LegacyHeadFilenames: xf / GK / KG intent heads, the
// pre-r5 tool heads). Only those exact names are touched; nothing loads them
// any more, so until the pinned heads have downloaded the app runs L1/L2
// (+ remote L3) and hot-attaches the new heads when they land.
func (a *App) removeLegacyHeadFiles(dir string) {
	for _, p := range embedding.RemoveLegacyHeadFiles(dir) {
		log.Printf("[embedding] removed legacy head file %s", p)
	}
}
