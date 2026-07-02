package main

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
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// embeddingModelDefaultURL is the primary download source (from corelib/embedding).
var embeddingModelDefaultURL = embedding.DefaultModelDownloadURL

// knowledgeStoreManager manages the process-level knowledge store instance.
// Shared by all HTTP handlers and the agent executor.
type knowledgeStoreManager struct {
	store    *knowledge.SQLiteStore
	access   *knowledgeAccessService
	agent    *multiKnowledgeStore
	embedder embedding.Embedder
	mu       sync.RWMutex
	dbPath   string
	dataRoot string
	closed   bool
	done     chan struct{}  // closed on Close(), signals background goroutines to stop
	wg       sync.WaitGroup // tracks background goroutines (download, backfill)
}

// newKnowledgeStoreManager initializes the knowledge store.
// The store is created at $MACLAW_DATA_ROOT/knowledge/knowledge.db.
// Embedding is wired later through srvAIModelManager so MaClawSrv only keeps
// one server-wide embedding model loaded in memory.
func newKnowledgeStoreManager(dataRoot string) (*knowledgeStoreManager, error) {
	dbPath := filepath.Join(dataRoot, "knowledge", "knowledge.db")
	store, err := knowledge.NewSQLiteStore(dbPath)
	if err != nil {
		return nil, err
	}

	access := newKnowledgeAccessService(newFileKVStore(filepath.Join(dataRoot, "knowledge_access.json")))
	mgr := &knowledgeStoreManager{
		store:    store,
		access:   access,
		agent:    newMultiKnowledgeStore(store, access),
		dbPath:   dbPath,
		dataRoot: dataRoot,
		done:     make(chan struct{}),
	}

	if isEmbeddingDisabled() {
		mgr.embedder = embedding.NewNoopEmbedder()
		store.SetEmbedder(mgr.embedder)
		log.Printf("[knowledge] embedding explicitly disabled via MACLAW_EMBEDDING_DISABLED, using FTS-only mode")
	} else {
		mgr.embedder = embedding.NewNoopEmbedder()
		store.SetEmbedder(mgr.embedder)
		log.Printf("[knowledge] embedding waits for shared AI model manager")
	}

	return mgr, nil
}

func (m *knowledgeStoreManager) UseSharedAIModels(aiModels *srvAIModelManager, cfgProvider func() corelib.AppConfig) {
	if m == nil || aiModels == nil || isEmbeddingDisabled() {
		return
	}
	adapter := srvAIModelEmbedderAdapter{manager: aiModels, config: cfgProvider}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	oldEmb := m.embedder
	m.embedder = adapter
	if m.store != nil {
		m.store.SetEmbedder(adapter)
	}
	m.mu.Unlock()
	if oldEmb != nil && !embedding.IsNoop(oldEmb) {
		oldEmb.Close()
	}
	aiModels.setEmbeddingReadyHook(func() {
		m.backfillEmbeddingsAsync("shared embedding model ready")
	})
	if exists, _ := modelFileReady(aiModels.modelPath(embedding.DefaultModelFilename)); exists {
		m.backfillEmbeddingsAsync("shared embedding model already ready")
	}
	log.Printf("[knowledge] embedding uses shared AI model manager")
}

func (m *knowledgeStoreManager) backfillEmbeddingsAsync(reason string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.closed || m.store == nil {
		m.mu.Unlock()
		return
	}
	store := m.store
	done := m.done
	m.wg.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		go func() {
			select {
			case <-done:
				cancel()
			case <-ctx.Done():
			}
		}()
		if err := store.RebuildFTSIndex(ctx); err != nil {
			if ctx.Err() == nil {
				log.Printf("[knowledge] embedding backfill failed after %s: %v", reason, err)
			}
		} else {
			log.Printf("[knowledge] embedding backfill completed after %s", reason)
		}
	}()
}

