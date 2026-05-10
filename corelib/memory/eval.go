package memory

import "strings"

type RecallEvalCase struct {
	Name             string   `json:"name"`
	Query            string   `json:"query"`
	Category         Category `json:"category,omitempty"`
	ProjectPath      string   `json:"project_path,omitempty"`
	ExpectedContains []string `json:"expected_contains,omitempty"`
}

type RecallEvalReport struct {
	Cases      []RecallEvalCaseResult      `json:"cases"`
	Strategies map[string]RecallEvalMetric `json:"strategies"`
	Theme      RecallEvalThemeReport       `json:"theme"`
}

type RecallEvalThemeReport struct {
	Health      ThemeHealth           `json:"health"`
	IssueCount  int                   `json:"issue_count"`
	ActionCount int                   `json:"action_count"`
	Diagnostics ThemeDiagnosticReport `json:"diagnostics,omitempty"`
	Maintenance ThemeMaintenancePlan  `json:"maintenance,omitempty"`
}

type RecallMaintenanceEvalReport struct {
	Before      RecallEvalReport       `json:"before"`
	Maintenance ThemeMaintenanceResult `json:"maintenance"`
	After       RecallEvalReport       `json:"after"`
	Delta       RecallMaintenanceDelta `json:"delta"`
}

type RecallMaintenanceDelta struct {
	ThemeCoverageRate       float64 `json:"theme_coverage_rate"`
	ThemeIssueCount         int     `json:"theme_issue_count"`
	ThemeActionCount        int     `json:"theme_action_count"`
	HybridHitRate           float64 `json:"hybrid_hit_rate"`
	AdaptiveHitRate         float64 `json:"adaptive_hit_rate"`
	HybridAvgThemeRepeats   float64 `json:"hybrid_avg_theme_repeats"`
	AdaptiveAvgThemeRepeats float64 `json:"adaptive_avg_theme_repeats"`
}

type RecallEvalCaseResult struct {
	Name       string                         `json:"name"`
	Query      string                         `json:"query"`
	Strategies map[string]RecallEvalCaseScore `json:"strategies"`
}

type RecallEvalCaseScore struct {
	Hit                bool                    `json:"hit"`
	MatchedExpected    []string                `json:"matched_expected,omitempty"`
	ResultIDs          []string                `json:"result_ids,omitempty"`
	ResultCount        int                     `json:"result_count"`
	TokenEstimate      int                     `json:"token_estimate"`
	DuplicateThemes    int                     `json:"duplicate_themes"`
	AdaptiveFallback   bool                    `json:"adaptive_fallback,omitempty"`
	QueryFacets        int                     `json:"query_facets,omitempty"`
	BudgetMaxItems     int                     `json:"budget_max_items,omitempty"`
	BudgetTokens       int                     `json:"budget_tokens,omitempty"`
	Diversity          *AdaptiveDiversityStats `json:"diversity,omitempty"`
	FacetCoverage      map[string]int          `json:"facet_coverage,omitempty"`
	SelectedThemes     int                     `json:"selected_themes,omitempty"`
	AggregatedThemes   int                     `json:"aggregated_themes,omitempty"`
	ThemeReasons       map[string]int          `json:"theme_reasons,omitempty"`
	ThemeMatchEvidence int                     `json:"theme_match_evidence,omitempty"`
	ThemeMatchSources  map[string]int          `json:"theme_match_sources,omitempty"`
	ExpandedEntries    int                     `json:"expanded_entries,omitempty"`
	SourceCounts       map[string]int          `json:"source_counts,omitempty"`
	SeedResults        int                     `json:"seed_results,omitempty"`
	ExpandedResults    int                     `json:"expanded_results,omitempty"`
}

type RecallEvalMetric struct {
	Cases                     int            `json:"cases"`
	Hits                      int            `json:"hits"`
	HitRate                   float64        `json:"hit_rate"`
	AvgTokens                 float64        `json:"avg_tokens"`
	AvgResults                float64        `json:"avg_results"`
	AvgThemeRepeats           float64        `json:"avg_theme_repeats"`
	SelectedThemeCoverageRate float64        `json:"selected_theme_coverage_rate,omitempty"`
	AvgReservedSelectedThemes float64        `json:"avg_reserved_selected_themes,omitempty"`
	AvgThemeMatchEvidence     float64        `json:"avg_theme_match_evidence,omitempty"`
	ThemeMatchSources         map[string]int `json:"theme_match_sources,omitempty"`
}

