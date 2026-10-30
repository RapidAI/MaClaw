package guiapp

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// embeddingModelFilename is the single local embedding model
// (Qwen3-Embedding-0.6B Q8_0, GitHub release Model_Release + hub mirror).
const embeddingModelFilename = embedding.DefaultModelFilename

// embeddingDownloadMu prevents concurrent model downloads.
var embeddingDownloadMu sync.Mutex

// initEarlyClassifier creates the shared classifier instances during app
// startup. Until the LLM route is configured, they return conservative
// unknown/ambiguous results instead of using keyword fallbacks.
//
// L3 (LLM) is wired immediately because it does not depend on vector search.
// L2 (embedding) is wired later by activateEmbedderAsync via SetEmbedder on the
// same instance. The UIC's Classify() method automatically uses whatever layers
// are available at call time:
//   - Before LLM config is ready: conservative unknown
//   - After LLM is wired: tree-only semantic classification
//   - After embedding loads: embedding + LLM fusion
//
// Thread-safe: uses sync.Once to guarantee the UIC is created exactly once,
// regardless of whether initEarlyClassifier or activateEmbedderAsync runs first.
func (a *App) initEarlyClassifier() {
	a.classifierOnce.Do(func() {
		// Create UIC with noop embedder; no local keyword fallback is enabled.
		uic := intent.New(intent.Config{
			Embedder:       embedding.NoopEmbedder{},
			LLMFunc:        a.intentL3Func(),
			LLMContextFunc: a.intentL3ContextFunc(),
			LocalHeadProbe: a.localL3Runtime().ProbeFunc(),
			LLMTimeout:     30 * time.Second,
			// 宁慢勿乱 (2026-09-18): a 12s tree deadline turned every slow-hub
			// turn into a degraded guess — missing tools, rescue chains, wrong
			// surfaces. Waiting out the full LLM timeout trades first-token
			// latency for a correct classification and a stable surface.
			FusionTreeDeadline: 30 * time.Second,
		})
		a.unifiedClassifier = uic

		// Wire to all consumers so they see the classifier immediately.
		if a.toolRouter != nil {
			a.toolRouter.SetUnifiedClassifier(uic)
		}
		if a.capabilityGapDetector != nil {
			a.capabilityGapDetector.SetUnifiedClassifier(uic)
		}
		setUnifiedClassifierForIM(uic)
		// Installed only with the desktop classifier. A package init would
		// make TestDetachedUICFusionAdoptsSingleUpstreamRequest wait out the
		// appear timeout inside its 3s adoption deadline.
		installLateTreeForegroundGate()

		log.Println("[classifier] early init complete: UIC L3 wired, L2 pending async embedding")
	})
}

// installLateTreeForegroundGate holds the background tree retry until the
// user-visible agent loop has claimed and released the foreground lane.
// Classifier HTTP is not that loop: task-context ClassifyContext runs after
// the verdict is scheduled and before the loop starts. The retry's HTTP
// budget starts after the wait, so the chat POST does not cancel it.
func installLateTreeForegroundGate() {
	intent.SetLateTreeForegroundGate(func() {
		waitLateTreeForForegroundIdle(globalLLMScheduler.foregroundLoopBusy, globalLLMScheduler.foregroundHTTPBusy, lateTreeForegroundAppearTimeout, lateTreeForegroundWaitCap, lateTreeForegroundPoll)
	})
}

// ensureEmbeddingEngineSync synchronously loads the embedding model and wires
// it to the UIC during startup, BEFORE the UI becomes interactive. This
// guarantees that the semantic veto layer (Step 4.5) is operational when the
// user sends their first message, preventing BM25 noise from triggering wrong
// workflow templates (e.g. "你是谁？" → nsfc_youth).
//
// Only blocks when:
// 1. VectorSearchEnabled is true in config
// 2. The model file exists on disk (no download needed)
//
// Typical latency: ~1.5s on SSD. If the model file is missing (fresh install,
// needs download), this function returns immediately and the async path
// (ensureConfiguredAIModels → backgroundPreloadEmbeddingModel) handles it.
func (a *App) ensureEmbeddingEngineSync(vectorSearchEnabled bool) {
	if !vectorSearchEnabled {
		log.Println("[startup] embedding sync: skipped (vector search disabled)")
		return
	}
	// Head files are separate release assets. An already-installed GGUF used to
	// skip backgroundPreloadEmbeddingModel, so intent_heads/ and tool_heads/
	// were never downloaded and L3 stayed on the remote tree LLM.
	a.startHeadAssetFetch()

	modelPath := embedding.DefaultModelPath()
	if modelPath == "" {
		log.Println("[startup] embedding sync: skipped (cannot determine home directory)")
		return
	}
	if !embedding.PreferredModelFileExists(modelPath) {
		log.Printf("[startup] embedding sync: skipped (local model not present yet; background fetch will hot-load it)")
		return
	}

	t0 := time.Now()
	log.Printf("[startup] embedding sync: loading model %s", modelPath)

	emb, err := a.sharedEmbeddingEmbedder(modelPath, loadAppEmbedder(false))
	if err != nil {
		log.Printf("[startup] embedding sync: load failed: %v (falling back to async)", err)
		return
	}
	if embedding.IsNoop(emb) {
		return
	}

	// Wire L2 before the UI is interactive. The intent head JSON is about 135MB;
	// parsing it here would hold startup after the GGUF is already loaded.
	// attachClassificationHeads runs that parse off this thread. A fetch that
	// finished before the embedder existed only logged attach=deferred; this
	// call is what binds the file.
	a.initEarlyClassifier()
	a.unifiedClassifier.SetEmbedder(emb)
	go a.attachClassificationHeads(emb)
	// The interrupt path needs the same runtime even before the broader async
	// vector-search wiring completes. Otherwise a task submitted during startup
	// can permanently capture a nil task embedding and fall back to broad intent
	// matching when a second IM message arrives.
	a.wireEmbedderToIMHandlers(emb, true)

	// Mark intent embedding as active so activateEmbedderAsync doesn't double-load.
	a.intentEmbeddingActive.Store(true)
	a.removeLegacyGemmaModel() // Qwen3 is live: drop a migrated user's stale Gemma cache

	log.Printf("[startup] embedding sync: done in %v — UIC L2 now available", time.Since(t0))
}

