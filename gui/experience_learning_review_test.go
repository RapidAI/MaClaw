package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestReviewExperienceTraceApprovesRollbackMemory(t *testing.T) {
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
		SourceURL:  "a2a://current_hub/disc-1",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, groupDiscussionRollbackTag)
	app := &App{memoryStore: store, configCache: corelib.AppConfig{RemoteMachineID: "machine-1"}, configCacheValid: true}

	record, err := app.ReviewExperienceTrace("memory:"+entry.ID, ExperienceTraceReviewRequest{Outcome: "approved", Note: "validated rollback gate", Reviewer: "owner"})
	if err != nil {
		t.Fatalf("ReviewExperienceTrace: %v", err)
	}
	if record.Outcome != experienceReviewOutcomeApproved || record.Kind != "rollback" {
		t.Fatalf("unexpected review record: %#v", record)
	}
	assertExperienceInspectionRecommendedToolCall(t, record.RecommendedToolCall, "memory:"+entry.ID)
	if !strings.Contains(record.NonExecutingBoundary, "manual review audit record only") || !strings.Contains(record.NonExecutingBoundary, "no rollback ran") {
		t.Fatalf("review record should expose non-executing boundary: %#v", record)
	}

	updated := mustFindExperienceReviewEntry(t, store, groupDiscussionRollbackTag)
	if hasTag(updated.Tags, experienceReviewRequiredTag) || !hasTag(updated.Tags, experienceReviewResolvedTag) || !hasTag(updated.Tags, experienceReviewStatusTagPrefix+experienceReviewOutcomeApproved) || !hasTag(updated.Tags, "rollback_reviewed") {
		t.Fatalf("unexpected approved tags: %#v", updated.Tags)
	}
	if !strings.Contains(updated.Content, "validated rollback gate") || !strings.Contains(updated.Content, "no skill, rollback, routing, or policy change") {
		t.Fatalf("review record not appended: %q", updated.Content)
	}
	snapshot := buildExperienceLearningSnapshot(nil, store)
	if snapshot.ReviewRequiredTraceCount != 0 {
		t.Fatalf("review required count = %d, want 0", snapshot.ReviewRequiredTraceCount)
	}
	if snapshot.ReviewStatusCounts[experienceReviewOutcomeApproved] != 1 {
		t.Fatalf("approved status count = %#v, want 1 approved", snapshot.ReviewStatusCounts)
	}
	if reviewSummary := mustFindExperienceReviewSummary(t, snapshot.ReviewSummaries, experienceReviewOutcomeApproved); reviewSummary.Count != 1 || reviewSummary.RequiredCount != 0 || reviewSummary.LatestReviewer != "owner" || reviewSummary.LatestNote != "validated rollback gate" || reviewSummary.LatestKind != "a2a_rollback_review" {
		t.Fatalf("unexpected approved review summary: %#v", reviewSummary)
	}
	if snapshot.NextActionTraceCount != 1 || snapshot.NextActionKindCounts["draft_rollback_workflow"] != 1 {
		t.Fatalf("approved next action counts = %d/%#v, want draft rollback workflow", snapshot.NextActionTraceCount, snapshot.NextActionKindCounts)
	}
	if summary := mustFindExperienceNextActionSummary(t, snapshot.NextActionSummaries, "draft_rollback_workflow"); summary.Count != 1 || summary.LatestTraceID == "" || !strings.Contains(summary.LatestAction, "rollback workflow") {
		t.Fatalf("unexpected rollback action summary: %#v", summary)
	}
	foundDetail := false
	for _, detail := range snapshot.TraceDetails {
		if detail.Kind == "a2a_rollback_review" && (detail.ReviewRequired || detail.ReviewAction != "" || detail.ReviewStatus != experienceReviewOutcomeApproved || detail.ReviewedAt == "") {
			t.Fatalf("approved rollback should stay visible but not pending: %#v", detail)
		}
		if detail.Kind == "a2a_rollback_review" {
			foundDetail = true
			if detail.Reviewer != "owner" || detail.ReviewNote != "validated rollback gate" || detail.ReviewCount != 1 {
				t.Fatalf("approved rollback should expose structured review audit: %#v", detail)
			}
			if detail.NextActionKind != "draft_rollback_workflow" || !strings.Contains(detail.NextAction, "human-approved rollback workflow") {
				t.Fatalf("approved rollback should expose safe next action: %#v", detail)
			}
		}
	}
	if !foundDetail {
		t.Fatalf("missing approved rollback detail: %#v", snapshot.TraceDetails)
	}
}

