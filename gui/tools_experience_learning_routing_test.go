package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestExperienceLearningToolRecoveryGovernanceSummarizesAdaptiveRetry(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)

	retry := NewAdaptiveRetry(nil)
	retry.SetMemoryStore(store)
	for i := 0; i < adaptiveRetryReviewThreshold; i++ {
		retry.RecordFailure("write_file", FailureArgs, RetryDecision{Action: RetryActionFix, Attempt: i})
	}
	retry.RecordFailure("browser_open", FailureNetwork, RetryDecision{Action: RetryActionRetry, Attempt: 0, ProviderName: "ChatFire", Model: "gpt-5.1-codex-mini", WireAPI: "responses"})

	app := &App{memoryStore: store}
	direct := app.QueryExperienceToolRecoverySummaries(ExperienceToolRecoveryQuery{ReviewOnly: true, Limit: 5})
	if direct.Count != 1 || direct.ReviewRequiredCount != 1 || direct.Returned != 1 {
		t.Fatalf("expected one review-required recovery summary: %#v", direct)
	}
	if direct.Summaries[0].ToolName != "write_file" || direct.Summaries[0].Category != "args" || direct.Summaries[0].FailureCount != 3 || direct.Summaries[0].FirstObservedAt == "" || direct.Summaries[0].LastObservedAt == "" {
		t.Fatalf("unexpected recovery summary row: %#v", direct.Summaries[0])
	}
	if direct.ToolCounts["write_file"] != 1 || direct.CategoryCounts["args"] != 1 || !strings.Contains(direct.NonExecutingBoundary, "read-only tool recovery") {
		t.Fatalf("unexpected recovery governance counts/boundary: %#v", direct)
	}
	directToolCounts, directToolCountsOK := direct.RecommendedFocusContext["tool_counts"].(map[string]int)
	directCategoryCounts, directCategoryCountsOK := direct.RecommendedFocusContext["category_counts"].(map[string]int)
	if !directToolCountsOK || !directCategoryCountsOK || directToolCounts["write_file"] != 1 || directCategoryCounts["args"] != 1 || direct.RecommendedFocusContext["recommended_tool"] != "write_file" || direct.RecommendedFocusContext["recommended_category"] != "args" {
		t.Fatalf("recovery focus context should expose count maps and recommended row dimensions: %#v", direct.RecommendedFocusContext)
	}
	providerDirect := app.QueryExperienceToolRecoverySummaries(ExperienceToolRecoveryQuery{Provider: "chatfire", Model: "GPT-5.1-CODEX-MINI", Limit: 5})
	if providerDirect.Count != 1 || providerDirect.Summaries[0].ToolName != "browser_open" || providerDirect.ProviderCounts["ChatFire"] != 1 || providerDirect.ModelCounts["gpt-5.1-codex-mini"] != 1 || providerDirect.WireAPICounts["responses"] != 1 {
		t.Fatalf("expected provider/model/wire_api filtered recovery summary: %#v", providerDirect)
	}
	tagModelDirect := app.QueryExperienceToolRecoverySummaries(ExperienceToolRecoveryQuery{Provider: "chatfire", Model: "gpt-5-1-codex-mini", Limit: 5})
	if tagModelDirect.Count != 1 || tagModelDirect.Summaries[0].ToolName != "browser_open" {
		t.Fatalf("expected safe-tag model filter to match provider recovery summary: %#v", tagModelDirect)
	}
	providerArgs, ok := providerDirect.RecommendedToolCall["args"].(map[string]interface{})
	if !ok || providerArgs["provider"] != "chatfire" || providerArgs["model"] != "GPT-5.1-CODEX-MINI" {
		t.Fatalf("provider/model filters should survive recommended call: %#v", providerDirect.RecommendedToolCall)
	}
	if providerDirect.RecommendedFocusContext["recommended_provider"] != "ChatFire" || providerDirect.RecommendedFocusContext["recommended_model"] != "gpt-5.1-codex-mini" || providerDirect.RecommendedFocusContext["recommended_wire_api"] != "responses" {
		t.Fatalf("provider focus context should expose recommended provider/model/wire dimensions: %#v", providerDirect.RecommendedFocusContext)
	}
	wireDirect := app.QueryExperienceToolRecoverySummaries(ExperienceToolRecoveryQuery{WireAPI: "RESPONSES", Limit: 5})
	if wireDirect.Count != 1 || wireDirect.Summaries[0].ToolName != "browser_open" || wireDirect.WireAPICounts["responses"] != 1 {
		t.Fatalf("expected wire_api filtered recovery summary: %#v", wireDirect)
	}
	wireArgs, ok := wireDirect.RecommendedToolCall["args"].(map[string]interface{})
	if !ok || wireArgs["wire_api"] != "RESPONSES" {
		t.Fatalf("wire_api filter should survive recommended call: %#v", wireDirect.RecommendedToolCall)
	}
	args, ok := direct.RecommendedToolCall["args"].(map[string]interface{})
	if !ok || direct.RecommendedToolCall["tool"] != "experience_learning" || direct.RecommendedToolCall["non_executing"] != true || args["action"] != "tool_recovery" || args["review_only"] != true {
		t.Fatalf("expected safe tool_recovery recommended call: %#v", direct.RecommendedToolCall)
	}
	emptyReviewOnly := app.QueryExperienceToolRecoverySummaries(ExperienceToolRecoveryQuery{Provider: "chatfire", ReviewOnly: true, Limit: 5})
	emptyArgs, ok := emptyReviewOnly.RecommendedToolCall["args"].(map[string]interface{})
	if !ok || emptyReviewOnly.Count != 0 || emptyArgs["provider"] != "chatfire" || emptyArgs["review_only"] != true {
		t.Fatalf("review_only filter should survive empty recommended calls: %#v", emptyReviewOnly)
	}

	governance := app.GetExperienceGovernanceSummary(ExperienceRoutingSignalQuery{})
	routing, ok := governance["routing_self_evolution"].(map[string]interface{})
	if !ok {
		t.Fatalf("routing_self_evolution missing: %#v", governance)
	}
	recoveryGov, ok := routing["tool_recovery_governance"].(map[string]interface{})
	providerCounts, _ := recoveryGov["provider_counts"].(map[string]int)
	modelCounts, _ := recoveryGov["model_counts"].(map[string]int)
	wireAPICounts, _ := recoveryGov["wire_api_counts"].(map[string]int)
	if !ok || recoveryGov["count"] != 2 || recoveryGov["review_required_count"] != 1 || providerCounts["ChatFire"] != 1 || modelCounts["gpt-5.1-codex-mini"] != 1 || wireAPICounts["responses"] != 1 {
		t.Fatalf("expected embedded recovery governance: %#v", routing["tool_recovery_governance"])
	}

	handler := &IMMessageHandler{app: app}
	payload := parseExperienceToolRecoveryToolResult(t, handler.toolExperienceLearning(map[string]interface{}{
		"action":      "tool_recovery",
		"review_only": true,
		"limit":       5,
	}))
	if !payload.OK || payload.Count != 1 || payload.ReviewRequiredCount != 1 || len(payload.Summaries) != 1 || payload.Summaries[0].ToolName != "write_file" {
		t.Fatalf("unexpected tool recovery payload: %#v", payload)
	}

	providerPayload := parseExperienceToolRecoveryToolResult(t, handler.toolExperienceLearning(map[string]interface{}{
		"action":   "tool_recovery",
		"provider": "chatfire",
		"model":    "GPT-5.1-CODEX-MINI",
		"wire_api": "RESPONSES",
		"limit":    5,
	}))
	if !providerPayload.OK || providerPayload.Count != 1 || len(providerPayload.Summaries) != 1 || providerPayload.Summaries[0].ToolName != "browser_open" {
		t.Fatalf("unexpected provider/model tool recovery payload: %#v", providerPayload)
	}
	if providerPayload.Query.Provider != "chatfire" || providerPayload.Query.Model != "GPT-5.1-CODEX-MINI" || providerPayload.Query.WireAPI != "RESPONSES" || providerPayload.ProviderCounts["ChatFire"] != 1 || providerPayload.ModelCounts["gpt-5.1-codex-mini"] != 1 || providerPayload.WireAPICounts["responses"] != 1 {
		t.Fatalf("provider/model/wire_api filters and counts should survive tool path: %#v", providerPayload)
	}

	aliasPayload := parseExperienceToolRecoveryToolResult(t, handler.toolExperienceLearning(map[string]interface{}{
		"action": "tool_recovery_governance",
		"model":  "gpt-5-1-codex-mini",
		"limit":  5,
	}))
	if !aliasPayload.OK || aliasPayload.Count != 1 || aliasPayload.Summaries[0].ToolName != "browser_open" || aliasPayload.Query.Model != "gpt-5-1-codex-mini" {
		t.Fatalf("tool recovery governance alias should preserve query and safe-tag model matching: %#v", aliasPayload)
	}

	shortAliasPayload := parseExperienceToolRecoveryToolResult(t, handler.toolExperienceLearning(map[string]interface{}{
		"action":   "recovery_governance",
		"provider": "chatfire",
		"limit":    5,
	}))
	if !shortAliasPayload.OK || shortAliasPayload.Count != 1 || shortAliasPayload.Summaries[0].ToolName != "browser_open" || shortAliasPayload.Query.Provider != "chatfire" {
		t.Fatalf("recovery_governance alias should preserve provider query: %#v", shortAliasPayload)
	}

	inspectAliasPayload := parseExperienceToolRecoveryToolResult(t, handler.toolExperienceLearning(map[string]interface{}{
		"action":   "inspect_tool_recovery_governance",
		"provider": "chatfire",
		"limit":    5,
	}))
	if !inspectAliasPayload.OK || inspectAliasPayload.Count != 1 || inspectAliasPayload.Summaries[0].ToolName != "browser_open" || inspectAliasPayload.Query.Provider != "chatfire" {
		t.Fatalf("inspect_tool_recovery_governance alias should preserve provider query: %#v", inspectAliasPayload)
	}
}

