package main

import (
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
}

func TestGroupDiscussionReadinessReadyWithExistingResult(t *testing.T) {
	t.Parallel()
	detail := a2a.HubDiscussionDetail{Discussion: a2a.HubDiscussionSummary{ID: "disc-1", Status: string(a2a.SessionDecided), ResultSummary: "Use staged rollout", ParticipantIDs: []string{"initiator", "expert-a"}}}
	got := groupDiscussionReadiness(detail)
	if !got.Ready || !got.HasResult {
		t.Fatalf("readiness = %+v, want ready existing result", got)
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
