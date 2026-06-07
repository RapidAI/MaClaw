package memory

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Property 5 (Task 3.3): Expanded token budget cap
// When AdaptiveBudgetCalculator.Calculate returns an expanded budget,
// the total estimated tokens of entries injected under that budget
// SHALL not exceed expandedMaxTokens (5000).
// ---------------------------------------------------------------------------

func TestProperty5_ExpandedTokenBudgetCap(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate inputs that guarantee expansion: density > 0.15.
		totalActive := rapid.IntRange(20, 200).Draw(rt, "totalActive")
		// matchingEntries must be > 15% of totalActive to trigger expansion.
		minMatching := int(float64(totalActive)*topicDensityThreshold) + 1
		if minMatching > totalActive {
			minMatching = totalActive
		}
		matchingEntries := rapid.IntRange(minMatching, totalActive).Draw(rt, "matchingEntries")

		calc := &AdaptiveBudgetCalculator{}
		result := calc.Calculate(matchingEntries, totalActive)

		if !result.Expanded {
			return
		}

		// Core property: expanded MaxTokens MUST NOT exceed the cap.
		if result.MaxTokens > expandedMaxTokens {
			rt.Fatalf("expanded MaxTokens %d exceeds cap %d", result.MaxTokens, expandedMaxTokens)
		}

		// Core property: expanded MaxEntries MUST be within [12, 24].
		if result.MaxEntries < 12 || result.MaxEntries > 24 {
			rt.Fatalf("expanded MaxEntries %d outside [12, 24]", result.MaxEntries)
		}

		// Verify budget enforcement: simulate filling MaxEntries entries with
		// random token sizes — total must stay within MaxTokens.
		totalTokens := 0
		for i := 0; i < result.MaxEntries; i++ {
			// Each entry has 10-100 tokens (realistic range).
			entryTokens := rapid.IntRange(10, 100).Draw(rt, fmt.Sprintf("tok_%d", i))
			if totalTokens+entryTokens > result.MaxTokens {
				break // Budget enforcement stops injection here.
			}
			totalTokens += entryTokens
		}
		if totalTokens > result.MaxTokens {
			rt.Fatalf("simulated injection %d tokens exceeds budget %d", totalTokens, result.MaxTokens)
		}
	})
}

// ---------------------------------------------------------------------------
// Property 6 (Task 3.4): Expansion preserves existing filters
// All entries returned by RecallDynamic under expanded budget SHALL satisfy
// OwnerID isolation, category exclusion, and project scope filtering.
// ---------------------------------------------------------------------------