func TestExperienceGovernanceSummaryRecommendsToolRecoveryInspection(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)

	retry := NewAdaptiveRetry(nil)
	retry.SetMemoryStore(store)
	retry.RecordFailure("browser_open", FailureNetwork, RetryDecision{Action: RetryActionRetry, Attempt: 0, ProviderName: "ChatFire", Model: "gpt-5.1-codex-mini", WireAPI: "responses"})

	app := &App{memoryStore: store}
	summary := app.GetExperienceGovernanceSummary(ExperienceRoutingSignalQuery{})
	if summary["recommended_next_action"] != experienceGovernanceActionInspectToolRecoveryGovernance.String() {
		t.Fatalf("expected tool recovery inspection recommendation: %#v", summary)
	}
	call, ok := summary["recommended_tool_call"].(map[string]interface{})
	args, argsOK := call["args"].(map[string]interface{})
	if !ok || !argsOK || call["tool"] != "experience_learning" || call["non_executing"] != true || args["action"] != "tool_recovery" || args["limit"] != 20 {
		t.Fatalf("expected safe tool_recovery recommended call: %#v", summary["recommended_tool_call"])
	}
	if boundary, _ := call["non_executing_boundary"].(string); !strings.Contains(boundary, "retry execution") || !strings.Contains(boundary, "credential change") {
		t.Fatalf("tool recovery recommendation should use strict recovery boundary: %#v", call)
	}
	callFocus, ok := call["recommended_focus_context"].(map[string]interface{})
	providerCounts, providerOK := callFocus["provider_counts"].(map[string]int)
	modelCounts, modelOK := callFocus["model_counts"].(map[string]int)
	wireAPICounts, wireOK := callFocus["wire_api_counts"].(map[string]int)
	if !ok || callFocus["action_kind"] != experienceGovernanceActionInspectToolRecoveryGovernance.String() || callFocus["count"] != 1 || callFocus["recommended_provider"] != "ChatFire" || !providerOK || providerCounts["ChatFire"] != 1 || !modelOK || modelCounts["gpt-5.1-codex-mini"] != 1 || !wireOK || wireAPICounts["responses"] != 1 {
		t.Fatalf("tool recovery recommendation should expose provider/model/wire focus context: %#v", call)
	}
	focus, ok := summary["recommended_focus"].(map[string]interface{})
	if !ok || focus["trace_filter"] != "tools" || focus["non_executing"] != true {
		t.Fatalf("expected tools focus for recovery governance: %#v", summary["recommended_focus"])
	}
	toolPayload := parseExperienceGovernanceSummaryToolResult(t, (&IMMessageHandler{app: app}).toolExperienceLearning(map[string]interface{}{"action": "governance_summary"}))
	if !toolPayload.OK || toolPayload.GovernanceSummary["recommended_next_action"] != experienceGovernanceActionInspectToolRecoveryGovernance.String() {
		t.Fatalf("expected tool governance summary recovery recommendation: %#v", toolPayload)
	}
}

