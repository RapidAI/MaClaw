package memory

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"pgregory.net/rapid"
)

// stubEmitter is a minimal EventEmitter for testing.
type stubEmitter struct {
	events []string
}

func (e *stubEmitter) Emit(eventType string, payload interface{}) {
	e.events = append(e.events, eventType)
}

func (e *stubEmitter) Subscribe(eventType string, handler corelib.EventHandler) {}

// Feature: memory-claude-style-upgrade, Property 11: Pinned and protected entries survive eviction and GC
// **Validates: Requirements 4.2, 4.3, 5.2**
//
// Pinned and self_identity entries survive eviction and GC — they are never
// removed from active memory by either mechanism.
func TestProperty_ProtectedSurviveEviction(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(rt *rapid.T) {
		storePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{8}`).Draw(rt, "fname")+".json")
		store, err := NewStore(storePath)
		if err != nil {
			rt.Fatal(err)
		}
		defer store.Stop()

		emitter := &stubEmitter{}
		comp := NewCompressor(store, nil, emitter)
		comp.SetGCThreshold(20) // low threshold for testing

		// Add some pinned entries.
		nPinned := rapid.IntRange(1, 5).Draw(rt, "nPinned")
		pinnedIDs := make(map[string]bool)
		for i := 0; i < nPinned; i++ {
			e := Entry{
				Content:     rapid.StringMatching(`[a-zA-Z]{10,30}`).Draw(rt, "pinnedContent") + "_pinned",
				Category:    CategoryProjectKnowledge,
				AccessCount: 1,
			}
			if err := store.Save(e); err != nil {
				rt.Fatal(err)
			}
			store.mu.RLock()
			id := store.entries[len(store.entries)-1].ID
			store.mu.RUnlock()
			if err := store.PinEntry(id); err != nil {
				rt.Fatal(err)
			}
			pinnedIDs[id] = true
		}

		// Add some self_identity (protected) entries.
		nProtected := rapid.IntRange(1, 3).Draw(rt, "nProtected")
		protectedIDs := make(map[string]bool)
		for i := 0; i < nProtected; i++ {
			e := Entry{
				Content:     rapid.StringMatching(`[a-zA-Z]{10,30}`).Draw(rt, "protContent") + "_identity",
				Category:    CategorySelfIdentity,
				AccessCount: 1,
			}
			if err := store.Save(e); err != nil {
				rt.Fatal(err)
			}
			store.mu.RLock()
			id := store.entries[len(store.entries)-1].ID
			store.mu.RUnlock()
			protectedIDs[id] = true
		}

		// Fill with regular entries to exceed GC threshold.
		for i := 0; i < 25; i++ {
			e := Entry{
				Content:     rapid.StringMatching(`[a-zA-Z]{5,20}`).Draw(rt, "filler") + "_regular",
				Category:    CategoryProjectKnowledge,
				AccessCount: rapid.IntRange(1, 10).Draw(rt, "fillerAccess"),
			}
			if err := store.Save(e); err != nil {
				rt.Fatal(err)
			}
		}

		// Run GC.
		ctx := context.Background()
		_, err = comp.RunGC(ctx)
		if err != nil {
			rt.Fatal(err)
		}

		// Verify all pinned entries survive.
		store.mu.RLock()
		activeIDs := make(map[string]bool)
		for _, e := range store.entries {
			activeIDs[e.ID] = true
		}
		store.mu.RUnlock()

		for id := range pinnedIDs {
			if !activeIDs[id] {
				rt.Fatalf("pinned entry %q was evicted by GC", id)
			}
		}
		for id := range protectedIDs {
			if !activeIDs[id] {
				rt.Fatalf("protected (self_identity) entry %q was evicted by GC", id)
			}
		}

		// Also verify eviction: fill store to max and trigger evictLRU.
		for i := 0; i < 500; i++ {
			e := Entry{
				Content:     rapid.StringMatching(`[a-zA-Z]{5,15}`).Draw(rt, "evictFill") + "_bulk",
				Category:    CategoryProjectKnowledge,
				AccessCount: 1,
			}
			_ = store.Save(e)
		}

		store.mu.RLock()
		activeIDs2 := make(map[string]bool)
		for _, e := range store.entries {
			activeIDs2[e.ID] = true
		}
		store.mu.RUnlock()

		for id := range pinnedIDs {
			if !activeIDs2[id] {
				rt.Fatalf("pinned entry %q was evicted by LRU", id)
			}
		}
		for id := range protectedIDs {
			if !activeIDs2[id] {
				rt.Fatalf("protected entry %q was evicted by LRU", id)
			}
		}
	})
}

// Feature: memory-claude-style-upgrade, Property 12: Pinned entries are compression-immune
// **Validates: Requirements 4.3**
//
// Pinned entries' Content is byte-identical before and after compression
// (dedup, merge, compress all skip pinned entries).
func TestProperty_PinnedCompressionImmune(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(rt *rapid.T) {
		storePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{8}`).Draw(rt, "fname")+".json")
		store, err := NewStore(storePath)
		if err != nil {
			rt.Fatal(err)
		}
		defer store.Stop()

		comp := NewCompressor(store, nil, nil) // nil LLM — only dedup runs

		// Create entries with duplicate content — one pinned, one not.
		content := rapid.StringMatching(`[a-zA-Z ]{25,80}`).Draw(rt, "content")

		pinnedEntry := Entry{
			Content:     content,
			Category:    CategoryProjectKnowledge,
			AccessCount: 1,
		}
		if err := store.Save(pinnedEntry); err != nil {
			rt.Fatal(err)
		}
		store.mu.RLock()
		pinnedID := store.entries[len(store.entries)-1].ID
		store.mu.RUnlock()
		if err := store.PinEntry(pinnedID); err != nil {
			rt.Fatal(err)
		}

		// Save a duplicate (not pinned).
		dupEntry := Entry{
			Content:     content,
			Category:    CategoryProjectKnowledge,
			AccessCount: 5,
		}
		if err := store.Save(dupEntry); err != nil {
			// Save deduplicates by content — it updates the existing entry.
			// That's fine; the pinned entry's content should still be unchanged.
		}

		// Also add some unique pinned entries with long content.
		nPinned := rapid.IntRange(1, 3).Draw(rt, "nPinned")
		pinnedContents := make(map[string]string) // id -> content
		for i := 0; i < nPinned; i++ {
			longContent := rapid.StringMatching(`[a-zA-Z ]{200,400}`).Draw(rt, "longPinned")
			e := Entry{
				Content:     longContent,
				Category:    CategoryProjectKnowledge,
				AccessCount: 1,
			}
			if err := store.Save(e); err != nil {
				rt.Fatal(err)
			}
			store.mu.RLock()
			id := store.entries[len(store.entries)-1].ID
			store.mu.RUnlock()
			if err := store.PinEntry(id); err != nil {
				rt.Fatal(err)
			}
			pinnedContents[id] = longContent
		}

		// Record all pinned entries' content before compression.
		store.mu.RLock()
		beforeContents := make(map[string]string)
		for _, e := range store.entries {
			if e.Pinned {
				beforeContents[e.ID] = e.Content
			}
		}
		store.mu.RUnlock()

		// Run compression (dedup only since LLM is nil).
		ctx := context.Background()
		_, _ = comp.Compress(ctx)

		// Verify all pinned entries' content is unchanged.
		store.mu.RLock()
		for _, e := range store.entries {
			if before, ok := beforeContents[e.ID]; ok {
				if e.Content != before {
					rt.Fatalf("pinned entry %q content changed: %q -> %q", e.ID, before, e.Content)
				}
			}
		}
		store.mu.RUnlock()
	})
}

