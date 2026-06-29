package memory

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestThemeManagerRebuildSeparatesEmbeddingThemes(t *testing.T) {
	tm := NewThemeManager()
	entries := []Entry{
		{ID: "go1", Content: "Go concurrency worker pools", Category: CategoryProjectKnowledge, Tags: []string{"go"}, Embedding: []float32{1, 0}, Status: StatusActive},
		{ID: "go2", Content: "Goroutine scheduling details", Category: CategoryProjectKnowledge, Tags: []string{"go"}, Embedding: []float32{0.98, 0.02}, Status: StatusActive},
		{ID: "db1", Content: "PostgreSQL backup rotation", Category: CategoryProjectKnowledge, Tags: []string{"postgresql"}, Embedding: []float32{0, 1}, Status: StatusActive},
		{ID: "db2", Content: "Database restore drill", Category: CategoryProjectKnowledge, Tags: []string{"postgresql"}, Embedding: []float32{0.03, 0.97}, Status: StatusActive},
	}

	themes := tm.Rebuild(entries, nil)
	if len(themes) != 2 {
		t.Fatalf("expected 2 themes, got %d: %+v", len(themes), themes)
	}

	sizes := map[int]int{}
	for _, theme := range themes {
		sizes[len(theme.EntryIDs)]++
		if theme.MemberCount != len(theme.EntryIDs) {
			t.Fatalf("member count mismatch for %+v", theme)
		}
		if len(theme.Centroid) != 2 {
			t.Fatalf("expected centroid for theme %+v", theme)
		}
	}
	if sizes[2] != 2 {
		t.Fatalf("expected two themes of size 2, got size histogram %#v", sizes)
	}
}

func TestThemeManagerRebuildComputesNeighbors(t *testing.T) {
	tm := NewThemeManager()
	entries := []Entry{
		{ID: "a1", Content: "alpha one", Category: CategoryProjectKnowledge, Tags: []string{"alpha"}, Embedding: []float32{1, 0, 0}, Status: StatusActive},
		{ID: "a2", Content: "alpha two", Category: CategoryProjectKnowledge, Tags: []string{"alpha"}, Embedding: []float32{0.97, 0.03, 0}, Status: StatusActive},
		{ID: "b1", Content: "beta one", Category: CategoryProjectKnowledge, Tags: []string{"beta"}, Embedding: []float32{0.80, 0.20, 0}, Status: StatusActive},
		{ID: "b2", Content: "beta two", Category: CategoryProjectKnowledge, Tags: []string{"beta"}, Embedding: []float32{0.78, 0.22, 0}, Status: StatusActive},
		{ID: "c1", Content: "gamma one", Category: CategoryProjectKnowledge, Tags: []string{"gamma"}, Embedding: []float32{0, 0, 1}, Status: StatusActive},
		{ID: "c2", Content: "gamma two", Category: CategoryProjectKnowledge, Tags: []string{"gamma"}, Embedding: []float32{0, 0.03, 0.97}, Status: StatusActive},
	}

	themes := tm.Rebuild(entries, nil)
	if len(themes) < 2 {
		t.Fatalf("expected at least 2 themes, got %d", len(themes))
	}
	foundNeighbor := false
	for _, theme := range themes {
		if len(theme.Neighbors) > 0 && len(theme.NeighborSims) == len(theme.Neighbors) {
			foundNeighbor = true
			break
		}
	}
	if !foundNeighbor {
		t.Fatalf("expected at least one theme neighbor, got %+v", themes)
	}
}

func TestThemeManagerFallbackTagThemesForEntriesWithoutEmbedding(t *testing.T) {
	tm := NewThemeManager()
	entries := []Entry{
		{ID: "e1", Content: "redis cache config", Category: CategoryProjectKnowledge, Tags: []string{"redis", "cache"}, Status: StatusActive},
		{ID: "e2", Content: "redis eviction policy", Category: CategoryProjectKnowledge, Tags: []string{"redis"}, Status: StatusActive},
		{ID: "e3", Content: "redis persistence", Category: CategoryProjectKnowledge, Tags: []string{"redis"}, Status: StatusActive},
		{ID: "e4", Content: "single unrelated", Category: CategoryProjectKnowledge, Tags: []string{"misc"}, Status: StatusActive},
	}

	themes := tm.Rebuild(entries, nil)
	for _, theme := range themes {
		for _, tag := range theme.Tags {
			if tag == "redis" && len(theme.EntryIDs) == 3 {
				return
			}
		}
	}
	t.Fatalf("expected redis fallback theme, got %+v", themes)
}

