package knowledge

import (
	"context"
	"math"
	"sort"
	"strings"
)

func (s *SQLiteStore) SourceQualityReport(ctx context.Context, opts ListSourcesOptions) (SourceQualityReport, error) {
	opts.Limit = sourceQualityReportLimit(opts)
	sources, err := s.ListSources(ctx, opts)
	if err != nil {
		return SourceQualityReport{}, err
	}
	report := SourceQualityReport{
		Grades:  map[string]int{},
		Signals: map[string]int{},
		Actions: map[string]int{},
		Items:   make([]SourceQualityItem, 0, len(sources)),
		Notes:   []string{"local_source_quality_no_llm"},
	}
	sensitiveCounts, sensitiveErr := s.sensitiveFindingCountsBySource(ctx)
	if sensitiveErr != nil {
		report.Notes = append(report.Notes, "sensitive_scan_unavailable:"+sensitiveErr.Error())
	}
	duplicateCounts, duplicateErr := s.duplicateClaimCountsBySource(ctx)
	if duplicateErr != nil {
		report.Notes = append(report.Notes, "duplicate_check_unavailable:"+duplicateErr.Error())
	}
	linkCounts, linkErr := s.sourceLinkCountsBySource(ctx)
	if linkErr != nil {
		report.Notes = append(report.Notes, "source_link_check_unavailable:"+linkErr.Error())
	}
	total := 0
	for _, source := range sources {
		item := sourceQualityItem(source, sensitiveCounts[source.ID], duplicateCounts[source.ID], linkCounts[source.ID], len(sources))
		if !sourceQualityItemMatchesOptions(item, opts) {
			continue
		}
		total += item.Score
		report.Grades[item.Grade]++
		for _, signal := range item.Signals {
			report.Signals[signal]++
		}
		for _, action := range item.Actions {
			report.Actions[action]++
		}
		report.Items = append(report.Items, item)
	}
	report.Count = len(report.Items)
	if len(report.Items) > 0 {
		report.AverageScore = math.Round((float64(total)/float64(len(report.Items)))*100) / 100
	}
	sort.SliceStable(report.Items, func(i, j int) bool {
		left := report.Items[i]
		right := report.Items[j]
		if left.Score != right.Score {
			return left.Score < right.Score
		}
		if left.SensitiveFindings != right.SensitiveFindings {
			return left.SensitiveFindings > right.SensitiveFindings
		}
		if left.DuplicateClaims != right.DuplicateClaims {
			return left.DuplicateClaims > right.DuplicateClaims
		}
		return left.Source.UpdatedAt.After(right.Source.UpdatedAt)
	})
	return report, nil
}

func sourceQualityReportLimit(opts ListSourcesOptions) int {
	return sourceFilterLimit(opts, 100, 5000, 5000)
}

