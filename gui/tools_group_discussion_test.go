package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/a2a"
)

func TestDecodeGroupDiscussionAuthorizationDecision(t *testing.T) {
	t.Parallel()
	got, err := decodeGroupDiscussionAuthorizationDecision("```json\n{\"decision\":\"approve\",\"confidence\":0.82,\"reason\":\"explicit approval\"}\n```")
	if err != nil {
		t.Fatalf("decode returned error: %v", err)
	}
	if got.Decision != "approve" || got.Confidence != 0.82 || got.Reason == "" {
		t.Fatalf("decoded %+v, want approve with confidence/reason", got)
	}
}

func TestDecodeGroupDiscussionAuthorizationDecisionRejectsUnknown(t *testing.T) {
	t.Parallel()
	if _, err := decodeGroupDiscussionAuthorizationDecision(`{"decision":"maybe","confidence":0.9,"reason":"bad"}`); err == nil {
		t.Fatal("expected unknown decision to fail")
	}
}

func TestRegisterGroupDiscussionToolIncludesWorkflowActions(t *testing.T) {
	t.Parallel()
	registry := NewToolRegistry()
	registerGroupDiscussionTools(registry, &App{}, &IMMessageHandler{})
	tool, ok := registry.Get("group_discussion")
	if !ok {
		t.Fatal("group_discussion tool was not registered")
	}
	for _, action := range []string{"workflow_state", "workflow_action_draft", "escalation_route", "rollback_readiness"} {
		if !strings.Contains(tool.Description, action) {
			t.Fatalf("description missing %s: %q", action, tool.Description)
		}
	}
	for _, term := range []string{"status, list_experts", "list_mine, get_discussion, get_detail", "summarize_result with preview=true", "cleanup_stale with dry_run=true", "summary-preview", "cleanup-preview", "workflow_action_draft", "recommended_focus_context", "discussion_focus_context", "recommended_tool_call", "non_executing_boundary"} {
		if !strings.Contains(tool.Description, term) {
			t.Fatalf("description missing %s: %q", term, tool.Description)
		}
	}
	actionSchema, ok := tool.InputSchema["action"].(map[string]interface{})
	if !ok {
		t.Fatalf("action schema missing or wrong type: %#v", tool.InputSchema["action"])
	}
	for _, action := range []string{"workflow_state", "workflow_action_draft", "escalation_route", "rollback_readiness"} {
		if !strings.Contains(fmt.Sprint(actionSchema["description"]), action) {
			t.Fatalf("action schema missing %s: %#v", action, actionSchema)
		}
	}
}

