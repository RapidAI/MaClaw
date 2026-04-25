package memory

import (
	"path/filepath"
	"testing"
)

func newOwnerTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test_memory.json")
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Stop() })
	return s
}

func TestSaveForUser_SetsOwnerID(t *testing.T) {
	s := newOwnerTestStore(t)

	entry := Entry{
		Content:  "User A project knowledge about deployment pipeline",
		Category: CategoryProjectKnowledge,
	}
	err := s.SaveForUser(entry, "feishu_ou_userA")
	if err != nil {
		t.Fatalf("SaveForUser failed: %v", err)
	}

	entries := s.List("", "")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].OwnerID != "feishu_ou_userA" {
		t.Errorf("expected OwnerID 'feishu_ou_userA', got %q", entries[0].OwnerID)
	}
}

func TestRecallDynamic_NoOwnerID_ReturnsAll(t *testing.T) {
	s := newOwnerTestStore(t)

	// Save entries with different owners.
	_ = s.SaveForUser(Entry{Content: "User A knows about PostgreSQL database configuration", Category: CategoryProjectKnowledge}, "userA")
	_ = s.SaveForUser(Entry{Content: "User B knows about Redis cache configuration", Category: CategoryProjectKnowledge}, "userB")
	_ = s.Save(Entry{Content: "Shared knowledge about Docker deployment process", Category: CategoryProjectKnowledge})

	// RecallDynamic without ownerID returns all entries.
	results := s.RecallDynamic("database configuration", "", "")
	if len(results) == 0 {
		t.Fatal("expected results without ownerID filter")
	}
}

func TestRecallDynamic_WithOwnerID_FiltersCorrectly(t *testing.T) {
	s := newOwnerTestStore(t)

	// Save entries with different owners.
	_ = s.SaveForUser(Entry{Content: "User A project uses PostgreSQL 16 for main database backend", Category: CategoryProjectKnowledge}, "userA")
	_ = s.SaveForUser(Entry{Content: "User B project uses MySQL 8 for main database backend", Category: CategoryProjectKnowledge}, "userB")
	_ = s.Save(Entry{Content: "Shared: all projects use Docker for deployment orchestration", Category: CategoryProjectKnowledge})

	// RecallDynamic with ownerID="userA" should return userA's entries + shared entries.
	results := s.RecallDynamic("database", "", "", "userA")

	hasUserA := false
	hasUserB := false
	hasShared := false
	for _, e := range results {
		t.Logf("  result: OwnerID=%q Content=%s", e.OwnerID, e.Content[:50])
		if e.OwnerID == "userA" {
			hasUserA = true
		}
		if e.OwnerID == "userB" {
			hasUserB = true
		}
		if e.OwnerID == "" {
			hasShared = true
		}
	}

	if !hasUserA {
		t.Error("expected userA's entries in results")
	}
	if hasUserB {
		t.Error("userB's entries should NOT be in results when filtering by userA")
	}
	if !hasShared {
		t.Error("shared entries (empty OwnerID) should be in results")
	}
}

func TestRecallDynamic_EmptyOwnerID_BackwardCompatible(t *testing.T) {
	s := newOwnerTestStore(t)

	// Save entries — some with OwnerID, some without (legacy).
	_ = s.SaveForUser(Entry{Content: "Owned entry about server configuration details", Category: CategoryProjectKnowledge}, "userA")
	_ = s.Save(Entry{Content: "Legacy entry about server deployment process", Category: CategoryProjectKnowledge})

	// RecallDynamic with empty ownerID (GUI/TUI single-user mode) returns all.
	results := s.RecallDynamic("server", "", "", "")
	hasOwned := false
	hasLegacy := false
	for _, e := range results {
		if e.OwnerID == "userA" {
			hasOwned = true
		}
		if e.OwnerID == "" {
			hasLegacy = true
		}
	}
	if !hasOwned || !hasLegacy {
		t.Errorf("empty ownerID should return all entries: hasOwned=%v hasLegacy=%v", hasOwned, hasLegacy)
	}
}

func TestOwnerID_JSONSerialization(t *testing.T) {
	s := newOwnerTestStore(t)

	_ = s.SaveForUser(Entry{Content: "Test entry for JSON serialization of OwnerID field", Category: CategoryProjectKnowledge}, "testUser123")

	// Flush to disk and reload.
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Create a new store from the same path to verify persistence.
	s2, err := NewStore(s.Path())
	if err != nil {
		t.Fatalf("NewStore reload failed: %v", err)
	}
	defer s2.Stop()

	entries := s2.List("", "")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after reload, got %d", len(entries))
	}
	if entries[0].OwnerID != "testUser123" {
		t.Errorf("OwnerID not persisted: expected 'testUser123', got %q", entries[0].OwnerID)
	}
}

func TestOwnerID_OmitEmptyInJSON(t *testing.T) {
	s := newOwnerTestStore(t)

	// Save without OwnerID — should not appear in JSON.
	_ = s.Save(Entry{Content: "Entry without owner for omitempty JSON test", Category: CategoryProjectKnowledge})

	if err := s.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	s2, err := NewStore(s.Path())
	if err != nil {
		t.Fatalf("NewStore reload failed: %v", err)
	}
	defer s2.Stop()

	entries := s2.List("", "")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].OwnerID != "" {
		t.Errorf("expected empty OwnerID for legacy entry, got %q", entries[0].OwnerID)
	}
}


