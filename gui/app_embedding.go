package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const embeddingModelFilename = "embeddinggemma-300M-Q8_0.gguf"

// embeddingModelDefaultURL is the primary download source (GitHub Releases).
const embeddingModelDefaultURL = "https://github.com/RapidAI/MaClaw/releases/download/Model_Release/embeddinggemma-300M-Q8_0.gguf"

// embeddingDownloadMu prevents concurrent model downloads.
var embeddingDownloadMu sync.Mutex

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
	Enabled      bool   `json:"enabled"`       // config toggle
	ModelExists  bool   `json:"model_exists"`  // GGUF file on disk
	ModelPath    string `json:"model_path"`    // full path to model file
	ModelSize    int64  `json:"model_size"`    // file size in bytes
	EmbedderOK   bool   `json:"embedder_ok"`   // embedder loaded and functional
	EmbedderDim  int    `json:"embedder_dim"`  // embedding dimension (0 if not loaded)
	EntryCount   int    `json:"entry_count"`   // total memory entries
	EmbeddedCount int   `json:"embedded_count"` // entries with embeddings
	HybridToolRetrievalActive bool `json:"hybrid_tool_retrieval_active"` // hybrid tool retrieval enabled
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
func (a *App) SetVectorSearchEnabled(enabled bool) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	cfg.VectorSearchEnabled = enabled
	if err := a.SaveConfig(cfg); err != nil {
		return err
	}

	// Re-wire embedder on the memory store, tool router, and tool builder.
	if enabled {
		modelPath := embedding.DefaultModelPath()
		emb := embedding.NewDefaultEmbedder(modelPath)
		if a.memoryStore != nil {
			a.memoryStore.SetEmbedder(emb)
		}
		if a.toolRouter != nil {
			a.toolRouter.SetEmbedder(emb)
		}
		if a.remoteSessions != nil && a.remoteSessions.hubClient != nil &&
			a.remoteSessions.hubClient.imHandler != nil &&
			a.remoteSessions.hubClient.imHandler.toolBuilder != nil {
			a.remoteSessions.hubClient.imHandler.toolBuilder.SetEmbedder(emb)
		}
	} else {
		noop := embedding.NoopEmbedder{}
		if a.memoryStore != nil {
			a.memoryStore.SetEmbedder(noop)
		}
		if a.toolRouter != nil {
			a.toolRouter.SetEmbedder(noop)
		}
		if a.remoteSessions != nil && a.remoteSessions.hubClient != nil &&
			a.remoteSessions.hubClient.imHandler != nil &&
			a.remoteSessions.hubClient.imHandler.toolBuilder != nil {
			a.remoteSessions.hubClient.imHandler.toolBuilder.SetEmbedder(noop)
		}
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
	// Prevent concurrent downloads — second caller is silently ignored.
	if !embeddingDownloadMu.TryLock() {
		return nil
	}
	defer embeddingDownloadMu.Unlock()

	dir, err := embeddingModelsDir()
	if err != nil {
		return fmt.Errorf("create models dir: %w", err)
	}
	destPath := filepath.Join(dir, embeddingModelFilename)

	// 1) Try default GitHub URL first (silent — don't emit errors to UI).
	if err := a.downloadModelFrom(embeddingModelDefaultURL, destPath, false); err == nil {
		return nil
	}

	// 2) Fallback: Hub URL (emit progress & errors to UI).
	cfg, err := a.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	hubURL := strings.TrimRight(cfg.RemoteHubURL, "/")
	if hubURL == "" {
		return fmt.Errorf("默认下载地址不可用，且 Hub URL 未配置")
	}
	fallbackURL := hubURL + "/api/v1/models/" + embeddingModelFilename
	return a.downloadModelFrom(fallbackURL, destPath, true)
}

// downloadModelFrom downloads a file from url to destPath, emitting progress events.
// When emitErrors is false, errors are not sent to the frontend (used for silent fallback attempts).
// Supports HTTP Range resume: if a .tmp file already exists, it sends a Range header to continue.
func (a *App) downloadModelFrom(url, destPath string, emitErrors bool) error {
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
			a.emitDownloadProgress(0, 0, 0, err.Error())
		}
		return fmt.Errorf("download request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		msg := fmt.Sprintf("HTTP %d from %s", resp.StatusCode, url)
		if emitErrors {
			a.emitDownloadProgress(0, 0, 0, msg)
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
					a.emitDownloadProgress(0, downloaded, totalSize, wErr.Error())
				}
				return fmt.Errorf("write file: %w", wErr)
			}
			downloaded += int64(n)
			if time.Since(lastEmit) > 200*time.Millisecond {
				pct := 0
				if totalSize > 0 {
					pct = int(downloaded * 100 / totalSize)
				}
				a.emitDownloadProgress(pct, downloaded, totalSize, "")
				lastEmit = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			if emitErrors {
				a.emitDownloadProgress(0, downloaded, totalSize, readErr.Error())
			}
			return fmt.Errorf("read body: %w", readErr)
		}
	}
	out.Close()

	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("rename: %w", err)
	}

	a.emitDownloadProgress(100, downloaded, totalSize, "")
	return nil
}

func (a *App) emitDownloadProgress(pct int, downloaded, total int64, errMsg string) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "embedding-download-progress", map[string]interface{}{
		"percent":    pct,
		"downloaded": downloaded,
		"total":      total,
		"error":      errMsg,
	})
}

// backgroundPreloadEmbeddingModel silently downloads the embedding model in the
// background when vector search is not yet enabled and the model file is missing.
// On success it verifies the model by loading it, then auto-enables vector search.
// Supports resume: if a previous .tmp file exists, the download continues from
// where it left off (HTTP Range).
func (a *App) backgroundPreloadEmbeddingModel() {
	// Wait a bit for ensureRemoteInfra to initialize memoryStore/toolRouter etc.
	time.Sleep(15 * time.Second)

	// Only run if vector search is currently OFF and model doesn't exist.
	cfg, err := a.LoadConfig()
	if err != nil || cfg.VectorSearchEnabled {
		return
	}

	dir, err := embeddingModelsDir()
	if err != nil {
		return
	}
	destPath := filepath.Join(dir, embeddingModelFilename)
	if _, err := os.Stat(destPath); err == nil {
		// Model file already exists — just verify and auto-enable.
		if a.verifyAndEnableEmbedding(destPath) {
			return
		}
		// Verification failed — file is corrupt, remove and re-download.
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

	// Verify and auto-enable.
	a.verifyAndEnableEmbedding(destPath)
}

// verifyAndEnableEmbedding loads the model to verify integrity, then auto-enables
// vector search if successful. Returns true on success.
func (a *App) verifyAndEnableEmbedding(modelPath string) bool {
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
		return false
	}

	fmt.Println("[embedding] model verified, auto-enabling vector search")
	if err := a.SetVectorSearchEnabled(true); err != nil {
		fmt.Printf("[embedding] auto-enable failed: %v\n", err)
		return false
	}
	return true
}