func TestThemeManagerTopThemesSortsByMemberCount(t *testing.T) {
	tm := NewThemeManager()
	now := time.Now()
	tm.themes = []ThemeNode{
		{ID: "small", MemberCount: 1, UpdatedAt: now.Add(time.Hour)},
		{ID: "large", MemberCount: 3, UpdatedAt: now},
		{ID: "medium", MemberCount: 2, UpdatedAt: now},
	}

	got := tm.TopThemes(2)
	if len(got) != 2 || got[0].ID != "large" || got[1].ID != "medium" {
		t.Fatalf("unexpected top themes: %+v", got)
	}
}

func TestThemeManagerHealthReportsCoverageAndConnectivity(t *testing.T) {
	tm := NewThemeManager()
	entries := []Entry{
		{ID: "go1", Content: "Go concurrency", Category: CategoryProjectKnowledge, Tags: []string{"go"}, Embedding: []float32{1, 0}, Status: StatusActive},
		{ID: "go2", Content: "Goroutine scheduling", Category: CategoryProjectKnowledge, Tags: []string{"go"}, Embedding: []float32{0.98, 0.02}, Status: StatusActive},
		{ID: "db1", Content: "Postgres backup", Category: CategoryProjectKnowledge, Tags: []string{"postgresql"}, Embedding: []float32{0, 1}, Status: StatusActive},
		{ID: "identity", Content: "I am a developer", Category: CategorySelfIdentity, Tags: []string{"profile"}, Status: StatusActive},
		{ID: "old", Content: "Dormant memory", Category: CategoryProjectKnowledge, Tags: []string{"old"}, Status: StatusDormant},
	}
	tm.Rebuild(entries, nil)

	health := tm.Health(entries)
	if health.ActiveEligibleEntries != 3 {
		t.Fatalf("active eligible = %d, want 3", health.ActiveEligibleEntries)
	}
	if health.CoveredEntries != 3 || health.UncoveredEntries != 0 || health.CoverageRate != 1 {
		t.Fatalf("unexpected coverage: %+v", health)
	}
	if health.ThemeCount == 0 || health.MaxThemeSize == 0 || health.AverageThemeSize == 0 {
		t.Fatalf("expected theme size stats: %+v", health)
	}
	if health.ThemesWithCentroid == 0 {
		t.Fatalf("expected centroid-backed themes: %+v", health)
	}
}

func TestThemeManagerExplainThemesIncludesRepresentativeEvidence(t *testing.T) {
	tm := NewThemeManager()
	now := time.Now()
	entries := []Entry{
		{ID: "go1", Content: "Go concurrency worker pools keep request handling bounded", Category: CategoryProjectKnowledge, Tags: []string{"go"}, Embedding: []float32{1, 0}, Status: StatusActive, SourceType: "manual", SourceURL: "notes.md", UpdatedAt: now, AccessCount: 4},
		{ID: "go2", Content: "Goroutine scheduling detail", Category: CategoryProjectKnowledge, Tags: []string{"go"}, Embedding: []float32{0.98, 0.02}, Status: StatusActive, SourceType: "conversation", UpdatedAt: now.Add(-time.Hour), AccessCount: 1},
		{ID: "db1", Content: "PostgreSQL backup rotation", Category: CategoryProjectKnowledge, Tags: []string{"postgresql"}, Embedding: []float32{0, 1}, Status: StatusActive, SourceType: "tool_usage", UpdatedAt: now},
	}
	tm.Rebuild(entries, nil)

	explanations := tm.ExplainThemes(entries, 10, 2)
	if len(explanations) == 0 {
		t.Fatal("expected theme explanations")
	}
	found := false
	for _, explanation := range explanations {
		if len(explanation.Evidence) == 0 {
			t.Fatalf("expected evidence for explanation: %+v", explanation)
		}
		if explanation.Cohesion <= 0 {
			t.Fatalf("expected cohesion for explanation: %+v", explanation)
		}
		for _, ev := range explanation.Evidence {
			if ev.EntryID == "go1" {
				found = true
				if ev.ContentPreview == "" || ev.SourceType == "" || ev.SourceURL != "notes.md" || ev.Similarity <= 0 {
					t.Fatalf("unexpected go1 evidence: %+v", ev)
				}
			}
		}
	}
	if !found {
		t.Fatalf("expected go1 representative evidence, got %+v", explanations)
	}
}