func TestExperienceLearningToolRecordsReviewWithSafeHandoff(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "A2A conflict review",
		Content:    "A2A conflict review candidate",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{groupDiscussionConflictTag, experienceReviewRequiredTag},
		SourceType: groupDiscussionMemorySourceType,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, groupDiscussionConflictTag)
	handler := &IMMessageHandler{app: &App{memoryStore: store}}

	result := handler.toolExperienceLearning(map[string]interface{}{
		"action":   "record_review",
		"trace_id": "memory:" + entry.ID,
		"outcome":  "deferred",
		"note":     "waiting for owner",
		"reviewer": "review-chair",
	})
	if !strings.Contains(result, `"ok":true`) || !strings.Contains(result, `"review_record"`) || !strings.Contains(result, `"outcome":"deferred"`) {
		t.Fatalf("unexpected record_review result: %s", result)
	}
	if !strings.Contains(result, `"recommended_focus_context"`) || !strings.Contains(result, `"recommended_tool_call"`) || !strings.Contains(result, `"non_executing_boundary"`) {
		t.Fatalf("record_review should mirror safe handoff fields: %s", result)
	}
	if !strings.Contains(result, "manual review audit record only") || !strings.Contains(result, "no rollback ran") {
		t.Fatalf("record_review should remain audit-only: %s", result)
	}

	updated := mustFindExperienceReviewEntry(t, store, groupDiscussionConflictTag)
	if !hasTag(updated.Tags, experienceReviewRequiredTag) || !hasTag(updated.Tags, experienceReviewStatusTagPrefix+experienceReviewOutcomeDeferred) {
		t.Fatalf("deferred record_review should keep review required with deferred status: %#v", updated.Tags)
	}
}

func TestReviewExperienceTraceDefersConflictMemory(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "A2A conflict review",
		Content:    "A2A conflict review candidate",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{groupDiscussionConflictTag, "topic:release", experienceReviewRequiredTag},
		SourceType: groupDiscussionMemorySourceType,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, groupDiscussionConflictTag)
	app := &App{memoryStore: store, configCache: corelib.AppConfig{RemoteMachineID: "machine-1"}, configCacheValid: true}

	record, err := app.ReviewExperienceTrace("memory:"+entry.ID, ExperienceTraceReviewRequest{Outcome: "deferred", Note: "waiting for owner\nneeds legal signoff", Reviewer: "review-chair"})
	if err != nil {
		t.Fatalf("ReviewExperienceTrace: %v", err)
	}
	if record.Outcome != experienceReviewOutcomeDeferred || record.Kind != "conflict" {
		t.Fatalf("unexpected review record: %#v", record)
	}
	assertExperienceInspectionRecommendedToolCall(t, record.RecommendedToolCall, "memory:"+entry.ID)

	updated := mustFindExperienceReviewEntry(t, store, groupDiscussionConflictTag)
	if !hasTag(updated.Tags, experienceReviewRequiredTag) || !hasTag(updated.Tags, experienceReviewStatusTagPrefix+experienceReviewOutcomeDeferred) || hasTag(updated.Tags, experienceReviewResolvedTag) {
		t.Fatalf("unexpected deferred tags: %#v", updated.Tags)
	}
	snapshot := buildExperienceLearningSnapshot(nil, store)
	if snapshot.ReviewRequiredTraceCount != 1 {
		t.Fatalf("review required count = %d, want 1", snapshot.ReviewRequiredTraceCount)
	}
	if snapshot.ReviewStatusCounts[experienceReviewOutcomeDeferred] != 1 {
		t.Fatalf("deferred status count = %#v, want 1 deferred", snapshot.ReviewStatusCounts)
	}
	if reviewSummary := mustFindExperienceReviewSummary(t, snapshot.ReviewSummaries, experienceReviewOutcomeDeferred); reviewSummary.Count != 1 || reviewSummary.RequiredCount != 1 || reviewSummary.LatestReviewer != "review-chair" || !strings.Contains(reviewSummary.LatestNote, "legal signoff") || reviewSummary.LatestKind != "a2a_conflict_review" {
		t.Fatalf("unexpected deferred review summary: %#v", reviewSummary)
	}
	if snapshot.NextActionTraceCount != 1 || snapshot.NextActionKindCounts["collect_a2a_conflict_evidence"] != 1 {
		t.Fatalf("deferred next action counts = %d/%#v, want collect conflict evidence", snapshot.NextActionTraceCount, snapshot.NextActionKindCounts)
	}
	if summary := mustFindExperienceNextActionSummary(t, snapshot.NextActionSummaries, "collect_a2a_conflict_evidence"); summary.Count != 1 || !strings.Contains(summary.LatestAction, "missing owner evidence") {
		t.Fatalf("unexpected conflict action summary: %#v", summary)
	}
	for _, detail := range snapshot.TraceDetails {
		if detail.Kind == "a2a_conflict_review" && detail.ReviewStatus != experienceReviewOutcomeDeferred {
			t.Fatalf("deferred conflict should expose status: %#v", detail)
		}
		if detail.Kind == "a2a_conflict_review" && (detail.Reviewer != "review-chair" || detail.ReviewNote != "waiting for owner\nneeds legal signoff" || detail.ReviewCount != 1 || detail.ReviewedAt == "") {
			t.Fatalf("deferred conflict should expose structured review audit: %#v", detail)
		}
		if detail.Kind == "a2a_conflict_review" && (detail.NextActionKind != "collect_a2a_conflict_evidence" || !strings.Contains(detail.NextAction, "review the conflict again")) {
			t.Fatalf("deferred conflict should expose evidence-collection next action: %#v", detail)
		}
	}
}

