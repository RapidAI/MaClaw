package commands

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// RunMemory 执行 memory 子命令。
func RunMemory(args []string, dataDir string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui memory <list|search|recall|themes|eval|save|delete|compress|backup|auto-compress|stats|embed-status|graph|strength|infer>")
	}
	switch args[0] {
	case "list":
		return memoryList(dataDir, args[1:])
	case "search":
		return memorySearch(dataDir, args[1:])
	case "recall":
		return memoryRecall(dataDir, args[1:])
	case "themes":
		return memoryThemes(dataDir, args[1:])
	case "eval":
		return memoryEval(dataDir, args[1:])
	case "save":
		return memorySave(dataDir, args[1:])
	case "delete":
		return memoryDelete(dataDir, args[1:])
	case "compress":
		return memoryCompress(dataDir, args[1:])
	case "backup":
		return memoryBackup(dataDir, args[1:])
	case "auto-compress":
		return memoryAutoCompress(dataDir, args[1:])
	case "stats":
		return memoryStats(dataDir)
	case "embed-status":
		return memoryEmbedStatus(dataDir)
	case "graph":
		return memoryGraph(dataDir, args[1:])
	case "strength":
		return memoryStrength(dataDir)
	case "infer":
		return memoryInfer(dataDir, args[1:])
	default:
		return NewUsageError("unknown memory action: %s", args[0])
	}
}

func openMemoryStore(dataDir string) (*memory.Store, error) {
	path := filepath.Join(dataDir, "memories.json")
	return memory.NewStore(path)
}

func memoryList(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory list", flag.ExitOnError)
	category := fs.String("category", "", "按分类过滤")
	keyword := fs.String("keyword", "", "关键词搜索")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	entries := store.List(memory.Category(*category), *keyword)
	if *jsonOut {
		return PrintJSON(entries)
	}
	if len(entries) == 0 {
		fmt.Println("No memories found.")
		return nil
	}
	fmt.Printf("%-24s %-20s %-12s %s\n", "ID", "CATEGORY", "ACCESS", "CONTENT")
	fmt.Println(strings.Repeat("-", 80))
	for _, e := range entries {
		content := e.Content
		if len(content) > 40 {
			content = content[:37] + "..."
		}
		content = strings.ReplaceAll(content, "\n", " ")
		fmt.Printf("%-24s %-20s %-12d %s\n", e.ID, e.Category, e.AccessCount, content)
	}
	return nil
}

func memorySearch(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory search", flag.ExitOnError)
	category := fs.String("category", "", "按分类过滤")
	keyword := fs.String("keyword", "", "关键词")
	limit := fs.Int("limit", 10, "最大返回条数")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	// 允许直接传关键词作为位置参数
	kw := *keyword
	if kw == "" && fs.NArg() > 0 {
		kw = fs.Arg(0)
	}

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	entries := store.Search(memory.Category(*category), kw, *limit)
	if *jsonOut {
		return PrintJSON(entries)
	}
	if len(entries) == 0 {
		fmt.Println("No memories found.")
		return nil
	}
	fmt.Printf("%-24s %-20s %-12s %s\n", "ID", "CATEGORY", "ACCESS", "CONTENT")
	fmt.Println(strings.Repeat("-", 80))
	for _, e := range entries {
		content := strings.ReplaceAll(e.Content, "\n", " ")
		if len(content) > 40 {
			content = content[:37] + "..."
		}
		fmt.Printf("%-24s %-20s %-12d %s\n", e.ID, e.Category, e.AccessCount, content)
	}
	return nil
}