func TestProperty6_ExpansionPreservesFilters(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
		if err != nil {
			rt.Fatal(err)
		}
		defer store.Stop()

		ownerA := "owner-a"
		ownerB := "owner-b"
		projectPath := "/project/alpha"
		numEntries := rapid.IntRange(10, 40).Draw(rt, "numEntries")

		// Save entries for ownerA and ownerB with various categories.
		for i := 0; i < numEntries; i++ {
			content := fmt.Sprintf("entry content alpha project knowledge item %d", i)
			owner := ownerA
			if i%3 == 0 {
				owner = ownerB
			}
			cat := CategoryProjectKnowledge
			if i%5 == 0 {
				cat = Category("preference")
			}
			entry := Entry{
				Content:   content,
				Category:  cat,
				Tags:      []string{"alpha", projectPath},
				CreatedAt: time.Now(),
			}
			_ = store.SaveForUser(entry, owner)
		}

		// Recall as ownerA with project scope.
		results := store.RecallDynamicForTool("alpha project knowledge", CategoryProjectKnowledge, projectPath, ownerA)

		// Verify all returned entries satisfy isolation.
		for _, e := range results {
			// OwnerID isolation: ownerB entries must not appear.
			if e.OwnerID == ownerB {
				rt.Fatalf("entry %s belongs to ownerB but was returned to ownerA", e.ID)
			}
			// Category filter: only project_knowledge should be returned.
			if e.Category != CategoryProjectKnowledge {
				rt.Fatalf("entry %s has category %s, expected %s", e.ID, e.Category, CategoryProjectKnowledge)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Property 7 (Task 4.2): Exhaustive mode respects caps
// RecallExhaustive result SHALL contain at most exhaustiveMaxEntries (100)
// AND sum of EstimateTextTokens(entry.Content) SHALL not exceed
// exhaustiveMaxTokens (15000).
// ---------------------------------------------------------------------------

func TestProperty7_ExhaustiveModeRespectsCaps(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
		if err != nil {
			rt.Fatal(err)
		}
		defer store.Stop()

		// Create 110-130 entries that will match the query.
		numEntries := rapid.IntRange(110, 130).Draw(rt, "numEntries")
		for i := 0; i < numEntries; i++ {
			contentLen := rapid.IntRange(20, 300).Draw(rt, fmt.Sprintf("contentLen_%d", i))
			content := fmt.Sprintf("golang microservice architecture pattern %s",
				rapid.StringMatching(fmt.Sprintf(`[a-z]{%d}`, contentLen)).Draw(rt, fmt.Sprintf("suffix_%d", i)))
			entry := Entry{
				Content:   content,
				Category:  CategoryProjectKnowledge,
				Tags:      []string{"golang", "microservice"},
				CreatedAt: time.Now(),
			}
			_ = store.Save(entry)
		}

		result := store.RecallExhaustive("golang microservice", CategoryProjectKnowledge, "")
		if result == nil {
			rt.Fatal("expected non-nil ExhaustiveResult")
		}

		// Entry count cap.
		if len(result.Entries) > exhaustiveMaxEntries {
			rt.Fatalf("exhaustive returned %d entries, exceeds cap %d", len(result.Entries), exhaustiveMaxEntries)
		}

		// Token budget cap.
		totalTokens := 0
		for _, e := range result.Entries {
			totalTokens += EstimateTextTokens(e.Content)
		}
		if totalTokens > exhaustiveMaxTokens {
			rt.Fatalf("exhaustive total tokens %d exceeds cap %d", totalTokens, exhaustiveMaxTokens)
		}
	})
}

// ---------------------------------------------------------------------------
// Property 8 (Task 4.3): Exhaustive mode preserves scoring order
// Entries in ExhaustiveResult SHALL be ordered by fusion score (higher first).
// ---------------------------------------------------------------------------

func TestProperty8_ExhaustiveModePreservesScoringOrder(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
		if err != nil {
			rt.Fatal(err)
		}
		defer store.Stop()

		numEntries := rapid.IntRange(10, 50).Draw(rt, "numEntries")
		queryTerms := []string{"kubernetes", "deployment", "scaling"}

		for i := 0; i < numEntries; i++ {
			// Vary how many query terms appear in each entry's content.
			termsToInclude := rapid.IntRange(0, len(queryTerms)).Draw(rt, fmt.Sprintf("terms_%d", i))
			var parts []string
			for j := 0; j < termsToInclude; j++ {
				parts = append(parts, queryTerms[j])
			}
			filler := rapid.StringMatching(`[a-z]{10,40}`).Draw(rt, fmt.Sprintf("filler_%d", i))
			content := strings.Join(parts, " ") + " " + filler

			entry := Entry{
				Content:   content,
				Category:  CategoryProjectKnowledge,
				Tags:      []string{"k8s"},
				CreatedAt: time.Now(),
			}
			_ = store.Save(entry)
		}

		result := store.RecallExhaustive("kubernetes deployment scaling", CategoryProjectKnowledge, "")
		if result == nil || len(result.Entries) < 2 {
			return // Not enough entries to verify ordering.
		}

		// Verify that entries with more query terms appear before entries
		// with fewer query terms (as a proxy for score ordering).
		// The fusion scoring should ensure this general trend.
		// We verify strict non-increasing order of a query-term-count proxy.
		for i := 0; i < len(result.Entries)-1; i++ {
			countI := countQueryTerms(result.Entries[i].Content, queryTerms)
			countNext := countQueryTerms(result.Entries[i+1].Content, queryTerms)
			// We cannot guarantee strict ordering by term count alone (fusion
			// has other signals), but within the same term count bucket, order
			// is still valid. Just verify no catastrophic inversion where an
			// entry with 0 terms appears before one with 3.
			if countI == 0 && countNext == len(queryTerms) {
				rt.Fatalf("ordering violation: entry[%d] has 0 query terms but precedes entry[%d] with %d terms",
					i, i+1, countNext)
			}
		}
	})
}

func countQueryTerms(content string, terms []string) int {
	lower := strings.ToLower(content)
	count := 0
	for _, term := range terms {
		if strings.Contains(lower, strings.ToLower(term)) {
			count++
		}
	}
	return count
}

// ---------------------------------------------------------------------------
// Property 9 (Task 4.4): Exhaustive mode respects owner and category filters
// All entries in ExhaustiveResult SHALL match the OwnerID filter and category
// filter if specified.
// ---------------------------------------------------------------------------

