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

	// Re-wire embedder on the memory store.
	if a.memoryStore != nil {
		if enabled {
			modelPath := embedding.DefaultModelPath()
			emb := embedding.NewDefaultEmbedder(modelPath)
			a.memoryStore.SetEmbedder(emb)
		} else {
			a.memoryStore.SetEmbedder(embedding.NoopEmbedder{})
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

// DownloadEmbeddingModel downloads the embedding model from the configured Hub.
// Progress is emitted via Wails event "embedding-download-progress" with payload:
//   { "percent": int, "downloaded": int64, "total": int64, "error": string }
// This method blocks until download completes or fails.
func (a *App) DownloadEmbeddingModel() error {
	// Prevent concurrent downloads — second caller returns immediately.
	if !embeddingDownloadMu.TryLock() {
		return fmt.Errorf("download already in progress")
	}
	defer embeddingDownloadMu.Unlock()

	cfg, err := a.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	hubURL := strings.TrimRight(cfg.RemoteHubURL, "/")
	if hubURL == "" {
		return fmt.Errorf("Hub URL 未配置，请先在「移动端注册」中配置 Hub 地址")
	}

	dir, err := embeddingModelsDir()
	if err != nil {
		return fmt.Errorf("create models dir: %w", err)
	}
	destPath := filepath.Join(dir, embeddingModelFilename)
	tmpPath := destPath + ".tmp"

	downloadURL := hubURL + "/api/v1/models/" + embeddingModelFilename

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		a.emitDownloadProgress(0, 0, 0, err.Error())
		return fmt.Errorf("download request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("Hub 返回 HTTP %d", resp.StatusCode)
		a.emitDownloadProgress(0, 0, 0, msg)
		return fmt.Errorf("hub returned HTTP %d", resp.StatusCode)
	}

	totalSize, _ := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)

	out, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer func() {
		out.Close()
		os.Remove(tmpPath) // clean up on failure; no-op if already renamed
	}()

	buf := make([]byte, 64*1024)
	var downloaded int64
	lastEmit := time.Now()

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := out.Write(buf[:n]); wErr != nil {
				a.emitDownloadProgress(0, downloaded, totalSize, wErr.Error())
				return fmt.Errorf("write file: %w", wErr)
			}
			downloaded += int64(n)
			// Emit progress at most every 200ms to avoid flooding
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
			a.emitDownloadProgress(0, downloaded, totalSize, readErr.Error())
			return fmt.Errorf("read body: %w", readErr)
		}
	}
	out.Close()

	// Rename tmp to final
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
