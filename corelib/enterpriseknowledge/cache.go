package enterpriseknowledge

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// Shared short-lived client cache for search / auto-recall hot paths.
// Sync agents should keep using Open()+Close() so they never pin a cache slot
// across multi-minute Hub pulls.

const (
	defaultCacheMaxEntries = 32
	defaultCacheIdleTTL    = 2 * time.Minute
)

type cacheEntry struct {
	client   *Client
	lastUsed time.Time
	refs     int
	// drop marks the entry for close on last Release (soft invalidate).
	// Avoids force-closing a client still borrowed by search/auto-recall.
	drop bool
}

// ClientCache is a process-wide pool of enterprise Clients keyed by absolute dataDir.
type ClientCache struct {
	mu      sync.Mutex
	entries map[string]*cacheEntry
	max     int
	idleTTL time.Duration
}

// DefaultCache is used by LeaseMeta / SearchActiveFromDataDir.
var DefaultCache = NewClientCache(defaultCacheMaxEntries, defaultCacheIdleTTL)

// NewClientCache builds a cache. max <= 0 or idleTTL <= 0 use defaults.
func NewClientCache(max int, idleTTL time.Duration) *ClientCache {
	if max <= 0 {
		max = defaultCacheMaxEntries
	}
	if idleTTL <= 0 {
		idleTTL = defaultCacheIdleTTL
	}
	return &ClientCache{
		entries: make(map[string]*cacheEntry),
		max:     max,
		idleTTL: idleTTL,
	}
}

// Lease is a borrowed Client. Always call Release (not Close) when finished.
type Lease struct {
	Client *Client
	cache  *ClientCache
	key    string
}

// Release returns the client to the cache (or no-ops if already released).
func (l *Lease) Release() {
	if l == nil {
		return
	}
	if l.cache == nil || l.key == "" {
		if l.Client != nil {
			_ = l.Client.Close()
			l.Client = nil
		}
		return
	}
	l.cache.release(l.key)
	l.Client = nil
	l.cache = nil
	l.key = ""
}

// LeaseMeta returns a pooled meta client for dataDir (EnsureStore opens knowledge on demand).
func LeaseMeta(dataDir string) (*Lease, error) {
	return DefaultCache.LeaseMeta(dataDir)
}

// LeaseMeta is the instance method for custom caches/tests.
func (c *ClientCache) LeaseMeta(dataDir string) (*Lease, error) {
	if c == nil {
		c = DefaultCache
	}
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return nil, fmt.Errorf("dataDir required")
	}
	key, err := filepath.Abs(dataDir)
	if err != nil || key == "" {
		key = dataDir
	}

	c.mu.Lock()
	now := time.Now()
	c.evictLocked(now)

	// Soft-invalidated (drop) entries stay usable until last Release so in-flight
	// search/auto-recall is not force-closed; SQLite WAL still sees concurrent writes.
	if e, ok := c.entries[key]; ok && e.client != nil && !e.client.isClosed() {
		e.refs++
		e.lastUsed = now
		client := e.client
		c.mu.Unlock()
		return &Lease{Client: client, cache: c, key: key}, nil
	}
	// Drop dead entry if any.
	if e, ok := c.entries[key]; ok {
		if e.client != nil {
			_ = e.client.Close()
		}
		delete(c.entries, key)
	}
	c.evictOneIdleLocked(now)
	// Open SQLite outside the cache lock (can be slow on cold disks).
	c.mu.Unlock()

	client, err := OpenMetaOnly(dataDir)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	// Another goroutine may have pooled the same key while we opened.
	if e, ok := c.entries[key]; ok && e.client != nil && !e.client.isClosed() {
		e.refs++
		e.lastUsed = time.Now()
		pooled := e.client
		c.mu.Unlock()
		_ = client.Close()
		return &Lease{Client: pooled, cache: c, key: key}, nil
	}
	if e, ok := c.entries[key]; ok {
		if e.client != nil {
			_ = e.client.Close()
		}
		delete(c.entries, key)
	}
	c.evictOneIdleLocked(time.Now())
	// Cap growth: if every pooled slot is busy, return an uncached lease so
	// multi-tenant search cannot pin unbounded SQLite handles.
	if len(c.entries) >= c.max {
		c.mu.Unlock()
		return &Lease{Client: client}, nil // Release closes; not pooled
	}
	c.entries[key] = &cacheEntry{
		client:   client,
		lastUsed: time.Now(),
		refs:     1,
	}
	c.mu.Unlock()
	return &Lease{Client: client, cache: c, key: key}, nil
}

