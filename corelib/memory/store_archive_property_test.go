package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// genEntry generates a random Entry with the given content and category.
func genEntry(t *rapid.T, suffix string) Entry {
	content := rapid.StringMatching(`[a-zA-Z0-9 ]{5,80}`).Draw(t, "content"+suffix)
	cat := genCategory().Draw(t, "cat"+suffix)
	nTags := rapid.IntRange(0, 3).Draw(t, "nTags"+suffix)
	tags := make([]string, nTags)
	for i := range tags {
		tags[i] = rapid.StringMatching(`[a-z]{2,8}`).Draw(t, "tag"+suffix+string(rune('0'+i)))
	}
	return Entry{
		ID:          generateID(),
		Content:     content,
		Category:    cat,
		Tags:        tags,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		AccessCount: rapid.IntRange(1, 100).Draw(t, "access"+suffix),
		Strength:    1.0,
		Scope:       InferScope(cat),
	}
}

// Feature: memory-claude-style-upgrade, Property 5: LRU eviction archives instead of deleting
// **Validates: Requirements 3.1**
//
// At max capacity, saving a new entry causes evicted entry to appear
// in archive and no longer be in active memory.
func TestProperty_LRUArchives(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(rt *rapid.T) {
		storePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{8}`).Draw(rt, "fname")+".json")
		store, err := NewStore(storePath)
		if err != nil {
			rt.Fatal(err)
		}
		defer store.Stop()

		// Temporarily lower maxItems for test performance.
		store.mu.Lock()
		store.maxItems = 500
		store.mu.Unlock()

		// Fill store to max capacity.
		for i := 0; i < 500; i++ {
			e := Entry{
				Content:     rapid.StringMatching(`[a-zA-Z]{5,20}`).Draw(rt, "fill") + string(rune(i%26+'a')),
				Category:    CategoryProjectKnowledge,
				AccessCount: rapid.IntRange(1, 50).Draw(rt, "fillAccess"),
			}
			if err := store.Save(e); err != nil {
				rt.Fatal(err)
			}
		}

		store.mu.RLock()
		countBefore := len(store.entries)
		store.mu.RUnlock()

		if countBefore != 500 {
			rt.Fatalf("expected 500 entries before new save, got %d", countBefore)
		}

		archiveCountBefore := store.archive.Count()

		// Save one more entry to trigger eviction.
		newEntry := Entry{
			Content:  "trigger-eviction-" + rapid.StringMatching(`[a-z]{5}`).Draw(rt, "trigger"),
			Category: CategoryProjectKnowledge,
		}
		if err := store.Save(newEntry); err != nil {
			rt.Fatal(err)
		}

		store.mu.RLock()
		countAfter := len(store.entries)
		store.mu.RUnlock()

		archiveCountAfter := store.archive.Count()

		// Active memory should be at or below max.
		if countAfter > 500 {
			rt.Fatalf("active memory exceeded max: %d", countAfter)
		}

		// Archive should have gained at least one entry.
		if archiveCountAfter <= archiveCountBefore {
			rt.Fatalf("archive count did not increase: before=%d, after=%d", archiveCountBefore, archiveCountAfter)
		}
	})
}

// Feature: memory-claude-style-upgrade, Property 6: Archive serialization round-trip
// **Validates: Requirements 3.2, 3.7**
//
// Add entries to archive, flush to disk, reload — entries preserved.
func TestProperty_ArchiveRoundTrip(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(rt *rapid.T) {
		archivePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{8}`).Draw(rt, "fname")+".json")
		_ = os.Remove(archivePath)

		archive, err := NewArchiveStore(archivePath)
		if err != nil {
			rt.Fatal(err)
		}

		n := rapid.IntRange(1, 20).Draw(rt, "n")
		original := make([]Entry, n)
		for i := 0; i < n; i++ {
			original[i] = genEntry(rt, string(rune('A'+i%26)))
		}

		if err := archive.Add(original...); err != nil {
			rt.Fatal(err)
		}

		// Flush to disk.
		if err := archive.Flush(); err != nil {
			rt.Fatal(err)
		}
		archive.Stop()

		// Reload from disk.
		archive2, err := NewArchiveStore(archivePath)
		if err != nil {
			rt.Fatal(err)
		}
		defer archive2.Stop()

		archive2.mu.RLock()
		loaded := archive2.entries
		archive2.mu.RUnlock()

		if len(loaded) != n {
			rt.Fatalf("expected %d entries after reload, got %d", n, len(loaded))
		}

		// Verify all original entries are present by ID.
		idSet := make(map[string]bool, n)
		for _, e := range loaded {
			idSet[e.ID] = true
		}
		for _, e := range original {
			if !idSet[e.ID] {
				rt.Fatalf("entry %q missing after reload", e.ID)
			}
		}

		// Verify field preservation via JSON round-trip comparison.
		for _, orig := range original {
			for _, le := range loaded {
				if orig.ID == le.ID {
					origJSON, _ := json.Marshal(orig)
					loadedJSON, _ := json.Marshal(le)
					if string(origJSON) != string(loadedJSON) {
						rt.Fatalf("entry %q fields changed after round-trip", orig.ID)
					}
					break
				}
			}
		}
	})
}

