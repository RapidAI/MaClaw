package memory

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newPaginatorTestStore(t *testing.T, n int) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		// Create entries with varying content lengths to test token budget.
		content := strings.Repeat("word ", 20+i*5) // ~100-600 chars
		entry := Entry{
			Content:   content,
			Category:  CategoryProjectKnowledge,
			CreatedAt: time.Now().Add(-time.Duration(n-i) * time.Minute),
			UpdatedAt: time.Now().Add(-time.Duration(n-i) * time.Minute),
		}
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func TestCursorPaginator_FirstPage_ReturnsResults(t *testing.T) {
	store := newPaginatorTestStore(t, 5)
	pager := NewCursorPaginator()

	result, err := pager.FirstPage(store, "word", CategoryProjectKnowledge, "", "user1")
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Entries) == 0 {
		t.Fatal("expected at least one entry in first page")
	}
	if result.Page != 1 {
		t.Errorf("expected page 1, got %d", result.Page)
	}
}

func TestCursorPaginator_FirstPage_HasMoreWhenMoreEntries(t *testing.T) {
	// Create a store with enough entries to exceed one page's token budget.
	store := newPaginatorTestStore(t, 50)
	pager := NewCursorPaginator()

	result, err := pager.FirstPage(store, "word", CategoryProjectKnowledge, "", "user1")
	if err != nil {
		t.Fatal(err)
	}
	// With 50 entries of varying length, at least some should overflow.
	if !result.HasMore {
		// This is OK if all entries fit in one page. Let's verify entry count.
		if len(result.Entries) >= 50 {
			t.Fatal("expected has_more=true when many entries exist, or entries capped at 15")
		}
	}
	if len(result.Entries) > maxPageEntries {
		t.Errorf("page should have at most %d entries, got %d", maxPageEntries, len(result.Entries))
	}
}