func (c *ClientCache) release(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || e == nil {
		return
	}
	if e.refs > 0 {
		e.refs--
	}
	e.lastUsed = time.Now()
	if e.drop && e.refs == 0 {
		if e.client != nil {
			_ = e.client.Close()
		}
		delete(c.entries, key)
	}
}

func (c *ClientCache) evictLocked(now time.Time) {
	for key, e := range c.entries {
		if e == nil || e.refs > 0 {
			continue
		}
		if now.Sub(e.lastUsed) >= c.idleTTL {
			if e.client != nil {
				_ = e.client.Close()
			}
			delete(c.entries, key)
		}
	}
}

func (c *ClientCache) evictOneIdleLocked(now time.Time) {
	if len(c.entries) < c.max {
		return
	}
	var oldestKey string
	var oldest time.Time
	for key, e := range c.entries {
		if e == nil || e.refs > 0 {
			continue
		}
		if oldestKey == "" || e.lastUsed.Before(oldest) {
			oldestKey = key
			oldest = e.lastUsed
		}
	}
	if oldestKey == "" {
		return
	}
	if e := c.entries[oldestKey]; e != nil && e.client != nil {
		_ = e.client.Close()
	}
	delete(c.entries, oldestKey)
	_ = now
}

// Len returns current cache entry count (tests/metrics).
func (c *ClientCache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// Clear force-closes all entries and drops the map (tests / process shutdown).
func (c *ClientCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, e := range c.entries {
		if e != nil && e.client != nil {
			_ = e.client.Close()
		}
		delete(c.entries, key)
	}
}

// Invalidate closes and drops the cache entry for dataDir (call after purge).
func InvalidateCache(dataDir string) {
	DefaultCache.Invalidate(dataDir)
}

// Invalidate drops one dataDir entry.
// If the client is currently leased (refs > 0), it is soft-invalidated and closed
// on the last Release instead of force-closing mid-search.
func (c *ClientCache) Invalidate(dataDir string) {
	if c == nil {
		return
	}
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return
	}
	key, err := filepath.Abs(dataDir)
	if err != nil || key == "" {
		key = dataDir
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	seen := map[string]struct{}{}
	for _, k := range []string{key, dataDir} {
		if _, done := seen[k]; done {
			continue
		}
		seen[k] = struct{}{}
		e, ok := c.entries[k]
		if !ok || e == nil {
			continue
		}
		if e.refs > 0 {
			e.drop = true
			continue
		}
		if e.client != nil {
			_ = e.client.Close()
		}
		delete(c.entries, k)
	}
}

func (c *Client) isClosed() bool {
	if c == nil {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// SearchActiveFromDataDir is a convenience for tools/HTTP: lease → search → release.
func SearchActiveFromDataDir(ctx context.Context, dataDir, query, libraryID string) ([]knowledge.SearchResult, error) {
	if !MetaDBExists(dataDir) {
		return nil, nil
	}
	lease, err := LeaseMeta(dataDir)
	if err != nil {
		return nil, err
	}
	defer lease.Release()
	if !lease.Client.HasActiveLibraries() {
		return []knowledge.SearchResult{}, nil
	}
	return lease.Client.SearchActive(ctx, query, libraryID)
}
