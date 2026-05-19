package memory

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestEvaluateRecallStrategiesReportsMetrics(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	entries := []Entry{
		{Content: "React migration decision kept component compatibility", Category: CategoryProjectKnowledge, Tags: []string{"react", "migration"}, Embedding: []float32{1, 0}, Status: StatusActive, SourceType: "manual"},
		{Content: "Vue migration decision reduced bundle complexity", Category: CategoryProjectKnowledge, Tags: []string{"vue", "migration"}, Embedding: []float32{0.98, 0.02}, Status: StatusActive, SourceType: "conversation"},
		{Content: "PostgreSQL backup decision uses Sunday restore drills", Category: CategoryProjectKnowledge, Tags: []string{"postgresql", "backup"}, Embedding: []float32{0, 1}, Status: StatusActive},
		{Content: "API port is 8080", Category: CategoryProjectKnowledge, Tags: []string{"api"}, Embedding: []float32{0, 0.95}, Status: StatusActive},
	}
	for _, entry := range entries {
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}
	}

	report := store.EvaluateRecallStrategies([]RecallEvalCase{
		{
			Name:             "migration",
			Query:            "why compare react and vue migration decisions",
			ExpectedContains: []string{"react migration", "vue migration"},
		},
		{
			Name:             "api",
			Query:            "what is the API port",
			ExpectedContains: []string{"api port"},
		},
	}, 5)

	if len(report.Cases) != 2 {
		t.Fatalf("expected 2 cases, got %d", len(report.Cases))
	}
	if report.Strategies["hybrid"].Cases != 2 || report.Strategies["adaptive"].Cases != 2 {
		t.Fatalf("strategy metrics missing: %+v", report.Strategies)
	}
	if report.Strategies["adaptive"].AvgResults == 0 {
		t.Fatalf("adaptive metrics should include results: %+v", report.Strategies["adaptive"])
	}
	if report.Strategies["adaptive"].SelectedThemeCoverageRate == 0 {
		t.Fatalf("adaptive metrics should include selected-theme coverage: %+v", report.Strategies["adaptive"])
	}
	if report.Strategies["adaptive"].AvgThemeMatchEvidence == 0 || len(report.Strategies["adaptive"].ThemeMatchSources) == 0 {
		t.Fatalf("adaptive metrics should include theme match evidence summary: %+v", report.Strategies["adaptive"])
	}
	if report.Theme.Health.ThemeCount == 0 || report.Theme.Health.CoverageRate == 0 {
		t.Fatalf("eval should report theme health: %+v", report.Theme)
	}
	if report.Theme.Diagnostics.Health.ThemeCount != report.Theme.Health.ThemeCount {
		t.Fatalf("eval diagnostics should align with health: %+v", report.Theme)
	}
	if report.Cases[0].Strategies["adaptive"].SelectedThemes == 0 {
		t.Fatalf("complex adaptive case should report selected themes: %+v", report.Cases[0].Strategies["adaptive"])
	}
	if report.Cases[0].Strategies["adaptive"].QueryFacets == 0 || len(report.Cases[0].Strategies["adaptive"].FacetCoverage) == 0 {
		t.Fatalf("complex adaptive case should report facet coverage: %+v", report.Cases[0].Strategies["adaptive"])
	}
	if report.Cases[0].Strategies["adaptive"].BudgetMaxItems == 0 || report.Cases[0].Strategies["adaptive"].BudgetTokens == 0 {
		t.Fatalf("complex adaptive case should report adaptive budget: %+v", report.Cases[0].Strategies["adaptive"])
	}
	if report.Cases[0].Strategies["adaptive"].Diversity == nil || report.Cases[0].Strategies["adaptive"].Diversity.ThemeCap == 0 || report.Cases[0].Strategies["adaptive"].Diversity.SourceCap == 0 {
		t.Fatalf("complex adaptive case should report diversity stats: %+v", report.Cases[0].Strategies["adaptive"])
	}
	if report.Cases[0].Strategies["adaptive"].Diversity.SelectedThemeTargets == 0 || report.Cases[0].Strategies["adaptive"].Diversity.SelectedThemeCoveredAfter == 0 {
		t.Fatalf("complex adaptive case should report selected-theme coverage stats: %+v", report.Cases[0].Strategies["adaptive"].Diversity)
	}
	if report.Cases[0].Strategies["adaptive"].AggregatedThemes == 0 {
		t.Fatalf("complex adaptive case should report theme aggregates: %+v", report.Cases[0].Strategies["adaptive"])
	}
	if len(report.Cases[0].Strategies["adaptive"].ThemeReasons) == 0 {
		t.Fatalf("complex adaptive case should report theme reasons: %+v", report.Cases[0].Strategies["adaptive"])
	}
	if report.Cases[0].Strategies["adaptive"].ThemeMatchEvidence == 0 || len(report.Cases[0].Strategies["adaptive"].ThemeMatchSources) == 0 {
		t.Fatalf("complex adaptive case should report theme match evidence: %+v", report.Cases[0].Strategies["adaptive"])
	}
	if report.Cases[0].Strategies["adaptive"].SeedResults == 0 {
		t.Fatalf("adaptive case should report seed result count: %+v", report.Cases[0].Strategies["adaptive"])
	}
	if len(report.Cases[0].Strategies["adaptive"].SourceCounts) == 0 {
		t.Fatalf("adaptive case should report source counts: %+v", report.Cases[0].Strategies["adaptive"])
	}
	if !report.Cases[1].Strategies["adaptive"].AdaptiveFallback {
		t.Fatalf("simple adaptive case should report fallback: %+v", report.Cases[1].Strategies["adaptive"])
	}
	if report.Cases[1].Strategies["adaptive"].Diversity != nil {
		t.Fatalf("simple adaptive fallback should not report empty diversity stats: %+v", report.Cases[1].Strategies["adaptive"])
	}
}