func memoryRecall(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory recall", flag.ExitOnError)
	category := fs.String("category", "", "filter by category")
	query := fs.String("query", "", "recall query")
	limit := fs.Int("limit", 10, "max results")
	mode := fs.String("mode", "hybrid", "recall mode: hybrid|adaptive|auto")
	project := fs.String("project", "", "project path for scoped recall")
	debug := fs.Bool("debug", false, "include adaptive recall debug plan")
	jsonOut := fs.Bool("json", false, "JSON output")
	fs.Parse(args)

	q := *query
	if q == "" && fs.NArg() > 0 {
		q = fs.Arg(0)
	}

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	entries, plan, err := memoryRecallByMode(store, q, memory.Category(*category), *mode, *project, *limit)
	if err != nil {
		return err
	}
	if *jsonOut {
		if *debug && plan != nil {
			return PrintJSON(map[string]interface{}{"entries": entries, "plan": plan})
		}
		return PrintJSON(entries)
	}
	if *debug && plan != nil {
		fmt.Printf("Adaptive Recall Plan: complexity=%s fallback=%v themes=%d expanded=%d\n",
			plan.Complexity, plan.Fallback, len(plan.SelectedThemes), len(plan.ExpandedEntryIDs))
		fmt.Printf("  budget max_items=%d token_budget=%d\n", plan.Budget.MaxItems, plan.Budget.TokenBudget)
		fmt.Printf("  diversity theme_cap=%d source_cap=%d deferred_theme=%d deferred_source=%d backfilled=%d selected_themes=%d selected_sources=%d selected_theme_coverage=%d/%d->%d reserved=%d\n",
			plan.Diversity.ThemeCap, plan.Diversity.SourceCap, plan.Diversity.DeferredByThemeCap, plan.Diversity.DeferredBySourceCap, plan.Diversity.BackfilledDeferred, plan.Diversity.SelectedThemeCount, plan.Diversity.SelectedSourceCount, plan.Diversity.SelectedThemeCoveredBefore, plan.Diversity.SelectedThemeTargets, plan.Diversity.SelectedThemeCoveredAfter, plan.Diversity.ReservedSelectedThemes)
		for _, facet := range plan.QueryFacets {
			fmt.Printf("  facet %s: %s tokens=%v\n", facet.Kind, facet.Text, facet.Tokens)
		}
		for _, coverage := range plan.FacetCoverage {
			fmt.Printf("  facet_coverage %s: themes=%v expanded=%v\n", coverage.Kind, coverage.SelectedThemeIDs, coverage.ExpandedEntryIDs)
		}
		for _, aggregate := range plan.ThemeAggregates {
			fmt.Printf("  aggregate %s: results=%d seeds=%d expanded=%d tokens=%d facets=%v\n",
				aggregate.ThemeID, len(aggregate.ResultEntryIDs), len(aggregate.SeedEntryIDs), len(aggregate.ExpandedEntryIDs), aggregate.TokenEstimate, aggregate.MatchedFacets)
			for _, preview := range aggregate.ResultPreviews {
				fmt.Printf("    preview: %s\n", preview)
			}
		}
		for _, theme := range plan.SelectedThemes {
			fmt.Printf("  theme %s: %s (%s facets=%v)\n", theme.ThemeID, theme.Summary, theme.Reason, theme.MatchedFacets)
			for _, ev := range theme.MatchEvidence {
				fmt.Printf("    match %s token=%s entry=%s source=%s preview=%s\n", ev.FacetKind, ev.Token, ev.EntryID, ev.SourceType, ev.ContentPreview)
			}
		}
		for _, ev := range plan.ResultEvidence {
			fmt.Printf("  result #%d %s via %s source=%s theme=%s\n", ev.Rank, ev.EntryID, ev.Reason, ev.SourceType, ev.ThemeID)
		}
		for _, ev := range plan.ExpandedEvidence {
			fmt.Printf("  expanded #%d %s source=%s theme=%s score=%d\n", ev.Rank, ev.EntryID, ev.SourceType, ev.ThemeID, ev.ExpansionScore)
		}
	}
	if len(entries) == 0 {
		fmt.Println("No memories found.")
		return nil
	}
	fmt.Printf("%-24s %-20s %-12s %s\n", "ID", "CATEGORY", "ACCESS", "CONTENT")
	fmt.Println(strings.Repeat("-", 80))
	for _, e := range entries {
		content := strings.ReplaceAll(e.Content, "\n", " ")
		if len(content) > 40 {
			content = content[:37] + "..."
		}
		fmt.Printf("%-24s %-20s %-12d %s\n", e.ID, e.Category, e.AccessCount, content)
	}
	return nil
}

func memoryRecallByMode(store *memory.Store, query string, category memory.Category, mode string, projectPath string, limit int) ([]memory.Entry, *memory.AdaptiveRecallPlan, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "hybrid"
	}
	switch mode {
	case "hybrid", "recall":
		return store.SearchByMode(query, memory.SearchHybrid, category, projectPath, limit), nil, nil
	case "auto":
		if !memory.ShouldUseAdaptiveRecall(query) {
			return store.SearchByMode(query, memory.SearchHybrid, category, projectPath, limit), nil, nil
		}
		fallthrough
	case "adaptive", "hier", "adaptive_hier":
		rebuildStoreThemes(store)
		entries, plan := store.RecallAdaptiveHierDebug(query, category, projectPath)
		if limit > 0 && len(entries) > limit {
			entries = entries[:limit]
			if len(plan.ResultEntryIDs) > limit {
				plan.ResultEntryIDs = plan.ResultEntryIDs[:limit]
			}
		}
		return entries, &plan, nil
	default:
		return nil, nil, NewUsageError("unknown memory recall mode: %s (use hybrid|adaptive|auto)", mode)
	}
}

func rebuildStoreThemes(store *memory.Store) {
	if store == nil || store.ThemeManager() == nil {
		return
	}
	store.ThemeManager().EnsureUpToDate(store.List("", ""), nil)
}

