package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestExperienceGovernanceRecommendedFocus(t *testing.T) {
	tests := []struct {
		name               string
		action             string
		filter             string
		actionKind         string
		followUpActionKind string
	}{
		{name: "triggered rollback review", action: "review_triggered_rollback_signal", filter: "actions", actionKind: "review_triggered_rollback_signal"},
		{name: "triggered rollback followup", action: "inspect_triggered_rollback_followups", filter: "followups", followUpActionKind: "draft_rollback_workflow"},
		{name: "review queue", action: "review_required_traces", filter: "review"},
		{name: "routing evidence", action: "review_routing_candidates", filter: "tools"},
		{name: "follow ups", action: "inspect_follow_up_actions", filter: "followups"},
		{name: "action kind", action: "draft_rollback_workflow", filter: "actions", actionKind: "draft_rollback_workflow"},
		{name: "empty", action: "", filter: "all"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			focus := experienceGovernanceRecommendedFocus(tt.action)
			if focus["trace_filter"] != tt.filter || focus["non_executing"] != true {
				t.Fatalf("unexpected focus for %s: %#v", tt.action, focus)
			}
			if tt.actionKind != "" {
				if focus["action_kind"] != tt.actionKind {
					t.Fatalf("expected action_kind %q: %#v", tt.actionKind, focus)
				}
			} else if _, ok := focus["action_kind"]; ok {
				t.Fatalf("unexpected action_kind for %s: %#v", tt.action, focus)
			}
			if tt.followUpActionKind != "" {
				if focus["follow_up_action_kind"] != tt.followUpActionKind {
					t.Fatalf("expected follow_up_action_kind %q: %#v", tt.followUpActionKind, focus)
				}
			} else if _, ok := focus["follow_up_action_kind"]; ok {
				t.Fatalf("unexpected follow_up_action_kind for %s: %#v", tt.action, focus)
			}
		})
	}
	call := experienceGovernanceRecommendedToolCall("build_memory_maintenance_draft", ExperienceLearningSnapshot{}, nil)
	args, ok := call["args"].(map[string]interface{})
	if !ok || call["tool"] != "experience_learning" || call["non_executing"] != true || args["action"] != "build_memory_maintenance_draft" {
		t.Fatalf("unexpected memory maintenance recommended tool call: %#v", call)
	}
	snapshot := ExperienceLearningSnapshot{NextActionSummaries: []ExperienceNextActionSummary{{Kind: "draft_rollback_workflow", LatestTraceID: "trace-1"}}}
	call = experienceGovernanceRecommendedToolCall("draft_rollback_workflow", snapshot, nil)
	args, ok = call["args"].(map[string]interface{})
	if !ok || args["action"] != "build_rollback_draft" || args["trace_id"] != "trace-1" {
		t.Fatalf("unexpected rollback draft recommended tool call: %#v", call)
	}
	snapshot = ExperienceLearningSnapshot{NextActionSummaries: []ExperienceNextActionSummary{{Kind: "review_triggered_rollback_signal", LatestTraceID: "trace-triggered", LatestTitle: "Triggered rollback review"}}}
	call = experienceGovernanceRecommendedToolCall("review_triggered_rollback_signal", snapshot, nil, "rollback condition matched")
	args, ok = call["args"].(map[string]interface{})
	if !ok || args["action"] != "trace_details" || args["filter"] != "actions" || args["action_kind"] != "review_triggered_rollback_signal" || args["kind"] != "a2a_rollback_review" {
		t.Fatalf("unexpected triggered rollback review recommended tool call: %#v", call)
	}
	context, ok := call["governance_focus_context"].(map[string]interface{})
	if !ok || context["priority_trace_id"] != "trace-triggered" || context["priority_trace_title"] != "Triggered rollback review" || context["reason"] != "rollback condition matched" {
		t.Fatalf("expected triggered rollback governance focus context: %#v", call)
	}
	recommendedContext, ok := call["recommended_focus_context"].(map[string]interface{})
	if !ok || recommendedContext["priority_trace_id"] != "trace-triggered" || recommendedContext["reason"] != "rollback condition matched" {
		t.Fatalf("expected triggered rollback recommended focus context: %#v", call)
	}
	snapshot = ExperienceLearningSnapshot{NextActionSummaries: []ExperienceNextActionSummary{{Kind: "collect_rollback_evidence", LatestTraceID: "trace-2"}}}
	call = experienceGovernanceRecommendedToolCall("collect_rollback_evidence", snapshot, nil)
	args, ok = call["args"].(map[string]interface{})
	if !ok || args["action"] != "build_followup" || args["trace_id"] != "trace-2" {
		t.Fatalf("unexpected evidence-collection recommended tool call: %#v", call)
	}
	snapshot = ExperienceLearningSnapshot{FollowUpActionSummaries: []ExperienceFollowUpActionSummary{{Kind: "draft_rollback_workflow", TriggeredRollback: true, TriggeredCount: 1, LatestTraceID: "trace-follow", LatestTitle: "Rollback follow-up", RecommendedTraceID: "trace-recommended", RecommendedTitle: "Recommended rollback follow-up", RecommendedReason: "matched trigger stayed active"}}}
	call = experienceGovernanceRecommendedToolCall("inspect_triggered_rollback_followups", snapshot, nil)
	args, ok = call["args"].(map[string]interface{})
	if !ok || args["action"] != "follow_up_actions" || args["follow_up_action_kind"] != "draft_rollback_workflow" || args["kind"] != "a2a_rollback_review" || args["triggered_rollback_only"] != true {
		t.Fatalf("unexpected triggered rollback follow-up recommended tool call: %#v", call)
	}
	context, ok = call["governance_focus_context"].(map[string]interface{})
	if !ok || context["priority_trace_id"] != "trace-recommended" || context["priority_trace_title"] != "Recommended rollback follow-up" || context["reason"] != "matched trigger stayed active" {
		t.Fatalf("expected triggered rollback follow-up governance focus context: %#v", call)
	}
	recommendedContext, ok = call["recommended_focus_context"].(map[string]interface{})
	if !ok || recommendedContext["priority_trace_id"] != "trace-recommended" || recommendedContext["reason"] != "matched trigger stayed active" {
		t.Fatalf("expected triggered rollback follow-up recommended focus context: %#v", call)
	}
	action, reason := experienceGovernanceRecommendedNextAction(ExperienceLearningSnapshot{
		NextActionSummaries:      []ExperienceNextActionSummary{{Kind: "review_triggered_rollback_signal", LatestTraceID: "trace-triggered"}},
		ReviewRequiredTraceCount: 1,
	}, nil)
	if action != "review_triggered_rollback_signal" || !strings.Contains(reason, "rollback conditions") {
		t.Fatalf("unexpected triggered rollback recommended next action: %q %q", action, reason)
	}
	action, reason = experienceGovernanceRecommendedNextAction(ExperienceLearningSnapshot{
		FollowUpTraceCount:      1,
		FollowUpActionSummaries: []ExperienceFollowUpActionSummary{{Kind: "draft_rollback_workflow", TriggeredRollback: true, TriggeredCount: 1, LatestTraceID: "trace-follow"}},
	}, nil)
	if action != "inspect_triggered_rollback_followups" || !strings.Contains(reason, "triggered rollback") {
		t.Fatalf("unexpected triggered rollback follow-up next action: %q %q", action, reason)
	}
	call = experienceGovernanceRecommendedToolCall("draft_skill_manually", ExperienceLearningSnapshot{}, nil)
	args, ok = call["args"].(map[string]interface{})
	if !ok || args["action"] != "trace_details" || args["action_kind"] != "draft_skill_manually" {
		t.Fatalf("unexpected fallback action-kind recommended tool call: %#v", call)
	}
}