func TestReviewExperienceTraceApprovesToolRecoveryMemory(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		ID:         "adaptive-retry-write_file-args",
		Title:      "Adaptive retry: write_file / args",
		Content:    "Adaptive retry failure evidence\n- Tool: write_file\n- Failure category: args\n- Failure count: 3",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{experienceTraceKindToolRecoveryPattern.String(), "adaptive_retry", "tool:write_file", "category:args", experienceReviewRequiredTag},
		SourceType: string(experienceTraceSourceToolUsage),
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	snapshot := buildExperienceLearningSnapshot(nil, store)
	if snapshot.ReviewRequiredTraceCount != 1 || !hasExperienceTraceKind(snapshot.TraceDetails, experienceTraceKindToolRecoveryPattern.String()) {
		t.Fatalf("expected pending tool recovery review trace: %#v", snapshot)
	}
	app := &App{memoryStore: store}
	record, err := app.ReviewExperienceTrace("memory:adaptive-retry-write_file-args", ExperienceTraceReviewRequest{Outcome: "approved", Note: "safe as context only", Reviewer: "operator"})
	if err != nil {
		t.Fatalf("ReviewExperienceTrace: %v", err)
	}
	if record.Outcome != experienceReviewOutcomeApproved || record.Kind != experienceReviewKindToolRecovery.String() {
		t.Fatalf("unexpected review record: %#v", record)
	}
	updated := store.SearchDirectByID("adaptive-retry-write_file-args")
	if len(updated) != 1 {
		t.Fatalf("expected updated memory entry, got %#v", updated)
	}
	if hasTag(updated[0].Tags, experienceReviewRequiredTag) || !hasTag(updated[0].Tags, experienceReviewResolvedTag) || !hasTag(updated[0].Tags, experienceReviewLifecycleTagToolRecoveryReviewed.String()) {
		t.Fatalf("unexpected tool recovery review tags: %#v", updated[0].Tags)
	}
	if !strings.Contains(updated[0].Content, "Experience review record:") || !strings.Contains(updated[0].Content, "no skill, rollback, routing, or policy change") {
		t.Fatalf("review audit missing: %s", updated[0].Content)
	}
}