func memoryThemes(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory themes", flag.ExitOnError)
	limit := fs.Int("limit", 20, "max themes")
	stats := fs.Bool("stats", false, "include theme health stats")
	evidence := fs.Int("evidence", 0, "representative evidence entries per theme")
	diagnose := fs.Bool("diagnose", false, "include actionable theme diagnostics")
	issueLimit := fs.Int("issue-limit", 50, "max diagnostic issues")
	plan := fs.Bool("plan", false, "include non-destructive theme maintenance plan")
	apply := fs.Bool("apply", false, "apply safe theme maintenance actions")
	actionLimit := fs.Int("action-limit", 20, "max maintenance actions")
	jsonOut := fs.Bool("json", false, "JSON output")
	fs.Parse(args)

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	rebuildStoreThemes(store)
	themes := store.ThemeManager().TopThemes(*limit)
	var explanations []memory.ThemeExplanation
	if *evidence > 0 {
		explanations = store.ThemeExplanations(*limit, *evidence)
	}
	var diagnostics memory.ThemeDiagnosticReport
	if *diagnose {
		diagnostics = store.ThemeDiagnostics(*issueLimit)
	}
	var maintenance memory.ThemeMaintenancePlan
	if *plan {
		if !*diagnose {
			diagnostics = store.ThemeDiagnostics(*issueLimit)
		}
		maintenance = memory.PlanThemeMaintenance(diagnostics, *actionLimit)
	}
	var maintenanceResult memory.ThemeMaintenanceResult
	if *apply {
		maintenanceResult = store.ApplyThemeMaintenancePlan(*issueLimit, *actionLimit)
		rebuildStoreThemes(store)
		themes = store.ThemeManager().TopThemes(*limit)
		diagnostics = store.ThemeDiagnostics(*issueLimit)
		maintenance = memory.PlanThemeMaintenance(diagnostics, *actionLimit)
		if *evidence > 0 {
			explanations = store.ThemeExplanations(*limit, *evidence)
		}
	}
	if *jsonOut {
		if *apply {
			payload := map[string]interface{}{
				"result":      maintenanceResult,
				"diagnostics": diagnostics,
				"plan":        maintenance,
			}
			if *evidence > 0 {
				payload["explanations"] = store.ThemeExplanations(*limit, *evidence)
			} else {
				payload["themes"] = themes
			}
			return PrintJSON(payload)
		}
		if *plan {
			payload := map[string]interface{}{
				"diagnostics": diagnostics,
				"plan":        maintenance,
			}
			if *evidence > 0 {
				payload["explanations"] = explanations
			} else {
				payload["themes"] = themes
			}
			return PrintJSON(payload)
		}
		if *diagnose {
			payload := map[string]interface{}{
				"diagnostics": diagnostics,
			}
			if *evidence > 0 {
				payload["explanations"] = explanations
			} else {
				payload["themes"] = themes
			}
			return PrintJSON(payload)
		}
		if *evidence > 0 {
			if *stats {
				return PrintJSON(map[string]interface{}{
					"health":       store.ThemeHealth(),
					"explanations": explanations,
				})
			}
			return PrintJSON(explanations)
		}
		if *stats {
			return PrintJSON(map[string]interface{}{
				"health": store.ThemeHealth(),
				"themes": themes,
			})
		}
		return PrintJSON(themes)
	}
	if *stats {
		printThemeHealth(store.ThemeHealth())
		fmt.Println()
	}
	if *diagnose {
		printThemeDiagnostics(diagnostics)
		fmt.Println()
	}
	if *plan {
		printThemeMaintenancePlan(maintenance)
		fmt.Println()
	}
	if *apply {
		printThemeMaintenanceResult(maintenanceResult)
		fmt.Println()
	}
	if len(themes) == 0 {
		fmt.Println("No memory themes found.")
		return nil
	}
	if *evidence > 0 {
		printThemeExplanations(explanations)
		return nil
	}
	fmt.Printf("%-32s %-7s %-32s %s\n", "ID", "MEMBERS", "TAGS", "SUMMARY")
	fmt.Println(strings.Repeat("-", 96))
	for _, theme := range themes {
		tags := strings.Join(theme.Tags, ",")
		if len(tags) > 30 {
			tags = tags[:27] + "..."
		}
		summary := strings.ReplaceAll(theme.Summary, "\n", " ")
		if len(summary) > 60 {
			summary = summary[:57] + "..."
		}
		fmt.Printf("%-32s %-7d %-32s %s\n", theme.ID, theme.MemberCount, tags, summary)
		if len(theme.Neighbors) > 0 {
			neighbors := strings.Join(theme.Neighbors, ",")
			if len(neighbors) > 80 {
				neighbors = neighbors[:77] + "..."
			}
			fmt.Printf("  neighbors: %s\n", neighbors)
		}
	}
	return nil
}