// backgroundDownloadAndActivate is the legacy knowledge-store download path.
// It only ensures the shared model file exists; runtime loading is owned by
// srvAIModelManager so MaClawSrv keeps one embedding model in memory.
// Respects m.done channel — exits early if the manager is being closed.
func (m *knowledgeStoreManager) backgroundDownloadAndActivate() {
	// Check if already shutting down.
	select {
	case <-m.done:
		return
	default:
	}

	modelsDir := filepath.Join(m.dataRoot, "models")
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		log.Printf("[knowledge] failed to create models dir: %v", err)
		return
	}

	destPath := filepath.Join(modelsDir, embedding.DefaultModelFilename)

	// If model already exists (race with another process), just activate.
	if fi, err := os.Stat(destPath); err == nil {
		if fi.Size() < embeddingModelMinSize {
			// Corrupt/incomplete file from a previous failed download — remove and re-download.
			log.Printf("[knowledge] existing model file too small (%d bytes), removing and re-downloading", fi.Size())
			os.Remove(destPath)
		} else {
			m.activateEmbedder(destPath)
			return
		}
	}

	// Skip download if a recent failure marker exists (avoid retry noise in air-gapped environments).
	failMarker := destPath + ".download-failed"
	if fi, err := os.Stat(failMarker); err == nil {
		if time.Since(fi.ModTime()) < 24*time.Hour {
			log.Printf("[knowledge] skipping download: previous attempt failed at %s (retry after 24h)", fi.ModTime().Format(time.RFC3339))
			return
		}
		// Marker expired — remove and retry.
		os.Remove(failMarker)
	}

	// Try primary URL (GitHub Releases).
	log.Printf("[knowledge] downloading embedding model from %s", embeddingModelDefaultURL)
	if err := downloadModelFile(embeddingModelDefaultURL, destPath, m.done); err != nil {
		log.Printf("[knowledge] primary download failed: %v, trying fallback...", err)

		// Check shutdown before fallback attempt.
		select {
		case <-m.done:
			return
		default:
		}

		// Fallback: Hub URL from environment.
		hubURL := strings.TrimSpace(os.Getenv("MACLAW_HUB_URL"))
		if hubURL == "" {
			log.Printf("[knowledge] embedding model download failed, no fallback URL configured. Vector search unavailable.")
			m.writeDownloadFailMarker(failMarker)
			return
		}
		fallbackURL := strings.TrimRight(hubURL, "/") + "/api/v1/models/" + embedding.DefaultModelFilename
		log.Printf("[knowledge] downloading embedding model from fallback: %s", fallbackURL)
		if err := downloadModelFile(fallbackURL, destPath, m.done); err != nil {
			log.Printf("[knowledge] fallback download also failed: %v. Vector search unavailable.", err)
			m.writeDownloadFailMarker(failMarker)
			return
		}
	}

	// Check shutdown before activation.
	select {
	case <-m.done:
		return
	default:
	}

	// Verify downloaded file size before activation.
	fi, err := os.Stat(destPath)
	if err != nil || fi.Size() < embeddingModelMinSize {
		log.Printf("[knowledge] downloaded model file is too small or missing (%v), removing", err)
		os.Remove(destPath)
		m.writeDownloadFailMarker(failMarker)
		return
	}

	log.Printf("[knowledge] embedding model downloaded successfully to %s (%d bytes)", destPath, fi.Size())
	m.activateEmbedder(destPath)
}

// embeddingModelMinSize is the minimum acceptable file size for the embedding model.
// The actual model is ~328MB; anything below 300MB indicates corruption or incomplete download.
const embeddingModelMinSize = 300 * 1024 * 1024

// writeDownloadFailMarker creates a marker file to suppress repeated download attempts
// in air-gapped environments. The marker expires after 24 hours.
func (m *knowledgeStoreManager) writeDownloadFailMarker(path string) {
	_ = os.WriteFile(path, []byte(time.Now().Format(time.RFC3339)), 0o644)
}

// activateEmbedder is retained for the legacy download path. The knowledge
// store uses srvAIModelEmbedderAdapter, so activation means rebuilding existing
// rows through the shared AI model manager rather than loading another model.
func (m *knowledgeStoreManager) activateEmbedder(modelPath string) {
	_ = modelPath
	m.backfillEmbeddingsAsync("legacy embedding activation")
}