func TestExperienceLearningToolResultNormalizesSafeHandoff(t *testing.T) {
	t.Parallel()
	raw := experienceLearningToolResult(map[string]interface{}{
		"recommended_focus_context": map[string]interface{}{"priority_trace_id": "trace-1", "reason": "inspect first"},
		"recommended_tool_call": map[string]interface{}{
			"tool": "experience_learning",
			"args": map[string]interface{}{"action": "trace_details", "trace_id": "trace-1"},
		},
		"non_executing_boundary": "read-only trace inspection; no review approval, memory rewrite, routing change, rollback execution, file write, notification, tool execution, or skill install was performed",
	}, nil)
	var payload struct {
		OK                      bool                   `json:"ok"`
		RecommendedFocusContext map[string]interface{} `json:"recommended_focus_context"`
		RecommendedToolCall     map[string]interface{} `json:"recommended_tool_call"`
		NonExecutingBoundary    string                 `json:"non_executing_boundary"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal normalized experience result: %v\n%s", err, raw)
	}
	if !payload.OK || payload.RecommendedFocusContext["priority_trace_id"] != "trace-1" {
		t.Fatalf("payload focus context not preserved: %#v", payload)
	}
	callFocus, ok := payload.RecommendedToolCall["recommended_focus_context"].(map[string]interface{})
	if !ok || callFocus["priority_trace_id"] != "trace-1" {
		t.Fatalf("recommended tool call should inherit focus context: %#v", payload.RecommendedToolCall)
	}
	governanceFocus, ok := payload.RecommendedToolCall["governance_focus_context"].(map[string]interface{})
	if !ok || governanceFocus["reason"] != "inspect first" {
		t.Fatalf("recommended tool call should expose governance focus alias: %#v", payload.RecommendedToolCall)
	}
	if payload.RecommendedToolCall["non_executing"] != true || !strings.Contains(fmt.Sprint(payload.RecommendedToolCall["non_executing_boundary"]), "read-only trace inspection") {
		t.Fatalf("recommended tool call should be normalized as non-executing: %#v", payload.RecommendedToolCall)
	}
}

func TestExperienceGovernanceSummaryEmbedsMemorySafeHandoff(t *testing.T) {
	summary := experienceGovernanceSummary(ExperienceLearningSnapshot{
		ProtectedMemoryCount:            3,
		LayeredMemoryRecommended:        true,
		LayeredMemoryReason:             "active memory volume crossed the layered threshold",
		MemoryMaintenanceRecommendation: "layered retention-aware maintenance is recommended",
		MemoryMaintenanceBoundary:       "read-only memory maintenance snapshot; no compression, promotion, deletion, or rewrite was performed",
	}, nil)
	memory, ok := summary["memory"].(map[string]interface{})
	if !ok {
		t.Fatalf("governance summary missing memory block: %#v", summary)
	}
	if memory["non_executing_boundary"] != "read-only memory maintenance snapshot; no compression, promotion, deletion, or rewrite was performed" {
		t.Fatalf("memory handoff should preserve memory boundary: %#v", memory)
	}
	focus, ok := memory["recommended_focus_context"].(map[string]interface{})
	if !ok || !strings.Contains(fmt.Sprint(focus["reason"]), "retention-aware maintenance") {
		t.Fatalf("memory handoff should expose focus context: %#v", memory)
	}
	call, ok := memory["recommended_tool_call"].(map[string]interface{})
	args, argsOK := call["args"].(map[string]interface{})
	if !ok || !argsOK || call["tool"] != "experience_learning" || call["non_executing"] != true || args["action"] != "build_memory_maintenance_draft" {
		t.Fatalf("memory handoff should point to safe maintenance draft: %#v", memory["recommended_tool_call"])
	}
}

func TestExperienceGovernanceSummaryEmbedsA2ASafeHandoff(t *testing.T) {
	summary := experienceGovernanceSummary(ExperienceLearningSnapshot{
		TraceKindCounts: map[string]int{
			"a2a_discussion_result": 1,
			"a2a_conflict_review":   1,
			"a2a_rollback_review":   1,
		},
		ReviewRequiredTraceCount: 2,
	}, nil)
	a2a, ok := summary["a2a_discussion"].(map[string]interface{})
	if !ok {
		t.Fatalf("governance summary missing A2A block: %#v", summary)
	}
	if !strings.Contains(fmt.Sprint(a2a["non_executing_boundary"]), "read-only A2A governance inspection") {
		t.Fatalf("A2A handoff should expose no-execute boundary: %#v", a2a)
	}
	focus, ok := a2a["recommended_focus_context"].(map[string]interface{})
	if !ok || focus["trace_filter"] != "review" || focus["review_required_trace_count"] != 2 {
		t.Fatalf("A2A handoff should prioritize review traces: %#v", a2a)
	}
	call, ok := a2a["recommended_tool_call"].(map[string]interface{})
	args, argsOK := call["args"].(map[string]interface{})
	if !ok || !argsOK || call["tool"] != "experience_learning" || call["non_executing"] != true || args["action"] != "trace_details" || args["filter"] != "review" {
		t.Fatalf("A2A handoff should point to safe trace inspection: %#v", a2a["recommended_tool_call"])
	}

	summary = experienceGovernanceSummary(ExperienceLearningSnapshot{
		TraceKindCounts: map[string]int{"a2a_discussion_result": 1},
	}, nil)
	a2a, ok = summary["a2a_discussion"].(map[string]interface{})
	if !ok {
		t.Fatalf("governance summary missing A2A block for discussion evidence: %#v", summary)
	}
	focus, ok = a2a["recommended_focus_context"].(map[string]interface{})
	call, ok = a2a["recommended_tool_call"].(map[string]interface{})
	args, argsOK = call["args"].(map[string]interface{})
	if !ok || !argsOK || focus["trace_filter"] != "a2a" || args["filter"] != "a2a" {
		t.Fatalf("A2A discussion-only handoff should inspect A2A traces: focus=%#v call=%#v", focus, call)
	}
}

func TestExperienceLearningDirectSummarySafeHandoffsAreNormalized(t *testing.T) {
	t.Parallel()

	app := &App{}
	summary := app.GetExperienceGovernanceSummary(ExperienceRoutingSignalQuery{})
	call, ok := summary["recommended_tool_call"].(map[string]interface{})
	if !ok || call["non_executing"] != true {
		t.Fatalf("governance summary recommended tool call should be non-executing: %#v", summary["recommended_tool_call"])
	}
	if _, ok := call["governance_focus_context"].(map[string]interface{}); !ok {
		t.Fatalf("governance summary recommended tool call should expose governance focus alias: %#v", call)
	}
	if !strings.Contains(fmt.Sprint(call["non_executing_boundary"]), "recommended inspection or draft-building call only") {
		t.Fatalf("governance summary recommended tool call should preserve boundary: %#v", call)
	}

	routingBlock, ok := summary["routing_self_evolution"].(map[string]interface{})
	if !ok {
		t.Fatalf("governance summary routing block missing: %#v", summary)
	}
	if routingCall, ok := routingBlock["recommended_tool_call"].(map[string]interface{}); ok && routingCall != nil {
		if routingCall["non_executing"] != true {
			t.Fatalf("routing block recommended tool call should be non-executing: %#v", routingCall)
		}
		if _, ok := routingCall["governance_focus_context"].(map[string]interface{}); !ok {
			t.Fatalf("routing block recommended tool call should expose governance focus alias: %#v", routingCall)
		}
	}

	memoryStore, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(memoryStore.Stop)
	if err := memoryStore.Save(corememory.Entry{
		Title:      "A2A rollback",
		Content:    "A2A decision with rollback trigger",
		Category:   corememory.CategoryProjectKnowledge,
		SourceType: "group_discussion",
		Tags:       []string{"discussion:disc-1"},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	app = &App{memoryStore: memoryStore}
	candidates := app.QueryExperienceProtectedMemoryCandidates(ExperienceMemoryCandidateQuery{})
	if candidates.RecommendedToolCall["non_executing"] != true {
		t.Fatalf("memory candidates recommended tool call should be non-executing: %#v", candidates.RecommendedToolCall)
	}
	if _, ok := candidates.RecommendedToolCall["governance_focus_context"].(map[string]interface{}); !ok {
		t.Fatalf("memory candidates recommended tool call should expose governance focus alias: %#v", candidates.RecommendedToolCall)
	}
	draft := app.BuildExperienceMemoryMaintenanceDraft(ExperienceMemoryCandidateQuery{})
	if draft.RecommendedToolCall["non_executing"] != true {
		t.Fatalf("memory maintenance draft recommended tool call should be non-executing: %#v", draft.RecommendedToolCall)
	}
	if _, ok := draft.RecommendedToolCall["governance_focus_context"].(map[string]interface{}); !ok {
		t.Fatalf("memory maintenance draft recommended tool call should expose governance focus alias: %#v", draft.RecommendedToolCall)
	}
}

func TestExperienceLearningToolMemoryCandidatesFiltersProtectedMemory(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(corememory.Entry{
		Title:      "A2A rollback",
		Content:    "A2A decision with rollback trigger and minority objection",
		Category:   corememory.CategoryProjectKnowledge,
		Tags:       []string{"discussion:disc-1", "rollback"},
		SourceType: "group_discussion",
	}); err != nil {
		t.Fatalf("Save a2a: %v", err)
	}
	if err := store.Save(corememory.Entry{
		Title:      "Tool recovery",
		Content:    "Tool usage recovered browser_search with browser_open",
		Category:   corememory.CategoryProjectKnowledge,
		SourceType: "tool_usage",
	}); err != nil {
		t.Fatalf("Save tool: %v", err)
	}
	if err := store.Save(corememory.Entry{
		Title:    "Pinned policy",
		Content:  "Pinned local policy",
		Category: corememory.CategoryProjectKnowledge,
		Pinned:   true,
	}); err != nil {
		t.Fatalf("Save pinned: %v", err)
	}
	handler := &IMMessageHandler{app: &App{memoryStore: store}}

	payload := parseExperienceMemoryCandidatesToolResult(t, handler.toolExperienceLearning(map[string]interface{}{"action": "memory_candidates", "reason": "a2a_discussion", "q": "rollback", "limit": 1}))
	if !payload.OK || payload.Count != 1 || payload.Returned != 1 || payload.Query.Limit != 1 {
		t.Fatalf("unexpected memory candidate payload: %#v", payload)
	}
	if payload.ScannedEntries != 3 || payload.ActiveEntries != 3 || payload.MaintenanceRecommendation == "" || payload.NonExecutingBoundary == "" {
		t.Fatalf("memory maintenance inspection fields missing: %#v", payload)
	}
	if payload.ReasonCounts["a2a_discussion"] != 1 || payload.ReasonCounts["tool_usage"] != 1 || payload.ReasonCounts["pinned"] != 1 {
		t.Fatalf("reason counts should describe all protected candidates: %#v", payload.ReasonCounts)
	}
	if payload.SourceCounts["a2a_discussion"] != 1 || payload.SourceCounts["tool_usage"] != 1 {
		t.Fatalf("source counts should describe all protected candidates: %#v", payload.SourceCounts)
	}
	if len(payload.MemoryCandidates) != 1 || payload.MemoryCandidates[0].Reason != "a2a_discussion" || !strings.Contains(payload.MemoryCandidates[0].Summary, "rollback") {
		t.Fatalf("expected filtered A2A rollback candidate: %#v", payload.MemoryCandidates)
	}
	if payload.RecommendedFocusContext["priority_trace_id"] != "memory:"+payload.MemoryCandidates[0].ID ||
		payload.RecommendedFocusContext["reason_filter"] != "a2a_discussion" ||
		payload.RecommendedFocusContext["query"] != "rollback" {
		t.Fatalf("memory candidates should expose leading anchor focus context: %#v", payload.RecommendedFocusContext)
	}
	recommendedArgs, ok := payload.RecommendedToolCall["args"].(map[string]interface{})
	if !ok ||
		payload.RecommendedToolCall["tool"] != "experience_learning" ||
		recommendedArgs["action"] != "build_memory_maintenance_draft" ||
		recommendedArgs["reason"] != "a2a_discussion" ||
		recommendedArgs["query"] != "rollback" {
		t.Fatalf("memory candidates should expose safe draft recommended tool call: %#v", payload.RecommendedToolCall)
	}
	governanceFocus, ok := payload.RecommendedToolCall["governance_focus_context"].(map[string]interface{})
	if !ok || governanceFocus["priority_trace_id"] != payload.RecommendedFocusContext["priority_trace_id"] {
		t.Fatalf("memory candidates recommended call should expose governance focus alias: %#v", payload.RecommendedToolCall)
	}

	all := parseExperienceMemoryCandidatesToolResult(t, handler.toolExperienceLearning(map[string]interface{}{"action": "memory_candidates", "limit": 1}))
	if len(all.MemoryCandidates) != 1 || all.MemoryCandidates[0].Reason != "pinned" {
		t.Fatalf("unfiltered memory candidates should prioritize pinned entries: %#v", all.MemoryCandidates)
	}

	governance := parseExperienceGovernanceSummaryToolResult(t, handler.toolExperienceLearning(map[string]interface{}{"action": "governance_summary"}))
	memory, ok := governance.GovernanceSummary["memory"].(map[string]interface{})
	if !ok {
		t.Fatalf("governance_summary tool should expose memory handoff block: %#v", governance.GovernanceSummary)
	}
	memoryCall, ok := memory["recommended_tool_call"].(map[string]interface{})
	memoryArgs, argsOK := memoryCall["args"].(map[string]interface{})
	if !ok || !argsOK || memoryCall["tool"] != "experience_learning" || memoryCall["non_executing"] != true || memoryArgs["action"] != "memory_candidates" {
		t.Fatalf("governance_summary memory handoff should point to safe memory inspection: %#v", memory["recommended_tool_call"])
	}
	if memory["recommended_focus_context"] == nil || memory["non_executing_boundary"] == "" {
		t.Fatalf("governance_summary memory handoff should include focus and boundary: %#v", memory)
	}
	a2a, ok := governance.GovernanceSummary["a2a_discussion"].(map[string]interface{})
	if !ok {
		t.Fatalf("governance_summary tool should expose A2A handoff block: %#v", governance.GovernanceSummary)
	}
	a2aCall, ok := a2a["recommended_tool_call"].(map[string]interface{})
	a2aArgs, argsOK := a2aCall["args"].(map[string]interface{})
	if !ok || !argsOK || a2aCall["tool"] != "experience_learning" || a2aCall["non_executing"] != true || a2aArgs["action"] != "trace_details" || a2aArgs["filter"] != "a2a" {
		t.Fatalf("governance_summary A2A handoff should point to safe A2A trace inspection: %#v", a2a["recommended_tool_call"])
	}
	if a2a["recommended_focus_context"] == nil || a2a["non_executing_boundary"] == "" {
		t.Fatalf("governance_summary A2A handoff should include focus and boundary: %#v", a2a)
	}
}

func TestExperienceLearningToolMemoryCandidatesReportsLayeredMaintenance(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	for i := 0; i < 40; i++ {
		if err := store.Save(corememory.Entry{
			Title:    "Trace volume",
			Content:  "ordinary active memory volume marker " + string(rune('a'+i%26)) + string(rune('0'+i/26)),
			Category: corememory.CategoryProjectKnowledge,
		}); err != nil {
			t.Fatalf("Save memory %d: %v", i, err)
		}
	}
	handler := &IMMessageHandler{app: &App{memoryStore: store}}

	payload := parseExperienceMemoryCandidatesToolResult(t, handler.toolExperienceLearning(map[string]interface{}{"action": "memory_candidates"}))
	if !payload.LayeredRecommended || !strings.Contains(payload.LayeredReason, "active memory volume") {
		t.Fatalf("expected layered maintenance recommendation, got %#v", payload)
	}
	if !strings.Contains(payload.MaintenanceRecommendation, "layered") || payload.ActiveEntries < 40 {
		t.Fatalf("expected layered maintenance fields, got %#v", payload)
	}
}

func TestExperienceLearningToolBuildsMemoryMaintenanceDraft(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(corememory.Entry{
		Title:      "A2A rollback guard",
		Content:    "A2A decision keeps rollback constraints and minority objection visible",
		Category:   corememory.CategoryProjectKnowledge,
		Tags:       []string{"discussion:disc-2", "rollback"},
		SourceType: "group_discussion",
	}); err != nil {
		t.Fatalf("Save a2a: %v", err)
	}
	if err := store.Save(corememory.Entry{
		Title:    "Pinned identity",
		Content:  "Pinned local behavior policy",
		Category: corememory.CategoryProjectKnowledge,
		Pinned:   true,
	}); err != nil {
		t.Fatalf("Save pinned: %v", err)
	}
	handler := &IMMessageHandler{app: &App{memoryStore: store}}

	payload := parseExperienceMemoryMaintenanceDraftToolResult(t, handler.toolExperienceLearning(map[string]interface{}{"action": "build_memory_maintenance_draft", "reason": "a2a_discussion", "limit": 2}))
	draft := payload.MemoryMaintenanceDraft
	if !payload.OK || draft.Query.Reason != "a2a_discussion" || draft.ProtectedReturned != 1 {
		t.Fatalf("unexpected maintenance draft payload: %#v", payload)
	}
	if len(draft.RetentionAnchors) != 1 || draft.RetentionAnchors[0].Reason != "a2a_discussion" {
		t.Fatalf("expected A2A retention anchor: %#v", draft.RetentionAnchors)
	}
	if draft.RecommendedFocusContext["priority_trace_id"] != "memory:"+draft.RetentionAnchors[0].ID || draft.RecommendedFocusContext["reason"] == "" {
		t.Fatalf("maintenance draft should expose anchor recommended_focus_context: %#v", draft)
	}
	if payload.RecommendedFocusContext["priority_trace_id"] != draft.RecommendedFocusContext["priority_trace_id"] || payload.NonExecutingBoundary != draft.NonExecutingBoundary {
		t.Fatalf("maintenance draft tool payload should mirror safe top-level handoff fields: %#v", payload)
	}
	reviewArgs, ok := draft.RecommendedToolCall["args"].(map[string]interface{})
	if !ok ||
		draft.RecommendedToolCall["tool"] != "experience_learning" ||
		draft.RecommendedToolCall["non_executing"] != true ||
		reviewArgs["action"] != "record_draft_review" ||
		reviewArgs["draft_kind"] != experienceDraftKindMaintenance ||
		reviewArgs["source_trace_id"] != "memory:"+draft.RetentionAnchors[0].ID {
		t.Fatalf("maintenance draft should expose safe draft review recommended tool call: %#v", draft.RecommendedToolCall)
	}
	topReviewArgs, ok := payload.RecommendedToolCall["args"].(map[string]interface{})
	if !ok || topReviewArgs["action"] != "record_draft_review" || topReviewArgs["source_trace_id"] != reviewArgs["source_trace_id"] {
		t.Fatalf("maintenance draft tool payload should expose top-level review handoff: %#v", payload.RecommendedToolCall)
	}
	if _, ok := reviewArgs["status"]; ok {
		t.Fatalf("maintenance draft review recommended tool call must not prefill status: %#v", reviewArgs)
	}
	if len(draft.Checks) == 0 || draft.NonExecutingBoundary == "" {
		t.Fatalf("expected checks and non-executing boundary: %#v", draft)
	}
	if !strings.Contains(draft.DraftMarkdown, "Memory Maintenance Draft") || !strings.Contains(draft.DraftMarkdown, "Retention Anchors") || !strings.Contains(draft.DraftMarkdown, "does not compress") {
		t.Fatalf("draft markdown missing expected sections: %s", draft.DraftMarkdown)
	}
}

func TestExperienceLearningToolRecordsDraftReviewEvidence(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	handler := &IMMessageHandler{app: &App{memoryStore: store}}

	payload := parseExperienceDraftReviewToolResult(t, handler.toolExperienceLearning(map[string]interface{}{
		"action":                 "record_draft_review",
		"draft_kind":             "routing_adjustment_draft",
		"status":                 "blocked",
		"source_trace_id":        "memory:routing-source",
		"query":                  "browser",
		"note":                   "needs owner routing policy",
		"actor":                  "owner",
		"draft_markdown":         "# Routing Adjustment Draft\n\nPrefer browser_open only with evidence.",
		"non_executing_boundary": "read-only routing adjustment draft only",
	}))
	if !payload.OK || payload.DraftReview.Kind != experienceDraftKindRouting || payload.DraftReview.Status != experienceFollowUpOutcomeBlocked || payload.DraftReview.TraceID == "" || payload.DraftReview.SourceTraceID != "memory:routing-source" {
		t.Fatalf("unexpected draft review payload: %#v", payload)
	}
	if payload.DraftReview.RecommendedFocusContext["priority_trace_id"] != "memory:routing-source" || payload.DraftReview.RecommendedFocusContext["reason"] == "" {
		t.Fatalf("draft review should preserve source recommended_focus_context: %#v", payload.DraftReview)
	}
	if payload.TraceID != payload.DraftReview.TraceID || payload.Status != payload.DraftReview.Status || payload.RecommendedFocusContext["priority_trace_id"] != "memory:routing-source" {
		t.Fatalf("record_draft_review tool should mirror safe top-level handoff fields: %#v", payload)
	}
	if payload.NonExecutingBoundary == "" || payload.NonExecutingBoundary != payload.DraftReview.NonExecutingBoundary || !strings.Contains(payload.NonExecutingBoundary, "draft review audit record only") {
		t.Fatalf("record_draft_review should mirror non-executing boundary: %#v", payload)
	}
	draftReviewCallArgs, ok := payload.DraftReview.RecommendedToolCall["args"].(map[string]interface{})
	if !ok ||
		payload.DraftReview.RecommendedToolCall["tool"] != "experience_learning" ||
		draftReviewCallArgs["action"] != "trace_details" ||
		draftReviewCallArgs["trace_id"] != payload.DraftReview.TraceID {
		t.Fatalf("draft review should expose safe trace inspection recommended tool call: %#v", payload.DraftReview.RecommendedToolCall)
	}
	topDraftReviewCallArgs, ok := payload.RecommendedToolCall["args"].(map[string]interface{})
	if !ok || topDraftReviewCallArgs["action"] != "trace_details" || topDraftReviewCallArgs["trace_id"] != payload.DraftReview.TraceID {
		t.Fatalf("record_draft_review tool should expose top-level trace inspection handoff: %#v", payload.RecommendedToolCall)
	}

	snapshot := buildExperienceLearningSnapshot(nil, store)
	if snapshot.FollowUpTraceCount != 1 || snapshot.FollowUpStatusCounts[experienceFollowUpOutcomeBlocked] != 1 {
		t.Fatalf("record_draft_review should aggregate blocked follow-up evidence: %#v", snapshot)
	}
	if snapshot.TraceKindCounts["routing_adjustment_draft_review"] != 1 || snapshot.TraceSourceCounts[experienceLearningSourceType] != 1 {
		t.Fatalf("record_draft_review should create routing draft review evidence: %#v/%#v", snapshot.TraceKindCounts, snapshot.TraceSourceCounts)
	}
	if snapshot.FollowUpActionKindCounts[experienceDraftKindRouting] != 1 || len(snapshot.FollowUpActionSummaries) != 1 {
		t.Fatalf("record_draft_review should aggregate follow-up action kind: %#v/%#v", snapshot.FollowUpActionKindCounts, snapshot.FollowUpActionSummaries)
	}
	details := parseExperienceTraceDetailsToolResult(t, handler.toolExperienceLearning(map[string]interface{}{
		"action":                "trace_details",
		"filter":                "followups",
		"follow_up_action_kind": experienceDraftKindRouting,
		"source_trace_id":       "memory:routing-source",
		"limit":                 5,
	}))
	if !details.OK || details.Count != 1 || details.TraceDetails[0].ID != payload.DraftReview.TraceID {
		t.Fatalf("trace_details should filter draft review by follow_up_action_kind: %#v", details)
	}
	if details.RecommendedFocusContext["priority_trace_id"] != payload.DraftReview.TraceID || details.RecommendedFocusContext["reason"] != details.RecommendedReason {
		t.Fatalf("trace_details should expose matching recommended_focus_context: %#v", details)
	}
	directFollowUps := handler.app.QueryExperienceFollowUpActions(ExperienceTraceDetailQuery{
		FollowUpActionKind: experienceDraftKindRouting,
		FollowUpStatus:     experienceFollowUpOutcomeBlocked,
		SourceTraceID:      "memory:routing-source",
		Limit:              5,
	})
	if directFollowUps.Count != 1 || directFollowUps.FollowUpDetails[0].ID != payload.DraftReview.TraceID || directFollowUps.FollowUpActionCounts[experienceDraftKindRouting] != 1 {
		t.Fatalf("QueryExperienceFollowUpActions should expose direct action-kind audit details: %#v", directFollowUps)
	}
	followUps := parseExperienceFollowUpActionsToolResult(t, handler.toolExperienceLearning(map[string]interface{}{
		"action":                "follow_up_actions",
		"follow_up_action_kind": experienceDraftKindRouting,
		"source_trace_id":       "memory:routing-source",
		"status":                experienceFollowUpOutcomeBlocked,
		"limit":                 5,
	}))
	if !followUps.OK || followUps.FollowUpActions.Count != 1 || followUps.FollowUpActions.FollowUpDetails[0].ID != payload.DraftReview.TraceID {
		t.Fatalf("follow_up_actions should expose direct action-kind queue details: %#v", followUps)
	}
	if followUps.FollowUpActions.FollowUpActionCounts[experienceDraftKindRouting] != 1 || followUps.FollowUpActions.FollowUpActionSummaries[0].Kind != experienceDraftKindRouting || followUps.FollowUpActions.NonExecutingBoundary == "" {
		t.Fatalf("follow_up_actions should include action summaries and safety boundary: %#v", followUps)
	}
	if followUps.FollowUpActions.Query.TriggeredRollbackOnly {
		t.Fatalf("routing follow_up_actions should not enable triggered rollback filtering: %#v", followUps.FollowUpActions.Query)
	}
	if followUps.FollowUpActions.RecommendedTraceID == "" || followUps.FollowUpActions.RecommendedTraceTitle == "" {
		t.Fatalf("follow_up_actions should expose a recommended trace target: %#v", followUps.FollowUpActions)
	}
	if followUps.FollowUpActions.RecommendedFocusContext["priority_trace_id"] != followUps.FollowUpActions.RecommendedTraceID || followUps.FollowUpActions.RecommendedFocusContext["reason"] != followUps.FollowUpActions.RecommendedReason {
		t.Fatalf("follow_up_actions should expose matching recommended_focus_context: %#v", followUps.FollowUpActions)
	}
	if followUps.RecommendedTraceID != followUps.FollowUpActions.RecommendedTraceID || followUps.RecommendedFocusContext["priority_trace_id"] != followUps.FollowUpActions.RecommendedTraceID || followUps.NonExecutingBoundary == "" {
		t.Fatalf("follow_up_actions tool should mirror safe top-level handoff fields: %#v", followUps)
	}
	followUpCallArgs, ok := followUps.FollowUpActions.RecommendedToolCall["args"].(map[string]interface{})
	if !ok ||
		followUps.FollowUpActions.RecommendedToolCall["tool"] != "experience_learning" ||
		followUpCallArgs["action"] != "trace_details" ||
		followUpCallArgs["filter"] != "followups" ||
		followUpCallArgs["trace_id"] != followUps.FollowUpActions.RecommendedTraceID ||
		followUpCallArgs["follow_up_action_kind"] != experienceDraftKindRouting {
		t.Fatalf("follow_up_actions should expose safe trace inspection recommended tool call: %#v", followUps.FollowUpActions.RecommendedToolCall)
	}
	topFollowUpCallArgs, ok := followUps.RecommendedToolCall["args"].(map[string]interface{})
	if !ok || topFollowUpCallArgs["action"] != "trace_details" || topFollowUpCallArgs["trace_id"] != followUps.FollowUpActions.RecommendedTraceID {
		t.Fatalf("follow_up_actions tool should expose top-level trace inspection handoff: %#v", followUps.RecommendedToolCall)
	}
	governanceSummary := handler.toolExperienceLearning(map[string]interface{}{"action": "governance_summary"})
	for _, want := range []string{`"governance_summary"`, `"recommended_next_action":"inspect_follow_up_actions"`, `"recommended_focus"`, `"trace_filter":"followups"`, `"memory"`, `"routing_self_evolution"`, `"a2a_discussion"`, `"queues"`, `"follow_up_action_summaries"`, experienceDraftKindRouting, "read-only governance summary"} {
		if !strings.Contains(governanceSummary, want) {
			t.Fatalf("governance_summary missing %q: %s", want, governanceSummary)
		}
	}
	var governancePayload struct {
		OK                      bool                   `json:"ok"`
		GovernanceSummary       map[string]interface{} `json:"governance_summary"`
		RecommendedFocusContext map[string]interface{} `json:"recommended_focus_context"`
		RecommendedToolCall     map[string]interface{} `json:"recommended_tool_call"`
		NonExecutingBoundary    string                 `json:"non_executing_boundary"`
	}
	if err := json.Unmarshal([]byte(governanceSummary), &governancePayload); err != nil {
		t.Fatalf("unmarshal governance_summary: %v\n%s", err, governanceSummary)
	}
	if !governancePayload.OK || governancePayload.RecommendedFocusContext["reason"] == "" || governancePayload.RecommendedToolCall["tool"] != "experience_learning" || governancePayload.NonExecutingBoundary == "" {
		t.Fatalf("governance_summary should mirror safe handoff at top level: %#v", governancePayload)
	}
	directSummary := handler.app.GetExperienceGovernanceSummary(ExperienceRoutingSignalQuery{})
	if directSummary["recommended_next_action"] != "inspect_follow_up_actions" {
		t.Fatalf("GetExperienceGovernanceSummary should recommend follow-up inspection: %#v", directSummary)
	}
	focus, ok := directSummary["recommended_focus"].(map[string]interface{})
	if !ok || focus["trace_filter"] != "followups" || focus["non_executing"] != true {
		t.Fatalf("GetExperienceGovernanceSummary should expose a read-only focus hint: %#v", directSummary)
	}
	toolCall, ok := directSummary["recommended_tool_call"].(map[string]interface{})
	if !ok {
		t.Fatalf("GetExperienceGovernanceSummary should expose recommended_tool_call: %#v", directSummary)
	}
	toolArgs, ok := toolCall["args"].(map[string]interface{})
	if !ok || toolArgs["action"] != "follow_up_actions" || toolArgs["follow_up_action_kind"] != experienceDraftKindRouting {
		t.Fatalf("recommended_tool_call should point to the concrete follow-up action queue: %#v", toolCall)
	}
	genericContext, ok := directSummary["recommended_focus_context"].(map[string]interface{})
	if !ok || genericContext["reason"] == "" {
		t.Fatalf("generic follow-up governance should expose read-only recommended_focus_context: %#v", directSummary)
	}
	for _, detail := range snapshot.TraceDetails {
		if detail.ID == payload.DraftReview.TraceID && (detail.FollowUpActionKind != experienceDraftKindRouting || detail.SourceTraceID != "memory:routing-source" || !strings.Contains(detail.FollowUpNote, "routing policy") || detail.NextActionKind != "") {
			t.Fatalf("record_draft_review detail should be audit-only: %#v", detail)
		}
	}
}

func TestExperienceLearningToolSchemaIncludesDraftReviewEvidence(t *testing.T) {
	registry := NewToolRegistry()
	registerBuiltinTools(registry, &IMMessageHandler{app: &App{}})
	tool, ok := registry.Get("experience_learning")
	if !ok {
		t.Fatal("experience_learning tool was not registered")
	}
	for _, want := range []string{
		"record_draft_review",
		"follow_up_actions",
		"governance_summary",
		"snapshot pointing to governance_summary",
		"governance_summary.memory carrying the memory maintenance",
		"governance_summary.routing_self_evolution carrying the routing_signals",
		"governance_summary.a2a_discussion carrying read-only A2A trace inspection handoffs",
		"trace_details exposing read-only non_executing_boundary",
		"recommended_focus_context",
		"recommended_tool_call",
		"governance_focus_context",
		"non_executing=true",
		"never",
	} {
		if !strings.Contains(tool.Description, want) {
			t.Fatalf("experience_learning description missing %q: %s", want, tool.Description)
		}
	}
	if tool.Description == "" {
		t.Fatalf("experience_learning description should describe draft review boundary: %s", tool.Description)
	}
	for _, name := range []string{"draft_kind", "trace_id", "source_trace_id", "draft_markdown", "non_executing_boundary", "status", "note", "actor", "query", "q"} {
		if _, ok := tool.InputSchema[name]; !ok {
			t.Fatalf("experience_learning schema missing %s: %#v", name, tool.InputSchema)
		}
	}
	traceIDSchema := fmt.Sprint(tool.InputSchema["trace_id"])
	if !strings.Contains(traceIDSchema, "exact trace_details filtering") {
		t.Fatalf("trace_id schema should document exact trace_details filtering: %s", traceIDSchema)
	}
	draftKindSchema := fmt.Sprint(tool.InputSchema["draft_kind"])
	for _, want := range []string{"skill_draft", "rollback_workflow_draft", "escalation_brief", "conflict_reconciliation_draft"} {
		if !strings.Contains(draftKindSchema, want) {
			t.Fatalf("experience_learning draft_kind schema missing %s: %s", want, draftKindSchema)
		}
	}
	if _, ok := tool.InputSchema["follow_up_action_kind"]; !ok {
		t.Fatalf("experience_learning schema missing follow_up_action_kind: %#v", tool.InputSchema)
	}
	if _, ok := tool.InputSchema["triggered_rollback_only"]; !ok {
		t.Fatalf("experience_learning schema missing triggered_rollback_only: %#v", tool.InputSchema)
	}
}

type experienceMemoryCandidatesToolPayload struct {
	OK                        bool                                      `json:"ok"`
	Query                     ExperienceMemoryCandidateQuery            `json:"query"`
	ScannedEntries            int                                       `json:"scanned_entries"`
	ActiveEntries             int                                       `json:"active_entries"`
	Total                     int                                       `json:"total"`
	Count                     int                                       `json:"count"`
	Returned                  int                                       `json:"returned"`
	ReasonCounts              map[string]int                            `json:"reason_counts"`
	SourceCounts              map[string]int                            `json:"source_counts"`
	LayeredRecommended        bool                                      `json:"layered_recommended"`
	LayeredReason             string                                    `json:"layered_reason"`
	MaintenanceRecommendation string                                    `json:"maintenance_recommendation"`
	RecommendedFocusContext   map[string]interface{}                    `json:"recommended_focus_context"`
	RecommendedToolCall       map[string]interface{}                    `json:"recommended_tool_call"`
	NonExecutingBoundary      string                                    `json:"non_executing_boundary"`
	MemoryCandidates          []corememory.ProtectedExperienceCandidate `json:"memory_candidates"`
}

type experienceMemoryMaintenanceDraftToolPayload struct {
	OK                      bool                             `json:"ok"`
	MemoryMaintenanceDraft  ExperienceMemoryMaintenanceDraft `json:"memory_maintenance_draft"`
	RecommendedFocusContext map[string]interface{}           `json:"recommended_focus_context"`
	RecommendedToolCall     map[string]interface{}           `json:"recommended_tool_call"`
	NonExecutingBoundary    string                           `json:"non_executing_boundary"`
}

type experienceDraftReviewToolPayload struct {
	OK                      bool                        `json:"ok"`
	DraftReview             ExperienceDraftReviewRecord `json:"draft_review"`
	TraceID                 string                      `json:"trace_id"`
	Status                  string                      `json:"status"`
	RecommendedFocusContext map[string]interface{}      `json:"recommended_focus_context"`
	RecommendedToolCall     map[string]interface{}      `json:"recommended_tool_call"`
	NonExecutingBoundary    string                      `json:"non_executing_boundary"`
}

type experienceFollowUpActionsToolPayload struct {
	OK                      bool                                `json:"ok"`
	FollowUpActions         ExperienceFollowUpActionAuditResult `json:"follow_up_actions"`
	RecommendedTraceID      string                              `json:"recommended_trace_id"`
	RecommendedFocusContext map[string]interface{}              `json:"recommended_focus_context"`
	RecommendedToolCall     map[string]interface{}              `json:"recommended_tool_call"`
	NonExecutingBoundary    string                              `json:"non_executing_boundary"`
}

func parseExperienceMemoryCandidatesToolResult(t *testing.T, raw string) experienceMemoryCandidatesToolPayload {
	t.Helper()
	var payload experienceMemoryCandidatesToolPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal memory candidates result: %v\n%s", err, raw)
	}
	return payload
}

func parseExperienceDraftReviewToolResult(t *testing.T, raw string) experienceDraftReviewToolPayload {
	t.Helper()
	var payload experienceDraftReviewToolPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal draft review result: %v\n%s", err, raw)
	}
	return payload
}

func parseExperienceFollowUpActionsToolResult(t *testing.T, raw string) experienceFollowUpActionsToolPayload {
	t.Helper()
	var payload experienceFollowUpActionsToolPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal follow-up actions result: %v\n%s", err, raw)
	}
	return payload
}

func parseExperienceMemoryMaintenanceDraftToolResult(t *testing.T, raw string) experienceMemoryMaintenanceDraftToolPayload {
	t.Helper()
	var payload experienceMemoryMaintenanceDraftToolPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal memory maintenance draft result: %v\n%s", err, raw)
	}
	return payload
}