// embeddingModelsDir returns the effective local AI models directory, creating it if needed.
func embeddingModelsDir() (string, error) {
	dir := embedding.DefaultModelsDir()
	if dir == "" {
		return "", fmt.Errorf("cannot determine models directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// GetVectorSearchEnabled returns the current vector search toggle state.
func (a *App) GetVectorSearchEnabled() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return false
	}
	return cfg.VectorSearchEnabled
}

// VectorSearchStatus describes the runtime state of vector search for the frontend.
type VectorSearchStatus struct {
	Enabled                   bool   `json:"enabled"`                      // config toggle
	ModelExists               bool   `json:"model_exists"`                 // GGUF file on disk
	ModelPath                 string `json:"model_path"`                   // full path to model file
	ModelSize                 int64  `json:"model_size"`                   // file size in bytes
	EmbedderOK                bool   `json:"embedder_ok"`                  // embedder loaded and functional
	EmbedderDim               int    `json:"embedder_dim"`                 // embedding dimension (0 if not loaded)
	EntryCount                int    `json:"entry_count"`                  // total memory entries
	EmbeddedCount             int    `json:"embedded_count"`               // entries with embeddings
	HybridToolRetrievalActive bool   `json:"hybrid_tool_retrieval_active"` // hybrid tool retrieval enabled
}

// GetVectorSearchStatus returns the full runtime status of vector search.
// The frontend uses this to show green/red indicators.
func (a *App) GetVectorSearchStatus() VectorSearchStatus {
	status := VectorSearchStatus{}

	// Config toggle.
	if cfg, err := a.LoadConfig(); err == nil {
		status.Enabled = cfg.VectorSearchEnabled
	}

	// Model file check.
	dir, _ := embeddingModelsDir()
	if dir != "" {
		// Report the file the single-model factory loads: the resolved
		// Qwen3 GGUF (or its download target).
		p := localEmbeddingModelPath(dir)
		status.ModelPath = p
		if fi, err := os.Stat(p); err == nil {
			status.ModelExists = true
			status.ModelSize = fi.Size()
		}
	}

	// Embedder runtime check.
	if a.memoryStore != nil {
		status.EmbedderOK = a.memoryStore.EmbedderActive()
		if status.EmbedderOK {
			status.EmbedderDim = a.memoryStore.EmbedderDim()
		}

		// Count entries with/without embeddings.
		a.memoryStore.RLock()
		entries := a.memoryStore.Entries()
		status.EntryCount = len(entries)
		for _, e := range entries {
			if len(e.Embedding) > 0 {
				status.EmbeddedCount++
			}
		}
		a.memoryStore.RUnlock()
	}

	// Hybrid tool retrieval status.
	if a.toolRouter != nil {
		status.HybridToolRetrievalActive = a.toolRouter.HybridActive()
	}

	return status
}

// SetVectorSearchEnabled persists the vector search toggle and
// activates/deactivates the embedder accordingly.
// When enabling, the embedder is wired asynchronously so the UI is not blocked.
func (a *App) SetVectorSearchEnabled(enabled bool) error {
	if _, err := a.PatchConfigFields(map[string]interface{}{"vector_search_enabled": enabled}); err != nil {
		return err
	}

	if enabled {
		modelPath := embedding.DefaultModelPath()
		emb, err := a.sharedEmbeddingEmbedder(modelPath, loadAppEmbedder(false))
		if err != nil {
			return err
		}
		if embedding.IsNoop(emb) {
			// Model file not found 鈥?config is saved but embedder stays inactive.
			// Download it in the background, then verify and activate it.
			go a.backgroundPreloadEmbeddingModel()
			return nil
		}
		go a.activateEmbedderAsync(emb)
	} else {
		noop := embedding.NoopEmbedder{}
		if a.memoryStore != nil {
			a.memoryStore.SetEmbedder(noop)
		}
		if a.toolRouter != nil {
			a.toolRouter.SetEmbedder(noop)
		}
		if a.remoteSessions != nil && a.remoteSessions.hubClient != nil {
			if handler := a.remoteSessions.hubClient.currentIMHandler(); handler != nil && handler.toolBuilder != nil {
				handler.toolBuilder.SetEmbedder(noop)
			}
		}
		// Allow re-activation if user re-enables vector search later.
		a.embeddingActivated.Store(false)
		// Do NOT clear a.gateIntentClassifier or a.unifiedClassifier.
		// initEarlyClassifier created conservative classifier instances that
		// fail closed until semantic channels are available. Clearing them
		// would leave the Coding Tool Gate with no classifier at all.
	}
	return nil
}

// localEmbeddingModelPath is the Qwen3 GGUF the factory loads from dir
// (resolved file if present, else the download target).
func localEmbeddingModelPath(dir string) string {
	slot := filepath.Join(dir, embeddingModelFilename)
	if q, _ := embedding.ResolveQwen3ModelPath(slot); q != "" {
		return q
	}
	return embedding.PreferredQwen3Path(slot)
}

// CheckEmbeddingModel reports the local embedding model (Qwen3-Embedding-0.6B)
// for Settings → AI Model. Returns { "exists", "path", "size", "name",
// "expected_size", "state", "custom" }; "state" is the background fetcher's
// state (missing / downloading / verifying / ready / failed, or "custom" when
// MACLAW_EMBED_MODEL_PATH points at a user file).
func (a *App) CheckEmbeddingModel() map[string]interface{} {
	out := map[string]interface{}{
		"exists": false, "path": "", "size": int64(0),
		"name":          "Qwen3-Embedding-0.6B (Q8_0)",
		"expected_size": embedding.Qwen3ModelSpec.Size,
		"state":         "missing",
		"custom":        false,
	}
	dir, err := embeddingModelsDir()
	if err != nil {
		return out
	}
	p := localEmbeddingModelPath(dir)
	out["path"] = p
	if f := a.localModelFetcher(); f != nil {
		out["state"] = f.Status().State.String()
	} else {
		out["state"], out["custom"] = "custom", true
	}
	if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Size() > 0 {
		out["exists"], out["size"] = true, fi.Size()
		if out["state"] == "missing" {
			out["state"] = "ready"
		}
	}
	return out
}

// DownloadEmbeddingModel downloads (or re-downloads) the local embedding
// model, Qwen3-Embedding-0.6B Q8_0, plus the intent / tool / interrupt heads:
// GitHub release Model_Release first, then the hub mirror
// (<hub>/api/v1/models/<file>); resumable .part, size + SHA-256 verified
// before install (embedding.ModelFetcher). Progress is emitted via the Wails
// event "embedding-download-progress":
//
//	{ "percent": int, "downloaded": int64, "total": int64, "error": string }
//
// It blocks until the model is installed and (with vector search on) loaded.
func (a *App) DownloadEmbeddingModel() error {
	f := a.localModelFetcher()
	if f == nil {
		return fmt.Errorf("%s points at a custom model file; nothing to download", embedding.EmbedModelPathEnv)
	}
	if !embeddingDownloadMu.TryLock() {
		return nil // a UI download is already running; progress events continue
	}
	defer embeddingDownloadMu.Unlock()
	if _, err := embeddingModelsDir(); err != nil {
		return fmt.Errorf("create models dir: %w", err)
	}
	a.startHeadAssetFetch()
	if _, err := f.EnsureShared(context.Background()); err != nil {
		a.emitDownloadProgressNamed("embedding-download-progress", 0, 0, 0, err.Error())
		return err
	}
	a.removeLegacyGemmaModel()
	embedding.ResetQwen3LoadBackoff()
	return a.verifyDownloadedEmbeddingModel(embedding.DefaultModelPath())
}

// DeleteEmbeddingModel removes the downloaded local embedding model (Settings
// → AI Model "delete"): the live embedder is detached and closed (intent and
// tools degrade to rules + BM25 + remote L3), then the GGUF, its .part /
// .verified side files and any legacy EmbeddingGemma file are deleted. A
// custom MACLAW_EMBED_MODEL_PATH file is never deleted. Heads are kept (they
// are small and re-verified on the next download).
func (a *App) DeleteEmbeddingModel() error {
	f := a.localModelFetcher()
	if f == nil {
		return fmt.Errorf("%s points at a custom model file; not deleting it", embedding.EmbedModelPathEnv)
	}
	if f.Running() {
		return fmt.Errorf("model download in progress")
	}
	a.detachLocalEmbedder()
	if err := f.Forget(); err != nil {
		return err
	}
	a.removeLegacyGemmaModel()
	embedding.ResetQwen3LoadBackoff()
	log.Printf("[embedding] local model deleted (%s)", f.Path())
	return nil
}

// detachLocalEmbedder unwires and closes the App's embedder so its file can
// be deleted (Windows refuses to delete a mapped file).
func (a *App) detachLocalEmbedder() {
	noop := embedding.NoopEmbedder{}
	if a.memoryStore != nil {
		a.memoryStore.SetEmbedder(noop)
	}
	if a.toolRouter != nil {
		a.toolRouter.SetEmbedder(noop)
	}
	if a.unifiedClassifier != nil {
		a.unifiedClassifier.SetEmbedder(noop)
	}
	if a.remoteSessions != nil && a.remoteSessions.hubClient != nil {
		if handler := a.remoteSessions.hubClient.currentIMHandler(); handler != nil && handler.toolBuilder != nil {
			handler.toolBuilder.SetEmbedder(noop)
		}
	}
	a.embeddingMu.Lock()
	if a.intentEmbedder != nil && !embedding.IsNoop(a.intentEmbedder) {
		a.localL3Runtime().Detach()
		a.intentEmbedder.Close()
	}
	a.intentEmbedder = nil
	a.intentEmbedderPath = ""
	a.embeddingMu.Unlock()
	a.intentEmbeddingActive.Store(false)
	a.embeddingActivated.Store(false)
}

// removeLegacyGemmaModel deletes the EmbeddingGemma-300M cache file (and its
// download side files) that earlier releases put in the models dir. Only
// that exact cache name is touched, and only once the Qwen3 model is the
// installed model, so users migrating from Gemma end up on Qwen3 without a
// stale 330 MB file.
func (a *App) removeLegacyGemmaModel() {
	dir := embedding.DefaultModelsDir()
	if dir == "" {
		return
	}
	base := filepath.Join(dir, embedding.LegacyGemmaModelFilename)
	for _, p := range []string{base, base + ".tmp", base + ".part", base + ".verified"} {
		if err := os.Remove(p); err == nil {
			log.Printf("[embedding] removed legacy EmbeddingGemma cache file %s", p)
		}
	}
}

func (a *App) verifyDownloadedEmbeddingModel(modelPath string) error {
	if !a.vectorSearchConfiguredEnabled() {
		return nil
	}
	if !a.verifyAndEnableEmbedding(modelPath) {
		return fmt.Errorf("embedding model downloaded but verification failed")
	}
	return nil
}

// downloadModelFrom downloads a file from url to destPath, emitting progress events.
// When emitErrors is false, errors are not sent to the frontend (used for silent fallback attempts).
// Supports HTTP Range resume: if a .tmp file already exists, it sends a Range header to continue.
func (a *App) downloadModelFrom(url, destPath string, emitErrors bool) error {
	return a.downloadModelFromWithEvent(url, destPath, emitErrors, "embedding-download-progress")
}

// downloadModelFromWithEvent is the same as downloadModelFrom but allows specifying
// a custom Wails event name for progress reporting.
func (a *App) downloadModelFromWithEvent(url, destPath string, emitErrors bool, eventName string) error {
	tmpPath := destPath + ".tmp"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	// Resume: check existing .tmp file size for Range request.
	var resumeOffset int64
	if fi, err := os.Stat(tmpPath); err == nil && fi.Size() > 0 {
		resumeOffset = fi.Size()
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", resumeOffset))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if emitErrors {
			a.emitDownloadProgressNamed(eventName, 0, 0, 0, err.Error())
		}
		return fmt.Errorf("download request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		msg := fmt.Sprintf("HTTP %d from %s", resp.StatusCode, url)
		if emitErrors {
			a.emitDownloadProgressNamed(eventName, 0, 0, 0, msg)
		}
		return fmt.Errorf("%s", msg)
	}

	// If server doesn't support Range (returned 200 instead of 206), start from scratch.
	if resp.StatusCode == http.StatusOK && resumeOffset > 0 {
		resumeOffset = 0
	}

	var totalSize int64
	if resp.StatusCode == http.StatusPartialContent {
		// Content-Range: bytes 12345-99999/100000
		if cr := resp.Header.Get("Content-Range"); cr != "" {
			// Parse total from "bytes start-end/total"
			if idx := strings.LastIndex(cr, "/"); idx >= 0 {
				totalSize, _ = strconv.ParseInt(cr[idx+1:], 10, 64)
			}
		}
		if totalSize == 0 {
			cl, _ := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
			totalSize = resumeOffset + cl
		}
	} else {
		totalSize, _ = strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	}

	var out *os.File
	if resumeOffset > 0 && resp.StatusCode == http.StatusPartialContent {
		out, err = os.OpenFile(tmpPath, os.O_WRONLY|os.O_APPEND, 0o644)
	} else {
		resumeOffset = 0
		out, err = os.Create(tmpPath)
	}
	if err != nil {
		return fmt.Errorf("open temp file: %w", err)
	}
	defer func() {
		out.Close()
		// Only clean up .tmp on non-resume errors; keep it for future resume.
		// The caller (backgroundPreloadEmbeddingModel) relies on .tmp surviving.
	}()

	buf := make([]byte, 64*1024)
	downloaded := resumeOffset
	lastEmit := time.Now()

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := out.Write(buf[:n]); wErr != nil {
				if emitErrors {
					a.emitDownloadProgressNamed(eventName, 0, downloaded, totalSize, wErr.Error())
				}
				return fmt.Errorf("write file: %w", wErr)
			}
			downloaded += int64(n)
			if time.Since(lastEmit) > 200*time.Millisecond {
				pct := 0
				if totalSize > 0 {
					pct = int(downloaded * 100 / totalSize)
				}
				a.emitDownloadProgressNamed(eventName, pct, downloaded, totalSize, "")
				lastEmit = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			if emitErrors {
				a.emitDownloadProgressNamed(eventName, 0, downloaded, totalSize, readErr.Error())
			}
			return fmt.Errorf("read body: %w", readErr)
		}
	}
	out.Close()

	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("rename: %w", err)
	}

	a.emitDownloadProgressNamed(eventName, 100, downloaded, totalSize, "")
	return nil
}

