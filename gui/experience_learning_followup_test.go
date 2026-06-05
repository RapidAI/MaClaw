package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func assertExperienceLearningToolTopLevelHandoff(t *testing.T, raw, expectedAction string) {
	t.Helper()
	var payload struct {
		OK                      bool                   `json:"ok"`
		TraceID                 string                 `json:"trace_id"`
		RecommendedFocusContext map[string]interface{} `json:"recommended_focus_context"`
		RecommendedToolCall     map[string]interface{} `json:"recommended_tool_call"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal experience_learning tool result: %v\n%s", err, raw)
	}
	if !payload.OK || strings.TrimSpace(payload.TraceID) == "" || len(payload.RecommendedFocusContext) == 0 {
		t.Fatalf("expected top-level safe handoff fields: %#v", payload)
	}
	args, ok := payload.RecommendedToolCall["args"].(map[string]interface{})
	if !ok || payload.RecommendedToolCall["tool"] != "experience_learning" || payload.RecommendedToolCall["non_executing"] != true || args["action"] != expectedAction || args["trace_id"] != payload.TraceID {
		t.Fatalf("expected top-level %s recommended tool call for trace %s: %#v", expectedAction, payload.TraceID, payload.RecommendedToolCall)
	}
}

func TestExperienceLearningToolSnapshotReturnsSafeGovernanceHandoff(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}}
	raw := handler.toolExperienceLearning(map[string]interface{}{"action": "snapshot"})
	var payload struct {
		OK                      bool                       `json:"ok"`
		Snapshot                ExperienceLearningSnapshot `json:"snapshot"`
		RecommendedFocusContext map[string]interface{}     `json:"recommended_focus_context"`
		RecommendedToolCall     map[string]interface{}     `json:"recommended_tool_call"`
		NonExecutingBoundary    string                     `json:"non_executing_boundary"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal snapshot result: %v\n%s", err, raw)
	}
	args, ok := payload.RecommendedToolCall["args"].(map[string]interface{})
	if !payload.OK || !ok || payload.RecommendedToolCall["tool"] != "experience_learning" || args["action"] != "governance_summary" || payload.RecommendedFocusContext["action_kind"] != "inspect_governance_summary" || payload.NonExecutingBoundary == "" {
		t.Fatalf("snapshot should expose safe governance handoff: %#v", payload)
	}
}

func TestBuildExperienceTraceFollowUpDraftsApprovedRollback(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "A2A: rollback review",
		Content:    "A2A discussion result\nRollback on:\n- gate fails",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{groupDiscussionResultTag, "discussion:disc-1", groupDiscussionRollbackTag, experienceReviewStatusTagPrefix + experienceReviewOutcomeApproved, experienceReviewResolvedTag},
		SourceType: groupDiscussionMemorySourceType,
		SourceURL:  "a2a://current_hub/disc-1",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, groupDiscussionRollbackTag)
	app := &App{memoryStore: store}

	draft, err := app.BuildExperienceTraceFollowUp("memory:" + entry.ID)
	if err != nil {
		t.Fatalf("BuildExperienceTraceFollowUp: %v", err)
	}
	if draft.ActionKind != "draft_rollback_workflow" || draft.SourceURL != "a2a://current_hub/disc-1" {
		t.Fatalf("unexpected rollback draft metadata: %#v", draft)
	}
	assertExperienceRecommendedFocusContext(t, draft.RecommendedFocusContext, "memory:"+entry.ID, "A2A: rollback review", "draft_rollback_workflow")
	if !strings.Contains(draft.NonExecutingBoundary, "manual follow-up draft") || !strings.Contains(draft.NonExecutingBoundary, "no rollback execution") {
		t.Fatalf("unexpected rollback follow-up boundary: %q", draft.NonExecutingBoundary)
	}
	if !strings.Contains(draft.Draft, "Manual rollback workflow draft") || !strings.Contains(draft.Draft, "does not execute rollback") || !strings.Contains(draft.Draft, "Rollback on") {
		t.Fatalf("unexpected rollback draft:\n%s", draft.Draft)
	}
}

func TestBuildExperienceTraceFollowUpDraftsTriggeredRollbackReview(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "A2A: triggered rollback review",
		Content:    "A2A discussion result\nSummary: ship option A\nRollback on:\n- gate fails\nMatched rollback triggers:\n- gate fails",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{groupDiscussionResultTag, "discussion:disc-1", groupDiscussionRollbackTag, groupDiscussionRollbackTriggered, experienceReviewStatusTagPrefix + experienceReviewOutcomeApproved, experienceReviewResolvedTag},
		SourceType: groupDiscussionMemorySourceType,
		SourceURL:  "a2a://current_hub/disc-1",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, groupDiscussionRollbackTriggered)
	app := &App{memoryStore: store}

	draft, err := app.BuildExperienceTraceFollowUp("memory:" + entry.ID)
	if err != nil {
		t.Fatalf("BuildExperienceTraceFollowUp: %v", err)
	}
	if draft.ActionKind != "draft_rollback_workflow" || draft.SourceURL != "a2a://current_hub/disc-1" {
		t.Fatalf("unexpected triggered rollback draft metadata: %#v", draft)
	}
	assertExperienceRecommendedFocusContext(t, draft.RecommendedFocusContext, "memory:"+entry.ID, "A2A: triggered rollback review", "triggered rollback")
	if !strings.Contains(draft.Draft, "Triggered rollback workflow handoff") || !strings.Contains(draft.Draft, "does not execute rollback") || !strings.Contains(draft.Draft, "Matched rollback triggers") {
		t.Fatalf("unexpected triggered rollback draft:\n%s", draft.Draft)
	}
	if len(draft.Checks) != 3 || !strings.Contains(strings.Join(draft.Checks, "\n"), "owner") {
		t.Fatalf("unexpected triggered rollback checks: %#v", draft.Checks)
	}
}

func TestBuildExperienceRollbackWorkflowDraftFromApprovedRollbackReview(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "A2A: rollback review",
		Content:    "A2A discussion result\nSummary: ship option A\nRationale: reviewed by two agents\nRollback on:\n- gate fails\n- owner rejects acceptance evidence\nDecision rationale: lower operational risk",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{groupDiscussionResultTag, "discussion:disc-1", groupDiscussionRollbackTag, experienceReviewStatusTagPrefix + experienceReviewOutcomeApproved, experienceReviewResolvedTag},
		SourceType: groupDiscussionMemorySourceType,
		SourceURL:  "a2a://current_hub/disc-1",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, groupDiscussionRollbackTag)
	app := &App{memoryStore: store}

	draft, err := app.BuildExperienceRollbackWorkflowDraft("memory:" + entry.ID)
	if err != nil {
		t.Fatalf("BuildExperienceRollbackWorkflowDraft: %v", err)
	}
	if draft.DecisionSummary != "ship option A" || draft.DecisionRationale != "lower operational risk" || len(draft.RollbackTriggers) != 2 {
		t.Fatalf("unexpected rollback draft metadata: %#v", draft)
	}
	assertExperienceRecommendedFocusContext(t, draft.RecommendedFocusContext, "memory:"+entry.ID, "A2A: rollback review", "rollback review")
	assertExperienceInspectionRecommendedToolCall(t, draft.RecommendedToolCall, "memory:"+entry.ID)
	if !strings.Contains(draft.DraftMarkdown, "Manual Rollback Workflow Draft") || !strings.Contains(draft.DraftMarkdown, "gate fails") || !strings.Contains(draft.NonExecutingBoundary, "no rollback execution") {
		t.Fatalf("unexpected rollback draft:\n%#v", draft)
	}
}

func TestBuildExperienceRollbackWorkflowDraftRejectsPendingReview(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "A2A: rollback review",
		Content:    "A2A discussion result\nRollback on:\n- gate fails",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{groupDiscussionResultTag, "discussion:disc-1", groupDiscussionRollbackTag, experienceReviewRequiredTag},
		SourceType: groupDiscussionMemorySourceType,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, groupDiscussionRollbackTag)
	app := &App{memoryStore: store}

	if _, err := app.BuildExperienceRollbackWorkflowDraft("memory:" + entry.ID); err == nil || !strings.Contains(err.Error(), "must be approved") {
		t.Fatalf("expected approved rollback draft rejection, got %v", err)
	}
}

func TestBuildExperienceEscalationBriefFromEscalationEvidence(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "A2A escalation",
		Content:    "A2A discussion result\nSummary: needs owner input\nEscalation:\n- Reason: unresolved policy owner\n- Target: human_owner\n- Raised by: agent-a",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{groupDiscussionResultTag, "discussion:disc-1", "has_escalation", "escalation_target:abc"},
		SourceType: groupDiscussionMemorySourceType,
		SourceURL:  "a2a://current_hub/disc-1",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, "has_escalation")
	app := &App{memoryStore: store}

	draft, err := app.BuildExperienceEscalationBrief("memory:" + entry.ID)
	if err != nil {
		t.Fatalf("BuildExperienceEscalationBrief: %v", err)
	}
	if draft.Target != "human_owner" || draft.RaisedBy != "agent-a" || draft.Reason != "unresolved policy owner" {
		t.Fatalf("unexpected escalation brief metadata: %#v", draft)
	}
	assertExperienceRecommendedFocusContext(t, draft.RecommendedFocusContext, "memory:"+entry.ID, "A2A escalation", "escalation")
	assertExperienceInspectionRecommendedToolCall(t, draft.RecommendedToolCall, "memory:"+entry.ID)
	if !strings.Contains(draft.BriefMarkdown, "A2A Escalation Handoff Brief") || !strings.Contains(draft.BriefMarkdown, "unresolved policy owner") || !strings.Contains(draft.NonExecutingBoundary, "no routing") {
		t.Fatalf("unexpected escalation brief:\n%#v", draft)
	}
	snapshot := buildExperienceLearningSnapshot(nil, store)
	if snapshot.NextActionKindCounts["prepare_escalation_brief"] != 1 {
		t.Fatalf("escalation evidence should be queued as next action: %#v", snapshot.NextActionKindCounts)
	}
}

