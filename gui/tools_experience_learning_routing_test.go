package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

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