func printThemeExplanations(explanations []memory.ThemeExplanation) {
	for _, explanation := range explanations {
		theme := explanation.Theme
		tags := strings.Join(theme.Tags, ",")
		fmt.Printf("%s members=%d cohesion=%.2f tags=%s\n", theme.ID, theme.MemberCount, explanation.Cohesion, tags)
		fmt.Printf("  summary: %s\n", strings.ReplaceAll(theme.Summary, "\n", " "))
		if len(theme.Neighbors) > 0 {
			fmt.Printf("  neighbors: %s\n", strings.Join(theme.Neighbors, ","))
		}
		for _, ev := range explanation.Evidence {
			fmt.Printf("  evidence: %s source=%s sim=%.2f access=%d %s\n",
				ev.EntryID, ev.SourceType, ev.Similarity, ev.AccessCount, ev.ContentPreview)
			if ev.SourceURL != "" {
				fmt.Printf("    url: %s\n", ev.SourceURL)
			}
		}
	}
}

func printThemeMaintenancePlan(plan memory.ThemeMaintenancePlan) {
	fmt.Println("Theme Maintenance Plan:")
	if len(plan.Actions) == 0 {
		fmt.Println("  No maintenance actions recommended.")
		return
	}
	for _, action := range plan.Actions {
		target := action.ThemeID
		if target == "" && len(action.EntryIDs) > 0 {
			target = strings.Join(action.EntryIDs, ",")
			if len(target) > 80 {
				target = target[:77] + "..."
			}
		}
		if target == "" {
			target = "-"
		}
		fmt.Printf("  [%s] %s %s\n", action.Priority, action.Action, target)
		fmt.Printf("    reason: %s\n", action.Reason)
		if len(action.IssueKinds) > 0 {
			fmt.Printf("    issues: %s\n", strings.Join(action.IssueKinds, ","))
		}
	}
}

func printThemeMaintenanceResult(result memory.ThemeMaintenanceResult) {
	fmt.Println("Theme Maintenance Result:")
	fmt.Printf("  Requested actions:     %d\n", result.RequestedActions)
	fmt.Printf("  Rebuilt themes:        %v\n", result.RebuiltThemes)
	fmt.Printf("  Backfilled embeddings: %d\n", result.BackfilledEmbeddings)
	if len(result.AppliedActions) > 0 {
		fmt.Printf("  Applied:               %s\n", strings.Join(result.AppliedActions, ","))
	}
	if len(result.SkippedActions) > 0 {
		fmt.Printf("  Skipped:               %s\n", strings.Join(result.SkippedActions, ","))
	}
	if len(result.Errors) > 0 {
		fmt.Printf("  Errors:                %s\n", strings.Join(result.Errors, "; "))
	}
	fmt.Printf("  Coverage:              %.2f -> %.2f\n", result.Before.CoverageRate, result.After.CoverageRate)
}

func printThemeDiagnostics(report memory.ThemeDiagnosticReport) {
	fmt.Println("Theme Diagnostics:")
	if len(report.Issues) == 0 {
		fmt.Println("  No actionable theme issues found.")
		return
	}
	for _, issue := range report.Issues {
		target := issue.ThemeID
		if target == "" {
			target = issue.EntryID
		}
		if target == "" {
			target = "-"
		}
		fmt.Printf("  [%s] %s %s: %s\n", issue.Severity, issue.Kind, target, issue.Message)
		fmt.Printf("    suggestion: %s\n", issue.Suggestion)
	}
}

func printThemeHealth(health memory.ThemeHealth) {
	fmt.Println("Theme Health:")
	fmt.Printf("  Themes:              %d\n", health.ThemeCount)
	fmt.Printf("  Active eligible:     %d\n", health.ActiveEligibleEntries)
	fmt.Printf("  Covered entries:     %d\n", health.CoveredEntries)
	fmt.Printf("  Uncovered entries:   %d\n", health.UncoveredEntries)
	fmt.Printf("  Coverage rate:       %.2f\n", health.CoverageRate)
	fmt.Printf("  Avg theme size:      %.2f\n", health.AverageThemeSize)
	fmt.Printf("  Max theme size:      %d\n", health.MaxThemeSize)
	fmt.Printf("  Neighbor links:      %d\n", health.NeighborLinks)
	fmt.Printf("  Isolated themes:     %d\n", health.IsolatedThemes)
	fmt.Printf("  Duplicate refs:      %d\n", health.DuplicateEntryRefs)
}