// Feature: memory-claude-style-upgrade, Property 15: GC triggers at threshold
// **Validates: Requirements 5.1**
//
// GC triggers when ActiveCount >= gcThreshold (default 450), not below.
func TestProperty_GCThreshold(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(rt *rapid.T) {
		storePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{8}`).Draw(rt, "fname")+".json")
		store, err := NewStore(storePath)
		if err != nil {
			rt.Fatal(err)
		}
		defer store.Stop()

		emitter := &stubEmitter{}
		comp := NewCompressor(store, nil, emitter)

		threshold := rapid.IntRange(10, 50).Draw(rt, "threshold")
		comp.SetGCThreshold(threshold)

		// Add entries below threshold.
		belowCount := rapid.IntRange(1, threshold-1).Draw(rt, "belowCount")
		for i := 0; i < belowCount; i++ {
			e := Entry{
				Content:     rapid.StringMatching(`[a-zA-Z]{5,20}`).Draw(rt, "below") + "_below",
				Category:    CategoryProjectKnowledge,
				AccessCount: 1,
			}
			if err := store.Save(e); err != nil {
				rt.Fatal(err)
			}
		}

		// Below threshold: maybeRunGC should NOT trigger GC.
		emitter.events = nil
		comp.maybeRunGC(context.Background())

		gcTriggered := false
		for _, ev := range emitter.events {
			if ev == "memory:gc" {
				gcTriggered = true
			}
		}
		if gcTriggered {
			rt.Fatalf("GC triggered with %d entries (below threshold %d)", belowCount, threshold)
		}

		// Add entries to reach or exceed threshold.
		for store.ActiveCount() < threshold {
			e := Entry{
				Content:     rapid.StringMatching(`[a-zA-Z]{5,20}`).Draw(rt, "fill") + "_fill",
				Category:    CategoryProjectKnowledge,
				AccessCount: 1,
			}
			if err := store.Save(e); err != nil {
				rt.Fatal(err)
			}
		}

		// At/above threshold: maybeRunGC should trigger GC.
		emitter.events = nil
		comp.maybeRunGC(context.Background())

		gcTriggered = false
		for _, ev := range emitter.events {
			if ev == "memory:gc" {
				gcTriggered = true
			}
		}
		if !gcTriggered {
			rt.Fatalf("GC NOT triggered with %d entries (at/above threshold %d)", store.ActiveCount(), threshold)
		}
	})
}

// Feature: memory-claude-style-upgrade, Property 16: GC archives instead of deleting
// **Validates: Requirements 5.3**
//
// Entries removed by GC appear in archive. Total count is preserved:
// (active after) + (newly archived) + (skipped pinned/protected already in active) = active before.
func TestProperty_GCArchives(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(rt *rapid.T) {
		storePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{8}`).Draw(rt, "fname")+".json")
		store, err := NewStore(storePath)
		if err != nil {
			rt.Fatal(err)
		}
		defer store.Stop()

		emitter := &stubEmitter{}
		comp := NewCompressor(store, nil, emitter)
		threshold := rapid.IntRange(15, 30).Draw(rt, "threshold")
		comp.SetGCThreshold(threshold)

		// Add some pinned entries.
		nPinned := rapid.IntRange(0, 3).Draw(rt, "nPinned")
		for i := 0; i < nPinned; i++ {
			e := Entry{
				Content:     rapid.StringMatching(`[a-zA-Z]{10,30}`).Draw(rt, "pinContent") + "_pin",
				Category:    CategoryProjectKnowledge,
				AccessCount: 1,
			}
			if err := store.Save(e); err != nil {
				rt.Fatal(err)
			}
			store.mu.RLock()
			id := store.entries[len(store.entries)-1].ID
			store.mu.RUnlock()
			_ = store.PinEntry(id)
		}

		// Fill above threshold with regular entries.
		totalTarget := threshold + rapid.IntRange(5, 15).Draw(rt, "excess")
		for store.ActiveCount() < totalTarget {
			e := Entry{
				Content:     rapid.StringMatching(`[a-zA-Z]{5,20}`).Draw(rt, "regular") + "_reg",
				Category:    CategoryProjectKnowledge,
				AccessCount: rapid.IntRange(1, 10).Draw(rt, "regAccess"),
			}
			if err := store.Save(e); err != nil {
				rt.Fatal(err)
			}
		}

		archiveBefore := store.archive.Count()
		activeBefore := store.ActiveCount()

		ctx := context.Background()
		result, err := comp.RunGC(ctx)
		if err != nil {
			rt.Fatal(err)
		}

		activeAfter := store.ActiveCount()
		archiveAfter := store.archive.Count()
		newlyArchived := archiveAfter - archiveBefore

		// The net archive growth = archived - revived (since revived entries are removed from archive).
		expectedNetGrowth := result.ArchivedCount - result.RevivedCount
		if newlyArchived != expectedNetGrowth {
			rt.Fatalf("archive net growth=%d but expected ArchivedCount(%d) - RevivedCount(%d) = %d",
				newlyArchived, result.ArchivedCount, result.RevivedCount, expectedNetGrowth)
		}

		// Total count preserved: active_after + archived - revived = active_before
		expectedActiveBefore := activeAfter + result.ArchivedCount - result.RevivedCount
		if expectedActiveBefore != activeBefore {
			rt.Fatalf("count mismatch: activeAfter(%d) + archived(%d) - revived(%d) = %d, expected activeBefore=%d",
				activeAfter, result.ArchivedCount, result.RevivedCount, expectedActiveBefore, activeBefore)
		}
	})
}