func TestGroupDiscussionToolResultMirrorsSafeHandoff(t *testing.T) {
	t.Parallel()
	status := GroupDiscussionStatus{
		Enabled:               true,
		Discoverable:          true,
		Experts:               []a2a.GroupProfile{{AgentID: "expert-a", DisplayName: "Expert A", Skills: []string{"go"}}},
		Discussions:           []a2a.HubDiscussionSummary{{ID: "disc-status", Topic: "status topic", Status: string(a2a.SessionOpen)}},
		ActiveDiscussionCount: 1,
	}
	focusContext := groupDiscussionStatusFocusContext(status)
	raw := groupDiscussionResultWithSafeHandoff(map[string]interface{}{"status": status}, focusContext, groupDiscussionStatusToolCall(focusContext), groupDiscussionStatusNonExecutingBoundary, nil)
	var payload struct {
		OK                      bool                              `json:"ok"`
		Result                  map[string]json.RawMessage        `json:"result"`
		RecommendedFocusContext map[string]interface{}            `json:"recommended_focus_context"`
		RecommendedToolCall     GroupDiscussionToolCallSuggestion `json:"recommended_tool_call"`
		NonExecutingBoundary    string                            `json:"non_executing_boundary"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal status result: %v\n%s", err, raw)
	}
	if !payload.OK || len(payload.Result["status"]) == 0 || payload.RecommendedFocusContext["action_kind"] != "inspect_group_discussion_status" {
		t.Fatalf("status handoff should preserve status and focus context: %s", raw)
	}
	if payload.RecommendedToolCall.Args["action"] != "get_detail" || payload.RecommendedToolCall.Args["consultation_id"] != "disc-status" || !payload.RecommendedToolCall.NonExecuting {
		t.Fatalf("status recommended tool call = %#v", payload.RecommendedToolCall)
	}
	if !strings.Contains(payload.NonExecutingBoundary, "read-only group discussion status") {
		t.Fatalf("status boundary = %q", payload.NonExecutingBoundary)
	}

	experts := []a2a.GroupProfile{{AgentID: "expert-b", DisplayName: "Expert B", Skills: []string{"security"}}}
	focusContext = groupDiscussionExpertsFocusContext(experts)
	raw = groupDiscussionResultWithSafeHandoff(map[string]interface{}{"experts": experts}, focusContext, groupDiscussionExpertsToolCall(focusContext), groupDiscussionExpertsNonExecutingBoundary, nil)
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal experts result: %v\n%s", err, raw)
	}
	if payload.RecommendedFocusContext["action_kind"] != "inspect_group_discussion_experts" || payload.RecommendedFocusContext["recommended_agent_id"] != "expert-b" {
		t.Fatalf("expert focus context = %#v", payload.RecommendedFocusContext)
	}
	if payload.RecommendedToolCall.Args["action"] != "rank_experts" || !strings.Contains(payload.NonExecutingBoundary, "read-only expert discovery") {
		t.Fatalf("expert handoff = %#v / %q", payload.RecommendedToolCall, payload.NonExecutingBoundary)
	}

	state := GroupDiscussionWorkflowState{
		ConsultationID:          "disc-1",
		Status:                  string(a2a.SessionOpen),
		SuggestedNextActionKind: "collect_reviews",
		SuggestedNextAction:     "Collect proposal reviews before deciding.",
		NonExecutingBoundary:    "read-only workflow state; no proposal, review, decision, escalation, message, result submission, or discussion state change was performed",
	}
	state.RecommendedFocusContext = groupDiscussionWorkflowStateFocusContext(state)
	state.RecommendedToolCall = groupDiscussionWorkflowStateToolCall(state)

	raw = groupDiscussionResultWithSafeHandoff(map[string]interface{}{"workflow_state": state}, state.RecommendedFocusContext, state.RecommendedToolCall, state.NonExecutingBoundary, nil)
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal group_discussion result: %v\n%s", err, raw)
	}
	if !payload.OK || len(payload.Result["workflow_state"]) == 0 {
		t.Fatalf("payload should preserve nested workflow_state result: %s", raw)
	}
	if payload.RecommendedFocusContext["consultation_id"] != "disc-1" || payload.RecommendedFocusContext["suggested_next_action_kind"] != "collect_reviews" {
		t.Fatalf("top-level focus context = %#v", payload.RecommendedFocusContext)
	}
	if payload.RecommendedToolCall.Tool != "group_discussion" || !payload.RecommendedToolCall.NonExecuting || payload.RecommendedToolCall.Args["action"] != "workflow_state" || payload.RecommendedToolCall.Args["consultation_id"] != "disc-1" {
		t.Fatalf("top-level recommended tool call = %#v", payload.RecommendedToolCall)
	}
	if payload.RecommendedToolCall.RecommendedFocusContext["consultation_id"] != "disc-1" || payload.RecommendedToolCall.DiscussionFocusContext["consultation_id"] != "disc-1" {
		t.Fatalf("top-level recommended tool call should expose both focus aliases: %#v", payload.RecommendedToolCall)
	}
	if !strings.Contains(payload.NonExecutingBoundary, "read-only workflow state") {
		t.Fatalf("top-level non-executing boundary = %q", payload.NonExecutingBoundary)
	}

	summary := finalizeGroupDiscussionSummaryPreview(GroupDiscussionSummarizeResult{ConsultationID: "disc-2", Summary: "Use staged rollout", AnswerCount: 2})
	raw = groupDiscussionResultWithSafeHandoff(map[string]interface{}{"summary": summary}, summary.RecommendedFocusContext, summary.RecommendedToolCall, summary.NonExecutingBoundary, nil)
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal preview summary result: %v\n%s", err, raw)
	}
	if payload.RecommendedFocusContext["action_kind"] != "summary_preview" || payload.RecommendedToolCall.Args["action"] != "get_detail" || payload.RecommendedToolCall.DiscussionFocusContext["consultation_id"] != "disc-2" {
		t.Fatalf("preview summary top-level handoff = %#v / %#v", payload.RecommendedFocusContext, payload.RecommendedToolCall)
	}
	if !strings.Contains(payload.NonExecutingBoundary, "preview-only") {
		t.Fatalf("preview summary boundary = %q", payload.NonExecutingBoundary)
	}

	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: "disc-3", Status: string(a2a.SessionOpen), Topic: "deployment", Question: "Which rollout?"},
		Messages:   []a2a.Message{{ID: "m1", FromID: "expert-a", Kind: a2a.MessageAnswer, Content: "Use staged rollout."}},
	}
	focusContext = groupDiscussionDetailFocusContext(detail, "")
	raw = groupDiscussionResultWithSafeHandoff(map[string]interface{}{"discussion_detail": detail}, focusContext, groupDiscussionDetailToolCall(focusContext), groupDiscussionDetailNonExecutingBoundary, nil)
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal detail result: %v\n%s", err, raw)
	}
	if payload.RecommendedFocusContext["action_kind"] != "inspect_discussion_detail" || payload.RecommendedFocusContext["consultation_id"] != "disc-3" || payload.RecommendedToolCall.Args["action"] != "workflow_state" {
		t.Fatalf("detail top-level handoff = %#v / %#v", payload.RecommendedFocusContext, payload.RecommendedToolCall)
	}
	if payload.RecommendedToolCall.DiscussionFocusContext["topic"] != "deployment" || !strings.Contains(payload.NonExecutingBoundary, "read-only discussion detail") {
		t.Fatalf("detail handoff aliases/boundary = %#v / %q", payload.RecommendedToolCall, payload.NonExecutingBoundary)
	}

	discussions := []a2a.HubDiscussionSummary{{ID: "disc-4", Topic: "routing", Status: string(a2a.SessionOpen), MessageCount: 3}}
	focusContext = groupDiscussionListMineFocusContext(discussions, "initiated")
	raw = groupDiscussionResultWithSafeHandoff(map[string]interface{}{"discussions": discussions}, focusContext, groupDiscussionListMineToolCall(focusContext), groupDiscussionListMineNonExecutingBoundary, nil)
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal list result: %v\n%s", err, raw)
	}
	if payload.RecommendedFocusContext["action_kind"] != "list_discussions" || payload.RecommendedToolCall.Args["action"] != "get_detail" || payload.RecommendedToolCall.Args["consultation_id"] != "disc-4" {
		t.Fatalf("list top-level handoff = %#v / %#v", payload.RecommendedFocusContext, payload.RecommendedToolCall)
	}

	summaryDiscussion := a2a.HubDiscussionSummary{ID: "disc-5", Topic: "memory", Status: string(a2a.SessionOpen), MessageCount: 2, AnswerCount: 1}
	focusContext = groupDiscussionSummaryFocusContext(summaryDiscussion, "")
	raw = groupDiscussionResultWithSafeHandoff(map[string]interface{}{"discussion": summaryDiscussion}, focusContext, groupDiscussionSummaryToolCall(focusContext), groupDiscussionSummaryNonExecutingBoundary, nil)
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal summary result: %v\n%s", err, raw)
	}
	if payload.RecommendedFocusContext["action_kind"] != "inspect_discussion_summary" || payload.RecommendedToolCall.Args["action"] != "get_detail" || payload.RecommendedToolCall.Args["consultation_id"] != "disc-5" {
		t.Fatalf("summary top-level handoff = %#v / %#v", payload.RecommendedFocusContext, payload.RecommendedToolCall)
	}

	cleanup := GroupDiscussionStaleCleanupResult{DryRun: true, TimeoutSeconds: 300, Stale: []a2a.HubDiscussionSummary{{ID: "disc-6", Status: string(a2a.SessionOpen)}}}
	focusContext = groupDiscussionStaleCleanupFocusContext(cleanup)
	raw = groupDiscussionResultWithSafeHandoff(map[string]interface{}{"cleanup": cleanup}, focusContext, groupDiscussionStaleCleanupToolCall(focusContext), groupDiscussionStaleCleanupNonExecutingBoundary, nil)
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal cleanup result: %v\n%s", err, raw)
	}
	if payload.RecommendedFocusContext["action_kind"] != "cleanup_stale_preview" || payload.RecommendedToolCall.Args["action"] != "get_detail" || payload.RecommendedToolCall.Args["consultation_id"] != "disc-6" {
		t.Fatalf("cleanup top-level handoff = %#v / %#v", payload.RecommendedFocusContext, payload.RecommendedToolCall)
	}
	if !strings.Contains(payload.NonExecutingBoundary, "dry-run") {
		t.Fatalf("cleanup boundary = %q", payload.NonExecutingBoundary)
	}

	minimalCall := &GroupDiscussionToolCallSuggestion{
		Tool: "group_discussion",
		Args: map[string]interface{}{"action": "get_detail", "consultation_id": "disc-7"},
	}
	focusContext = map[string]interface{}{"action_kind": "inspect_discussion_detail", "consultation_id": "disc-7"}
	raw = groupDiscussionResultWithSafeHandoff(map[string]interface{}{"discussion": map[string]string{"id": "disc-7"}}, focusContext, minimalCall, groupDiscussionDetailNonExecutingBoundary, nil)
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal normalized handoff result: %v\n%s", err, raw)
	}
	if !payload.RecommendedToolCall.NonExecuting ||
		payload.RecommendedToolCall.RecommendedFocusContext["consultation_id"] != "disc-7" ||
		payload.RecommendedToolCall.DiscussionFocusContext["consultation_id"] != "disc-7" ||
		!strings.Contains(payload.RecommendedToolCall.NonExecutingBoundary, "read-only discussion detail") {
		t.Fatalf("minimal safe handoff was not normalized: %#v", payload.RecommendedToolCall)
	}
}

func TestGroupDiscussionStatusModelCarriesSafeHandoff(t *testing.T) {
	t.Parallel()
	status := groupDiscussionStatusWithSafeHandoff(GroupDiscussionStatus{
		Enabled:               true,
		Discoverable:          true,
		Experts:               []a2a.GroupProfile{{AgentID: "expert-a", DisplayName: "Expert A"}},
		Discussions:           []a2a.HubDiscussionSummary{{ID: "disc-1", Topic: "rollout", Status: string(a2a.SessionOpen), ReadyToSummarize: true}},
		ActiveDiscussionCount: 1,
		ReadyDiscussionCount:  1,
	})
	if status.RecommendedFocusContext["action_kind"] != "inspect_group_discussion_status" || status.RecommendedToolCall == nil {
		t.Fatalf("status safe handoff missing: %+v", status)
	}
	if status.RecommendedToolCall.Args["action"] != "get_detail" || status.RecommendedToolCall.Args["consultation_id"] != "disc-1" {
		t.Fatalf("status recommended call = %#v", status.RecommendedToolCall)
	}
	if !strings.Contains(status.NonExecutingBoundary, "read-only group discussion status inspection") {
		t.Fatalf("status boundary = %q", status.NonExecutingBoundary)
	}
	if status.RecommendedToolCall.DiscussionFocusContext["action_kind"] != "inspect_group_discussion_status" ||
		!strings.Contains(status.RecommendedToolCall.NonExecutingBoundary, "read-only group discussion status inspection") ||
		!status.RecommendedToolCall.NonExecuting {
		t.Fatalf("status direct handoff should be normalized: %#v", status.RecommendedToolCall)
	}

	pending := groupDiscussionStatusWithSafeHandoff(GroupDiscussionStatus{
		Enabled:        true,
		Discoverable:   true,
		PendingInvites: []a2a.GroupInviteSummary{{ID: "inv-1", SessionID: "disc-pending", FromID: "expert-a", Role: a2a.GroupRoleSpeak, Topic: "review rollout"}},
	})
	if pending.RecommendedFocusContext["recommended_action_kind"] != "review_pending_invites" ||
		pending.RecommendedFocusContext["recommended_invite_id"] != "inv-1" ||
		pending.RecommendedFocusContext["recommended_consultation_id"] != "disc-pending" {
		t.Fatalf("pending invite focus context = %#v", pending.RecommendedFocusContext)
	}
	if pending.RecommendedToolCall == nil || pending.RecommendedToolCall.Args["action"] != "status" {
		t.Fatalf("pending invite status should remain read-only status inspection: %#v", pending.RecommendedToolCall)
	}
	if _, hasConsultationArg := pending.RecommendedToolCall.Args["consultation_id"]; hasConsultationArg {
		t.Fatalf("pending invite status should not suggest detail fetch as invite processing handoff: %#v", pending.RecommendedToolCall.Args)
	}
}

func TestGroupDiscussionDirectSafeHandoffsAreNormalized(t *testing.T) {
	t.Parallel()
	ranking := rankGroupDiscussionExperts([]a2a.GroupProfile{{AgentID: "expert-a", Discoverable: true, Available: true}}, corelib.AppConfig{RemoteMachineID: "local"}, a2a.GroupConsultationRequest{Topic: "deployment"}, 1)
	if ranking.RecommendedToolCall == nil ||
		ranking.RecommendedToolCall.DiscussionFocusContext["action_kind"] != "rank_experts" ||
		!strings.Contains(ranking.RecommendedToolCall.NonExecutingBoundary, "expert ranking preview") ||
		!ranking.RecommendedToolCall.NonExecuting {
		t.Fatalf("ranking direct handoff should be normalized: %#v", ranking.RecommendedToolCall)
	}

	readiness := finalizeGroupDiscussionReadiness(GroupDiscussionReadiness{ConsultationID: "disc-1", Ready: true})
	if readiness.RecommendedToolCall == nil ||
		readiness.RecommendedToolCall.DiscussionFocusContext["consultation_id"] != "disc-1" ||
		!strings.Contains(readiness.RecommendedToolCall.NonExecutingBoundary, "readiness") ||
		!readiness.RecommendedToolCall.NonExecuting {
		t.Fatalf("readiness direct handoff should be normalized: %#v", readiness.RecommendedToolCall)
	}

	preview := finalizeGroupDiscussionSummaryPreview(GroupDiscussionSummarizeResult{ConsultationID: "disc-2", Summary: "Use staged rollout"})
	if preview.RecommendedToolCall == nil ||
		preview.RecommendedToolCall.DiscussionFocusContext["consultation_id"] != "disc-2" ||
		!strings.Contains(preview.RecommendedToolCall.NonExecutingBoundary, "preview") ||
		!preview.RecommendedToolCall.NonExecuting {
		t.Fatalf("summary preview direct handoff should be normalized: %#v", preview.RecommendedToolCall)
	}
}

func TestGroupDiscussionSuggestReturnsPreAuthorizationHandoff(t *testing.T) {
	t.Parallel()
	app := &App{}
	raw := groupDiscussionSuggest(app, map[string]interface{}{
		"topic":           "deployment",
		"question":        "Which rollout?",
		"context_summary": "Need staged rollout review.",
		"skills_wanted":   []interface{}{"go", "security"},
		"risk_level":      "high",
	})
	var payload struct {
		CurrentHubOnly          bool                              `json:"current_hub_only"`
		Topic                   string                            `json:"topic"`
		RecommendedFocusContext map[string]interface{}            `json:"recommended_focus_context"`
		RecommendedToolCall     GroupDiscussionToolCallSuggestion `json:"recommended_tool_call"`
		NonExecutingBoundary    string                            `json:"non_executing_boundary"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal suggest result: %v\n%s", err, raw)
	}
	if !payload.CurrentHubOnly || payload.Topic != "deployment" || payload.RecommendedFocusContext["action_kind"] != "suggest_group_discussion" {
		t.Fatalf("suggest focus payload = %#v", payload)
	}
	if payload.RecommendedToolCall.Tool != "group_discussion" || !payload.RecommendedToolCall.NonExecuting {
		t.Fatalf("suggest should expose a non-executing group_discussion handoff: %#v", payload.RecommendedToolCall)
	}
	if action := payload.RecommendedToolCall.Args["action"]; action != "rank_experts" && action != "status" {
		t.Fatalf("suggest recommended action = %#v, want rank_experts when available or status when unavailable", action)
	}
	if payload.RecommendedToolCall.RecommendedFocusContext["topic"] != "deployment" || payload.RecommendedToolCall.DiscussionFocusContext["risk_level"] != "high" {
		t.Fatalf("suggest recommended call should carry focus aliases: %#v", payload.RecommendedToolCall)
	}
	if !strings.Contains(payload.NonExecutingBoundary, "no discussion was started") {
		t.Fatalf("suggest boundary = %q", payload.NonExecutingBoundary)
	}
}

func TestSelectGroupDiscussionInvitees_RestrictsEmptySecurityGroupWhenFreeDiscussion(t *testing.T) {
	t.Parallel()
	cfg := corelib.AppConfig{RemoteMachineID: "local"}
	cfg.GroupDiscussion.AllowSecurityGroupFreeDiscussion = true
	cfg.GroupDiscussion.SecurityGroupID = "team-a"
	experts := []a2a.GroupProfile{
		{AgentID: "same", SecurityGroupID: "team-a", Discoverable: true, Available: true, Skills: []string{"go"}},
		{AgentID: "empty", Discoverable: true, Available: true, Skills: []string{"go"}},
		{AgentID: "other", SecurityGroupID: "team-b", Discoverable: true, Available: true, Skills: []string{"go"}},
	}
	got := selectGroupDiscussionInvitees(experts, cfg, a2a.GroupConsultationRequest{SkillsWanted: []string{"go"}})
	if len(got) != 1 || got[0] != "same" {
		t.Fatalf("selected %v, want only same security group expert", got)
	}
}

func TestGroupDiscussionShouldAutoContribute(t *testing.T) {
	t.Parallel()
	cfg := corelib.AppConfig{}
	cfg.GroupDiscussion.Enabled = true
	if !groupDiscussionShouldAutoContribute(cfg, a2a.GroupInviteSummary{Role: a2a.GroupRoleSpeak}) {
		t.Fatal("speak invite should auto contribute")
	}
	if groupDiscussionShouldAutoContribute(cfg, a2a.GroupInviteSummary{Role: a2a.GroupRoleObserve}) {
		t.Fatal("observe invite should not auto contribute")
	}
	cfg.GroupDiscussion.RejectWhenDND = true
	cfg.GroupDiscussion.Availability = "dnd"
	if groupDiscussionShouldAutoContribute(cfg, a2a.GroupInviteSummary{Role: a2a.GroupRoleSpeak}) {
		t.Fatal("DND should suppress auto contribution")
	}
}