// Feature: memory-claude-style-upgrade, Property 7: Archive capacity invariant
// **Validates: Requirements 3.3**
//
// Archive never exceeds 1000 entries, oldest evicted first.
func TestProperty_ArchiveCapacity(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(rt *rapid.T) {
		archivePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{8}`).Draw(rt, "fname")+".json")

		archive, err := NewArchiveStore(archivePath)
		if err != nil {
			rt.Fatal(err)
		}
		defer archive.Stop()

		// Add a batch that exceeds capacity.
		total := rapid.IntRange(1000, 1200).Draw(rt, "total")
		entries := make([]Entry, total)
		baseTime := time.Now().Add(-time.Duration(total) * time.Minute)
		for i := 0; i < total; i++ {
			entries[i] = Entry{
				ID:        generateID(),
				Content:   rapid.StringMatching(`[a-z]{5,20}`).Draw(rt, "cap") + string(rune(i%26+'a')),
				Category:  CategoryProjectKnowledge,
				UpdatedAt: baseTime.Add(time.Duration(i) * time.Minute),
				CreatedAt: baseTime.Add(time.Duration(i) * time.Minute),
			}
		}

		if err := archive.Add(entries...); err != nil {
			rt.Fatal(err)
		}

		count := archive.Count()
		if count > 1000 {
			rt.Fatalf("archive exceeded max capacity: %d > 1000", count)
		}

		// Verify the oldest entries were evicted.
		archive.mu.RLock()
		remaining := archive.entries
		archive.mu.RUnlock()

		if len(remaining) > 0 {
			oldestRemaining := remaining[0].UpdatedAt
			for _, e := range remaining[1:] {
				if e.UpdatedAt.Before(oldestRemaining) {
					oldestRemaining = e.UpdatedAt
				}
			}
			cutoffIdx := total - 1000
			if cutoffIdx > 0 {
				lastEvicted := entries[cutoffIdx-1].UpdatedAt
				if lastEvicted.After(oldestRemaining) {
					rt.Fatalf("eviction order wrong: evicted entry (%v) is newer than remaining (%v)",
						lastEvicted, oldestRemaining)
				}
			}
		}
	})
}

// Feature: memory-claude-style-upgrade, Property 8: Archive list filtering consistency
// **Validates: Requirements 3.4**
//
// ListArchive returns correct subset matching category/keyword.
func TestProperty_ArchiveListFilter(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(rt *rapid.T) {
		archivePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{8}`).Draw(rt, "fname")+".json")

		archive, err := NewArchiveStore(archivePath)
		if err != nil {
			rt.Fatal(err)
		}
		defer archive.Stop()

		n := rapid.IntRange(5, 30).Draw(rt, "n")
		entries := make([]Entry, n)
		for i := 0; i < n; i++ {
			entries[i] = genEntry(rt, string(rune('A'+i%26)))
		}
		if err := archive.Add(entries...); err != nil {
			rt.Fatal(err)
		}

		// Pick a random category filter.
		filterCat := genCategory().Draw(rt, "filterCat")
		// Pick a random keyword from one of the entries' content.
		filterKeyword := ""
		if rapid.Bool().Draw(rt, "useKeyword") {
			idx := rapid.IntRange(0, n-1).Draw(rt, "kwIdx")
			content := entries[idx].Content
			if len(content) > 3 {
				filterKeyword = strings.ToLower(content[:3])
			}
		}

		result := archive.List(filterCat, filterKeyword)

		// Verify: every returned entry matches the filter.
		kw := strings.ToLower(filterKeyword)
		for _, e := range result {
			if filterCat != "" && e.Category != filterCat {
				rt.Fatalf("returned entry category %q doesn't match filter %q", e.Category, filterCat)
			}
			if kw != "" && !containsKeyword(e, kw) {
				rt.Fatalf("returned entry doesn't contain keyword %q", kw)
			}
		}

		// Verify: no matching entry was missed.
		archive.mu.RLock()
		allEntries := archive.entries
		archive.mu.RUnlock()

		expectedCount := 0
		for _, e := range allEntries {
			if filterCat != "" && e.Category != filterCat {
				continue
			}
			if kw != "" && !containsKeyword(e, kw) {
				continue
			}
			expectedCount++
		}
		if len(result) != expectedCount {
			rt.Fatalf("expected %d matching entries, got %d", expectedCount, len(result))
		}
	})
}