func TestExperienceLearningToolRoutingSignalsFiltersToolEvidence(t *testing.T) {
	tracker, err := coretool.NewUsageTracker("")
	if err != nil {
		t.Fatalf("NewUsageTracker: %v", err)
	}
	now := time.Now()
	for i := 0; i < 3; i++ {
		tracker.RecordExperience(coretool.ToolExperience{
			ToolName:     "browser_search",
			QueryTokens:  []string{"browser", "research"},
			Success:      true,
			Timestamp:    now.Add(time.Duration(i) * time.Minute),
			TaskType:     "research",
			ToolSequence: []string{"browser_search", "browser_open"},
			ErrorClass:   "timeout",
			RecoveryTool: "browser_open",
			FinalOutcome: "recovered",
		})
	}
	handler := &IMMessageHandler{app: &App{usageTracker: tracker}}

	payload := parseExperienceRoutingSignalsToolResult(t, handler.toolExperienceLearning(map[string]interface{}{
		"action":    "routing_signals",
		"task_type": "research",
		"tool":      "browser_open",
		"q":         "browser",
		"limit":     1,
	}))
	if !payload.OK || payload.Query.Limit != 1 || !strings.Contains(payload.NonExecutingBoundary, "read-only") {
		t.Fatalf("unexpected routing signal payload header: %#v", payload)
	}
	if !strings.Contains(payload.NonExecutingBoundary, "no tool execution") || !strings.Contains(payload.NonExecutingBoundary, "skill creation") || !strings.Contains(payload.NonExecutingBoundary, "policy update") {
		t.Fatalf("routing signal boundary should come from the shared result model: %q", payload.NonExecutingBoundary)
	}
	if payload.Counts["routing_hints"] != 1 || payload.Counts["recovery_patterns"] != 1 || payload.Counts["skill_nudge_candidates"] != 1 {
		t.Fatalf("routing signal counts should include hint, recovery, and skill nudge evidence: %#v", payload.Counts)
	}
	if payload.Returned["routing_hints"] != 1 || payload.Returned["recovery_patterns"] != 1 || payload.Returned["skill_nudge_candidates"] != 1 {
		t.Fatalf("routing signal returned counts should respect limit: %#v", payload.Returned)
	}
	if len(payload.RecoveryPatterns) != 1 || payload.RecoveryPatterns[0].RecoveryTool != "browser_open" {
		t.Fatalf("expected browser_open recovery signal: %#v", payload.RecoveryPatterns)
	}
	if len(payload.ScoreAdjustments) != 1 || payload.ScoreAdjustments[0].ToolName != "browser_open" || payload.ScoreAdjustments[0].Adjustment <= 0 {
		t.Fatalf("expected positive browser_open score adjustment: %#v", payload.ScoreAdjustments)
	}
	if len(payload.ToolCandidates) != 1 || payload.ToolCandidates[0].ToolName != "browser_open" || payload.ToolCandidates[0].Direction != "prefer" || !strings.Contains(payload.RoutingRecommendation, "read-only") {
		t.Fatalf("expected read-only browser_open routing candidate: %#v recommendation=%q", payload.ToolCandidates, payload.RoutingRecommendation)
	}
	if payload.RecommendedFocusContext["tool"] != "browser_open" ||
		payload.RecommendedFocusContext["direction"] != "prefer" ||
		payload.RecommendedFocusContext["task_type"] != "research" ||
		payload.RecommendedFocusContext["query"] != "browser" {
		t.Fatalf("routing signals should expose candidate focus context: %#v", payload.RecommendedFocusContext)
	}
	recommendedArgs, ok := payload.RecommendedToolCall["args"].(map[string]interface{})
	if !ok ||
		payload.RecommendedToolCall["tool"] != "experience_learning" ||
		recommendedArgs["action"] != "build_routing_adjustment_draft" ||
		recommendedArgs["task_type"] != "research" ||
		recommendedArgs["tool"] != "browser_open" ||
		recommendedArgs["query"] != "browser" {
		t.Fatalf("routing signals should expose safe draft recommended tool call: %#v", payload.RecommendedToolCall)
	}
	if !experienceRoutingSignalsHasReason(payload.ScoreAdjustments[0].Reasons, "recovery_tool_evidence") {
		t.Fatalf("expected recovery evidence reason: %#v", payload.ScoreAdjustments[0])
	}
	if len(payload.SkillNudgeCandidates) != 1 || strings.Join(payload.SkillNudgeCandidates[0].ToolSequence, ">") != "browser_search>browser_open" {
		t.Fatalf("expected browser search/open skill nudge: %#v", payload.SkillNudgeCandidates)
	}
}