func TestReviewExperienceTraceApprovesSkillNudgeMemory(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "Skill candidate",
		Content:    "Skill nudge candidate browser_flow; sequence browser_observe -> browser_click",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{"skill_nudge_candidate", "browser", "browser_flow", experienceReviewRequiredTag},
		SourceType: "tool_usage",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, "skill_nudge_candidate")
	snapshot := buildExperienceLearningSnapshot(nil, store)
	if snapshot.ReviewRequiredTraceCount != 1 || !hasExperienceTraceKind(snapshot.TraceDetails, "skill_nudge_review") {
		t.Fatalf("expected pending skill nudge review trace: %#v", snapshot)
	}
	app := &App{memoryStore: store, configCache: corelib.AppConfig{RemoteMachineID: "machine-1"}, configCacheValid: true}

	record, err := app.ReviewExperienceTrace("memory:"+entry.ID, ExperienceTraceReviewRequest{Outcome: "accept", Note: "safe to draft manually"})
	if err != nil {
		t.Fatalf("ReviewExperienceTrace: %v", err)
	}
	if record.Outcome != experienceReviewOutcomeApproved || record.Kind != "skill_nudge" {
		t.Fatalf("unexpected review record: %#v", record)
	}
	assertExperienceInspectionRecommendedToolCall(t, record.RecommendedToolCall, "memory:"+entry.ID)

	updated := mustFindExperienceReviewEntry(t, store, "skill_nudge_candidate")
	if hasTag(updated.Tags, experienceReviewRequiredTag) || !hasTag(updated.Tags, experienceReviewResolvedTag) || !hasTag(updated.Tags, "skill_nudge_reviewed") {
		t.Fatalf("unexpected skill nudge review tags: %#v", updated.Tags)
	}
	if !strings.Contains(updated.Content, "no skill, rollback, routing, or policy change") {
		t.Fatalf("review safety record missing: %q", updated.Content)
	}
	snapshot = buildExperienceLearningSnapshot(nil, store)
	if snapshot.ReviewRequiredTraceCount != 0 {
		t.Fatalf("review required count = %d, want 0", snapshot.ReviewRequiredTraceCount)
	}
	if snapshot.NextActionTraceCount != 1 || snapshot.NextActionKindCounts["draft_skill_manually"] != 1 {
		t.Fatalf("approved skill next action counts = %d/%#v, want manual skill draft", snapshot.NextActionTraceCount, snapshot.NextActionKindCounts)
	}
	if summary := mustFindExperienceNextActionSummary(t, snapshot.NextActionSummaries, "draft_skill_manually"); summary.Count != 1 || !strings.Contains(summary.LatestAction, "manual skill draft") {
		t.Fatalf("unexpected skill action summary: %#v", summary)
	}
	for _, detail := range snapshot.TraceDetails {
		if detail.Kind == "skill_nudge_review" && (detail.ReviewStatus != experienceReviewOutcomeApproved || detail.ReviewedAt == "") {
			t.Fatalf("approved skill nudge should expose status: %#v", detail)
		}
		if detail.Kind == "skill_nudge_review" && detail.Reviewer != "machine-1" {
			t.Fatalf("approved skill nudge should default reviewer to cached machine id: %#v", detail)
		}
		if detail.Kind == "skill_nudge_review" && (detail.NextActionKind != "draft_skill_manually" || !strings.Contains(detail.NextAction, "manual skill draft")) {
			t.Fatalf("approved skill nudge should expose manual draft next action: %#v", detail)
		}
	}
}

func TestReviewExperienceTraceUsesLatestAuditWhenReviewedAgain(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "Skill candidate",
		Content:    "Skill nudge candidate browser_flow; sequence browser_observe -> browser_click",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{"skill_nudge_candidate", "browser", "browser_flow", experienceReviewRequiredTag},
		SourceType: "tool_usage",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entry := mustFindExperienceReviewEntry(t, store, "skill_nudge_candidate")
	app := &App{memoryStore: store}
	if _, err := app.ReviewExperienceTrace("memory:"+entry.ID, ExperienceTraceReviewRequest{Outcome: "deferred", Note: "need owner", Reviewer: "triage"}); err != nil {
		t.Fatalf("ReviewExperienceTrace deferred: %v", err)
	}
	if _, err := app.ReviewExperienceTrace("memory:"+entry.ID, ExperienceTraceReviewRequest{Outcome: "approved", Note: "approved after owner check", Reviewer: "owner"}); err != nil {
		t.Fatalf("ReviewExperienceTrace approved: %v", err)
	}

	snapshot := buildExperienceLearningSnapshot(nil, store)
	if snapshot.NextActionTraceCount != 1 || snapshot.NextActionKindCounts["draft_skill_manually"] != 1 {
		t.Fatalf("latest review next action counts = %d/%#v, want manual skill draft", snapshot.NextActionTraceCount, snapshot.NextActionKindCounts)
	}
	for _, detail := range snapshot.TraceDetails {
		if detail.Kind != "skill_nudge_review" {
			continue
		}
		if detail.ReviewRequired || detail.ReviewStatus != experienceReviewOutcomeApproved || detail.Reviewer != "owner" || detail.ReviewNote != "approved after owner check" || detail.ReviewCount != 2 {
			t.Fatalf("latest review audit not surfaced: %#v", detail)
		}
		if detail.NextActionKind != "draft_skill_manually" || !strings.Contains(detail.NextAction, "do not install") {
			t.Fatalf("latest review audit should keep safe manual next action: %#v", detail)
		}
		return
	}
	t.Fatalf("missing skill nudge review detail: %#v", snapshot.TraceDetails)
}

