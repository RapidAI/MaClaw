package memory

// entity_index.go maintains a mapping from entity names to entry IDs,
// enabling entity-centric queries like "find all facts about Alice".
// Inspired by Graphiti's Semantic Entity Subgraph and Mem0^g's entity nodes.
//
// This is Phase B of the entity-relation improvement plan:
// - Phase A (done): entity triples stored as Entry.Entities tags
// - Phase B (this file): entity name → entry ID index for fast lookup
// - Phase C (future): full entity-relation graph with typed edges

import (
	"sort"
	"strings"
	"sync"
)

// EntityIndex maps entity names to the entry IDs that mention them.
// Thread-safe for concurrent read/write.
type EntityIndex struct {
	mu      sync.RWMutex
	byName  map[string][]string // normalized entity name → []entryID
	byEntry map[string][]string // entryID → []entity names
}

// NewEntityIndex creates an empty EntityIndex.
func NewEntityIndex() *EntityIndex {
	return &EntityIndex{
		byName:  make(map[string][]string),
		byEntry: make(map[string][]string),
	}
}

// IndexEntry adds an entry's entities to the index.
func (ei *EntityIndex) IndexEntry(e *Entry) {
	if e == nil || len(e.Entities) == 0 {
		return
	}

	ei.mu.Lock()
	defer ei.mu.Unlock()

	// Remove old mappings for this entry.
	if oldNames, ok := ei.byEntry[e.ID]; ok {
		for _, name := range oldNames {
			ei.removeFromSlice(name, e.ID)
		}
	}

	// Add new mappings.
	var names []string
	for _, ent := range e.Entities {
		if strings.HasPrefix(ent, "entity:") {
			name := normalizeEntityName(strings.TrimPrefix(ent, "entity:"))
			if name == "" {
				continue
			}
			names = append(names, name)
			ei.byName[name] = appendUnique(ei.byName[name], e.ID)
		}
	}
	ei.byEntry[e.ID] = names
}

// RemoveEntry removes an entry from the index.
func (ei *EntityIndex) RemoveEntry(entryID string) {
	ei.mu.Lock()
	defer ei.mu.Unlock()

	if names, ok := ei.byEntry[entryID]; ok {
		for _, name := range names {
			ei.removeFromSlice(name, entryID)
		}
		delete(ei.byEntry, entryID)
	}
}

// FindByEntity returns all entry IDs that mention the given entity name.
func (ei *EntityIndex) FindByEntity(entityName string) []string {
	ei.mu.RLock()
	defer ei.mu.RUnlock()

	name := normalizeEntityName(entityName)
	ids := ei.byName[name]
	result := make([]string, len(ids))
	copy(result, ids)
	return result
}

// FindRelatedEntities returns entities that co-occur with the given entity
// in the same entries. This enables simple multi-hop reasoning:
// "Alice" → entries mentioning Alice → other entities in those entries.
func (ei *EntityIndex) FindRelatedEntities(entityName string) []string {
	ei.mu.RLock()
	defer ei.mu.RUnlock()

	name := normalizeEntityName(entityName)
	entryIDs := ei.byName[name]
	if len(entryIDs) == 0 {
		return nil
	}

	// Collect all entities from entries that mention the target entity.
	related := make(map[string]int) // entity name → co-occurrence count
	for _, eid := range entryIDs {
		for _, ename := range ei.byEntry[eid] {
			if ename != name {
				related[ename]++
			}
		}
	}

	// Sort by co-occurrence count descending.
	type entityCount struct {
		name  string
		count int
	}
	var sorted []entityCount
	for n, c := range related {
		sorted = append(sorted, entityCount{name: n, count: c})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].count > sorted[j].count
	})

	result := make([]string, 0, len(sorted))
	for _, ec := range sorted {
		result = append(result, ec.name)
	}
	return result
}

// Rebuild reconstructs the index from a slice of entries.
func (ei *EntityIndex) Rebuild(entries []Entry) {
	ei.mu.Lock()
	defer ei.mu.Unlock()

	ei.byName = make(map[string][]string, len(entries)/2)
	ei.byEntry = make(map[string][]string, len(entries)/2)

	for i := range entries {
		e := &entries[i]
		if len(e.Entities) == 0 {
			continue
		}
		var names []string
		for _, ent := range e.Entities {
			if strings.HasPrefix(ent, "entity:") {
				name := normalizeEntityName(strings.TrimPrefix(ent, "entity:"))
				if name == "" {
					continue
				}
				names = append(names, name)
				ei.byName[name] = appendUnique(ei.byName[name], e.ID)
			}
		}
		ei.byEntry[e.ID] = names
	}
}

// Stats returns the number of unique entities and indexed entries.
func (ei *EntityIndex) Stats() (entities int, entries int) {
	ei.mu.RLock()
	defer ei.mu.RUnlock()
	return len(ei.byName), len(ei.byEntry)
}

// removeFromSlice removes entryID from the byName[name] slice.
// Caller must hold ei.mu.Lock.
func (ei *EntityIndex) removeFromSlice(name, entryID string) {
	ids := ei.byName[name]
	for i, id := range ids {
		if id == entryID {
			ei.byName[name] = append(ids[:i], ids[i+1:]...)
			if len(ei.byName[name]) == 0 {
				delete(ei.byName, name)
			}
			return
		}
	}
}

func normalizeEntityName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}