func TestGroupDiscussionRoleAllowed(t *testing.T) {
	t.Parallel()
	cfg := corelib.AppConfig{}
	if !groupDiscussionRoleAllowed(cfg, a2a.GroupRoleSpeak) || !groupDiscussionRoleAllowed(cfg, a2a.GroupRoleReview) || !groupDiscussionRoleAllowed(cfg, a2a.GroupRoleObserve) {
		t.Fatal("empty allowed roles should use safe defaults")
	}
	cfg.GroupDiscussion.AllowedRoles = []string{"observe"}
	if !groupDiscussionRoleAllowed(cfg, a2a.GroupRoleObserve) {
		t.Fatal("observe role should be allowed")
	}
	if groupDiscussionRoleAllowed(cfg, a2a.GroupRoleSpeak) {
		t.Fatal("speak role should be blocked by allowed_roles")
	}
	if groupDiscussionShouldAutoContribute(cfg, a2a.GroupInviteSummary{Role: a2a.GroupRoleSpeak}) {
		t.Fatal("blocked role should not auto contribute")
	}
}

func TestBuildGroupDiscussionContributionInput(t *testing.T) {
	t.Parallel()
	cfg := corelib.AppConfig{MaclawRoleName: "Architect"}
	cfg.GroupDiscussion.ContextPolicy = "summary_only"
	profile := a2a.GroupProfile{DisplayName: "Reviewer", Skills: []string{"go", "security"}, Description: "reviews backend designs"}
	input := buildGroupDiscussionContributionInput(profile, cfg, a2a.GroupInviteSummary{Topic: "T", Question: "Q", Role: a2a.GroupRoleReview})
	for _, want := range []string{"Reviewer", "go, security", "reviews backend designs", "T", "Q", "review", "summary_only"} {
		if !strings.Contains(input, want) {
			t.Fatalf("input missing %q:\n%s", want, input)
		}
	}
}

func TestGroupDiscussionRiskRank(t *testing.T) {
	t.Parallel()
	if groupDiscussionRiskRank("low") >= groupDiscussionRiskRank("medium") {
		t.Fatal("low should rank below medium")
	}
	if groupDiscussionRiskRank("critical") <= groupDiscussionRiskRank("high") {
		t.Fatal("critical should rank above high")
	}
	if groupDiscussionRiskRank("unknown") != groupDiscussionRiskRank("medium") {
		t.Fatal("unknown risk should fail to medium rank")
	}
}

func TestGroupDiscussionMessageKind(t *testing.T) {
	t.Parallel()
	if got := groupDiscussionMessageKind("evidence"); got != a2a.MessageEvidence {
		t.Fatalf("message kind = %q, want evidence", got)
	}
	if got := groupDiscussionMessageKind("bad"); got != a2a.MessageStatement {
		t.Fatalf("message kind = %q, want default statement", got)
	}
}

func TestGroupDiscussionReviewPosition(t *testing.T) {
	t.Parallel()
	if got := groupDiscussionReviewPosition("concern"); got != a2a.ReviewConcern {
		t.Fatalf("review position = %q, want concern", got)
	}
	if got := groupDiscussionReviewPosition("bad"); got != a2a.ReviewAbstain {
		t.Fatalf("review position = %q, want default abstain", got)
	}
}

func TestNormalizeGroupDiscussionEscalationDefaultsTargetAndRaisedBy(t *testing.T) {
	t.Parallel()
	got := normalizeGroupDiscussionEscalation(a2a.Escalation{Reason: " needs owner "}, " local-agent ")
	if got.Target != "human_owner" {
		t.Fatalf("target = %q, want default human_owner", got.Target)
	}
	if got.RaisedBy != "local-agent" {
		t.Fatalf("raised_by = %q, want fallback", got.RaisedBy)
	}
	if got.Reason != "needs owner" {
		t.Fatalf("reason = %q, want trimmed", got.Reason)
	}
}

func TestSummarizeGroupDiscussionDetailUsesExpertAnswers(t *testing.T) {
	t.Parallel()
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: "disc-1", Topic: "architecture", Question: "Which path?"},
		Messages: []a2a.Message{
			{ID: "m1", FromID: "maclaw-a", Kind: a2a.MessageAnswer, Content: "invitation inv-1: accept"},
			{ID: "m2", FromID: "maclaw-b", Kind: a2a.MessageAnswer, Content: "Prefer a staged rollout."},
			{ID: "m3", FromID: "maclaw-c", Kind: a2a.MessageObjection, Content: "Watch migration risk."},
		},
	}
	got := summarizeGroupDiscussionDetail(detail)
	if got.AnswerCount != 2 {
		t.Fatalf("AnswerCount = %d, want 2", got.AnswerCount)
	}
	if !strings.Contains(got.Rationale, "staged rollout") || strings.Contains(got.Rationale, "invitation inv-1") {
		t.Fatalf("rationale = %q", got.Rationale)
	}
}

func TestDecodeGroupDiscussionResultSummary(t *testing.T) {
	t.Parallel()
	got, err := decodeGroupDiscussionResultSummary("```json\n{\"summary\":\"Use staged rollout\",\"rationale\":\"Two experts agree\",\"risks\":[\"Migration risk\",\"\"]}\n```")
	if err != nil {
		t.Fatalf("decode returned error: %v", err)
	}
	if got.Summary != "Use staged rollout" || got.Rationale == "" || len(got.Risks) != 1 {
		t.Fatalf("decoded = %+v", got)
	}
}

func TestFormatGroupDiscussionSupplement(t *testing.T) {
	t.Parallel()
	text := formatGroupDiscussionSupplement(GroupDiscussionSummarizeResult{ConsultationID: "disc-1", Summary: "Use staged rollout", Rationale: "Safer", Risks: []string{"Migration risk"}})
	for _, want := range []string{"disc-1", "Use staged rollout", "Safer", "Migration risk"} {
		if !strings.Contains(text, want) {
			t.Fatalf("supplement missing %q: %s", want, text)
		}
	}
}

func TestGroupDiscussionSummarizeResultInjectUsesRuntimeOwner(t *testing.T) {
	ownerID := "weixin:user-1"
	desktopID := "desktop-user"
	handler := &IMMessageHandler{lastUserID: desktopID, currentLoopCtx: &LoopContext{Runtime: RuntimeContext{RequestID: "req-desktop", PolicyOwnerID: desktopID}}}
	ownerState := handler.getSessionLoop(ownerID)
	ownerState.stateMu.Lock()
	ownerState.loopCtx = NewLoopContext("chat", 20, nil)
	ownerState.stateMu.Unlock()
	desktopState := handler.getSessionLoop(desktopID)
	desktopState.stateMu.Lock()
	desktopState.loopCtx = NewLoopContext("desktop", 20, nil)
	desktopState.stateMu.Unlock()
	app := &App{imHandler: handler}
	if !toolAcceptsRuntimePolicyOwnerArg("group_discussion") {
		t.Fatal("group_discussion must accept hidden runtime owner args")
	}
	args := map[string]interface{}{
		registeredToolPolicyOwnerIDField: ownerID,
	}
	gotOwner, explicitRuntime := groupDiscussionRuntimeOwnerForInjection(handler, args, true)
	if !explicitRuntime || gotOwner != ownerID {
		t.Fatalf("runtime owner = %q explicit=%v, want %q explicit", gotOwner, explicitRuntime, ownerID)
	}
	if _, ok := args[registeredToolPolicyOwnerIDField]; ok {
		t.Fatal("runtime owner arg leaked after consumption")
	}
	if _, err := app.injectGroupDiscussionSummaryForUser(gotOwner, GroupDiscussionSummarizeResult{ConsultationID: "disc-runtime-owner", Summary: "Use staged rollout"}); err != nil {
		t.Fatalf("injectGroupDiscussionSummaryForUser: %v", err)
	}
	if _, ok := handler.pendingInjection.Load(desktopID); ok {
		t.Fatal("group discussion injection leaked into desktop/lastUserID session")
	}
	pending, ok := handler.pendingInjection.Load(ownerID)
	if !ok || !strings.Contains(fmt.Sprint(pending), "Use staged rollout") {
		t.Fatalf("owner pending injection = %#v, ok=%v", pending, ok)
	}
}

func TestGroupDiscussionSummarizeResultEmptyRuntimeOwnerFailsClosed(t *testing.T) {
	handler := &IMMessageHandler{lastUserID: "desktop-user", currentLoopCtx: &LoopContext{Runtime: RuntimeContext{RequestID: "req-desktop", PolicyOwnerID: "desktop-user"}}}
	desktopState := handler.getSessionLoop("desktop-user")
	desktopState.stateMu.Lock()
	desktopState.loopCtx = NewLoopContext("desktop", 20, nil)
	desktopState.stateMu.Unlock()
	app := &App{imHandler: handler}

	raw := dispatchGroupDiscussionTool(app, handler, map[string]interface{}{
		"action":                         "summarize_result",
		"consultation_id":                "disc-empty-owner",
		"force":                          true,
		"inject":                         true,
		registeredToolPolicyOwnerIDField: "",
	})
	var payload struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal summarize result: %v\n%s", err, raw)
	}
	if payload.OK || !strings.Contains(payload.Error, "non-empty runtime owner") {
		t.Fatalf("empty runtime owner should fail closed, got %s", raw)
	}
	if _, ok := handler.pendingInjection.Load("desktop-user"); ok {
		t.Fatal("empty runtime owner inherited desktop/lastUserID injection")
	}
}

func TestGroupDiscussionSummarizeResultInjectWithoutHiddenOwnerDoesNotUseCurrentLoop(t *testing.T) {
	handler := &IMMessageHandler{lastUserID: "desktop-user", currentLoopCtx: &LoopContext{Runtime: RuntimeContext{RequestID: "req-desktop", PolicyOwnerID: "desktop-user"}}}
	desktopState := handler.getSessionLoop("desktop-user")
	desktopState.stateMu.Lock()
	desktopState.loopCtx = NewLoopContext("desktop", 20, nil)
	desktopState.stateMu.Unlock()
	app := &App{imHandler: handler}

	raw := dispatchGroupDiscussionTool(app, handler, map[string]interface{}{
		"action":          "summarize_result",
		"consultation_id": "disc-no-hidden-owner",
		"force":           true,
		"inject":          true,
	})
	var payload struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal summarize result: %v\n%s", err, raw)
	}
	if payload.OK || !strings.Contains(payload.Error, "non-empty runtime owner") {
		t.Fatalf("missing hidden runtime owner should fail closed, got %s", raw)
	}
	if _, ok := handler.pendingInjection.Load("desktop-user"); ok {
		t.Fatal("missing hidden runtime owner inherited currentLoopCtx injection")
	}
}

func TestFinalizeGroupDiscussionSummaryPreviewCarriesSafeHandoff(t *testing.T) {
	t.Parallel()
	got := finalizeGroupDiscussionSummaryPreview(GroupDiscussionSummarizeResult{ConsultationID: "disc-1", Summary: "Use staged rollout", AnswerCount: 2, UsedLLM: true})
	if got.Submitted || got.Injected {
		t.Fatalf("preview should not mark submitted/injected: %+v", got)
	}
	if got.RecommendedFocusContext["action_kind"] != "summary_preview" || got.RecommendedFocusContext["consultation_id"] != "disc-1" || got.RecommendedFocusContext["answer_count"] != 2 || got.RecommendedFocusContext["used_llm"] != true {
		t.Fatalf("preview focus context = %#v", got.RecommendedFocusContext)
	}
	if got.RecommendedToolCall == nil || got.RecommendedToolCall.Args["action"] != "get_detail" || got.RecommendedToolCall.Args["consultation_id"] != "disc-1" || !got.RecommendedToolCall.NonExecuting {
		t.Fatalf("preview recommended call = %#v", got.RecommendedToolCall)
	}
	if got.RecommendedToolCall.RecommendedFocusContext["consultation_id"] != "disc-1" || got.RecommendedToolCall.DiscussionFocusContext["action_kind"] != "summary_preview" || got.NonExecutingBoundary == "" || got.RecommendedToolCall.NonExecutingBoundary == "" {
		t.Fatalf("preview handoff should carry focus aliases and boundaries: %#v", got)
	}
}

