package httpapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

// GossipCache manages a gzip-compressed JSON snapshot of all gossip posts.
type GossipCache struct {
	gossip       store.GossipRepository
	filePath     string
	mu           sync.Mutex
	failureCount int // consecutive refresh failures
}

// snapshotMaxPosts is the upper bound for posts loaded into the snapshot cache.
// Adjust if the gossip board grows beyond this limit.
const snapshotMaxPosts = 100000

func NewGossipCache(gossip store.GossipRepository, filePath string) *GossipCache {
	return &GossipCache{gossip: gossip, filePath: filePath}
}

func (gc *GossipCache) EnsureExists(ctx context.Context) {
	if _, err := os.Stat(gc.filePath); err != nil {
		log.Printf("[gossip-cache] cache file not found, generating...")
		if err := gc.Refresh(ctx); err != nil {
			log.Printf("[gossip-cache] initial generation failed: %v", err)
		}
	}
}

func (gc *GossipCache) Refresh(ctx context.Context) error {
	gc.mu.Lock()
	defer gc.mu.Unlock()

	if err := gc.doRefresh(ctx); err != nil {
		gc.failureCount++
		if gc.failureCount >= 3 {
			log.Printf("[gossip-cache] ERROR: refresh failed %d consecutive times: %v", gc.failureCount, err)
		}
		return err
	}
	gc.failureCount = 0
	return nil
}

// doRefresh performs the actual snapshot generation. Separated so Refresh
// can uniformly track failure counts regardless of which step fails.
func (gc *GossipCache) doRefresh(ctx context.Context) error {
	posts, _, err := gc.gossip.ListPosts(ctx, 0, snapshotMaxPosts)
	if err != nil {
		return fmt.Errorf("list posts: %w", err)
	}

	items := make([]map[string]any, 0, len(posts))
	for _, p := range posts {
		items = append(items, gossipPostToPublicMap(p))
	}
	payload, err := json.Marshal(map[string]any{"posts": items, "total": len(items)})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	var buf bytes.Buffer
	gz, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if _, err := gz.Write(payload); err != nil {
		return fmt.Errorf("gzip write: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("gzip close: %w", err)
	}

	dir := filepath.Dir(gc.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp := gc.filePath + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	// On Windows, os.Rename fails if destination exists; remove first.
	_ = os.Remove(gc.filePath)
	if err := os.Rename(tmp, gc.filePath); err != nil {
		return fmt.Errorf("rename: %w", err)
	}

	log.Printf("[gossip-cache] refreshed, %d posts, %d bytes gz", len(items), buf.Len())
	return nil
}

func computeETag(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf(`"%x"`, h[:8])
}

func GossipSnapshotHandler(gc *GossipCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "If-None-Match")
		w.Header().Set("Access-Control-Expose-Headers", "ETag")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		data, err := os.ReadFile(gc.filePath)
		if err != nil {
			if os.IsNotExist(err) {
				writeError(w, http.StatusServiceUnavailable, "NOT_READY", "Gossip cache not yet generated")
				return
			}
			writeError(w, http.StatusInternalServerError, "READ_FAILED", err.Error())
			return
		}

		etag := computeETag(data)
		if match := r.Header.Get("If-None-Match"); match == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}
}
