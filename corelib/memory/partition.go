package memory

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/corelib/fileutil"
)

// partitionGroup defines a named group of categories that share a partition file.
type partitionGroup struct {
	Name       string     // e.g. "identity", "user", "project"
	Categories []Category // categories in this group
	FileName   string     // e.g. "identity.json"
}

// partitionGroups defines the category-to-file mapping.
// Claude-style categories are mapped to canonical before grouping.
var partitionGroups = []partitionGroup{
	{Name: "identity", Categories: []Category{CategorySelfIdentity}, FileName: "part_identity.json"},
	{Name: "user", Categories: []Category{CategoryUserFact, CategoryUser, CategoryPreference, CategoryInstruction, CategoryFeedback}, FileName: "part_user.json"},
	{Name: "project", Categories: []Category{CategoryProjectKnowledge, CategoryProject, CategoryReference, CategoryTaskArtifact}, FileName: "part_project.json"},
	{Name: "episodic", Categories: []Category{CategoryConversationSummary, CategorySessionCheckpoint}, FileName: "part_episodic.json"},
	{Name: "profile", Categories: []Category{CategoryProfile}, FileName: "part_profile.json"},
}

// partition holds entries for a group of categories with independent dirty tracking.
type partition struct {
	group   partitionGroup
	path    string // full file path
	dirty   bool
	indices []int // indices into Store.entries for entries in this partition
}

// partitionManager manages category-based partitions for incremental persistence.
type partitionManager struct {
	dir        string
	partitions []*partition
	catToGroup map[Category]int // category → partition index
	enabled    bool             // false until migration is complete
}

func newPartitionManager(storeDir string) *partitionManager {
	pm := &partitionManager{
		dir:        storeDir,
		catToGroup: make(map[Category]int),
	}
	for i, g := range partitionGroups {
		p := &partition{
			group: g,
			path:  filepath.Join(storeDir, g.FileName),
		}
		pm.partitions = append(pm.partitions, p)
		for _, cat := range g.Categories {
			pm.catToGroup[cat] = i
		}
	}
	return pm
}

// partitionIndexFor returns the partition index for a category.
// Returns -1 for unknown categories (they go to the "project" partition as fallback).
func (pm *partitionManager) partitionIndexFor(cat Category) int {
	if idx, ok := pm.catToGroup[cat]; ok {
		return idx
	}
	// Fallback: map to canonical and try again.
	canonical := MapToCanonical(cat)
	if idx, ok := pm.catToGroup[canonical]; ok {
		return idx
	}
	// Default to "project" partition (index 2).
	return 2
}

// markAllDirty marks all partitions as dirty (used by flush).
func (pm *partitionManager) markAllDirty() {
	for _, p := range pm.partitions {
		p.dirty = true
	}
}

// rebuildIndices rebuilds the partition index mapping from the entries slice.
// Must be called after any operation that changes the entries slice (save, delete, evict).
func (pm *partitionManager) rebuildIndices(entries []Entry) {
	for _, p := range pm.partitions {
		p.indices = p.indices[:0]
	}
	for i, e := range entries {
		idx := pm.partitionIndexFor(e.Category)
		pm.partitions[idx].indices = append(pm.partitions[idx].indices, i)
	}
}

// flushDirty writes only dirty partitions to disk. Returns the number of
// partitions written and any error from the first failure.
func (pm *partitionManager) flushDirty(entries []Entry) (int, error) {
	if !pm.enabled {
		return 0, nil
	}
	pm.rebuildIndices(entries)
	written := 0
	for _, p := range pm.partitions {
		if !p.dirty {
			continue
		}
		partEntries := make([]Entry, 0, len(p.indices))
		for _, idx := range p.indices {
			partEntries = append(partEntries, entries[idx])
		}
		data, err := json.MarshalIndent(partEntries, "", "  ")
		if err != nil {
			return written, fmt.Errorf("partition %s: marshal: %w", p.group.Name, err)
		}
		if err := fileutil.AtomicWriteFile(p.path, data, 0o644); err != nil {
			return written, fmt.Errorf("partition %s: write: %w", p.group.Name, err)
		}
		p.dirty = false
		written++
	}
	return written, nil
}

// loadPartitions loads entries from partition files. Returns all loaded entries.
// If no partition files exist, returns nil (caller should fall back to legacy file).
func (pm *partitionManager) loadPartitions() ([]Entry, bool) {
	// Check if any partition file exists.
	anyExists := false
	for _, p := range pm.partitions {
		if _, err := os.Stat(p.path); err == nil {
			anyExists = true
			break
		}
	}
	if !anyExists {
		return nil, false
	}

	var all []Entry
	for _, p := range pm.partitions {
		data, err := os.ReadFile(p.path)
		if err != nil {
			if os.IsNotExist(err) {
				continue // empty partition
			}
			log.Printf("[partition] WARNING: failed to read %s: %v, skipping", p.path, err)
			continue
		}
		if len(data) == 0 {
			continue
		}
		var entries []Entry
		if err := json.Unmarshal(data, &entries); err != nil {
			log.Printf("[partition] WARNING: corrupted %s, skipping: %v", p.path, err)
			continue
		}
		all = append(all, entries...)
	}
	return all, true
}

// migrateFromLegacy splits entries from the legacy single file into partition
// files. Called once during the first load when partition files don't exist
// but the legacy file does.
func (pm *partitionManager) migrateFromLegacy(entries []Entry, legacyPath string) error {
	if len(entries) == 0 {
		return nil
	}

	// Ensure partition directory exists.
	if err := os.MkdirAll(pm.dir, 0o755); err != nil {
		return fmt.Errorf("partition migrate: mkdir: %w", err)
	}

	// Group entries by partition.
	grouped := make(map[int][]Entry)
	for _, e := range entries {
		idx := pm.partitionIndexFor(e.Category)
		grouped[idx] = append(grouped[idx], e)
	}

	// Write each partition.
	for idx, partEntries := range grouped {
		p := pm.partitions[idx]
		data, err := json.MarshalIndent(partEntries, "", "  ")
		if err != nil {
			return fmt.Errorf("partition migrate %s: marshal: %w", p.group.Name, err)
		}
		if err := fileutil.AtomicWriteFile(p.path, data, 0o644); err != nil {
			return fmt.Errorf("partition migrate %s: write: %w", p.group.Name, err)
		}
	}

	// Rename legacy file to indicate migration is complete.
	migratedPath := legacyPath + ".migrated"
	if err := os.Rename(legacyPath, migratedPath); err != nil {
		// Non-fatal: partition files are already written.
		log.Printf("[partition] WARNING: failed to rename legacy file: %v", err)
	} else {
		log.Printf("[partition] migrated %d entries from %s to %d partition files, legacy renamed to %s",
			len(entries), filepath.Base(legacyPath), len(grouped), filepath.Base(migratedPath))
	}

	pm.enabled = true
	return nil
}

// partitionNameFor returns the partition group name for a category (for logging/testing).
func (pm *partitionManager) partitionNameFor(cat Category) string {
	idx := pm.partitionIndexFor(cat)
	return pm.partitions[idx].group.Name
}

// isEnabled returns whether partitioned storage is active.
func (pm *partitionManager) isEnabled() bool {
	return pm.enabled
}

// enable activates partitioned storage.
func (pm *partitionManager) enable() {
	pm.enabled = true
}
