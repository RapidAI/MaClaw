package agent

// tool_memory.go implements the memory tool handler as a standalone function.
// Shared by GUI (via IMMessageHandler.toolMemory wrapper) and TUI.

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// ToolMemory handles memory operations (save/recall/themes/delete/list).
func ToolMemory(store *memory.Store, args map[string]interface{}) string {
	if store == nil {
		return "长期记忆未初始化"
	}

	action := StringArg(args, "action")
	switch action {
	case "themes":
		rebuildAgentMemoryThemes(store)
		limit := intArg(args, "limit", 20)
		evidence := boolArg(args, "evidence", false)
		evidenceLimit := intArg(args, "evidence_limit", 3)
		diagnose := boolArg(args, "diagnose", false)
		issueLimit := intArg(args, "issue_limit", 50)
		plan := boolArg(args, "plan", false)
		actionLimit := intArg(args, "action_limit", 20)
		apply := boolArg(args, "apply", false)
		if apply {
			return formatMemoryThemeMaintenanceResult(store.ApplyThemeMaintenancePlan(issueLimit, actionLimit))
		}
		if plan {
			report := store.ThemeDiagnostics(issueLimit)
			return formatMemoryThemeMaintenancePlan(memory.PlanThemeMaintenance(report, actionLimit), report, store.ThemeExplanations(limit, evidenceLimit), evidence)
		}
		if diagnose {
			return formatMemoryThemeDiagnostics(store.ThemeDiagnostics(issueLimit), store.ThemeExplanations(limit, evidenceLimit), evidence)
		}
		if evidence {
			return formatMemoryThemeExplanations(store.ThemeExplanations(limit, evidenceLimit), store.ThemeHealth(), boolArg(args, "stats", false))
		}
		return formatMemoryThemesWithHealth(store.ThemeManager().TopThemes(limit), store.ThemeHealth(), boolArg(args, "stats", false))

	case "recall":
		query := StringArg(args, "query")
		if query == "" {
			return "缺少 query 参数"
		}
		category := memory.Category(StringArg(args, "category"))
		mode := strings.ToLower(strings.TrimSpace(StringArg(args, "mode")))
		projectPath := StringArg(args, "project_path")
		debug := boolArg(args, "debug", false)
		var entries []memory.Entry
		var plan *memory.AdaptiveRecallPlan
		switch mode {
		case "", "dynamic":
			entries = store.RecallDynamic(query, category, projectPath)
		case "hybrid", "recall":
			entries = store.SearchByMode(query, memory.SearchHybrid, category, projectPath, 0)
		case "auto":
			if memory.ShouldUseAdaptiveRecall(query) {
				rebuildAgentMemoryThemes(store)
				result, p := store.RecallAdaptiveHierDebug(query, category, projectPath)
				entries = result
				plan = &p
			} else {
				entries = store.RecallDynamic(query, category, projectPath)
			}
		case "adaptive", "hier", "adaptive_hier":
			rebuildAgentMemoryThemes(store)
			result, p := store.RecallAdaptiveHierDebug(query, category, projectPath)
			entries = result
			plan = &p
		default:
			return fmt.Sprintf("未知 memory recall mode: %s（支持 dynamic/hybrid/adaptive/auto）", mode)
		}
		if len(entries) == 0 {
			return "没有找到相关记忆。"
		}
		var b strings.Builder
		fmt.Fprintf(&b, "召回 %d 条相关记忆:\n", len(entries))
		if debug && plan != nil {
			fmt.Fprintf(&b, "Adaptive plan: complexity=%s fallback=%v themes=%d expanded=%d\n",
				plan.Complexity, plan.Fallback, len(plan.SelectedThemes), len(plan.ExpandedEntryIDs))
			fmt.Fprintf(&b, "  budget max_items=%d token_budget=%d\n", plan.Budget.MaxItems, plan.Budget.TokenBudget)
			fmt.Fprintf(&b, "  diversity theme_cap=%d source_cap=%d deferred_theme=%d deferred_source=%d backfilled=%d selected_themes=%d selected_sources=%d selected_theme_coverage=%d/%d->%d reserved=%d\n",
				plan.Diversity.ThemeCap, plan.Diversity.SourceCap, plan.Diversity.DeferredByThemeCap, plan.Diversity.DeferredBySourceCap, plan.Diversity.BackfilledDeferred, plan.Diversity.SelectedThemeCount, plan.Diversity.SelectedSourceCount, plan.Diversity.SelectedThemeCoveredBefore, plan.Diversity.SelectedThemeTargets, plan.Diversity.SelectedThemeCoveredAfter, plan.Diversity.ReservedSelectedThemes)
			for _, facet := range plan.QueryFacets {
				fmt.Fprintf(&b, "  facet %s: %s tokens=%v\n", facet.Kind, facet.Text, facet.Tokens)
			}
			for _, coverage := range plan.FacetCoverage {
				fmt.Fprintf(&b, "  facet_coverage %s: themes=%v expanded=%v\n", coverage.Kind, coverage.SelectedThemeIDs, coverage.ExpandedEntryIDs)
			}
			for _, aggregate := range plan.ThemeAggregates {
				fmt.Fprintf(&b, "  aggregate %s: results=%d seeds=%d expanded=%d tokens=%d facets=%v\n",
					aggregate.ThemeID, len(aggregate.ResultEntryIDs), len(aggregate.SeedEntryIDs), len(aggregate.ExpandedEntryIDs), aggregate.TokenEstimate, aggregate.MatchedFacets)
				for _, preview := range aggregate.ResultPreviews {
					fmt.Fprintf(&b, "    preview: %s\n", preview)
				}
			}
			for _, theme := range plan.SelectedThemes {
				fmt.Fprintf(&b, "  theme %s: %s (%s facets=%v)\n", theme.ThemeID, theme.Summary, theme.Reason, theme.MatchedFacets)
				for _, ev := range theme.MatchEvidence {
					fmt.Fprintf(&b, "    match %s token=%s entry=%s source=%s preview=%s\n", ev.FacetKind, ev.Token, ev.EntryID, ev.SourceType, ev.ContentPreview)
				}
			}
			for _, ev := range plan.ResultEvidence {
				fmt.Fprintf(&b, "  result #%d %s via %s source=%s theme=%s\n", ev.Rank, ev.EntryID, ev.Reason, ev.SourceType, ev.ThemeID)
			}
			for _, ev := range plan.ExpandedEvidence {
				fmt.Fprintf(&b, "  expanded #%d %s source=%s theme=%s score=%d\n", ev.Rank, ev.EntryID, ev.SourceType, ev.ThemeID, ev.ExpansionScore)
			}
		}
		for _, e := range entries {
			fmt.Fprintf(&b, "- [%s] %s\n", string(e.Category), e.Content)
		}
		ids := make([]string, len(entries))
		for i, e := range entries {
			ids[i] = e.ID
		}
		store.TouchAccess(ids)
		return b.String()

	case "save":
		content := StringArg(args, "content")
		if content == "" {
			return "缺少 content 参数"
		}
		category := StringArg(args, "category")
		if category == "" {
			category = "user_fact"
		}
		var tags []string
		if rawTags, ok := args["tags"]; ok {
			if tagSlice, ok := rawTags.([]interface{}); ok {
				for _, t := range tagSlice {
					if s, ok := t.(string); ok && s != "" {
						tags = append(tags, s)
					}
				}
			}
		}
		if len(tags) == 0 {
			expanded := memory.ExpandQuery(content)
			tags = expanded.Entities
		}
		entry := memory.Entry{
			Content:  content,
			Category: memory.Category(category),
			Tags:     tags,
		}
		// Derive a title from the first meaningful line of content.
		// This provides a clean display name for the task list.
		for _, line := range strings.SplitN(content, "\n", 10) {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			line = strings.TrimPrefix(line, "# ")
			line = strings.TrimPrefix(line, "## ")
			if runes := []rune(line); len(runes) > 60 {
				entry.Title = string(runes[:60])
			} else {
				entry.Title = line
			}
			break
		}
		// Use SaveWithContext when conversation context is available,
		// enriching tags with entities from surrounding dialogue.
		contextHint := StringArg(args, "_context_hint")
		if err := store.SaveWithContext(entry, contextHint); err != nil {
			return fmt.Sprintf("保存记忆失败: %s", err.Error())
		}
		summary := content
		if len(summary) > 50 {
			summary = summary[:50] + "..."
		}
		return fmt.Sprintf("已保存记忆: %s", summary)

	case "list":
		category := memory.Category(StringArg(args, "category"))
		keyword := StringArg(args, "keyword")
		entries := store.List(category, keyword)
		if len(entries) == 0 {
			return "没有找到匹配的记忆条目。"
		}
		var b strings.Builder
		fmt.Fprintf(&b, "找到 %d 条记忆:\n", len(entries))
		for _, e := range entries {
			fmt.Fprintf(&b, "- [%s] (%s) %s\n", e.ID, e.Category, e.Content)
		}
		return b.String()

	case "delete":
		id := StringArg(args, "id")
		if id == "" {
			return "缺少 id 参数"
		}
		if err := store.Delete(id); err != nil {
			return fmt.Sprintf("删除记忆失败: %s", err.Error())
		}
		return fmt.Sprintf("已删除记忆: %s", id)

	default:
		return fmt.Sprintf("未知 memory action: %s（支持 save/recall/themes/delete/list）", action)
	}
}