// downloadModelFile downloads a file from url to destPath with HTTP Range resume support.
// The done channel allows early cancellation (e.g., process shutdown).
func downloadModelFile(url, destPath string, done <-chan struct{}) error {
	tmpPath := destPath + ".tmp"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Wire done channel into context for early cancellation.
	go func() {
		select {
		case <-done:
			cancel()
		case <-ctx.Done():
		}
	}()

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
		return fmt.Errorf("download request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	// If server doesn't support Range (returned 200 instead of 206), start from scratch.
	if resp.StatusCode == http.StatusOK && resumeOffset > 0 {
		resumeOffset = 0
	}

	var totalSize int64
	if resp.StatusCode == http.StatusPartialContent {
		if cr := resp.Header.Get("Content-Range"); cr != "" {
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

	buf := make([]byte, 64*1024)
	downloaded := resumeOffset
	lastLog := time.Now()

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := out.Write(buf[:n]); wErr != nil {
				out.Close()
				return fmt.Errorf("write file: %w", wErr)
			}
			downloaded += int64(n)
			if time.Since(lastLog) > 10*time.Second {
				pct := 0
				if totalSize > 0 {
					pct = int(downloaded * 100 / totalSize)
				}
				log.Printf("[knowledge] embedding model download progress: %d%% (%d/%d bytes)", pct, downloaded, totalSize)
				lastLog = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			out.Close()
			return fmt.Errorf("read body: %w", readErr)
		}
	}
	out.Close()

	// Final check: if shutdown was signaled during download, don't rename —
	// the file may be incomplete even if we reached EOF (server-side truncation).
	select {
	case <-done:
		return fmt.Errorf("download cancelled during shutdown")
	default:
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// Store returns the shared knowledge SQLiteStore.
func (m *knowledgeStoreManager) Store() *knowledge.SQLiteStore {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.store
}

// AgentStore returns the authorization-aware knowledge store used by agents and read APIs.
func (m *knowledgeStoreManager) AgentStore() *multiKnowledgeStore {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store == nil {
		return m.agent
	}
	if m.access == nil && strings.TrimSpace(m.dataRoot) != "" {
		m.access = newKnowledgeAccessService(newFileKVStore(filepath.Join(m.dataRoot, "knowledge_access.json")))
	}
	if m.agent == nil || m.agent.store != m.store || m.agent.access != m.access {
		m.agent = newMultiKnowledgeStore(m.store, m.access)
	}
	return m.agent
}

// Access returns the service that controls cross-user readable knowledge scopes.
func (m *knowledgeStoreManager) Access() *knowledgeAccessService {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.access == nil && strings.TrimSpace(m.dataRoot) != "" {
		m.access = newKnowledgeAccessService(newFileKVStore(filepath.Join(m.dataRoot, "knowledge_access.json")))
	}
	return m.access
}

// Close releases the knowledge store and embedding model resources.
// Signals background goroutines (download, backfill) to stop via done channel,
// then waits for them to exit before closing the store.
func (m *knowledgeStoreManager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	close(m.done) // signal all background goroutines
	m.mu.Unlock()

	// Wait for background goroutines to finish (bounded by their own timeouts).
	m.wg.Wait()

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store != nil {
		_ = m.store.Close()
	}
	if m.embedder != nil {
		m.embedder.Close()
	}
	log.Printf("[knowledge] store closed")
}

// resolveEmbeddingModelPath determines the embedding model file path.
// Priority: MACLAW_EMBEDDING_MODEL_PATH env > $MACLAW_DATA_ROOT/models/ > active data directory models.
func resolveEmbeddingModelPath(dataRoot string) string {
	if custom := strings.TrimSpace(os.Getenv("MACLAW_EMBEDDING_MODEL_PATH")); custom != "" {
		return custom
	}
	// Check in data root first
	candidate := filepath.Join(dataRoot, "models", embedding.DefaultModelFilename)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	// Fall back to the active data directory model path.
	return embedding.DefaultModelPath()
}

// isEmbeddingDisabled checks if embedding is explicitly disabled via env.
func isEmbeddingDisabled() bool {
	v := strings.TrimSpace(os.Getenv("MACLAW_EMBEDDING_DISABLED"))
	return v == "1" || v == "true" || v == "TRUE" || v == "yes"
}
