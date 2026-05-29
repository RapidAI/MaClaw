package corelib

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type LLMPromptResponseCache struct {
	dir      string
	mu       sync.Mutex
	entries  map[string]*LLMPromptResponseCacheEntry
	inflight map[string]*llmPromptResponseCacheFlight
}

type LLMPromptResponseCacheEntry struct {
	Key       string    `json:"key"`
	Body      []byte    `json:"body"`
	ExpiresAt time.Time `json:"expires_at"`
	LastUsed  time.Time `json:"last_used"`
	Size      int64     `json:"size"`
}

type llmPromptResponseCacheFlight struct {
	done   chan struct{}
	body   []byte
	stored bool
	err    error
}

func NewLLMPromptResponseCache(dir string) *LLMPromptResponseCache {
	return &LLMPromptResponseCache{dir: dir, entries: make(map[string]*LLMPromptResponseCacheEntry), inflight: make(map[string]*llmPromptResponseCacheFlight)}
}

func (c *LLMPromptResponseCache) Get(key string, cfg LLMPromptCacheConfig) ([]byte, bool) {
	cfg = cfg.WithDefaults()
	now := time.Now()
	c.mu.Lock()
	if entry := c.entries[key]; entry != nil {
		if now.Before(entry.ExpiresAt) {
			shouldTouch := now.Sub(entry.LastUsed) > time.Second
			entry.LastUsed = now
			body := append([]byte(nil), entry.Body...)
			c.mu.Unlock()
			if shouldTouch {
				c.touchDisk(key, now)
			}
			return body, true
		}
		delete(c.entries, key)
	}
	c.mu.Unlock()
	entry, ok := c.readDisk(key)
	if !ok {
		return nil, false
	}
	if now.After(entry.ExpiresAt) {
		c.Delete(key)
		return nil, false
	}
	entry.LastUsed = now
	c.touchDisk(key, now)
	c.mu.Lock()
	c.entries[key] = entry
	c.pruneMemoryLocked(cfg)
	c.mu.Unlock()
	return append([]byte(nil), entry.Body...), true
}

func (c *LLMPromptResponseCache) Set(key string, body []byte, cfg LLMPromptCacheConfig) bool {
	if len(body) == 0 {
		return false
	}
	cfg = cfg.WithDefaults()
	now := time.Now()
	entry := &LLMPromptResponseCacheEntry{Key: key, Body: append([]byte(nil), body...), ExpiresAt: now.Add(time.Duration(cfg.TTLSeconds) * time.Second), LastUsed: now, Size: int64(len(body))}
	stored := false
	if entry.Size <= cfg.MemoryMaxBytes && cfg.MemoryMaxEntries > 0 {
		c.mu.Lock()
		c.entries[key] = entry
		c.pruneMemoryLocked(cfg)
		c.mu.Unlock()
		stored = true
	}
	if entry.Size <= cfg.DiskMaxBytes {
		stored = c.writeDisk(entry, cfg) || stored
	}
	return stored
}

func (c *LLMPromptResponseCache) Delete(key string) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
	_ = os.Remove(c.path(key))
}

func (c *LLMPromptResponseCache) DoSingleflight(ctx context.Context, key string, cfg LLMPromptCacheConfig, fn func() ([]byte, error)) ([]byte, error) {
	body, _, err := c.DoSingleflightWithShared(ctx, key, cfg, fn)
	return body, err
}

func (c *LLMPromptResponseCache) DoSingleflightWithShared(ctx context.Context, key string, cfg LLMPromptCacheConfig, fn func() ([]byte, error)) ([]byte, bool, error) {
	body, shared, _, err := c.DoSingleflightWithSharedStore(ctx, key, cfg, func() ([]byte, bool, error) {
		body, err := fn()
		return body, true, err
	})
	return body, shared, err
}

func (c *LLMPromptResponseCache) DoSingleflightWithSharedStore(ctx context.Context, key string, cfg LLMPromptCacheConfig, fn func() ([]byte, bool, error)) ([]byte, bool, bool, error) {
	cfg = cfg.WithDefaults()
	c.mu.Lock()
	if flight := c.inflight[key]; flight != nil {
		wait := time.Duration(cfg.SingleflightWaitTimeoutMS) * time.Millisecond
		done := flight.done
		c.mu.Unlock()
		select {
		case <-done:
			return append([]byte(nil), flight.body...), true, flight.stored, flight.err
		case <-ctx.Done():
			return nil, false, false, ctx.Err()
		case <-time.After(wait):
			body, store, err := fn()
			stored := false
			if err == nil && store {
				stored = c.Set(key, body, cfg)
			}
			return body, false, stored, err
		}
	}
	flight := &llmPromptResponseCacheFlight{done: make(chan struct{})}
	c.inflight[key] = flight
	c.mu.Unlock()
	body, store, err := fn()
	stored := false
	if err == nil && store {
		stored = c.Set(key, body, cfg)
	}
	c.mu.Lock()
	flight.body = append([]byte(nil), body...)
	flight.stored = stored
	flight.err = err
	delete(c.inflight, key)
	close(flight.done)
	c.mu.Unlock()
	return body, false, flight.stored, err
}

func (c *LLMPromptResponseCache) pruneMemoryLocked(cfg LLMPromptCacheConfig) {
	var total int64
	items := make([]*LLMPromptResponseCacheEntry, 0, len(c.entries))
	for _, entry := range c.entries {
		total += entry.Size
		items = append(items, entry)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].LastUsed.Before(items[j].LastUsed) })
	for len(c.entries) > cfg.MemoryMaxEntries || total > cfg.MemoryMaxBytes {
		if len(items) == 0 {
			break
		}
		victim := items[0]
		items = items[1:]
		delete(c.entries, victim.Key)
		total -= victim.Size
	}
}

