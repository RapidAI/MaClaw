package llmcache

import (
	"container/list"
	"context"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type Config struct {
	MemoryMaxEntries int
	MemoryMaxBytes   int64
}

type Status struct {
	MemoryEntries    int   `json:"memory_entries"`
	MemoryBytes      int64 `json:"memory_bytes"`
	MemoryMaxEntries int   `json:"memory_max_entries"`
	MemoryMaxBytes   int64 `json:"memory_max_bytes"`
	MemoryHits       int64 `json:"memory_hits"`
	DiskEntries      int64 `json:"disk_entries"`
	DiskBytes        int64 `json:"disk_bytes"`
	DiskExpired      int64 `json:"disk_expired_entries"`
	DiskExpiredBytes int64 `json:"disk_expired_bytes"`
	DiskHits         int64 `json:"disk_hits"`
}

type Cache struct {
	repo  store.LLMPromptCacheRepository
	cfg   Config
	mu    sync.Mutex
	ll    *list.List
	items map[string]*list.Element
	bytes int64
	hits  int64
}

type memoryEntry struct {
	entry *store.LLMPromptCacheEntry
	size  int64
}

func normalizeConfig(cfg Config) Config {
	if cfg.MemoryMaxEntries <= 0 {
		cfg.MemoryMaxEntries = 128
	}
	if cfg.MemoryMaxBytes <= 0 {
		cfg.MemoryMaxBytes = 8 << 20
	}
	return cfg
}

func New(repo store.LLMPromptCacheRepository, cfg Config) *Cache {
	cfg = normalizeConfig(cfg)
	return &Cache{repo: repo, cfg: cfg, ll: list.New(), items: map[string]*list.Element{}}
}

func (c *Cache) UpdateConfig(cfg Config) {
	cfg = normalizeConfig(cfg)
	c.mu.Lock()
	c.cfg = cfg
	for len(c.items) > c.cfg.MemoryMaxEntries || c.bytes > c.cfg.MemoryMaxBytes {
		back := c.ll.Back()
		if back == nil {
			break
		}
		c.removeElement(back)
	}
	c.mu.Unlock()
}

func (c *Cache) Get(ctx context.Context, cacheKey string) (*store.LLMPromptCacheEntry, error) {
	if entry := c.getMemory(cacheKey, time.Now().UTC()); entry != nil {
		return entry, nil
	}
	if c.repo == nil {
		return nil, nil
	}
	entry, err := c.repo.Get(ctx, cacheKey)
	if err != nil || entry == nil {
		return entry, err
	}
	c.putMemory(cloneEntry(entry))
	return cloneEntry(entry), nil
}

func (c *Cache) Put(ctx context.Context, entry *store.LLMPromptCacheEntry) error {
	if entry == nil {
		return nil
	}
	copyEntry := cloneEntry(entry)
	if copyEntry.PayloadBytes <= 0 {
		copyEntry.PayloadBytes = int64(len(copyEntry.Payload))
	}
	if copyEntry.AccessedAt.IsZero() {
		copyEntry.AccessedAt = time.Now().UTC()
	}
	if copyEntry.CreatedAt.IsZero() {
		copyEntry.CreatedAt = copyEntry.AccessedAt
	}
	c.putMemory(copyEntry)
	if c.repo == nil {
		return nil
	}
	return c.repo.Put(ctx, cloneEntry(copyEntry))
}

func (c *Cache) Delete(ctx context.Context, cacheKey string) error {
	c.deleteMemory(cacheKey)
	if c.repo == nil {
		return nil
	}
	return c.repo.Delete(ctx, cacheKey)
}

func (c *Cache) Purge(ctx context.Context) (int64, error) {
	c.mu.Lock()
	memoryCount := int64(len(c.items))
	c.ll = list.New()
	c.items = map[string]*list.Element{}
	c.bytes = 0
	c.mu.Unlock()
	if c.repo == nil {
		return memoryCount, nil
	}
	diskCount, err := c.repo.Purge(ctx)
	if err != nil {
		return memoryCount, err
	}
	return memoryCount + diskCount, nil
}
func (c *Cache) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	c.deleteExpiredMemory(now)
	if c.repo == nil {
		return 0, nil
	}
	return c.repo.DeleteExpired(ctx, now)
}