// Feature: memory-claude-style-upgrade, Property 9: Restore from archive round-trip
// **Validates: Requirements 3.5, 3.6, 5.6**
//
// Restore removes from archive, adds to active, UpdatedAt~now, AccessCount=1.
func TestProperty_RestoreRoundTrip(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(rt *rapid.T) {
		storePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{8}`).Draw(rt, "fname")+".json")
		store, err := NewStore(storePath)
		if err != nil {
			rt.Fatal(err)
		}
		defer store.Stop()

		// Create an entry and add it directly to the archive.
		entry := genEntry(rt, "R")
		entry.AccessCount = rapid.IntRange(5, 100).Draw(rt, "oldAccess")
		entry.UpdatedAt = time.Now().Add(-24 * time.Hour) // old timestamp

		if err := store.archive.Add(entry); err != nil {
			rt.Fatal(err)
		}

		archiveCountBefore := store.archive.Count()
		beforeRestore := time.Now()

		// Restore the entry.
		if err := store.RestoreFromArchive(entry.ID); err != nil {
			rt.Fatal(err)
		}

		afterRestore := time.Now()

		// Entry should be removed from archive.
		archiveCountAfter := store.archive.Count()
		if archiveCountAfter != archiveCountBefore-1 {
			rt.Fatalf("archive count: expected %d, got %d", archiveCountBefore-1, archiveCountAfter)
		}

		// Entry should be in active memory.
		store.mu.RLock()
		var found *Entry
		for i := range store.entries {
			if store.entries[i].ID == entry.ID {
				found = &store.entries[i]
				break
			}
		}
		store.mu.RUnlock()

		if found == nil {
			rt.Fatal("restored entry not found in active memory")
		}

		// AccessCount should be 1.
		if found.AccessCount != 1 {
			rt.Fatalf("expected AccessCount=1, got %d", found.AccessCount)
		}

		// UpdatedAt should be approximately now (within 1 second).
		if found.UpdatedAt.Before(beforeRestore) || found.UpdatedAt.After(afterRestore.Add(time.Second)) {
			rt.Fatalf("UpdatedAt %v not within expected range [%v, %v]",
				found.UpdatedAt, beforeRestore, afterRestore.Add(time.Second))
		}

		// Content should be preserved.
		if found.Content != entry.Content {
			rt.Fatalf("content mismatch: %q != %q", found.Content, entry.Content)
		}

		// Category should be preserved.
		if found.Category != entry.Category {
			rt.Fatalf("category mismatch: %q != %q", found.Category, entry.Category)
		}
	})
}