func TestReviewStateResetsWhenRollbackMemoryChanges(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	tags := []string{groupDiscussionResultTag, "discussion:disc-1", groupDiscussionRollbackTag, experienceReviewRequiredTag}
	_, _ = upsertGroupDiscussionMemory(store, "A2A discussion result\nRollback on:\n- gate fails", tags, 2, "A2A rollback")
	entry := mustFindExperienceReviewEntry(t, store, groupDiscussionRollbackTag)
	app := &App{memoryStore: store}
	if _, err := app.ReviewExperienceTrace("memory:"+entry.ID, ExperienceTraceReviewRequest{Outcome: "approved"}); err != nil {
		t.Fatalf("ReviewExperienceTrace: %v", err)
	}
	approved := mustFindExperienceReviewEntry(t, store, groupDiscussionRollbackTag)
	if !hasTag(approved.Tags, experienceReviewResolvedTag) {
		t.Fatalf("expected approved review tag: %#v", approved.Tags)
	}

	_, changed := upsertGroupDiscussionMemory(store, "A2A discussion result\nRollback on:\n- gate fails\n- owner cancels", tags, 2, "A2A rollback")
	if !changed {
		t.Fatal("expected changed rollback memory")
	}
	updated := mustFindExperienceReviewEntry(t, store, groupDiscussionRollbackTag)
	if !hasTag(updated.Tags, experienceReviewRequiredTag) || hasTag(updated.Tags, experienceReviewResolvedTag) || hasTag(updated.Tags, "rollback_reviewed") {
		t.Fatalf("updated rollback should require fresh review: %#v", updated.Tags)
	}
	snapshot := buildExperienceLearningSnapshot(nil, store)
	if snapshot.NextActionTraceCount != 1 || snapshot.NextActionKindCounts["review_signal"] != 1 {
		t.Fatalf("changed rollback next action counts = %d/%#v, want review signal", snapshot.NextActionTraceCount, snapshot.NextActionKindCounts)
	}
	for _, detail := range snapshot.TraceDetails {
		if detail.Kind == "a2a_rollback_review" && (!detail.ReviewRequired || detail.ReviewCount != 0 || detail.Reviewer != "" || detail.ReviewNote != "") {
			t.Fatalf("changed rollback content should clear stale audit fields: %#v", detail)
		}
		if detail.Kind == "a2a_rollback_review" && (detail.NextActionKind != "review_signal" || detail.NextAction == "") {
			t.Fatalf("changed rollback content should return to review next action: %#v", detail)
		}
	}
}

func TestReviewStateResetsWhenSkillNudgeMemoryChanges(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	bridge := NewUsagePatternBridge(nil, store)
	tags := []string{"skill_nudge_candidate", "browser", "browser_flow", "browser_observe", experienceReviewRequiredTag}
	created, changed := bridge.upsertUsageMemory("Skill nudge candidate browser_flow; sequence browser_observe", tags)
	if !created || changed {
		t.Fatalf("first upsert created/changed = %v/%v", created, changed)
	}
	entry := mustFindExperienceReviewEntry(t, store, "skill_nudge_candidate")
	app := &App{memoryStore: store}
	if _, err := app.ReviewExperienceTrace("memory:"+entry.ID, ExperienceTraceReviewRequest{Outcome: "approved"}); err != nil {
		t.Fatalf("ReviewExperienceTrace: %v", err)
	}

	created, changed = bridge.upsertUsageMemory("Skill nudge candidate browser_flow; sequence browser_observe -> browser_click", tags)
	if created || !changed {
		t.Fatalf("second upsert created/changed = %v/%v", created, changed)
	}
	updated := mustFindExperienceReviewEntry(t, store, "skill_nudge_candidate")
	if !hasTag(updated.Tags, experienceReviewRequiredTag) || hasTag(updated.Tags, experienceReviewResolvedTag) || hasTag(updated.Tags, "skill_nudge_reviewed") {
		t.Fatalf("updated skill nudge should require fresh review: %#v", updated.Tags)
	}
}

func mustFindExperienceReviewEntry(t *testing.T, store *memory.Store, tag string) memory.Entry {
	t.Helper()
	for _, entry := range store.List("", "") {
		if hasTag(entry.Tags, tag) {
			return entry
		}
	}
	t.Fatalf("missing memory entry with tag %q", tag)
	return memory.Entry{}
}