func (a *App) emitDownloadProgressNamed(eventName string, pct int, downloaded, total int64, errMsg string) {
	if a.ctx == nil {
		return
	}
	status := downloadProgressNamedStatus(pct, errMsg)
	a.emitEvent(eventName, map[string]interface{}{
		"percent":        pct,
		"downloaded":     downloaded,
		"total":          total,
		"error":          errMsg,
		"status":         status,
		"runtime_status": string(status.RuntimeStatusValue()),
	})
}

// backgroundPreloadEmbeddingModel silently downloads the embedding model in the
// background when vector search is enabled and the model file is missing.
// On success it verifies the model by loading it, then wires the embedder into
// the running app if vector search is still configured on.
// Supports resume: if a previous .tmp file exists, the download continues from
// where it left off (HTTP Range).
func (a *App) backgroundPreloadEmbeddingModel() {
	a.logMemorySnapshot("embeddingPreload:start")

	// Only run when vector search is configured on. Missing models are downloaded
	// first, then verified before the runtime embedder is activated.
	cfg, err := a.LoadConfig()
	if err != nil || !cfg.VectorSearchEnabled {
		return
	}

	if _, err := embeddingModelsDir(); err != nil {
		return
	}
	// Local model (Qwen3) first run: background, resumable, sha-verified
	// download + hot-load (app_model_fetch.go). Never blocks startup.
	if a.startLocalModelFetch() {
		return
	}
	// Custom MACLAW_EMBED_MODEL_PATH: load what is there (the factory
	// resolves it); never download over a user file.
	a.verifyAndEnableEmbedding(embedding.DefaultModelPath())
}