func TestExperienceLearningToolBuildsRoutingAdjustmentDraft(t *testing.T) {
	tracker, err := coretool.NewUsageTracker("")
	if err != nil {
		t.Fatalf("NewUsageTracker: %v", err)
	}
	now := time.Now()
	for i := 0; i < 3; i++ {
		tracker.RecordExperience(coretool.ToolExperience{
			ToolName:     "browser_search",
			QueryTokens:  []string{"browser", "research"},
			Success:      true,
			Timestamp:    now.Add(time.Duration(i) * time.Minute),
			TaskType:     "research",
			ToolSequence: []string{"browser_search", "browser_open"},
			ErrorClass:   "timeout",
			RecoveryTool: "browser_open",
			FinalOutcome: "recovered",
		})
	}
	handler := &IMMessageHandler{app: &App{usageTracker: tracker}}

	payload := parseExperienceRoutingAdjustmentDraftToolResult(t, handler.toolExperienceLearning(map[string]interface{}{
		"action":    "build_routing_adjustment_draft",
		"task_type": "research",
		"tool":      "browser_open",
		"q":         "browser",
		"limit":     2,
	}))
	draft := payload.RoutingAdjustmentDraft
	if !payload.OK || draft.Query.TaskType != "research" || len(draft.ToolCandidates) != 1 {
		t.Fatalf("unexpected routing draft payload: %#v", payload)
	}
	if draft.ToolCandidates[0].ToolName != "browser_open" || draft.ToolCandidates[0].Direction != "prefer" {
		t.Fatalf("expected browser_open preference draft: %#v", draft.ToolCandidates)
	}
	if len(draft.Checks) == 0 || draft.NonExecutingBoundary == "" {
		t.Fatalf("expected checks and non-executing boundary: %#v", draft)
	}
	if draft.RecommendedFocusContext["reason"] == "" || !strings.Contains(draft.RecommendedFocusContext["reason"].(string), "routing adjustment draft") {
		t.Fatalf("routing draft should expose review-only recommended_focus_context: %#v", draft)
	}
	reviewArgs, ok := draft.RecommendedToolCall["args"].(map[string]interface{})
	if !ok ||
		draft.RecommendedToolCall["tool"] != "experience_learning" ||
		draft.RecommendedToolCall["non_executing"] != true ||
		reviewArgs["action"] != "record_draft_review" ||
		reviewArgs["draft_kind"] != experienceDraftKindRouting ||
		reviewArgs["query"] != "browser" {
		t.Fatalf("routing draft should expose safe draft review recommended tool call: %#v", draft.RecommendedToolCall)
	}
	if _, ok := reviewArgs["status"]; ok {
		t.Fatalf("routing draft review recommended tool call must not prefill status: %#v", reviewArgs)
	}
	if !strings.Contains(draft.DraftMarkdown, "Routing Adjustment Draft") || !strings.Contains(draft.DraftMarkdown, "Candidate Adjustments") || !strings.Contains(draft.DraftMarkdown, "does not run tools") {
		t.Fatalf("draft markdown missing expected sections: %s", draft.DraftMarkdown)
	}
}