func TestEvaluateRecallStrategiesReportsMemoryMatchEvidence(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	entries := []Entry{
		{Content: "React compatibility decision remains the migration anchor", Category: CategoryProjectKnowledge, Tags: []string{"frontend"}, Embedding: []float32{1, 0}, Status: StatusActive, SourceType: "manual"},
		{Content: "Component compatibility notes explain why migration stayed incremental", Category: CategoryProjectKnowledge, Tags: []string{"frontend"}, Embedding: []float32{0.99, 0.01}, Status: StatusActive, SourceType: "conversation"},
		{Content: "PostgreSQL restore drill runs Sunday", Category: CategoryProjectKnowledge, Tags: []string{"database"}, Embedding: []float32{0, 1}, Status: StatusActive},
	}
	for _, entry := range entries {
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}
	}

	report := store.EvaluateRecallStrategies([]RecallEvalCase{{
		Name:             "opaque-react",
		Query:            "why compare react compatibility migration over time",
		ExpectedContains: []string{"react compatibility"},
	}}, 5)

	score := report.Cases[0].Strategies["adaptive"]
	if score.ThemeMatchSources["memory"] == 0 {
		t.Fatalf("expected eval to report memory-backed theme match evidence: %+v", score)
	}
	if report.Strategies["adaptive"].ThemeMatchSources["memory"] == 0 {
		t.Fatalf("expected strategy summary to include memory-backed match evidence: %+v", report.Strategies["adaptive"])
	}
}

func TestAdaptiveEvidenceCountsDeduplicateEntryIDs(t *testing.T) {
	evidence := []AdaptiveEntryEvidence{
		{EntryID: "a-1", Reason: "seed", SourceType: "manual"},
		{EntryID: "a-1", Reason: "theme_expansion", SourceType: "conversation"},
		{EntryID: "a-2", Reason: "theme_expansion", SourceType: "conversation"},
	}
	sources := adaptiveEvidenceSourceCounts(evidence)
	if sources["manual"] != 1 || sources["conversation"] != 1 {
		t.Fatalf("expected source counts to deduplicate entry ids: %+v", sources)
	}
	seeds, expanded := adaptiveEvidenceReasonCounts(evidence)
	if seeds != 1 || expanded != 1 {
		t.Fatalf("expected reason counts to deduplicate entry ids, got seed=%d expanded=%d", seeds, expanded)
	}
}