func TestBuildExperienceConflictReconciliationDraftFromApprovedConflictReview(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title: "A2A conflict review",
		Content: strings.Join([]string{
			"A2A conflict review candidate",
			"Topic: release",
			"Question: ship?",
			"New discussion: disc-new",
			"New summary: choose option A",
			"Existing memory: disc-old",
			"Existing summary: reject option A",
			"Review before treating either A2A result as durable project policy; opposite decision signals were detected for the same topic or question.",
		}, "\n"),
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{groupDiscussionConflictTag, "topic:release", experienceReviewStatusTagPrefix + experienceReviewOutcomeApproved, experienceReviewResolvedTag, "conflict_reviewed"},
		SourceType: groupDiscussionMemorySourceType,
		SourceURL:  "a2a://current_hub/disc-new",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, groupDiscussionConflictTag)
	app := &App{memoryStore: store}

	draft, err := app.BuildExperienceConflictReconciliationDraft("memory:" + entry.ID)
	if err != nil {
		t.Fatalf("BuildExperienceConflictReconciliationDraft: %v", err)
	}
	if draft.Topic != "release" || draft.Question != "ship?" || draft.NewSummary != "choose option A" || draft.ExistingSummary != "reject option A" {
		t.Fatalf("unexpected conflict draft metadata: %#v", draft)
	}
	assertExperienceRecommendedFocusContext(t, draft.RecommendedFocusContext, "memory:"+entry.ID, "A2A conflict review", "conflict review")
	assertExperienceInspectionRecommendedToolCall(t, draft.RecommendedToolCall, "memory:"+entry.ID)
	if !strings.Contains(draft.DraftMarkdown, "A2A Conflict Reconciliation Draft") || !strings.Contains(draft.DraftMarkdown, "choose option A") || !strings.Contains(draft.DraftMarkdown, "reject option A") || !strings.Contains(draft.NonExecutingBoundary, "no memory update") {
		t.Fatalf("unexpected conflict draft:\n%#v", draft)
	}
	snapshot := buildExperienceLearningSnapshot(nil, store)
	if snapshot.NextActionTraceCount != 1 || snapshot.NextActionKindCounts["resolve_a2a_conflict_manually"] != 1 {
		t.Fatalf("approved conflict review should queue reconciliation follow-up: %#v", snapshot)
	}
}

func TestBuildExperienceTraceFollowUpRejectsPendingReview(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "A2A: rollback review",
		Content:    "A2A discussion result\nRollback on:\n- gate fails",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{groupDiscussionResultTag, "discussion:disc-1", groupDiscussionRollbackTag, experienceReviewRequiredTag},
		SourceType: groupDiscussionMemorySourceType,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, groupDiscussionRollbackTag)
	app := &App{memoryStore: store}

	if _, err := app.BuildExperienceTraceFollowUp("memory:" + entry.ID); err == nil || !strings.Contains(err.Error(), "must be reviewed") {
		t.Fatalf("expected pending review rejection, got %v", err)
	}
}

func TestBuildExperienceTraceFollowUpDraftsApprovedSkillNudge(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "Skill candidate",
		Content:    "Skill nudge candidate refactor_flow; sequence rg -> apply_patch -> go_test",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{"skill_nudge_candidate", "refactor_flow", "rg", "apply_patch", "go_test", experienceReviewStatusTagPrefix + experienceReviewOutcomeApproved, experienceReviewResolvedTag, "skill_nudge_reviewed"},
		SourceType: "tool_usage",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, "skill_nudge_candidate")
	app := &App{memoryStore: store}

	draft, err := app.BuildExperienceTraceFollowUp("memory:" + entry.ID)
	if err != nil {
		t.Fatalf("BuildExperienceTraceFollowUp: %v", err)
	}
	if draft.ActionKind != "draft_skill_manually" || len(draft.Checks) == 0 {
		t.Fatalf("unexpected skill draft metadata: %#v", draft)
	}
	assertExperienceRecommendedFocusContext(t, draft.RecommendedFocusContext, "memory:"+entry.ID, "Skill candidate", "draft_skill_manually")
	assertExperienceInspectionRecommendedToolCall(t, draft.RecommendedToolCall, "memory:"+entry.ID)
	if !strings.Contains(draft.Draft, "Manual skill draft brief") || !strings.Contains(draft.Draft, "does not execute rollback, create or install skills") {
		t.Fatalf("unexpected skill draft:\n%s", draft.Draft)
	}
}

func TestBuildExperienceSkillDraftRejectsLegacyBrowserSkillNudge(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "Skill candidate",
		Content:    "Skill nudge candidate browser_flow; sequence browser_observe -> browser_click -> browser_verify; tokens [browser, checkout]; evidence 5, success 100%, confidence 0.82",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{"skill_nudge_candidate", "browser_automation", "browser_flow", "browser_observe", "browser_click", "browser_verify", experienceReviewStatusTagPrefix + experienceReviewOutcomeApproved, experienceReviewResolvedTag, "skill_nudge_reviewed"},
		SourceType: "tool_usage",
		SourceURL:  "tool_usage://skill/browser_flow",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, "skill_nudge_candidate")
	app := &App{memoryStore: store}

	if _, err := app.BuildExperienceSkillDraft("memory:" + entry.ID); err == nil || !strings.Contains(err.Error(), "not a skill nudge review") {
		t.Fatalf("expected legacy browser skill nudge rejection, got %v", err)
	}
}