func TestGroupDiscussionReadinessWaitsForExpectedAnswers(t *testing.T) {
	t.Parallel()
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: "disc-1", Status: string(a2a.SessionOpen), ParticipantIDs: []string{"initiator", "expert-a", "expert-b"}},
		Messages:   []a2a.Message{{ID: "m1", FromID: "expert-a", Kind: a2a.MessageAnswer, Content: "Prefer staged rollout."}},
	}
	got := groupDiscussionReadiness(detail)
	if got.Ready {
		t.Fatalf("readiness = %+v, want not ready", got)
	}
	if got.AnswerCount != 1 || got.ExpectedAnswerCount != 2 {
		t.Fatalf("readiness counts = %+v", got)
	}
}

func TestGroupDiscussionReadinessReadyWithEnoughAnswers(t *testing.T) {
	t.Parallel()
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: "disc-1", Status: string(a2a.SessionOpen), ParticipantIDs: []string{"initiator", "expert-a", "expert-b"}},
		Messages: []a2a.Message{
			{ID: "m1", FromID: "expert-a", Kind: a2a.MessageAnswer, Content: "Prefer staged rollout."},
			{ID: "m2", FromID: "expert-b", Kind: a2a.MessageAnswer, Content: "Add rollback plan."},
		},
	}
	got := groupDiscussionReadiness(detail)
	if !got.Ready || got.AnswerCount != 2 || got.ExpectedAnswerCount != 2 {
		t.Fatalf("readiness = %+v, want ready with two answers", got)
	}
	got.ConsultationID = "disc-1"
	got = finalizeGroupDiscussionReadiness(got)
	if got.RecommendedFocusContext["action_kind"] != "preview_summary" || got.RecommendedToolCall == nil || got.RecommendedToolCall.Args["action"] != "summarize_result" || got.RecommendedToolCall.Args["preview"] != true {
		t.Fatalf("ready handoff = focus:%#v call:%#v", got.RecommendedFocusContext, got.RecommendedToolCall)
	}
	if got.RecommendedToolCall.RecommendedFocusContext["consultation_id"] != "disc-1" || got.NonExecutingBoundary == "" {
		t.Fatalf("ready handoff should carry focus alias and boundary: %#v", got)
	}
}

func TestGroupDiscussionReadinessIgnoresInitiatorAndObserverMessages(t *testing.T) {
	t.Parallel()
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: "disc-roles", Status: string(a2a.SessionOpen), ParticipantIDs: []string{"human", "maclaw", "observer"}},
		Session: &a2a.Session{Participants: []a2a.Participant{
			{ID: "human", RoleCode: "initiator"},
			{ID: "maclaw", RoleCode: "review"},
			{ID: "observer", RoleCode: "observe"},
		}},
		Messages: []a2a.Message{
			{ID: "m1", FromID: "human", Kind: a2a.MessageStatement, Content: "Here is more background."},
			{ID: "m2", FromID: "observer", Kind: a2a.MessageStatement, Content: "I am watching."},
		},
	}
	got := groupDiscussionReadiness(detail)
	if got.AnswerCount != 0 || got.ExpectedAnswerCount != 1 || got.Ready {
		t.Fatalf("readiness = %+v, want initiator/observer ignored", got)
	}
	detail.Messages = append(detail.Messages, a2a.Message{ID: "m3", FromID: "maclaw", Kind: a2a.MessageAnswer, Content: "Approve with a retention note."})
	got = groupDiscussionReadiness(detail)
	if got.AnswerCount != 1 || got.ExpectedAnswerCount != 1 || !got.Ready {
		t.Fatalf("readiness = %+v, want reviewer answer counted", got)
	}
}

func TestGroupDiscussionReadinessReadyWithExistingResult(t *testing.T) {
	t.Parallel()
	detail := a2a.HubDiscussionDetail{Discussion: a2a.HubDiscussionSummary{ID: "disc-1", Status: string(a2a.SessionDecided), ResultSummary: "Use staged rollout", ParticipantIDs: []string{"initiator", "expert-a"}}}
	got := groupDiscussionReadiness(detail)
	if !got.Ready || !got.HasResult {
		t.Fatalf("readiness = %+v, want ready existing result", got)
	}
}

func TestGroupDiscussionWorkflowStateSuggestsDecisionForSatisfiedProposal(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	session, err := a2a.NewSession("disc-1", "deployment", "pick rollout", []a2a.Participant{{ID: "owner"}, {ID: "expert-a"}, {ID: "expert-b"}}, a2a.PolicyMajority, now)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := session.AddProposal(a2a.Proposal{ID: "prop-1", AuthorID: "owner", Title: "Staged rollout", Content: "Ship behind gates", CreatedAt: now}); err != nil {
		t.Fatalf("AddProposal: %v", err)
	}
	if err := session.AddReview(a2a.Review{ID: "rev-1", ProposalID: "prop-1", ReviewerID: "expert-a", Position: a2a.ReviewApprove, CreatedAt: now}); err != nil {
		t.Fatalf("AddReview expert: %v", err)
	}
	if err := session.AddReview(a2a.Review{ID: "rev-2", ProposalID: "prop-1", ReviewerID: "expert-b", Position: a2a.ReviewApprove, CreatedAt: now}); err != nil {
		t.Fatalf("AddReview expert-b: %v", err)
	}
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: "disc-1", Topic: "deployment", Question: "Which rollout?", Status: string(a2a.SessionOpen), ParticipantIDs: []string{"owner", "expert-a", "expert-b"}},
		Session:    session,
		Proposals:  session.Proposals,
		Reviews:    session.Reviews,
	}

	got := groupDiscussionWorkflowState(detail)
	if got.DecidableProposalCount != 1 || got.SuggestedNextActionKind != "decide_policy_satisfied_proposal" {
		t.Fatalf("workflow state = %+v, want decidable proposal next action", got)
	}
	if !strings.Contains(got.NonExecutingBoundary, "read-only workflow state") || !strings.Contains(got.NonExecutingBoundary, "no proposal") {
		t.Fatalf("workflow state boundary = %q, want explicit non-executing boundary", got.NonExecutingBoundary)
	}
	if got.RecommendedFocusContext["consultation_id"] != "disc-1" ||
		got.RecommendedFocusContext["action_kind"] != "workflow_state" ||
		got.RecommendedFocusContext["suggested_next_action_kind"] != "decide_policy_satisfied_proposal" {
		t.Fatalf("workflow state focus context = %#v", got.RecommendedFocusContext)
	}
	if got.RecommendedToolCall == nil ||
		got.RecommendedToolCall.Args["action"] != "workflow_state" ||
		got.RecommendedToolCall.RecommendedFocusContext["consultation_id"] != "disc-1" ||
		got.RecommendedToolCall.DiscussionFocusContext["consultation_id"] != "disc-1" {
		t.Fatalf("workflow state recommended tool call = %#v", got.RecommendedToolCall)
	}
	if got.OpenProposalCount != 1 || len(got.Proposals) != 1 || !got.Proposals[0].PolicySatisfied || got.Proposals[0].ReviewSummary.Approvals != 2 {
		t.Fatalf("proposal workflow state = %+v", got.Proposals)
	}
	if got.EscalationRoute == nil || got.EscalationRoute.Suggested || got.EscalationRoute.Target != "human_owner" {
		t.Fatalf("workflow escalation route = %+v, want embedded non-suggested default route", got.EscalationRoute)
	}
	if got.RollbackReadiness != nil {
		t.Fatalf("workflow rollback readiness = %+v, want absent before decision rollback triggers", got.RollbackReadiness)
	}
	if got.ActionDraft == nil || got.ActionDraft.ActionKind != "record_decision" || got.ActionDraft.ProposalID != "prop-1" {
		t.Fatalf("workflow action draft = %+v, want embedded decision draft", got.ActionDraft)
	}
	if strings.Join(got.ActionDraft.TargetProposalIDs, ",") != "prop-1" {
		t.Fatalf("workflow action draft target proposals = %v, want prop-1", got.ActionDraft.TargetProposalIDs)
	}
}

func TestGroupDiscussionWorkflowStateExposesMissingParticipants(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	session, err := a2a.NewSession("disc-1", "deployment", "Which rollout?", []a2a.Participant{{ID: "initiator"}, {ID: "expert-a"}, {ID: "expert-b"}}, a2a.PolicyMajority, now)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := session.AddProposal(a2a.Proposal{ID: "prop-1", AuthorID: "initiator", Title: "Staged rollout", Content: "Ship behind gates", CreatedAt: now}); err != nil {
		t.Fatalf("AddProposal: %v", err)
	}
	if err := session.AddReview(a2a.Review{ID: "rev-1", ProposalID: "prop-1", ReviewerID: "expert-a", Position: a2a.ReviewApprove, CreatedAt: now}); err != nil {
		t.Fatalf("AddReview: %v", err)
	}
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: "disc-1", Topic: "deployment", Question: "Which rollout?", Status: string(a2a.SessionOpen), ParticipantIDs: []string{"initiator", "expert-a", "expert-b"}},
		Session:    session,
		Proposals:  session.Proposals,
		Reviews:    session.Reviews,
		Messages:   []a2a.Message{{ID: "msg-1", FromID: "expert-a", Kind: a2a.MessageAnswer, Content: "Prefer staged rollout."}},
	}

	got := groupDiscussionWorkflowState(detail)
	if strings.Join(got.MissingAnswerParticipants, ",") != "expert-b" {
		t.Fatalf("missing answers = %v, want expert-b", got.MissingAnswerParticipants)
	}
	if len(got.Proposals) != 1 {
		t.Fatalf("proposal workflow state = %+v, want one proposal", got.Proposals)
	}
	if strings.Join(got.Proposals[0].MissingReviewers, ",") != "expert-b" {
		t.Fatalf("missing reviewers = %v, want expert-b", got.Proposals[0].MissingReviewers)
	}
	if !hasGroupDiscussionWorkflowBlocker(got.WorkflowBlockers, "pending_proposal_reviews") {
		t.Fatalf("workflow blockers = %+v, want pending proposal reviews", got.WorkflowBlockers)
	}
	if !hasGroupDiscussionWorkflowBlocker(got.Proposals[0].Blockers, "missing_reviewers") {
		t.Fatalf("proposal blockers = %+v, want missing reviewers", got.Proposals[0].Blockers)
	}
}

func TestGroupDiscussionReviewSummaryDedupesReviewerAliasesWithoutSession(t *testing.T) {
	t.Parallel()
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: "disc-1", Status: string(a2a.SessionOpen), ParticipantIDs: []string{"owner", "machine-a", "ve-machine-a", "machine-b"}},
		Reviews: []a2a.Review{
			{ID: "rev-1", ProposalID: "prop-1", ReviewerID: "machine-a", Position: a2a.ReviewApprove},
			{ID: "rev-2", ProposalID: "prop-1", ReviewerID: "ve-machine-a", Position: a2a.ReviewConcern},
			{ID: "rev-3", ProposalID: "prop-1", ReviewerID: "machine-b", Position: a2a.ReviewApprove},
		},
	}

	summary := groupDiscussionReviewSummaryFor(detail, "prop-1")
	if summary.Approvals != 1 || summary.Concerns != 1 || summary.Rejections != 0 || summary.Abstains != 0 {
		t.Fatalf("review summary = %+v, want alias-deduped latest review counts", summary)
	}
	if strings.Join(summary.ReviewedBy, ",") != "machine-b,ve-machine-a" {
		t.Fatalf("reviewed_by = %v, want latest alias reviewer ids", summary.ReviewedBy)
	}
	if groupDiscussionProposalPolicySatisfied(detail, "prop-1", summary) {
		t.Fatal("concern from reviewer alias should block proposal policy")
	}

	detail.Reviews = append(detail.Reviews, a2a.Review{ID: "rev-4", ProposalID: "prop-1", ReviewerID: "ve_machine-a", Position: a2a.ReviewApprove})
	summary = groupDiscussionReviewSummaryFor(detail, "prop-1")
	if summary.Approvals != 2 || !groupDiscussionProposalPolicySatisfied(detail, "prop-1", summary) {
		t.Fatalf("alias approvals should satisfy deduped participant majority, summary=%+v", summary)
	}
}