func (c *LLMPromptResponseCache) path(key string) string {
	return filepath.Join(c.dir, llmPromptResponseCacheFileName(key)+".json")
}

func llmPromptResponseCacheFileName(key string) string {
	if key != "" && isSafeLLMPromptResponseCacheKey(key) {
		return key
	}
	sum := sha256.Sum256([]byte(key))
	return "llm_resp_" + hex.EncodeToString(sum[:])
}

func isSafeLLMPromptResponseCacheKey(key string) bool {
	if key == "." || key == ".." || strings.ContainsAny(key, `/\`) {
		return false
	}
	for _, r := range key {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func (c *LLMPromptResponseCache) readDisk(key string) (*LLMPromptResponseCacheEntry, bool) {
	path := c.path(key)
	entry, ok := readLLMPromptResponseCacheDiskFile(path, key)
	if !ok {
		_ = os.Remove(path)
	}
	return entry, ok
}

func (c *LLMPromptResponseCache) touchDisk(key string, ts time.Time) {
	if err := os.Chtimes(c.path(key), ts, ts); err != nil && !os.IsNotExist(err) {
		log.Printf("[llm_cache] touch failed: %v", err)
	}
}

func (c *LLMPromptResponseCache) writeDisk(entry *LLMPromptResponseCacheEntry, cfg LLMPromptCacheConfig) bool {
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		log.Printf("[llm_cache] mkdir failed: %v", err)
		return false
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return false
	}
	if err := writeLLMPromptResponseCacheFileAtomic(c.path(entry.Key), data); err != nil {
		log.Printf("[llm_cache] write failed: %v", err)
		return false
	}
	c.pruneDisk(cfg)
	if _, err := os.Stat(c.path(entry.Key)); err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[llm_cache] stat after prune failed: %v", err)
		}
		return false
	}
	return true
}

func writeLLMPromptResponseCacheFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".llm_resp_*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func (c *LLMPromptResponseCache) pruneDisk(cfg LLMPromptCacheConfig) {
	files, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	type diskItem struct {
		path string
		mod  time.Time
		size int64
	}
	now := time.Now()
	items := make([]diskItem, 0, len(files))
	var total int64
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		if isLLMPromptResponseCacheTempFile(file.Name()) {
			_ = os.Remove(filepath.Join(c.dir, file.Name()))
			continue
		}
		if !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		path := filepath.Join(c.dir, file.Name())
		entry, ok := readLLMPromptResponseCacheDiskFile(path, "")
		if !ok || now.After(entry.ExpiresAt) {
			_ = os.Remove(path)
			continue
		}
		info, err := file.Info()
		if err != nil {
			continue
		}
		items = append(items, diskItem{path: path, mod: info.ModTime(), size: info.Size()})
		total += info.Size()
	}
	sort.Slice(items, func(i, j int) bool { return items[i].mod.Before(items[j].mod) })
	for total > cfg.DiskMaxBytes && len(items) > 0 {
		victim := items[0]
		items = items[1:]
		if err := os.Remove(victim.path); err == nil {
			total -= victim.size
		}
	}
}

func isLLMPromptResponseCacheTempFile(name string) bool {
	return strings.HasPrefix(name, ".llm_resp_") && strings.HasSuffix(name, ".tmp")
}

func readLLMPromptResponseCacheDiskFile(path string, expectedKey string) (*LLMPromptResponseCacheEntry, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var entry LLMPromptResponseCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil || len(entry.Body) == 0 || strings.TrimSpace(entry.Key) == "" || entry.Size != int64(len(entry.Body)) {
		return nil, false
	}
	if expectedKey != "" && entry.Key != expectedKey {
		return nil, false
	}
	if filepath.Base(path) != llmPromptResponseCacheFileName(entry.Key)+".json" {
		return nil, false
	}
	return &entry, true
}

func (c *LLMPromptResponseCache) String() string {
	return fmt.Sprintf("LLMPromptResponseCache(%s)", c.dir)
}

func MigrateLLMPromptResponseCacheDir(fromDir, toDir string) (int, error) {
	fromDir = ExpandLLMPromptCacheDir(fromDir)
	toDir = ExpandLLMPromptCacheDir(toDir)
	if sameDirPath(fromDir, toDir) {
		return 0, nil
	}
	entries, err := os.ReadDir(fromDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if err := os.MkdirAll(toDir, 0o755); err != nil {
		return 0, err
	}
	copied := 0
	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || !isSafeLLMPromptResponseCacheKey(strings.TrimSuffix(entry.Name(), ".json")) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		fromPath := filepath.Join(fromDir, entry.Name())
		diskEntry, ok := readLLMPromptResponseCacheDiskFile(fromPath, "")
		if !ok || now.After(diskEntry.ExpiresAt) {
			continue
		}
		toPath := filepath.Join(toDir, entry.Name())
		if _, err := os.Stat(toPath); err == nil {
			continue
		} else if err != nil && !os.IsNotExist(err) {
			return copied, err
		}
		if err := copyLLMPromptResponseCacheFile(fromPath, toPath); err != nil {
			return copied, err
		}
		copied++
	}
	return copied, nil
}

func sameDirPath(a, b string) bool {
	aAbs, aErr := filepath.Abs(a)
	bAbs, bErr := filepath.Abs(b)
	if aErr == nil {
		a = aAbs
	}
	if bErr == nil {
		b = bAbs
	}
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func copyLLMPromptResponseCacheFile(fromPath, toPath string) error {
	in, err := os.Open(fromPath)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(toPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(toPath)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(toPath)
		return closeErr
	}
	return nil
}