func rebuildAgentMemoryThemes(store *memory.Store) {
	if store == nil || store.ThemeManager() == nil {
		return
	}
	store.ThemeManager().Rebuild(store.List("", ""), nil)
}

func formatMemoryThemes(themes []memory.ThemeNode) string {
	if len(themes) == 0 {
		return "没有找到记忆主题。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "记忆主题 %d 个\n", len(themes))
	for _, theme := range themes {
		fmt.Fprintf(&b, "- %s members=%d tags=%v summary=%s\n", theme.ID, theme.MemberCount, theme.Tags, theme.Summary)
		if len(theme.Neighbors) > 0 {
			fmt.Fprintf(&b, "  neighbors=%v\n", theme.Neighbors)
		}
	}
	return b.String()
}

func formatMemoryThemesWithHealth(themes []memory.ThemeNode, health memory.ThemeHealth, includeStats bool) string {
	out := formatMemoryThemes(themes)
	if !includeStats {
		return out
	}
	return formatThemeHealth(health) + out
}

func formatThemeHealth(health memory.ThemeHealth) string {
	return fmt.Sprintf(
		"theme_health: themes=%d active=%d covered=%d uncovered=%d coverage=%.2f avg_size=%.2f max_size=%d neighbor_links=%d isolated=%d duplicate_refs=%d\n",
		health.ThemeCount,
		health.ActiveEligibleEntries,
		health.CoveredEntries,
		health.UncoveredEntries,
		health.CoverageRate,
		health.AverageThemeSize,
		health.MaxThemeSize,
		health.NeighborLinks,
		health.IsolatedThemes,
		health.DuplicateEntryRefs,
	)
}

func formatMemoryThemeExplanations(explanations []memory.ThemeExplanation, health memory.ThemeHealth, includeStats bool) string {
	if len(explanations) == 0 {
		return formatMemoryThemesWithHealth(nil, health, includeStats)
	}
	var b strings.Builder
	if includeStats {
		b.WriteString(formatThemeHealth(health))
	}
	fmt.Fprintf(&b, "记忆主题 %d 个\n", len(explanations))
	for _, explanation := range explanations {
		theme := explanation.Theme
		fmt.Fprintf(&b, "- %s members=%d cohesion=%.2f tags=%v summary=%s\n", theme.ID, theme.MemberCount, explanation.Cohesion, theme.Tags, theme.Summary)
		for _, ev := range explanation.Evidence {
			fmt.Fprintf(&b, "  evidence %s source=%s sim=%.2f access=%d %s\n",
				ev.EntryID, ev.SourceType, ev.Similarity, ev.AccessCount, ev.ContentPreview)
			if ev.SourceURL != "" {
				fmt.Fprintf(&b, "    url=%s\n", ev.SourceURL)
			}
		}
	}
	return b.String()
}

func formatMemoryThemeDiagnostics(report memory.ThemeDiagnosticReport, explanations []memory.ThemeExplanation, includeEvidence bool) string {
	var b strings.Builder
	b.WriteString(formatThemeHealth(report.Health))
	if len(report.Issues) == 0 {
		b.WriteString("theme_diagnostics: no actionable issues\n")
	} else {
		fmt.Fprintf(&b, "theme_diagnostics: issues=%d\n", len(report.Issues))
		for _, issue := range report.Issues {
			target := issue.ThemeID
			if target == "" {
				target = issue.EntryID
			}
			fmt.Fprintf(&b, "- [%s] %s %s: %s\n", issue.Severity, issue.Kind, target, issue.Message)
			fmt.Fprintf(&b, "  suggestion: %s\n", issue.Suggestion)
		}
	}
	if includeEvidence {
		b.WriteString(formatMemoryThemeExplanations(explanations, report.Health, false))
	}
	return b.String()
}

func formatMemoryThemeMaintenancePlan(plan memory.ThemeMaintenancePlan, report memory.ThemeDiagnosticReport, explanations []memory.ThemeExplanation, includeEvidence bool) string {
	var b strings.Builder
	b.WriteString(formatThemeHealth(plan.Health))
	fmt.Fprintf(&b, "theme_diagnostics: issues=%d\n", len(report.Issues))
	if len(plan.Actions) == 0 {
		b.WriteString("theme_maintenance_plan: no actions recommended\n")
	} else {
		fmt.Fprintf(&b, "theme_maintenance_plan: actions=%d\n", len(plan.Actions))
		for _, action := range plan.Actions {
			target := action.ThemeID
			if target == "" {
				target = strings.Join(action.EntryIDs, ",")
			}
			fmt.Fprintf(&b, "- [%s] %s %s\n", action.Priority, action.Action, target)
			fmt.Fprintf(&b, "  reason: %s\n", action.Reason)
			if len(action.IssueKinds) > 0 {
				fmt.Fprintf(&b, "  issues: %s\n", strings.Join(action.IssueKinds, ","))
			}
		}
	}
	if includeEvidence {
		b.WriteString(formatMemoryThemeExplanations(explanations, plan.Health, false))
	}
	return b.String()
}

func formatMemoryThemeMaintenanceResult(result memory.ThemeMaintenanceResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "theme_maintenance_result: requested=%d rebuilt=%v backfilled=%d coverage=%.2f->%.2f\n",
		result.RequestedActions, result.RebuiltThemes, result.BackfilledEmbeddings, result.Before.CoverageRate, result.After.CoverageRate)
	if len(result.AppliedActions) > 0 {
		fmt.Fprintf(&b, "applied=%s\n", strings.Join(result.AppliedActions, ","))
	}
	if len(result.SkippedActions) > 0 {
		fmt.Fprintf(&b, "skipped=%s\n", strings.Join(result.SkippedActions, ","))
	}
	if len(result.Errors) > 0 {
		fmt.Fprintf(&b, "errors=%s\n", strings.Join(result.Errors, "; "))
	}
	if len(result.Plan.Actions) > 0 {
		fmt.Fprintf(&b, "planned_actions=%d\n", len(result.Plan.Actions))
	}
	return b.String()
}