// verifyAndEnableEmbedding loads the model to verify integrity, then activates
// vector search runtime wiring if the config is still enabled. Returns true on
// success.
//
// The embedder is activated asynchronously so the AI assistant remains
// responsive while the vector index is being built.
func (a *App) verifyAndEnableEmbedding(modelPath string) bool {
	a.logMemorySnapshot("verifyAndEnableEmbedding:start")
	// Ensure infrastructure is ready before enabling.
	a.ensureRemoteInfra()

	// Smoke-tested load of the single model (Qwen3).
	// Failure degrades to rules + BM25; the factory backs off and retries.
	emb, err := a.sharedEmbeddingEmbedder(modelPath, loadAppEmbedder(true))
	if err != nil {
		fmt.Printf("[embedding] verification failed: %v\n", err)
		return false
	}

	fmt.Println("[embedding] model verified, enabling vector search asynchronously")

	if !a.vectorSearchConfiguredEnabled() {
		a.closeEmbedderIfNotShared(emb)
		return false
	}

	// Wire the embedder to memoryStore, toolRouter, and toolBuilder in the
	// background so the AI assistant is not blocked. The router will use
	// pure BM25 scoring until this goroutine finishes.
	go a.activateEmbedderAsync(emb)
	a.logMemorySnapshot("verifyAndEnableEmbedding:scheduled")

	return true
}