func TestExperienceGovernanceSummaryUsesQueryScopedRoutingCandidates(t *testing.T) {
	tracker, err := coretool.NewUsageTracker("")
	if err != nil {
		t.Fatalf("NewUsageTracker: %v", err)
	}
	now := time.Now()
	for i := 0; i < 3; i++ {
		tracker.RecordExperience(coretool.ToolExperience{
			ToolName:     "browser_search",
			QueryTokens:  []string{"browser", "research"},
			Success:      true,
			Timestamp:    now.Add(time.Duration(i) * time.Minute),
			TaskType:     "research",
			ToolSequence: []string{"browser_search", "browser_open"},
			ErrorClass:   "timeout",
			RecoveryTool: "browser_open",
			FinalOutcome: "recovered",
		})
	}
	app := &App{usageTracker: tracker}
	directSignals := app.QueryExperienceRoutingSignals(ExperienceRoutingSignalQuery{TaskType: "research", Tool: "browser_open", Query: "browser", Limit: 2})
	if !strings.Contains(directSignals.NonExecutingBoundary, "read-only routing") || !strings.Contains(directSignals.NonExecutingBoundary, "memory rewrite") {
		t.Fatalf("direct routing signals should expose shared non-executing boundary: %#v", directSignals)
	}

	summary := app.GetExperienceGovernanceSummary(ExperienceRoutingSignalQuery{
		TaskType: "research",
		Tool:     "browser_open",
		Query:    "browser",
		Limit:    2,
	})
	if summary["recommended_next_action"] != "review_routing_candidates" {
		t.Fatalf("expected query-scoped routing recommendation: %#v", summary)
	}
	focus, ok := summary["recommended_focus"].(map[string]interface{})
	if !ok || focus["trace_filter"] != "tools" || focus["non_executing"] != true {
		t.Fatalf("expected read-only tools focus for routing candidate review: %#v", summary)
	}
	recommendedCall, ok := summary["recommended_tool_call"].(map[string]interface{})
	if !ok || recommendedCall["tool"] != "experience_learning" || recommendedCall["non_executing"] != true {
		t.Fatalf("expected read-only recommended tool call: %#v", summary["recommended_tool_call"])
	}
	recommendedArgs, ok := recommendedCall["args"].(map[string]interface{})
	if !ok || recommendedArgs["action"] != "build_routing_adjustment_draft" || recommendedArgs["task_type"] != "research" || recommendedArgs["tool"] != "browser_open" || recommendedArgs["query"] != "browser" {
		t.Fatalf("expected routing draft recommended args: %#v", recommendedCall["args"])
	}
	routing, ok := summary["routing_self_evolution"].(map[string]interface{})
	if !ok {
		t.Fatalf("routing_self_evolution missing: %#v", summary)
	}
	candidates, ok := routing["tool_candidates"].([]ExperienceRoutingToolCandidate)
	if !ok || len(candidates) != 1 || candidates[0].ToolName != "browser_open" || candidates[0].Direction != "prefer" {
		t.Fatalf("expected browser_open routing candidate: %#v", routing["tool_candidates"])
	}
	query, ok := routing["query"].(ExperienceRoutingSignalQuery)
	if !ok || query.TaskType != "research" || query.Tool != "browser_open" || query.Query != "browser" || query.Limit != 2 {
		t.Fatalf("expected normalized governance routing query: %#v", routing["query"])
	}
	routingFocus, ok := routing["recommended_focus_context"].(map[string]interface{})
	if !ok || routingFocus["tool"] != "browser_open" || routingFocus["query"] != "browser" {
		t.Fatalf("expected governance routing safe focus context: %#v", routing["recommended_focus_context"])
	}
	routingCall, ok := routing["recommended_tool_call"].(map[string]interface{})
	routingArgs, argsOK := routingCall["args"].(map[string]interface{})
	if !ok || !argsOK || routingCall["tool"] != "experience_learning" || routingCall["non_executing"] != true || routingArgs["action"] != "build_routing_adjustment_draft" {
		t.Fatalf("expected governance routing safe recommended tool call: %#v", routing["recommended_tool_call"])
	}
	routingCallFocus, ok := routingCall["governance_focus_context"].(map[string]interface{})
	if !ok || routingCallFocus["tool"] != "browser_open" || routingCallFocus["query"] != "browser" {
		t.Fatalf("expected governance routing recommended call focus alias: %#v", routing["recommended_tool_call"])
	}
	if routing["non_executing_boundary"] != directSignals.NonExecutingBoundary {
		t.Fatalf("expected governance routing boundary to reuse routing signal boundary: %#v", routing["non_executing_boundary"])
	}

	handler := &IMMessageHandler{app: app}
	toolPayload := parseExperienceGovernanceSummaryToolResult(t, handler.toolExperienceLearning(map[string]interface{}{
		"action":    "governance_summary",
		"task_type": "research",
		"tool":      "browser_open",
		"query":     "browser",
		"limit":     2,
	}))
	if !toolPayload.OK || toolPayload.GovernanceSummary["recommended_next_action"] != "review_routing_candidates" {
		t.Fatalf("expected tool governance summary to use scoped routing candidates: %#v", toolPayload)
	}
	toolRouting, ok := toolPayload.GovernanceSummary["routing_self_evolution"].(map[string]interface{})
	if !ok {
		t.Fatalf("tool governance summary missing routing_self_evolution: %#v", toolPayload.GovernanceSummary)
	}
	toolCandidates, ok := toolRouting["tool_candidates"].([]interface{})
	if !ok || len(toolCandidates) != 1 {
		t.Fatalf("tool governance summary missing routing candidates: %#v", toolRouting["tool_candidates"])
	}
	toolRoutingBoundary, _ := toolRouting["non_executing_boundary"].(string)
	if !strings.Contains(toolRoutingBoundary, "read-only routing") {
		t.Fatalf("tool governance summary should expose routing safety boundary: %#v", toolRouting["non_executing_boundary"])
	}
	toolRoutingFocus, ok := toolRouting["recommended_focus_context"].(map[string]interface{})
	if !ok || toolRoutingFocus["tool"] != "browser_open" || toolRoutingFocus["query"] != "browser" || toolRoutingFocus["direction"] != "prefer" {
		t.Fatalf("tool governance summary should expose routing handoff focus context: %#v", toolRouting["recommended_focus_context"])
	}
	toolRoutingCall, ok := toolRouting["recommended_tool_call"].(map[string]interface{})
	toolRoutingArgs, argsOK := toolRoutingCall["args"].(map[string]interface{})
	if !ok || !argsOK || toolRoutingCall["tool"] != "experience_learning" || toolRoutingArgs["action"] != "build_routing_adjustment_draft" || toolRoutingArgs["tool"] != "browser_open" {
		t.Fatalf("tool governance summary should expose routing handoff tool call: %#v", toolRouting["recommended_tool_call"])
	}
}