func (c *Cache) TrimDiskToBytes(ctx context.Context, maxBytes int64) (int64, error) {
	if c.repo == nil {
		return 0, nil
	}
	return c.repo.TrimToBytes(ctx, maxBytes)
}

func (c *Cache) Repository() store.LLMPromptCacheRepository {
	return c.repo
}
func (c *Cache) Status(ctx context.Context, now time.Time) (*Status, error) {
	st := &Status{}
	c.mu.Lock()
	st.MemoryEntries = len(c.items)
	st.MemoryBytes = c.bytes
	st.MemoryMaxEntries = c.cfg.MemoryMaxEntries
	st.MemoryMaxBytes = c.cfg.MemoryMaxBytes
	st.MemoryHits = c.hits
	c.mu.Unlock()
	if c.repo == nil {
		return st, nil
	}
	disk, err := c.repo.Stats(ctx, now)
	if err != nil {
		return nil, err
	}
	st.DiskEntries = disk.Entries
	st.DiskBytes = disk.TotalBytes
	st.DiskExpired = disk.ExpiredEntries
	st.DiskExpiredBytes = disk.ExpiredBytes
	st.DiskHits = disk.TotalHits
	return st, nil
}

func (c *Cache) getMemory(cacheKey string, now time.Time) *store.LLMPromptCacheEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	elem := c.items[cacheKey]
	if elem == nil {
		return nil
	}
	item := elem.Value.(*memoryEntry)
	if expired(item.entry, now) {
		c.removeElement(elem)
		return nil
	}
	item.entry.AccessedAt = now
	item.entry.HitCount++
	c.hits++
	c.ll.MoveToFront(elem)
	return cloneEntry(item.entry)
}

func (c *Cache) putMemory(entry *store.LLMPromptCacheEntry) {
	if entry == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	size := entry.PayloadBytes
	if size <= 0 {
		size = int64(len(entry.Payload))
	}
	if elem := c.items[entry.CacheKey]; elem != nil {
		item := elem.Value.(*memoryEntry)
		c.bytes -= item.size
		item.entry = cloneEntry(entry)
		item.size = size
		c.bytes += size
		c.ll.MoveToFront(elem)
	} else {
		elem := c.ll.PushFront(&memoryEntry{entry: cloneEntry(entry), size: size})
		c.items[entry.CacheKey] = elem
		c.bytes += size
	}
	for len(c.items) > c.cfg.MemoryMaxEntries || c.bytes > c.cfg.MemoryMaxBytes {
		back := c.ll.Back()
		if back == nil {
			break
		}
		c.removeElement(back)
	}
}

func (c *Cache) deleteMemory(cacheKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem := c.items[cacheKey]; elem != nil {
		c.removeElement(elem)
	}
}

func (c *Cache) deleteExpiredMemory(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for elem := c.ll.Back(); elem != nil; {
		prev := elem.Prev()
		item := elem.Value.(*memoryEntry)
		if expired(item.entry, now) {
			c.removeElement(elem)
		}
		elem = prev
	}
}

func (c *Cache) removeElement(elem *list.Element) {
	item := elem.Value.(*memoryEntry)
	delete(c.items, item.entry.CacheKey)
	c.ll.Remove(elem)
	c.bytes -= item.size
	if c.bytes < 0 {
		c.bytes = 0
	}
}

func cloneEntry(entry *store.LLMPromptCacheEntry) *store.LLMPromptCacheEntry {
	if entry == nil {
		return nil
	}
	copyEntry := *entry
	copyEntry.Payload = append([]byte(nil), entry.Payload...)
	if entry.ExpiresAt != nil {
		t := *entry.ExpiresAt
		copyEntry.ExpiresAt = &t
	}
	return &copyEntry
}

func expired(entry *store.LLMPromptCacheEntry, now time.Time) bool {
	return entry != nil && entry.ExpiresAt != nil && !entry.ExpiresAt.IsZero() && !entry.ExpiresAt.After(now)
}