func SourceQualityMaintenancePolicies() []SourceQualityMaintenancePolicy {
	return []SourceQualityMaintenancePolicy{
		{
			Name:                      "conservative",
			Title:                     "Conservative local cleanup",
			Description:               "Refresh missing parsed nodes, backfill labels, rebuild missing cards/facts from existing parsed nodes, and refresh topic links. No sensitive-source disable, no duplicate suppression, no LLM.",
			Actions:                   []string{"refresh_or_reimport_missing_nodes", "backfill_labels", "rebuild_derived_gaps", "refresh_topic_links"},
			DefaultDryRun:             true,
			DistillMode:               "rules_only",
			MaxSourcesPerAction:       50,
			QueryRequiresLLM:          false,
			RequiresExplicitWrite:     true,
			RequiresExplicitSensitive: true,
			RequiresExplicitDuplicate: true,
			Notes:                     []string{"query_no_llm", "write_rules_only", "no_sensitive_disable", "no_duplicate_suppression"},
		},
		{
			Name:                      "balanced",
			Title:                     "Balanced quality maintenance",
			Description:               "Refresh missing parsed nodes, rebuild gaps, backfill labels, and reversibly suppress duplicate card groups. Does not disable sensitive sources and does not use LLM.",
			Actions:                   []string{"refresh_or_reimport_missing_nodes", "rebuild_derived_gaps", "refresh_topic_links", "backfill_labels", "suppress_duplicate_groups"},
			DefaultDryRun:             true,
			DistillMode:               "rules_only",
			MaxSourcesPerAction:       100,
			AllowDuplicateSuppression: true,
			QueryRequiresLLM:          false,
			RequiresExplicitWrite:     true,
			RequiresExplicitSensitive: true,
			RequiresExplicitDuplicate: true,
			Notes:                     []string{"query_no_llm", "write_rules_only", "duplicate_suppression_reversible"},
		},
		{
			Name:                      "enriched",
			Title:                     "Enriched structuring",
			Description:               "Balanced maintenance with missing-node refresh and llm_if_available for derived rebuilds, so storage quality can improve when MaClaw LLM is available. Query still stays local.",
			Actions:                   []string{"refresh_or_reimport_missing_nodes", "rebuild_derived_gaps", "refresh_topic_links", "backfill_labels", "suppress_duplicate_groups"},
			DefaultDryRun:             true,
			DistillMode:               "llm_if_available",
			MaxSourcesPerAction:       50,
			AllowDuplicateSuppression: true,
			QueryRequiresLLM:          false,
			MayUseLLMForStructuring:   true,
			RequiresExplicitWrite:     true,
			RequiresExplicitSensitive: true,
			RequiresExplicitDuplicate: true,
			Notes:                     []string{"query_no_llm", "storage_may_use_llm", "duplicate_suppression_reversible"},
		},
		{
			Name:                      "strict",
			Title:                     "Strict isolation and cleanup",
			Description:               "Disable possible sensitive sources, refresh missing parsed nodes, rebuild gaps, backfill labels, and reversibly suppress duplicate card groups. No LLM unless distill_mode is overridden.",
			Actions:                   []string{"disable_sensitive_sources", "refresh_or_reimport_missing_nodes", "rebuild_derived_gaps", "refresh_topic_links", "backfill_labels", "suppress_duplicate_groups"},
			DefaultDryRun:             true,
			DistillMode:               "rules_only",
			MaxSourcesPerAction:       100,
			AllowSensitiveDisable:     true,
			AllowDuplicateSuppression: true,
			QueryRequiresLLM:          false,
			RequiresExplicitWrite:     true,
			RequiresExplicitSensitive: true,
			RequiresExplicitDuplicate: true,
			Notes:                     []string{"query_no_llm", "sensitive_sources_disabled", "duplicate_suppression_reversible"},
		},
	}
}

func SourceQualityMaintenancePolicyByName(name string) (SourceQualityMaintenancePolicy, bool) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return SourceQualityMaintenancePolicy{}, false
	}
	for _, policy := range SourceQualityMaintenancePolicies() {
		if policy.Name == normalized {
			return policy, true
		}
	}
	return SourceQualityMaintenancePolicy{}, false
}

