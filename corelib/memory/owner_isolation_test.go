package memory

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestDedup_SharedEntry_DoesNotMergeWithNamedOwner(t *testing.T) {
	s := newOwnerTestStore(t)

	content := "Docker deployment best practices for production"
	_ = s.Save(Entry{Content: content, Category: CategoryProjectKnowledge})
	_ = s.SaveForUser(Entry{Content: content, Category: CategoryProjectKnowledge}, "userA")
	_ = s.SaveForUser(Entry{Content: content, Category: CategoryProjectKnowledge}, "userA")

	entries := s.List("", "")
	if len(entries) != 2 {
		t.Fatalf("expected shared and named copies to stay distinct, got %d", len(entries))
	}
	counts := map[string]int{}
	for _, entry := range entries {
		counts[entry.OwnerID]++
	}
	if counts[""] != 1 || counts["userA"] != 1 {
		t.Fatalf("owner counts = %v", counts)
	}
}

func TestDedup_SameContentAfterOtherOwnerStillDedups(t *testing.T) {
	s := newOwnerTestStore(t)
	content := "PostgreSQL 16 with pgvector extension for vector search"
	_ = s.SaveForUser(Entry{Content: content, Category: CategoryProjectKnowledge}, "userA")
	_ = s.SaveForUser(Entry{Content: content, Category: CategoryProjectKnowledge}, "userB")
	_ = s.SaveForUser(Entry{Content: content, Category: CategoryProjectKnowledge}, "userA")

	counts := map[string]int{}
	for _, entry := range s.List("", "") {
		counts[entry.OwnerID]++
	}
	if counts["userA"] != 1 || counts["userB"] != 1 {
		t.Fatalf("repeat save grew a duplicate after the other owner: %v", counts)
	}
}