func TestGroupDiscussionReadinessDedupesExpectedParticipantAliases(t *testing.T) {
	t.Parallel()
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: "disc-alias", Status: string(a2a.SessionOpen), ParticipantIDs: []string{"owner", "machine-a", "ve-machine-a", "machine-b"}},
		Session: &a2a.Session{Participants: []a2a.Participant{
			{ID: "owner", RoleCode: "initiator"},
			{ID: "machine-a", RoleCode: "speak"},
			{ID: "ve-machine-a", RoleCode: "speak"},
			{ID: "machine-b", RoleCode: "speak"},
		}},
		Messages: []a2a.Message{{ID: "msg-1", FromID: "machine/a", Kind: a2a.MessageAnswer, Content: "A"}},
	}

	readiness := groupDiscussionReadiness(detail)
	if readiness.ParticipantCount != 3 || readiness.AnswerCount != 1 || readiness.ExpectedAnswerCount != 2 || readiness.Ready {
		t.Fatalf("readiness = %+v, want one of two distinct expert answers", readiness)
	}

	detail.Messages = append(detail.Messages, a2a.Message{ID: "msg-2", FromID: "machine-b", Kind: a2a.MessageAnswer, Content: "B"})
	readiness = groupDiscussionReadiness(detail)
	if readiness.ParticipantCount != 3 || readiness.AnswerCount != 2 || readiness.ExpectedAnswerCount != 2 || !readiness.Ready {
		t.Fatalf("readiness after second expert = %+v, want ready with deduped participants", readiness)
	}
}

func TestGroupDiscussionEscalationRouteSuggestsOwnerForBlockingReviews(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	session, err := a2a.NewSession("disc-1", "deployment", "pick rollout", []a2a.Participant{{ID: "owner"}, {ID: "expert-a"}, {ID: "expert-b"}}, a2a.PolicyMajority, now)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := session.AddProposal(a2a.Proposal{ID: "prop-1", AuthorID: "owner", Title: "Staged rollout", Content: "Ship behind gates", CreatedAt: now}); err != nil {
		t.Fatalf("AddProposal: %v", err)
	}
	if err := session.AddReview(a2a.Review{ID: "review-1", ProposalID: "prop-1", ReviewerID: "expert-a", Position: a2a.ReviewConcern, Comment: "needs rollback trigger", CreatedAt: now}); err != nil {
		t.Fatalf("AddReview: %v", err)
	}
	detail := a2a.HubDiscussionDetail{
		Discussion:      a2a.HubDiscussionSummary{ID: session.ID, Status: string(session.Status), Topic: session.Topic, Question: session.Goal, ParticipantIDs: []string{"owner", "expert-a", "expert-b"}},
		Session:         session,
		Proposals:       session.Proposals,
		Reviews:         session.Reviews,
		ReviewSummaries: map[string]a2a.ReviewSummary{"prop-1": session.ReviewSummary("prop-1")},
	}

	got := groupDiscussionEscalationRouteSuggestion(detail)
	if !got.Suggested || got.Target != "release-owner" || !hasGroupDiscussionTrigger(got.Triggers, "blocking_reviews") || !hasGroupDiscussionTrigger(got.Triggers, "release_or_rollback_metadata") {
		t.Fatalf("escalation route = %+v, want release owner suggestion for blocking review", got)
	}
	if got.BlockingReviewCount != 1 || !strings.Contains(got.NonExecutingBoundary, "no escalation") || !strings.Contains(got.Reason, "release") {
		t.Fatalf("escalation route counts/boundary = %+v", got)
	}
	if !hasGroupDiscussionDraftEvidence(got.PolicyEvidence, "policy_target: release-owner") || !hasGroupDiscussionDraftEvidence(got.PolicyEvidence, "matched_keywords:") {
		t.Fatalf("escalation route policy evidence = %v, want release policy evidence", got.PolicyEvidence)
	}
	state := groupDiscussionWorkflowState(detail)
	if state.EscalationRoute == nil || !state.EscalationRoute.Suggested || !hasGroupDiscussionTrigger(state.EscalationRoute.Triggers, "blocking_reviews") {
		t.Fatalf("embedded workflow escalation route = %+v, want blocking review suggestion", state.EscalationRoute)
	}
	if !hasGroupDiscussionWorkflowBlocker(state.WorkflowBlockers, "blocking_reviews") || !hasGroupDiscussionWorkflowBlocker(state.Proposals[0].Blockers, "proposal_blocking_reviews") {
		t.Fatalf("workflow/proposal blockers = %+v / %+v, want blocking review blockers", state.WorkflowBlockers, state.Proposals[0].Blockers)
	}
	draft := groupDiscussionWorkflowActionDraft(detail, state)
	if draft.ActionKind != "prepare_escalation" || draft.EscalationTarget != "release-owner" || !draft.RequiresConfirmation {
		t.Fatalf("workflow action draft = %+v, want escalation draft requiring confirmation", draft)
	}
	if draft.SuggestedArguments["action"] != "escalate" || draft.SuggestedArguments["consultation_id"] != "disc-1" {
		t.Fatalf("workflow action draft args = %#v", draft.SuggestedArguments)
	}
	if draft.RecommendedFocusContext["consultation_id"] != "disc-1" || draft.RecommendedFocusContext["action_kind"] != "prepare_escalation" || draft.RecommendedFocusContext["escalation_target"] != "release-owner" {
		t.Fatalf("workflow action draft focus context = %#v", draft.RecommendedFocusContext)
	}
	if !hasGroupDiscussionDraftEvidence(draft.Evidence, "blocking_reviews:") ||
		!hasGroupDiscussionDraftEvidence(draft.Evidence, "proposal Staged rollout has concerns=1") ||
		!hasGroupDiscussionDraftEvidence(draft.Evidence, "policy_target: release-owner") {
		t.Fatalf("workflow action draft evidence = %v, want blocking review and policy evidence", draft.Evidence)
	}
	if !hasGroupDiscussionDraftEvidence(draft.RiskBoundaries, "explicit human confirmation") {
		t.Fatalf("workflow action draft boundaries = %v, want confirmation boundary", draft.RiskBoundaries)
	}
}

func TestGroupDiscussionEscalationRoutePolicyPrioritizesSecurityMetadata(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	session, err := a2a.NewSession("disc-1", "deployment", "roll back tenant auth release", []a2a.Participant{{ID: "owner"}, {ID: "expert-a"}}, a2a.PolicyMajority, now)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := session.AddProposal(a2a.Proposal{ID: "prop-1", AuthorID: "owner", Title: "Rollback auth rollout", Content: "Rollback if token scope is wrong", CreatedAt: now}); err != nil {
		t.Fatalf("AddProposal: %v", err)
	}
	if err := session.AddReview(a2a.Review{ID: "review-1", ProposalID: "prop-1", ReviewerID: "expert-a", Position: a2a.ReviewConcern, Comment: "secret exposure risk needs security owner", CreatedAt: now}); err != nil {
		t.Fatalf("AddReview: %v", err)
	}
	detail := a2a.HubDiscussionDetail{
		Discussion:      a2a.HubDiscussionSummary{ID: session.ID, Status: string(session.Status), Topic: session.Topic, Question: session.Goal, ParticipantIDs: []string{"owner", "expert-a"}},
		Session:         session,
		Proposals:       session.Proposals,
		Reviews:         session.Reviews,
		ReviewSummaries: map[string]a2a.ReviewSummary{"prop-1": session.ReviewSummary("prop-1")},
	}

	got := groupDiscussionEscalationRouteSuggestion(detail)
	if !got.Suggested || got.Target != "security-governance" {
		t.Fatalf("escalation route = %+v, want security-governance target", got)
	}
	if !hasGroupDiscussionTrigger(got.Triggers, "security_or_compliance_metadata") || !hasGroupDiscussionTrigger(got.Triggers, "blocking_reviews") {
		t.Fatalf("escalation route triggers = %v, want security metadata and blocking reviews", got.Triggers)
	}
	if !strings.Contains(got.Reason, "security") {
		t.Fatalf("escalation route reason = %q, want security policy context", got.Reason)
	}
	if !hasGroupDiscussionDraftEvidence(got.PolicyEvidence, "policy_target: security-governance") ||
		!hasGroupDiscussionDraftEvidence(got.PolicyEvidence, "matched_keywords:") {
		t.Fatalf("escalation route policy evidence = %v, want security policy evidence", got.PolicyEvidence)
	}
}

func TestGroupDiscussionWorkflowActionDraftSuggestsDecisionForSatisfiedProposal(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	session, err := a2a.NewSession("disc-1", "deployment", "pick rollout", []a2a.Participant{{ID: "owner"}, {ID: "expert-a"}, {ID: "expert-b"}}, a2a.PolicyMajority, now)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := session.AddProposal(a2a.Proposal{ID: "prop-1", AuthorID: "owner", Title: "Staged rollout", Content: "Ship behind gates", CreatedAt: now}); err != nil {
		t.Fatalf("AddProposal: %v", err)
	}
	if err := session.AddReview(a2a.Review{ID: "rev-1", ProposalID: "prop-1", ReviewerID: "expert-a", Position: a2a.ReviewApprove, CreatedAt: now}); err != nil {
		t.Fatalf("AddReview expert-a: %v", err)
	}
	if err := session.AddReview(a2a.Review{ID: "rev-2", ProposalID: "prop-1", ReviewerID: "expert-b", Position: a2a.ReviewApprove, CreatedAt: now}); err != nil {
		t.Fatalf("AddReview expert-b: %v", err)
	}
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: "disc-1", Topic: "deployment", Question: "Which rollout?", Status: string(a2a.SessionOpen), ParticipantIDs: []string{"owner", "expert-a", "expert-b"}},
		Session:    session,
		Proposals:  session.Proposals,
		Reviews:    session.Reviews,
	}

	draft := groupDiscussionWorkflowActionDraft(detail, groupDiscussionWorkflowState(detail))
	if draft.ActionKind != "record_decision" || draft.ProposalID != "prop-1" || !draft.RequiresConfirmation {
		t.Fatalf("workflow action draft = %+v, want decision draft for prop-1", draft)
	}
	if draft.SuggestedArguments["action"] != "decide" || draft.SuggestedArguments["proposal_id"] != "prop-1" {
		t.Fatalf("workflow action draft args = %#v", draft.SuggestedArguments)
	}
	if draft.RecommendedToolCall == nil || draft.RecommendedToolCall.Tool != "group_discussion" || !draft.RecommendedToolCall.NonExecuting {
		t.Fatalf("workflow action draft recommended call = %+v, want non-executing group_discussion call", draft.RecommendedToolCall)
	}
	if draft.RecommendedToolCall.Args["action"] != "workflow_action_draft" || draft.RecommendedToolCall.Args["consultation_id"] != "disc-1" {
		t.Fatalf("workflow action draft recommended args = %#v, want safe draft inspection call", draft.RecommendedToolCall.Args)
	}
	if draft.RecommendedFocusContext["proposal_id"] != "prop-1" || draft.RecommendedToolCall.RecommendedFocusContext["proposal_id"] != "prop-1" || draft.RecommendedToolCall.DiscussionFocusContext["proposal_id"] != "prop-1" {
		t.Fatalf("workflow action draft focus context = draft:%#v call:%#v", draft.RecommendedFocusContext, draft.RecommendedToolCall.DiscussionFocusContext)
	}
	if !strings.Contains(fmt.Sprint(draft.SuggestedArguments["rationale"]), "2 approval") {
		t.Fatalf("workflow action draft rationale = %#v, want review-backed draft rationale", draft.SuggestedArguments["rationale"])
	}
	rollback, ok := draft.SuggestedArguments["rollback_on"].([]string)
	if !ok || len(rollback) == 0 || !strings.Contains(rollback[0], "Staged rollout") {
		t.Fatalf("workflow action draft rollback = %#v, want conservative rollback draft", draft.SuggestedArguments["rollback_on"])
	}
	if !hasGroupDiscussionDraftEvidence(draft.Evidence, "decision_policy: satisfied") ||
		!hasGroupDiscussionDraftEvidence(draft.Evidence, "reviews: approvals=2") {
		t.Fatalf("workflow action draft evidence = %v, want decision policy and review evidence", draft.Evidence)
	}
	if !hasGroupDiscussionDraftEvidence(draft.RiskBoundaries, "advisory only") {
		t.Fatalf("workflow action draft boundaries = %v, want advisory boundary", draft.RiskBoundaries)
	}
}

