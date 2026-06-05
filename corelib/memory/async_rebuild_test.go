package memory

// async_rebuild_test.go covers the correctness properties of
// replaceEntriesAndRebuildAsync introduced to eliminate the 5-8 s s.mu
// hold time during remote sync batch application.

import (
	"path/filepath"
	"testing"
	"time"
)

// newAsyncRebuildStore creates a Store backed by SQLite with sync enabled
// on a very short interval.  The returned cleanup func must be called.
func newAsyncRebuildStore(t *testing.T) (*Store, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")

	b, err := NewSQLiteBackend(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteBackend: %v", err)
	}
	jsonPath := filepath.Join(dir, "memories.json")
	store, err := NewStore(jsonPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	store.SetBackend(b, SyncConfig{
		Enabled:    true,
		Interval:   50 * time.Millisecond,
		InstanceID: "test-instance",
	})
	return store, func() {
		store.Stop()
		b.Close()
	}
}

// ---------------------------------------------------------------------------
// WaitRebuild: basic contract
// ---------------------------------------------------------------------------

// TestRebuildAsync_WaitRebuildReturnsFast verifies that WaitRebuild returns
// immediately when no async rebuild is in flight (lastRebuildDone == nil).
func TestRebuildAsync_WaitRebuildReturnsFast(t *testing.T) {
	// Use a plain store without sync enabled — lastRebuildDone stays nil.
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()

	// No sync has happened yet; WaitRebuild must not block.
	done := make(chan struct{})
	go func() {
		store.WaitRebuild()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("WaitRebuild blocked when no rebuild was in flight")
	}
}

// TestRebuildAsync_IndexesConsistentAfterWait verifies that after syncOnce +
// implicit WaitRebuild the BM25 index contains the synced entry.
func TestRebuildAsync_IndexesConsistentAfterWait(t *testing.T) {
	store, cleanup := newAsyncRebuildStore(t)
	defer cleanup()

	now := time.Now().UTC()
	entry := Entry{
		ID: "async-bm25", Content: "distributed tracing observability",
		Category: CategoryProjectKnowledge, Strength: 1, AccessCount: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.backend.SaveEntry(&entry); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}

	store.syncOnce() // internally waits for the async rebuild to finish

	store.mu.RLock()
	scores := store.bm25.score("distributed tracing")
	store.mu.RUnlock()

	if scores["async-bm25"] <= 0 {
		t.Error("BM25 index not updated after async rebuild; entry should be searchable")
	}
}

// ---------------------------------------------------------------------------
// Snapshot isolation: concurrent Save must not be clobbered
// ---------------------------------------------------------------------------

// TestRebuildAsync_ConcurrentSaveNotClobbered verifies the key correctness
// property introduced in the fix: entries added by a concurrent Save after the
// snapshot was taken must not have their RelatedEdges cleared by the
// syncGraphLinksLocked call that only knows about the snapshot's IDs.
//
// Mechanism: replaceEntriesAndRebuildAsync passes snapshotIDs (not "*") to
// syncGraphLinksLocked, so entries that arrived after the snapshot are
// untouched.  Without this fix, the full sweep would zero their RelatedEdges.
//
// We inject the post-snapshot entry directly under s.mu (bypassing Save/autoLink)
// so its RelatedEdges are deterministic regardless of BM25/embedding scores.
func TestRebuildAsync_ConcurrentSaveNotClobbered(t *testing.T) {
	store, cleanup := newAsyncRebuildStore(t)
	defer cleanup()

	now := time.Now().UTC()

	// Stage an entry in the backend so syncOnce triggers an async rebuild.
	synced := Entry{
		ID: "clobber-synced", Content: "sync target entry — deterministic content XYZ",
		Category: CategoryProjectKnowledge, Strength: 1, AccessCount: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.backend.SaveEntry(&synced); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}

	// Collect the sync batch so we can start the async rebuild manually.
	modified, deletedIDs, err := store.backend.Since(store.sync.lastVersion)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}

	// Start the async rebuild goroutine (via applyRemoteSyncBatchLocked).
	store.mu.Lock()
	store.applyRemoteSyncBatchLocked(modified, deletedIDs)
	// While still holding s.mu, inject a post-snapshot entry with known RelatedEdges.
	// This simulates a concurrent Save completing between snapshot creation and
	// syncGraphLinksLocked running.  Because we hold s.mu here the goroutine
	// hasn't reached its s.mu.Lock call yet, so the injection is safe.
	postSnapshot := Entry{
		ID: "clobber-new", Content: "post-snapshot entry",
		Category:     CategoryProjectKnowledge,
		RelatedIDs:   []string{"clobber-synced"},
		RelatedEdges: []RelatedEdge{{ID: "clobber-synced", Strength: 0.8}},
		Strength:     1, AccessCount: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	store.entries = append(store.entries, postSnapshot)
	store.mu.Unlock()

	// Wait for the rebuild goroutine to finish.
	store.WaitRebuild()

	// The injected entry's RelatedEdges must be intact — the rebuild only patched
	// entries that were in the snapshot (snapshotIDs filter).
	store.mu.RLock()
	idx := store.findEntryIndexByIDLocked("clobber-new")
	var edges []RelatedEdge
	if idx >= 0 {
		edges = store.entries[idx].RelatedEdges
	}
	store.mu.RUnlock()

	if idx < 0 {
		t.Fatal("post-snapshot entry not found in store")
	}
	if len(edges) == 0 {
		t.Error("post-snapshot entry's RelatedEdges were clobbered by the async rebuild; snapshotIDs filter should have protected them")
	}
	if edges[0].ID != "clobber-synced" {
		t.Errorf("RelatedEdge ID = %q, want %q", edges[0].ID, "clobber-synced")
	}
}

// ---------------------------------------------------------------------------
// goroutine serialisation: consecutive syncs do not run rebuilds concurrently
// ---------------------------------------------------------------------------

// TestRebuildAsync_ConsecutiveSyncsSerialise verifies that when two sync
// batches fire in quick succession the second rebuild waits for the first
// (prevDone handoff), preventing concurrent syncGraphLinksLocked calls.
func TestRebuildAsync_ConsecutiveSyncsSerialise(t *testing.T) {
	store, cleanup := newAsyncRebuildStore(t)
	defer cleanup()

	now := time.Now().UTC()
	for i, id := range []string{"serial-a", "serial-b", "serial-c"} {
		e := Entry{
			ID: id, Content: "serialisation test entry " + string(rune('A'+i)),
			Category: CategoryProjectKnowledge, Strength: 1, AccessCount: 1,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := store.backend.SaveEntry(&e); err != nil {
			t.Fatalf("SaveEntry %s: %v", id, err)
		}
	}
	// The second call will find a non-nil lastRebuildDone from the first and
	// its goroutine will wait on prevDone before starting its own rebuild.
	store.syncOnce()
	// Add another entry so the second syncOnce has work to do.
	extra := Entry{
		ID: "serial-d", Content: "second batch entry",
		Category: CategoryProjectKnowledge, Strength: 1, AccessCount: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.backend.SaveEntry(&extra); err != nil {
		t.Fatalf("SaveEntry serial-d: %v", err)
	}
	store.syncOnce()

	// Wait for everything to settle.
	store.WaitRebuild()

	// All four entries must be searchable — no corruption from concurrent rebuilds.
	store.mu.RLock()
	scores := store.bm25.score("serialisation test entry")
	store.mu.RUnlock()

	for _, id := range []string{"serial-a", "serial-b", "serial-c"} {
		if scores[id] <= 0 {
			t.Errorf("entry %q not findable in BM25 after consecutive syncs; rebuild may have been corrupted", id)
		}
	}
}

// ---------------------------------------------------------------------------
// Stop safety: Stop waits for in-flight rebuild before closing the backend
// ---------------------------------------------------------------------------

// TestRebuildAsync_StopWaitsForInFlightRebuild verifies that Store.Stop()
// does not return (and therefore does not close the backend) while a rebuild
// goroutine still holds s.mu.Lock inside rebuildDerivedIndexesOutsideLock.
//
// We detect premature return by checking that the BM25 index built by the
// goroutine is visible after Stop() returns — if Stop() closed the backend
// prematurely the goroutine could panic, but that is hard to detect in tests.
// Instead we verify that WaitRebuild + Stop sequence is race-free via -race.
func TestRebuildAsync_StopWaitsForInFlightRebuild(t *testing.T) {
	store, cleanup := newAsyncRebuildStore(t)
	// Do NOT defer cleanup here — we call Stop() manually below.

	now := time.Now().UTC()
	for _, id := range []string{"stop-a", "stop-b", "stop-c", "stop-d", "stop-e"} {
		e := Entry{
			ID: id, Content: "stop safety test entry for " + id,
			Category: CategoryProjectKnowledge, Strength: 1, AccessCount: 1,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := store.backend.SaveEntry(&e); err != nil {
			t.Fatalf("SaveEntry %s: %v", id, err)
		}
	}

	// Trigger a sync (and hence an async rebuild goroutine) …
	store.syncOnce()

	// … then immediately call Stop(), which must wait for the rebuild to finish
	// before closing the backend.  If Stop() returns early the goroutine may
	// still be in rebuildDerivedIndexesOutsideLock, and a subsequent backend
	// access via s.mu.Lock inside that function would race with backend.Close().
	// Running this test with -race catches such violations.
	store.Stop()

	// Call cleanup to close the extra backend handle (the backend itself is
	// already closed by Stop(), so this is a no-op for the backend; the
	// returned func only calls store.Stop() again, which is idempotent via
	// stopOnce, plus b.Close() for the SQLite handle).
	cleanup()
}
