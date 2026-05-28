package httpapi

import (
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
	asyncMu      sync.Mutex
	asyncRunning bool
	asyncPending bool
}

// snapshotMaxPosts is the upper bound for posts loaded into the snapshot cache.
// Adjust if the gossip board grows beyond this limit.
const snapshotMaxPosts = 100000
const snapshotRefreshPageSize = 1000

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
	if gc == nil {
		return nil
	}
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

func (gc *GossipCache) RefreshAsync(ctx context.Context) {
	if gc == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	} else {
		ctx = context.WithoutCancel(ctx)
	}
	gc.asyncMu.Lock()
	if gc.asyncRunning {
		gc.asyncPending = true
		gc.asyncMu.Unlock()
		return
	}
	gc.asyncRunning = true
	gc.asyncMu.Unlock()
	go gc.runAsyncRefresh(ctx)
}

func (gc *GossipCache) runAsyncRefresh(ctx context.Context) {
	for {
		if err := gc.Refresh(ctx); err != nil {
			log.Printf("[gossip-cache] async refresh failed: %v", err)
		}
		gc.asyncMu.Lock()
		if gc.asyncPending {
			gc.asyncPending = false
			gc.asyncMu.Unlock()
			continue
		}
		gc.asyncRunning = false
		gc.asyncMu.Unlock()
		return
	}
}

// doRefresh performs the actual snapshot generation. Separated so Refresh
// can uniformly track failure counts regardless of which step fails.
func (gc *GossipCache) doRefresh(ctx context.Context) error {
	dir := filepath.Dir(gc.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp := gc.filePath + ".tmp"
	file, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	removeTmp := true
	defer func() {
		_ = file.Close()
		if removeTmp {
			_ = os.Remove(tmp)
		}
	}()

	gz, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("gzip writer: %w", err)
	}
	written, err := gc.writeSnapshotGzip(ctx, gz)
	if err != nil {
		_ = gz.Close()
		return err
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("gzip close: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close tmp: %w", err)
	}
	// On Windows, os.Rename fails if destination exists; remove first.
	_ = os.Remove(gc.filePath)
	if err := os.Rename(tmp, gc.filePath); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	removeTmp = false
	info, _ := os.Stat(gc.filePath)
	size := int64(0)
	if info != nil {
		size = info.Size()
	}

	log.Printf("[gossip-cache] refreshed, %d posts, %d bytes gz", written, size)
	return nil
}

func (gc *GossipCache) writeSnapshotGzip(ctx context.Context, gz *gzip.Writer) (int, error) {
	if _, err := gz.Write([]byte(`{"posts":[`)); err != nil {
		return 0, fmt.Errorf("gzip write: %w", err)
	}
	written := 0
	total := 0
	first := true
	for offset := 0; offset < snapshotMaxPosts; offset += snapshotRefreshPageSize {
		limit := snapshotRefreshPageSize
		if remaining := snapshotMaxPosts - offset; remaining < limit {
			limit = remaining
		}
		posts, nextTotal, err := gc.gossip.ListPosts(ctx, offset, limit)
		if err != nil {
			return written, fmt.Errorf("list posts: %w", err)
		}
		if nextTotal > total {
			total = nextTotal
		}
		for _, post := range posts {
			if post == nil {
				continue
			}
			if !first {
				if _, err := gz.Write([]byte{','}); err != nil {
					return written, fmt.Errorf("gzip write: %w", err)
				}
			}
			first = false
			data, err := json.Marshal(gossipPostToPublicMap(post))
			if err != nil {
				return written, fmt.Errorf("marshal: %w", err)
			}
			if _, err := gz.Write(data); err != nil {
				return written, fmt.Errorf("gzip write: %w", err)
			}
			written++
		}
		if len(posts) == 0 || len(posts) < limit || offset+len(posts) >= total {
			break
		}
	}
	if total > snapshotMaxPosts {
		total = snapshotMaxPosts
	}
	if total < written {
		total = written
	}
	if _, err := gz.Write([]byte(fmt.Sprintf(`],"total":%d}`, total))); err != nil {
		return written, fmt.Errorf("gzip write: %w", err)
	}
	return written, nil
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