// activateEmbedderAsync wires an already-loaded embedder into the memory store,
// tool router, and tool builder in the background. After wiring, it pre-warms
// the tool embedding cache so the first user message that hits the hybrid
// retrieval path doesn't pay the cold-start cost.
func (a *App) activateEmbedderAsync(emb embedding.Embedder) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[embedding] activateEmbedderAsync panic: %v\n", r)
		}
	}()

	if emb == nil || embedding.IsNoop(emb) {
		return
	}

	if !a.vectorSearchConfiguredEnabled() {
		a.closeEmbedderIfNotShared(emb)
		return
	}

	// Ensure only one goroutine performs activation. Multiple call sites
	// (ensureMemoryStore, SetVectorSearchEnabled, verifyAndEnableEmbedding)
	// may race; the first one wins, subsequent calls close the redundant embedder.
	if !a.embeddingActivated.CompareAndSwap(false, true) {
		a.closeEmbedderIfNotShared(emb)
		return
	}
	emb, reusedIntentEmbedder := a.claimEmbeddingForFullActivation(emb)

	a.logMemorySnapshot("activateEmbedderAsync:start")
	t0 := time.Now()

	// Wire embedder into memory store (triggers backfillEmbeddings in background).
	if a.memoryStore != nil {
		a.memoryStore.SetEmbedder(emb)
		a.refreshMemoryEvolutionLLM()
	}

	// Wire embedder into knowledge auto-recall store (enables semantic hybrid Search).
	// openKnowledgeStore also attaches via attachKnowledgeEmbedder for stores created later.
	knowledgeAutoRecallStoreMu.Lock()
	if knowledgeAutoRecallStore != nil {
		knowledgeAutoRecallStore.UseEmbedder(emb)
	}
	knowledgeAutoRecallStoreMu.Unlock()
	a.ensureKnowledgeEmbeddingRefresh(emb)

	// Wire embedder into tool router (enables hybrid retrieval).
	if a.toolRouter != nil {
		a.toolRouter.SetEmbedder(emb)
	}

	// Create and wire IntentClassifier (uses the same embedder for Layer 2).
	if a.toolRouter != nil {
		ic := tool.NewIntentClassifier(emb)
		// Wire Layer 3 LLM callback using the app's LLM config.
		// Legacy tool-router intent classifier: its remote LLM fallback is
		// dropped only in MACLAW_INTENT_LOCAL_L3=strict (see app_local_intent.go).
		if a.localL3Runtime().Mode() != intent.LocalL3Strict {
			ic.SetLLMFunc(a.buildIntentLLMFunc())
		}
		a.toolRouter.SetIntentClassifier(ic)
		log.Println("[embedding] IntentClassifier created and wired to tool router")
	}

	// Upgrade existing UIC with real embedder (enables Layer 2 + Layer 3).
	// initEarlyClassifier created the UIC in conservative mode. Now we wire
	// in the real embedder and LLM, upgrading it in-place.
	// All consumers (including GIC, which delegates to UIC) already hold
	// a reference to this UIC instance — no re-wiring needed.
	//
	// Guarantee: classifierOnce.Do ensures the UIC exists before we reach here,
	// even if this goroutine races ahead of the main startup sequence.
	a.initEarlyClassifier() // idempotent via sync.Once
	a.attachLocalL3(emb)
	a.attachToolHead(emb)
	a.startHeadAssetFetch()
	if !reusedIntentEmbedder {
		a.unifiedClassifier.SetEmbedder(emb)
	}
	a.unifiedClassifier.SetLLMFunc(a.intentL3Func())
	a.unifiedClassifier.SetLLMContextFunc(a.intentL3ContextFunc())
	a.unifiedClassifier.SetLocalHeadProbe(a.localL3Runtime().ProbeFunc())
	// Ensure toolRouter has the UIC reference. initEarlyClassifier may have
	// skipped this wiring if toolRouter was nil at that time (e.g., startup
	// without Hub credentials where ensureRemoteInfra runs later).
	if a.toolRouter != nil {
		a.toolRouter.SetUnifiedClassifier(a.unifiedClassifier)
	}
	log.Println("[embedding] UnifiedIntentClassifier upgraded: L2 embedding + L3 LLM now available")

	// Wire the shared runtime to every current IM handler. The desktop handler
	// and Hub handler are created on different startup paths, so wiring only one
	// of them leaves active conversations silently without relevance scoring.
	a.wireEmbedderToIMHandlers(emb, true)

	// Pre-warm tool embedding cache by running a dummy route. This triggers
	// FuseScores 鈫?GetBatch which synchronously computes and caches embeddings
	// for all candidate tools, so the first real user message is fast.
	var handler *IMMessageHandler
	if a.imHandler != nil {
		handler = a.imHandler
	} else if a.remoteSessions != nil && a.remoteSessions.hubClient != nil {
		handler = a.remoteSessions.hubClient.currentIMHandler()
	}
	if handler != nil {
		handler.WarmupTools()
		// Precompute frozen static memory section off the first-message path.
		// StaticMemorySectionForPrompt can be multi-second on large user_fact stores.
		go handler.WarmFrozenMemorySnapshot(desktopUserID)
	}

	a.logMemorySnapshot("activateEmbedderAsync:done")
	fmt.Printf("[embedding] async activation complete in %v\n", time.Since(t0))
}

