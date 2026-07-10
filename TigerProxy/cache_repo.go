package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

// fileCacheRepo implements llmpool.CacheRepository using one JSON file per entry
// in a local directory. Designed for TigerProxy's single-user desktop usage
// (typically <256 entries, <32MB total). No external dependencies.
type fileCacheRepo struct {
	dir     string
	maxSize int64 // max total bytes on disk
	mu      sync.Mutex
}

// diskEntry is the JSON-serialized form of a cache entry on disk.
type diskEntry struct {
	CacheKey     string     `json:"cache_key"`
	Model        string     `json:"model"`
	Kind         string     `json:"kind"`
	Payload      []byte     `json:"payload"`
	PayloadBytes int64      `json:"payload_bytes"`
	HitCount     int64      `json:"hit_count"`
	CreatedAt    time.Time  `json:"created_at"`
	AccessedAt   time.Time  `json:"accessed_at"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

func newFileCacheRepo(dir string, maxSizeBytes int64) (*fileCacheRepo, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	return &fileCacheRepo{dir: dir, maxSize: maxSizeBytes}, nil
}

func (r *fileCacheRepo) entryPath(cacheKey string) string {
	// cacheKey is "llm_resp_<64hex>" — safe for filenames, use first 32 chars to avoid path length issues.
	name := cacheKey
	if len(name) > 40 {
		name = name[:40]
	}
	return filepath.Join(r.dir, name+".json")
}

func (r *fileCacheRepo) Get(_ context.Context, cacheKey string) (*llmpool.CacheEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.getLocked(cacheKey)
}

func (r *fileCacheRepo) getLocked(cacheKey string) (*llmpool.CacheEntry, error) {
	data, err := os.ReadFile(r.entryPath(cacheKey))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var de diskEntry
	if err := json.Unmarshal(data, &de); err != nil {
		// Corrupted file — remove and return nil.
		_ = os.Remove(r.entryPath(cacheKey))
		return nil, nil
	}
	return &llmpool.CacheEntry{
		CacheKey:     de.CacheKey,
		Model:        de.Model,
		Kind:         de.Kind,
		Payload:      de.Payload,
		PayloadBytes: de.PayloadBytes,
		HitCount:     de.HitCount,
		CreatedAt:    de.CreatedAt,
		AccessedAt:   de.AccessedAt,
		ExpiresAt:    de.ExpiresAt,
	}, nil
}

func (r *fileCacheRepo) Put(_ context.Context, entry *llmpool.CacheEntry) error {
	if entry == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	de := diskEntry{
		CacheKey:     entry.CacheKey,
		Model:        entry.Model,
		Kind:         entry.Kind,
		Payload:      entry.Payload,
		PayloadBytes: entry.PayloadBytes,
		HitCount:     entry.HitCount,
		CreatedAt:    entry.CreatedAt,
		AccessedAt:   entry.AccessedAt,
		ExpiresAt:    entry.ExpiresAt,
	}
	data, err := json.Marshal(de)
	if err != nil {
		return err
	}
	path := r.entryPath(entry.CacheKey)
	// Atomic write: tmp + rename.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (r *fileCacheRepo) Delete(_ context.Context, cacheKey string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	err := os.Remove(r.entryPath(cacheKey))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (r *fileCacheRepo) Purge(_ context.Context) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return 0, err
	}
	var count int64
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		if os.Remove(filepath.Join(r.dir, e.Name())) == nil {
			count++
		}
	}
	return count, nil
}

func (r *fileCacheRepo) DeleteExpired(_ context.Context, now time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return 0, err
	}
	var count int64
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(r.dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var de diskEntry
		if json.Unmarshal(data, &de) != nil {
			_ = os.Remove(path)
			count++
			continue
		}
		if de.ExpiresAt != nil && !de.ExpiresAt.IsZero() && !de.ExpiresAt.After(now) {
			_ = os.Remove(path)
			count++
		}
	}
	return count, nil
}

func (r *fileCacheRepo) TrimToBytes(_ context.Context, maxBytes int64) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	type fileInfo struct {
		path       string
		size       int64
		accessedAt time.Time
	}
	dirEntries, err := os.ReadDir(r.dir)
	if err != nil {
		return 0, err
	}
	var files []fileInfo
	var totalSize int64
	for _, e := range dirEntries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{
			path:       filepath.Join(r.dir, e.Name()),
			size:       info.Size(),
			accessedAt: info.ModTime(),
		})
		totalSize += info.Size()
	}
	if totalSize <= maxBytes {
		return 0, nil
	}
	// Evict oldest-accessed first.
	sort.Slice(files, func(i, j int) bool {
		return files[i].accessedAt.Before(files[j].accessedAt)
	})
	var removed int64
	for _, f := range files {
		if totalSize <= maxBytes {
			break
		}
		if os.Remove(f.path) == nil {
			totalSize -= f.size
			removed++
		}
	}
	return removed, nil
}

func (r *fileCacheRepo) Stats(_ context.Context, now time.Time) (*llmpool.CacheStats, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	dirEntries, err := os.ReadDir(r.dir)
	if err != nil {
		return &llmpool.CacheStats{}, nil
	}
	st := &llmpool.CacheStats{}
	for _, e := range dirEntries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		st.Entries++
		st.TotalBytes += info.Size()
		// Check expiration by reading file header (only for stats accuracy).
		data, err := os.ReadFile(filepath.Join(r.dir, e.Name()))
		if err != nil {
			continue
		}
		var de diskEntry
		if json.Unmarshal(data, &de) != nil {
			continue
		}
		st.TotalHits += de.HitCount
		if de.ExpiresAt != nil && !de.ExpiresAt.IsZero() && !de.ExpiresAt.After(now) {
			st.ExpiredEntries++
			st.ExpiredBytes += info.Size()
		}
	}
	return st, nil
}