func TestGroupDiscussionWorkflowActionDraftBuildsTargetedFollowup(t *testing.T) {
	t.Parallel()
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: "disc-1", Topic: "deployment", Question: "Which rollout?", Status: string(a2a.SessionOpen), ParticipantIDs: []string{"initiator", "expert-a", "expert-b"}},
		Messages:   []a2a.Message{{ID: "m1", FromID: "expert-a", Kind: a2a.MessageAnswer, Content: "Prefer staged rollout."}},
	}

	draft := groupDiscussionWorkflowActionDraft(detail, groupDiscussionWorkflowState(detail))
	if draft.ActionKind != "send_followup" || !draft.RequiresConfirmation {
		t.Fatalf("workflow action draft = %+v, want follow-up draft requiring confirmation", draft)
	}
	if draft.SuggestedArguments["action"] != "send_message" || draft.SuggestedArguments["message_kind"] != "question" {
		t.Fatalf("workflow action draft args = %#v", draft.SuggestedArguments)
	}
	if strings.Join(draft.TargetParticipants, ",") != "expert-b" {
		t.Fatalf("workflow action draft target participants = %v, want expert-b", draft.TargetParticipants)
	}
	if got := fmt.Sprint(draft.RecommendedFocusContext["target_participants"]); !strings.Contains(got, "expert-b") {
		t.Fatalf("workflow action draft focus target participants = %#v", draft.RecommendedFocusContext)
	}
	content := fmt.Sprint(draft.SuggestedArguments["content"])
	if !strings.Contains(content, "expert-b") || !strings.Contains(content, "Which rollout?") {
		t.Fatalf("workflow action draft content = %q, want missing expert and question context", content)
	}
	if !hasGroupDiscussionDraftEvidence(draft.Evidence, "missing_expected_answers: expert-b") {
		t.Fatalf("workflow action draft evidence = %v, want missing expert evidence", draft.Evidence)
	}
}

func TestGroupDiscussionWorkflowActionDraftBuildsInitialAnswerReminder(t *testing.T) {
	t.Parallel()
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: "disc-1", Topic: "deployment", Question: "Which rollout?", Status: string(a2a.SessionOpen), ParticipantIDs: []string{"initiator", "expert-a", "expert-b"}},
	}

	draft := groupDiscussionWorkflowActionDraft(detail, groupDiscussionWorkflowState(detail))
	if draft.ActionKind != "wait_for_answers" || !draft.RequiresConfirmation {
		t.Fatalf("workflow action draft = %+v, want answer reminder requiring confirmation", draft)
	}
	if draft.SuggestedArguments["action"] != "send_message" || draft.SuggestedArguments["message_kind"] != "question" {
		t.Fatalf("workflow action draft args = %#v", draft.SuggestedArguments)
	}
	if strings.Join(draft.TargetParticipants, ",") != "expert-a,expert-b" {
		t.Fatalf("workflow action draft target participants = %v, want missing experts", draft.TargetParticipants)
	}
	content := fmt.Sprint(draft.SuggestedArguments["content"])
	if !strings.Contains(content, "expert-a") || !strings.Contains(content, "expert-b") || !strings.Contains(content, "Which rollout?") {
		t.Fatalf("workflow action draft content = %q, want missing experts and question context", content)
	}
	if !hasGroupDiscussionDraftEvidence(draft.Evidence, "missing_expected_answers: expert-a, expert-b") ||
		!hasGroupDiscussionDraftEvidence(draft.Evidence, "answers: 0/2") {
		t.Fatalf("workflow action draft evidence = %v, want initial-answer evidence", draft.Evidence)
	}
}

func TestGroupDiscussionWorkflowActionDraftBuildsProposalReviewRequest(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	session, err := a2a.NewSession("disc-1", "deployment", "Which rollout?", []a2a.Participant{{ID: "owner"}, {ID: "expert-a"}, {ID: "expert-b"}}, a2a.PolicyMajority, now)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := session.AddProposal(a2a.Proposal{ID: "prop-1", AuthorID: "owner", Title: "Staged rollout", Content: "Ship behind gates", CreatedAt: now}); err != nil {
		t.Fatalf("AddProposal: %v", err)
	}
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: "disc-1", Topic: "deployment", Question: "Which rollout?", Status: string(a2a.SessionOpen), ParticipantIDs: []string{"owner", "expert-a", "expert-b"}},
		Session:    session,
		Proposals:  session.Proposals,
		Reviews:    session.Reviews,
	}

	draft := groupDiscussionWorkflowActionDraft(detail, groupDiscussionWorkflowState(detail))
	if draft.ActionKind != "collect_reviews" || !draft.RequiresConfirmation {
		t.Fatalf("workflow action draft = %+v, want review collection draft requiring confirmation", draft)
	}
	if draft.SuggestedArguments["action"] != "send_message" || draft.SuggestedArguments["message_kind"] != "question" {
		t.Fatalf("workflow action draft args = %#v", draft.SuggestedArguments)
	}
	if strings.Join(draft.TargetParticipants, ",") != "expert-a,expert-b" || strings.Join(draft.TargetProposalIDs, ",") != "prop-1" {
		t.Fatalf("workflow action draft targets participants=%v proposals=%v", draft.TargetParticipants, draft.TargetProposalIDs)
	}
	if got := fmt.Sprint(draft.RecommendedFocusContext["target_proposal_ids"]); !strings.Contains(got, "prop-1") {
		t.Fatalf("workflow action draft focus target proposals = %#v", draft.RecommendedFocusContext)
	}
	content := fmt.Sprint(draft.SuggestedArguments["content"])
	if !strings.Contains(content, "Staged rollout") || !strings.Contains(content, "expert-a") || !strings.Contains(content, "expert-b") {
		t.Fatalf("workflow action draft content = %q, want proposal title and missing reviewers", content)
	}
	if !hasGroupDiscussionDraftEvidence(draft.Evidence, "open_proposals: 1") ||
		!hasGroupDiscussionDraftEvidence(draft.Evidence, "missing_reviewers=expert-a, expert-b") {
		t.Fatalf("workflow action draft evidence = %v, want review coverage evidence", draft.Evidence)
	}
}

func TestGroupDiscussionWorkflowActionDraftReviewsExistingDecision(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	session, err := a2a.NewSession("disc-1", "deployment", "pick rollout", []a2a.Participant{{ID: "owner"}, {ID: "expert-a"}, {ID: "expert-b"}}, a2a.PolicyMajority, now)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := session.AddProposal(a2a.Proposal{ID: "prop-1", AuthorID: "owner", Title: "Staged rollout", Content: "Ship behind gates", CreatedAt: now}); err != nil {
		t.Fatalf("AddProposal: %v", err)
	}
	if err := session.AddReview(a2a.Review{ID: "rev-1", ProposalID: "prop-1", ReviewerID: "expert-a", Position: a2a.ReviewApprove, CreatedAt: now}); err != nil {
		t.Fatalf("AddReview expert-a: %v", err)
	}
	if err := session.AddReview(a2a.Review{ID: "rev-2", ProposalID: "prop-1", ReviewerID: "expert-b", Position: a2a.ReviewApprove, CreatedAt: now}); err != nil {
		t.Fatalf("AddReview expert-b: %v", err)
	}
	decision, err := session.TryDecide("decision-1", "prop-1", "Ship staged rollout", now)
	if err != nil {
		t.Fatalf("TryDecide: %v", err)
	}
	decision.Rationale = "reviews satisfied"
	decision.RollbackOn = []string{"error budget burn", "manual owner veto"}
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: "disc-1", Topic: "deployment", Question: "Which rollout?", Status: string(a2a.SessionDecided), ResultSummary: "Ship staged rollout", ParticipantIDs: []string{"owner", "expert-a", "expert-b"}},
		Session:    session,
		Proposals:  session.Proposals,
		Reviews:    session.Reviews,
	}

	state := groupDiscussionWorkflowState(detail)
	if !state.HasDecision || state.Decision == nil {
		t.Fatalf("workflow state decision = %+v, want session decision recognized", state.Decision)
	}
	draft := groupDiscussionWorkflowActionDraft(detail, state)
	if draft.ActionKind != "review_result_reuse" || draft.RequiresConfirmation {
		t.Fatalf("workflow action draft = %+v, want non-executing result review", draft)
	}
	if draft.SuggestedArguments["action"] != "get_detail" {
		t.Fatalf("workflow action draft args = %#v", draft.SuggestedArguments)
	}
	if !hasGroupDiscussionDraftEvidence(draft.Evidence, "decision_summary: Ship staged rollout") ||
		!hasGroupDiscussionDraftEvidence(draft.Evidence, "rollback_on: error budget burn") {
		t.Fatalf("workflow action draft evidence = %v, want decision and rollback evidence", draft.Evidence)
	}
	if state.RollbackReadiness == nil || !state.RollbackReadiness.HasDecision || strings.Join(state.RollbackReadiness.RollbackOn, ",") != "error budget burn,manual owner veto" {
		t.Fatalf("workflow rollback readiness = %+v, want decision rollback triggers", state.RollbackReadiness)
	}
	if draft.RecommendedToolCall == nil || draft.RecommendedToolCall.Args["action"] != "rollback_readiness" {
		t.Fatalf("workflow action draft recommended call = %+v, want safe rollback_readiness inspection", draft.RecommendedToolCall)
	}
	if draft.RecommendedFocusContext["action_kind"] != "review_result_reuse" || draft.RecommendedToolCall.DiscussionFocusContext["consultation_id"] != "disc-1" {
		t.Fatalf("workflow action draft focus context = draft:%#v call:%#v", draft.RecommendedFocusContext, draft.RecommendedToolCall.DiscussionFocusContext)
	}
}