func memoryEval(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory eval", flag.ExitOnError)
	casesPath := fs.String("cases", "", "JSON file with recall eval cases")
	limit := fs.Int("limit", 10, "max recall results per strategy")
	maintenance := fs.Bool("maintenance", false, "apply safe theme maintenance and re-run eval")
	issueLimit := fs.Int("issue-limit", 50, "max diagnostic issues for maintenance eval")
	actionLimit := fs.Int("action-limit", 20, "max maintenance actions for maintenance eval")
	jsonOut := fs.Bool("json", false, "JSON output")
	fs.Parse(args)

	if *casesPath == "" && fs.NArg() > 0 {
		*casesPath = fs.Arg(0)
	}
	if *casesPath == "" {
		return NewUsageError("usage: memory eval --cases <cases.json> [--limit N] [--json]")
	}

	cases, err := loadRecallEvalCases(*casesPath)
	if err != nil {
		return err
	}

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	if *maintenance {
		report := store.EvaluateRecallStrategiesWithMaintenance(cases, *limit, *issueLimit, *actionLimit)
		if *jsonOut {
			return PrintJSON(report)
		}
		printRecallMaintenanceEvalReport(report)
		return nil
	}

	report := store.EvaluateRecallStrategies(cases, *limit)
	if *jsonOut {
		return PrintJSON(report)
	}
	printRecallEvalReport(report)
	return nil
}

func printRecallMaintenanceEvalReport(report memory.RecallMaintenanceEvalReport) {
	fmt.Println("Memory Recall Eval With Maintenance")
	fmt.Printf("Maintenance: requested=%d rebuilt=%v backfilled=%d applied=%v skipped=%v\n",
		report.Maintenance.RequestedActions,
		report.Maintenance.RebuiltThemes,
		report.Maintenance.BackfilledEmbeddings,
		report.Maintenance.AppliedActions,
		report.Maintenance.SkippedActions)
	fmt.Printf("Delta: coverage=%+.2f hybrid_hit=%+.2f adaptive_hit=%+.2f hybrid_theme_repeat=%+.2f adaptive_theme_repeat=%+.2f issues=%+d actions=%+d\n",
		report.Delta.ThemeCoverageRate,
		report.Delta.HybridHitRate,
		report.Delta.AdaptiveHitRate,
		report.Delta.HybridAvgThemeRepeats,
		report.Delta.AdaptiveAvgThemeRepeats,
		report.Delta.ThemeIssueCount,
		report.Delta.ThemeActionCount)
	fmt.Println()
	fmt.Println("Before:")
	printRecallEvalReport(report.Before)
	fmt.Println()
	fmt.Println("After:")
	printRecallEvalReport(report.After)
}

func loadRecallEvalCases(path string) ([]memory.RecallEvalCase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []memory.RecallEvalCase
	if err := json.Unmarshal(data, &cases); err == nil {
		return cases, nil
	}
	var wrapper struct {
		Cases []memory.RecallEvalCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.Cases, nil
}

func printRecallEvalReport(report memory.RecallEvalReport) {
	fmt.Println("Memory Recall Eval")
	fmt.Printf("Theme health: coverage=%.2f themes=%d issues=%d actions=%d isolated=%d\n",
		report.Theme.Health.CoverageRate,
		report.Theme.Health.ThemeCount,
		report.Theme.IssueCount,
		report.Theme.ActionCount,
		report.Theme.Health.IsolatedThemes)
	fmt.Printf("%-12s %-8s %-8s %-10s %-10s %-12s %-10s\n", "STRATEGY", "CASES", "HITS", "HIT_RATE", "TOKENS", "THEME_REPEAT", "MATCH_EV")
	fmt.Println(strings.Repeat("-", 72))
	for _, name := range []string{"hybrid", "adaptive"} {
		metric := report.Strategies[name]
		fmt.Printf("%-12s %-8d %-8d %-10.2f %-10.1f %-12.1f %-10.1f\n",
			name, metric.Cases, metric.Hits, metric.HitRate, metric.AvgTokens, metric.AvgThemeRepeats, metric.AvgThemeMatchEvidence)
	}
	for _, c := range report.Cases {
		fmt.Printf("\n%s: %s\n", c.Name, c.Query)
		for _, name := range []string{"hybrid", "adaptive"} {
			score := c.Strategies[name]
			fmt.Printf("  %-8s hit=%v results=%d tokens=%d repeats=%d matched=%v",
				name, score.Hit, score.ResultCount, score.TokenEstimate, score.DuplicateThemes, score.MatchedExpected)
			if name == "adaptive" {
				fmt.Printf(" fallback=%v facets=%d budget=%d/%d", score.AdaptiveFallback, score.QueryFacets, score.BudgetMaxItems, score.BudgetTokens)
				if score.Diversity != nil {
					fmt.Printf(" diversity=%+v", *score.Diversity)
				}
				fmt.Printf(" facet_coverage=%v themes=%d aggregates=%d expanded=%d seed_results=%d expanded_results=%d theme_reasons=%v match_evidence=%d match_sources=%v sources=%v",
					score.FacetCoverage, score.SelectedThemes, score.AggregatedThemes, score.ExpandedEntries, score.SeedResults, score.ExpandedResults, score.ThemeReasons, score.ThemeMatchEvidence, score.ThemeMatchSources, score.SourceCounts)
			}
			fmt.Println()
		}
	}
}

func memorySave(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory save", flag.ExitOnError)
	content := fs.String("content", "", "记忆内容（必填）")
	category := fs.String("category", "user_fact", "分类")
	tags := fs.String("tags", "", "标签（逗号分隔）")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if *content == "" {
		return NewUsageError("usage: memory save --content <text> [--category <cat>] [--tags <t1,t2>]")
	}

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	var tagList []string
	if *tags != "" {
		tagList = strings.Split(*tags, ",")
	}

	entry := memory.Entry{
		Content:  *content,
		Category: memory.Category(*category),
		Tags:     tagList,
	}
	if err := store.Save(entry); err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(map[string]string{"status": "saved"})
	}
	fmt.Println("Memory saved.")
	return nil
}