func (s *Store) EvaluateRecallStrategies(cases []RecallEvalCase, limit int) RecallEvalReport {
	if limit <= 0 {
		limit = 10
	}
	rebuildEvalThemes(s)
	diagnostics := s.ThemeDiagnostics(50)
	maintenance := PlanThemeMaintenance(diagnostics, 20)
	report := RecallEvalReport{
		Strategies: map[string]RecallEvalMetric{
			"hybrid":   {},
			"adaptive": {},
		},
		Theme: RecallEvalThemeReport{
			Health:      diagnostics.Health,
			IssueCount:  len(diagnostics.Issues),
			ActionCount: len(maintenance.Actions),
			Diagnostics: diagnostics,
			Maintenance: maintenance,
		},
	}
	for _, tc := range cases {
		caseResult := RecallEvalCaseResult{
			Name:       tc.Name,
			Query:      tc.Query,
			Strategies: make(map[string]RecallEvalCaseScore),
		}

		hybrid := limitSearchResults(s.SearchByMode(tc.Query, SearchHybrid, tc.Category, tc.ProjectPath, limit), limit)
		adaptive, adaptivePlan := s.RecallAdaptiveHierDebug(tc.Query, tc.Category, tc.ProjectPath)
		adaptive = limitSearchResults(adaptive, limit)
		caseResult.Strategies["hybrid"] = scoreRecallEvalCase(s, hybrid, tc.ExpectedContains)
		adaptiveScore := scoreRecallEvalCase(s, adaptive, tc.ExpectedContains)
		adaptiveScore.AdaptiveFallback = adaptivePlan.Fallback
		adaptiveScore.QueryFacets = len(adaptivePlan.QueryFacets)
		adaptiveScore.BudgetMaxItems = adaptivePlan.Budget.MaxItems
		adaptiveScore.BudgetTokens = adaptivePlan.Budget.TokenBudget
		if adaptiveDiversityHasData(adaptivePlan.Diversity) {
			adaptiveScore.Diversity = &adaptivePlan.Diversity
		}
		adaptiveScore.FacetCoverage = adaptiveFacetCoverageCounts(adaptivePlan.FacetCoverage)
		adaptiveScore.SelectedThemes = len(adaptivePlan.SelectedThemes)
		adaptiveScore.AggregatedThemes = len(adaptivePlan.ThemeAggregates)
		adaptiveScore.ThemeReasons = adaptiveThemeReasonCounts(adaptivePlan.SelectedThemes)
		adaptiveScore.ThemeMatchEvidence = adaptiveThemeMatchEvidenceCount(adaptivePlan.SelectedThemes)
		adaptiveScore.ThemeMatchSources = adaptiveThemeMatchSourceCounts(adaptivePlan.SelectedThemes)
		adaptiveScore.ExpandedEntries = len(adaptivePlan.ExpandedEntryIDs)
		adaptiveScore.SourceCounts = adaptiveEvidenceSourceCounts(adaptivePlan.ResultEvidence)
		adaptiveScore.SeedResults, adaptiveScore.ExpandedResults = adaptiveEvidenceReasonCounts(adaptivePlan.ResultEvidence)
		caseResult.Strategies["adaptive"] = adaptiveScore
		report.Cases = append(report.Cases, caseResult)
	}

	for _, strategy := range []string{"hybrid", "adaptive"} {
		metric := RecallEvalMetric{Cases: len(report.Cases)}
		selectedThemeCoverageCases := 0
		themeMatchEvidenceCases := 0
		for _, c := range report.Cases {
			score := c.Strategies[strategy]
			if score.Hit {
				metric.Hits++
			}
			metric.AvgTokens += float64(score.TokenEstimate)
			metric.AvgResults += float64(score.ResultCount)
			metric.AvgThemeRepeats += float64(score.DuplicateThemes)
			if score.ThemeMatchEvidence > 0 {
				themeMatchEvidenceCases++
				metric.AvgThemeMatchEvidence += float64(score.ThemeMatchEvidence)
				mergeIntCounts(&metric.ThemeMatchSources, score.ThemeMatchSources)
			}
			if score.Diversity != nil && score.Diversity.SelectedThemeTargets > 0 {
				selectedThemeCoverageCases++
				metric.SelectedThemeCoverageRate += float64(score.Diversity.SelectedThemeCoveredAfter) / float64(score.Diversity.SelectedThemeTargets)
				metric.AvgReservedSelectedThemes += float64(score.Diversity.ReservedSelectedThemes)
			}
		}
		if metric.Cases > 0 {
			metric.HitRate = float64(metric.Hits) / float64(metric.Cases)
			metric.AvgTokens /= float64(metric.Cases)
			metric.AvgResults /= float64(metric.Cases)
			metric.AvgThemeRepeats /= float64(metric.Cases)
		}
		if selectedThemeCoverageCases > 0 {
			metric.SelectedThemeCoverageRate /= float64(selectedThemeCoverageCases)
			metric.AvgReservedSelectedThemes /= float64(selectedThemeCoverageCases)
		}
		if themeMatchEvidenceCases > 0 {
			metric.AvgThemeMatchEvidence /= float64(themeMatchEvidenceCases)
		}
		report.Strategies[strategy] = metric
	}
	return report
}