func TestGroupDiscussionRollbackReadinessMatchesEvidence(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	session, err := a2a.NewSession("disc-1", "deployment", "pick rollout", []a2a.Participant{{ID: "owner"}, {ID: "expert-a"}, {ID: "expert-b"}}, a2a.PolicyMajority, now)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := session.AddProposal(a2a.Proposal{ID: "prop-1", AuthorID: "owner", Title: "Staged rollout", Content: "Ship behind gates", CreatedAt: now}); err != nil {
		t.Fatalf("AddProposal: %v", err)
	}
	if err := session.AddReview(a2a.Review{ID: "rev-1", ProposalID: "prop-1", ReviewerID: "expert-a", Position: a2a.ReviewApprove, CreatedAt: now}); err != nil {
		t.Fatalf("AddReview expert-a: %v", err)
	}
	if err := session.AddReview(a2a.Review{ID: "rev-2", ProposalID: "prop-1", ReviewerID: "expert-b", Position: a2a.ReviewApprove, CreatedAt: now}); err != nil {
		t.Fatalf("AddReview expert-b: %v", err)
	}
	decision, err := session.TryDecide("decision-1", "prop-1", "Ship staged rollout", now)
	if err != nil {
		t.Fatalf("TryDecide: %v", err)
	}
	decision.RollbackOn = []string{"error budget burn", "manual owner veto"}
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: "disc-1", Topic: "deployment", Question: "Which rollout?", Status: string(a2a.SessionDecided), ParticipantIDs: []string{"owner", "expert-a", "expert-b"}},
		Session:    session,
		Messages:   []a2a.Message{{ID: "msg-1", FromID: "expert-a", Kind: a2a.MessageEvidence, Content: "SLO report shows error budget burn after rollout.", CreatedAt: now}},
	}

	got := groupDiscussionRollbackReadiness(detail, "")
	if !got.HasDecision || !got.Suggested || got.SuggestedNextActionKind != "prepare_owner_approved_rollback_review" {
		t.Fatalf("rollback readiness = %+v, want matched rollback review suggestion", got)
	}
	if strings.Join(got.MatchedTriggers, ",") != "error budget burn" || strings.Join(got.UnmatchedTriggers, ",") != "manual owner veto" {
		t.Fatalf("rollback readiness triggers matched=%v unmatched=%v", got.MatchedTriggers, got.UnmatchedTriggers)
	}
	if got.RecommendedToolCall == nil || got.RecommendedToolCall.Args["action"] != "rollback_readiness" || !strings.Contains(got.NonExecutingBoundary, "no rollback executed") {
		t.Fatalf("rollback readiness tool/boundary = %+v / %q", got.RecommendedToolCall, got.NonExecutingBoundary)
	}
	if got.RecommendedFocusContext["action_kind"] != "rollback_readiness" ||
		got.RecommendedFocusContext["proposal_id"] != "prop-1" ||
		got.RecommendedToolCall.DiscussionFocusContext["consultation_id"] != "disc-1" {
		t.Fatalf("rollback readiness focus context = readiness:%#v call:%#v", got.RecommendedFocusContext, got.RecommendedToolCall.DiscussionFocusContext)
	}
	if matched, ok := got.RecommendedToolCall.DiscussionFocusContext["matched_triggers"].([]string); !ok || strings.Join(matched, ",") != "error budget burn" {
		t.Fatalf("rollback readiness tool focus matched triggers = %#v", got.RecommendedToolCall.DiscussionFocusContext)
	}
	state := groupDiscussionWorkflowState(detail)
	if state.RollbackReadiness == nil || !state.RollbackReadiness.Suggested {
		t.Fatalf("workflow rollback readiness = %+v, want embedded suggested rollback readiness", state.RollbackReadiness)
	}
	draft := groupDiscussionWorkflowActionDraft(detail, state)
	if draft.ActionKind != "prepare_rollback_review" || draft.RequiresConfirmation {
		t.Fatalf("workflow action draft = %+v, want non-executing rollback review draft", draft)
	}
	if draft.SuggestedArguments["action"] != "rollback_readiness" {
		t.Fatalf("workflow action draft args = %#v, want rollback_readiness action", draft.SuggestedArguments)
	}
	if draft.RecommendedToolCall == nil || draft.RecommendedToolCall.Args["action"] != "rollback_readiness" {
		t.Fatalf("workflow action draft recommended call = %+v, want rollback_readiness inspection", draft.RecommendedToolCall)
	}
	if draft.RecommendedFocusContext["action_kind"] != "prepare_rollback_review" || draft.RecommendedToolCall.DiscussionFocusContext["action_kind"] != "prepare_rollback_review" {
		t.Fatalf("workflow rollback draft focus context = draft:%#v call:%#v", draft.RecommendedFocusContext, draft.RecommendedToolCall.DiscussionFocusContext)
	}
	if !hasGroupDiscussionDraftEvidence(draft.Evidence, "matched_rollback_review: error budget burn") {
		t.Fatalf("workflow action draft evidence = %v, want matched rollback review evidence", draft.Evidence)
	}
}

func TestGroupDiscussionWorkflowActionDraftReviewsExistingEscalation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	session, err := a2a.NewSession("disc-1", "deployment", "pick rollout", []a2a.Participant{{ID: "owner"}, {ID: "expert-a"}}, a2a.PolicyMajority, now)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := session.Escalate(a2a.Escalation{ID: "esc-1", RaisedBy: "owner", Reason: "needs rollout owner", Target: "human_owner", CreatedAt: now}); err != nil {
		t.Fatalf("Escalate: %v", err)
	}
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: "disc-1", Topic: "deployment", Question: "Which rollout?", Status: string(a2a.SessionEscalated), ParticipantIDs: []string{"owner", "expert-a"}},
		Session:    session,
	}

	draft := groupDiscussionWorkflowActionDraft(detail, groupDiscussionWorkflowState(detail))
	if draft.ActionKind != "review_escalation_handoff" || draft.RequiresConfirmation {
		t.Fatalf("workflow action draft = %+v, want non-executing escalation handoff review", draft)
	}
	if !hasGroupDiscussionDraftEvidence(draft.Evidence, "escalation_target: human_owner") ||
		!hasGroupDiscussionDraftEvidence(draft.Evidence, "escalation_reason: needs rollout owner") {
		t.Fatalf("workflow action draft evidence = %v, want escalation evidence", draft.Evidence)
	}
	if hasGroupDiscussionDraftEvidence(draft.RiskBoundaries, "explicit human confirmation") {
		t.Fatalf("workflow action draft boundaries = %v, want read-only boundaries only", draft.RiskBoundaries)
	}
}

func TestGroupDiscussionEscalationRouteDoesNotSuggestWhenDecisionExists(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	decision := &a2a.Decision{ID: "decision-1", SessionID: "disc-1", ProposalID: "prop-1", Summary: "Ship staged rollout", Rationale: "reviews satisfied", CreatedAt: now}
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: "disc-1", Status: string(a2a.SessionDecided), Topic: "deployment", Question: "pick rollout", ParticipantIDs: []string{"owner", "expert-a"}},
		Decision:   decision,
	}

	got := groupDiscussionEscalationRouteSuggestion(detail)
	if got.Suggested || !hasGroupDiscussionTrigger(got.Triggers, "result_available") {
		t.Fatalf("escalation route = %+v, want no suggestion after decision", got)
	}
	if got.Target != "human_owner" || !strings.Contains(got.Reason, "decision") {
		t.Fatalf("escalation route target/reason = %+v", got)
	}
	if got.RecommendedFocusContext["consultation_id"] != "disc-1" ||
		got.RecommendedFocusContext["action_kind"] != "review_escalation_route" ||
		got.RecommendedFocusContext["escalation_target"] != "human_owner" {
		t.Fatalf("escalation route focus context = %#v", got.RecommendedFocusContext)
	}
	if got.RecommendedToolCall == nil ||
		got.RecommendedToolCall.Args["action"] != "escalation_route" ||
		got.RecommendedToolCall.DiscussionFocusContext["action_kind"] != "review_escalation_route" {
		t.Fatalf("escalation route recommended tool call = %#v", got.RecommendedToolCall)
	}
}

func TestGroupDiscussionEscalationRouteFocusContextForBlockingReviews(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	session, err := a2a.NewSession("disc-1", "deployment", "pick rollout", []a2a.Participant{{ID: "owner"}, {ID: "expert-a"}, {ID: "expert-b"}}, a2a.PolicyMajority, now)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := session.AddProposal(a2a.Proposal{ID: "prop-1", AuthorID: "owner", Title: "Staged rollout", Content: "Ship behind gates", CreatedAt: now}); err != nil {
		t.Fatalf("AddProposal: %v", err)
	}
	if err := session.AddReview(a2a.Review{ID: "rev-1", ProposalID: "prop-1", ReviewerID: "expert-a", Position: a2a.ReviewReject, Comment: "requires security owner escalation", CreatedAt: now}); err != nil {
		t.Fatalf("AddReview expert-a: %v", err)
	}
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: "disc-1", Topic: "deployment", Question: "Which rollout?", Status: string(a2a.SessionOpen), ParticipantIDs: []string{"owner", "expert-a", "expert-b"}},
		Session:    session,
		Proposals:  session.Proposals,
		Reviews:    session.Reviews,
	}

	got := groupDiscussionEscalationRouteSuggestion(detail)
	if !got.Suggested || got.RecommendedFocusContext["action_kind"] != "prepare_escalation" {
		t.Fatalf("escalation route = %+v, want prepare escalation focus", got)
	}
	if got.RecommendedToolCall == nil ||
		got.RecommendedToolCall.Args["action"] != "escalation_route" ||
		got.RecommendedToolCall.DiscussionFocusContext["action_kind"] != "prepare_escalation" {
		t.Fatalf("escalation route recommended tool call = %#v", got.RecommendedToolCall)
	}
	if got.RecommendedFocusContext["consultation_id"] != "disc-1" || got.RecommendedFocusContext["escalation_target"] == "" {
		t.Fatalf("escalation route focus context = %#v", got.RecommendedFocusContext)
	}
	triggers, ok := got.RecommendedFocusContext["triggers"].([]string)
	if !ok || !hasGroupDiscussionTrigger(triggers, "blocking_reviews") {
		t.Fatalf("escalation route focus triggers = %#v", got.RecommendedFocusContext)
	}
}

func TestStaleGroupDiscussions(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	items := []a2a.HubDiscussionSummary{
		{ID: "old-open", Status: string(a2a.SessionOpen), CreatedAt: now.Add(-10 * time.Minute)},
		{ID: "fresh-open", Status: string(a2a.SessionOpen), CreatedAt: now.Add(-2 * time.Minute)},
		{ID: "old-decided", Status: string(a2a.SessionDecided), CreatedAt: now.Add(-20 * time.Minute)},
	}
	got := staleGroupDiscussions(items, 300, now)
	if len(got) != 1 || got[0].ID != "old-open" {
		t.Fatalf("stale = %+v, want only old-open", got)
	}
}

func TestStaleGroupDiscussionsUsesUpdatedAtWhenCreatedAtMissing(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	items := []a2a.HubDiscussionSummary{{ID: "old-open", Status: string(a2a.SessionOpen), UpdatedAt: now.Add(-10 * time.Minute)}}
	got := staleGroupDiscussions(items, 300, now)
	if len(got) != 1 || got[0].ID != "old-open" {
		t.Fatalf("stale = %+v, want old-open", got)
	}
}

func TestShouldUseLayeredGroupDiscussionSummaryForManyAnswers(t *testing.T) {
	t.Parallel()
	detail := a2a.HubDiscussionDetail{Discussion: a2a.HubDiscussionSummary{Topic: "T", Question: "Q"}}
	for i := 0; i < groupDiscussionLayeredAnswerThreshold+1; i++ {
		detail.Messages = append(detail.Messages, a2a.Message{ID: "m", FromID: "expert", Kind: a2a.MessageAnswer, Content: "answer"})
	}
	if !shouldUseLayeredGroupDiscussionSummary(detail) {
		t.Fatal("expected layered summary for many answers")
	}
}

