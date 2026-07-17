package scheduler

import (
	"strings"
	"sync"
	"time"
)

// DefaultTargetListCacheTTL is the default freshness window for catalog lists
// (group directories are relatively stable; avoid hammering open platforms).
const DefaultTargetListCacheTTL = 2 * time.Minute

// TargetListCache is a small per-channel TTL cache of TargetRef slices.
// Safe for concurrent use. Used by GUI/TUI catalog list implementations.
// Channel keys are canonical (DefaultDeliveryChannel) so "蓝信" and "lansenger" share a slot.
type TargetListCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]targetListCacheEntry
}

type targetListCacheEntry struct {
	refs []TargetRef
	at   time.Time
}

// NewTargetListCache creates a cache. ttl <= 0 uses DefaultTargetListCacheTTL.
func NewTargetListCache(ttl time.Duration) *TargetListCache {
	if ttl <= 0 {
		ttl = DefaultTargetListCacheTTL
	}
	return &TargetListCache{
		ttl:     ttl,
		entries: make(map[string]targetListCacheEntry),
	}
}

// Get returns a copy of cached refs when still fresh.
func (c *TargetListCache) Get(channel string) ([]TargetRef, bool) {
	if c == nil {
		return nil, false
	}
	ch := DefaultDeliveryChannel(channel)
	c.mu.Lock()
	defer c.mu.Unlock()
	ent, ok := c.entries[ch]
	if !ok || time.Since(ent.at) > c.ttl {
		return nil, false
	}
	return cloneTargetRefs(ent.refs), true
}

// Put stores a copy of refs for channel.
func (c *TargetListCache) Put(channel string, refs []TargetRef) {
	if c == nil {
		return
	}
	ch := DefaultDeliveryChannel(channel)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]targetListCacheEntry)
	}
	c.entries[ch] = targetListCacheEntry{
		refs: cloneTargetRefs(refs),
		at:   time.Now(),
	}
}

// Invalidate drops one channel. Empty channel invalidates all entries
// (cannot mean "default channel" — use an explicit name to drop lansenger only).
func (c *TargetListCache) Invalidate(channel string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		return
	}
	if strings.TrimSpace(channel) == "" {
		c.entries = make(map[string]targetListCacheEntry)
		return
	}
	delete(c.entries, DefaultDeliveryChannel(channel))
}

// GetOrLoad returns cached refs or calls load, then caches the full unfiltered list.
// Callers should apply FilterTargetRefs for query after GetOrLoad.
func (c *TargetListCache) GetOrLoad(channel string, load func() ([]TargetRef, error)) ([]TargetRef, error) {
	if load == nil {
		return nil, nil
	}
	if refs, ok := c.Get(channel); ok {
		return refs, nil
	}
	refs, err := load()
	if err != nil {
		return nil, err
	}
	c.Put(channel, refs)
	return cloneTargetRefs(refs), nil
}

func cloneTargetRefs(in []TargetRef) []TargetRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]TargetRef, len(in))
	copy(out, in)
	return out
}