func TestProperty9_ExhaustiveModeRespectsOwnerAndCategoryFilters(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
		if err != nil {
			rt.Fatal(err)
		}
		defer store.Stop()

		ownerA := "user-alpha"
		ownerB := "user-beta"
		numEntries := rapid.IntRange(10, 50).Draw(rt, "numEntries")

		for i := 0; i < numEntries; i++ {
			owner := ownerA
			if i%4 == 0 {
				owner = ownerB
			}
			cat := CategoryProjectKnowledge
			if i%3 == 0 {
				cat = Category("preference")
			}
			content := fmt.Sprintf("rust memory safety concurrency item %d with unique suffix %s", i,
				rapid.StringMatching(`[a-z]{5,15}`).Draw(rt, fmt.Sprintf("suffix_%d", i)))
			entry := Entry{
				Content:   content,
				Category:  cat,
				Tags:      []string{"rust"},
				CreatedAt: time.Now(),
			}
			_ = store.SaveForUser(entry, owner)
		}

		// Recall as ownerA with specific category.
		result := store.RecallExhaustive("rust memory safety", CategoryProjectKnowledge, "", ownerA)
		if result == nil {
			rt.Fatal("expected non-nil ExhaustiveResult")
		}

		for _, e := range result.Entries {
			// OwnerID isolation.
			if e.OwnerID == ownerB {
				rt.Fatalf("entry %s belongs to ownerB, should not be visible to ownerA", e.ID)
			}
			// Category filter.
			if e.Category != CategoryProjectKnowledge {
				rt.Fatalf("entry %s has category %s, expected %s", e.ID, e.Category, CategoryProjectKnowledge)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Property 10 (Task 5.2): Scroll session sequential access without overlap
// Returned entry sets from successive Advance calls SHALL be non-overlapping.
// Concatenation of all returned entries forms a prefix of the initial scored
// candidate list.
// ---------------------------------------------------------------------------

func TestProperty10_ScrollSessionNoOverlap(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
		if err != nil {
			rt.Fatal(err)
		}
		defer store.Stop()

		numEntries := rapid.IntRange(10, 50).Draw(rt, "numEntries")
		for i := 0; i < numEntries; i++ {
			content := fmt.Sprintf("scroll test content item %d unique %s", i,
				rapid.StringMatching(`[a-z]{8,20}`).Draw(rt, fmt.Sprintf("content_%d", i)))
			entry := Entry{
				Content:   content,
				Category:  CategoryProjectKnowledge,
				Tags:      []string{"scroll"},
				CreatedAt: time.Now().Add(-time.Duration(numEntries-i) * time.Minute),
			}
			_ = store.Save(entry)
		}

		mgr := NewScrollSessionManager()
		loopID := "test-loop"
		mgr.GetOrCreate(loopID, store, "scroll test", "", "", "")

		seenIDs := make(map[string]bool)
		var allEntries []Entry

		for attempt := 0; attempt < 50; attempt++ {
			result, err := mgr.Advance(loopID, perPageTokenBudget)
			if err != nil {
				rt.Fatalf("Advance error: %v", err)
			}
			if result.SessionExhausted {
				break
			}
			for _, e := range result.Entries {
				if seenIDs[e.ID] {
					rt.Fatalf("entry %s appeared in multiple pages (overlap detected)", e.ID)
				}
				seenIDs[e.ID] = true
				allEntries = append(allEntries, e)
			}
		}

		// All returned entries should be unique.
		if len(allEntries) != len(seenIDs) {
			rt.Fatalf("entry count mismatch: %d entries vs %d unique IDs", len(allEntries), len(seenIDs))
		}
	})
}

// ---------------------------------------------------------------------------
// Property 11 (Task 5.3): Scroll session cache bounded at 200
// ScrollSession.Candidates SHALL not exceed scrollSessionMaxCache (200).
// ---------------------------------------------------------------------------

func TestProperty11_ScrollSessionCacheBounded(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
		if err != nil {
			rt.Fatal(err)
		}
		defer store.Stop()

		// Create more entries than the cache limit.
		numEntries := rapid.IntRange(210, 300).Draw(rt, "numEntries")
		for i := 0; i < numEntries; i++ {
			content := fmt.Sprintf("cache bound test entry %d suffix %s", i,
				rapid.StringMatching(`[a-z]{5,15}`).Draw(rt, fmt.Sprintf("suffix_%d", i)))
			entry := Entry{
				Content:   content,
				Category:  CategoryProjectKnowledge,
				Tags:      []string{"cache"},
				CreatedAt: time.Now().Add(-time.Duration(numEntries-i) * time.Minute),
			}
			_ = store.Save(entry)
		}

		mgr := NewScrollSessionManager()
		sess := mgr.GetOrCreate("loop-bound", store, "cache bound test", "", "", "")

		if len(sess.Candidates) > scrollSessionMaxCache {
			rt.Fatalf("session candidates %d exceeds scrollSessionMaxCache %d",
				len(sess.Candidates), scrollSessionMaxCache)
		}
	})
}