// Feature: memory-claude-style-upgrade, Property 17: Archive revival limit
// **Validates: Requirements 5.5**
//
// At most 10 entries revived per GC cycle.
func TestProperty_RevivalLimit(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(rt *rapid.T) {
		storePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{8}`).Draw(rt, "fname")+".json")
		store, err := NewStore(storePath)
		if err != nil {
			rt.Fatal(err)
		}
		defer store.Stop()

		emitter := &stubEmitter{}
		comp := NewCompressor(store, nil, emitter)
		comp.SetGCThreshold(20)

		// Pre-populate archive with many entries that share tags with active entries.
		sharedTag := "shared-topic"
		nArchived := rapid.IntRange(15, 30).Draw(rt, "nArchived")
		archiveEntries := make([]Entry, nArchived)
		for i := 0; i < nArchived; i++ {
			archiveEntries[i] = Entry{
				ID:          generateID(),
				Content:     rapid.StringMatching(`[a-zA-Z]{10,30}`).Draw(rt, "archContent"),
				Category:    CategoryProjectKnowledge,
				Tags:        []string{sharedTag},
				CreatedAt:   time.Now().Add(-time.Duration(i) * time.Hour),
				UpdatedAt:   time.Now().Add(-time.Duration(i) * time.Hour),
				AccessCount: 1,
				Strength:    1.0,
			}
		}
		if err := store.archive.Add(archiveEntries...); err != nil {
			rt.Fatal(err)
		}

		// Add active entries above threshold, all with the shared tag.
		for i := 0; i < 25; i++ {
			e := Entry{
				Content:     rapid.StringMatching(`[a-zA-Z]{5,20}`).Draw(rt, "active") + "_active",
				Category:    CategoryProjectKnowledge,
				Tags:        []string{sharedTag},
				AccessCount: rapid.IntRange(1, 50).Draw(rt, "activeAccess"),
			}
			if err := store.Save(e); err != nil {
				rt.Fatal(err)
			}
		}

		ctx := context.Background()
		result, err := comp.RunGC(ctx)
		if err != nil {
			rt.Fatal(err)
		}

		if result.RevivedCount > 10 {
			rt.Fatalf("revived %d entries, expected at most 10", result.RevivedCount)
		}
	})
}

func TestRunGCSynchronizesSQLiteArchiveTombstonesAndRevival(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	backend := newTestSQLiteBackend(t)
	store.SetBackend(backend, SyncConfig{})

	now := time.Now()
	active := []Entry{
		{ID: "gc-low-0", Content: "low zero task artifact", Category: CategoryTaskArtifact, Tags: []string{"cold"}, AccessCount: 1, Strength: 1, CreatedAt: now.Add(-6 * time.Hour), UpdatedAt: now.Add(-6 * time.Hour)},
		{ID: "gc-low-1", Content: "low one task artifact", Category: CategoryTaskArtifact, Tags: []string{"cold"}, AccessCount: 1, Strength: 1, CreatedAt: now.Add(-5 * time.Hour), UpdatedAt: now.Add(-5 * time.Hour)},
		{ID: "gc-keep-0", Content: "keep zero project topic", Category: CategoryProjectKnowledge, Tags: []string{"hot"}, AccessCount: 20, Strength: 1, CreatedAt: now.Add(-4 * time.Hour), UpdatedAt: now.Add(-4 * time.Hour)},
		{ID: "gc-keep-1", Content: "keep one project topic", Category: CategoryProjectKnowledge, Tags: []string{"hot"}, AccessCount: 19, Strength: 1, CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour)},
		{ID: "gc-keep-2", Content: "keep two project topic", Category: CategoryProjectKnowledge, Tags: []string{"hot"}, AccessCount: 18, Strength: 1, CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour)},
		{ID: "gc-keep-3", Content: "keep three project topic", Category: CategoryProjectKnowledge, Tags: []string{"hot"}, AccessCount: 17, Strength: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)},
	}
	if err := store.UpsertEntriesByID(active); err != nil {
		t.Fatalf("UpsertEntriesByID active: %v", err)
	}
	if err := store.archive.Add(Entry{ID: "gc-revive-0", Content: "revived project topic", Category: CategoryProjectKnowledge, Tags: []string{"hot"}, AccessCount: 1, Strength: 1, CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now.Add(-24 * time.Hour)}); err != nil {
		t.Fatalf("archive add: %v", err)
	}

	comp := NewCompressor(store, nil, nil)
	comp.SetGCThreshold(4)
	result, err := comp.RunGC(context.Background())
	if err != nil {
		t.Fatalf("RunGC: %v", err)
	}
	if result.ArchivedCount != 2 {
		t.Fatalf("ArchivedCount=%d, want 2", result.ArchivedCount)
	}
	if result.RevivedCount != 1 {
		t.Fatalf("RevivedCount=%d, want 1", result.RevivedCount)
	}

	loaded, err := backend.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	seen := make(map[string]bool, len(loaded))
	for _, entry := range loaded {
		seen[entry.ID] = true
	}
	for _, id := range []string{"gc-low-0", "gc-low-1"} {
		if seen[id] {
			t.Fatalf("archived entry %s should be tombstoned from active SQLite rows", id)
		}
	}
	if !seen["gc-revive-0"] {
		t.Fatal("revived archive entry should be restored through SQLite batch upsert")
	}

	_, deleted, err := backend.Since(0)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	deletedSet := make(map[string]bool, len(deleted))
	for _, id := range deleted {
		deletedSet[id] = true
	}
	for _, id := range []string{"gc-low-0", "gc-low-1"} {
		if !deletedSet[id] {
			t.Fatalf("archived entry %s should appear in SQLite sync tombstones: %v", id, deleted)
		}
	}
}