func TestEvaluateRecallStrategiesWithMaintenanceReportsDelta(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{Content: "React migration decision kept component compatibility", Category: CategoryProjectKnowledge, Tags: []string{"react", "migration"}, Embedding: []float32{1, 0}, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Entry{Content: "Loose note without embedding", Category: CategoryProjectKnowledge, Tags: []string{"loose"}, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}

	report := store.EvaluateRecallStrategiesWithMaintenance([]RecallEvalCase{{
		Name:             "migration",
		Query:            "react migration decision",
		ExpectedContains: []string{"react migration"},
	}}, 5, 50, 20)

	if report.Before.Theme.Health.ActiveEligibleEntries == 0 || report.After.Theme.Health.ActiveEligibleEntries == 0 {
		t.Fatalf("expected theme health before/after: %+v", report)
	}
	if !report.Maintenance.RebuiltThemes {
		t.Fatalf("maintenance eval should rebuild themes: %+v", report.Maintenance)
	}
	if report.Delta.ThemeIssueCount > 0 {
		t.Fatalf("safe maintenance should not increase issue count in this case: %+v", report.Delta)
	}
}

func TestFormatRecallEvalReportForTool(t *testing.T) {
	report := RecallEvalReport{
		Strategies: map[string]RecallEvalMetric{
			"hybrid":   {Cases: 1, Hits: 1, HitRate: 1, AvgTokens: 12, AvgThemeRepeats: 0, AvgThemeMatchEvidence: 0},
			"adaptive": {Cases: 1, Hits: 1, HitRate: 1, AvgTokens: 10, AvgThemeRepeats: 1, AvgThemeMatchEvidence: 2},
		},
		Theme: RecallEvalThemeReport{Health: ThemeHealth{ThemeCount: 2, CoverageRate: 0.75, IsolatedThemes: 1}, IssueCount: 1, ActionCount: 1},
		Cases: []RecallEvalCaseResult{{
			Name:  "case-1",
			Query: "react migration decision",
			Strategies: map[string]RecallEvalCaseScore{
				"hybrid":   {Hit: true, ResultCount: 1, TokenEstimate: 12, MatchedExpected: []string{"react"}},
				"adaptive": {Hit: true, ResultCount: 1, TokenEstimate: 10, DuplicateThemes: 1, MatchedExpected: []string{"react"}, AdaptiveFallback: false, QueryFacets: 2, BudgetMaxItems: 8, BudgetTokens: 500, SelectedThemes: 1, AggregatedThemes: 1, ThemeMatchEvidence: 2},
			},
		}},
	}

	out := FormatRecallEvalReportForTool(report)
	for _, want := range []string{"Memory Recall Eval", "Theme health: coverage=0.75", "STRATEGY", "case-1: react migration decision", "adaptive hit=true"} {
		if !strings.Contains(out, want) {
			t.Fatalf("formatted eval missing %q:\n%s", want, out)
		}
	}
}

func TestFormatRecallMaintenanceEvalReportForTool(t *testing.T) {
	report := RecallMaintenanceEvalReport{
		Maintenance: ThemeMaintenanceResult{RequestedActions: 2, RebuiltThemes: true, BackfilledEmbeddings: 1, AppliedActions: []string{"rebuild_themes"}},
		Delta:       RecallMaintenanceDelta{ThemeCoverageRate: 0.25, HybridHitRate: 0.1, AdaptiveHitRate: 0.2, ThemeIssueCount: -1, ThemeActionCount: -1},
		Before:      RecallEvalReport{Strategies: map[string]RecallEvalMetric{"hybrid": {}, "adaptive": {}}, Theme: RecallEvalThemeReport{Health: ThemeHealth{}}, Cases: nil},
		After:       RecallEvalReport{Strategies: map[string]RecallEvalMetric{"hybrid": {}, "adaptive": {}}, Theme: RecallEvalThemeReport{Health: ThemeHealth{CoverageRate: 0.25}}, Cases: nil},
	}

	out := FormatRecallMaintenanceEvalReportForTool(report)
	for _, want := range []string{"Memory Recall Eval With Maintenance", "Maintenance: requested=2", "Delta: coverage=+0.25", "Before:", "After:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("formatted maintenance eval missing %q:\n%s", want, out)
		}
	}
}