func TestExperienceGovernanceSummaryKeepsGlobalReviewPriorityForUnscopedRouting(t *testing.T) {
	snapshot := ExperienceLearningSnapshot{ReviewRequiredTraceCount: 1}
	routingSignals := &ExperienceRoutingSignalResult{
		Query: ExperienceRoutingSignalQuery{},
		ToolCandidates: []ExperienceRoutingToolCandidate{{
			ToolName:  "browser_open",
			Direction: "prefer",
		}},
	}

	summary := experienceGovernanceSummary(snapshot, routingSignals)
	if summary["recommended_next_action"] != "review_required_traces" {
		t.Fatalf("expected unscoped governance summary to keep global review priority: %#v", summary)
	}
	focus, ok := summary["recommended_focus"].(map[string]interface{})
	if !ok || focus["trace_filter"] != "review" || focus["non_executing"] != true {
		t.Fatalf("expected read-only review focus for unscoped governance summary: %#v", summary)
	}
	recommendedCall, ok := summary["recommended_tool_call"].(map[string]interface{})
	recommendedArgs, argsOK := recommendedCall["args"].(map[string]interface{})
	if !ok || !argsOK || recommendedCall["tool"] != "experience_learning" || recommendedArgs["action"] != "queues" {
		t.Fatalf("expected review queue recommended tool call: %#v", summary["recommended_tool_call"])
	}
	routing, ok := summary["routing_self_evolution"].(map[string]interface{})
	if !ok {
		t.Fatalf("routing_self_evolution missing: %#v", summary)
	}
	candidates, ok := routing["tool_candidates"].([]ExperienceRoutingToolCandidate)
	if !ok || len(candidates) != 1 || candidates[0].ToolName != "browser_open" {
		t.Fatalf("expected routing candidates to remain visible without changing global priority: %#v", routing["tool_candidates"])
	}
}

