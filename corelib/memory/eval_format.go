package memory

import (
	"fmt"
	"strings"
)

// FormatRecallMaintenanceEvalReportForTool renders a recall evaluation with
// conservative theme maintenance before/after metrics.
func FormatRecallMaintenanceEvalReportForTool(report RecallMaintenanceEvalReport) string {
	var b strings.Builder
	b.WriteString("Memory Recall Eval With Maintenance\n")
	fmt.Fprintf(&b, "Maintenance: requested=%d rebuilt=%v backfilled=%d applied=%v skipped=%v\n",
		report.Maintenance.RequestedActions,
		report.Maintenance.RebuiltThemes,
		report.Maintenance.BackfilledEmbeddings,
		report.Maintenance.AppliedActions,
		report.Maintenance.SkippedActions)
	fmt.Fprintf(&b, "Delta: coverage=%+.2f hybrid_hit=%+.2f adaptive_hit=%+.2f hybrid_theme_repeat=%+.2f adaptive_theme_repeat=%+.2f issues=%+d actions=%+d\n",
		report.Delta.ThemeCoverageRate,
		report.Delta.HybridHitRate,
		report.Delta.AdaptiveHitRate,
		report.Delta.HybridAvgThemeRepeats,
		report.Delta.AdaptiveAvgThemeRepeats,
		report.Delta.ThemeIssueCount,
		report.Delta.ThemeActionCount)
	b.WriteString("\nBefore:\n")
	b.WriteString(FormatRecallEvalReportForTool(report.Before))
	b.WriteString("\nAfter:\n")
	b.WriteString(FormatRecallEvalReportForTool(report.After))
	return b.String()
}

// FormatRecallEvalReportForTool renders recall evaluation metrics in the same
// stable table used by host memory commands.
func FormatRecallEvalReportForTool(report RecallEvalReport) string {
	var b strings.Builder
	b.WriteString("Memory Recall Eval\n")
	fmt.Fprintf(&b, "Theme health: coverage=%.2f themes=%d issues=%d actions=%d isolated=%d\n",
		report.Theme.Health.CoverageRate,
		report.Theme.Health.ThemeCount,
		report.Theme.IssueCount,
		report.Theme.ActionCount,
		report.Theme.Health.IsolatedThemes)
	fmt.Fprintf(&b, "%-12s %-8s %-8s %-10s %-10s %-12s %-10s\n", "STRATEGY", "CASES", "HITS", "HIT_RATE", "TOKENS", "THEME_REPEAT", "MATCH_EV")
	b.WriteString(strings.Repeat("-", 72))
	b.WriteByte('\n')
	for _, name := range []string{"hybrid", "adaptive"} {
		metric := report.Strategies[name]
		fmt.Fprintf(&b, "%-12s %-8d %-8d %-10.2f %-10.1f %-12.1f %-10.1f\n",
			name, metric.Cases, metric.Hits, metric.HitRate, metric.AvgTokens, metric.AvgThemeRepeats, metric.AvgThemeMatchEvidence)
	}
	for _, c := range report.Cases {
		fmt.Fprintf(&b, "\n%s: %s\n", c.Name, c.Query)
		for _, name := range []string{"hybrid", "adaptive"} {
			score := c.Strategies[name]
			fmt.Fprintf(&b, "  %-8s hit=%v results=%d tokens=%d repeats=%d matched=%v",
				name, score.Hit, score.ResultCount, score.TokenEstimate, score.DuplicateThemes, score.MatchedExpected)
			if name == "adaptive" {
				fmt.Fprintf(&b, " fallback=%v facets=%d budget=%d/%d", score.AdaptiveFallback, score.QueryFacets, score.BudgetMaxItems, score.BudgetTokens)
				if score.Diversity != nil {
					fmt.Fprintf(&b, " diversity=%+v", *score.Diversity)
				}
				fmt.Fprintf(&b, " facet_coverage=%v themes=%d aggregates=%d expanded=%d seed_results=%d expanded_results=%d theme_reasons=%v match_evidence=%d match_sources=%v sources=%v",
					score.FacetCoverage, score.SelectedThemes, score.AggregatedThemes, score.ExpandedEntries, score.SeedResults, score.ExpandedResults, score.ThemeReasons, score.ThemeMatchEvidence, score.ThemeMatchSources, score.SourceCounts)
			}
			b.WriteByte('\n')
		}
	}
	return b.String()
}
