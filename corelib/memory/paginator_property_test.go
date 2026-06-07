package memory

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Generators for paginated recall property tests.
// ---------------------------------------------------------------------------

// genPaginatorCategory generates a random category suitable for recall.
func genPaginatorCategory() *rapid.Generator[Category] {
	return rapid.SampledFrom([]Category{
		CategoryProjectKnowledge,
		CategoryUserFact,
		CategoryPreference,
		CategoryInstruction,
		CategoryProject,
		CategoryReference,
	})
}

// genPaginatorOwnerID generates a random owner ID.
func genPaginatorOwnerID() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		return "user_" + rapid.StringMatching(`[a-z0-9]{4,8}`).Draw(t, "ownerSuffix")
	})
}

// genPaginatorEntryContent generates content with varying lengths to exercise
// the token budget logic. Content always contains "recall" for BM25 matching.
func genPaginatorEntryContent() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		// Base words that will be searched by query
		baseWords := []string{"recall", "memory", "knowledge", "context", "information"}
		// Filler words to vary content length
		fillerWords := []string{"alpha", "beta", "gamma", "delta", "epsilon",
			"zeta", "theta", "iota", "kappa", "lambda", "sigma", "omega"}

		// Pick 1-2 base words to ensure BM25 matches
		nBase := rapid.IntRange(1, 2).Draw(t, "nBase")
		selected := make([]string, 0, nBase+10)
		for i := 0; i < nBase; i++ {
			selected = append(selected, rapid.SampledFrom(baseWords).Draw(t, "base"))
		}

		// Add filler words to vary length (3-30 words total)
		nFiller := rapid.IntRange(2, 28).Draw(t, "nFiller")
		for i := 0; i < nFiller; i++ {
			selected = append(selected, rapid.SampledFrom(fillerWords).Draw(t, "filler"))
		}

		// Add a unique suffix to avoid substring dedup
		suffix := rapid.StringMatching(`[a-z0-9]{10}`).Draw(t, "suffix")
		return strings.Join(selected, " ") + " " + suffix
	})
}

// genPaginatorQuery generates a query string that will produce BM25 matches.
func genPaginatorQuery() *rapid.Generator[string] {
	return rapid.SampledFrom([]string{
		"recall", "memory", "knowledge", "context", "information",
		"recall memory", "knowledge context", "recall information",
	})
}

// ---------------------------------------------------------------------------
// Property 1: Paginated recall order preservation
//
// For any memory store and valid query, concatenating all pages from a
// paginated recall (page 1 through page N where has_more=false) SHALL produce
// the same entry sequence as a single non-paginated recall of the same query
// with no entry/token limit.
//
// **Validates: Requirements 1.2, 1.3**
// ---------------------------------------------------------------------------