func (a *App) claimEmbeddingForFullActivation(emb embedding.Embedder) (embedding.Embedder, bool) {
	a.embeddingMu.Lock()
	defer a.embeddingMu.Unlock()
	if a.intentEmbedder != nil && !embedding.IsNoop(a.intentEmbedder) {
		intentWasActive := a.intentEmbeddingActive.Load()
		if emb != nil && !sameEmbedder(a.intentEmbedder, emb) {
			emb.Close()
		}
		a.intentEmbeddingActive.Store(true)
		return a.intentEmbedder, intentWasActive
	}
	a.intentEmbedder = emb
	a.intentEmbeddingActive.Store(true)
	return emb, false
}

// activateIntentClassifierEmbedderAsync wires a local embedder only into the
// intent classifiers. This keeps short-message routing fast even when the user
// has disabled memory/tool vector search.
func (a *App) activateIntentClassifierEmbedderAsync(emb embedding.Embedder) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[embedding] activateIntentClassifierEmbedderAsync panic: %v\n", r)
		}
	}()
	if emb == nil || embedding.IsNoop(emb) {
		return
	}
	if a.embeddingActivated.Load() {
		a.closeEmbedderIfNotShared(emb)
		return
	}
	a.embeddingMu.Lock()
	if a.intentEmbeddingActive.Load() {
		a.embeddingMu.Unlock()
		a.closeEmbedderIfNotShared(emb)
		return
	}
	if a.intentEmbedder != nil && !embedding.IsNoop(a.intentEmbedder) {
		if !sameEmbedder(a.intentEmbedder, emb) {
			emb.Close()
		}
		emb = a.intentEmbedder
	} else {
		a.intentEmbedder = emb
	}
	a.intentEmbeddingActive.Store(true)
	a.embeddingMu.Unlock()
	t0 := time.Now()
	a.initEarlyClassifier()
	a.attachLocalL3(emb)
	a.attachToolHead(emb)
	a.startHeadAssetFetch()
	a.unifiedClassifier.SetEmbedder(emb)
	a.unifiedClassifier.SetLLMFunc(a.intentL3Func())
	a.unifiedClassifier.SetLLMContextFunc(a.intentL3ContextFunc())
	// Intent-only activation is also useful for interruption relevance. It is
	// deliberately available when vector search is off, so this must not wait for
	// activateEmbedderAsync (which correctly exits in that configuration).
	a.wireEmbedderToIMHandlers(emb, false)
	if a.toolRouter != nil {
		a.toolRouter.SetUnifiedClassifier(a.unifiedClassifier)
	}
	log.Println("[embedding] UnifiedIntentClassifier upgraded: L2 embedding + L3 LLM available for intent routing")
	fmt.Printf("[embedding] intent-only activation complete in %v\n", time.Since(t0))
}

// sharedEmbeddingEmbedder serializes model loading and publishes one shared
// embedding runtime per App. This prevents concurrent startup/settings/download
// paths from loading multiple model instances and spiking memory.
func (a *App) sharedEmbeddingEmbedder(modelPath string, load func(string) (embedding.Embedder, error)) (embedding.Embedder, error) {
	if a == nil {
		return embedding.NoopEmbedder{}, nil
	}
	a.embeddingMu.Lock()
	defer a.embeddingMu.Unlock()
	if a.intentEmbedder != nil && !embedding.IsNoop(a.intentEmbedder) {
		if strings.TrimSpace(modelPath) == "" || modelPath == a.intentEmbedderPath {
			return a.intentEmbedder, nil
		}
		log.Printf("[embedding] switching embedder %s -> %s (closing previous instance)", embedding.EmbedderFamily(a.intentEmbedder), modelPath)
		a.localL3Runtime().Detach()
		a.intentEmbedder.Close()
		a.intentEmbedder = nil
		a.intentEmbedderPath = ""
		a.intentEmbeddingActive.Store(false)
		a.embeddingActivated.Store(false)
	}
	if cfg, err := a.LoadConfig(); err == nil {
		embedding.SetHWAccelPreferred(cfg.EmbedHWAccelEnabled())
	}
	emb, err := load(modelPath)
	if err != nil {
		return nil, err
	}
	if emb == nil || embedding.IsNoop(emb) {
		return emb, nil
	}
	if g, ok := emb.(embedding.AccelEmbedder); ok {
		g.ApplyAccel(embedding.HWAccelPreferred())
	}
	a.intentEmbedder = emb
	a.intentEmbedderPath = modelPath
	return emb, nil
}

