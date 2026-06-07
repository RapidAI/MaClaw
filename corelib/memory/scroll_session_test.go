package memory

import (
	"path/filepath"
	"testing"
	"time"
)

// newScrollTestStore creates a Store with n entries for scroll session testing.
func newScrollTestStore(t *testing.T, n int) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		entry := Entry{
			Content:   "test entry content number " + string(rune('A'+i%26)) + " with some text for token counting item " + string(rune('0'+i%10)),
			Category:  CategoryProjectKnowledge,
			Tags:      []string{"test"},
			CreatedAt: time.Now().Add(-time.Duration(n-i) * time.Minute),
			UpdatedAt: time.Now().Add(-time.Duration(n-i) * time.Minute),
		}
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func TestScrollSessionManager_GetOrCreate(t *testing.T) {
	store := newScrollTestStore(t, 10)

	mgr := NewScrollSessionManager()

	// Create a new session.
	sess := mgr.GetOrCreate("loop-1", store, "test", "", "", "")
	if sess == nil {
		t.Fatal("expected non-nil session")
	}
	if sess.LoopID != "loop-1" {
		t.Errorf("expected LoopID='loop-1', got %q", sess.LoopID)
	}
	if sess.Query != "test" {
		t.Errorf("expected Query='test', got %q", sess.Query)
	}
	if sess.Position != 0 {
		t.Errorf("expected Position=0, got %d", sess.Position)
	}
	if len(sess.Candidates) == 0 {
		t.Error("expected non-empty candidates")
	}

	// Same loopID + same query returns same session (no re-creation).
	sess2 := mgr.GetOrCreate("loop-1", store, "test", "", "", "")
	if sess2 != sess {
		t.Error("expected same session pointer for same loopID and query")
	}
}

func TestScrollSessionManager_Advance(t *testing.T) {
	store := newScrollTestStore(t, 20)

	mgr := NewScrollSessionManager()
	mgr.GetOrCreate("loop-1", store, "test", "", "", "")

	// First advance should return some entries.
	result, err := mgr.Advance("loop-1", perPageTokenBudget)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SessionExhausted {
		t.Error("expected session not exhausted on first advance")
	}
	if len(result.Entries) == 0 {
		t.Error("expected non-empty entries from first advance")
	}

	firstBatchIDs := make(map[string]bool)
	for _, e := range result.Entries {
		firstBatchIDs[e.ID] = true
	}

	// Second advance should return different entries (no overlap).
	result2, err := mgr.Advance("loop-1", perPageTokenBudget)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, e := range result2.Entries {
		if firstBatchIDs[e.ID] {
			t.Errorf("entry %q appeared in both first and second batch (overlap)", e.ID)
		}
	}
}

func TestScrollSessionManager_Exhaustion(t *testing.T) {
	// Use a small number of entries so we can exhaust them quickly.
	store := newScrollTestStore(t, 3)

	mgr := NewScrollSessionManager()
	mgr.GetOrCreate("loop-1", store, "test", "", "", "")

	// Advance until exhausted.
	totalReturned := 0
	for i := 0; i < 10; i++ {
		result, err := mgr.Advance("loop-1", perPageTokenBudget)
		if err != nil {
			t.Fatalf("unexpected error on advance %d: %v", i, err)
		}
		if result.SessionExhausted {
			if len(result.Entries) != 0 {
				t.Error("expected empty entries when session exhausted")
			}
			break
		}
		totalReturned += len(result.Entries)
	}

	// Should have returned all entries that matched.
	if totalReturned == 0 {
		t.Error("expected at least some entries to be returned before exhaustion")
	}

	// Subsequent advance should also return exhausted.
	result, _ := mgr.Advance("loop-1", perPageTokenBudget)
	if !result.SessionExhausted {
		t.Error("expected session exhausted after all candidates returned")
	}
	if len(result.Entries) != 0 {
		t.Error("expected empty entries after exhaustion")
	}
}

func TestScrollSessionManager_QueryChange(t *testing.T) {
	store := newScrollTestStore(t, 10)

	mgr := NewScrollSessionManager()

	// Create session with query "alpha".
	sess1 := mgr.GetOrCreate("loop-1", store, "Alpha", "", "", "")
	if sess1.Query != "alpha" {
		t.Errorf("expected normalized query 'alpha', got %q", sess1.Query)
	}

	// Advance to move position.
	mgr.Advance("loop-1", perPageTokenBudget)

	// Change query to "beta" — should reset session.
	sess2 := mgr.GetOrCreate("loop-1", store, "Beta", "", "", "")
	if sess2.Query != "beta" {
		t.Errorf("expected normalized query 'beta', got %q", sess2.Query)
	}
	if sess2.Position != 0 {
		t.Errorf("expected Position=0 after query change, got %d", sess2.Position)
	}
	// Should be a new session (different pointer after re-creation).
	if sess2 == sess1 {
		t.Error("expected new session after query change")
	}
}

func TestScrollSessionManager_Destroy(t *testing.T) {
	store := newScrollTestStore(t, 5)

	mgr := NewScrollSessionManager()
	mgr.GetOrCreate("loop-1", store, "test", "", "", "")

	// Destroy.
	mgr.Destroy("loop-1")

	// After destroy, advance should return exhausted (no session).
	result, _ := mgr.Advance("loop-1", perPageTokenBudget)
	if !result.SessionExhausted {
		t.Error("expected session exhausted after destroy")
	}
}

func TestScrollSessionManager_CacheBounded(t *testing.T) {
	// Create more entries than the cache limit (200).
	store := newScrollTestStore(t, 250)

	mgr := NewScrollSessionManager()
	sess := mgr.GetOrCreate("loop-1", store, "test", "", "", "")

	if len(sess.Candidates) > scrollSessionMaxCache {
		t.Errorf("expected candidates bounded at %d, got %d", scrollSessionMaxCache, len(sess.Candidates))
	}
}

func TestScrollSessionManager_AdvanceNonExistentSession(t *testing.T) {
	mgr := NewScrollSessionManager()

	result, err := mgr.Advance("nonexistent", perPageTokenBudget)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.SessionExhausted {
		t.Error("expected session exhausted for non-existent session")
	}
	if len(result.Entries) != 0 {
		t.Error("expected empty entries for non-existent session")
	}
}

func TestScrollSessionManager_QueryNormalization(t *testing.T) {
	store := newScrollTestStore(t, 5)

	mgr := NewScrollSessionManager()

	// Different casing should be the same query.
	sess1 := mgr.GetOrCreate("loop-1", store, "Test Query", "", "", "")
	sess2 := mgr.GetOrCreate("loop-1", store, "test query", "", "", "")

	if sess1 != sess2 {
		t.Error("expected same session for case-insensitive equivalent queries")
	}

	// Leading/trailing whitespace trimmed.
	sess3 := mgr.GetOrCreate("loop-1", store, "  test query  ", "", "", "")
	if sess3 != sess1 {
		t.Error("expected same session for whitespace-trimmed equivalent queries")
	}
}
