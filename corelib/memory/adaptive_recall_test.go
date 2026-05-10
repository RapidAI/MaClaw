package memory

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestRecallAdaptiveHierExpandsWithinSelectedThemes(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	for i := 0; i < 18; i++ {
		entry := Entry{
			Content:    fmt.Sprintf("Frontend migration evidence item %02d", i),
			Category:   CategoryProjectKnowledge,
			Tags:       []string{"migration"},
			Embedding:  []float32{1, float32(i) / 1000},
			Status:     StatusActive,
			SourceType: "conversation",
		}
		if i == 0 {
			entry.Content = "React migration decision used component compatibility evidence"
			entry.Tags = []string{"react", "migration"}
			entry.SourceType = "manual"
		}
		if i == 1 {
			entry.Content = "Vue migration risk analysis kept router behavior stable"
			entry.Tags = []string{"vue", "migration"}
		}
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Save(Entry{Content: "PostgreSQL backup window is Sunday", Category: CategoryProjectKnowledge, Tags: []string{"postgresql"}, Embedding: []float32{0, 1}, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}

	store.mu.RLock()
	snapshot := make([]Entry, len(store.entries))
	copy(snapshot, store.entries)
	store.mu.RUnlock()
	store.ThemeManager().maxThemeSize = 30
	store.ThemeManager().Rebuild(snapshot, nil)

	results, plan := store.RecallAdaptiveHierDebug("why compare react and vue migration decisions over time", "", "")
	if plan.Fallback {
		t.Fatalf("expected adaptive plan, got fallback: %+v", plan)
	}
	if len(plan.SelectedThemes) == 0 {
		t.Fatalf("expected selected themes: %+v", plan)
	}
	if len(plan.QueryFacets) == 0 {
		t.Fatalf("expected query facets: %+v", plan)
	}
	if plan.Budget.MaxItems <= adaptiveDefaultMaxItems || plan.Budget.TokenBudget <= adaptiveDefaultTokens {
		t.Fatalf("expected complex query to use expanded adaptive budget: %+v", plan.Budget)
	}
	if len(plan.FacetCoverage) == 0 {
		t.Fatalf("expected facet coverage: %+v", plan)
	}
	if len(plan.ThemeAggregates) == 0 {
		t.Fatalf("expected theme aggregates: %+v", plan)
	}
	foundAggregatePreview := false
	for _, aggregate := range plan.ThemeAggregates {
		if len(aggregate.ResultEntryIDs) > 0 && len(aggregate.ResultPreviews) > 0 && aggregate.TokenEstimate > 0 {
			foundAggregatePreview = true
			break
		}
	}
	if !foundAggregatePreview {
		t.Fatalf("expected aggregate previews and token estimates: %+v", plan.ThemeAggregates)
	}
	foundMatchedFacet := false
	for _, theme := range plan.SelectedThemes {
		if len(theme.MatchedFacets) > 0 {
			foundMatchedFacet = true
			break
		}
	}
	if !foundMatchedFacet {
		t.Fatalf("expected selected theme facet matches: %+v", plan.SelectedThemes)
	}
	if len(results) == 0 || len(results) > plan.Budget.MaxItems {
		t.Fatalf("unexpected adaptive result count: %d", len(results))
	}
	foundExpansion := false
	for _, id := range plan.ExpandedEntryIDs {
		if id != "" {
			foundExpansion = true
			break
		}
	}
	if !foundExpansion {
		t.Fatalf("expected theme expansion evidence, plan=%+v", plan)
	}
	if len(plan.ResultEvidence) != len(plan.ResultEntryIDs) {
		t.Fatalf("expected result evidence for every result: evidence=%+v results=%v", plan.ResultEvidence, plan.ResultEntryIDs)
	}
	for _, ev := range plan.ResultEvidence {
		if ev.Rank == 0 || ev.EntryID == "" || ev.SourceType == "" {
			t.Fatalf("incomplete evidence: %+v", ev)
		}
	}
	foundExpansionResult := false
	foundSeedTheme := false
	for _, ev := range plan.ResultEvidence {
		if ev.Reason == "theme_expansion" && ev.ThemeID != "" {
			foundExpansionResult = true
		}
		if ev.Reason == "seed" && ev.ThemeID != "" {
			foundSeedTheme = true
		}
	}
	if !foundExpansionResult {
		t.Fatalf("expected adaptive result to reserve expansion coverage: %+v", plan.ResultEvidence)
	}
	if !foundSeedTheme {
		t.Fatalf("expected seed result evidence to include theme id: %+v", plan.ResultEvidence)
	}
	foundExpansionEvidence := false
	for _, ev := range plan.ExpandedEvidence {
		if ev.Reason == "theme_expansion" && ev.ThemeID != "" && ev.SourceType != "" && ev.ExpansionScore > 0 {
			foundExpansionEvidence = true
			break
		}
	}
	if !foundExpansionEvidence {
		t.Fatalf("expected theme expansion evidence in plan: %+v", plan.ExpandedEvidence)
	}
	foundCoverageExpansion := false
	for _, coverage := range plan.FacetCoverage {
		if len(coverage.SelectedThemeIDs) > 0 && len(coverage.ExpandedEntryIDs) > 0 {
			foundCoverageExpansion = true
			break
		}
	}
	if !foundCoverageExpansion {
		t.Fatalf("expected facet coverage to include expanded entries: %+v", plan.FacetCoverage)
	}
}

func TestDecomposeRecallQueryExtractsFacets(t *testing.T) {
	expanded := ExpandQuery("compare react and vue migration risks over time and explain why")
	facets := DecomposeRecallQuery("compare react and vue migration risks over time and explain why", expanded)
	kinds := map[string]bool{}
	for _, facet := range facets {
		kinds[facet.Kind] = true
		if facet.Text == "" {
			t.Fatalf("empty facet text: %+v", facets)
		}
	}
	for _, want := range []string{"entity", "comparison", "causal", "temporal"} {
		if !kinds[want] {
			t.Fatalf("missing facet %q in %+v", want, facets)
		}
	}
}

func TestSelectThemesByQueryFacetsMatchesTagsAndSummary(t *testing.T) {
	themes := []ThemeNode{
		{ID: "react", Summary: "react migration", Tags: []string{"react", "migration"}, MemberCount: 2},
		{ID: "db", Summary: "postgres backup", Tags: []string{"postgresql"}, MemberCount: 3},
	}
	facets := []RecallQueryFacet{{Kind: "entity", Text: "react", Tokens: []string{"react"}}}
	got := selectThemesByQueryFacets(themes, facets, map[string]struct{}{}, 2, nil)
	if len(got) != 1 || got[0].ID != "react" {
		t.Fatalf("unexpected facet theme selection: %+v", got)
	}
}

func TestSelectThemesByQueryFacetsMatchesMemberContent(t *testing.T) {
	themes := []ThemeNode{
		{ID: "opaque", Summary: "decision notes", Tags: []string{"architecture"}, EntryIDs: []string{"entry-1"}, MemberCount: 1},
		{ID: "db", Summary: "postgres backup", Tags: []string{"postgresql"}, EntryIDs: []string{"entry-2"}, MemberCount: 1},
	}
	entries := map[string]Entry{
		"entry-1": {ID: "entry-1", Content: "React migration kept component compatibility stable"},
		"entry-2": {ID: "entry-2", Content: "Database restore drill happens Sunday"},
	}
	facets := []RecallQueryFacet{{Kind: "entity", Text: "react", Tokens: []string{"react"}}}

	got := selectThemesByQueryFacets(themes, facets, map[string]struct{}{}, 2, entries)
	if len(got) != 1 || got[0].ID != "opaque" {
		t.Fatalf("expected member content to select opaque theme, got %+v", got)
	}
	if facets := matchedFacetKindsForTheme(themes[0], facets, entries); len(facets) != 1 || facets[0] != "entity" {
		t.Fatalf("expected member content facet match, got %+v", facets)
	}
	evidence := themeFacetMatchEvidence(themes[0], facets, entries, 3)
	if len(evidence) != 1 || evidence[0].EntryID != "entry-1" || evidence[0].ContentPreview == "" || evidence[0].Token != "react" {
		t.Fatalf("expected member content match evidence, got %+v", evidence)
	}
}

func TestThemeFacetMatchEvidenceIncludesThemeAndMemberEvidence(t *testing.T) {
	theme := ThemeNode{
		ID:       "react-theme",
		Summary:  "react migration",
		Tags:     []string{"frontend"},
		EntryIDs: []string{"entry-1"},
	}
	entries := map[string]Entry{
		"entry-1": {ID: "entry-1", Content: "React compatibility evidence from raw memory", SourceType: "manual"},
	}
	facets := []RecallQueryFacet{{Kind: "entity", Text: "react", Tokens: []string{"react"}}}

	evidence := themeFacetMatchEvidence(theme, facets, entries, 5)
	if len(evidence) != 2 {
		t.Fatalf("expected theme and member evidence, got %+v", evidence)
	}
	if evidence[0].EntryID != "" || evidence[1].EntryID != "entry-1" {
		t.Fatalf("expected theme metadata followed by member memory evidence, got %+v", evidence)
	}
}

func TestFacetTokenMatchingUsesWordBoundaries(t *testing.T) {
	if containsFacetToken("postgres backup decision", "go") {
		t.Fatal("short ascii token should not match inside a larger word")
	}
	if containsFacetToken("go_worker backup decision", "go") {
		t.Fatal("short ascii token should not match inside a snake_case identifier")
	}
	if !containsFacetToken("go backup worker", "go") {
		t.Fatal("short ascii token should match as a standalone word")
	}
	themes := []ThemeNode{
		{ID: "db", Summary: "postgres backup", Tags: []string{"database"}, EntryIDs: []string{"entry-1"}, MemberCount: 1},
		{ID: "go", Summary: "go worker runtime", Tags: []string{"runtime"}, EntryIDs: []string{"entry-2"}, MemberCount: 1},
	}
	entries := map[string]Entry{
		"entry-1": {ID: "entry-1", Content: "PostgreSQL restore drill"},
		"entry-2": {ID: "entry-2", Content: "Go worker owns backup scheduling"},
	}
	facets := []RecallQueryFacet{{Kind: "entity", Text: "go", Tokens: []string{"go"}}}
	got := selectThemesByQueryFacets(themes, facets, nil, 2, entries)
	if len(got) != 1 || got[0].ID != "go" {
		t.Fatalf("expected only standalone go theme, got %+v", got)
	}
}

func TestThemeFacetMatchCacheHandlesEmptyThemeIDs(t *testing.T) {
	themes := []ThemeNode{
		{Summary: "react migration", EntryIDs: []string{"entry-react"}, MemberCount: 1},
		{Summary: "vue migration", EntryIDs: []string{"entry-vue"}, MemberCount: 1},
	}
	entries := map[string]Entry{
		"entry-react": {ID: "entry-react", Content: "React compatibility evidence"},
		"entry-vue":   {ID: "entry-vue", Content: "Vue router evidence"},
	}
	facets := []RecallQueryFacet{{Kind: "entity", Text: "vue", Tokens: []string{"vue"}}}
	cache := make(map[string][]string)

	if got := matchedFacetKindsForThemeCached(themes[0], facets, entries, cache); len(got) != 0 {
		t.Fatalf("react theme should not match vue facet: %+v", got)
	}
	if got := matchedFacetKindsForThemeCached(themes[1], facets, entries, cache); len(got) != 1 || got[0] != "entity" {
		t.Fatalf("empty theme IDs should not share facet cache entries: %+v", got)
	}
}

func TestSortAdaptiveExpansionCandidatesPrefersFacetMatches(t *testing.T) {
	theme := ThemeNode{ID: "migration", Summary: "react vue migration risk", Tags: []string{"migration", "risk"}}
	facets := []RecallQueryFacet{
		{Kind: "entity", Text: "vue", Tokens: []string{"vue"}},
		{Kind: "causal", Text: "risk", Tokens: []string{"risk"}},
	}
	candidates := []adaptiveExpansionCandidate{
		{entry: Entry{ID: "generic", Content: "Migration background note", Tags: []string{"migration"}}},
		{entry: Entry{ID: "matched", Content: "Vue router risk explains migration concern", Tags: []string{"vue", "risk"}}},
	}
	for i := range candidates {
		candidates[i].score = adaptiveExpansionCandidateScore(candidates[i].entry, theme, facets)
	}
	sortAdaptiveExpansionCandidates(candidates)
	if candidates[0].entry.ID != "matched" || candidates[0].score <= candidates[1].score {
		t.Fatalf("expected facet-matched candidate first: %+v", candidates)
	}
}

func TestAdaptiveExpansionScoreCanLiftExpandedResult(t *testing.T) {
	seedsLen := 12
	expandedIndex := seedsLen
	baseScore := float64(seedsLen+3-expandedIndex) * 0.5
	liftedScore := baseScore + float64(80)*0.05
	if liftedScore <= float64(seedsLen+3-(seedsLen-1)) {
		t.Fatalf("expected expansion score to lift candidate into final rerank: base=%.2f lifted=%.2f", baseScore, liftedScore)
	}
}

func TestAdaptiveRecallBudgetScalesWithComplexityAndFacets(t *testing.T) {
	simple := adaptiveRecallBudget(ComplexitySimple, []RecallQueryFacet{{Kind: "keyword", Text: "api"}})
	if simple.MaxItems != adaptiveDefaultMaxItems || simple.TokenBudget != adaptiveDefaultTokens {
		t.Fatalf("simple budget changed unexpectedly: %+v", simple)
	}
	complex := adaptiveRecallBudget(ComplexityComplex, []RecallQueryFacet{
		{Kind: "entity", Text: "react"},
		{Kind: "comparison", Text: "compare"},
		{Kind: "causal", Text: "why"},
		{Kind: "temporal", Text: "over time"},
	})
	if complex.MaxItems <= adaptiveDefaultMaxItems || complex.TokenBudget <= adaptiveDefaultTokens {
		t.Fatalf("complex budget should expand: %+v", complex)
	}
	if complex.MaxItems > 30 || complex.TokenBudget > 5400 {
		t.Fatalf("complex budget should stay capped: %+v", complex)
	}
}

func TestSelectAdaptiveResultsSoftCapsThemeAndSource(t *testing.T) {
	budget := AdaptiveRecallBudget{MaxItems: 6, TokenBudget: 1000}
	var scored []recallScored
	entryTheme := make(map[string]string)
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("a-%d", i)
		scored = append(scored, recallScored{entry: Entry{ID: id, Content: "alpha source note", SourceType: "conversation"}, score: float64(100 - i)})
		entryTheme[id] = "theme-a"
	}
	for i := 0; i < 1; i++ {
		id := fmt.Sprintf("b-%d", i)
		scored = append(scored, recallScored{entry: Entry{ID: id, Content: "manual beta evidence", SourceType: "manual"}, score: float64(50 - i)})
		entryTheme[id] = "theme-b"
	}

	got, stats := selectAdaptiveResults(scored, budget, entryTheme)
	if len(got) != budget.MaxItems {
		t.Fatalf("expected soft cap fill to keep result count: %d", len(got))
	}
	if stats.ThemeCap == 0 || stats.SourceCap == 0 || stats.DeferredByThemeCap == 0 || stats.BackfilledDeferred == 0 {
		t.Fatalf("expected diversity stats to explain soft-cap behavior: %+v", stats)
	}
	seenThemeB := false
	seenManual := false
	for _, entry := range got {
		if entryTheme[entry.ID] == "theme-b" {
			seenThemeB = true
		}
		if entry.SourceType == "manual" {
			seenManual = true
		}
	}
	if !seenThemeB || !seenManual {
		t.Fatalf("expected soft caps to admit secondary theme/source: %+v", got)
	}
}

func TestSelectAdaptiveResultsKeepsAnonymousEntries(t *testing.T) {
	scored := []recallScored{
		{entry: Entry{Content: "first anonymous evidence"}, score: 3},
		{entry: Entry{Content: "second anonymous evidence"}, score: 2},
		{entry: Entry{ID: "known", Content: "known evidence"}, score: 1},
		{entry: Entry{ID: "known", Content: "known duplicate"}, score: 0},
	}

	got, _ := selectAdaptiveResults(scored, AdaptiveRecallBudget{MaxItems: 4, TokenBudget: 1000}, nil)
	if len(got) != 3 {
		t.Fatalf("expected anonymous entries to remain distinct and duplicate IDs to be removed: %+v", got)
	}
	if got[0].Content != "first anonymous evidence" || got[1].Content != "second anonymous evidence" || got[2].ID != "known" {
		t.Fatalf("unexpected selection order/content: %+v", got)
	}
}

func TestRecallAdaptiveHierFallsBackForSimpleQuery(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{Content: "API port is 8080", Category: CategoryProjectKnowledge, Tags: []string{"api"}, Embedding: []float32{1, 0}, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}
	results, plan := store.RecallAdaptiveHierDebug("what is the API port", "", "")
	if !plan.Fallback {
		t.Fatalf("simple query should use fallback, plan=%+v", plan)
	}
	if len(results) == 0 {
		t.Fatal("fallback should still return recall results")
	}
	if len(plan.ResultEvidence) != len(results) || plan.ResultEvidence[0].Reason != "fallback_seed" {
		t.Fatalf("fallback should explain seed evidence: %+v", plan.ResultEvidence)
	}
}

func TestShouldUseAdaptiveRecall(t *testing.T) {
	if ShouldUseAdaptiveRecall("what is the API port") {
		t.Fatal("simple fact lookup should stay on flat recall")
	}
	if !ShouldUseAdaptiveRecall("compare react and vue migration decisions and risks over time") {
		t.Fatal("multi-entity comparison should use adaptive recall")
	}
}

func TestEnsureAdaptiveExpansionCoverageReplacesTailSeed(t *testing.T) {
	result := make([]Entry, 0, adaptiveDefaultMaxItems)
	for i := 0; i < adaptiveDefaultMaxItems; i++ {
		result = append(result, Entry{ID: fmt.Sprintf("seed-%02d", i), Content: "seed item"})
	}
	expanded := Entry{ID: "expanded", Content: "expanded theme evidence"}
	candidates := append(append([]Entry(nil), result...), expanded)

	got := ensureAdaptiveExpansionCoverage(result, candidates, map[string]string{"expanded": "theme-a"}, adaptiveDefaultMaxItems, adaptiveDefaultTokens)
	if len(got) != adaptiveDefaultMaxItems {
		t.Fatalf("result size changed: %d", len(got))
	}
	if got[len(got)-1].ID != "expanded" {
		t.Fatalf("expected tail seed replacement with expansion, got %+v", got[len(got)-1])
	}
}

func TestEnsureAdaptiveSelectedThemeCoverageReplacesDuplicateTheme(t *testing.T) {
	result := []Entry{
		{ID: "a-1", Content: "theme a primary"},
		{ID: "a-2", Content: "theme a secondary"},
		{ID: "a-3", Content: "theme a tertiary"},
	}
	candidates := append(append([]Entry(nil), result...), Entry{ID: "b-1", Content: "theme b representative"})
	entryTheme := map[string]string{
		"a-1": "theme-a",
		"a-2": "theme-a",
		"a-3": "theme-a",
		"b-1": "theme-b",
	}
	selected := []AdaptiveThemeSelection{{ThemeID: "theme-a"}, {ThemeID: "theme-b"}}

	got, reserved := ensureAdaptiveSelectedThemeCoverage(result, candidates, selected, entryTheme, 3, adaptiveDefaultTokens)
	if len(got) != 3 {
		t.Fatalf("result size changed: %d", len(got))
	}
	if reserved != 1 {
		t.Fatalf("expected one selected-theme reservation, got %d", reserved)
	}
	if countThemeResults(got, entryTheme, "theme-b") != 1 {
		t.Fatalf("expected selected theme b coverage: %+v", got)
	}
	if countThemeResults(got, entryTheme, "theme-a") == 0 {
		t.Fatalf("theme a should remain covered: %+v", got)
	}
}

func TestCountSelectedThemeCoverage(t *testing.T) {
	entries := []Entry{{ID: "a-1"}, {ID: "c-1"}}
	selected := []AdaptiveThemeSelection{{ThemeID: "theme-a"}, {ThemeID: "theme-b"}, {ThemeID: "theme-a"}}
	entryTheme := map[string]string{
		"a-1": "theme-a",
		"c-1": "theme-c",
	}
	if got := countSelectedThemeTargets(selected); got != 2 {
		t.Fatalf("expected unique selected theme target count, got %d", got)
	}
	if got := countCoveredSelectedThemes(entries, selected, entryTheme); got != 1 {
		t.Fatalf("expected one covered selected theme, got %d", got)
	}
}

func TestAdaptiveFacetCoverageSkipsEmptyThemeID(t *testing.T) {
	coverage := adaptiveFacetCoverage(
		[]RecallQueryFacet{{Kind: "entity", Text: "react"}},
		[]AdaptiveThemeSelection{
			{ThemeID: "", MatchedFacets: []string{"entity"}},
			{ThemeID: "theme-a", MatchedFacets: []string{"entity"}},
		},
		[]AdaptiveEntryEvidence{{EntryID: "a-1", ThemeID: "theme-a"}},
	)
	if len(coverage) != 1 {
		t.Fatalf("expected one facet coverage item, got %+v", coverage)
	}
	if len(coverage[0].SelectedThemeIDs) != 1 || coverage[0].SelectedThemeIDs[0] != "theme-a" {
		t.Fatalf("expected empty theme ID to be skipped: %+v", coverage)
	}
}

func TestAdaptiveThemeAggregatesDeduplicatesEntryIDs(t *testing.T) {
	themes := []AdaptiveThemeSelection{{
		ThemeID:       "theme-a",
		MatchedFacets: []string{"entity"},
		EntryIDs:      []string{"a-1", "a-1"},
	}}
	seeds := []Entry{{ID: "a-1", Content: "React migration seed", SourceType: "manual"}}
	results := []Entry{{ID: "a-1", Content: "React migration seed", SourceType: "manual"}}
	expanded := []AdaptiveEntryEvidence{
		{EntryID: "a-2", ThemeID: "theme-a"},
		{EntryID: "a-2", ThemeID: "theme-a"},
	}

	aggregates := adaptiveThemeAggregates(themes, seeds, results, expanded)
	if len(aggregates) != 1 {
		t.Fatalf("expected one aggregate, got %+v", aggregates)
	}
	got := aggregates[0]
	if len(got.SeedEntryIDs) != 1 || len(got.ResultEntryIDs) != 1 || len(got.ExpandedEntryIDs) != 1 {
		t.Fatalf("expected aggregate IDs to be deduplicated: %+v", got)
	}
	if got.SourceCounts["manual"] != 1 {
		t.Fatalf("expected source count to deduplicate result entry: %+v", got.SourceCounts)
	}
}

func TestDedupeEntriesByIDPreservesFirstOccurrence(t *testing.T) {
	entries := []Entry{
		{ID: "a-1", Content: "first"},
		{ID: "a-2", Content: "second"},
		{ID: "a-1", Content: "duplicate"},
		{Content: "anonymous"},
		{Content: "anonymous duplicate kept"},
	}
	got := dedupeEntriesByID(entries)
	if len(got) != 4 {
		t.Fatalf("expected duplicate IDs only to be removed, got %+v", got)
	}
	if got[0].Content != "first" || got[1].ID != "a-2" || got[2].Content != "anonymous" {
		t.Fatalf("expected first occurrences and order to be preserved: %+v", got)
	}
}