// EvaluateRecallStrategiesWithMaintenance evaluates recall before and after
// applying safe theme maintenance. This mutates the store only through
// ApplyThemeMaintenancePlan's conservative operations.
func (s *Store) EvaluateRecallStrategiesWithMaintenance(cases []RecallEvalCase, limit int, issueLimit int, actionLimit int) RecallMaintenanceEvalReport {
	before := s.EvaluateRecallStrategies(cases, limit)
	maintenance := s.ApplyThemeMaintenancePlan(issueLimit, actionLimit)
	after := s.EvaluateRecallStrategies(cases, limit)
	return RecallMaintenanceEvalReport{
		Before:      before,
		Maintenance: maintenance,
		After:       after,
		Delta:       recallMaintenanceDelta(before, after),
	}
}

func recallMaintenanceDelta(before RecallEvalReport, after RecallEvalReport) RecallMaintenanceDelta {
	return RecallMaintenanceDelta{
		ThemeCoverageRate:       after.Theme.Health.CoverageRate - before.Theme.Health.CoverageRate,
		ThemeIssueCount:         after.Theme.IssueCount - before.Theme.IssueCount,
		ThemeActionCount:        after.Theme.ActionCount - before.Theme.ActionCount,
		HybridHitRate:           after.Strategies["hybrid"].HitRate - before.Strategies["hybrid"].HitRate,
		AdaptiveHitRate:         after.Strategies["adaptive"].HitRate - before.Strategies["adaptive"].HitRate,
		HybridAvgThemeRepeats:   after.Strategies["hybrid"].AvgThemeRepeats - before.Strategies["hybrid"].AvgThemeRepeats,
		AdaptiveAvgThemeRepeats: after.Strategies["adaptive"].AvgThemeRepeats - before.Strategies["adaptive"].AvgThemeRepeats,
	}
}

func scoreRecallEvalCase(store *Store, entries []Entry, expected []string) RecallEvalCaseScore {
	score := RecallEvalCaseScore{
		ResultIDs:       recallResultIDs(entries),
		ResultCount:     len(entries),
		DuplicateThemes: countDuplicateThemes(store, entries),
	}
	for _, entry := range entries {
		score.TokenEstimate += EstimateTextTokens(entry.Content)
	}
	if len(expected) == 0 {
		score.Hit = len(entries) > 0
		return score
	}
	seen := make(map[string]struct{}, len(expected))
	for _, want := range expected {
		want = strings.ToLower(strings.TrimSpace(want))
		if want == "" {
			continue
		}
		for _, entry := range entries {
			if strings.Contains(strings.ToLower(entry.Content), want) {
				if _, ok := seen[want]; !ok {
					seen[want] = struct{}{}
					score.MatchedExpected = append(score.MatchedExpected, want)
				}
				break
			}
		}
	}
	score.Hit = len(seen) == len(nonEmptyExpected(expected))
	return score
}

func nonEmptyExpected(expected []string) []string {
	out := expected[:0]
	for _, e := range expected {
		if strings.TrimSpace(e) != "" {
			out = append(out, e)
		}
	}
	return out
}

