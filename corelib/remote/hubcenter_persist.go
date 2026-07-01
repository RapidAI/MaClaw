package remote

import (
	"strings"
	"sync"
	"time"
)

// HubCenterPersister is the interface for persisting HubCenter URL selection.
// Both GUI and TUI implement this interface to share the same persistence logic.
type HubCenterPersister interface {
	// LoadHubCenterURLs returns the currently configured HubCenter URLs.
	// Returns (preferredURL, allURLs).
	LoadHubCenterURLs() (string, []string)

	// SaveHubCenterURLs persists the selected HubCenter URL and discovered fallback list.
	SaveHubCenterURLs(preferred string, discovered []string) error
}

// HubCenterSelectionCache provides write-throttling for HubCenter URL persistence.
// It prevents redundant disk writes when the selection hasn't changed.
//
// This is the shared implementation that both GUI and TUI use. The actual
// persistence is delegated to the HubCenterPersister interface.
type HubCenterSelectionCache struct {
	mu         sync.RWMutex
	base       string
	all        []string
	resolvedAt time.Time
	ttl        time.Duration
}

// NewHubCenterSelectionCache creates a new cache with the given TTL.
func NewHubCenterSelectionCache(ttl time.Duration) *HubCenterSelectionCache {
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	return &HubCenterSelectionCache{ttl: ttl}
}

// Get returns the cached base URL and all URLs. Returns "", nil if expired.
func (c *HubCenterSelectionCache) Get() (string, []string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.base == "" || time.Since(c.resolvedAt) > c.ttl {
		return "", nil
	}
	cp := make([]string, len(c.all))
	copy(cp, c.all)
	return c.base, cp
}

// Set stores a freshly resolved URL.
func (c *HubCenterSelectionCache) Set(base string, all []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.base = base
	c.all = append([]string(nil), all...)
	c.resolvedAt = time.Now()
}

// Invalidate clears the cached HubCenter URL, forcing the next resolution to
// perform a full discovery probe. Call this when all cached candidates fail
// (connection refused, timeout, 5xx) — the cached address is likely stale/dead.
func (c *HubCenterSelectionCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.base = ""
	c.all = nil
	c.resolvedAt = time.Time{}
}

// RememberSelectionThrottled persists the HubCenter selection only when it changes.
// This is the single source of truth for the throttling logic.
//
// Parameters:
//   - persister: the interface to actually save the config
//   - base: the selected HubCenter base URL
//   - discovered: all discovered HubCenter URLs (for failover)
//
// The function compares against the cache and only calls persister.SaveHubCenterURLs
// when the base URL or discovered list actually changes.
func (c *HubCenterSelectionCache) RememberSelectionThrottled(persister HubCenterPersister, base string, discovered []string) {
	if persister == nil || base == "" {
		return
	}

	// Normalize the base URL.
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	discovered = NormalizeHubCenterURLs(discovered)

	// Check cache to avoid redundant writes.
	cachedBase, cachedAll := c.Get()
	if base == cachedBase && StringSliceEqual(discovered, cachedAll) {
		return // nothing changed, skip disk write
	}

	// Update cache first.
	c.Set(base, discovered)

	// Persist to disk.
	_ = persister.SaveHubCenterURLs(base, discovered)
}

// StringSliceEqual reports whether two string slices have the same elements
// in the same order.
func StringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