func (s *SQLiteStore) SourceQualityMaintenancePlan(ctx context.Context, opts ListSourcesOptions) (SourceQualityMaintenancePlan, error) {
	report, err := s.SourceQualityReport(ctx, opts)
	if err != nil {
		return SourceQualityMaintenancePlan{}, err
	}
	plan := SourceQualityMaintenancePlan{
		Quality: report,
		Notes:   []string{"local_quality_maintenance_plan_no_llm", "plan_only_no_writes"},
	}
	add := func(action SourceQualityMaintenanceAction) {
		if action.Count == 0 {
			action.Count = len(action.SourceIDs)
		}
		if action.Count == 0 {
			return
		}
		plan.Actions = append(plan.Actions, action)
	}
	add(SourceQualityMaintenanceAction{
		Kind:        "disable_sensitive_sources",
		Title:       "Disable sensitive sources",
		Description: "Disable sources with possible_sensitive_content so default local recall excludes likely credentials or secrets.",
		Severity:    "critical",
		SourceIDs:   sourceQualityIDsWithSignal(report.Items, "possible_sensitive_content"),
		Signals:     []string{"possible_sensitive_content"},
		Tool:        "knowledge_disable_quality_sensitive_sources",
		Args:        sourceQualityToolArgs(opts, "possible_sensitive_content"),
	})
	add(SourceQualityMaintenanceAction{
		Kind:        "rebuild_derived_gaps",
		Title:       "Rebuild cards and facts",
		Description: "Rebuild KnowledgeCard and KnowledgeFact from existing DocumentNode for parsed sources with missing_cards or missing_facts.",
		Severity:    "warning",
		SourceIDs:   sourceQualityIDsWithAnySignalExcluding(report.Items, []string{"missing_cards", "missing_facts"}, "missing_nodes"),
		Signals:     []string{"missing_cards", "missing_facts"},
		Tool:        "knowledge_rebuild_quality_gaps",
		Args:        sourceQualityToolArgs(opts, "missing_cards_or_missing_facts"),
	})
	add(SourceQualityMaintenanceAction{
		Kind:        "backfill_labels",
		Title:       "Backfill auto labels",
		Description: "Add local rule-based labels such as kind, scope, domain, and folder for unlabeled sources.",
		Severity:    "info",
		SourceIDs:   sourceQualityIDsWithSignal(report.Items, "missing_labels"),
		Signals:     []string{"missing_labels"},
		Tool:        "knowledge_backfill_quality_labels",
		Args:        sourceQualityToolArgs(opts, "missing_labels"),
	})
	add(SourceQualityMaintenanceAction{
		Kind:        "refresh_topic_links",
		Title:       "Refresh topic links",
		Description: "Connect active sources with missing topic_related links into the local source graph using topic relevance. Query-time recall still does not call an LLM.",
		Severity:    "info",
		SourceIDs:   sourceQualityIDsWithSignal(report.Items, "missing_links"),
		Signals:     []string{"missing_links"},
		Tool:        "knowledge_refresh_topic_links",
		Args:        sourceQualityToolArgs(opts, "missing_links"),
	})
	add(SourceQualityMaintenanceAction{
		Kind:        "suppress_duplicate_groups",
		Title:       "Suppress duplicate card groups",
		Description: "Reversibly suppress duplicate card claims touching the selected low-quality sources.",
		Severity:    "warning",
		SourceIDs:   sourceQualityIDsWithSignal(report.Items, "duplicate_card_claims"),
		Signals:     []string{"duplicate_card_claims"},
		Tool:        "knowledge_suppress_quality_duplicate_groups",
		Args:        sourceQualityToolArgs(opts, "duplicate_card_claims"),
	})
	add(SourceQualityMaintenanceAction{
		Kind:        "refresh_or_reimport_missing_nodes",
		Title:       "Refresh or reimport missing nodes",
		Description: "Refresh or reimport sources that have no parsed DocumentNode; existing-node rebuild cannot repair these.",
		Severity:    "warning",
		SourceIDs:   sourceQualityIDsWithSignal(report.Items, "missing_nodes"),
		Signals:     []string{"missing_nodes"},
		Tool:        "knowledge_refresh_sources",
		Args:        sourceQualityToolArgs(opts, "missing_nodes"),
	})
	plan.Count = len(plan.Actions)
	sort.SliceStable(plan.Actions, func(i, j int) bool {
		left := plan.Actions[i]
		right := plan.Actions[j]
		if sourceQualityActionPriority(left.Kind) != sourceQualityActionPriority(right.Kind) {
			return sourceQualityActionPriority(left.Kind) < sourceQualityActionPriority(right.Kind)
		}
		if left.Count != right.Count {
			return left.Count > right.Count
		}
		return left.Kind < right.Kind
	})
	return plan, nil
}