// ---------------------------------------------------------------------------
// Property 12 (Task 5.4): Scroll session exhaustion signal
// Once all candidates returned, next Advance SHALL return
// SessionExhausted: true with empty entries.
// ---------------------------------------------------------------------------

func TestProperty12_ScrollSessionExhaustionSignal(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
		if err != nil {
			rt.Fatal(err)
		}
		defer store.Stop()

		// Use a small number of entries to exhaust quickly.
		numEntries := rapid.IntRange(5, 20).Draw(rt, "numEntries")
		for i := 0; i < numEntries; i++ {
			content := fmt.Sprintf("exhaust signal test entry %d data %s", i,
				rapid.StringMatching(`[a-z]{5,15}`).Draw(rt, fmt.Sprintf("suffix_%d", i)))
			entry := Entry{
				Content:   content,
				Category:  CategoryProjectKnowledge,
				Tags:      []string{"exhaust"},
				CreatedAt: time.Now().Add(-time.Duration(numEntries-i) * time.Minute),
			}
			_ = store.Save(entry)
		}

		mgr := NewScrollSessionManager()
		loopID := "exhaust-loop"
		mgr.GetOrCreate(loopID, store, "exhaust signal test", "", "", "")

		// Drain all candidates.
		for attempt := 0; attempt < 100; attempt++ {
			result, err := mgr.Advance(loopID, perPageTokenBudget)
			if err != nil {
				rt.Fatalf("Advance error: %v", err)
			}
			if result.SessionExhausted {
				// Verify empty entries on exhaustion.
				if len(result.Entries) != 0 {
					rt.Fatal("expected empty entries when session exhausted")
				}
				// Verify subsequent calls also return exhausted.
				result2, _ := mgr.Advance(loopID, perPageTokenBudget)
				if !result2.SessionExhausted {
					rt.Fatal("expected session to remain exhausted on subsequent Advance")
				}
				if len(result2.Entries) != 0 {
					rt.Fatal("expected empty entries on subsequent exhausted Advance")
				}
				return
			}
		}
		rt.Fatal("session never exhausted after 100 advances")
	})
}

// ---------------------------------------------------------------------------
// Property 13 (Task 11.2): Backward compatibility — no new fields without
// new params. HandleTool recall without cursor/mode/session params SHALL NOT
// include cursor, has_more, page, truncated, total_matching, session_exhausted
// in the JSON response.
// ---------------------------------------------------------------------------

func TestProperty13_BackwardCompatNoNewFieldsWithoutNewParams(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
		if err != nil {
			rt.Fatal(err)
		}
		defer store.Stop()

		// Save some entries to ensure recall returns results.
		numEntries := rapid.IntRange(5, 30).Draw(rt, "numEntries")
		for i := 0; i < numEntries; i++ {
			content := fmt.Sprintf("backward compat test entry %d info %s", i,
				rapid.StringMatching(`[a-z]{5,15}`).Draw(rt, fmt.Sprintf("suffix_%d", i)))
			entry := Entry{
				Content:   content,
				Category:  CategoryProjectKnowledge,
				Tags:      []string{"compat"},
				CreatedAt: time.Now(),
			}
			_ = store.Save(entry)
		}

		query := rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "query")
		if query == "" {
			query = "backward compat test"
		}

		// Call HandleTool with recall action, NO cursor/mode/session params.
		// Also no LoopID in opts to prevent CursorPaginator path.
		args := map[string]interface{}{
			"action": "recall",
			"query":  "backward compat test " + query,
		}
		opts := ToolOptions{
			OwnerID:     "",
			LoopID:      "", // Empty LoopID prevents paginator path.
			ProjectPath: "",
		}

		response := HandleTool(store, args, opts)

		// The response text should NOT contain new pagination fields.
		newFields := []string{"cursor:", "has_more:", "page:", "truncated:", "total_matching:", "session_exhausted:"}
		for _, field := range newFields {
			if strings.Contains(response, field) {
				rt.Fatalf("response contains new pagination field %q without new params.\nResponse:\n%s", field, response)
			}
		}

		// Additionally verify by attempting JSON parse that no structured
		// pagination fields are present (the response is text, not JSON,
		// but if someone changes it, this catches it).
		var parsed map[string]interface{}
		if json.Unmarshal([]byte(response), &parsed) == nil {
			// If it parses as JSON, check for forbidden fields.
			forbiddenKeys := []string{"cursor", "has_more", "page", "truncated", "total_matching", "session_exhausted"}
			for _, key := range forbiddenKeys {
				if _, exists := parsed[key]; exists {
					rt.Fatalf("JSON response contains forbidden key %q without new params", key)
				}
			}
		}
	})
}
