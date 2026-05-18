package memory

import (
	"fmt"
	"strings"
)

// ToolOptions carries host-specific context into the shared memory tool logic.
type ToolOptions struct {
	ProjectPath string
	ContextHint string
	OwnerID     string
	AfterWrite  func()
}

// ToolRecallResult is the shared recall outcome used by GUI, TUI, and server agents.
type ToolRecallResult struct {
	Entries        []Entry             `json:"entries"`
	AdaptivePlan   *AdaptiveRecallPlan `json:"adaptive_plan,omitempty"`
	LightMemPlan   *LightMemRecallPlan `json:"lightmem_plan,omitempty"`
	NormalizedMode string              `json:"mode"`
}

// RecallByMode applies the shared memory recall mode matrix.
func (s *Store) RecallByMode(query string, category Category, mode string, projectPath string, limit int, ownerID ...string) (ToolRecallResult, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "dynamic"
	}
	result := ToolRecallResult{NormalizedMode: mode}
	if s == nil {
		return result, nil
	}
	switch mode {
	case "dynamic":
		result.Entries = s.RecallDynamic(query, category, projectPath, ownerID...)
	case "hybrid", "recall":
		result.Entries = s.SearchByMode(query, SearchHybrid, category, projectPath, limit, ownerID...)
	case "lightmem", "light_mem", "planned":
		entries, plan := s.RecallLightMemDebug(query, category, projectPath, ownerID...)
		result.Entries = entries
		result.LightMemPlan = &plan
	case "auto":
		if ShouldUseAdaptiveRecall(query) {
			s.EnsureThemesUpToDate()
			entries, plan := s.RecallAdaptiveHierDebug(query, category, projectPath, ownerID...)
			result.Entries = entries
			result.AdaptivePlan = &plan
		} else {
			result.Entries = s.RecallDynamic(query, category, projectPath, ownerID...)
		}
	case "adaptive", "hier", "adaptive_hier":
		s.EnsureThemesUpToDate()
		entries, plan := s.RecallAdaptiveHierDebug(query, category, projectPath, ownerID...)
		result.Entries = entries
		result.AdaptivePlan = &plan
	default:
		return result, fmt.Errorf("unknown memory recall mode: %s (use dynamic|hybrid|lightmem|adaptive|auto)", mode)
	}
	if limit > 0 && len(result.Entries) > limit {
		result.Entries = result.Entries[:limit]
		if result.AdaptivePlan != nil && len(result.AdaptivePlan.ResultEntryIDs) > limit {
			result.AdaptivePlan.ResultEntryIDs = result.AdaptivePlan.ResultEntryIDs[:limit]
		}
		if result.LightMemPlan != nil && len(result.LightMemPlan.ResultEntryIDs) > limit {
			result.LightMemPlan.ResultEntryIDs = result.LightMemPlan.ResultEntryIDs[:limit]
		}
	}
	return result, nil
}

// EnsureThemesUpToDate centralizes the lazy theme rebuild used by all frontends.
func (s *Store) EnsureThemesUpToDate() {
	if s == nil || s.ThemeManager() == nil {
		return
	}
	s.ThemeManager().EnsureUpToDate(s.List("", ""), nil)
}