func (s *SQLiteStore) ExecuteSourceQualityMaintenancePlan(ctx context.Context, req SourceQualityMaintenanceExecuteRequest) (SourceQualityMaintenanceExecuteResult, error) {
	if policy, ok := SourceQualityMaintenancePolicyByName(req.Policy); ok {
		if len(req.Actions) == 0 {
			req.Actions = append([]string{}, policy.Actions...)
		}
		if strings.TrimSpace(req.DistillMode) == "" {
			req.DistillMode = policy.DistillMode
		}
		if req.MaxSourcesPerAction <= 0 {
			req.MaxSourcesPerAction = policy.MaxSourcesPerAction
		}
		req.AllowSensitiveDisable = req.AllowSensitiveDisable || policy.AllowSensitiveDisable
		req.AllowDuplicateSuppression = req.AllowDuplicateSuppression || policy.AllowDuplicateSuppression
	}
	explicitSources := sourceQualityExplicitSourceLimit(req.Filter.SourceIDs)
	if explicitSources > req.Filter.Limit {
		req.Filter.Limit = explicitSources
	}
	plan, err := s.SourceQualityMaintenancePlan(ctx, req.Filter)
	if err != nil {
		return SourceQualityMaintenanceExecuteResult{}, err
	}
	req.MaxSourcesPerAction = sourceQualityExecutionMaxSources(req.MaxSourcesPerAction)
	if explicitSources > req.MaxSourcesPerAction {
		req.MaxSourcesPerAction = explicitSources
	}
	result := SourceQualityMaintenanceExecuteResult{
		Plan:   plan,
		DryRun: req.DryRun,
		Notes:  []string{"local_quality_maintenance_execute_no_llm"},
	}
	if policy, ok := SourceQualityMaintenancePolicyByName(req.Policy); ok {
		result.Notes = append(result.Notes, "policy_"+policy.Name)
		if policy.MayUseLLMForStructuring {
			result.Notes = append(result.Notes, "storage_may_use_llm_for_structuring")
		}
	}
	wanted := sourceQualityWantedActions(req.Actions)
	for _, action := range plan.Actions {
		if len(wanted) > 0 {
			if _, ok := wanted[action.Kind]; !ok {
				continue
			}
		}
		if len(action.SourceIDs) > req.MaxSourcesPerAction {
			result.Results = append(result.Results, SourceQualityMaintenanceActionResult{
				Kind:      action.Kind,
				Requested: len(action.SourceIDs),
				Skipped:   len(action.SourceIDs),
				DryRun:    req.DryRun,
				SourceIDs: append([]string{}, action.SourceIDs...),
				Error:     "max_sources_per_action_exceeded",
			})
			continue
		}
		actionResult := s.executeSourceQualityMaintenanceAction(ctx, action, req)
		result.Results = append(result.Results, actionResult)
		result.Warnings = append(result.Warnings, actionResult.Warnings...)
	}
	result.Count = len(result.Results)
	return result, nil
}

func (s *SQLiteStore) executeSourceQualityMaintenanceAction(ctx context.Context, action SourceQualityMaintenanceAction, req SourceQualityMaintenanceExecuteRequest) SourceQualityMaintenanceActionResult {
	result := SourceQualityMaintenanceActionResult{
		Kind:      action.Kind,
		Requested: len(action.SourceIDs),
		DryRun:    req.DryRun,
		SourceIDs: append([]string{}, action.SourceIDs...),
	}
	if len(action.SourceIDs) == 0 {
		return result
	}
	switch action.Kind {
	case "disable_sensitive_sources":
		if !req.DryRun && !req.AllowSensitiveDisable {
			result.Skipped = len(action.SourceIDs)
			result.Error = "allow_sensitive_disable_required"
			return result
		}
		if req.DryRun {
			return result
		}
		update := s.DisableSources(ctx, action.SourceIDs)
		result.Updated = update.Updated
		result.Failed = update.Failed
		result.Result = update
	case "rebuild_derived_gaps":
		if req.DryRun {
			return result
		}
		rebuild := s.RebuildSourcesDerived(ctx, action.SourceIDs, req.DistillMode)
		result.Updated = rebuild.Rebuilt
		result.Failed = rebuild.Failed
		result.Result = rebuild
		result.Warnings = append(result.Warnings, rebuild.Warnings...)
	case "backfill_labels":
		backfill, err := s.BackfillSourceAutoLabels(ctx, SourceAutoLabelBackfillRequest{
			SourceIDs: action.SourceIDs,
			DryRun:    req.DryRun,
			Limit:     len(action.SourceIDs),
		})
		if err != nil {
			result.Failed = len(action.SourceIDs)
			result.Error = err.Error()
			return result
		}
		result.Updated = backfill.Updated
		result.Failed = backfill.Failed
		result.Result = backfill
	case "refresh_topic_links":
		if req.DryRun {
			return result
		}
		refresh := SourceTopicLinkBuildResult{
			Scanned: len(action.SourceIDs),
			Notes:   []string{"local_quality_maintenance_topic_links_no_llm"},
		}
		for _, sourceID := range action.SourceIDs {
			item, err := s.RefreshSourceTopicLinks(ctx, sourceID, 8)
			if err != nil {
				refresh.Skipped++
				refresh.Notes = append(refresh.Notes, sourceID+":"+err.Error())
				continue
			}
			refresh.Linked += item.Linked
			refresh.Skipped += item.Skipped
			refresh.Links = append(refresh.Links, item.Links...)
			result.Updated++
		}
		result.Skipped = refresh.Skipped
		result.Result = refresh
	case "suppress_duplicate_groups":
		if !req.DryRun && !req.AllowDuplicateSuppression {
			result.Skipped = len(action.SourceIDs)
			result.Error = "allow_duplicate_suppression_required"
			return result
		}
		suppressed := s.suppressDuplicateGroupsForSourceIDs(ctx, action.SourceIDs, req.DryRun)
		result.Requested = suppressed.Requested
		result.Updated = suppressed.Updated
		result.Failed = suppressed.Failed
		result.Skipped = suppressed.Skipped
		result.Result = suppressed
		if suppressed.Error != "" {
			result.Error = suppressed.Error
		}
	case "refresh_or_reimport_missing_nodes":
		if req.DryRun {
			preview := s.PreviewSourcesRefresh(ctx, action.SourceIDs)
			result.Updated = preview.Changed
			result.Failed = preview.Failed
			result.Skipped = preview.Unchanged
			result.Result = preview
			if preview.Failed > 0 {
				result.Error = "refresh_or_reimport_preview_failed"
			}
			return result
		}
		refresh := s.RefreshSources(ctx, action.SourceIDs)
		result.Updated = refresh.Refreshed
		result.Failed = refresh.Failed
		result.Result = refresh
		result.Warnings = append(result.Warnings, refresh.Warnings...)
		if refresh.Failed > 0 {
			result.Error = "refresh_or_reimport_failed"
		}
	default:
		result.Skipped = len(action.SourceIDs)
		result.Error = "unsupported_quality_maintenance_action"
	}
	return result
}

