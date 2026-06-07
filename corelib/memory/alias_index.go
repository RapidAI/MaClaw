package memory

import (
	"strings"
	"sync"
)

// AliasMatchBoost is the additive score boost applied when a recall query
// matches a known alias. It sits between baseline (0) and tagExactMatchBoost
// (+5.0), providing a moderate signal for semantic gap bridging.
const AliasMatchBoost = 2.0

// aliasCapacity is the maximum number of normalized terms tracked.
// When exceeded, the oldest entries (by insertion order) are evicted (FIFO).
const aliasCapacity = 1000

// AliasIndex maps entity aliases for recall query expansion.
// Rebuilt from entry Tags and Entities during rebuildDerivedIndexesLocked.
//
// Bidirectional: if "4090服务器" → "api.rapidai.tech" is registered,
// then "api.rapidai.tech" → "4090服务器" is also stored.
type AliasIndex struct {
	mu       sync.RWMutex
	aliases  map[string][]string // normalized term → list of known aliases
	order    []string            // insertion order for FIFO eviction
	capacity int
}

// NewAliasIndex creates an AliasIndex with the default capacity.
func NewAliasIndex() *AliasIndex {
	return &AliasIndex{
		aliases:  make(map[string][]string),
		order:    make([]string, 0, aliasCapacity),
		capacity: aliasCapacity,
	}
}

// Expand returns known aliases for any entities found in the input.
// Used by RecallDynamic to augment the BM25 multi-query set.
// The returned slice is deduplicated and excludes the input entities themselves.
func (ai *AliasIndex) Expand(entities []string) []string {
	if len(entities) == 0 {
		return nil
	}
	ai.mu.RLock()
	defer ai.mu.RUnlock()

	if len(ai.aliases) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(entities))
	for _, e := range entities {
		seen[normalize(e)] = struct{}{}
	}

	var result []string
	for _, entity := range entities {
		key := normalize(entity)
		aliases, ok := ai.aliases[key]
		if !ok {
			continue
		}
		for _, alias := range aliases {
			norm := normalize(alias)
			if _, exists := seen[norm]; exists {
				continue
			}
			seen[norm] = struct{}{}
			result = append(result, alias)
		}
	}
	return result
}

// Register adds a bidirectional alias mapping.
// For each alias in aliases, both term→alias and alias→term are stored.
func (ai *AliasIndex) Register(term string, aliases []string) {
	if term == "" || len(aliases) == 0 {
		return
	}
	ai.mu.Lock()
	defer ai.mu.Unlock()

	termNorm := normalize(term)
	for _, alias := range aliases {
		if alias == "" {
			continue
		}
		aliasNorm := normalize(alias)
		if aliasNorm == termNorm {
			continue // don't alias to self
		}

		// term → alias
		ai.addMappingLocked(termNorm, alias)
		// alias → term (bidirectional)
		ai.addMappingLocked(aliasNorm, term)
	}
}

// Rebuild reconstructs the index from all active entries' Tags and Entities.
// Pairs of tags within the same entry are considered potential aliases.
//
// NOTE: Complexity is O(entries × tags²) per entry. For entries with many tags
// (>10), the inner loop generates up to C(n,2) pairs. The capacity limit (1000
// normalized terms, FIFO eviction) bounds total memory, but rebuild time could
// be significant for stores with 10000+ entries having many tags. In practice,
// entries rarely have more than 5-6 tags, keeping this well within budget. If
// profiling reveals hotspots, consider capping tags per entry to 10 during rebuild.
func (ai *AliasIndex) Rebuild(entries []Entry) {
	ai.mu.Lock()
	defer ai.mu.Unlock()

	// Reset state.
	ai.aliases = make(map[string][]string)
	ai.order = make([]string, 0, aliasCapacity)

	for _, entry := range entries {
		if !entry.IsActive() {
			continue
		}
		tags := entry.Tags
		if len(tags) < 2 {
			continue
		}
		// Each pair of tags in the same entry are potential aliases.
		for i := 0; i < len(tags); i++ {
			for j := i + 1; j < len(tags); j++ {
				tagI := tags[i]
				tagJ := tags[j]
				if tagI == "" || tagJ == "" {
					continue
				}
				normI := normalize(tagI)
				normJ := normalize(tagJ)
				if normI == normJ {
					continue
				}
				ai.addMappingLocked(normI, tagJ)
				ai.addMappingLocked(normJ, tagI)
			}
		}
	}
}

// Len returns the number of normalized terms in the index (for testing).
func (ai *AliasIndex) Len() int {
	ai.mu.RLock()
	defer ai.mu.RUnlock()
	return len(ai.aliases)
}

// addMappingLocked adds alias to the list for key. Performs FIFO eviction if
// capacity is reached. Caller must hold ai.mu write lock.
func (ai *AliasIndex) addMappingLocked(key, alias string) {
	// Check if key already exists.
	existing, exists := ai.aliases[key]
	if exists {
		// Check for duplicate alias (case-insensitive).
		aliasNorm := normalize(alias)
		for _, a := range existing {
			if normalize(a) == aliasNorm {
				return // already registered
			}
		}
		ai.aliases[key] = append(existing, alias)
		return
	}

	// New key — check capacity and evict if needed.
	for len(ai.order) >= ai.capacity {
		ai.evictOldestLocked()
	}

	ai.aliases[key] = []string{alias}
	ai.order = append(ai.order, key)
}

// evictOldestLocked removes the oldest entry by insertion order.
// Caller must hold ai.mu write lock.
func (ai *AliasIndex) evictOldestLocked() {
	if len(ai.order) == 0 {
		return
	}
	oldest := ai.order[0]
	ai.order = ai.order[1:]
	delete(ai.aliases, oldest)
}

// normalize converts a string to lowercase for case-insensitive matching.
func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