func memoryDelete(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory delete", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: memory delete <id>")
	}
	id := fs.Arg(0)

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	if err := store.Delete(id); err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(map[string]string{"id": id, "status": "deleted"})
	}
	fmt.Printf("Memory %s deleted.\n", id)
	return nil
}

func memoryCompress(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory compress", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	// 无 LLM 时仅做 dedup（传 nil LLM）
	compressor := memory.NewCompressor(store, nil, nil)
	result, err := compressor.Compress(context.Background())
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(result)
	}
	fmt.Printf("Memory Compress Result:\n")
	fmt.Printf("  Total entries:  %d\n", result.TotalEntries)
	fmt.Printf("  Dedup removed:  %d\n", result.DedupCount)
	fmt.Printf("  Merged:         %d\n", result.MergedCount)
	fmt.Printf("  Compressed:     %d\n", result.CompressedCount)
	fmt.Printf("  Skipped:        %d\n", result.SkippedCount)
	fmt.Printf("  Errors:         %d\n", result.ErrorCount)
	fmt.Printf("  Saved chars:    %d\n", result.SavedChars)
	if result.BackupName != "" {
		fmt.Printf("  Backup:         %s\n", result.BackupName)
	}
	return nil
}

func memoryBackup(dataDir string, args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui memory backup <list|restore|delete>")
	}
	switch args[0] {
	case "list":
		return memoryBackupList(dataDir, args[1:])
	case "restore":
		return memoryBackupRestore(dataDir, args[1:])
	case "delete":
		return memoryBackupDelete(dataDir, args[1:])
	default:
		return NewUsageError("unknown memory backup action: %s", args[0])
	}
}

func memoryBackupList(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory backup list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	compressor := memory.NewCompressor(store, nil, nil)
	backups, err := compressor.ListBackups()
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(backups)
	}
	if len(backups) == 0 {
		fmt.Println("No backups found.")
		return nil
	}
	fmt.Printf("%-40s %-22s %-10s %s\n", "NAME", "CREATED", "SIZE", "ENTRIES")
	fmt.Println(strings.Repeat("-", 85))
	for _, b := range backups {
		fmt.Printf("%-40s %-22s %-10d %d\n", b.Name, b.CreatedAt, b.SizeBytes, b.EntryCount)
	}
	return nil
}

func memoryBackupRestore(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory backup restore", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() == 0 {
		return NewUsageError("usage: maclaw-tui memory backup restore <backup-name>")
	}
	name := fs.Arg(0)

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	compressor := memory.NewCompressor(store, nil, nil)
	if err := compressor.RestoreBackup(name); err != nil {
		return err
	}
	fmt.Printf("Backup %s restored.\n", name)
	return nil
}

func memoryBackupDelete(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory backup delete", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() == 0 {
		return NewUsageError("usage: maclaw-tui memory backup delete <backup-name>")
	}
	name := fs.Arg(0)

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	compressor := memory.NewCompressor(store, nil, nil)
	if err := compressor.DeleteBackup(name); err != nil {
		return err
	}
	fmt.Printf("Backup %s deleted.\n", name)
	return nil
}

