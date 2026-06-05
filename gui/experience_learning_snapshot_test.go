package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/session"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestBuildExperienceLearningSnapshotIncludesToolAndMemorySignals(t *testing.T) {
	tracker, err := coretool.NewUsageTracker("")
	if err != nil {
		t.Fatalf("NewUsageTracker: %v", err)
	}
	now := time.Now()
	for i := 0; i < 5; i++ {
		tracker.RecordExperience(coretool.ToolExperience{
			ToolName:     "browser_observe",
			QueryTokens:  []string{"browser", "button"},
			TaskType:     "browser_automation",
			ToolSequence: []string{"browser_observe", "browser_click"},
			Success:      true,
			FinalOutcome: "completed",
			Timestamp:    now,
		})
	}
	for i := 0; i < 4; i++ {
		tracker.RecordExperience(coretool.ToolExperience{
			ToolName:     "browser_click",
			QueryTokens:  []string{"browser", "button"},
			TaskType:     "browser_automation",
			ToolSequence: []string{"browser_click", "browser_observe"},
			Success:      false,
			ErrorClass:   "element_missing",
			RecoveryTool: "browser_observe",
			FinalOutcome: "recovered",
			Timestamp:    now,
		})
	}

	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Content:    "A2A expert raised rollback risk with concrete evidence.",
		Category:   memory.CategoryConversationSummary,
		SourceType: "group_discussion",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	snapshot := buildExperienceLearningSnapshot(tracker, store)
	if snapshot.RoutingHintCount == 0 || len(snapshot.RoutingHints) == 0 {
		t.Fatalf("expected routing hints in snapshot: %#v", snapshot)
	}
	if snapshot.SkillNudgeCount == 0 || len(snapshot.SkillNudgeCandidates) == 0 {
		t.Fatalf("expected skill nudge candidates in snapshot: %#v", snapshot)
	}
	if snapshot.UsagePatternCount == 0 || len(snapshot.UsagePatterns) == 0 {
		t.Fatalf("expected usage patterns in snapshot: %#v", snapshot)
	}
	if snapshot.RecoveryPatternCount == 0 || len(snapshot.RecoveryPatterns) == 0 {
		t.Fatalf("expected recovery patterns in snapshot: %#v", snapshot)
	}
	if snapshot.ProtectedMemoryCount == 0 || snapshot.MemoryExperience == nil {
		t.Fatalf("expected protected memory experience in snapshot: %#v", snapshot)
	}
	if snapshot.MemoryMaintenanceRecommendation == "" || snapshot.MemoryMaintenanceBoundary == "" {
		t.Fatalf("expected memory maintenance recommendation/boundary in snapshot: %#v", snapshot)
	}
	if len(snapshot.TraceDetails) == 0 {
		t.Fatalf("expected trace details in snapshot: %#v", snapshot)
	}
	if !hasExperienceTraceKind(snapshot.TraceDetails, "routing_hint") || !hasExperienceTraceKind(snapshot.TraceDetails, "tool_recovery_pattern") || !hasExperienceTraceKind(snapshot.TraceDetails, "a2a_discussion_result") {
		t.Fatalf("expected routing, recovery, and A2A trace details: %#v", snapshot.TraceDetails)
	}
	if snapshot.TraceDetailCount == 0 || snapshot.TraceKindCounts["routing_hint"] == 0 || snapshot.TraceSourceCounts["tool_usage"] == 0 {
		t.Fatalf("expected trace detail counts in snapshot: %#v", snapshot)
	}
}

func TestBuildExperienceLearningSnapshotHandlesNilInputs(t *testing.T) {
	snapshot := buildExperienceLearningSnapshot(nil, nil)
	if snapshot.RoutingHintCount != 0 || snapshot.SkillNudgeCount != 0 || snapshot.UsagePatternCount != 0 || snapshot.ProtectedMemoryCount != 0 {
		t.Fatalf("empty snapshot has counts: %#v", snapshot)
	}
	if snapshot.RoutingHints == nil || snapshot.SkillNudgeCandidates == nil || snapshot.RecoveryPatterns == nil || snapshot.UsagePatterns == nil || snapshot.TraceKindCounts == nil || snapshot.TraceSourceCounts == nil || snapshot.ReviewStatusCounts == nil || snapshot.NextActionKindCounts == nil || snapshot.FollowUpStatusCounts == nil || snapshot.ReviewSummaries == nil || snapshot.NextActionSummaries == nil || snapshot.FollowUpSummaries == nil {
		t.Fatalf("empty snapshot should return non-nil slices: %#v", snapshot)
	}
}