// GetEmbedAccelInfo prefers the live GUI embedder instance pin (About badge SoT).
func (a *App) GetEmbedAccelInfo() embedding.AccelInfo {
	if a == nil {
		return embedding.CurrentAccelInfo()
	}
	a.embeddingMu.Lock()
	defer a.embeddingMu.Unlock()
	// Interface, not a concrete model type, so the badge reports the live
	// Qwen3 instance rather than the global NPU detection.
	if g, ok := a.intentEmbedder.(embedding.AccelEmbedder); ok && g != nil && !embedding.IsNoop(a.intentEmbedder) {
		return g.Accel()
	}
	info := embedding.CurrentAccelInfo()
	if info.Backend == "" {
		info.Backend = embedding.BackendCPUSIMD
	}
	return info
}

// SetEmbedHWAccel persists prefer-NPU and retargets the live GUI instance.
func (a *App) SetEmbedHWAccel(enabled bool) (corelib.AppConfig, error) {
	embedding.SetHWAccelPreferred(enabled)
	if a != nil {
		a.embeddingMu.Lock()
		if g, ok := a.intentEmbedder.(embedding.AccelEmbedder); ok && g != nil && !embedding.IsNoop(a.intentEmbedder) {
			g.ApplyAccel(enabled)
		}
		a.embeddingMu.Unlock()
	}
	embedding.ReloadSharedEmbedderAccel()
	return a.PatchConfigFields(map[string]interface{}{"embed_hw_accel": enabled})
}

func (a *App) closeEmbedderIfNotShared(emb embedding.Embedder) {
	if emb == nil || embedding.IsNoop(emb) {
		return
	}
	if a != nil {
		a.embeddingMu.Lock()
		shared := sameEmbedder(a.intentEmbedder, emb)
		a.embeddingMu.Unlock()
		if shared {
			return
		}
	}
	emb.Close()
}

func sameEmbedder(left, right embedding.Embedder) (same bool) {
	if left == nil || right == nil {
		return left == right
	}
	defer func() {
		if recover() != nil {
			same = false
		}
	}()
	return left == right
}

// activeInterruptEmbedder returns the single local embedding runtime shared by
// intent and vector-search features. It is intentionally separate from the
// vector-search enabled flag: interruption relevance remains valuable even
// when users turn off memory/tool vector retrieval.
func (a *App) activeInterruptEmbedder() embedding.Embedder {
	if a == nil {
		return nil
	}
	a.embeddingMu.Lock()
	defer a.embeddingMu.Unlock()
	if a.intentEmbedder == nil || embedding.IsNoop(a.intentEmbedder) {
		return nil
	}
	return a.intentEmbedder
}

// wireEmbedderToIMHandlers keeps every IM entry point on the same semantic
// relevance runtime. Tool routing only receives this runtime when vector search
// is enabled; interruption relevance stays available independently.
func (a *App) wireEmbedderToIMHandlers(emb embedding.Embedder, wireTools bool) {
	if a == nil || emb == nil || embedding.IsNoop(emb) {
		return
	}
	wire := func(handler *IMMessageHandler) {
		if handler == nil {
			return
		}
		if wireTools && handler.toolBuilder != nil {
			handler.toolBuilder.SetEmbedder(emb)
		}
		if handler.interruptHandler != nil {
			handler.interruptHandler.SetEmbedder(emb)
		}
	}
	wire(a.imHandler)
	if a.remoteSessions != nil && a.remoteSessions.hubClient != nil {
		wire(a.remoteSessions.hubClient.currentIMHandler())
		for _, handler := range a.remoteSessions.hubClient.hardwareAgentHandlers() {
			wire(handler)
		}
	}
	// Local gateway handlers own their own per-user loop locks and must receive
	// the same relevance runtime. Without this, only desktop/Hub messages get
	// embeddings while WeChat/Telegram/QQ/etc. silently fall back to broad
	// domain matching.
	if a.weixinGateway != nil {
		wire(a.weixinGateway.currentLocalHandler())
	}
	if a.telegramGateway != nil {
		wire(a.telegramGateway.currentLocalHandler())
	}
	if a.messengerGateway != nil {
		wire(a.messengerGateway.currentLocalHandler())
	}
	if a.qqBotGateway != nil {
		wire(a.qqBotGateway.currentLocalHandler())
	}
	if a.lansengerGateways != nil && !a.lansengerGateways.isEmpty() {
		for _, handler := range a.lansengerGateways.handlers() {
			wire(handler)
		}
	} else if a.lansengerGateway != nil {
		wire(a.lansengerGateway.currentLocalHandler())
	}
	if a.thirdPartyGateway != nil {
		wire(a.thirdPartyGateway.currentLocalHandler())
		for _, handler := range a.thirdPartyGateway.localHardwareHandlers() {
			wire(handler)
		}
	}
}

// buildIntentLLMFunc creates a LLMClassifyFunc callback that uses the app's
// current LLM config to make a lightweight classification request.
func (a *App) buildIntentLLMFunc() tool.LLMClassifyFunc {
	return func(prompt string) (string, error) {
		cfg := a.GetMaclawLLMConfig()
		if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
			return "", fmt.Errorf("LLM not configured")
		}
		if reason, skip := a.shouldSkipLightweightLLM(cfg, llmEndpointCategoryLightweightClassify); skip {
			return "", fmt.Errorf("intent-classifier LLM endpoint temporarily unavailable after recent network failure: %s", reason)
		}
		messages := []interface{}{
			map[string]string{"role": "user", "content": prompt},
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		ctx = llm.WithRequestTrace(ctx, llm.RequestTrace{Caller: "intent-classifier"})
		resp, err := doSimpleLLMRequest(ctx, cfg, messages, nil, 30*time.Second)
		a.observeLLMEndpointResult(cfg, llmEndpointCategoryLightweightClassify, ctx.Err() != nil || isLLMBudgetFiredError(err), err)
		if err != nil {
			return "", err
		}
		return resp.Content, nil
	}
}