func memoryAutoCompress(dataDir string, args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui memory auto-compress <on|off|status>")
	}
	store := NewFileConfigStore(dataDir)
	switch args[0] {
	case "on":
		cfg, err := store.LoadConfig()
		if err != nil {
			return err
		}
		cfg.MemoryAutoCompress = true
		if err := store.SaveConfig(cfg); err != nil {
			return err
		}
		fmt.Println("自动压缩已开启。")
		return nil
	case "off":
		cfg, err := store.LoadConfig()
		if err != nil {
			return err
		}
		cfg.MemoryAutoCompress = false
		if err := store.SaveConfig(cfg); err != nil {
			return err
		}
		fmt.Println("自动压缩已关闭。")
		return nil
	case "status":
		cfg, err := store.LoadConfig()
		if err != nil {
			return err
		}
		if cfg.MemoryAutoCompress {
			fmt.Println("自动压缩: 开启")
		} else {
			fmt.Println("自动压缩: 关闭")
		}
		return nil
	default:
		return NewUsageError("usage: maclaw-tui memory auto-compress <on|off|status>")
	}
}

func memoryStats(dataDir string) error {
	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	entries := store.List("", "")
	total := len(entries)
	var active, dormant, superseded int
	var withEmb, withGraph int
	scopeGlobal, scopeProject := 0, 0
	catCounts := make(map[memory.Category]int)
	tierSemantic, tierEpisodic := 0, 0

	for _, e := range entries {
		catCounts[e.Category]++
		switch e.Status {
		case memory.StatusDormant:
			dormant++
		case memory.StatusSuperseded:
			superseded++
		default:
			active++
		}
		if len(e.Embedding) > 0 {
			withEmb++
		}
		if len(e.RelatedIDs) > 0 {
			withGraph++
		}
		if e.Scope == memory.ScopeGlobal {
			scopeGlobal++
		} else {
			scopeProject++
		}
		if e.Category.Tier() == memory.TierSemantic {
			tierSemantic++
		} else {
			tierEpisodic++
		}
	}

	fmt.Printf("Memory Store Stats:\n")
	fmt.Printf("  Total entries:    %d\n", total)
	fmt.Printf("  Active:           %d\n", active)
	fmt.Printf("  Dormant:          %d\n", dormant)
	fmt.Printf("  Superseded:       %d\n", superseded)
	fmt.Printf("  With embedding:   %d\n", withEmb)
	fmt.Printf("  With graph links: %d\n", withGraph)
	fmt.Printf("  Scope global:     %d\n", scopeGlobal)
	fmt.Printf("  Scope project:    %d\n", scopeProject)
	fmt.Printf("  Tier semantic:    %d\n", tierSemantic)
	fmt.Printf("  Tier episodic:    %d\n", tierEpisodic)
	rebuildStoreThemes(store)
	health := store.ThemeHealth()
	fmt.Printf("  Theme count:      %d\n", health.ThemeCount)
	fmt.Printf("  Theme coverage:   %.2f (%d/%d)\n", health.CoverageRate, health.CoveredEntries, health.ActiveEligibleEntries)
	fmt.Printf("  Theme isolated:   %d\n", health.IsolatedThemes)
	fmt.Printf("  Categories:\n")
	for cat, count := range catCounts {
		fmt.Printf("    %-25s %d\n", cat, count)
	}
	return nil
}

func memoryEmbedStatus(dataDir string) error {
	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	store.RLock()
	entries := store.Entries()
	total := len(entries)
	withEmb := 0
	for _, e := range entries {
		if len(e.Embedding) > 0 {
			withEmb++
		}
	}
	store.RUnlock()

	embedder := store.Embedder()
	embedderType := "Noop"
	modelPath := "(none)"
	if embedder != nil && !embedding.IsNoop(embedder) {
		embedderType = "Gemma"
		modelPath = embedding.DefaultModelPath()
	}

	fmt.Printf("Embedding Status:\n")
	fmt.Printf("  Total entries:           %d\n", total)
	fmt.Printf("  With embeddings:         %d\n", withEmb)
	fmt.Printf("  Without embeddings:      %d\n", total-withEmb)
	fmt.Printf("  Embedder type:           %s\n", embedderType)
	fmt.Printf("  Model path:              %s\n", modelPath)
	return nil
}

func memoryGraph(dataDir string, args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui memory graph <id>")
	}
	id := args[0]

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	neighbors := store.GraphNeighbors(id)
	if len(neighbors) == 0 {
		fmt.Printf("No graph neighbors for entry %s.\n", id)
		return nil
	}

	// Build entry lookup for content preview.
	store.RLock()
	entryByID := make(map[string]*memory.Entry)
	for i := range store.Entries() {
		entryByID[store.Entries()[i].ID] = &store.Entries()[i]
	}
	store.RUnlock()

	fmt.Printf("Graph neighbors for %s:\n\n", id)
	fmt.Printf("%-26s %-10s %s\n", "NEIGHBOR", "STRENGTH", "CONTENT")
	fmt.Println(strings.Repeat("-", 76))

	// Sort neighbor IDs for stable output.
	type neighborInfo struct {
		id       string
		strength float64
	}
	sorted := make([]neighborInfo, 0, len(neighbors))
	for nid, str := range neighbors {
		sorted = append(sorted, neighborInfo{id: nid, strength: str})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].strength > sorted[j].strength
	})

	for _, n := range sorted {
		content := "(not found)"
		if e, ok := entryByID[n.id]; ok {
			content = strings.ReplaceAll(e.Content, "\n", " ")
			if len(content) > 36 {
				content = content[:33] + "..."
			}
		}
		fmt.Printf("%-26s %-10.4f %s\n", n.id, n.strength, content)
	}
	return nil
}