func TestBuildGroupDiscussionSummaryShardsGroupsByParticipantAndKind(t *testing.T) {
	t.Parallel()
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{Topic: "T", Question: "Q"},
		Messages: []a2a.Message{
			{ID: "m1", FromID: "expert-a", Kind: a2a.MessageAnswer, Content: "Use staged rollout."},
			{ID: "m2", FromID: "expert-a", Kind: a2a.MessageObjection, Content: "Migration risk remains."},
			{ID: "m3", FromID: "expert-b", Kind: a2a.MessageEvidence, Content: "Prior deploy failed without rollback."},
		},
	}
	shards := buildGroupDiscussionSummaryShards(detail, 3, 2000)
	if len(shards) != 3 {
		t.Fatalf("shards = %+v, want 3 participant/kind shards", shards)
	}
	labels := strings.Join([]string{shards[0].Label, shards[1].Label, shards[2].Label}, "|")
	for _, want := range []string{"expert-a/answer", "expert-a/objection", "expert-b/evidence"} {
		if !strings.Contains(labels, want) {
			t.Fatalf("labels %q missing %q", labels, want)
		}
	}
	fallback := fallbackGroupDiscussionShardSummary(shards[1])
	if len(fallback.Risks) == 0 || len(fallback.Disagreements) == 0 {
		t.Fatalf("objection fallback should preserve risk/disagreement: %+v", fallback)
	}
}

func TestDecodeGroupDiscussionResultSummaryExtendedFields(t *testing.T) {
	t.Parallel()
	got, err := decodeGroupDiscussionResultSummary(`{"summary":"Use staged rollout","rationale":"A wins with caveats","risks":["Migration risk","Migration risk"],"disagreements":["B wants full rewrite"],"open_questions":["Who owns rollback?"],"participant_contributions":{"expert-a":"staged rollout"},"confidence":1.4}`)
	if err != nil {
		t.Fatalf("decode returned error: %v", err)
	}
	if len(got.Risks) != 1 || len(got.Disagreements) != 1 || len(got.OpenQuestions) != 1 {
		t.Fatalf("extended slices not normalized: %+v", got)
	}
	if got.ParticipantContributions["expert-a"] == "" || got.Confidence != 1 {
		t.Fatalf("extended fields = %+v", got)
	}
}

func TestSelectGroupDiscussionInvitees_UsesContributionScore(t *testing.T) {
	t.Parallel()
	cfg := corelib.AppConfig{RemoteMachineID: "local"}
	experts := []a2a.GroupProfile{
		{AgentID: "low", Discoverable: true, Available: true, ContributionScore: 0.1, ContributionEvidence: 12},
		{AgentID: "high", Discoverable: true, Available: true, ContributionScore: 0.95, ContributionEvidence: 12},
	}
	got := selectGroupDiscussionInvitees(experts, cfg, a2a.GroupConsultationRequest{})
	if len(got) < 2 || got[0] != "high" {
		t.Fatalf("selected %v, want high contribution expert first", got)
	}
}

func TestSelectGroupDiscussionInvitees_IgnoresContributionScoreWhenDisabled(t *testing.T) {
	t.Parallel()
	disabled := false
	cfg := corelib.AppConfig{RemoteMachineID: "local"}
	cfg.GroupDiscussion.UseCrossAgentExperience = &disabled
	experts := []a2a.GroupProfile{
		{AgentID: "low", Discoverable: true, Available: true, ContributionScore: 0.1, ContributionEvidence: 12},
		{AgentID: "high", Discoverable: true, Available: true, ContributionScore: 0.95, ContributionEvidence: 12},
	}
	got := selectGroupDiscussionInvitees(experts, cfg, a2a.GroupConsultationRequest{})
	if len(got) < 2 || got[0] != "low" {
		t.Fatalf("selected %v, want input order when cross-agent experience is disabled", got)
	}
}

func TestRankGroupDiscussionExpertsExplainsSelectedSignals(t *testing.T) {
	t.Parallel()
	cfg := corelib.AppConfig{RemoteMachineID: "local"}
	cfg.GroupDiscussion.SecurityGroupID = "team-a"
	experts := []a2a.GroupProfile{
		{AgentID: "low", DisplayName: "Low", SecurityGroupID: "team-a", Discoverable: true, Available: true, Skills: []string{"docs"}},
		{AgentID: "high", DisplayName: "High", SecurityGroupID: "team-a", Discoverable: true, Available: true, Skills: []string{"go", "security"}, Description: "reviews backend plans", ContributionScore: 0.95, ContributionEvidence: 12},
	}
	req := a2a.GroupConsultationRequest{Topic: "deployment", Question: "Which path?", SkillsWanted: []string{"go"}, RiskLevel: "high"}
	got := rankGroupDiscussionExperts(experts, cfg, req, 1)
	if len(got.InviteeIDs) != 1 || got.InviteeIDs[0] != "high" {
		t.Fatalf("invitees = %v, want high", got.InviteeIDs)
	}
	if !got.UseCrossAgentExperience || !strings.Contains(got.NonExecutingBoundary, "no discussion") {
		t.Fatalf("ranking boundary/cross-agent flags = %+v", got)
	}
	if len(got.Ranked) != 2 || got.Ranked[0].AgentID != "high" || !got.Ranked[0].Selected {
		t.Fatalf("ranked = %+v, want high selected first", got.Ranked)
	}
	if !hasGroupDiscussionRankReason(got.Ranked[0].Reasons, "same_security_group:+4") ||
		!hasGroupDiscussionRankReason(got.Ranked[0].Reasons, "skill:go:+3") ||
		!hasGroupDiscussionRankReason(got.Ranked[0].Reasons, "contribution_score:+") {
		t.Fatalf("reasons = %v, want security, skill, and contribution reasons", got.Ranked[0].Reasons)
	}
	if len(got.Ranked[0].MatchedSkills) != 1 || got.Ranked[0].MatchedSkills[0] != "go" {
		t.Fatalf("matched skills = %v, want go", got.Ranked[0].MatchedSkills)
	}
	if got.RecommendedFocusContext["action_kind"] != "rank_experts" || fmt.Sprint(got.RecommendedFocusContext["selected_invitee_ids"]) == "" || got.RecommendedFocusContext["topic"] != "deployment" {
		t.Fatalf("ranking focus context = %#v", got.RecommendedFocusContext)
	}
	if got.RecommendedToolCall == nil ||
		got.RecommendedToolCall.Tool != "group_discussion" ||
		!got.RecommendedToolCall.NonExecuting ||
		got.RecommendedToolCall.Args["action"] != "rank_experts" ||
		got.RecommendedToolCall.Args["topic"] != "deployment" ||
		got.RecommendedToolCall.RecommendedFocusContext["action_kind"] != "rank_experts" ||
		got.RecommendedToolCall.DiscussionFocusContext["action_kind"] != "rank_experts" {
		t.Fatalf("ranking recommended tool call = %#v", got.RecommendedToolCall)
	}
	if _, hasLimitArg := got.RecommendedToolCall.Args["limit"]; hasLimitArg {
		t.Fatalf("ranking recommended tool call should not include ignored limit arg: %#v", got.RecommendedToolCall.Args)
	}
}

func TestRankGroupDiscussionExpertsIgnoresContributionWhenDisabled(t *testing.T) {
	t.Parallel()
	disabled := false
	cfg := corelib.AppConfig{RemoteMachineID: "local"}
	cfg.GroupDiscussion.UseCrossAgentExperience = &disabled
	experts := []a2a.GroupProfile{
		{AgentID: "low", Discoverable: true, Available: true, ContributionScore: 0.1, ContributionEvidence: 12},
		{AgentID: "high", Discoverable: true, Available: true, ContributionScore: 0.95, ContributionEvidence: 12},
	}
	got := rankGroupDiscussionExperts(experts, cfg, a2a.GroupConsultationRequest{}, 2)
	if got.UseCrossAgentExperience {
		t.Fatal("ranking should report disabled cross-agent experience")
	}
	if len(got.Ranked) < 2 || got.Ranked[0].AgentID != "low" || got.Ranked[1].AgentID != "high" {
		t.Fatalf("ranked = %+v, want stable input order without contribution score", got.Ranked)
	}
	for _, rank := range got.Ranked {
		if hasGroupDiscussionRankReason(rank.Reasons, "contribution_score:+") {
			t.Fatalf("rank %s unexpectedly has contribution reason: %v", rank.AgentID, rank.Reasons)
		}
	}
}

func TestGroupDiscussionContributionScoreBonusRequiresEvidence(t *testing.T) {
	t.Parallel()
	if got := groupDiscussionContributionScoreBonus(a2a.GroupProfile{ContributionScore: 1, ContributionEvidence: 1}); got != 0 {
		t.Fatalf("bonus = %d, want 0 without enough evidence", got)
	}
	if got := groupDiscussionContributionScoreBonus(a2a.GroupProfile{ContributionScore: 0.95, ContributionEvidence: 8}); got <= 0 || got > 3 {
		t.Fatalf("bonus = %d, want bounded positive", got)
	}
}

func TestGroupDiscussionContributionQualityRewardsEvidence(t *testing.T) {
	t.Parallel()
	detail := a2a.HubDiscussionDetail{Messages: []a2a.Message{
		{ID: "m1", FromID: "local", Kind: a2a.MessageAnswer, Content: "Use staged rollout."},
		{ID: "m2", FromID: "local", Kind: a2a.MessageEvidence, Content: "Prior deploy failed without rollback."},
	}}
	result := GroupDiscussionSummarizeResult{
		AnswerCount:              2,
		ParticipantContributions: map[string]string{"local": "staged rollout with evidence"},
		Risks:                    []string{"rollback risk"},
		Confidence:               0.8,
	}
	got := groupDiscussionContributionQuality(detail, result, "local")
	if got <= 0.8 || got > 1 {
		t.Fatalf("quality = %.3f, want high bounded score", got)
	}
	if missing := groupDiscussionContributionQuality(detail, result, "other"); missing != 0 {
		t.Fatalf("missing participant quality = %.3f, want 0", missing)
	}
}

func TestMissingGroupDiscussionAnswerParticipantsMatchesGeneratedVEAliases(t *testing.T) {
	t.Parallel()
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ParticipantIDs: []string{"human", "machine-anna", "machine-xiaoyan"}},
		Messages: []a2a.Message{
			{ID: "m1", FromID: "ve-machine-anna", Kind: a2a.MessageAnswer, Content: "Anna answer"},
			{ID: "m2", FromID: "ve_machine-xiaoyan", Kind: a2a.MessageAnswer, Content: "Xiaoyan answer"},
		},
	}
	if got := missingGroupDiscussionAnswerParticipants(detail); len(got) != 0 {
		t.Fatalf("missing = %v, want none after alias-matched answers", got)
	}
}

func TestMissingGroupDiscussionProposalReviewersMatchesGeneratedVEAliases(t *testing.T) {
	t.Parallel()
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ParticipantIDs: []string{"human", "machine-anna", "machine-xiaoyan"}},
	}
	proposal := GroupDiscussionProposalWorkflowState{
		AuthorID: "ve-machine-anna",
		ReviewSummary: a2a.ReviewSummary{
			ReviewedBy: []string{"ve_machine-xiaoyan"},
		},
	}
	if got := missingGroupDiscussionProposalReviewers(detail, proposal); len(got) != 1 || got[0] != "human" {
		t.Fatalf("missing reviewers = %v, want only human", got)
	}
}

func hasGroupDiscussionRankReason(reasons []string, prefix string) bool {
	for _, reason := range reasons {
		if strings.HasPrefix(reason, prefix) {
			return true
		}
	}
	return false
}

func hasGroupDiscussionTrigger(triggers []string, target string) bool {
	for _, trigger := range triggers {
		if trigger == target {
			return true
		}
	}
	return false
}

func hasGroupDiscussionWorkflowBlocker(blockers []GroupDiscussionWorkflowBlocker, code string) bool {
	for _, blocker := range blockers {
		if blocker.Code == code {
			return true
		}
	}
	return false
}

func hasGroupDiscussionDraftEvidence(values []string, fragment string) bool {
	for _, value := range values {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}