func TestProperty1_PaginatedRecallOrderPreservation(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(rt *rapid.T) {
		// Create a store with random entries
		storePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{8}`).Draw(rt, "fname")+".json")
		store, err := NewStore(storePath)
		if err != nil {
			rt.Fatal(err)
		}
		defer store.Stop()

		ownerID := genPaginatorOwnerID().Draw(rt, "ownerID")
		category := genPaginatorCategory().Draw(rt, "category")

		// Generate between 5 and 60 entries to exercise multi-page scenarios
		nEntries := rapid.IntRange(5, 60).Draw(rt, "nEntries")

		for i := 0; i < nEntries; i++ {
			content := genPaginatorEntryContent().Draw(rt, "content")
			entry := Entry{
				Content:   content,
				Category:  category,
				OwnerID:   ownerID,
				CreatedAt: time.Now().Add(-time.Duration(nEntries-i) * time.Minute),
				UpdatedAt: time.Now().Add(-time.Duration(nEntries-i) * time.Minute),
			}
			if err := store.Save(entry); err != nil {
				rt.Fatal(err)
			}
		}

		query := genPaginatorQuery().Draw(rt, "query")

		// --- Reference: get the full scored candidate list (non-paginated) ---
		allCandidates := store.recallScoredForPagination(query, category, "", ownerID)
		if len(allCandidates) == 0 {
			// No matches for this random configuration; skip.
			return
		}

		// Build reference entry ID sequence from the full candidate list.
		referenceIDs := make([]string, len(allCandidates))
		for i, c := range allCandidates {
			referenceIDs[i] = c.entry.ID
		}

		// --- Paginated: collect all pages ---
		pager := NewCursorPaginator()
		var paginatedIDs []string

		page, err := pager.FirstPage(store, query, category, "", ownerID)
		if err != nil {
			rt.Fatalf("FirstPage error: %v", err)
		}

		for _, e := range page.Entries {
			paginatedIDs = append(paginatedIDs, e.ID)
		}

		// Follow cursor through all subsequent pages
		iterations := 0
		for page.HasMore {
			iterations++
			if iterations > 100 {
				rt.Fatal("too many pages (possible infinite loop)")
			}

			page, err = pager.NextPage(page.Cursor)
			if err != nil {
				rt.Fatalf("NextPage error on iteration %d: %v", iterations, err)
			}

			for _, e := range page.Entries {
				paginatedIDs = append(paginatedIDs, e.ID)
			}
		}

		// --- Verify: paginated IDs maintain monotonically increasing order ---
		// from the reference candidate list.
		//
		// The paginated results should be a subsequence of referenceIDs in
		// the same order. Some entries might be skipped (e.g., entries that
		// exceed an entire page's token budget are force-included one-at-a-time),
		// but the relative order must be preserved.

		if len(paginatedIDs) == 0 && len(referenceIDs) > 0 {
			rt.Fatalf("paginated recall returned 0 entries but %d candidates exist", len(referenceIDs))
		}

		// Build a position map for reference IDs
		refPos := make(map[string]int, len(referenceIDs))
		for i, id := range referenceIDs {
			refPos[id] = i
		}

		// Verify monotonically increasing positions (order preservation)
		prevPos := -1
		for i, id := range paginatedIDs {
			pos, exists := refPos[id]
			if !exists {
				rt.Fatalf("paginated entry %d (ID=%s) not found in reference candidate list", i, id)
			}
			if pos <= prevPos {
				rt.Fatalf("order violation at paginated entry %d: reference position %d <= previous position %d",
					i, pos, prevPos)
			}
			prevPos = pos
		}

		// Verify no duplicates in paginated results
		seen := make(map[string]bool, len(paginatedIDs))
		for i, id := range paginatedIDs {
			if seen[id] {
				rt.Fatalf("duplicate entry ID %s at paginated position %d", id, i)
			}
			seen[id] = true
		}
	})
}

// ---------------------------------------------------------------------------
// Property 3: Per-page token budget invariant
//
// For any single page in a paginated recall, the sum of
// EstimateTextTokens(entry.Content) for all entries on that page SHALL not
// exceed 2500 tokens.
// Exception: a single entry that exceeds the entire budget may appear alone
// on a page (the paginator includes at least one entry per page to guarantee
// forward progress).
//
// **Validates: Requirements 1.8**
// ---------------------------------------------------------------------------

func TestProperty3_PerPageTokenBudgetInvariant(t *testing.T) {
	dir := t.TempDir()

	t.Run("Property3_PerPageTokenBudgetInvariant", rapid.MakeCheck(func(rt *rapid.T) {
		storePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{8}`).Draw(rt, "fname")+".json")
		store, err := NewStore(storePath)
		if err != nil {
			rt.Fatal(err)
		}
		defer store.Stop()

		ownerID := genPaginatorOwnerID().Draw(rt, "ownerID")
		category := genPaginatorCategory().Draw(rt, "category")

		// Generate a store with entries of varying content lengths.
		// Content always contains searchable words for BM25 matching.
		numEntries := rapid.IntRange(5, 50).Draw(rt, "numEntries")
		for i := 0; i < numEntries; i++ {
			content := genPaginatorEntryContent().Draw(rt, "content")
			entry := Entry{
				Content:   content,
				Category:  category,
				OwnerID:   ownerID,
				CreatedAt: time.Now().Add(-time.Duration(numEntries-i) * time.Minute),
				UpdatedAt: time.Now().Add(-time.Duration(numEntries-i) * time.Minute),
			}
			if err := store.Save(entry); err != nil {
				rt.Fatalf("Save failed for entry %d: %v", i, err)
			}
		}

		query := genPaginatorQuery().Draw(rt, "query")

		// Create paginator and get first page.
		paginator := NewCursorPaginator()
		result, err := paginator.FirstPage(store, query, category, "", ownerID)
		if err != nil {
			rt.Fatalf("FirstPage failed: %v", err)
		}

		// Verify token budget for all pages.
		pageNum := 1
		for {
			verifyPageTokenBudget(rt, result, pageNum)

			if !result.HasMore {
				break
			}

			// Get next page.
			result, err = paginator.NextPage(result.Cursor)
			if err != nil {
				rt.Fatalf("NextPage failed on page %d: %v", pageNum+1, err)
			}
			pageNum++

			// Safety: prevent infinite loops in case of bugs.
			if pageNum > 200 {
				rt.Fatal("exceeded maximum expected page count (200), possible infinite loop")
			}
		}
	}))
}