func memoryStrength(dataDir string) error {
	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	store.RLock()
	entries := make([]memory.Entry, len(store.Entries()))
	copy(entries, store.Entries())
	store.RUnlock()

	if len(entries) == 0 {
		fmt.Println("No memories found.")
		return nil
	}

	now := time.Now()

	type strengthEntry struct {
		entry   memory.Entry
		current float64
		dormant bool
	}

	items := make([]strengthEntry, 0, len(entries))
	for _, e := range entries {
		cur := e.Strength
		if cur > 0 {
			hours := now.Sub(e.UpdatedAt).Hours()
			if hours < 0 {
				hours = 0
			}
			cur = e.Strength * math.Exp(-0.003*hours)
		}
		isDormant := cur < 0.1 && e.Status != memory.StatusSuperseded && !e.Category.IsProtected()
		items = append(items, strengthEntry{entry: e, current: cur, dormant: isDormant})
	}

	// Sort by current strength ascending (weakest first).
	sort.Slice(items, func(i, j int) bool {
		return items[i].current < items[j].current
	})

	fmt.Printf("%-26s %-10s %-12s %-20s %s\n", "ID", "STRENGTH", "STATUS", "LAST ACCESS", "CONTENT")
	fmt.Println(strings.Repeat("-", 96))

	for _, it := range items {
		status := string(it.entry.Status)
		if status == "" {
			status = "active"
		}
		marker := "  "
		if it.dormant || it.entry.Status == memory.StatusDormant {
			marker = "⚠ "
		}

		content := strings.ReplaceAll(it.entry.Content, "\n", " ")
		if len(content) > 24 {
			content = content[:21] + "..."
		}

		lastAccess := it.entry.UpdatedAt.Format("2006-01-02 15:04")

		fmt.Printf("%s%-24s %-10.4f %-12s %-20s %s\n",
			marker, it.entry.ID, it.current, status, lastAccess, content)
	}
	return nil
}

func memoryInfer(dataDir string, args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui memory infer <query>\n\nRuns the multi-hop inference engine on the given query and displays derived facts.")
	}
	query := strings.Join(args, " ")

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	ie := store.InferenceEngine()
	if ie == nil {
		fmt.Println("Inference engine not available (semantic graph may be empty).")
		return nil
	}

	expanded := memory.ExpandQuery(query)
	if len(expanded.Entities) == 0 {
		fmt.Printf("No entities extracted from query: %q\n", query)
		return nil
	}
	fmt.Printf("Query entities: %v\n\n", expanded.Entities)

	derived := ie.Infer(expanded.Entities, memory.InferenceOptions{
		MaxDerived:      20,
		MinConfidence:   0.40,
		MaxVisitedFacts: 200,
	})

	if len(derived) == 0 {
		fmt.Println("No derived facts found.")
		fmt.Printf("\nSemantic graph stats:\n")
		if sg := store.SemanticGraph(); sg != nil {
			entities, facts, _ := sg.Stats()
			fmt.Printf("  Entities: %d\n  Facts: %d\n", entities, facts)
		}
		return nil
	}

	fmt.Printf("Derived facts (%d):\n\n", len(derived))
	fmt.Printf("%-20s %-15s %-20s %-10s %-30s %s\n", "SUBJECT", "PREDICATE", "OBJECT", "CONF", "RULE", "EXPLANATION")
	fmt.Println(strings.Repeat("-", 120))

	for _, df := range derived {
		subj := df.Subject
		if len(subj) > 18 {
			subj = subj[:15] + "..."
		}
		obj := df.Object
		if len(obj) > 18 {
			obj = obj[:15] + "..."
		}
		ruleName := df.RuleName
		if len(ruleName) > 28 {
			ruleName = ruleName[:25] + "..."
		}
		fmt.Printf("%-20s %-15s %-20s %-10.0f%% %-30s %s\n",
			subj, df.Predicate, obj, df.Confidence*100, ruleName, df.Explanation)
	}

	fmt.Printf("\nSemantic graph stats:\n")
	if sg := store.SemanticGraph(); sg != nil {
		entities, facts, _ := sg.Stats()
		fmt.Printf("  Entities: %d\n  Facts: %d\n", entities, facts)
	}
	fmt.Printf("  Rules: %d\n", len(ie.Rules()))
	return nil
}