// ============================================================================
// Tests for OwnerID fixes (Phase 6 improvements)
// ============================================================================

func TestDedup_DifferentOwners_NotDeduplicated(t *testing.T) {
	s := newOwnerTestStore(t)

	// Save identical content for two different users.
	content := "PostgreSQL 16 with pgvector extension for vector search"
	_ = s.SaveForUser(Entry{Content: content, Category: CategoryProjectKnowledge}, "userA")
	_ = s.SaveForUser(Entry{Content: content, Category: CategoryProjectKnowledge}, "userB")

	entries := s.List("", "")
	if len(entries) != 2 {
		t.Errorf("expected 2 entries (one per user), got %d", len(entries))
	}

	// Verify both users have their own copy.
	ownerCounts := make(map[string]int)
	for _, e := range entries {
		ownerCounts[e.OwnerID]++
	}
	if ownerCounts["userA"] != 1 || ownerCounts["userB"] != 1 {
		t.Errorf("expected 1 entry per user, got userA=%d userB=%d", ownerCounts["userA"], ownerCounts["userB"])
	}
}

func TestDedup_SameOwner_Deduplicated(t *testing.T) {
	s := newOwnerTestStore(t)

	// Save identical content twice for the same user.
	content := "PostgreSQL 16 with pgvector extension for vector search"
	_ = s.SaveForUser(Entry{Content: content, Category: CategoryProjectKnowledge}, "userA")
	_ = s.SaveForUser(Entry{Content: content, Category: CategoryProjectKnowledge}, "userA")

	entries := s.List("", "")
	if len(entries) != 1 {
		t.Errorf("expected 1 entry (deduplicated), got %d", len(entries))
	}
}

func TestDedup_SharedEntry_DeduplicatesWithAnyUser(t *testing.T) {
	s := newOwnerTestStore(t)

	// Save shared entry (empty OwnerID).
	content := "Docker deployment best practices for production"
	_ = s.Save(Entry{Content: content, Category: CategoryProjectKnowledge})

	// Try to save same content for a specific user — should be deduplicated.
	_ = s.SaveForUser(Entry{Content: content, Category: CategoryProjectKnowledge}, "userA")

	entries := s.List("", "")
	if len(entries) != 1 {
		t.Errorf("expected 1 entry (shared deduplicates with user), got %d", len(entries))
	}
}

func TestUniqueOwnerIDs_ReturnsAllUsers(t *testing.T) {
	s := newOwnerTestStore(t)

	// Save entries for multiple users.
	_ = s.SaveForUser(Entry{Content: "User A content", Category: CategoryProjectKnowledge}, "userA")
	_ = s.SaveForUser(Entry{Content: "User B content", Category: CategoryProjectKnowledge}, "userB")
	_ = s.SaveForUser(Entry{Content: "User C content", Category: CategoryProjectKnowledge}, "userC")
	_ = s.Save(Entry{Content: "Shared content", Category: CategoryProjectKnowledge}) // No OwnerID

	ownerIDs := s.UniqueOwnerIDs()

	// Should return 3 unique owner IDs (excluding empty).
	if len(ownerIDs) != 3 {
		t.Errorf("expected 3 unique owner IDs, got %d: %v", len(ownerIDs), ownerIDs)
	}

	// Verify all expected IDs are present.
	idSet := make(map[string]bool)
	for _, id := range ownerIDs {
		idSet[id] = true
	}
	for _, expected := range []string{"userA", "userB", "userC"} {
		if !idSet[expected] {
			t.Errorf("expected %q in UniqueOwnerIDs, got %v", expected, ownerIDs)
		}
	}
}

func TestUniqueOwnerIDs_ExcludesEmptyOwnerID(t *testing.T) {
	s := newOwnerTestStore(t)

	// Save only shared entries (empty OwnerID).
	_ = s.Save(Entry{Content: "Shared content 1", Category: CategoryProjectKnowledge})
	_ = s.Save(Entry{Content: "Shared content 2", Category: CategoryProjectKnowledge})

	ownerIDs := s.UniqueOwnerIDs()

	if len(ownerIDs) != 0 {
		t.Errorf("expected 0 owner IDs (all shared), got %d: %v", len(ownerIDs), ownerIDs)
	}
}

func TestUniqueOwnerIDs_DeduplicatesMultipleEntriesSameUser(t *testing.T) {
	s := newOwnerTestStore(t)

	// Save multiple entries for the same user.
	_ = s.SaveForUser(Entry{Content: "User A content 1", Category: CategoryProjectKnowledge}, "userA")
	_ = s.SaveForUser(Entry{Content: "User A content 2", Category: CategoryProjectKnowledge}, "userA")
	_ = s.SaveForUser(Entry{Content: "User A content 3", Category: CategoryProjectKnowledge}, "userA")

	ownerIDs := s.UniqueOwnerIDs()

	if len(ownerIDs) != 1 {
		t.Errorf("expected 1 unique owner ID, got %d: %v", len(ownerIDs), ownerIDs)
	}
	if ownerIDs[0] != "userA" {
		t.Errorf("expected 'userA', got %q", ownerIDs[0])
	}
}