func TestBuildExperienceSessionTraceDetailsUsesSessionFocusTags(t *testing.T) {
	details := buildExperienceSessionTraceDetails([]session.SessionSummary{{
		SessionID: "sess-1",
		Timestamp: "2026-05-05T10:00:00Z",
		Platform:  "gui",
		Topic:     "release planning",
		TextLen:   2400,
	}})
	if len(details) != 1 {
		t.Fatalf("details = %#v", details)
	}
	detail := details[0]
	if detail.Kind != "session_history" || detail.ID != "session:sess-1" || detail.SourceURL != "session://sess-1" {
		t.Fatalf("unexpected session trace detail: %#v", detail)
	}
	if !hasTag(detail.Tags, "session:sess-1") || !hasTag(detail.Tags, "platform:gui") {
		t.Fatalf("session trace tags missing focus tags: %#v", detail.Tags)
	}
}

func TestBuildExperienceTraceDetailsMarksReviewCandidates(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Content:    "A2A conflict review candidate\nReview before treating either result as policy.",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{groupDiscussionConflictTag, "topic:release", "review_required"},
		SourceType: groupDiscussionMemorySourceType,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	snapshot := buildExperienceLearningSnapshot(nil, store)
	if !hasExperienceTraceKind(snapshot.TraceDetails, "a2a_conflict_review") {
		t.Fatalf("expected conflict review trace detail: %#v", snapshot.TraceDetails)
	}
	if snapshot.ReviewRequiredTraceCount != 1 {
		t.Fatalf("review required count = %d, want 1", snapshot.ReviewRequiredTraceCount)
	}
	if snapshot.NextActionTraceCount != 1 || snapshot.NextActionKindCounts["review_signal"] != 1 {
		t.Fatalf("next action counts = %d/%#v, want one review signal", snapshot.NextActionTraceCount, snapshot.NextActionKindCounts)
	}
	reviewSummary := mustFindExperienceReviewSummary(t, snapshot.ReviewSummaries, "required")
	if reviewSummary.Count != 1 || reviewSummary.RequiredCount != 1 || reviewSummary.LatestTraceID == "" || reviewSummary.LatestKind != "a2a_conflict_review" || reviewSummary.LatestAction == "" {
		t.Fatalf("unexpected review summary: %#v", reviewSummary)
	}
	summary := mustFindExperienceNextActionSummary(t, snapshot.NextActionSummaries, "review_signal")
	if summary.Count != 1 || summary.LatestTraceID == "" || summary.LatestTitle == "" || summary.LatestAction == "" {
		t.Fatalf("unexpected review action summary: %#v", summary)
	}
	for _, detail := range snapshot.TraceDetails {
		if detail.Kind == "a2a_conflict_review" && (!detail.ReviewRequired || detail.ReviewAction == "" || detail.NextActionKind != "review_signal") {
			t.Fatalf("conflict review should require review/action: %#v", detail)
		}
	}
}