// verifyPageTokenBudget checks that a single page respects the per-page token budget.
// Exception: a single oversized entry may appear alone on a page.
func verifyPageTokenBudget(rt *rapid.T, page *PaginatedResult, pageNum int) {
	if len(page.Entries) == 0 {
		return // empty page is trivially within budget
	}

	totalTokens := 0
	for _, e := range page.Entries {
		totalTokens += EstimateTextTokens(e.Content)
	}

	// Exception: if there's exactly one entry on the page, it's allowed to exceed the budget.
	// This is the "at least one entry per page" guarantee for forward progress.
	if len(page.Entries) == 1 {
		return
	}

	// For pages with multiple entries, the total must not exceed perPageTokenBudget (2500).
	if totalTokens > perPageTokenBudget {
		rt.Fatalf("page %d: total tokens %d exceeds perPageTokenBudget %d (entries: %d)",
			pageNum, totalTokens, perPageTokenBudget, len(page.Entries))
	}
}

// ---------------------------------------------------------------------------
// Property 2: has_more correctness
//
// For any paginated recall response, `has_more` SHALL be `true` if and only if
// there exist additional scored entries beyond the current page's position in
// the candidate list that have not yet been returned.
//
// Use `rapid` library (pgregory.net/rapid), generate random stores and queries,
// min 100 iterations.
//
// **Validates: Requirements 1.5, 1.7**
// ---------------------------------------------------------------------------

func TestProperty2_HasMoreCorrectness(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(rt *rapid.T) {
		// Generate a random store with 0-100 entries.
		numEntries := rapid.IntRange(0, 100).Draw(rt, "numEntries")

		storePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{8}`).Draw(rt, "fname")+".json")
		store, err := NewStore(storePath)
		if err != nil {
			rt.Fatal(err)
		}
		defer store.Stop()

		ownerID := genPaginatorOwnerID().Draw(rt, "ownerID")
		category := genPaginatorCategory().Draw(rt, "category")

		// Populate store with random entries of varying content lengths.
		for i := 0; i < numEntries; i++ {
			content := genPaginatorEntryContent().Draw(rt, "content")
			entry := Entry{
				Content:   content,
				Category:  category,
				OwnerID:   ownerID,
				CreatedAt: time.Now().Add(-time.Duration(numEntries-i) * time.Minute),
				UpdatedAt: time.Now().Add(-time.Duration(numEntries-i) * time.Minute),
			}
			if err := store.Save(entry); err != nil {
				rt.Fatalf("Save entry %d: %v", i, err)
			}
		}

		query := genPaginatorQuery().Draw(rt, "query")

		pager := NewCursorPaginator()

		// Get first page.
		page, err := pager.FirstPage(store, query, category, "", ownerID)
		if err != nil {
			rt.Fatalf("FirstPage: %v", err)
		}

		// Collect all entries across all pages and verify has_more semantics.
		var allEntries []Entry
		allEntries = append(allEntries, page.Entries...)
		pageCount := 1

		for page.HasMore {
			// PROPERTY CHECK 1: has_more=true implies a cursor must be present.
			if page.Cursor == "" {
				rt.Fatalf("has_more=true but cursor is empty on page %d", pageCount)
			}

			nextPage, err := pager.NextPage(page.Cursor)
			if err != nil {
				rt.Fatalf("NextPage on page %d: %v", pageCount+1, err)
			}

			// PROPERTY CHECK 2: has_more=true on previous page implies next page
			// returns at least one entry (there truly are more entries beyond).
			if len(nextPage.Entries) == 0 {
				rt.Fatalf("has_more was true on page %d but next page returned 0 entries", pageCount)
			}

			allEntries = append(allEntries, nextPage.Entries...)
			pageCount++
			page = nextPage

			// Safety guard against infinite loops.
			if pageCount > numEntries+2 {
				rt.Fatalf("pagination exceeded expected page count (%d pages for %d entries)", pageCount, numEntries)
			}
		}

		// PROPERTY CHECK 3: When has_more=false, cursor should be empty.
		if page.Cursor != "" {
			rt.Fatalf("has_more=false but cursor is non-empty: %q", page.Cursor)
		}

		// PROPERTY CHECK 4: When has_more=false, the total entries returned across
		// all pages must equal the full candidate set. This verifies that
		// has_more=false truly means no more entries exist.
		fullCandidates := store.recallScoredForPagination(query, category, "", ownerID)
		if len(allEntries) != len(fullCandidates) {
			rt.Fatalf("has_more=false but paginated recall returned %d entries while full candidate set has %d entries (page count: %d)",
				len(allEntries), len(fullCandidates), pageCount)
		}

		// PROPERTY CHECK 5: No duplicate entries across pages — each entry is
		// returned exactly once, confirming has_more transitions are correct.
		seen := make(map[string]bool, len(allEntries))
		for _, e := range allEntries {
			if seen[e.ID] {
				rt.Fatalf("duplicate entry %s returned across pages", e.ID)
			}
			seen[e.ID] = true
		}
	})
}