func TestExperienceLearningToolBuildsSkillDraftFromUsageNudgeWithoutTrace(t *testing.T) {
	tracker, err := coretool.NewUsageTracker("")
	if err != nil {
		t.Fatalf("NewUsageTracker: %v", err)
	}
	now := time.Now()
	for i := 0; i < 4; i++ {
		tracker.RecordExperience(coretool.ToolExperience{
			ToolName:     "apply_patch",
			QueryTokens:  []string{"refactor", "checkout"},
			TaskType:     "code_refactor",
			ToolSequence: []string{"rg", "apply_patch", "go_test"},
			Success:      true,
			FinalOutcome: "completed",
			Timestamp:    now,
		})
	}

	handler := &IMMessageHandler{app: &App{usageTracker: tracker}}
	raw := handler.toolExperienceLearning(map[string]interface{}{
		"action": "build_skill_draft",
		"query":  "refactor",
		"limit":  5,
	})

	var payload struct {
		OK                   bool                 `json:"ok"`
		SkillDraft           ExperienceSkillDraft `json:"skill_draft"`
		NonExecutingBoundary string               `json:"non_executing_boundary"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal build_skill_draft result: %v\n%s", err, raw)
	}
	if !payload.OK {
		t.Fatalf("expected ok result: %s", raw)
	}
	if payload.SkillDraft.TraceID != "" || payload.SkillDraft.Kind != experienceDraftKindSkill {
		t.Fatalf("expected usage-sourced skill draft without trace id: %#v", payload.SkillDraft)
	}
	if payload.SkillDraft.TaskType != "code_refactor" || len(payload.SkillDraft.ToolSequence) != 3 || payload.SkillDraft.ToolSequence[0] != "rg" {
		t.Fatalf("unexpected usage skill draft metadata: %#v", payload.SkillDraft)
	}
	if !strings.Contains(payload.SkillDraft.DraftMarkdown, "UsageTracker.DistillSkillNudgeCandidates") || !strings.Contains(payload.SkillDraft.NonExecutingBoundary, "read-only usage-sequence skill draft only") {
		t.Fatalf("expected read-only usage draft evidence and boundary: %#v", payload.SkillDraft)
	}
	if !strings.Contains(payload.NonExecutingBoundary, "no file write") {
		t.Fatalf("expected top-level non-executing boundary, got %q", payload.NonExecutingBoundary)
	}
}

func TestBuildExperienceSkillDraftRejectsNonApprovedSkillNudge(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "Skill candidate",
		Content:    "Skill nudge candidate refactor_flow; sequence rg -> apply_patch -> go_test",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{"skill_nudge_candidate", "refactor_flow", "rg", "apply_patch", "go_test", experienceReviewRequiredTag},
		SourceType: "tool_usage",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, "skill_nudge_candidate")
	app := &App{memoryStore: store}

	if _, err := app.BuildExperienceSkillDraft("memory:" + entry.ID); err == nil || !strings.Contains(err.Error(), "must be approved") {
		t.Fatalf("expected approved skill draft rejection, got %v", err)
	}
}

func TestRecordExperienceTraceFollowUpCompletedRemovesNextAction(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "A2A: rollback review",
		Content:    "A2A discussion result\nRollback on:\n- gate fails",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{groupDiscussionResultTag, "discussion:disc-1", groupDiscussionRollbackTag, experienceReviewStatusTagPrefix + experienceReviewOutcomeApproved, experienceReviewResolvedTag},
		SourceType: groupDiscussionMemorySourceType,
		SourceURL:  "a2a://current_hub/disc-1",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, groupDiscussionRollbackTag)
	app := &App{memoryStore: store}

	record, err := app.RecordExperienceTraceFollowUp("memory:"+entry.ID, ExperienceTraceFollowUpRequest{Status: "done", Note: "owner accepted manual workflow", Actor: "owner"})
	if err != nil {
		t.Fatalf("RecordExperienceTraceFollowUp: %v", err)
	}
	if record.Status != experienceFollowUpOutcomeCompleted || record.ActionKind != "draft_rollback_workflow" {
		t.Fatalf("unexpected follow-up record: %#v", record)
	}
	assertExperienceInspectionRecommendedToolCall(t, record.RecommendedToolCall, "memory:"+entry.ID)

	updated := mustFindExperienceReviewEntry(t, store, groupDiscussionRollbackTag)
	if !hasTag(updated.Tags, experienceFollowUpStatusTagPrefix+experienceFollowUpOutcomeCompleted) || !hasTag(updated.Tags, experienceFollowUpResolvedTag) || !strings.Contains(updated.Content, "Experience follow-up record:") {
		t.Fatalf("unexpected follow-up tags/content: %#v\n%s", updated.Tags, updated.Content)
	}
	snapshot := buildExperienceLearningSnapshot(nil, store)
	if snapshot.NextActionTraceCount != 0 || len(snapshot.NextActionSummaries) != 0 {
		t.Fatalf("completed follow-up should leave no queued action: %#v", snapshot)
	}
	if snapshot.FollowUpTraceCount != 1 || snapshot.FollowUpStatusCounts[experienceFollowUpOutcomeCompleted] != 1 {
		t.Fatalf("completed follow-up counts not aggregated: %d/%#v", snapshot.FollowUpTraceCount, snapshot.FollowUpStatusCounts)
	}
	followUpSummary := mustFindExperienceFollowUpSummary(t, snapshot.FollowUpSummaries, experienceFollowUpOutcomeCompleted)
	if followUpSummary.Count != 1 || followUpSummary.LatestTraceID == "" || followUpSummary.LatestActionKind != "draft_rollback_workflow" || !strings.Contains(followUpSummary.LatestNote, "manual workflow") {
		t.Fatalf("completed follow-up summary not aggregated: %#v", followUpSummary)
	}
	for _, detail := range snapshot.TraceDetails {
		if detail.Kind != "a2a_rollback_review" {
			continue
		}
		if detail.NextActionKind != "" || detail.FollowUpStatus != experienceFollowUpOutcomeCompleted || detail.FollowUpActionKind != "draft_rollback_workflow" || detail.FollowUpActor != "owner" || detail.FollowUpCount != 1 || !strings.Contains(detail.FollowUpNote, "manual workflow") {
			t.Fatalf("completed follow-up audit not surfaced: %#v", detail)
		}
		if _, err := app.BuildExperienceTraceFollowUp(detail.ID); err == nil {
			t.Fatal("completed follow-up should not draft again")
		}
		return
	}
	t.Fatalf("missing rollback detail: %#v", snapshot.TraceDetails)
}

func TestRecordExperienceDraftReviewCreatesFollowUpTrace(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	app := &App{memoryStore: store}
	source := memory.Entry{
		ID:         "rollback-source",
		Title:      "A2A: rollback source",
		Content:    "Tool-backed source trace for memory maintenance draft review.",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{"usage_pattern"},
		SourceType: "tool_usage",
	}
	if err := store.Save(source); err != nil {
		t.Fatalf("Save source: %v", err)
	}

	record, err := app.RecordExperienceDraftReview(ExperienceDraftReviewRequest{
		Kind:                 "memory_maintenance_draft",
		Status:               "completed",
		SourceTraceID:        "memory:" + source.ID,
		Query:                "a2a rollback",
		Note:                 "owner accepted retention plan",
		Actor:                "owner",
		DraftMarkdown:        "# Memory Maintenance Draft\n\nKeep rollback anchors.",
		NonExecutingBoundary: "read-only memory maintenance draft only",
	})
	if err != nil {
		t.Fatalf("RecordExperienceDraftReview: %v", err)
	}
	if record.Kind != experienceDraftKindMaintenance || record.Status != experienceFollowUpOutcomeCompleted || record.TraceID == "" || record.MemoryID == "" || record.SourceTraceID != "memory:"+source.ID {
		t.Fatalf("unexpected draft review record: %#v", record)
	}
	assertExperienceRecommendedFocusContext(t, record.RecommendedFocusContext, "memory:"+source.ID, "A2A: rollback source", "draft review")

	snapshot := buildExperienceLearningSnapshot(nil, store)
	if snapshot.FollowUpTraceCount != 1 || snapshot.FollowUpStatusCounts[experienceFollowUpOutcomeCompleted] != 1 {
		t.Fatalf("draft review should be aggregated as completed follow-up evidence: %#v", snapshot)
	}
	if snapshot.TraceKindCounts["memory_maintenance_draft_review"] != 1 || snapshot.TraceSourceCounts[experienceLearningSourceType] != 1 {
		t.Fatalf("draft review trace kind/source not aggregated: %#v/%#v", snapshot.TraceKindCounts, snapshot.TraceSourceCounts)
	}
	if snapshot.FollowUpActionKindCounts[experienceDraftKindMaintenance] != 1 || len(snapshot.FollowUpActionSummaries) != 1 {
		t.Fatalf("draft review action kind not aggregated: %#v/%#v", snapshot.FollowUpActionKindCounts, snapshot.FollowUpActionSummaries)
	}
	if summary := snapshot.FollowUpActionSummaries[0]; summary.Kind != experienceDraftKindMaintenance || summary.Count != 1 || summary.StatusCounts[experienceFollowUpOutcomeCompleted] != 1 || summary.LatestTraceID != record.TraceID || !strings.Contains(summary.LatestNote, "retention") {
		t.Fatalf("draft review action summary missing latest audit detail: %#v", summary)
	}
	if snapshot.NextActionTraceCount != 0 || snapshot.ReviewRequiredTraceCount != 0 {
		t.Fatalf("draft review should not create action or review queues: %#v", snapshot)
	}
	for _, detail := range snapshot.TraceDetails {
		if detail.ID != record.TraceID {
			continue
		}
		if detail.Kind != "memory_maintenance_draft_review" || detail.SourceType != experienceLearningSourceType || detail.SourceURL != "experience://draft/memory_maintenance_draft" || detail.SourceTraceID != "memory:"+source.ID {
			t.Fatalf("unexpected draft review trace metadata: %#v", detail)
		}
		if detail.FollowUpStatus != experienceFollowUpOutcomeCompleted || detail.FollowUpActionKind != experienceDraftKindMaintenance || detail.FollowUpActor != "owner" || !strings.Contains(detail.FollowUpNote, "retention") {
			t.Fatalf("draft review follow-up audit not surfaced: %#v", detail)
		}
		if detail.NextActionKind != "" || detail.ReviewRequired || !strings.Contains(detail.Detail, "Draft excerpt") || !strings.Contains(detail.Detail, "Source trace: memory:"+source.ID) {
			t.Fatalf("draft review should remain evidence-only: %#v", detail)
		}
		return
	}
	t.Fatalf("missing draft review detail for %s: %#v", record.TraceID, snapshot.TraceDetails)
}

func TestRecordExperienceDraftReviewSupportsTraceDraftKinds(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	app := &App{memoryStore: store}

	cases := []struct {
		name      string
		inputKind string
		wantKind  string
		wantTrace string
	}{
		{name: "skill", inputKind: "skill_draft", wantKind: experienceDraftKindSkill, wantTrace: "skill_draft_review"},
		{name: "rollback", inputKind: "rollback_draft", wantKind: experienceDraftKindRollback, wantTrace: "rollback_workflow_draft_review"},
		{name: "escalation", inputKind: "escalation_brief", wantKind: experienceDraftKindEscalation, wantTrace: "escalation_brief_review"},
		{name: "conflict", inputKind: "conflict_draft", wantKind: experienceDraftKindConflict, wantTrace: "conflict_reconciliation_draft_review"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record, err := app.RecordExperienceDraftReview(ExperienceDraftReviewRequest{
				Kind:                 tc.inputKind,
				Status:               "deferred",
				SourceTraceID:        "memory:" + tc.name + "-source",
				Note:                 "owner requested more evidence",
				Actor:                "owner",
				DraftMarkdown:        "# Draft\n\nManual-only evidence.",
				NonExecutingBoundary: "read-only draft review only",
			})
			if err != nil {
				t.Fatalf("RecordExperienceDraftReview(%s): %v", tc.inputKind, err)
			}
			if record.Kind != tc.wantKind || record.Status != experienceFollowUpOutcomeDeferred || record.TraceID == "" || record.SourceTraceID != "memory:"+tc.name+"-source" {
				t.Fatalf("unexpected draft review record: %#v", record)
			}
		})
	}

	snapshot := buildExperienceLearningSnapshot(nil, store)
	if snapshot.FollowUpTraceCount != len(cases) || snapshot.FollowUpStatusCounts[experienceFollowUpOutcomeDeferred] != len(cases) {
		t.Fatalf("trace draft review outcomes should aggregate as deferred follow-ups: %#v", snapshot)
	}
	for _, tc := range cases {
		if snapshot.TraceKindCounts[tc.wantTrace] != 1 || snapshot.FollowUpActionKindCounts[tc.wantKind] != 1 {
			t.Fatalf("missing trace draft review aggregation for %s: %#v/%#v", tc.wantKind, snapshot.TraceKindCounts, snapshot.FollowUpActionKindCounts)
		}
		found := false
		for _, detail := range snapshot.TraceDetails {
			if detail.Kind != tc.wantTrace {
				continue
			}
			found = true
			if detail.SourceURL != "experience://draft/"+tc.wantKind || detail.SourceTraceID != "memory:"+tc.name+"-source" || detail.FollowUpActionKind != tc.wantKind || detail.NextActionKind != "" || detail.ReviewRequired {
				t.Fatalf("trace draft review should remain audit-only for %s: %#v", tc.wantKind, detail)
			}
		}
		if !found {
			t.Fatalf("missing trace detail for %s: %#v", tc.wantTrace, snapshot.TraceDetails)
		}
	}
}

func TestRecordExperienceDraftReviewReturnsSkillDraftExecutionPreview(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	app := &App{memoryStore: store}
	draftID := "skill_draft:mark_needs_review:broken:"

	record, err := app.RecordExperienceDraftReview(ExperienceDraftReviewRequest{
		Kind:          experienceDraftKindSkill,
		Status:        "completed",
		DraftID:       draftID,
		DraftMarkdown: "# Skill governance draft\n\nMark broken as needs_review.",
	})
	if err != nil {
		t.Fatalf("RecordExperienceDraftReview: %v", err)
	}
	if record.DraftID != draftID {
		t.Fatalf("draft id = %q, want %q", record.DraftID, draftID)
	}
	args, ok := record.RecommendedToolCall["args"].(map[string]interface{})
	if !ok || record.RecommendedToolCall["tool"] != "manage_skill" || args["action"] != "execute_maintenance_plan" || args["dry_run"] != true {
		t.Fatalf("unexpected recommended tool call: %#v", record.RecommendedToolCall)
	}
	reviewIDs, ok := args["approved_review_trace_ids"].([]string)
	if !ok || len(reviewIDs) != 1 || reviewIDs[0] != record.TraceID {
		t.Fatalf("approved review trace ids = %#v", args["approved_review_trace_ids"])
	}

	entry, err := findExperienceMemoryEntryByTraceID(store, record.TraceID)
	if err != nil {
		t.Fatalf("find review entry: %v", err)
	}
	detail, ok := traceDetailFromMemoryEntry(entry)
	if !ok || detail.DraftID != draftID || detail.Kind != "skill_draft_review" {
		t.Fatalf("draft review detail = %#v ok=%v", detail, ok)
	}

	snapshot := buildExperienceLearningSnapshot(nil, store)
	if snapshot.ApprovedSkillDraftReviewCount != 1 || len(snapshot.ApprovedSkillDraftReviews) != 1 || snapshot.ApprovedSkillDraftReviews[0].TraceID != record.TraceID || snapshot.ApprovedSkillDraftReviews[0].DraftID != draftID {
		t.Fatalf("approved skill draft review queue = %#v", snapshot.ApprovedSkillDraftReviews)
	}
	action, reason := experienceGovernanceRecommendedNextAction(snapshot, nil)
	if action != experienceGovernanceActionExecuteApprovedSkillDraftReviews.String() || !strings.Contains(reason, "execution preview") {
		t.Fatalf("recommended action = %q reason=%q", action, reason)
	}
	call := experienceGovernanceRecommendedToolCall(action, snapshot, nil, reason)
	callArgs, ok := call["args"].(map[string]interface{})
	if !ok || call["tool"] != "manage_skill" || callArgs["action"] != "execute_maintenance_plan" || callArgs["dry_run"] != true {
		t.Fatalf("governance recommended tool call = %#v", call)
	}
	governanceReviewIDs, ok := callArgs["approved_review_trace_ids"].([]string)
	if !ok || len(governanceReviewIDs) != 1 || governanceReviewIDs[0] != record.TraceID {
		t.Fatalf("approved review trace ids = %#v", callArgs["approved_review_trace_ids"])
	}

	followUps := app.QueryExperienceFollowUpActions(ExperienceTraceDetailQuery{Kind: "skill_draft_review", FollowUpStatus: experienceFollowUpOutcomeCompleted})
	followUpArgs, ok := followUps.RecommendedToolCall["args"].(map[string]interface{})
	if !ok || followUps.RecommendedToolCall["tool"] != "manage_skill" || followUpArgs["action"] != "execute_maintenance_plan" || followUpArgs["dry_run"] != true {
		t.Fatalf("follow-up recommended tool call = %#v", followUps.RecommendedToolCall)
	}
	followUpReviewIDs, ok := followUpArgs["approved_review_trace_ids"].([]string)
	if !ok || len(followUpReviewIDs) != 1 || followUpReviewIDs[0] != record.TraceID {
		t.Fatalf("follow-up approved review trace ids = %#v", followUpArgs["approved_review_trace_ids"])
	}

	governance := app.GetExperienceGovernanceSummary(ExperienceRoutingSignalQuery{})
	if governance["recommended_next_action"] != experienceGovernanceActionExecuteApprovedSkillDraftReviews.String() {
		t.Fatalf("governance summary action = %#v", governance)
	}
	governanceCall, ok := governance["recommended_tool_call"].(map[string]interface{})
	if !ok || governanceCall["tool"] != "manage_skill" {
		t.Fatalf("governance summary tool call = %#v", governance["recommended_tool_call"])
	}
	queues, ok := governance["queues"].(map[string]interface{})
	if !ok || queues["approved_skill_draft_review_count"] != 1 {
		t.Fatalf("governance queues = %#v", governance["queues"])
	}
	queueBlock, ok := queues["skill_draft_review_queues"].(ExperienceSkillDraftReviewQueues)
	if !ok || len(queueBlock.ApprovedUnpreviewed) != 1 || len(queueBlock.PreviewedWaitingConfirm) != 0 {
		t.Fatalf("expected explicit unpreviewed queue: %#v", queues["skill_draft_review_queues"])
	}

	if err := (&IMMessageHandler{app: app}).recordSkillDraftExecutionAudit([]string{record.TraceID}, skillDraftExecutionBlocked, "current plan no longer contains reviewed draft"); err != nil {
		t.Fatalf("record blocked execution audit: %v", err)
	}
	blockedSnapshot := buildExperienceLearningSnapshot(nil, store)
	if blockedSnapshot.ApprovedSkillDraftReviewCount != 0 || blockedSnapshot.BlockedSkillDraftReviewCount != 1 || len(blockedSnapshot.SkillDraftReviewQueues.Blocked) != 1 {
		t.Fatalf("blocked skill draft queues = %#v", blockedSnapshot.SkillDraftReviewQueues)
	}
	blockedAction, blockedReason := experienceGovernanceRecommendedNextAction(blockedSnapshot, nil)
	if blockedAction != experienceGovernanceActionInspectBlockedSkillDraftReviews.String() || !strings.Contains(blockedReason, "repair") {
		t.Fatalf("blocked recommended action = %q reason=%q", blockedAction, blockedReason)
	}
	blockedCall := experienceGovernanceRecommendedToolCall(blockedAction, blockedSnapshot, nil, blockedReason)
	blockedArgs, ok := blockedCall["args"].(map[string]interface{})
	if !ok || blockedCall["tool"] != "experience_learning" || blockedArgs["action"] != "build_blocked_skill_draft" || blockedArgs["trace_id"] != record.TraceID {
		t.Fatalf("blocked recommended tool call = %#v", blockedCall)
	}
	blockedDraft, err := app.BuildExperienceBlockedSkillDraft(record.TraceID)
	if err != nil {
		t.Fatalf("BuildExperienceBlockedSkillDraft: %v", err)
	}
	if blockedDraft.DraftID != draftID || blockedDraft.ExecutionStatus != skillDraftExecutionBlocked || !strings.Contains(blockedDraft.DraftMarkdown, "Reviewer Decision Required") || len(blockedDraft.Checks) == 0 || blockedDraft.ReviewOptions["close"] == nil || blockedDraft.ReviewOptions["reopen"] == nil {
		t.Fatalf("blocked draft = %#v", blockedDraft)
	}
	if len(blockedDraft.ReviewAffordances) != 2 {
		t.Fatalf("blocked draft review affordances = %#v", blockedDraft.ReviewAffordances)
	}
	var closeAffordance, reopenAffordance ExperienceReviewAffordance
	for _, affordance := range blockedDraft.ReviewAffordances {
		switch affordance.ID {
		case "close":
			closeAffordance = affordance
		case "reopen":
			reopenAffordance = affordance
		}
	}
	closeArgs, closeOK := closeAffordance.ToolCall["args"].(map[string]interface{})
	reopenArgs, reopenOK := reopenAffordance.ToolCall["args"].(map[string]interface{})
	if closeAffordance.Intent != "close_blocked_skill_draft" || len(closeAffordance.RequiredInputs) != 0 || !closeOK || closeArgs["action"] != "record_blocked_skill_draft_review" || closeArgs["resolution"] != "close" {
		t.Fatalf("close affordance = %#v args=%#v", closeAffordance, closeArgs)
	}
	if reopenAffordance.Intent != "reopen_blocked_skill_draft" || len(reopenAffordance.RequiredInputs) != 1 || reopenAffordance.RequiredInputs[0].Name != "replacement_draft_id" || !reopenAffordance.RequiredInputs[0].Required || !reopenOK || reopenArgs["action"] != "record_blocked_skill_draft_review" || reopenArgs["resolution"] != "reopen" {
		t.Fatalf("reopen affordance = %#v args=%#v", reopenAffordance, reopenArgs)
	}
	toolRaw := (&IMMessageHandler{app: app}).toolExperienceLearning(map[string]interface{}{"action": "build_blocked_skill_draft", "trace_id": record.TraceID})
	if !strings.Contains(toolRaw, "blocked_skill_draft") || !strings.Contains(toolRaw, "Current Maintenance Plan Diff") {
		t.Fatalf("blocked skill draft tool output missing draft: %s", toolRaw)
	}
	reopenedDraftID := "skill_draft:mark_needs_review:fixed-review-skill:"
	reopened, err := app.RecordBlockedSkillDraftReview(record.TraceID, "reopen", reopenedDraftID, "", "reviewer")
	if err != nil {
		t.Fatalf("record reopened draft review: %v", err)
	}
	reopenedSource, err := findExperienceMemoryEntryByTraceID(store, record.TraceID)
	if err != nil {
		t.Fatalf("find reopened source trace: %v", err)
	}
	reopenedSourceDetail, ok := traceDetailFromMemoryEntry(reopenedSource)
	if !ok || reopenedSourceDetail.DraftExecutionStatus != skillDraftExecutionReopened {
		t.Fatalf("source trace should be reopened after repair review: %#v ok=%v", reopenedSourceDetail, ok)
	}
	reopenedSnapshot := buildExperienceLearningSnapshot(nil, store)
	if reopenedSnapshot.BlockedSkillDraftReviewCount != 0 || reopenedSnapshot.ApprovedSkillDraftReviewCount != 1 || reopenedSnapshot.ApprovedSkillDraftReviews[0].TraceID != reopened.TraceID || reopenedSnapshot.ApprovedSkillDraftReviews[0].DraftID != reopenedDraftID {
		t.Fatalf("reopened queues = active:%#v blocked:%#v", reopenedSnapshot.ApprovedSkillDraftReviews, reopenedSnapshot.SkillDraftReviewQueues.Blocked)
	}
	if len(reopenedSnapshot.SkillDraftReviewQueues.Reopened) != 1 || reopenedSnapshot.ReopenedSkillDraftReviewCount != 1 {
		t.Fatalf("reopened history queue = %#v", reopenedSnapshot.SkillDraftReviewQueues.Reopened)
	}
	closedDraftID := "skill_draft:mark_needs_review:closed-review-skill:"
	closedSource, err := app.RecordExperienceDraftReview(ExperienceDraftReviewRequest{Kind: experienceDraftKindSkill, Status: experienceFollowUpOutcomeCompleted, DraftID: closedDraftID})
	if err != nil {
		t.Fatalf("record closed source review: %v", err)
	}
	if err := (&IMMessageHandler{app: app}).recordSkillDraftExecutionAudit([]string{closedSource.TraceID}, skillDraftExecutionBlocked, "stale approval"); err != nil {
		t.Fatalf("record closed source blocked audit: %v", err)
	}
	toolCloseRaw := (&IMMessageHandler{app: app}).toolExperienceLearning(map[string]interface{}{"action": "record_blocked_skill_draft_review", "trace_id": closedSource.TraceID, "resolution": "close"})
	if !strings.Contains(toolCloseRaw, "blocked_skill_draft_review") || !strings.Contains(toolCloseRaw, `"status":"blocked"`) {
		t.Fatalf("blocked skill close tool output missing review: %s", toolCloseRaw)
	}
	closedSnapshot := buildExperienceLearningSnapshot(nil, store)
	if closedSnapshot.ClosedSkillDraftReviewCount != 1 || len(closedSnapshot.SkillDraftReviewQueues.Closed) != 1 {
		t.Fatalf("closed queues = %#v", closedSnapshot.SkillDraftReviewQueues)
	}
}

func TestExperienceSkillDraftReviewQueuesTrackHistoryAndStaleBlocked(t *testing.T) {
	old := time.Now().UTC().AddDate(0, 0, -experienceBlockedSkillDraftStaleDays-1).Format(time.RFC3339)
	queues := buildExperienceSkillDraftReviewQueues([]ExperienceTraceDetail{
		{ID: "active", Kind: "skill_draft_review", FollowUpStatus: experienceFollowUpOutcomeCompleted, DraftID: "draft:active"},
		{ID: "blocked", Kind: "skill_draft_review", FollowUpStatus: experienceFollowUpOutcomeCompleted, DraftID: "draft:blocked", DraftExecutionStatus: skillDraftExecutionBlocked, DraftExecutionAt: old},
		{ID: "reopened", Kind: "skill_draft_review", FollowUpStatus: experienceFollowUpOutcomeCompleted, DraftID: "draft:reopened", DraftExecutionStatus: skillDraftExecutionReopened},
		{ID: "closed", Kind: "skill_draft_review", FollowUpStatus: experienceFollowUpOutcomeCompleted, DraftID: "draft:closed", DraftExecutionStatus: skillDraftExecutionClosed},
	})
	if len(queues.ApprovedUnpreviewed) != 1 || len(queues.Blocked) != 1 || len(queues.Reopened) != 1 || len(queues.Closed) != 1 {
		t.Fatalf("unexpected skill draft review queues: %#v", queues)
	}
	if !queues.Blocked[0].Stale || queues.Blocked[0].StaleDays < experienceBlockedSkillDraftStaleDays || queues.Blocked[0].StaleRecommendation == "" {
		t.Fatalf("blocked queue should carry stale policy: %#v", queues.Blocked[0])
	}
}

func TestRecordBlockedSkillDraftReviewPersistsReviewAndSourceTransitionInSQLiteBatch(t *testing.T) {
	dir := t.TempDir()
	store, err := memory.NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	backend, err := memory.NewSQLiteBackend(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatalf("NewSQLiteBackend: %v", err)
	}
	store.SetBackend(backend, memory.SyncConfig{Enabled: false})
	t.Cleanup(store.Stop)
	app := &App{memoryStore: store}
	source, err := app.RecordExperienceDraftReview(ExperienceDraftReviewRequest{
		Kind:    experienceDraftKindSkill,
		Status:  experienceFollowUpOutcomeCompleted,
		DraftID: "skill_draft:mark_needs_review:blocked-sqlite:",
	})
	if err != nil {
		t.Fatalf("record source review: %v", err)
	}
	if err := (&IMMessageHandler{app: app}).recordSkillDraftExecutionAudit([]string{source.TraceID}, skillDraftExecutionBlocked, "sqlite blocked source"); err != nil {
		t.Fatalf("record blocked audit: %v", err)
	}
	reopened, err := app.RecordBlockedSkillDraftReview(source.TraceID, "reopen", "skill_draft:mark_needs_review:replacement-sqlite:", "sqlite repair accepted", "reviewer")
	if err != nil {
		t.Fatalf("RecordBlockedSkillDraftReview: %v", err)
	}
	entries, err := backend.LoadAll()
	if err != nil {
		t.Fatalf("backend LoadAll: %v", err)
	}
	seen := map[string]memory.Entry{}
	for _, entry := range entries {
		seen[entry.ID] = entry
	}
	if _, ok := seen[reopened.MemoryID]; !ok {
		t.Fatalf("new repair review was not persisted in sqlite batch: ids=%v", memoryEntryMapKeys(seen))
	}
	sourceID := strings.TrimPrefix(source.TraceID, "memory:")
	sourceEntry, ok := seen[sourceID]
	if !ok {
		t.Fatalf("source review missing from sqlite backend: ids=%v", memoryEntryMapKeys(seen))
	}
	detail, ok := traceDetailFromMemoryEntry(sourceEntry)
	if !ok || detail.DraftExecutionStatus != skillDraftExecutionReopened {
		t.Fatalf("source transition not persisted atomically: %#v ok=%v", detail, ok)
	}
}

func memoryEntryMapKeys(values map[string]memory.Entry) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func TestRecordExperienceTraceFollowUpDeferredKeepsNextAction(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "Skill candidate",
		Content:    "Skill nudge candidate refactor_flow; sequence rg -> apply_patch -> go_test",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{"skill_nudge_candidate", "refactor_flow", "rg", "apply_patch", "go_test", experienceReviewStatusTagPrefix + experienceReviewOutcomeApproved, experienceReviewResolvedTag, "skill_nudge_reviewed"},
		SourceType: "tool_usage",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, "skill_nudge_candidate")
	app := &App{memoryStore: store}

	if _, err := app.RecordExperienceTraceFollowUp("memory:"+entry.ID, ExperienceTraceFollowUpRequest{Status: "later", Note: "needs two more examples"}); err != nil {
		t.Fatalf("RecordExperienceTraceFollowUp: %v", err)
	}

	snapshot := buildExperienceLearningSnapshot(nil, store)
	if snapshot.NextActionTraceCount != 1 || snapshot.NextActionKindCounts["draft_skill_manually"] != 1 {
		t.Fatalf("deferred follow-up should remain queued: %#v", snapshot)
	}
	if snapshot.FollowUpTraceCount != 1 || snapshot.FollowUpStatusCounts[experienceFollowUpOutcomeDeferred] != 1 {
		t.Fatalf("deferred follow-up counts not aggregated: %d/%#v", snapshot.FollowUpTraceCount, snapshot.FollowUpStatusCounts)
	}
	followUpSummary := mustFindExperienceFollowUpSummary(t, snapshot.FollowUpSummaries, experienceFollowUpOutcomeDeferred)
	if followUpSummary.Count != 1 || followUpSummary.LatestActionKind != "draft_skill_manually" || !strings.Contains(followUpSummary.LatestNote, "two more examples") {
		t.Fatalf("deferred follow-up summary not aggregated: %#v", followUpSummary)
	}
	for _, detail := range snapshot.TraceDetails {
		if detail.Kind == "skill_nudge_review" && (detail.FollowUpStatus != experienceFollowUpOutcomeDeferred || detail.FollowUpActionKind != "draft_skill_manually" || detail.NextActionKind != "draft_skill_manually" || detail.FollowUpCount != 1) {
			t.Fatalf("deferred follow-up audit not surfaced: %#v", detail)
		}
	}
}

func TestRecordExperienceTraceFollowUpTriggeredRollbackReviewKeepsAuditKind(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "A2A: triggered rollback review",
		Content:    "A2A discussion result\nRollback on:\n- gate fails\nMatched rollback triggers:\n- gate fails",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{groupDiscussionResultTag, "discussion:disc-1", groupDiscussionRollbackTag, groupDiscussionRollbackTriggered, experienceReviewStatusTagPrefix + experienceReviewOutcomeApproved, experienceReviewResolvedTag},
		SourceType: groupDiscussionMemorySourceType,
		SourceURL:  "a2a://current_hub/disc-1",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, groupDiscussionRollbackTriggered)
	app := &App{memoryStore: store}

	if _, err := app.RecordExperienceTraceFollowUp("memory:"+entry.ID, ExperienceTraceFollowUpRequest{Status: "blocked", Note: "owner rejected rollback path"}); err != nil {
		t.Fatalf("RecordExperienceTraceFollowUp: %v", err)
	}

	snapshot := buildExperienceLearningSnapshot(nil, store)
	if snapshot.FollowUpTraceCount != 1 || snapshot.FollowUpStatusCounts[experienceFollowUpOutcomeBlocked] != 1 {
		t.Fatalf("triggered rollback follow-up counts not aggregated: %#v", snapshot)
	}
	followUpSummary := mustFindExperienceFollowUpSummary(t, snapshot.FollowUpSummaries, experienceFollowUpOutcomeBlocked)
	if followUpSummary.Count != 1 || followUpSummary.LatestActionKind != "draft_rollback_workflow" || !strings.Contains(followUpSummary.LatestNote, "owner rejected rollback path") {
		t.Fatalf("unexpected triggered rollback follow-up summary: %#v", followUpSummary)
	}
	found := false
	for _, detail := range snapshot.TraceDetails {
		if detail.Kind != "a2a_rollback_review" {
			continue
		}
		found = true
		if detail.FollowUpStatus != experienceFollowUpOutcomeBlocked || detail.FollowUpActionKind != "draft_rollback_workflow" || detail.NextActionKind != "" || detail.FollowUpCount != 1 {
			t.Fatalf("unexpected triggered rollback follow-up detail: %#v", detail)
		}
	}
	if !found {
		t.Fatalf("missing triggered rollback trace detail: %#v", snapshot.TraceDetails)
	}

	result := app.QueryExperienceFollowUpActions(ExperienceTraceDetailQuery{
		Filter:                "followups",
		FollowUpActionKind:    "draft_rollback_workflow",
		TriggeredRollbackOnly: true,
		Kind:                  "a2a_rollback_review",
		Limit:                 5,
	})
	if result.Count != 1 || result.Returned != 1 || !result.Query.TriggeredRollbackOnly {
		t.Fatalf("unexpected triggered rollback follow-up query result: %#v", result)
	}
	if len(result.FollowUpActionSummaries) != 1 || !result.FollowUpActionSummaries[0].TriggeredRollback || result.FollowUpActionSummaries[0].TriggeredCount != 1 {
		t.Fatalf("triggered rollback follow-up action summaries should carry audit markers: %#v", result.FollowUpActionSummaries)
	}
	if result.FollowUpActionSummaries[0].RecommendedTraceID == "" || result.FollowUpActionSummaries[0].RecommendedTitle == "" {
		t.Fatalf("triggered rollback follow-up action summaries should carry recommended trace metadata: %#v", result.FollowUpActionSummaries)
	}
	if !strings.Contains(result.FollowUpActionSummaries[0].RecommendedReason, "rollback-trigger") {
		t.Fatalf("triggered rollback follow-up action summaries should carry recommended trace reason: %#v", result.FollowUpActionSummaries)
	}
	if len(result.FollowUpSummaries) != 1 || !result.FollowUpSummaries[0].TriggeredRollback || result.FollowUpSummaries[0].TriggeredCount != 1 {
		t.Fatalf("triggered rollback follow-up summaries should carry audit markers: %#v", result.FollowUpSummaries)
	}
	if result.FollowUpSummaries[0].RecommendedTraceID == "" || result.FollowUpSummaries[0].RecommendedTitle == "" {
		t.Fatalf("triggered rollback follow-up summaries should carry recommended trace metadata: %#v", result.FollowUpSummaries)
	}
	if !strings.Contains(result.FollowUpSummaries[0].RecommendedReason, "rollback-trigger") {
		t.Fatalf("triggered rollback follow-up summaries should carry recommended trace reason: %#v", result.FollowUpSummaries)
	}
	if result.RecommendedTraceID == "" || result.RecommendedTraceTitle == "" || !strings.Contains(result.RecommendedReason, "triggered rollback") {
		t.Fatalf("triggered rollback follow-up query should expose a recommended trace: %#v", result)
	}
}

func TestExperienceLearningToolBuildsNonExecutingFollowUp(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "Skill candidate",
		Content:    "Skill nudge candidate refactor_flow; sequence rg -> apply_patch -> go_test",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{"skill_nudge_candidate", "refactor_flow", "rg", "apply_patch", "go_test", experienceReviewStatusTagPrefix + experienceReviewOutcomeApproved, experienceReviewResolvedTag, "skill_nudge_reviewed"},
		SourceType: "tool_usage",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, "skill_nudge_candidate")
	handler := &IMMessageHandler{app: &App{memoryStore: store}}

	result := handler.toolExperienceLearning(map[string]interface{}{"action": "build_followup", "trace_id": "memory:" + entry.ID})
	if !strings.Contains(result, `"ok":true`) || !strings.Contains(result, "Manual skill draft brief") || strings.Contains(result, "install skills automatically") || !strings.Contains(result, "recommended_focus_context") || !strings.Contains(result, "non_executing_boundary") {
		t.Fatalf("unexpected experience_learning tool result: %s", result)
	}
	assertExperienceLearningToolTopLevelHandoff(t, result, "trace_details")
}

func TestExperienceLearningToolBuildsNonExecutingSkillDraft(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "Skill candidate",
		Content:    "Skill nudge candidate browser_flow; sequence browser_observe -> browser_click; tokens [browser]",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{"skill_nudge_candidate", "browser_flow", experienceReviewStatusTagPrefix + experienceReviewOutcomeApproved, experienceReviewResolvedTag, "skill_nudge_reviewed"},
		SourceType: "tool_usage",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, "skill_nudge_candidate")
	handler := &IMMessageHandler{app: &App{memoryStore: store}}

	result := handler.toolExperienceLearning(map[string]interface{}{"action": "build_skill_draft", "trace_id": "memory:" + entry.ID})
	if !strings.Contains(result, `"ok":false`) || !strings.Contains(result, "not a skill nudge review") {
		t.Fatalf("legacy browser skill draft should be rejected: %s", result)
	}
}

func TestExperienceLearningToolBuildsNonExecutingRollbackDraft(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "A2A: rollback review",
		Content:    "A2A discussion result\nSummary: use proposal\nRollback on:\n- evidence fails",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{groupDiscussionResultTag, "discussion:disc-1", groupDiscussionRollbackTag, experienceReviewStatusTagPrefix + experienceReviewOutcomeApproved, experienceReviewResolvedTag},
		SourceType: groupDiscussionMemorySourceType,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, groupDiscussionRollbackTag)
	handler := &IMMessageHandler{app: &App{memoryStore: store}}

	result := handler.toolExperienceLearning(map[string]interface{}{"action": "build_rollback_draft", "trace_id": "memory:" + entry.ID})
	if !strings.Contains(result, `"ok":true`) || !strings.Contains(result, "Manual Rollback Workflow Draft") || !strings.Contains(result, "read-only rollback workflow draft only") || !strings.Contains(result, "recommended_focus_context") {
		t.Fatalf("unexpected experience_learning rollback draft result: %s", result)
	}
	assertExperienceLearningToolTopLevelHandoff(t, result, "trace_details")
}

func TestExperienceLearningToolBuildsNonExecutingEscalationBrief(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "A2A escalation",
		Content:    "A2A discussion result\nEscalation:\n- Reason: needs owner\n- Target: human_owner\n- Raised by: agent-a",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{groupDiscussionResultTag, "discussion:disc-1", "has_escalation"},
		SourceType: groupDiscussionMemorySourceType,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, "has_escalation")
	handler := &IMMessageHandler{app: &App{memoryStore: store}}

	result := handler.toolExperienceLearning(map[string]interface{}{"action": "build_escalation_brief", "trace_id": "memory:" + entry.ID})
	if !strings.Contains(result, `"ok":true`) || !strings.Contains(result, "A2A Escalation Handoff Brief") || !strings.Contains(result, "read-only escalation brief only") || !strings.Contains(result, "recommended_focus_context") {
		t.Fatalf("unexpected experience_learning escalation brief result: %s", result)
	}
	assertExperienceLearningToolTopLevelHandoff(t, result, "trace_details")
}

func TestExperienceLearningToolBuildsNonExecutingConflictDraft(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title: "A2A conflict review",
		Content: strings.Join([]string{
			"A2A conflict review candidate",
			"Topic: release",
			"Question: ship?",
			"New discussion: disc-new",
			"New summary: choose option A",
			"Existing memory: disc-old",
			"Existing summary: reject option A",
		}, "\n"),
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{groupDiscussionConflictTag, "topic:release", experienceReviewStatusTagPrefix + experienceReviewOutcomeApproved, experienceReviewResolvedTag, "conflict_reviewed"},
		SourceType: groupDiscussionMemorySourceType,
		SourceURL:  "a2a://current_hub/disc-new",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, groupDiscussionConflictTag)
	handler := &IMMessageHandler{app: &App{memoryStore: store}}

	result := handler.toolExperienceLearning(map[string]interface{}{"action": "build_conflict_draft", "trace_id": "memory:" + entry.ID})
	if !strings.Contains(result, `"ok":true`) || !strings.Contains(result, "A2A Conflict Reconciliation Draft") || !strings.Contains(result, "read-only conflict reconciliation draft only") || !strings.Contains(result, "recommended_focus_context") {
		t.Fatalf("unexpected experience_learning conflict draft result: %s", result)
	}
	assertExperienceLearningToolTopLevelHandoff(t, result, "trace_details")
}

func TestExperienceLearningToolTraceDetailsFiltersGovernanceQueues(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "A2A: rollback review",
		Content:    "A2A discussion result\nRollback on:\n- gate fails",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{groupDiscussionResultTag, "discussion:disc-1", groupDiscussionRollbackTag, experienceReviewRequiredTag},
		SourceType: groupDiscussionMemorySourceType,
	}); err != nil {
		t.Fatalf("Save rollback: %v", err)
	}
	if err := store.Save(memory.Entry{
		Title:      "Skill candidate",
		Content:    "Skill nudge candidate refactor_flow; sequence rg -> apply_patch -> go_test",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{"skill_nudge_candidate", "refactor_flow", "rg", "apply_patch", "go_test", experienceReviewStatusTagPrefix + experienceReviewOutcomeApproved, experienceReviewResolvedTag, "skill_nudge_reviewed"},
		SourceType: "tool_usage",
	}); err != nil {
		t.Fatalf("Save skill: %v", err)
	}
	handler := &IMMessageHandler{app: &App{memoryStore: store}}

	requiredResult := parseExperienceTraceDetailsToolResult(t, handler.toolExperienceLearning(map[string]interface{}{"action": "trace_details", "filter": "review", "review_status": "required", "limit": 1}))
	if !requiredResult.OK || requiredResult.Count != 1 || requiredResult.Returned != 1 || requiredResult.Query.Limit != 1 {
		t.Fatalf("unexpected required trace detail result: %#v", requiredResult)
	}
	if requiredResult.TraceDetails[0].Kind != "a2a_rollback_review" || !requiredResult.TraceDetails[0].ReviewRequired {
		t.Fatalf("required trace details should include rollback review: %#v", requiredResult.TraceDetails)
	}

	actionResult := parseExperienceTraceDetailsToolResult(t, handler.toolExperienceLearning(map[string]interface{}{"action": "trace_details", "filter": "actions", "action_kind": "draft_skill_manually", "q": "browser"}))
	if !actionResult.OK || actionResult.Count != 0 || len(actionResult.TraceDetails) != 0 {
		t.Fatalf("legacy browser skill nudges should not queue skill drafts: %#v", actionResult)
	}
	if actionResult.RecommendedToolCall != nil {
		t.Fatalf("empty legacy browser skill draft queue should not expose draft handoff: %#v", actionResult.RecommendedToolCall)
	}
	if !strings.Contains(actionResult.NonExecutingBoundary, "read-only trace detail inspection") {
		t.Fatalf("trace_details should expose top-level non-executing boundary: %q", actionResult.NonExecutingBoundary)
	}
}

func TestExperienceLearningToolQueuesReturnsBoundedGovernanceDetails(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "A2A: rollback review",
		Content:    "A2A discussion result\nRollback on:\n- gate fails",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{groupDiscussionResultTag, "discussion:disc-1", groupDiscussionRollbackTag, experienceReviewRequiredTag},
		SourceType: groupDiscussionMemorySourceType,
	}); err != nil {
		t.Fatalf("Save rollback: %v", err)
	}
	if err := store.Save(memory.Entry{
		Title:      "Skill candidate",
		Content:    "Skill nudge candidate refactor_flow; sequence rg -> apply_patch -> go_test",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{"skill_nudge_candidate", "refactor_flow", "rg", "apply_patch", "go_test", experienceReviewStatusTagPrefix + experienceReviewOutcomeApproved, experienceReviewResolvedTag, "skill_nudge_reviewed"},
		SourceType: "tool_usage",
	}); err != nil {
		t.Fatalf("Save skill: %v", err)
	}
	if err := store.Save(memory.Entry{
		Title:      "A2A conflict review",
		Content:    "A2A conflict review candidate",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{groupDiscussionConflictTag, "topic:release", experienceReviewStatusTagPrefix + experienceReviewOutcomeApproved, experienceReviewResolvedTag, "conflict_reviewed"},
		SourceType: groupDiscussionMemorySourceType,
	}); err != nil {
		t.Fatalf("Save conflict: %v", err)
	}
	conflict := mustFindExperienceReviewEntry(t, store, groupDiscussionConflictTag)
	app := &App{memoryStore: store}
	if _, err := app.RecordExperienceTraceFollowUp("memory:"+conflict.ID, ExperienceTraceFollowUpRequest{Status: "blocked", Note: "owner rejected reconciliation"}); err != nil {
		t.Fatalf("RecordExperienceTraceFollowUp: %v", err)
	}
	handler := &IMMessageHandler{app: app}

	payload := parseExperienceQueuesToolResult(t, handler.toolExperienceLearning(map[string]interface{}{"action": "queues", "limit": 1}))
	if !payload.OK || payload.Limit != 1 {
		t.Fatalf("unexpected queue payload header: %#v", payload)
	}
	if payload.ReviewQueueCount != 1 || len(payload.ReviewQueue) != 1 || payload.ReviewQueue[0].Kind != "a2a_rollback_review" {
		t.Fatalf("review queue should expose bounded rollback review details: %#v", payload)
	}
	if payload.NextActionQueueCount != 1 || len(payload.NextActionQueue) != 1 || payload.NextActionQueue[0].NextActionKind != "draft_skill_manually" {
		t.Fatalf("next action queue should expose bounded skill draft details: %#v", payload)
	}
	if payload.FollowUpQueueCount != 1 || len(payload.FollowUpQueue) != 1 || payload.FollowUpQueue[0].FollowUpStatus != experienceFollowUpOutcomeBlocked {
		t.Fatalf("follow-up queue should expose bounded audit details: %#v", payload)
	}
	if len(payload.ReviewSummaries) == 0 || len(payload.NextActionSummaries) == 0 || len(payload.FollowUpSummaries) == 0 || len(payload.FollowUpActionSummaries) == 0 {
		t.Fatalf("queues should include compact summaries: %#v", payload)
	}
	if payload.FollowUpActionCounts["resolve_a2a_conflict_manually"] != 1 || payload.FollowUpActionSummaries[0].Kind != "resolve_a2a_conflict_manually" {
		t.Fatalf("queues should include follow-up action aggregation: %#v/%#v", payload.FollowUpActionCounts, payload.FollowUpActionSummaries)
	}
	if payload.RecommendedNextAction != "review_required_traces" || payload.RecommendedToolCall["tool"] != "experience_learning" || payload.NonExecutingBoundary == "" {
		t.Fatalf("queues should include safe recommended action/call boundary: %#v", payload)
	}
}

func TestExperienceLearningToolRecordsFollowUpAuditOnly(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "Skill candidate",
		Content:    "Skill nudge candidate refactor_flow; sequence rg -> apply_patch -> go_test",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{"skill_nudge_candidate", "refactor_flow", "rg", "apply_patch", "go_test", experienceReviewStatusTagPrefix + experienceReviewOutcomeApproved, experienceReviewResolvedTag, "skill_nudge_reviewed"},
		SourceType: "tool_usage",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, "skill_nudge_candidate")
	app := &App{memoryStore: store}
	handler := &IMMessageHandler{app: app}

	result := handler.toolExperienceLearning(map[string]interface{}{"action": "record_followup", "trace_id": "memory:" + entry.ID, "status": "completed", "note": "draft created by owner", "actor": "owner"})
	payload := parseExperienceRecordFollowUpToolResult(t, result)
	if !payload.OK || payload.TraceID != "memory:"+entry.ID || payload.Status != experienceFollowUpOutcomeCompleted {
		t.Fatalf("unexpected record_followup payload: %#v", payload)
	}
	if payload.FollowUpRecord.TraceID != "memory:"+entry.ID || payload.FollowUpRecord.Status != experienceFollowUpOutcomeCompleted || payload.FollowUpRecord.ActionKind != "draft_skill_manually" {
		t.Fatalf("record_followup should return structured followup_record: %#v", payload.FollowUpRecord)
	}
	assertExperienceRecommendedFocusContext(t, payload.FollowUpRecord.RecommendedFocusContext, "memory:"+entry.ID, "Skill candidate", "follow-up audit")
	assertExperienceInspectionRecommendedToolCall(t, payload.FollowUpRecord.RecommendedToolCall, "memory:"+entry.ID)
	if payload.RecommendedFocusContext["priority_trace_id"] != payload.FollowUpRecord.RecommendedFocusContext["priority_trace_id"] {
		t.Fatalf("top-level recommended focus should mirror followup_record: payload=%#v record=%#v", payload.RecommendedFocusContext, payload.FollowUpRecord.RecommendedFocusContext)
	}
	if payload.NonExecutingBoundary == "" || payload.NonExecutingBoundary != payload.FollowUpRecord.NonExecutingBoundary || !strings.Contains(payload.NonExecutingBoundary, "follow-up audit record only") {
		t.Fatalf("record_followup should mirror non-executing boundary: %#v", payload)
	}
	snapshot := app.GetExperienceLearningSnapshot()
	if snapshot.NextActionTraceCount != 0 || snapshot.FollowUpStatusCounts[experienceFollowUpOutcomeCompleted] != 1 {
		t.Fatalf("record_followup should close only audit action state: %#v", snapshot)
	}
	summary := mustFindExperienceFollowUpSummary(t, snapshot.FollowUpSummaries, experienceFollowUpOutcomeCompleted)
	if summary.LatestActionKind != "draft_skill_manually" || !strings.Contains(summary.LatestNote, "draft created") {
		t.Fatalf("record_followup summary missing audit detail: %#v", summary)
	}
}

func assertExperienceRecommendedFocusContext(t *testing.T, context map[string]interface{}, traceID, title, reasonContains string) {
	t.Helper()
	if context == nil {
		t.Fatalf("expected recommended focus context for %s", traceID)
	}
	if got, _ := context["priority_trace_id"].(string); got != traceID {
		t.Fatalf("unexpected priority trace id: got %q want %q in %#v", got, traceID, context)
	}
	if got, _ := context["priority_trace_title"].(string); got != title {
		t.Fatalf("unexpected priority trace title: got %q want %q in %#v", got, title, context)
	}
	if got, _ := context["reason"].(string); !strings.Contains(got, reasonContains) {
		t.Fatalf("unexpected priority trace reason: got %q want substring %q in %#v", got, reasonContains, context)
	}
}

func assertExperienceInspectionRecommendedToolCall(t *testing.T, call map[string]interface{}, traceID string) {
	t.Helper()
	args, ok := call["args"].(map[string]interface{})
	if !ok ||
		call["tool"] != "experience_learning" ||
		call["non_executing"] != true ||
		args["action"] != "trace_details" ||
		args["trace_id"] != traceID {
		t.Fatalf("expected safe trace inspection recommended tool call for %s: %#v", traceID, call)
	}
	focus, ok := call["recommended_focus_context"].(map[string]interface{})
	if !ok || focus["priority_trace_id"] != traceID {
		t.Fatalf("expected trace inspection recommended focus context for %s: %#v", traceID, call)
	}
	governanceFocus, ok := call["governance_focus_context"].(map[string]interface{})
	if !ok || governanceFocus["priority_trace_id"] != traceID {
		t.Fatalf("expected trace inspection governance focus alias for %s: %#v", traceID, call)
	}
	if strings.TrimSpace(fmt.Sprint(call["non_executing_boundary"])) == "" {
		t.Fatalf("expected non-empty non-executing boundary for %s: %#v", traceID, call)
	}
}

type experienceTraceDetailsToolPayload struct {
	OK                      bool                       `json:"ok"`
	Count                   int                        `json:"count"`
	Returned                int                        `json:"returned"`
	Total                   int                        `json:"total"`
	RecommendedTraceID      string                     `json:"recommended_trace_id"`
	RecommendedTraceTitle   string                     `json:"recommended_trace_title"`
	RecommendedReason       string                     `json:"recommended_reason"`
	RecommendedFocusContext map[string]interface{}     `json:"recommended_focus_context"`
	RecommendedToolCall     map[string]interface{}     `json:"recommended_tool_call"`
	NonExecutingBoundary    string                     `json:"non_executing_boundary"`
	Query                   ExperienceTraceDetailQuery `json:"query"`
	TraceDetails            []ExperienceTraceDetail    `json:"trace_details"`
}

func parseExperienceTraceDetailsToolResult(t *testing.T, raw string) experienceTraceDetailsToolPayload {
	t.Helper()
	var payload experienceTraceDetailsToolPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal trace details result: %v\n%s", err, raw)
	}
	return payload
}

type experienceQueuesToolPayload struct {
	OK                      bool                              `json:"ok"`
	RecommendedNextAction   string                            `json:"recommended_next_action"`
	RecommendedToolCall     map[string]interface{}            `json:"recommended_tool_call"`
	NonExecutingBoundary    string                            `json:"non_executing_boundary"`
	Limit                   int                               `json:"limit"`
	ReviewQueueCount        int                               `json:"review_queue_count"`
	ReviewQueue             []ExperienceTraceDetail           `json:"review_queue"`
	ReviewSummaries         []ExperienceReviewSummary         `json:"review_summaries"`
	NextActionQueueCount    int                               `json:"next_action_queue_count"`
	NextActionQueue         []ExperienceTraceDetail           `json:"next_action_queue"`
	NextActionSummaries     []ExperienceNextActionSummary     `json:"next_action_summaries"`
	FollowUpQueueCount      int                               `json:"follow_up_queue_count"`
	FollowUpQueue           []ExperienceTraceDetail           `json:"follow_up_queue"`
	FollowUpSummaries       []ExperienceFollowUpSummary       `json:"follow_up_summaries"`
	FollowUpActionCounts    map[string]int                    `json:"follow_up_action_counts"`
	FollowUpActionSummaries []ExperienceFollowUpActionSummary `json:"follow_up_action_summaries"`
}

type experienceRecordFollowUpToolPayload struct {
	OK                      bool                          `json:"ok"`
	TraceID                 string                        `json:"trace_id"`
	Status                  string                        `json:"status"`
	RecommendedFocusContext map[string]interface{}        `json:"recommended_focus_context"`
	RecommendedToolCall     map[string]interface{}        `json:"recommended_tool_call"`
	NonExecutingBoundary    string                        `json:"non_executing_boundary"`
	FollowUpRecord          ExperienceTraceFollowUpRecord `json:"followup_record"`
}

func parseExperienceRecordFollowUpToolResult(t *testing.T, raw string) experienceRecordFollowUpToolPayload {
	t.Helper()
	var payload experienceRecordFollowUpToolPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal record follow-up result: %v\n%s", err, raw)
	}
	return payload
}

func parseExperienceQueuesToolResult(t *testing.T, raw string) experienceQueuesToolPayload {
	t.Helper()
	var payload experienceQueuesToolPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal queues result: %v\n%s", err, raw)
	}
	return payload
}

func TestExperienceLearningToolNextActionsIncludesFollowUpAuditCounts(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "A2A: rollback review",
		Content:    "A2A discussion result\nRollback on:\n- gate fails\nMatched rollback triggers:\n- gate fails",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{groupDiscussionResultTag, "discussion:disc-1", groupDiscussionRollbackTag, groupDiscussionRollbackTriggered, experienceReviewStatusTagPrefix + experienceReviewOutcomeApproved, experienceReviewResolvedTag},
		SourceType: groupDiscussionMemorySourceType,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, groupDiscussionRollbackTag)
	app := &App{memoryStore: store}
	if _, err := app.RecordExperienceTraceFollowUp("memory:"+entry.ID, ExperienceTraceFollowUpRequest{Status: "blocked", Note: "owner rejected trigger"}); err != nil {
		t.Fatalf("RecordExperienceTraceFollowUp: %v", err)
	}
	handler := &IMMessageHandler{app: app}

	result := handler.toolExperienceLearning(map[string]interface{}{"action": "next_actions"})
	if !strings.Contains(result, `"ok":true`) || !strings.Contains(result, `"recommended_tool_call"`) || !strings.Contains(result, `"non_executing_boundary"`) || !strings.Contains(result, `"review_summaries"`) || !strings.Contains(result, `"follow_up_trace_count":1`) || !strings.Contains(result, `"blocked":1`) || !strings.Contains(result, `"follow_up_summaries"`) || !strings.Contains(result, `"follow_up_action_summaries"`) || !strings.Contains(result, `"draft_rollback_workflow":1`) || !strings.Contains(result, `"latest_action_kind":"draft_rollback_workflow"`) {
		t.Fatalf("next_actions should include follow-up audit counts and summaries: %s", result)
	}
	if !strings.Contains(result, `"triggered_rollback":true`) || !strings.Contains(result, `"triggered_count":1`) {
		t.Fatalf("next_actions should preserve triggered rollback audit markers: %s", result)
	}
}

func TestExperienceLearningDirectSafeHandoffsAreNormalized(t *testing.T) {
	t.Parallel()

	followup := finalizeExperienceTraceFollowUpDraft(ExperienceTraceFollowUpDraft{
		TraceID:                 "memory:trace-1",
		Title:                   "Trace 1",
		RecommendedFocusContext: map[string]interface{}{"priority_trace_id": "memory:trace-1", "reason": "manual follow-up draft"},
		RecommendedToolCall: map[string]interface{}{
			"tool": "experience_learning",
			"args": map[string]interface{}{"action": "trace_details", "trace_id": "memory:trace-1"},
		},
		NonExecutingBoundary: "manual follow-up draft only",
	})
	assertExperienceInspectionRecommendedToolCall(t, followup.RecommendedToolCall, "memory:trace-1")

	record := finalizeExperienceTraceFollowUpRecord(ExperienceTraceFollowUpRecord{
		TraceID:                 "memory:trace-2",
		RecommendedFocusContext: map[string]interface{}{"priority_trace_id": "memory:trace-2", "reason": "follow-up audit"},
		RecommendedToolCall: map[string]interface{}{
			"tool": "experience_learning",
			"args": map[string]interface{}{"action": "trace_details", "trace_id": "memory:trace-2"},
		},
		NonExecutingBoundary: "follow-up audit record only",
	})
	assertExperienceInspectionRecommendedToolCall(t, record.RecommendedToolCall, "memory:trace-2")

	skill := finalizeExperienceSkillDraft(ExperienceSkillDraft{
		TraceID:                 "memory:trace-3",
		Title:                   "Skill trace",
		RecommendedFocusContext: map[string]interface{}{"priority_trace_id": "memory:trace-3", "reason": "approved skill nudge"},
		RecommendedToolCall: map[string]interface{}{
			"tool": "experience_learning",
			"args": map[string]interface{}{"action": "trace_details", "trace_id": "memory:trace-3"},
		},
		NonExecutingBoundary: "manual skill draft only",
	})
	assertExperienceInspectionRecommendedToolCall(t, skill.RecommendedToolCall, "memory:trace-3")

	rollback := finalizeExperienceRollbackWorkflowDraft(ExperienceRollbackWorkflowDraft{
		TraceID:                 "memory:trace-4",
		Title:                   "Rollback trace",
		RecommendedFocusContext: map[string]interface{}{"priority_trace_id": "memory:trace-4", "reason": "approved rollback review"},
		RecommendedToolCall: map[string]interface{}{
			"tool": "experience_learning",
			"args": map[string]interface{}{"action": "trace_details", "trace_id": "memory:trace-4"},
		},
		NonExecutingBoundary: "manual rollback workflow draft only",
	})
	assertExperienceInspectionRecommendedToolCall(t, rollback.RecommendedToolCall, "memory:trace-4")

	brief := finalizeExperienceEscalationBrief(ExperienceEscalationBrief{
		TraceID:                 "memory:trace-5",
		Title:                   "Escalation trace",
		RecommendedFocusContext: map[string]interface{}{"priority_trace_id": "memory:trace-5", "reason": "escalation handoff briefing"},
		RecommendedToolCall: map[string]interface{}{
			"tool": "experience_learning",
			"args": map[string]interface{}{"action": "trace_details", "trace_id": "memory:trace-5"},
		},
		NonExecutingBoundary: "escalation handoff brief only",
	})
	assertExperienceInspectionRecommendedToolCall(t, brief.RecommendedToolCall, "memory:trace-5")

	conflict := finalizeExperienceConflictReconciliationDraft(ExperienceConflictReconciliationDraft{
		TraceID:                 "memory:trace-6",
		Title:                   "Conflict trace",
		RecommendedFocusContext: map[string]interface{}{"priority_trace_id": "memory:trace-6", "reason": "approved conflict review"},
		RecommendedToolCall: map[string]interface{}{
			"tool": "experience_learning",
			"args": map[string]interface{}{"action": "trace_details", "trace_id": "memory:trace-6"},
		},
		NonExecutingBoundary: "conflict reconciliation draft only",
	})
	assertExperienceInspectionRecommendedToolCall(t, conflict.RecommendedToolCall, "memory:trace-6")

	draftReview := finalizeExperienceDraftReviewRecord(ExperienceDraftReviewRecord{
		TraceID:                 "memory:trace-7",
		RecommendedFocusContext: map[string]interface{}{"priority_trace_id": "memory:trace-7", "reason": "draft review audit"},
		RecommendedToolCall: map[string]interface{}{
			"tool": "experience_learning",
			"args": map[string]interface{}{"action": "trace_details", "trace_id": "memory:trace-7"},
		},
		NonExecutingBoundary: "draft review audit record only",
	})
	assertExperienceInspectionRecommendedToolCall(t, draftReview.RecommendedToolCall, "memory:trace-7")
}