type experienceToolRecoveryToolPayload struct {
	OK                   bool                            `json:"ok"`
	Query                ExperienceToolRecoveryQuery     `json:"query"`
	Count                int                             `json:"count"`
	Returned             int                             `json:"returned"`
	ReviewRequiredCount  int                             `json:"review_required_count"`
	DisabledCount        int                             `json:"disabled_count"`
	ToolCounts           map[string]int                  `json:"tool_counts"`
	ProviderCounts       map[string]int                  `json:"provider_counts"`
	ModelCounts          map[string]int                  `json:"model_counts"`
	WireAPICounts        map[string]int                  `json:"wire_api_counts"`
	CategoryCounts       map[string]int                  `json:"category_counts"`
	Summaries            []ExperienceToolRecoverySummary `json:"summaries"`
	NonExecutingBoundary string                          `json:"non_executing_boundary"`
}

type experienceRoutingSignalsToolPayload struct {
	OK                      bool                                        `json:"ok"`
	Query                   ExperienceRoutingSignalQuery                `json:"query"`
	Counts                  map[string]int                              `json:"counts"`
	Returned                map[string]int                              `json:"returned"`
	RoutingHints            []coretool.ToolRoutingHint                  `json:"routing_hints"`
	RecoveryPatterns        []coretool.ToolRecoveryPattern              `json:"recovery_patterns"`
	SkillNudgeCandidates    []coretool.ToolSkillNudgeCandidate          `json:"skill_nudge_candidates"`
	UsagePatterns           []coretool.UsagePattern                     `json:"usage_patterns"`
	ScoreAdjustments        []coretool.RoutingHintAdjustmentExplanation `json:"score_adjustments"`
	ToolCandidates          []ExperienceRoutingToolCandidate            `json:"tool_candidates"`
	RoutingRecommendation   string                                      `json:"routing_recommendation"`
	RecommendedFocusContext map[string]interface{}                      `json:"recommended_focus_context"`
	RecommendedToolCall     map[string]interface{}                      `json:"recommended_tool_call"`
	NonExecutingBoundary    string                                      `json:"non_executing_boundary"`
}