// buildUICLLMFunc creates an intent.LLMClassifyFunc callback that uses the
// app's current LLM config to make a classification request with system + user
// messages. Used by the UnifiedIntentClassifier's Layer 3.
//
// Timeout: 30s for the HTTP/LLM call itself (tree-only Classify can still use
// DefaultLLMTimeout). Dual-channel fusion waits cfg.FusionTreeDeadline before
// degrading to embedding-only — the desktop app sets it to 30s so a slow hub
// degrades the ANSWER's latency instead of the answer's correctness
// (product principle 2026-09-18: 宁慢勿乱 — a degraded guess surfaces missing
// tools and rescue chains; a waited-on verdict surfaces the right plan).
func (a *App) buildUICLLMFunc() intent.LLMClassifyFunc {
	return func(systemPrompt, userText string) (string, error) {
		cfg := a.GetMaclawLLMConfig()
		if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
			return "", fmt.Errorf("LLM not configured")
		}
		if reason, skip := a.shouldSkipLightweightLLM(cfg, llmEndpointCategoryUICTree); skip {
			return "", fmt.Errorf("unified-intent-classifier LLM endpoint temporarily unavailable after recent network failure: %s", reason)
		}
		messages := []interface{}{
			map[string]string{"role": "system", "content": systemPrompt},
			map[string]string{"role": "user", "content": userText},
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		ctx = llm.WithRequestTrace(ctx, llm.RequestTrace{Caller: "unified-intent-classifier"})
		resp, err := doSimpleLLMRequestWithOptions(ctx, attachLightweightHubHint(cfg, llm.TaskIntent), messages, nil, 30*time.Second, simpleLLMRequestOptions{
			ResponseFormat:         intentTreeResponseFormat(),
			PreserveResponseFormat: true,
		})
		a.observeLLMEndpointResult(cfg, llmEndpointCategoryUICTree, ctx.Err() != nil || isLLMBudgetFiredError(err), err)
		if err != nil {
			return "", err
		}
		return resp.Content, nil
	}
}

// buildUICLLMContextFunc is the production L3 callback. The classification
// budget rides on ctx (fusion deadline or tree-only timeout); parentCtx is
// the caller's user/turn context. The request is detachable (P0-3): when the
// budget fires, the helper returns a budget error but keeps the connection
// alive in the background, deriving the detached read from parentCtx — a
// user cancellation still aborts the read, and a late-verdict retry with the
// same payload adopts the result instead of double-sending. A detached read
// that completes successfully feeds the endpoint gate a positive health
// signal for the uic-tree category.
func (a *App) buildUICLLMContextFunc() intent.LLMClassifyContextFunc {
	return func(ctx context.Context, parentCtx context.Context, systemPrompt, userText string) (string, error) {
		cfg := a.GetMaclawLLMConfig()
		if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
			return "", fmt.Errorf("LLM not configured")
		}
		if reason, skip := a.shouldSkipLightweightLLM(cfg, llmEndpointCategoryUICTree); skip {
			return "", fmt.Errorf("unified-intent-classifier LLM endpoint temporarily unavailable after recent network failure: %s", reason)
		}
		if parentCtx == nil {
			parentCtx = context.Background()
		}
		messages := []interface{}{
			map[string]string{"role": "system", "content": systemPrompt},
			map[string]string{"role": "user", "content": userText},
		}
		caller := "unified-intent-classifier"
		if intent.LateTree(ctx) {
			caller = "unified-intent-classifier-late"
		}
		lateTree := intent.LateTree(ctx)
		ctx = llm.WithRequestTrace(ctx, llm.RequestTrace{Caller: caller})
		resp, err := doSimpleLLMRequestWithOptions(ctx, attachLightweightHubHint(cfg, llm.TaskIntent), messages, nil, 30*time.Second, simpleLLMRequestOptions{
			ResponseFormat:          intentTreeResponseFormat(),
			PreserveResponseFormat:  true,
			DetachParentCtx:         parentCtx,
			AdoptInFlightPastCancel: lateTree,
			OnDetachedComplete: func(detachErr error) {
				if detachErr == nil {
					a.observeLLMEndpointLightweightSuccess(cfg, llmEndpointCategoryUICTree)
				}
			},
		})
		// A failure raised by our own budget (fusion deadline, request budget,
		// or the detach handoff — i.e. ctx fires first or the helper reports a
		// budget-fired error) says nothing about endpoint health. Reporting it
		// to the endpoint gate poisoned the next 30s of classifications: one
		// slow tree call made every following ambiguous turn skip the tree and
		// degrade to unknown (2026-08-25 production chain). Successes and
		// errors from a still-live context remain endpoint evidence.
		a.observeLLMEndpointResult(cfg, llmEndpointCategoryUICTree, ctx.Err() != nil || isLLMBudgetFiredError(err), err)
		if err != nil {
			return "", err
		}
		return resp.Content, nil
	}
}

// intentTreeResponseFormat is a transport-level contract for Layer 3.  The
// prompt remains useful to providers without structured-output support, but
// OpenAI-compatible providers must receive a verifiable JSON schema instead
// of treating classification as ordinary assistant chat.
func intentTreeResponseFormat() map[string]interface{} {
	return intent.TreeResponseFormat()
}
