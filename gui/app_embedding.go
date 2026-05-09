package main

import (
	"context"
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const embeddingModelFilename = "embeddinggemma-300M-Q8_0.gguf"

// embeddingModelDefaultURL is the primary download source (GitHub Releases).
const embeddingModelDefaultURL = "https://github.com/RapidAI/MaClaw/releases/download/Model_Release/embeddinggemma-300M-Q8_0.gguf"

// embeddingDownloadMu prevents concurrent model downloads.
var embeddingDownloadMu sync.Mutex

// initEarlyClassifier creates the shared classifier instances during app
// startup. Until embeddings or LLM routing are wired in, they return
// conservative unknown/ambiguous results instead of using keyword fallbacks.
//
// L2 (embedding) and L3 (LLM) are wired in later by activateEmbedderAsync
// via SetEmbedder/SetLLMFunc on the same instance. The UIC's Classify()
// method automatically uses whatever layers are available at call time:
//   - Before semantic classifiers are ready: conservative unknown
//   - After embedding loads: embedding classification
//   - After LLM is wired: embedding + LLM fusion
func (a *App) initEarlyClassifier() {
	// Create UIC with noop embedder; no local keyword fallback is enabled.
	uic := intent.New(intent.Config{
		Embedder:   embedding.NoopEmbedder{},
		LLMTimeout: 15 * time.Second,
	})
	a.unifiedClassifier = uic

	// Create GIC with nil embedder; it delegates to UIC or LLM when available.
	gic := NewGateIntentClassifier(nil)
	gic.SetLLMConfig(func() corelib.MaclawLLMConfig { return a.GetMaclawLLMConfig() }, &http.Client{Timeout: 5 * time.Second})
	gic.SetUnifiedClassifier(uic)
	a.gateIntentClassifier = gic

	// Wire to all consumers so they see the classifier immediately.
	if a.toolRouter != nil {
		a.toolRouter.SetUnifiedClassifier(uic)
	}
	if a.capabilityGapDetector != nil {
		a.capabilityGapDetector.SetUnifiedClassifier(uic)
	}
	setUnifiedClassifierForIM(uic)

	log.Println("[classifier] early init complete: semantic classifiers pending async wiring")
}

// embeddingModelsDir returns ~/.maclaw/models, creating it if needed.
func embeddingModelsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".maclaw", "models")
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
		p := filepath.Join(dir, embeddingModelFilename)
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
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	cfg.VectorSearchEnabled = enabled
	if err := a.SaveConfig(cfg); err != nil {
		return err
	}

	if enabled {
		modelPath := embedding.DefaultModelPath()
		emb := embedding.NewDefaultEmbedder(modelPath)
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
			if handler := a.remoteSessions.hubClient.imHandler; handler != nil && handler.toolBuilder != nil {
				handler.toolBuilder.SetEmbedder(noop)
			}
		}
		// Do NOT clear a.gateIntentClassifier or a.unifiedClassifier.
		// initEarlyClassifier created conservative classifier instances that
		// fail closed until semantic channels are available. Clearing them
		// would leave the Coding Tool Gate with no classifier at all.
	}
	return nil
}

// CheckEmbeddingModel checks if the embedding model file exists locally.
// Returns: { "exists": bool, "path": string, "size": int64 }
func (a *App) CheckEmbeddingModel() map[string]interface{} {
	dir, err := embeddingModelsDir()
	if err != nil {
		return map[string]interface{}{"exists": false, "path": "", "size": int64(0)}
	}
	p := filepath.Join(dir, embeddingModelFilename)
	fi, err := os.Stat(p)
	if err != nil {
		return map[string]interface{}{"exists": false, "path": p, "size": int64(0)}
	}
	return map[string]interface{}{"exists": true, "path": p, "size": fi.Size()}
}