func TestBuildExperienceTraceDetailsMarksRollbackReview(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "A2A: release safety",
		Content:    "A2A discussion result\nRollback on:\n- gate fails\nMatched rollback triggers:\n- gate fails",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{groupDiscussionResultTag, "discussion:disc-1", groupDiscussionRollbackTag, groupDiscussionRollbackTriggered, groupDiscussionRollbackTagPrefix + "abc123", groupDiscussionRollbackMatchPref + "abc123"},
		SourceType: groupDiscussionMemorySourceType,
		SourceURL:  "a2a://current_hub/disc-1",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	snapshot := buildExperienceLearningSnapshot(nil, store)
	var found *ExperienceTraceDetail
	for i := range snapshot.TraceDetails {
		if snapshot.TraceDetails[i].Kind == "a2a_rollback_review" {
			found = &snapshot.TraceDetails[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected rollback review trace detail: %#v", snapshot.TraceDetails)
	}
	if snapshot.ReviewRequiredTraceCount != 1 {
		t.Fatalf("review required count = %d, want 1", snapshot.ReviewRequiredTraceCount)
	}
	if snapshot.TraceDetailCount != 1 || snapshot.TraceKindCounts["a2a_rollback_review"] != 1 || snapshot.TraceSourceCounts[groupDiscussionMemorySourceType] != 1 || snapshot.NextActionTraceCount != 1 || snapshot.NextActionKindCounts["review_triggered_rollback_signal"] != 1 {
		t.Fatalf("unexpected trace counts: %#v", snapshot)
	}
	if reviewSummary := mustFindExperienceReviewSummary(t, snapshot.ReviewSummaries, "required"); reviewSummary.Count != 1 || reviewSummary.LatestTraceID != found.ID || reviewSummary.LatestTitle != found.Title {
		t.Fatalf("unexpected rollback review summary: %#v", reviewSummary)
	}
	if summary := mustFindExperienceNextActionSummary(t, snapshot.NextActionSummaries, "review_triggered_rollback_signal"); summary.Count != 1 || summary.LatestTraceID != found.ID || summary.LatestTitle != found.Title {
		t.Fatalf("unexpected rollback action summary: %#v", summary)
	}
	if !found.ReviewRequired || found.ReviewAction == "" || found.SourceURL != "a2a://current_hub/disc-1" || !hasTag(found.Tags, groupDiscussionRollbackTag) || found.NextActionKind != "review_triggered_rollback_signal" || !strings.Contains(found.Impact, "matches one or more rollback triggers") {
		t.Fatalf("unexpected rollback review trace detail: %#v", *found)
	}
}

func TestBuildExperienceTraceDetailsPrioritizesReviewSignalsBeforeLimit(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(memory.Entry{
		Title:      "A2A: rollback",
		Content:    "A2A discussion result\nRollback on:\n- gate fails",
		Category:   memory.CategoryProjectKnowledge,
		Tags:       []string{groupDiscussionResultTag, "discussion:disc-1", groupDiscussionRollbackTag},
		SourceType: groupDiscussionMemorySourceType,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	snapshot := ExperienceLearningSnapshot{}
	for i := 0; i < 8; i++ {
		snapshot.RoutingHints = append(snapshot.RoutingHints, coretool.ToolRoutingHint{
			ContextKey:  fmt.Sprintf("ctx-%d", i),
			PreferTools: []string{"tool"},
			Evidence:    3,
			Confidence:  0.9,
		})
	}

	details := buildExperienceTraceDetails(snapshot, store, 3)
	if len(details) != 3 {
		t.Fatalf("details len = %d, want 3", len(details))
	}
	if details[0].Kind != "a2a_rollback_review" || !details[0].ReviewRequired {
		t.Fatalf("first detail should be rollback review despite limit: %#v", details)
	}
}

func TestBuildExperienceTraceDetailsKeepsSkillNextActionBeforeLimit(t *testing.T) {
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
	snapshot := ExperienceLearningSnapshot{}
	for i := 0; i < 8; i++ {
		snapshot.RoutingHints = append(snapshot.RoutingHints, coretool.ToolRoutingHint{
			ContextKey:  fmt.Sprintf("ctx-%d", i),
			PreferTools: []string{"tool"},
			Evidence:    3,
			Confidence:  0.9,
		})
	}

	details := buildExperienceTraceDetails(snapshot, store, 3)
	if len(details) != 3 {
		t.Fatalf("details len = %d, want 3", len(details))
	}
	if details[0].Kind != "skill_nudge_review" || details[0].NextActionKind != "draft_skill_manually" || details[0].ReviewRequired {
		t.Fatalf("first detail should keep approved skill next action despite limit: %#v", details)
	}
}

func TestBuildExperienceTraceDetailsKeepsFollowUpAuditBeforeLimit(t *testing.T) {
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
	if _, err := app.RecordExperienceTraceFollowUp("memory:"+entry.ID, ExperienceTraceFollowUpRequest{Status: "completed", Note: "manual skill draft created"}); err != nil {
		t.Fatalf("RecordExperienceTraceFollowUp: %v", err)
	}
	snapshot := ExperienceLearningSnapshot{}
	for i := 0; i < 8; i++ {
		snapshot.RoutingHints = append(snapshot.RoutingHints, coretool.ToolRoutingHint{
			ContextKey:  fmt.Sprintf("ctx-%d", i),
			PreferTools: []string{"tool"},
			Evidence:    3,
			Confidence:  0.9,
		})
	}

	details := buildExperienceTraceDetails(snapshot, store, 3)
	if len(details) != 3 {
		t.Fatalf("details len = %d, want 3", len(details))
	}
	if details[0].Kind != "skill_nudge_review" || details[0].FollowUpStatus != experienceFollowUpOutcomeCompleted || details[0].FollowUpActionKind != "draft_skill_manually" || details[0].NextActionKind != "" {
		t.Fatalf("first detail should keep completed follow-up audit despite limit: %#v", details)
	}
}

func hasExperienceTraceKind(details []ExperienceTraceDetail, kind string) bool {
	for _, detail := range details {
		if detail.Kind == kind {
			return true
		}
	}
	return false
}

func mustFindExperienceNextActionSummary(t *testing.T, summaries []ExperienceNextActionSummary, kind string) ExperienceNextActionSummary {
	t.Helper()
	for _, summary := range summaries {
		if summary.Kind == kind {
			return summary
		}
	}
	t.Fatalf("missing next action summary %q in %#v", kind, summaries)
	return ExperienceNextActionSummary{}
}

func mustFindExperienceReviewSummary(t *testing.T, summaries []ExperienceReviewSummary, status string) ExperienceReviewSummary {
	t.Helper()
	for _, summary := range summaries {
		if summary.Status.String() == status {
			return summary
		}
	}
	t.Fatalf("missing review summary %q in %#v", status, summaries)
	return ExperienceReviewSummary{}
}

func mustFindExperienceFollowUpSummary(t *testing.T, summaries []ExperienceFollowUpSummary, status string) ExperienceFollowUpSummary {
	t.Helper()
	for _, summary := range summaries {
		if summary.Status.String() == status {
			return summary
		}
	}
	t.Fatalf("missing follow-up summary %q in %#v", status, summaries)
	return ExperienceFollowUpSummary{}
}