// HandleTool implements the shared memory tool behavior for GUI and server agents.
func HandleTool(store *Store, args map[string]interface{}, opts ToolOptions) string {
	if store == nil {
		return "long-term memory is not initialized"
	}
	rawAction := toolStringArg(args, "action")
	action := NormalizeMemoryToolAction(rawAction)
	switch action {
	case MemoryToolActionThemes:
		store.EnsureThemesUpToDate()
		limit := toolIntArg(args, "limit", 20)
		evidence := toolBoolArg(args, "evidence", false)
		evidenceLimit := toolIntArg(args, "evidence_limit", 3)
		diagnose := toolBoolArg(args, "diagnose", false)
		issueLimit := toolIntArg(args, "issue_limit", 50)
		plan := toolBoolArg(args, "plan", false)
		actionLimit := toolIntArg(args, "action_limit", 20)
		apply := toolBoolArg(args, "apply", false)
		if apply {
			return FormatThemeMaintenanceResultForTool(store.ApplyThemeMaintenancePlan(issueLimit, actionLimit))
		}
		if plan {
			report := store.ThemeDiagnostics(issueLimit)
			return FormatThemeMaintenancePlanForTool(PlanThemeMaintenance(report, actionLimit), report, store.ThemeExplanations(limit, evidenceLimit), evidence)
		}
		if diagnose {
			return FormatThemeDiagnosticsForTool(store.ThemeDiagnostics(issueLimit), store.ThemeExplanations(limit, evidenceLimit), evidence)
		}
		if evidence {
			return FormatThemeExplanationsForTool(store.ThemeExplanations(limit, evidenceLimit), store.ThemeHealth(), toolBoolArg(args, "stats", false))
		}
		return FormatThemesForTool(store.ThemeManager().TopThemes(limit), store.ThemeHealth(), toolBoolArg(args, "stats", false))

	case MemoryToolActionScenes:
		return FormatSceneIndexForTool(store.SceneIndex(toolIntArg(args, "limit", 10)))

	case MemoryToolActionTrace:
		formatted := FormatRecallTraceForTool(store.LastRecallTrace())
		if formatted == "" {
			return "No recall trace available."
		}
		return formatted

	case MemoryToolActionCandidates:
		return FormatMemoryCandidatesForTool(store, toolStringArg(args, "keyword"), toolIntArg(args, "limit", 20))

	case MemoryToolActionRecall:
		query := toolStringArg(args, "query")
		if query == "" {
			return "missing query parameter"
		}
		projectPath := firstNonEmptyMemoryToolString(opts.ProjectPath, toolStringArg(args, "project_path"), toolStringArg(args, "project"))
		recall, err := store.RecallByMode(query, Category(toolStringArg(args, "category")), toolStringArg(args, "mode"), projectPath, toolIntArg(args, "limit", 0), opts.OwnerID)
		if err != nil {
			return err.Error()
		}
		return FormatRecallResultForTool(store, query, recall, toolBoolArg(args, "debug", false), true)

	case MemoryToolActionSave:
		content := toolStringArg(args, "content")
		if content == "" {
			return "missing content parameter"
		}
		category := toolStringArg(args, "category")
		if category == "" {
			category = "user_fact"
		}
		entry := Entry{Content: content, Category: Category(category), Tags: toolStringSliceArg(args, "tags"), OwnerID: opts.OwnerID}
		if len(entry.Tags) == 0 {
			entry.Tags = ExpandQuery(content).Entities
		}
		entry.Title = deriveToolMemoryTitle(content)
		contextHint := firstNonEmptyMemoryToolString(opts.ContextHint, toolStringArg(args, "_context_hint"))
		decision, err := store.SaveGovernedWithContext(entry, contextHint)
		if err != nil {
			if err == ErrMemoryCandidateRejected {
				return fmt.Sprintf("memory candidate rejected: score=%d reasons=%s", decision.Score, strings.Join(decision.Reasons, "; "))
			}
			return fmt.Sprintf("save memory failed: %s", err.Error())
		}
		if opts.AfterWrite != nil {
			opts.AfterWrite()
		}
		summary := strings.ReplaceAll(content, "\n", " ")
		if len([]rune(summary)) > 50 {
			summary = string([]rune(summary)[:50]) + "..."
		}
		if decision.Action == MemoryGovernanceQuarantine {
			return fmt.Sprintf("Memory saved as candidate: %s", summary)
		}
		return fmt.Sprintf("Memory saved: %s", summary)

	case MemoryToolActionList:
		entries := store.List(Category(toolStringArg(args, "category")), toolStringArg(args, "keyword"))
		if len(entries) == 0 {
			return "No matching memories found."
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Found %d memories\n", len(entries))
		for _, e := range entries {
			fmt.Fprintf(&b, "- [%s] (%s) %s", e.ID, e.Category, e.Content)
			if len(e.Tags) > 0 {
				fmt.Fprintf(&b, " tags=%v", e.Tags)
			}
			b.WriteByte('\n')
		}
		return b.String()

	case MemoryToolActionDelete:
		id := toolStringArg(args, "id")
		if id == "" {
			return "missing id parameter"
		}
		if err := store.Delete(id); err != nil {
			return fmt.Sprintf("delete memory failed: %s", err.Error())
		}
		if opts.AfterWrite != nil {
			opts.AfterWrite()
		}
		return fmt.Sprintf("Memory deleted: %s", id)

	default:
		return fmt.Sprintf("unknown memory action: %s (use save/recall/candidates/themes/scenes/trace/delete/list)", rawAction)
	}
}

func FormatRecallResultForTool(store *Store, query string, recall ToolRecallResult, debug bool, touch bool) string {
	if len(recall.Entries) == 0 {
		return "No relevant memories found."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Recalled %d relevant memories\n", len(recall.Entries))
	if debug && store != nil {
		if trace := store.LastRecallTrace(); trace.Query == query {
			b.WriteString(FormatRecallTraceForTool(trace))
		}
	}
	if debug && recall.AdaptivePlan != nil {
		b.WriteString(FormatAdaptiveRecallPlanForTool(*recall.AdaptivePlan))
	}
	if debug && recall.LightMemPlan != nil {
		b.WriteString(FormatLightMemRecallPlanForTool(*recall.LightMemPlan))
	}
	for _, e := range recall.Entries {
		b.WriteString(FormatRecallEntryForTool(e))
		b.WriteByte('\n')
	}
	if touch && store != nil {
		ids := make([]string, 0, len(recall.Entries))
		for _, e := range recall.Entries {
			ids = append(ids, e.ID)
		}
		store.TouchAccess(ids)
	}
	return b.String()
}

func FormatAdaptiveRecallPlanForTool(plan AdaptiveRecallPlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Adaptive plan: complexity=%s fallback=%v themes=%d expanded=%d\n", plan.Complexity, plan.Fallback, len(plan.SelectedThemes), len(plan.ExpandedEntryIDs))
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
		fmt.Fprintf(&b, "  aggregate %s: results=%d seeds=%d expanded=%d tokens=%d facets=%v\n", aggregate.ThemeID, len(aggregate.ResultEntryIDs), len(aggregate.SeedEntryIDs), len(aggregate.ExpandedEntryIDs), aggregate.TokenEstimate, aggregate.MatchedFacets)
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
	return b.String()
}

func FormatLightMemRecallPlanForTool(plan LightMemRecallPlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "LightMem plan: need_memory=%v reason=%s routes=%d budget=%d/%d\n", plan.NeedMemory, plan.Reason, len(plan.Routes), plan.MaxItems, plan.TokenBudget)
	for _, route := range plan.Routes {
		fmt.Fprintf(&b, "  route %s category=%s budget=%d project=%v candidates=%d reason=%s\n", route.Name, route.Category, route.Budget, route.ProjectScoped, plan.RouteCandidateCounts[route.Name], route.Reason)
	}
	fmt.Fprintf(&b, "  results=%v\n", plan.ResultEntryIDs)
	return b.String()
}

func FormatMemoryCandidatesForTool(store *Store, keyword string, limit int) string {
	if store == nil {
		return "long-term memory is not initialized"
	}
	candidates := store.ListMemoryCandidates(keyword, limit)
	if len(candidates) == 0 {
		return "No memory candidates found."
	}
	health := store.MemoryCandidateHealth()
	var b strings.Builder
	fmt.Fprintf(&b, "Memory candidates: %d (accept=%d quarantine=%d reject=%d stale=%d)\n", health.Total, health.Accept, health.Quarantine, health.Reject, health.Stale)
	for _, candidate := range candidates {
		entry := candidate.Entry
		content := strings.ReplaceAll(entry.Content, "\n", " ")
		if len([]rune(content)) > 120 {
			content = string([]rune(content)[:117]) + "..."
		}
		status := string(entry.Status)
		if entry.Stale {
			status += "/stale"
		}
		fmt.Fprintf(&b, "- [%s] (%s) score=%d action=%s status=%s %s\n", entry.ID, entry.Category, candidate.Decision.Score, candidate.Decision.Action, status, content)
		if len(candidate.Decision.Reasons) > 0 {
			fmt.Fprintf(&b, "  reasons=%s\n", strings.Join(candidate.Decision.Reasons, "; "))
		}
		if len(entry.Tags) > 0 {
			fmt.Fprintf(&b, "  tags=%v\n", entry.Tags)
		}
	}
	return b.String()
}

func FormatSceneIndexForTool(scenes []SceneRecord) string {
	if len(scenes) == 0 {
		return "No project scenes found."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Scene index: %d project(s)\n", len(scenes))
	for i, scene := range scenes {
		fmt.Fprintf(&b, "%d. %s\n", i+1, firstNonEmptyMemoryToolString(scene.Name, scene.ProjectPath))
		fmt.Fprintf(&b, "   project: %s entries=%d\n", scene.ProjectPath, scene.EntryCount)
		if len(scene.WorkflowTypes) > 0 {
			fmt.Fprintf(&b, "   workflows: %s\n", strings.Join(scene.WorkflowTypes, ", "))
		}
		if scene.Preview != "" {
			fmt.Fprintf(&b, "   latest: %s\n", scene.Preview)
		}
		for _, artifact := range scene.RecentArtifacts {
			fmt.Fprintf(&b, "   artifact: %s [%s]", firstNonEmptyMemoryToolString(artifact.Title, artifact.Preview), artifact.SourceType)
			if artifact.SourceURL != "" {
				fmt.Fprintf(&b, " source=%s", artifact.SourceURL)
			}
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func FormatThemesForTool(themes []ThemeNode, health ThemeHealth, includeStats bool) string {
	var b strings.Builder
	if includeStats {
		b.WriteString(FormatThemeHealthForTool(health))
	}
	if len(themes) == 0 {
		b.WriteString("No memory themes found.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "Memory themes: %d\n", len(themes))
	for _, theme := range themes {
		fmt.Fprintf(&b, "- %s members=%d tags=%v summary=%s\n", theme.ID, theme.MemberCount, theme.Tags, theme.Summary)
		if len(theme.Neighbors) > 0 {
			fmt.Fprintf(&b, "  neighbors=%v\n", theme.Neighbors)
		}
	}
	return b.String()
}

func FormatThemeHealthForTool(health ThemeHealth) string {
	return fmt.Sprintf("theme_health: themes=%d active=%d covered=%d uncovered=%d coverage=%.2f avg_size=%.2f max_size=%d neighbor_links=%d isolated=%d duplicate_refs=%d\n",
		health.ThemeCount, health.ActiveEligibleEntries, health.CoveredEntries, health.UncoveredEntries, health.CoverageRate, health.AverageThemeSize, health.MaxThemeSize, health.NeighborLinks, health.IsolatedThemes, health.DuplicateEntryRefs)
}

func FormatThemeExplanationsForTool(explanations []ThemeExplanation, health ThemeHealth, includeStats bool) string {
	if len(explanations) == 0 {
		return FormatThemesForTool(nil, health, includeStats)
	}
	var b strings.Builder
	if includeStats {
		b.WriteString(FormatThemeHealthForTool(health))
	}
	fmt.Fprintf(&b, "Memory themes: %d\n", len(explanations))
	for _, explanation := range explanations {
		theme := explanation.Theme
		fmt.Fprintf(&b, "- %s members=%d cohesion=%.2f tags=%v summary=%s\n", theme.ID, theme.MemberCount, explanation.Cohesion, theme.Tags, theme.Summary)
		for _, ev := range explanation.Evidence {
			fmt.Fprintf(&b, "  evidence %s source=%s sim=%.2f access=%d %s\n", ev.EntryID, ev.SourceType, ev.Similarity, ev.AccessCount, ev.ContentPreview)
			if ev.SourceURL != "" {
				fmt.Fprintf(&b, "    url=%s\n", ev.SourceURL)
			}
		}
	}
	return b.String()
}

func FormatThemeDiagnosticsForTool(report ThemeDiagnosticReport, explanations []ThemeExplanation, includeEvidence bool) string {
	var b strings.Builder
	b.WriteString(FormatThemeHealthForTool(report.Health))
	if len(report.Issues) == 0 {
		b.WriteString("theme_diagnostics: no actionable issues\n")
	} else {
		fmt.Fprintf(&b, "theme_diagnostics: issues=%d\n", len(report.Issues))
		for _, issue := range report.Issues {
			target := firstNonEmptyMemoryToolString(issue.ThemeID, issue.EntryID, "-")
			fmt.Fprintf(&b, "- [%s] %s %s: %s\n", issue.Severity, issue.Kind, target, issue.Message)
			fmt.Fprintf(&b, "  suggestion: %s\n", issue.Suggestion)
		}
	}
	if includeEvidence {
		b.WriteString(FormatThemeExplanationsForTool(explanations, report.Health, false))
	}
	return b.String()
}

func FormatThemeMaintenancePlanForTool(plan ThemeMaintenancePlan, report ThemeDiagnosticReport, explanations []ThemeExplanation, includeEvidence bool) string {
	var b strings.Builder
	b.WriteString(FormatThemeHealthForTool(plan.Health))
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
		b.WriteString(FormatThemeExplanationsForTool(explanations, plan.Health, false))
	}
	return b.String()
}

func FormatThemeMaintenanceResultForTool(result ThemeMaintenanceResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "theme_maintenance_result: requested=%d rebuilt=%v backfilled=%d coverage=%.2f->%.2f\n", result.RequestedActions, result.RebuiltThemes, result.BackfilledEmbeddings, result.Before.CoverageRate, result.After.CoverageRate)
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

func toolStringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	if v, ok := args[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func toolBoolArg(args map[string]interface{}, key string, fallback bool) bool {
	if args == nil {
		return fallback
	}
	switch v := args[key].(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off":
			return false
		}
	}
	return fallback
}

func toolIntArg(args map[string]interface{}, key string, fallback int) int {
	if args == nil {
		return fallback
	}
	switch v := args[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &n); err == nil {
			return n
		}
	}
	return fallback
}

func toolStringSliceArg(args map[string]interface{}, key string) []string {
	if args == nil {
		return nil
	}
	raw, ok := args[key]
	if !ok {
		return nil
	}
	var out []string
	switch v := raw.(type) {
	case []string:
		out = append(out, v...)
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	case string:
		for _, item := range strings.Split(v, ",") {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
	}
	return out
}

func deriveToolMemoryTitle(content string) string {
	for _, line := range strings.SplitN(content, "\n", 10) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(strings.TrimPrefix(line, "## "), "# ")
		if runes := []rune(line); len(runes) > 60 {
			return string(runes[:60])
		}
		return line
	}
	return ""
}

func firstNonEmptyMemoryToolString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