// DownloadEmbeddingModel downloads the embedding model.
// It first tries the default GitHub Releases URL; on failure it falls back
// to the user-configured Hub URL.
// Progress is emitted via Wails event "embedding-download-progress" with payload:
//
//	{ "percent": int, "downloaded": int64, "total": int64, "error": string }
//
// This method blocks until download completes or fails.
func (a *App) DownloadEmbeddingModel() error {
	// Prevent concurrent downloads 鈥?second caller is silently ignored.
	if !embeddingDownloadMu.TryLock() {
		return nil
	}
	defer embeddingDownloadMu.Unlock()

	dir, err := embeddingModelsDir()
	if err != nil {
		return fmt.Errorf("create models dir: %w", err)
	}
	destPath := filepath.Join(dir, embeddingModelFilename)

	// 1) Try default GitHub URL first (silent 鈥?don't emit errors to UI).
	if err := a.downloadModelFrom(embeddingModelDefaultURL, destPath, false); err == nil {
		return a.verifyDownloadedEmbeddingModel(destPath)
	}

	// 2) Fallback: Hub URL (emit progress & errors to UI).
	cfg, err := a.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	hubURL := strings.TrimRight(cfg.RemoteHubURL, "/")
	if hubURL == "" {
		return fmt.Errorf("default download URL is unavailable and Hub URL is not configured")
	}
	fallbackURL := hubURL + "/api/v1/models/" + embeddingModelFilename
	if err := a.downloadModelFrom(fallbackURL, destPath, true); err != nil {
		return err
	}
	return a.verifyDownloadedEmbeddingModel(destPath)
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
	runtime.EventsEmit(a.ctx, eventName, map[string]interface{}{
		"percent":    pct,
		"downloaded": downloaded,
		"total":      total,
		"error":      errMsg,
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

	dir, err := embeddingModelsDir()
	if err != nil {
		return
	}
	destPath := filepath.Join(dir, embeddingModelFilename)
	if _, err := os.Stat(destPath); err == nil {
		// Model file already exists; verify and wire it into the runtime.
		if a.verifyAndEnableEmbedding(destPath) {
			return
		}
		// Verification failed 鈥?file is corrupt, remove and re-download.
		if !a.vectorSearchConfiguredEnabled() {
			return
		}
		os.Remove(destPath)
	}

	// Acquire download lock; skip if another download is in progress.
	if !embeddingDownloadMu.TryLock() {
		return
	}
	defer embeddingDownloadMu.Unlock()

	fmt.Println("[embedding] background preload: starting silent download")

	// Try default GitHub URL first.
	if err := a.downloadModelFrom(embeddingModelDefaultURL, destPath, false); err != nil {
		// Fallback: Hub URL.
		hubURL := strings.TrimRight(cfg.RemoteHubURL, "/")
		if hubURL == "" {
			fmt.Printf("[embedding] background preload: all sources failed: %v\n", err)
			return
		}
		fallbackURL := hubURL + "/api/v1/models/" + embeddingModelFilename
		if err := a.downloadModelFrom(fallbackURL, destPath, false); err != nil {
			fmt.Printf("[embedding] background preload: fallback failed: %v\n", err)
			return
		}
	}

	// Verify and activate unless the user disabled vector search while the
	// download was running.
	if !a.vectorSearchConfiguredEnabled() {
		return
	}
	a.verifyAndEnableEmbedding(destPath)
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

	emb, err := embedding.NewGemmaEmbedder(modelPath, 256)
	if err != nil {
		fmt.Printf("[embedding] verification failed: %v\n", err)
		return false
	}

	// Quick smoke test: embed a short string.
	vec, err := emb.Embed("test")
	if err != nil || len(vec) == 0 {
		fmt.Printf("[embedding] smoke test failed: err=%v len=%d\n", err, len(vec))
		emb.Close()
		return false
	}

	fmt.Println("[embedding] model verified, enabling vector search asynchronously")

	if !a.vectorSearchConfiguredEnabled() {
		emb.Close()
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

	if !a.vectorSearchConfiguredEnabled() {
		emb.Close()
		return
	}

	a.logMemorySnapshot("activateEmbedderAsync:start")
	t0 := time.Now()

	// Wire embedder into memory store (triggers backfillEmbeddings in background).
	if a.memoryStore != nil {
		a.memoryStore.SetEmbedder(emb)
		a.refreshMemoryEvolutionLLM()
	}

	// Wire embedder into tool router (enables hybrid retrieval).
	if a.toolRouter != nil {
		a.toolRouter.SetEmbedder(emb)
	}

	// Create and wire IntentClassifier (uses the same embedder for Layer 2).
	if a.toolRouter != nil {
		ic := tool.NewIntentClassifier(emb)
		// Wire Layer 3 LLM callback using the app's LLM config.
		ic.SetLLMFunc(a.buildIntentLLMFunc())
		a.toolRouter.SetIntentClassifier(ic)
		log.Println("[embedding] IntentClassifier created and wired to tool router")
	}

	// Upgrade existing UIC with real embedder (enables Layer 2 + Layer 3).
	// initEarlyClassifier created the UIC in conservative mode. Now we wire
	// in the real embedder and LLM, upgrading it in-place.
	// All consumers (including GIC, which delegates to UIC) already hold
	// a reference to this UIC instance 鈥?no re-wiring needed.
	if a.unifiedClassifier != nil {
		a.unifiedClassifier.SetEmbedder(emb)
		a.unifiedClassifier.SetLLMFunc(a.buildUICLLMFunc())
		log.Println("[embedding] UnifiedIntentClassifier upgraded: L2 embedding + L3 LLM now available")
	} else {
		// Fallback: initEarlyClassifier didn't run (shouldn't happen in production).
		uic := intent.New(intent.Config{
			Embedder:   emb,
			LLMTimeout: 15 * time.Second,
		})
		uic.SetLLMFunc(a.buildUICLLMFunc())
		a.unifiedClassifier = uic
		setUnifiedClassifierForIM(uic)
		if a.gateIntentClassifier != nil {
			a.gateIntentClassifier.SetUnifiedClassifier(uic)
		}
		if a.toolRouter != nil {
			a.toolRouter.SetUnifiedClassifier(uic)
		}
		if a.capabilityGapDetector != nil {
			a.capabilityGapDetector.SetUnifiedClassifier(uic)
		}
		log.Println("[embedding] UnifiedIntentClassifier created fresh (early init missed)")
	}

	// Wire embedder into tool builder.
	if a.remoteSessions != nil && a.remoteSessions.hubClient != nil {
		if handler := a.remoteSessions.hubClient.imHandler; handler != nil && handler.toolBuilder != nil {
			handler.toolBuilder.SetEmbedder(emb)
		}
		// Wire embedder into interrupt handler for semantic relevance scoring.
		if handler := a.remoteSessions.hubClient.imHandler; handler != nil && handler.interruptHandler != nil {
			handler.interruptHandler.SetEmbedder(emb)
			log.Println("[embedding] interrupt handler embedder wired")
		}
	}

	// Pre-warm tool embedding cache by running a dummy route. This triggers
	// FuseScores 鈫?GetBatch which synchronously computes and caches embeddings
	// for all candidate tools, so the first real user message is fast.
	var handler *IMMessageHandler
	if a.remoteSessions != nil && a.remoteSessions.hubClient != nil {
		handler = a.remoteSessions.hubClient.imHandler
	}
	if handler != nil {
		handler.WarmupTools()
	}

	a.logMemorySnapshot("activateEmbedderAsync:done")
	fmt.Printf("[embedding] async activation complete in %v\n", time.Since(t0))
}

// buildIntentLLMFunc creates a LLMClassifyFunc callback that uses the app's
// current LLM config to make a lightweight classification request.
func (a *App) buildIntentLLMFunc() tool.LLMClassifyFunc {
	return func(prompt string) (string, error) {
		cfg := a.GetMaclawLLMConfig()
		if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
			return "", fmt.Errorf("LLM not configured")
		}
		messages := []interface{}{
			map[string]string{"role": "user", "content": prompt},
		}
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := doSimpleLLMRequest(context.Background(), cfg, messages, client, 5*time.Second)
		if err != nil {
			return "", err
		}
		return resp.Content, nil
	}
}

// buildUICLLMFunc creates an intent.LLMClassifyFunc callback that uses the
// app's current LLM config to make a classification request with system + user
// messages. Used by the UnifiedIntentClassifier's Layer 3.
func (a *App) buildUICLLMFunc() intent.LLMClassifyFunc {
	return func(systemPrompt, userText string) (string, error) {
		cfg := a.GetMaclawLLMConfig()
		if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
			return "", fmt.Errorf("LLM not configured")
		}
		messages := []interface{}{
			map[string]string{"role": "system", "content": systemPrompt},
			map[string]string{"role": "user", "content": userText},
		}
		// Use 15s timeout to match the UIC LLMTimeout. Third-party API
		// providers (e.g., api.rapidai.tech proxy) typically respond in
		// 8-15s. The previous 8s timeout caused L3 tree channel to fail
		// on nearly every call, leaving WorkflowType empty and preventing
		// workflow startup.
		client := &http.Client{Timeout: 20 * time.Second}
		resp, err := doSimpleLLMRequest(context.Background(), cfg, messages, client, 15*time.Second)
		if err != nil {
			return "", err
		}
		return resp.Content, nil
	}
}
