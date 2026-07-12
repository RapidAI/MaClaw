package skill

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestExecuteSkillMaintenancePlanDryRunDoesNotMutate(t *testing.T) {
	plan := SkillMaintenancePlan{Actions: []SkillMaintenanceAction{{Action: MaintenanceActionMarkNeedsReview, Skill: "broken"}}}
	skills := []corelib.NLSkillEntry{{Name: "broken", Status: "active"}}

	updated, result := ExecuteSkillMaintenancePlan(skills, plan, SkillMaintenanceExecutionOptions{DryRun: true})

	if updated[0].Status != "active" {
		t.Fatalf("status = %q, want active", updated[0].Status)
	}
	if !result.DryRun || result.ExecutedCount != 1 || result.SkippedCount != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestExecuteSkillMaintenancePlanMarksNeedsReviewWhenApproved(t *testing.T) {
	plan := SkillMaintenancePlan{Actions: []SkillMaintenanceAction{{Action: MaintenanceActionMarkNeedsReview, Skill: "broken"}}}
	skills := []corelib.NLSkillEntry{{Name: "broken", Status: "active"}}

	updated, result := ExecuteSkillMaintenancePlan(skills, plan, SkillMaintenanceExecutionOptions{DryRun: false, ApprovedActions: []string{" MARK_NEEDS_REVIEW "}})

	if updated[0].Status != "needs_review" {
		t.Fatalf("status = %q, want needs_review", updated[0].Status)
	}
	if result.ExecutedCount != 1 || result.Actions[0].Status != MaintenanceExecutionStatusExecuted {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestExecuteSkillMaintenancePlanAcceptsDelimitedApprovedActions(t *testing.T) {
	plan := SkillMaintenancePlan{Actions: []SkillMaintenanceAction{
		{Action: MaintenanceActionMarkNeedsReview, Skill: "broken"},
		{Action: MaintenanceActionRefreshLifecycle, Skill: "fixed"},
	}}
	skills := []corelib.NLSkillEntry{
		{Name: "broken", Status: "active"},
		{Name: "fixed", RepairAttemptCount: 1, LastError: "old"},
	}

	updated, result := ExecuteSkillMaintenancePlan(skills, plan, SkillMaintenanceExecutionOptions{DryRun: false, ApprovedActions: []string{" mark_needs_review, refresh_lifecycle\n", "refresh_lifecycle\tmark_needs_review"}})

	if updated[0].Status != "needs_review" || updated[1].RepairAttemptCount != 0 || updated[1].LastError != "" {
		t.Fatalf("updated skills = %#v, want both approved actions applied", updated)
	}
	if result.ExecutedCount != 2 || result.SkippedCount != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestExecuteSkillMaintenancePlanRealRunRequiresApprovedActions(t *testing.T) {
	plan := SkillMaintenancePlan{Actions: []SkillMaintenanceAction{{Action: MaintenanceActionMarkNeedsReview, Skill: "broken"}}}
	skills := []corelib.NLSkillEntry{{Name: "broken", Status: "active"}}

	updated, result := ExecuteSkillMaintenancePlan(skills, plan, SkillMaintenanceExecutionOptions{DryRun: false})

	if updated[0].Status != "active" {
		t.Fatalf("status = %q, want active", updated[0].Status)
	}
	if result.OK || result.Error == "" || result.SkippedCount != 1 || result.ExecutedCount != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestExecuteSkillMaintenancePlanDoesNotMatchEmptySkillName(t *testing.T) {
	plan := SkillMaintenancePlan{Actions: []SkillMaintenanceAction{{Action: MaintenanceActionMarkNeedsReview}}}
	skills := []corelib.NLSkillEntry{{Status: "active"}}

	updated, result := ExecuteSkillMaintenancePlan(skills, plan, SkillMaintenanceExecutionOptions{DryRun: false, ApprovedActions: []string{MaintenanceActionMarkNeedsReview}})

	if updated[0].Status != "active" {
		t.Fatalf("empty skill action mutated unnamed skill: %#v", updated[0])
	}
	if result.SkippedCount != 1 || result.Actions[0].Reason != "skill was not found" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestExecuteSkillMaintenancePlanRefreshLifecycleClearsRepairMetadata(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	plan := SkillMaintenancePlan{Actions: []SkillMaintenanceAction{{Action: MaintenanceActionRefreshLifecycle, Skill: "fixed"}}}
	skills := []corelib.NLSkillEntry{{Name: "fixed", RepairAttemptCount: 2, LastError: "old error"}}

	updated, result := ExecuteSkillMaintenancePlan(skills, plan, SkillMaintenanceExecutionOptions{Now: now, DryRun: false, ApprovedActions: []string{MaintenanceActionRefreshLifecycle}})

	if updated[0].RepairAttemptCount != 0 || updated[0].LastError != "" {
		t.Fatalf("metadata = count %d error %q, want cleared", updated[0].RepairAttemptCount, updated[0].LastError)
	}
	if result.ExecutedCount != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestExecuteSkillMaintenancePlanQueuesAttemptRepair(t *testing.T) {
	formatted := FormatErrorForLLM(ClassifiedError{Class: ErrCommandNotFound, UserMessage: "missing cmd", Repairable: true})
	plan := SkillMaintenancePlan{Actions: []SkillMaintenanceAction{{Action: MaintenanceActionAttemptRepair, Skill: "repairable"}}}
	skills := []corelib.NLSkillEntry{{Name: "repairable", Source: "hub", UsageCount: 3, FailureCount: 3, LastError: formatted}}

	updated, result := ExecuteSkillMaintenancePlan(skills, plan, SkillMaintenanceExecutionOptions{DryRun: false, ApprovedActions: []string{MaintenanceActionAttemptRepair}})

	if updated[0].RepairAttemptCount != 0 || updated[0].LastError != formatted {
		t.Fatalf("attempt_repair should not mutate in core executor: %#v", updated[0])
	}
	if result.QueuedCount != 1 || result.Actions[0].Status != MaintenanceExecutionStatusQueued {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestExecuteSkillMaintenancePlanSkipsFileBackedAttemptRepair(t *testing.T) {
	formatted := FormatErrorForLLM(ClassifiedError{Class: ErrCommandNotFound, UserMessage: "missing cmd", Repairable: true})
	plan := SkillMaintenancePlan{Actions: []SkillMaintenanceAction{{Action: MaintenanceActionAttemptRepair, Skill: "file-repairable"}}}
	skills := []corelib.NLSkillEntry{{Name: "file-repairable", Source: "file", SkillDir: t.TempDir(), UsageCount: 3, FailureCount: 3, LastError: formatted}}

	updated, result := ExecuteSkillMaintenancePlan(skills, plan, SkillMaintenanceExecutionOptions{DryRun: false, ApprovedActions: []string{MaintenanceActionAttemptRepair}})

	if updated[0].RepairAttemptCount != 0 || updated[0].LastError != formatted {
		t.Fatalf("file-backed attempt_repair should not mutate: %#v", updated[0])
	}
	if result.SkippedCount != 1 || result.QueuedCount != 0 || !strings.Contains(result.Actions[0].Reason, "reviewed patch flow") {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Actions[0].PatchDraft == nil || result.Actions[0].PatchDraft.Kind != MaintenanceActionAttemptRepair {
		t.Fatalf("expected file-backed repair patch draft, got %#v", result.Actions[0])
	}
	if result.Actions[0].PatchDraft.SuggestedYAML == "" || result.Actions[0].PatchDraft.ErrorClass == "" {
		t.Fatalf("incomplete repair patch draft: %#v", result.Actions[0].PatchDraft)
	}
}

func TestExecuteSkillMaintenancePlanNoopsAttemptRepairWhenNotEligible(t *testing.T) {
	plan := SkillMaintenancePlan{Actions: []SkillMaintenanceAction{{Action: MaintenanceActionAttemptRepair, Skill: "clean"}}}
	skills := []corelib.NLSkillEntry{{Name: "clean", Source: "hub", UsageCount: 3}}

	_, result := ExecuteSkillMaintenancePlan(skills, plan, SkillMaintenanceExecutionOptions{DryRun: false, ApprovedActions: []string{MaintenanceActionAttemptRepair}})

	if result.NoopCount != 1 || result.Actions[0].Status != MaintenanceExecutionStatusNoop {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestExecuteSkillMaintenancePlanSkipsUnapprovedActions(t *testing.T) {
	plan := SkillMaintenancePlan{Actions: []SkillMaintenanceAction{
		{Action: MaintenanceActionMarkNeedsReview, Skill: "broken"},
		{Action: MaintenanceActionArchiveStale, Skill: "old"},
	}}
	skills := []corelib.NLSkillEntry{{Name: "broken", Status: "active"}, {Name: "old", Source: "learned", Status: "active"}}

	updated, result := ExecuteSkillMaintenancePlan(skills, plan, SkillMaintenanceExecutionOptions{DryRun: false, ApprovedActions: []string{MaintenanceActionMergeDuplicate}})

	if updated[0].Status != "active" || updated[1].Status != "active" {
		t.Fatalf("unexpected mutation: %#v", updated)
	}
	if result.ExecutedCount != 0 || result.SkippedCount != 2 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestExecuteSkillMaintenancePlanArchivesStaleLearnedSkillAsDisabled(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	plan := SkillMaintenancePlan{Actions: []SkillMaintenanceAction{{Action: MaintenanceActionArchiveStale, Skill: "old", Reason: "unused"}}}
	skills := []corelib.NLSkillEntry{{Name: "old", Source: "learned", Status: "active"}}

	updated, result := ExecuteSkillMaintenancePlan(skills, plan, SkillMaintenanceExecutionOptions{Now: now, DryRun: false, ApprovedActions: []string{MaintenanceActionArchiveStale}})

	if updated[0].Status != "disabled" || updated[0].LastError == "" {
		t.Fatalf("archive metadata = status %q error %q, want disabled with marker", updated[0].Status, updated[0].LastError)
	}
	if result.ExecutedCount != 1 || result.Actions[0].Status != MaintenanceExecutionStatusExecuted {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestExecuteSkillMaintenancePlanDoesNotArchiveExternalSkill(t *testing.T) {
	plan := SkillMaintenancePlan{Actions: []SkillMaintenanceAction{{Action: MaintenanceActionArchiveStale, Skill: "hub-skill", Reason: "unused"}}}
	skills := []corelib.NLSkillEntry{{Name: "hub-skill", Source: "hub", Status: "active"}}

	updated, result := ExecuteSkillMaintenancePlan(skills, plan, SkillMaintenanceExecutionOptions{DryRun: false, ApprovedActions: []string{MaintenanceActionArchiveStale}})

	if updated[0].Status != "active" {
		t.Fatalf("external skill mutated: %#v", updated[0])
	}
	if result.SkippedCount != 1 || result.Actions[0].Status != MaintenanceExecutionStatusSkipped {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestExecuteSkillMaintenancePlanImprovesConfigSkillContract(t *testing.T) {
	plan := SkillMaintenancePlan{Actions: []SkillMaintenanceAction{{Action: MaintenanceActionImproveContract, Skill: "legacy"}}}
	skills := []corelib.NLSkillEntry{{
		Name:   "legacy",
		Source: "manual",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "convert {{input}} {{output}}"},
		}},
	}}

	updated, result := ExecuteSkillMaintenancePlan(skills, plan, SkillMaintenanceExecutionOptions{DryRun: false, ApprovedActions: []string{MaintenanceActionImproveContract}})

	if len(updated[0].Params) != 2 || len(updated[0].RequiredArgs) != 2 {
		t.Fatalf("contract = params %#v required %#v, want synthesized input/output", updated[0].Params, updated[0].RequiredArgs)
	}
	if !result.RequiresIndexRefresh || result.ExecutedCount != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestExecuteSkillMaintenancePlanCompletesPartialConfigSkillContract(t *testing.T) {
	plan := SkillMaintenancePlan{Actions: []SkillMaintenanceAction{{Action: MaintenanceActionImproveContract, Skill: "partial"}}}
	skills := []corelib.NLSkillEntry{{
		Name:   "partial",
		Source: "manual",
		Params: []corelib.NLSkillParam{{Name: "input", Description: "source file"}},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "convert {{input}} {{output}}"},
		}},
	}}

	updated, result := ExecuteSkillMaintenancePlan(skills, plan, SkillMaintenanceExecutionOptions{DryRun: false, ApprovedActions: []string{MaintenanceActionImproveContract}})

	if len(updated[0].Params) != 2 || updated[0].Params[0].Name != "input" || updated[0].Params[0].Description != "source file" || updated[0].Params[1].Name != "output" {
		t.Fatalf("params = %#v, want existing input preserved plus output", updated[0].Params)
	}
	if len(updated[0].RequiredArgs) != 1 || updated[0].RequiredArgs[0] != "output" {
		t.Fatalf("required_args = %#v, want missing output only", updated[0].RequiredArgs)
	}
	if !result.RequiresIndexRefresh || result.ExecutedCount != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestExecuteSkillMaintenancePlanSkipsFileBackedContractMutation(t *testing.T) {
	plan := SkillMaintenancePlan{Actions: []SkillMaintenanceAction{{Action: MaintenanceActionImproveContract, Skill: "file-skill"}}}
	skills := []corelib.NLSkillEntry{{
		Name:     "file-skill",
		Source:   " file ",
		SkillDir: t.TempDir(),
		Steps:    []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo {{input}}"}}},
	}}

	updated, result := ExecuteSkillMaintenancePlan(skills, plan, SkillMaintenanceExecutionOptions{DryRun: false, ApprovedActions: []string{MaintenanceActionImproveContract}})

	if len(updated[0].Params) != 0 || len(updated[0].RequiredArgs) != 0 {
		t.Fatalf("file-backed skill mutated: %#v", updated[0])
	}
	if result.SkippedCount != 1 || result.Actions[0].PatchDraft == nil {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Actions[0].PatchDraft.RequiredArgs[0] != "input" || !containsMaintenanceYAML(result.Actions[0].PatchDraft.SuggestedYAML, "params:") {
		t.Fatalf("unexpected patch draft: %#v", result.Actions[0].PatchDraft)
	}
}

func TestExecuteSkillMaintenancePlanFileBackedPartialContractDraft(t *testing.T) {
	plan := SkillMaintenancePlan{Actions: []SkillMaintenanceAction{{Action: MaintenanceActionImproveContract, Skill: "file-partial"}}}
	skills := []corelib.NLSkillEntry{{
		Name:     "file-partial",
		Source:   "file",
		SkillDir: t.TempDir(),
		Params: []corelib.NLSkillParam{{
			Name:        "input",
			Description: "source file",
			Aliases:     []string{"src"},
			CLIFlag:     "--input",
			Default:     "in.txt",
		}},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "convert {{input}} {{output}}"},
		}},
	}}

	updated, result := ExecuteSkillMaintenancePlan(skills, plan, SkillMaintenanceExecutionOptions{DryRun: false, ApprovedActions: []string{MaintenanceActionImproveContract}})

	if len(updated[0].Params) != 1 || len(updated[0].RequiredArgs) != 0 {
		t.Fatalf("file-backed skill mutated: %#v", updated[0])
	}
	if result.SkippedCount != 1 || result.Actions[0].PatchDraft == nil {
		t.Fatalf("unexpected result: %#v", result)
	}
	draft := result.Actions[0].PatchDraft
	if len(draft.Params) != 2 || draft.Params[0].Name != "input" || draft.Params[0].Description != "source file" || draft.Params[1].Name != "output" {
		t.Fatalf("patch draft params = %#v, want existing input plus output", draft.Params)
	}
	if len(draft.RequiredArgs) != 1 || draft.RequiredArgs[0] != "output" {
		t.Fatalf("patch draft required_args = %#v, want output", draft.RequiredArgs)
	}
	for _, want := range []string{`name: "input"`, `description: "source file"`, `- "src"`, `cli_flag: "--input"`, `default: "in.txt"`, `name: "output"`} {
		if !strings.Contains(draft.SuggestedYAML, want) {
			t.Fatalf("patch draft YAML missing %q:\n%s", want, draft.SuggestedYAML)
		}
	}
}

func TestExecuteSkillMaintenancePlanReturnsMergeDuplicateDraft(t *testing.T) {
	plan := SkillMaintenancePlan{Actions: []SkillMaintenanceAction{{
		Action:            MaintenanceActionMergeDuplicate,
		Skill:             "old-logs",
		RelatedSkill:      "better-logs",
		RecommendedAction: "review and merge manually",
	}}}
	skills := []corelib.NLSkillEntry{
		{Name: "old-logs", Source: "learned", Status: "active", Description: "collect logs", UsageCount: 2, SuccessCount: 1, FailureCount: 1},
		{Name: "better-logs", Source: "crafted", Status: "active", Description: "collect logs", UsageCount: 8, SuccessCount: 7, FailureCount: 1, Params: []corelib.NLSkillParam{{Name: "path"}}},
	}

	updated, result := ExecuteSkillMaintenancePlan(skills, plan, SkillMaintenanceExecutionOptions{DryRun: false, ApprovedActions: []string{MaintenanceActionMergeDuplicate}})

	if updated[0].Status != "active" || updated[1].Status != "active" {
		t.Fatalf("merge draft must not mutate skills: %#v", updated)
	}
	if result.SkippedCount != 1 || result.Actions[0].MergeDraft == nil {
		t.Fatalf("unexpected result: %#v", result)
	}
	draft := result.Actions[0].MergeDraft
	if draft.RecommendedKeep != "better-logs" || draft.RecommendedRetire != "old-logs" || draft.RecommendedAction != "review and merge manually" {
		t.Fatalf("unexpected merge draft: %#v", draft)
	}
}

func TestExecuteSkillMaintenancePlanRetiresDuplicateWhenExplicitlyAllowed(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	plan := SkillMaintenancePlan{Actions: []SkillMaintenanceAction{{
		Action:       MaintenanceActionMergeDuplicate,
		Skill:        "old-logs",
		RelatedSkill: "better-logs",
	}}}
	skills := []corelib.NLSkillEntry{
		{Name: "old-logs", Source: "learned", Status: "active", UsageCount: 2, SuccessCount: 1, FailureCount: 1},
		{Name: "better-logs", Source: "crafted", Status: "active", UsageCount: 8, SuccessCount: 7, FailureCount: 1, Params: []corelib.NLSkillParam{{Name: "path"}}},
	}

	updated, result := ExecuteSkillMaintenancePlan(skills, plan, SkillMaintenanceExecutionOptions{Now: now, DryRun: false, ApprovedActions: []string{MaintenanceActionMergeDuplicate}, AllowDuplicateRetire: true})

	if updated[0].Status != "disabled" || !strings.Contains(updated[0].LastError, "retired_by_maintenance_duplicate") {
		t.Fatalf("retire metadata = %#v, want old skill disabled with marker", updated[0])
	}
	if updated[1].Status != "active" {
		t.Fatalf("kept skill mutated: %#v", updated[1])
	}
	if result.ExecutedCount != 1 || !result.RequiresIndexRefresh || result.Actions[0].MergeDraft == nil {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestExecuteSkillMaintenancePlanDoesNotRetireExternalDuplicate(t *testing.T) {
	plan := SkillMaintenancePlan{Actions: []SkillMaintenanceAction{{Action: MaintenanceActionMergeDuplicate, Skill: "external", RelatedSkill: "better"}}}
	skills := []corelib.NLSkillEntry{
		{Name: "external", Source: "hub", Status: "active", UsageCount: 1},
		{Name: "better", Source: "crafted", Status: "active", UsageCount: 8, SuccessCount: 8},
	}

	updated, result := ExecuteSkillMaintenancePlan(skills, plan, SkillMaintenanceExecutionOptions{DryRun: false, ApprovedActions: []string{MaintenanceActionMergeDuplicate}, AllowDuplicateRetire: true})

	if updated[0].Status != "active" || updated[1].Status != "active" {
		t.Fatalf("external duplicate should not be retired: %#v", updated)
	}
	if result.SkippedCount != 1 || !strings.Contains(result.Actions[0].Reason, "only learned or crafted") {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestExecuteSkillMaintenancePlanSkipsMergeDuplicateWhenRelatedMissing(t *testing.T) {
	plan := SkillMaintenancePlan{Actions: []SkillMaintenanceAction{{Action: MaintenanceActionMergeDuplicate, Skill: "old-logs", RelatedSkill: "missing"}}}
	skills := []corelib.NLSkillEntry{{Name: "old-logs", Source: "learned", Status: "active"}}

	_, result := ExecuteSkillMaintenancePlan(skills, plan, SkillMaintenanceExecutionOptions{DryRun: false, ApprovedActions: []string{MaintenanceActionMergeDuplicate}})

	if result.SkippedCount != 1 || result.Actions[0].MergeDraft != nil || !strings.Contains(result.Actions[0].Reason, "related duplicate") {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestExecuteSkillMaintenancePlanRefreshIndexRequestsCallerRefresh(t *testing.T) {
	plan := SkillMaintenancePlan{Actions: []SkillMaintenanceAction{{Action: MaintenanceActionRefreshIndex, Skill: "fixed"}}}

	_, result := ExecuteSkillMaintenancePlan(nil, plan, SkillMaintenanceExecutionOptions{DryRun: false, ApprovedActions: []string{MaintenanceActionRefreshIndex}})

	if !result.RequiresIndexRefresh || result.ExecutedCount != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func containsMaintenanceYAML(text, needle string) bool {
	return strings.Contains(text, needle)
}
