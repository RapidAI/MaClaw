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

// AliasIndex maps explicit synonym pairs for recall query expansion.
// Rebuild loads only alias:<term>=<alias> tags, scoped by entry OwnerID.
// Co-occurring tags are not treated as synonyms.
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

// Expand returns unscoped aliases. Prefer ExpandForOwner when a caller has an owner.
func (ai *AliasIndex) Expand(entities []string) []string {
	return ai.ExpandForOwner(entities, "")
}

// ExpandForOwner returns aliases registered for ownerID only.
// The returned slice is deduplicated and excludes the input entities themselves.
func (ai *AliasIndex) ExpandForOwner(entities []string, ownerID string) []string {
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
		key := aliasIndexKey(ownerID, entity)
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

// ExplicitAliasTag encodes one synonym pair. Only tags in this form are loaded
// by Rebuild. Co-occurring labels, paths, user ids, and session ids are not aliases.
func ExplicitAliasTag(term, alias string) string {
	term = strings.TrimSpace(term)
	alias = strings.TrimSpace(alias)
	if term == "" || alias == "" || strings.Contains(term, "=") || strings.Contains(alias, "=") {
		return ""
	}
	if normalize(term) == normalize(alias) {
		return ""
	}
	return "alias:" + term + "=" + alias
}

func parseExplicitAliasTag(tag string) (string, string, bool) {
	tag = strings.TrimSpace(tag)
	if len(tag) < len("alias:x=y") || !strings.EqualFold(tag[:len("alias:")], "alias:") {
		return "", "", false
	}
	term, alias, ok := strings.Cut(tag[len("alias:"):], "=")
	term = strings.TrimSpace(term)
	alias = strings.TrimSpace(alias)
	if !ok || term == "" || alias == "" || normalize(term) == normalize(alias) {
		return "", "", false
	}
	return term, alias, true
}

func aliasIndexKey(ownerID, term string) string {
	return strings.TrimSpace(ownerID) + "\x00" + normalize(term)
}

// Register adds a bidirectional alias mapping for the unscoped owner.
// For each alias in aliases, both term→alias and alias→term are stored.
func (ai *AliasIndex) Register(term string, aliases []string) {
	ai.RegisterForOwner("", term, aliases)
}

// RegisterForOwner stores aliases visible only to recall for that owner.
// An empty owner does not expand into another owner's queries.
func (ai *AliasIndex) RegisterForOwner(ownerID, term string, aliases []string) {
	if term == "" || len(aliases) == 0 {
		return
	}
	ai.mu.Lock()
	defer ai.mu.Unlock()

	termNorm := normalize(term)
	ownerID = strings.TrimSpace(ownerID)
	for _, alias := range aliases {
		if alias == "" {
			continue
		}
		aliasNorm := normalize(alias)
		if aliasNorm == termNorm {
			continue
		}
		ai.addMappingLocked(aliasIndexKey(ownerID, term), alias)
		ai.addMappingLocked(aliasIndexKey(ownerID, alias), term)
	}
}

// Rebuild loads explicit alias:<term>=<alias> tags and entities.
// Tag co-occurrence is not a synonym.
func (ai *AliasIndex) Rebuild(entries []Entry) {
	ai.mu.Lock()
	defer ai.mu.Unlock()

	ai.aliases = make(map[string][]string)
	ai.order = make([]string, 0, aliasCapacity)

	for _, entry := range entries {
		if !entry.IsActive() {
			continue
		}
		ownerID := strings.TrimSpace(entry.OwnerID)
		fields := make([]string, 0, len(entry.Tags)+len(entry.Entities))
		fields = append(fields, entry.Tags...)
		fields = append(fields, entry.Entities...)
		for _, field := range fields {
			term, alias, ok := parseExplicitAliasTag(field)
			if !ok {
				continue
			}
			ai.addMappingLocked(aliasIndexKey(ownerID, term), alias)
			ai.addMappingLocked(aliasIndexKey(ownerID, alias), term)
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