type experienceGovernanceSummaryToolPayload struct {
	OK                bool                   `json:"ok"`
	GovernanceSummary map[string]interface{} `json:"governance_summary"`
}

type experienceRoutingAdjustmentDraftToolPayload struct {
	OK                     bool                             `json:"ok"`
	RoutingAdjustmentDraft ExperienceRoutingAdjustmentDraft `json:"routing_adjustment_draft"`
}

func parseExperienceToolRecoveryToolResult(t *testing.T, raw string) experienceToolRecoveryToolPayload {
	t.Helper()
	var payload experienceToolRecoveryToolPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal tool recovery result: %v\n%s", err, raw)
	}
	return payload
}

func parseExperienceRoutingSignalsToolResult(t *testing.T, raw string) experienceRoutingSignalsToolPayload {
	t.Helper()
	var payload experienceRoutingSignalsToolPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal routing signals result: %v\n%s", err, raw)
	}
	return payload
}

func parseExperienceGovernanceSummaryToolResult(t *testing.T, raw string) experienceGovernanceSummaryToolPayload {
	t.Helper()
	var payload experienceGovernanceSummaryToolPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal governance summary result: %v\n%s", err, raw)
	}
	return payload
}

func parseExperienceRoutingAdjustmentDraftToolResult(t *testing.T, raw string) experienceRoutingAdjustmentDraftToolPayload {
	t.Helper()
	var payload experienceRoutingAdjustmentDraftToolPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal routing adjustment draft result: %v\n%s", err, raw)
	}
	return payload
}

func experienceRoutingSignalsHasReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}