func TestDiscoverMissingLinksStaysInsideOwner(t *testing.T) {
	s := newOwnerTestStore(t)
	shared := []string{"mysql-setting", "backup-window"}
	if err := s.SaveForUser(Entry{Content: "alpha mysql backup window for production", Category: CategoryProjectKnowledge, Tags: shared}, "userA"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveForUser(Entry{Content: "beta mysql backup window for production", Category: CategoryProjectKnowledge, Tags: shared}, "userB"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveForUser(Entry{Content: "gamma mysql backup window for production", Category: CategoryProjectKnowledge, Tags: shared}, "userA"); err != nil {
		t.Fatal(err)
	}

	_ = s.discoverMissingLinks()
	linkedA := 0
	for _, entry := range s.List(CategoryProjectKnowledge, "") {
		for _, related := range entry.RelatedIDs {
			other := findEntryByID(s, related)
			if other.ID == "" {
				continue
			}
			if other.OwnerID != entry.OwnerID {
				t.Fatalf("link crossed owners: %s (%s) -> %s (%s)", entry.ID, entry.OwnerID, other.ID, other.OwnerID)
			}
		}
		if entry.OwnerID == "userA" && len(entry.RelatedIDs) > 0 {
			linkedA++
		}
		if entry.OwnerID == "userB" && len(entry.RelatedIDs) != 0 {
			t.Fatalf("userB was linked without a same-owner peer: %+v", entry.RelatedIDs)
		}
	}
	if linkedA == 0 {
		t.Fatal("same-owner entries should still be linked")
	}
}

func findEntryByID(s *Store, id string) Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, entry := range s.entries {
		if entry.ID == id {
			return entry
		}
	}
	return Entry{}
}

func TestEvictLRUQuotaIsPerOwner(t *testing.T) {
	s := newOwnerTestStore(t)
	s.mu.Lock()
	s.maxItems = 4
	s.mu.Unlock()

	for i := 0; i < 4; i++ {
		entry := Entry{
			Content:     "userA hot fact " + string(rune('a'+i)),
			Category:    CategoryProjectKnowledge,
			AccessCount: 100,
			OwnerID:     "userA",
		}
		if err := s.SaveForUser(entry, "userA"); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		entry := Entry{
			Content:     "userB cold fact " + string(rune('a'+i)),
			Category:    CategoryProjectKnowledge,
			AccessCount: 1,
			OwnerID:     "userB",
		}
		if err := s.SaveForUser(entry, "userB"); err != nil {
			t.Fatal(err)
		}
	}

	counts := map[string]int{}
	for _, entry := range s.List("", "") {
		counts[entry.OwnerID]++
	}
	if counts["userA"] != 2 || counts["userB"] != 2 {
		t.Fatalf("per-owner hot set = %v", counts)
	}
}

func TestDetectStaleRequiresSameOwnerAndEntityTag(t *testing.T) {
	s := newOwnerTestStore(t)
	_ = s.SaveForUser(Entry{Content: "alpha mysql setting one", Category: CategoryProjectKnowledge, Tags: []string{"mysql-setting"}, OwnerID: "userA"}, "userA")
	time.Sleep(time.Millisecond)
	_ = s.SaveForUser(Entry{Content: "beta mysql setting two", Category: CategoryProjectKnowledge, Tags: []string{"mysql-setting"}, OwnerID: "userB"}, "userB")
	time.Sleep(time.Millisecond)
	_ = s.SaveForUser(Entry{Content: "untagged newer project note", Category: CategoryProjectKnowledge, OwnerID: "userA"}, "userA")
	time.Sleep(time.Millisecond)
	_ = s.SaveForUser(Entry{Content: "path only overlap", Category: CategoryProjectKnowledge, Tags: []string{"form_data", `D:/repo`}, OwnerID: "userA"}, "userA")

	if got := s.DetectStale(); got != 0 {
		t.Fatalf("DetectStale = %d, want 0", got)
	}
	for _, entry := range s.List("", "") {
		if entry.Stale {
			t.Fatalf("entry %s was marked stale", entry.ID)
		}
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

func TestListAndSearchTreatUserAsUserFact(t *testing.T) {
	s := newOwnerTestStore(t)
	if err := s.Save(Entry{Content: "姓名是张三", Category: CategoryUser}); err != nil {
		t.Fatal(err)
	}
	if got := s.List(CategoryUserFact, "张三"); len(got) != 1 {
		t.Fatalf("list canonical category: got %d", len(got))
	}
	if got := s.Search(CategoryUserFact, "张三", 5); len(got) != 1 {
		t.Fatalf("search canonical category: got %d", len(got))
	}
}

func TestMemoryToolListHidesOtherNamedOwners(t *testing.T) {
	s := newOwnerTestStore(t)
	if err := s.SaveForUser(Entry{Content: "user-a fact", Category: CategoryProjectKnowledge}, "user-a"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveForUser(Entry{Content: "user-b fact", Category: CategoryProjectKnowledge}, "user-b"); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Entry{Content: "shared fact", Category: CategoryProjectKnowledge}); err != nil {
		t.Fatal(err)
	}

	listed := HandleTool(s, map[string]interface{}{"action": "list"}, ToolOptions{OwnerID: "user-a"})
	if !strings.Contains(listed, "user-a fact") || !strings.Contains(listed, "shared fact") || strings.Contains(listed, "user-b fact") {
		t.Fatalf("list owner filter: %q", listed)
	}

	var otherID string
	for _, entry := range s.List("", "") {
		if entry.OwnerID == "user-b" {
			otherID = entry.ID
		}
	}
	deleted := HandleTool(s, map[string]interface{}{"action": "delete", "id": otherID}, ToolOptions{OwnerID: "user-a"})
	if deleted != "memory not found" {
		t.Fatalf("delete other owner = %q", deleted)
	}
	if got := s.List("", ""); len(got) != 3 {
		t.Fatalf("other owner entry was removed, remaining %d", len(got))
	}
}

func TestSearchLimitKeepsNewestActiveMatches(t *testing.T) {
	s := newOwnerTestStore(t)
	for _, content := range []string{"alpha one", "alpha two", "alpha three"} {
		if err := s.Save(Entry{Content: content, Category: CategoryProjectKnowledge, Status: StatusActive}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Save(Entry{Content: "alpha superseded", Category: CategoryProjectKnowledge, Status: StatusSuperseded}); err != nil {
		t.Fatal(err)
	}
	got := s.Search(CategoryProjectKnowledge, "alpha", 1)
	if len(got) != 1 || got[0].Content != "alpha three" {
		t.Fatalf("search limit = %+v", got)
	}
}

func TestMemoryToolListSkipsInactive(t *testing.T) {
	s := newOwnerTestStore(t)
	if err := s.Save(Entry{Content: "live memory", Category: CategoryProjectKnowledge, Status: StatusActive, OwnerID: "user-a"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Entry{Content: "dead memory", Category: CategoryProjectKnowledge, Status: StatusSuperseded, OwnerID: "user-a"}); err != nil {
		t.Fatal(err)
	}
	listed := HandleTool(s, map[string]interface{}{"action": "list"}, ToolOptions{OwnerID: "user-a"})
	if !strings.Contains(listed, "live memory") || strings.Contains(listed, "dead memory") {
		t.Fatalf("list = %q", listed)
	}
}

func TestRecallSeesOwnerDespitePadding(t *testing.T) {
	s := newOwnerTestStore(t)
	if err := s.Save(Entry{
		Content:  "user-a deployment pipeline uses blue green",
		Category: CategoryProjectKnowledge,
		OwnerID:  " user-a ",
		Status:   StatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Entry{
		Content:  "user-b deployment pipeline uses rolling restart",
		Category: CategoryProjectKnowledge,
		OwnerID:  "user-b",
		Status:   StatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	got := s.RecallDynamic("deployment pipeline", "", "", "user-a")
	if len(got) != 1 || !strings.Contains(got[0].Content, "blue green") {
		t.Fatalf("padded owner recall = %+v", got)
	}
}

func TestDerivedFactsStayInsideOwner(t *testing.T) {
	s := newOwnerTestStore(t)
	s.recordRecallDebug("user-a", nil, []DerivedFact{{Explanation: "user-a chain", Confidence: 0.8}})
	s.recordRecallDebug("user-b", nil, []DerivedFact{{Explanation: "user-b chain", Confidence: 0.8}})
	section, _ := s.ProactiveContextForPrompt("", ProactivePromptOptions{
		IncludeDerivedFacts: true,
		Recall:              ProactiveRecallOptions{OwnerID: "user-a"},
	})
	if strings.Contains(section, "user-b chain") || !strings.Contains(section, "user-a chain") {
		t.Fatalf("prompt = %q", section)
	}
	if len(s.LastDerivedFactsForOwner("user-c")) != 0 {
		t.Fatal("missing owner returned another user's derived facts")
	}
}