func countDuplicateThemes(store *Store, entries []Entry) int {
	if store == nil || store.ThemeManager() == nil || len(entries) == 0 {
		return 0
	}
	entryTheme := make(map[string]string)
	for _, theme := range store.ThemeManager().Themes() {
		for _, id := range theme.EntryIDs {
			if _, ok := entryTheme[id]; !ok {
				entryTheme[id] = theme.ID
			}
		}
	}
	counts := make(map[string]int)
	repeats := 0
	for _, entry := range entries {
		tid := entryTheme[entry.ID]
		if tid == "" {
			continue
		}
		counts[tid]++
		if counts[tid] > 1 {
			repeats++
		}
	}
	return repeats
}

func rebuildEvalThemes(store *Store) {
	if store == nil || store.ThemeManager() == nil {
		return
	}
	store.ThemeManager().Rebuild(store.List("", ""), nil)
}

func adaptiveEvidenceSourceCounts(evidence []AdaptiveEntryEvidence) map[string]int {
	if len(evidence) == 0 {
		return nil
	}
	counts := make(map[string]int)
	seen := make(map[string]struct{}, len(evidence))
	for _, ev := range evidence {
		if ev.EntryID != "" {
			if _, ok := seen[ev.EntryID]; ok {
				continue
			}
			seen[ev.EntryID] = struct{}{}
		}
		source := strings.TrimSpace(ev.SourceType)
		if source == "" {
			source = string(ExperienceSourceUnknown)
		}
		counts[source]++
	}
	return counts
}

func adaptiveEvidenceReasonCounts(evidence []AdaptiveEntryEvidence) (seedResults int, expandedResults int) {
	seen := make(map[string]struct{}, len(evidence))
	for _, ev := range evidence {
		if ev.EntryID != "" {
			if _, ok := seen[ev.EntryID]; ok {
				continue
			}
			seen[ev.EntryID] = struct{}{}
		}
		switch ev.Reason {
		case "seed", "fallback_seed":
			seedResults++
		case "theme_expansion":
			expandedResults++
		}
	}
	return seedResults, expandedResults
}

func adaptiveDiversityHasData(stats AdaptiveDiversityStats) bool {
	return stats.ThemeCap != 0 ||
		stats.SourceCap != 0 ||
		stats.DeferredByThemeCap != 0 ||
		stats.DeferredBySourceCap != 0 ||
		stats.BackfilledDeferred != 0 ||
		stats.SelectedThemeCount != 0 ||
		stats.SelectedSourceCount != 0 ||
		stats.SelectedThemeTargets != 0 ||
		stats.SelectedThemeCoveredBefore != 0 ||
		stats.SelectedThemeCoveredAfter != 0 ||
		stats.ReservedSelectedThemes != 0
}

func adaptiveThemeReasonCounts(selections []AdaptiveThemeSelection) map[string]int {
	if len(selections) == 0 {
		return nil
	}
	counts := make(map[string]int)
	for _, selection := range selections {
		reason := strings.TrimSpace(selection.Reason)
		if reason == "" {
			reason = "unknown"
		}
		counts[reason]++
	}
	return counts
}

func adaptiveThemeMatchEvidenceCount(selections []AdaptiveThemeSelection) int {
	count := 0
	for _, selection := range selections {
		count += len(selection.MatchEvidence)
	}
	return count
}

func adaptiveThemeMatchSourceCounts(selections []AdaptiveThemeSelection) map[string]int {
	if len(selections) == 0 {
		return nil
	}
	counts := make(map[string]int)
	for _, selection := range selections {
		for _, ev := range selection.MatchEvidence {
			key := "theme"
			if ev.EntryID != "" {
				key = "memory"
			}
			counts[key]++
		}
	}
	if len(counts) == 0 {
		return nil
	}
	return counts
}

func mergeIntCounts(dst *map[string]int, src map[string]int) {
	if len(src) == 0 {
		return
	}
	if *dst == nil {
		*dst = make(map[string]int, len(src))
	}
	for key, count := range src {
		if key == "" || count == 0 {
			continue
		}
		(*dst)[key] += count
	}
}

func adaptiveFacetCoverageCounts(coverage []AdaptiveFacetCoverage) map[string]int {
	if len(coverage) == 0 {
		return nil
	}
	counts := make(map[string]int)
	for _, item := range coverage {
		if item.Kind == "" {
			continue
		}
		counts[item.Kind] = len(item.SelectedThemeIDs)
	}
	return counts
}
