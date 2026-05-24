package skill

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
)

func TestGovernanceDraftProviderExposesRepairDraft(t *testing.T) {
	skills := []corelib.NLSkillEntry{{
		Name:               "pdf-report",
		Description:        "Generate project PDF reports",
		SourceProject:      "/repo/app",
		UsageCount:         4,
		FailureCount:       4,
		LastError:          "error_class=missing_param missing output path",
		RepairAttemptCount: 0,
	}}
	provider := NewGovernanceDraftProvider(skills, SkillMaintenancePlanOptions{Now: time.Unix(10, 0), MinFailureRuns: 3})

	entries, err := provider.ListExperience(context.Background(), lifecycle.Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one draft entry, got %d: %+v", len(entries), entries)
	}
	entry := entries[0]
	if entry.EntryType != lifecycle.EntryTypeComparativeSkill || entry.Governance != lifecycle.GovernanceDraft {
		t.Fatalf("unexpected draft entry metadata: %+v", entry)
	}
	if entry.SourceType != "skill_governance_draft" || !strings.Contains(entry.Content, "review required") || !strings.Contains(entry.Content, "attempt_repair") {
		t.Fatalf("unexpected draft content: %+v", entry)
	}
}

func TestGovernanceDraftProviderSearchesDraftEvidence(t *testing.T) {
	skills := []corelib.NLSkillEntry{{
		Name:          "deploy-docs",
		Description:   "Deploy generated docs",
		SourceProject: "/repo/app",
		UsageCount:    5,
		FailureCount:  5,
		LastError:     "error_class=command_not_found missing pnpm",
	}}
	provider := NewGovernanceDraftProvider(skills, SkillMaintenancePlanOptions{Now: time.Unix(10, 0), MinFailureRuns: 3})

	candidates, err := provider.SearchExperience(context.Background(), lifecycle.Query{
		Text:     "deploy docs missing pnpm repair",
		Boundary: lifecycle.Boundary{ProjectPath: "/repo/app"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected one matching candidate, got %d: %+v", len(candidates), candidates)
	}
	if candidates[0].Reason != "skill_governance_draft_provider" || candidates[0].BoundaryScore <= 0 || candidates[0].PriorityScore <= 0 {
		t.Fatalf("unexpected candidate scoring: %+v", candidates[0])
	}
}

func TestGovernanceDraftProviderSearchHonorsLimit(t *testing.T) {
	skills := []corelib.NLSkillEntry{
		{Name: "repair-one", UsageCount: 5, FailureCount: 5, LastError: "error_class=command_not_found missing pnpm"},
		{Name: "repair-two", UsageCount: 5, FailureCount: 5, LastError: "error_class=command_not_found missing npm"},
	}
	provider := NewGovernanceDraftProvider(skills, SkillMaintenancePlanOptions{Now: time.Unix(10, 0), MinFailureRuns: 3})

	candidates, err := provider.SearchExperience(context.Background(), lifecycle.Query{Text: "repair missing", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected one limited draft candidate, got %+v", candidates)
	}
}

func TestGovernanceDraftProviderMarksBadSkillReviewAsFailureEvidence(t *testing.T) {
	skills := []corelib.NLSkillEntry{{
		Name:               "broken",
		UsageCount:         3,
		FailureCount:       3,
		LastError:          "rate_limit: too many requests",
		RepairAttemptCount: SelfRepairMaxAttempts,
	}}
	provider := NewGovernanceDraftProvider(skills, SkillMaintenancePlanOptions{Now: time.Unix(10, 0), MinFailureRuns: 3})

	entries, err := provider.ListExperience(context.Background(), lifecycle.Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].EntryType != lifecycle.EntryTypeFailureSkill {
		t.Fatalf("expected mark_needs_review failure draft, got %+v", entries)
	}
}

func TestExecuteReviewedGovernanceDraftsAppliesOnlySelectedDraft(t *testing.T) {
	now := time.Unix(20, 0)
	skills := []corelib.NLSkillEntry{
		{Name: "broken-a", Status: "active", UsageCount: 3, FailureCount: 3, LastError: "rate_limit: too many requests", RepairAttemptCount: SelfRepairMaxAttempts},
		{Name: "broken-b", Status: "active", UsageCount: 3, FailureCount: 3, LastError: "rate_limit: too many requests", RepairAttemptCount: SelfRepairMaxAttempts},
	}
	provider := NewGovernanceDraftProvider(skills, SkillMaintenancePlanOptions{Now: now, MinFailureRuns: 3})
	draftID := governanceDraftID(provider.Plan.Actions[0])

	updated, result := ExecuteReviewedGovernanceDrafts(skills, GovernanceDraftExecutionOptions{
		Now:              now,
		DryRun:           false,
		ReviewedDraftIDs: []string{draftID},
		PlanOptions:      SkillMaintenancePlanOptions{MinFailureRuns: 3},
	})

	if !result.OK || result.ExecutedCount != 1 || result.SkippedCount != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if updated[0].Status != "needs_review" || updated[1].Status != "active" {
		t.Fatalf("selected draft execution mutated wrong skills: %#v", updated)
	}
	if result.Boundary != GovernanceDraftExecuteBoundary {
		t.Fatalf("boundary = %q", result.Boundary)
	}
}

func TestExecuteReviewedGovernanceDraftsRealRunRequiresDraftIDs(t *testing.T) {
	skills := []corelib.NLSkillEntry{{Name: "broken", Status: "active", UsageCount: 3, FailureCount: 3, LastError: "rate_limit", RepairAttemptCount: SelfRepairMaxAttempts}}

	updated, result := ExecuteReviewedGovernanceDrafts(skills, GovernanceDraftExecutionOptions{DryRun: false})

	if result.OK || result.Error == "" || updated[0].Status != "active" {
		t.Fatalf("expected blocked execution without reviewed ids, updated=%#v result=%#v", updated, result)
	}
}

func TestExecuteReviewedGovernanceDraftsBlocksBlankDraftIDs(t *testing.T) {
	skills := []corelib.NLSkillEntry{{Name: "broken", Status: "active", UsageCount: 3, FailureCount: 3, LastError: "rate_limit", RepairAttemptCount: SelfRepairMaxAttempts}}

	updated, result := ExecuteReviewedGovernanceDrafts(skills, GovernanceDraftExecutionOptions{DryRun: false, ReviewedDraftIDs: []string{" "}})

	if result.OK || result.Error == "" || updated[0].Status != "active" {
		t.Fatalf("expected blank reviewed draft id to block execution, updated=%#v result=%#v", updated, result)
	}
}

func TestExecuteReviewedGovernanceDraftsDryRunBlankDraftIDDoesNotPreviewAll(t *testing.T) {
	skills := []corelib.NLSkillEntry{{Name: "broken", Status: "active", UsageCount: 3, FailureCount: 3, LastError: "rate_limit", RepairAttemptCount: SelfRepairMaxAttempts}}

	_, result := ExecuteReviewedGovernanceDrafts(skills, GovernanceDraftExecutionOptions{DryRun: true, ReviewedDraftIDs: []string{" "}})

	if result.OK || result.SkippedCount != 1 || len(result.Actions) != 1 || result.Actions[0].Skill != "<empty>" {
		t.Fatalf("blank draft id should not preview all actions: %#v", result)
	}
}

func TestExecuteReviewedGovernanceDraftsBlocksMissingDraftIDs(t *testing.T) {
	skills := []corelib.NLSkillEntry{{Name: "broken", Status: "active", UsageCount: 3, FailureCount: 3, LastError: "rate_limit", RepairAttemptCount: SelfRepairMaxAttempts}}

	updated, result := ExecuteReviewedGovernanceDrafts(skills, GovernanceDraftExecutionOptions{
		DryRun:           true,
		ReviewedDraftIDs: []string{"skill_draft:mark_needs_review:missing:"},
		PlanOptions:      SkillMaintenancePlanOptions{MinFailureRuns: 3},
	})

	if result.OK || result.Error == "" || result.SkippedCount != 1 || updated[0].Status != "active" {
		t.Fatalf("expected missing draft id to block reviewed execution, updated=%#v result=%#v", updated, result)
	}
}

func TestExecuteReviewedGovernanceDraftsMissingIDBlocksAllSelectedDrafts(t *testing.T) {
	now := time.Unix(20, 0)
	skills := []corelib.NLSkillEntry{{Name: "broken", Status: "active", UsageCount: 3, FailureCount: 3, LastError: "rate_limit", RepairAttemptCount: SelfRepairMaxAttempts}}
	provider := NewGovernanceDraftProvider(skills, SkillMaintenancePlanOptions{Now: now, MinFailureRuns: 3})
	draftID := governanceDraftID(provider.Plan.Actions[0])

	updated, result := ExecuteReviewedGovernanceDrafts(skills, GovernanceDraftExecutionOptions{
		Now:              now,
		DryRun:           false,
		ReviewedDraftIDs: []string{draftID, "skill_draft:mark_needs_review:missing:"},
		PlanOptions:      SkillMaintenancePlanOptions{MinFailureRuns: 3},
	})

	if result.OK || result.ExecutedCount != 0 || result.SkippedCount != 2 || updated[0].Status != "active" {
		t.Fatalf("missing draft id should block all selected draft execution, updated=%#v result=%#v", updated, result)
	}
}

func TestExecuteReviewedGovernanceDraftsKeepsFileBackedPatchAsReviewPacket(t *testing.T) {
	skills := []corelib.NLSkillEntry{{
		Name:     "file-contract",
		Source:   "file",
		SkillDir: t.TempDir(),
		Steps:    []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "convert {{input}}"}}},
	}}
	provider := NewGovernanceDraftProvider(skills, SkillMaintenancePlanOptions{Now: time.Unix(20, 0)})
	if len(provider.Plan.Actions) == 0 {
		t.Fatal("expected contract draft action")
	}
	draftID := governanceDraftID(provider.Plan.Actions[0])

	updated, result := ExecuteReviewedGovernanceDrafts(skills, GovernanceDraftExecutionOptions{
		DryRun:           false,
		ReviewedDraftIDs: []string{draftID},
	})

	if len(updated[0].Params) != 0 || len(updated[0].RequiredArgs) != 0 {
		t.Fatalf("file-backed skill should not be patched by governance draft executor: %#v", updated[0])
	}
	if result.SkippedCount != 1 || result.Actions[0].PatchDraft == nil {
		t.Fatalf("expected review packet skip, got %#v", result)
	}
}