func sourceQualityExecutionMaxSources(value int) int {
	if value <= 0 {
		return 100
	}
	if value > 5000 {
		return 5000
	}
	return value
}

func sourceFilterLimit(opts ListSourcesOptions, fallback, normalMax, explicitMax int) int {
	if fallback <= 0 {
		fallback = 100
	}
	if normalMax <= 0 {
		normalMax = fallback
	}
	if explicitMax < normalMax {
		explicitMax = normalMax
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = fallback
	}
	if explicitSources := sourceQualityExplicitSourceLimit(opts.SourceIDs); explicitSources > limit {
		limit = explicitSources
	}
	maxLimit := normalMax
	if sourceQualityExplicitSourceLimit(opts.SourceIDs) > normalMax {
		maxLimit = explicitMax
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return limit
}

func sourceQualityExplicitSourceLimit(sourceIDs []string) int {
	if len(sourceIDs) == 0 {
		return 0
	}
	seen := make(map[string]struct{}, len(sourceIDs))
	for _, id := range sourceIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		seen[id] = struct{}{}
	}
	if len(seen) > 5000 {
		return 5000
	}
	return len(seen)
}

func sourceQualityItemMatchesOptions(item SourceQualityItem, opts ListSourcesOptions) bool {
	grades := normalizeSearchStrings(append(append([]string{}, opts.QualityGrades...), opts.QualityGrade))
	if len(grades) > 0 {
		matched := false
		for _, grade := range grades {
			if item.Grade == grade {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if opts.MinQualityScore > 0 && item.Score < opts.MinQualityScore {
		return false
	}
	if opts.MaxQualityScore > 0 && item.Score > opts.MaxQualityScore {
		return false
	}
	return true
}

type sourceQualityDuplicateSuppressionSummary struct {
	Requested int      `json:"requested"`
	Updated   int      `json:"updated"`
	Failed    int      `json:"failed"`
	Skipped   int      `json:"skipped"`
	DryRun    bool     `json:"dry_run,omitempty"`
	GroupKeys []string `json:"group_keys,omitempty"`
	Errors    []string `json:"errors,omitempty"`
	Error     string   `json:"error,omitempty"`
}

func (s *SQLiteStore) suppressDuplicateGroupsForSourceIDs(ctx context.Context, sourceIDs []string, dryRun bool) sourceQualityDuplicateSuppressionSummary {
	summary := sourceQualityDuplicateSuppressionSummary{DryRun: dryRun}
	idSet := map[string]struct{}{}
	for _, id := range sourceIDs {
		if id != "" {
			idSet[id] = struct{}{}
		}
	}
	groups, err := s.ListDuplicateCards(ctx, 1000)
	if err != nil {
		summary.Error = err.Error()
		return summary
	}
	for _, group := range groups {
		if !sourceQualityStringSetIntersects(idSet, group.SourceIDs) {
			summary.Skipped++
			continue
		}
		summary.Requested++
		summary.GroupKeys = appendLimited(summary.GroupKeys, group.Key, 20)
		if dryRun {
			continue
		}
		result, err := s.SuppressDuplicateCards(ctx, DuplicateCardSuppressionRequest{
			Key:         group.Key,
			OwnerID:     group.OwnerID,
			TenantID:    group.TenantID,
			ProjectPath: group.ProjectPath,
			Reason:      "quality_maintenance_plan",
		})
		if err != nil {
			summary.Failed++
			summary.Errors = append(summary.Errors, err.Error())
			continue
		}
		summary.Updated += result.Suppressed
	}
	return summary
}

func sourceQualityStringSetIntersects(set map[string]struct{}, values []string) bool {
	for _, value := range values {
		if _, ok := set[value]; ok {
			return true
		}
	}
	return false
}

func sourceQualityWantedActions(actions []string) map[string]struct{} {
	result := map[string]struct{}{}
	for _, action := range normalizeSearchStrings(actions) {
		if action != "" {
			result[action] = struct{}{}
		}
	}
	return result
}

func sourceQualityIDsWithSignal(items []SourceQualityItem, signal string) []string {
	return sourceQualityIDsWithAnySignal(items, signal)
}

func sourceQualityIDsWithAnySignal(items []SourceQualityItem, signals ...string) []string {
	return sourceQualityIDsWithAnySignalExcluding(items, signals, "")
}

func sourceQualityIDsWithAnySignalExcluding(items []SourceQualityItem, signals []string, excludedSignals ...string) []string {
	want := map[string]struct{}{}
	for _, signal := range signals {
		want[signal] = struct{}{}
	}
	excluded := map[string]struct{}{}
	for _, signal := range excludedSignals {
		if strings.TrimSpace(signal) != "" {
			excluded[signal] = struct{}{}
		}
	}
	result := make([]string, 0)
	seen := map[string]struct{}{}
	for _, item := range items {
		id := item.Source.ID
		if id == "" {
			continue
		}
		skip := false
		for _, signal := range item.Signals {
			if _, ok := excluded[signal]; ok {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		for _, signal := range item.Signals {
			if _, ok := want[signal]; !ok {
				continue
			}
			if _, ok := seen[id]; ok {
				break
			}
			seen[id] = struct{}{}
			result = append(result, id)
			break
		}
	}
	return result
}

func sourceQualityToolArgs(opts ListSourcesOptions, signal string) map[string]interface{} {
	args := map[string]interface{}{}
	if opts.Query != "" {
		args["query"] = opts.Query
	}
	if opts.Kind != "" {
		args["kind"] = opts.Kind
	}
	if len(opts.SourceKinds) > 0 {
		args["source_kinds"] = opts.SourceKinds
	}
	if opts.Status != "" {
		args["status"] = opts.Status
	}
	if opts.Domain != "" {
		args["domain"] = opts.Domain
	}
	if opts.Label != "" {
		args["label"] = opts.Label
	}
	if len(opts.Labels) > 0 {
		args["labels"] = opts.Labels
	}
	if len(opts.SourceIDs) > 0 {
		args["source_ids"] = opts.SourceIDs
	}
	if opts.ProjectPath != "" {
		args["project_path"] = opts.ProjectPath
	}
	if opts.OwnerID != "" {
		args["owner_id"] = opts.OwnerID
	}
	if opts.TenantID != "" {
		args["tenant_id"] = opts.TenantID
	}
	if opts.CoverageFilter != "" {
		args["coverage_filter"] = opts.CoverageFilter
	}
	if opts.QualityGrade != "" {
		args["quality_grade"] = opts.QualityGrade
	}
	if len(opts.QualityGrades) > 0 {
		args["quality_grades"] = opts.QualityGrades
	}
	if opts.MinQualityScore > 0 {
		args["min_quality_score"] = opts.MinQualityScore
	}
	if opts.MaxQualityScore > 0 {
		args["max_quality_score"] = opts.MaxQualityScore
	}
	if opts.Limit > 0 {
		args["limit"] = opts.Limit
	}
	if signal != "" {
		args["quality_signal"] = signal
	}
	return args
}

func sourceQualityActionPriority(kind string) int {
	switch kind {
	case "disable_sensitive_sources":
		return 0
	case "refresh_or_reimport_missing_nodes":
		return 1
	case "rebuild_derived_gaps":
		return 2
	case "refresh_topic_links":
		return 3
	case "suppress_duplicate_groups":
		return 4
	case "backfill_labels":
		return 5
	default:
		return 10
	}
}

func (s *SQLiteStore) sensitiveFindingCountsBySource(ctx context.Context) (map[string]int, error) {
	result, err := s.ScanSensitiveContent(ctx, 1000)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, finding := range result.Findings {
		if finding.SourceID == "" {
			continue
		}
		counts[finding.SourceID]++
	}
	return counts, nil
}

func (s *SQLiteStore) duplicateClaimCountsBySource(ctx context.Context) (map[string]int, error) {
	groups, err := s.ListDuplicateCards(ctx, 1000)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, group := range groups {
		for _, sourceID := range group.SourceIDs {
			counts[sourceID]++
		}
	}
	return counts, nil
}

func (s *SQLiteStore) sourceLinkCountsBySource(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source_id, COUNT(*) FROM knowledge_source_links GROUP BY source_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var sourceID string
		var count int
		if err := rows.Scan(&sourceID, &count); err != nil {
			return nil, err
		}
		if sourceID != "" {
			counts[sourceID] = count
		}
	}
	return counts, rows.Err()
}

func sourceQualityItem(source Source, sensitiveFindings, duplicateClaims, sourceLinks, totalSources int) SourceQualityItem {
	score := 100
	signals := make([]string, 0)
	actions := make([]string, 0)
	switch source.Status {
	case StatusFailed:
		score -= 45
		signals = append(signals, "failed_source")
		actions = append(actions, "inspect_error_and_refresh_or_reimport")
	case StatusPending:
		score -= 25
		signals = append(signals, "pending_source")
		actions = append(actions, "wait_for_import_or_retry_batch")
	case StatusStale:
		score -= 15
		signals = append(signals, "stale_source")
		actions = append(actions, "preview_and_refresh_source")
	case StatusDisabled:
		score -= 20
		signals = append(signals, "disabled_source")
		actions = append(actions, "enable_if_the_source_should_participate_in_recall")
	}
	if source.NodeCount == 0 && source.Status != StatusFailed && source.Status != StatusPending {
		score -= 25
		signals = append(signals, "missing_nodes")
		actions = append(actions, "refresh_or_reimport_to_rebuild_parsed_nodes")
	}
	if source.CardCount == 0 && source.Status != StatusFailed && source.Status != StatusPending {
		score -= 25
		signals = append(signals, "missing_cards")
		if source.NodeCount > 0 {
			actions = append(actions, "rebuild_cards_and_facts_from_existing_nodes")
		}
	}
	if source.FactCount == 0 && (source.Status == StatusDistilled || source.Status == StatusStale) {
		score -= 10
		signals = append(signals, "missing_facts")
		if source.NodeCount > 0 {
			actions = append(actions, "rebuild_cards_and_facts_from_existing_nodes")
		}
	}
	if totalSources > 1 && sourceLinks == 0 && source.Status != StatusFailed && source.Status != StatusPending && source.Status != StatusDisabled {
		score -= 5
		signals = append(signals, "missing_links")
		actions = append(actions, "refresh_topic_links_to_connect_source_graph")
	}
	if len(source.Labels) == 0 && source.Status != StatusDisabled {
		score -= 8
		signals = append(signals, "missing_labels")
		actions = append(actions, "backfill_auto_labels_or_add_manual_labels")
	}
	if source.SourceTrust > 0 && source.SourceTrust < 0.4 {
		score -= 10
		signals = append(signals, "low_source_trust")
		actions = append(actions, "review_source_trust")
	}
	if sensitiveFindings > 0 {
		score -= 40
		signals = append(signals, "possible_sensitive_content")
		actions = append(actions, "disable_sensitive_source_or_redact_content")
	}
	if duplicateClaims > 0 {
		score -= 8 * duplicateClaims
		signals = append(signals, "duplicate_card_claims")
		actions = append(actions, "inspect_and_suppress_duplicate_cards")
	}
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return SourceQualityItem{
		Source:            source,
		Score:             score,
		Grade:             sourceQualityGrade(score),
		Signals:           uniqueTrimmed(signals),
		Actions:           uniqueTrimmed(actions),
		SensitiveFindings: sensitiveFindings,
		DuplicateClaims:   duplicateClaims,
	}
}

func sourceQualityGrade(score int) string {
	switch {
	case score >= 90:
		return "excellent"
	case score >= 75:
		return "good"
	case score >= 55:
		return "needs_attention"
	default:
		return "poor"
	}
}