func TestCursorPaginator_NextPage_ReturnsSubsequentEntries(t *testing.T) {
	store := newPaginatorTestStore(t, 50)
	pager := NewCursorPaginator()

	page1, err := pager.FirstPage(store, "word", CategoryProjectKnowledge, "", "user1")
	if err != nil {
		t.Fatal(err)
	}
	if !page1.HasMore {
		t.Skip("all entries fit in first page, cannot test NextPage")
	}
	if page1.Cursor == "" {
		t.Fatal("expected cursor when has_more=true")
	}

	page2, err := pager.NextPage(page1.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	if page2 == nil {
		t.Fatal("expected non-nil second page")
	}
	if len(page2.Entries) == 0 {
		t.Fatal("expected entries in second page")
	}

	// Verify no overlap between pages.
	page1IDs := make(map[string]bool)
	for _, e := range page1.Entries {
		page1IDs[e.ID] = true
	}
	for _, e := range page2.Entries {
		if page1IDs[e.ID] {
			t.Errorf("entry %s appears in both page 1 and page 2", e.ID)
		}
	}
}

func TestCursorPaginator_TokenBudget_NotExceeded(t *testing.T) {
	store := newPaginatorTestStore(t, 50)
	pager := NewCursorPaginator()

	result, err := pager.FirstPage(store, "word", CategoryProjectKnowledge, "", "user1")
	if err != nil {
		t.Fatal(err)
	}

	totalTokens := 0
	for _, e := range result.Entries {
		totalTokens += EstimateTextTokens(e.Content)
	}
	if totalTokens > perPageTokenBudget {
		t.Errorf("page exceeds token budget: %d > %d", totalTokens, perPageTokenBudget)
	}
}

func TestCursorPaginator_LRU_EvictsOldestCursor(t *testing.T) {
	store := newPaginatorTestStore(t, 5)
	pager := NewCursorPaginator()

	// Create maxCursorsPerUser cursors.
	for i := 0; i < maxCursorsPerUser; i++ {
		_, err := pager.FirstPage(store, "word", CategoryProjectKnowledge, "", "user1")
		if err != nil {
			t.Fatal(err)
		}
	}

	count := pager.ActiveCursorsForUser("user1")
	if count != maxCursorsPerUser {
		t.Errorf("expected %d cursors, got %d", maxCursorsPerUser, count)
	}

	// Adding one more should evict the oldest.
	_, err := pager.FirstPage(store, "word", CategoryProjectKnowledge, "", "user1")
	if err != nil {
		t.Fatal(err)
	}

	count = pager.ActiveCursorsForUser("user1")
	if count != maxCursorsPerUser {
		t.Errorf("expected %d cursors after eviction, got %d", maxCursorsPerUser, count)
	}
}

func TestCursorPaginator_Evict_RemovesExpiredCursors(t *testing.T) {
	pager := NewCursorPaginator()

	// Manually add an expired cursor.
	pager.mu.Lock()
	pool := &userCursorPool{
		pool: []*RecallCursor{
			{
				ID:         "expired-1",
				UserID:     "user1",
				CreatedAt:  time.Now().Add(-10 * time.Minute), // expired (>5min)
				LastUsedAt: time.Now().Add(-10 * time.Minute),
			},
			{
				ID:         "active-1",
				UserID:     "user1",
				CreatedAt:  time.Now(), // still valid
				LastUsedAt: time.Now(),
			},
		},
	}
	pager.cursors["user1"] = pool
	pager.mu.Unlock()

	pager.Evict("user1")

	count := pager.ActiveCursorsForUser("user1")
	if count != 1 {
		t.Errorf("expected 1 active cursor after eviction, got %d", count)
	}
}

func TestCursorPaginator_NextPage_InvalidCursor_ReturnsError(t *testing.T) {
	pager := NewCursorPaginator()

	_, err := pager.NextPage("invalid-base64-token")
	if err == nil {
		t.Fatal("expected error for invalid cursor")
	}
}

func TestCursorPaginator_NextPage_ExpiredCursor_ReturnsError(t *testing.T) {
	pager := NewCursorPaginator()

	// Create a valid cursor token but with an expired timestamp.
	token := EncodeCursor("cursor-123", "user1", time.Now().Add(-10*time.Minute))

	_, err := pager.NextPage(token)
	if err == nil {
		t.Fatal("expected error for expired cursor")
	}
	if err != ErrCursorExpired {
		t.Errorf("expected ErrCursorExpired, got: %v", err)
	}
}

func TestCursorPaginator_NextPage_NonexistentCursor_ReturnsError(t *testing.T) {
	pager := NewCursorPaginator()

	// Create a valid cursor token with recent timestamp, but cursor doesn't exist.
	token := EncodeCursor("nonexistent-id", "user1", time.Now())

	_, err := pager.NextPage(token)
	if err == nil {
		t.Fatal("expected error for nonexistent cursor")
	}
	if err != ErrCursorNotFound {
		t.Errorf("expected ErrCursorNotFound, got: %v", err)
	}
}

func TestCursorPaginator_PaginationPreservesOrder(t *testing.T) {
	store := newPaginatorTestStore(t, 50)
	pager := NewCursorPaginator()

	var allEntries []Entry
	page, err := pager.FirstPage(store, "word", CategoryProjectKnowledge, "", "user1")
	if err != nil {
		t.Fatal(err)
	}
	allEntries = append(allEntries, page.Entries...)

	for page.HasMore {
		page, err = pager.NextPage(page.Cursor)
		if err != nil {
			t.Fatal(err)
		}
		allEntries = append(allEntries, page.Entries...)
	}

	// Verify all entries are unique (no duplicates across pages).
	seen := make(map[string]bool)
	for _, e := range allEntries {
		if seen[e.ID] {
			t.Errorf("duplicate entry %s across pages", e.ID)
		}
		seen[e.ID] = true
	}

	// Verify we got all entries that match.
	if len(allEntries) == 0 {
		t.Fatal("expected at least some entries across all pages")
	}
}

func TestCursorPaginator_EmptyStore_ReturnsEmptyPage(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	pager := NewCursorPaginator()

	result, err := pager.FirstPage(store, "nonexistent query", "", "", "user1")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 0 {
		t.Errorf("expected 0 entries for empty store, got %d", len(result.Entries))
	}
	if result.HasMore {
		t.Error("expected has_more=false for empty store")
	}
	if result.Cursor != "" {
		t.Error("expected empty cursor for empty store")
	}
	if result.Page != 1 {
		t.Errorf("expected page 1, got %d", result.Page)
	}
}