func TestThemeManagerDiagnoseThemesFindsUncoveredEntries(t *testing.T) {
	tm := NewThemeManager()
	entries := []Entry{
		{ID: "go1", Content: "Go concurrency", Category: CategoryProjectKnowledge, Tags: []string{"go"}, Embedding: []float32{1, 0}, Status: StatusActive},
		{ID: "go2", Content: "Goroutine scheduling", Category: CategoryProjectKnowledge, Tags: []string{"go"}, Embedding: []float32{0.98, 0.02}, Status: StatusActive},
		{ID: "loose", Content: "Loose note without embedding", Category: CategoryProjectKnowledge, Tags: []string{"single"}, Status: StatusActive},
	}
	tm.Rebuild(entries, nil)

	report := tm.DiagnoseThemes(entries, 10)
	if report.Health.UncoveredEntries != 1 {
		t.Fatalf("uncovered entries = %d, want 1: %+v", report.Health.UncoveredEntries, report)
	}
	for _, issue := range report.Issues {
		if issue.Kind == "uncovered_entry" && issue.EntryID == "loose" && strings.Contains(issue.Suggestion, "embedding") {
			return
		}
	}
	t.Fatalf("expected uncovered entry diagnostic, got %+v", report.Issues)
}

func TestPlanThemeMaintenanceAggregatesDiagnostics(t *testing.T) {
	report := ThemeDiagnosticReport{
		Health: ThemeHealth{ActiveEligibleEntries: 2, CoveredEntries: 1, UncoveredEntries: 1, CoverageRate: 0.5},
		Issues: []ThemeIssue{
			{Kind: "low_coverage", Severity: "high", Message: "low"},
			{Kind: "uncovered_entry", Severity: "medium", EntryID: "loose"},
			{Kind: "theme_at_capacity", Severity: "medium", ThemeID: "theme_go", EntryIDs: []string{"go1", "go2"}},
		},
	}
	plan := PlanThemeMaintenance(report, 10)
	if len(plan.Actions) != 2 {
		t.Fatalf("actions = %+v, want 2", plan.Actions)
	}
	foundBackfill := false
	foundSplit := false
	for _, action := range plan.Actions {
		switch action.Action {
		case "backfill_theme_inputs":
			foundBackfill = true
			if action.Priority != "high" || !containsString(action.EntryIDs, "loose") || !containsString(action.IssueKinds, "low_coverage") {
				t.Fatalf("unexpected backfill action: %+v", action)
			}
		case "review_split_theme":
			foundSplit = true
			if action.ThemeID != "theme_go" || !containsString(action.EntryIDs, "go1") {
				t.Fatalf("unexpected split action: %+v", action)
			}
		}
	}
	if !foundBackfill || !foundSplit {
		t.Fatalf("missing expected actions: %+v", plan.Actions)
	}
}

func TestApplyThemeMaintenancePlanBackfillsEmbeddingsAndRebuilds(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{ID: "loose", Content: "Loose note needs embedding", Category: CategoryProjectKnowledge, Tags: []string{"loose"}, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}
	before := store.ThemeHealth()
	if before.CoveredEntries != 0 || before.UncoveredEntries != 1 {
		t.Fatalf("unexpected initial health: %+v", before)
	}

	store.mu.Lock()
	store.embedder = &fakeEmbedder{dim: 2}
	store.embedderGen++
	store.mu.Unlock()

	result := store.ApplyThemeMaintenancePlan(10, 10)
	if result.BackfilledEmbeddings != 1 || !result.RebuiltThemes {
		t.Fatalf("unexpected maintenance result: %+v", result)
	}
	if result.After.CoveredEntries != 1 || result.After.UncoveredEntries != 0 {
		t.Fatalf("unexpected post-maintenance health: %+v", result.After)
	}
	if !containsString(result.AppliedActions, "backfill_theme_inputs") || !containsString(result.AppliedActions, "rebuild_theme_layer") {
		t.Fatalf("missing applied actions: %+v", result.AppliedActions)
	}
}

func TestPipelineBuildsThemeLayer(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{Content: "Go memory allocation", Category: CategoryProjectKnowledge, Tags: []string{"go"}, Embedding: []float32{1, 0}, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Entry{Content: "Goroutine leak detection", Category: CategoryProjectKnowledge, Tags: []string{"go"}, Embedding: []float32{0.98, 0.02}, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Entry{Content: "Postgres backup", Category: CategoryProjectKnowledge, Tags: []string{"postgresql"}, Embedding: []float32{0, 1}, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}

	p := NewPipeline(store, nil, nil, nil, nil)
	p.RunOnce(context.Background())

	themes := store.ThemeManager().Themes()
	if len(themes) == 0 {
		t.Fatal("expected pipeline to build theme layer")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestStoreKeepsThemeLayerInSync(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	first := Entry{ID: "go1", Content: "Go memory allocation", Category: CategoryProjectKnowledge, Tags: []string{"go"}, Embedding: []float32{1, 0}, Status: StatusActive}
	second := Entry{ID: "go2", Content: "Goroutine leak detection", Category: CategoryProjectKnowledge, Tags: []string{"go"}, Embedding: []float32{0.98, 0.02}, Status: StatusActive}
	if err := store.Save(first); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(second); err != nil {
		t.Fatal(err)
	}
	// Theme layer is lazily rebuilt — call EnsureUpToDate before checking.
	store.ThemeManager().EnsureUpToDate(store.List("", ""), nil)
	if !themeLayerContainsEntry(store.ThemeManager().Themes(), "go2") {
		t.Fatalf("expected saved entry in theme layer: %+v", store.ThemeManager().Themes())
	}

	if err := store.Update("go2", "PostgreSQL restore drill", CategoryProjectKnowledge, []string{"postgresql"}); err != nil {
		t.Fatal(err)
	}
	store.ThemeManager().EnsureUpToDate(store.List("", ""), nil)
	themes := store.ThemeManager().Themes()
	if !themeLayerContainsEntry(themes, "go2") {
		t.Fatalf("expected updated entry to remain represented: %+v", themes)
	}

	if err := store.Delete("go2"); err != nil {
		t.Fatal(err)
	}
	store.WaitRebuild()
	store.ThemeManager().EnsureUpToDate(store.List("", ""), nil)
	if themeLayerContainsEntry(store.ThemeManager().Themes(), "go2") {
		t.Fatalf("deleted entry should be removed from theme layer: %+v", store.ThemeManager().Themes())
	}
}

func themeLayerContainsEntry(themes []ThemeNode, id string) bool {
	for _, theme := range themes {
		for _, entryID := range theme.EntryIDs {
			if entryID == id {
				return true
			}
		}
	}
	return false
}

func TestThemeManagerRebuildStoresEvidenceBoundary(t *testing.T) {
	tm := NewThemeManager()
	now := time.Now()
	entries := []Entry{
		{ID: "go1", Content: "Go migration", Category: CategoryProjectKnowledge, Tags: []string{"project:D:/work/a"}, Embedding: []float32{1, 0}, Scope: ScopeProject, OwnerID: "owner-1", SourceType: "manual", UpdatedAt: now, Status: StatusActive},
		{ID: "go2", Content: "Go tests", Category: CategoryProjectKnowledge, Tags: []string{"project:D:/work/a"}, Embedding: []float32{0.98, 0.02}, Scope: ScopeProject, OwnerID: "owner-1", SourceType: "manual", UpdatedAt: now, Status: StatusActive},
	}
	themes := tm.Rebuild(entries, nil)
	if len(themes) == 0 {
		t.Fatal("expected theme")
	}
	got := themes[0]
	if got.DerivedKind != "theme" || strings.Join(got.EvidenceIDs, ",") != "go1,go2" {
		t.Fatalf("missing theme evidence metadata: %+v", got)
	}
	if got.Boundary == nil || got.Boundary.OwnerID != "owner-1" || got.Boundary.ProjectPath == "" {
		t.Fatalf("unexpected theme boundary: %+v", got.Boundary)
	}
}
