package skill

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestBuildSkillMaintenancePlanFlagsBrokenSkill(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	plan := BuildSkillMaintenancePlan([]corelib.NLSkillEntry{
		{
			Name:         "pdf-converter",
			Status:       "active",
			Source:       "hub",
			UsageCount:   4,
			SuccessCount: 0,
			FailureCount: 4,
		},
	}, SkillMaintenancePlanOptions{Now: now})

	if len(plan.Actions) != 1 {
		t.Fatalf("actions=%d, want 1: %#v", len(plan.Actions), plan.Actions)
	}
	action := plan.Actions[0]
	if action.Action != MaintenanceActionMarkNeedsReview || action.Risk != MaintenanceRiskMedium {
		t.Fatalf("unexpected action: %#v", action)
	}
	if action.Skill != "pdf-converter" || action.Reason != "0/4 successful runs" {
		t.Fatalf("unexpected skill/reason: %#v", action)
	}
}

func TestBuildSkillMaintenancePlanPrefersRepairBeforeReview(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	formatted := FormatErrorForLLM(ClassifiedError{Class: ErrCommandNotFound, UserMessage: "command missing", Repairable: true})
	plan := BuildSkillMaintenancePlan([]corelib.NLSkillEntry{
		{
			Name:         "repairable",
			Status:       "active",
			Source:       "hub",
			UsageCount:   4,
			SuccessCount: 0,
			FailureCount: 4,
			LastError:    formatted,
		},
	}, SkillMaintenancePlanOptions{Now: now})

	if len(plan.Actions) != 1 {
		t.Fatalf("actions=%d, want 1: %#v", len(plan.Actions), plan.Actions)
	}
	action := plan.Actions[0]
	if action.Action != MaintenanceActionAttemptRepair || action.Skill != "repairable" {
		t.Fatalf("expected attempt_repair, got %#v", action)
	}
}

func TestBuildSkillMaintenancePlanFileBackedRepairableUsesReviewDraftPath(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	formatted := FormatErrorForLLM(ClassifiedError{Class: ErrCommandNotFound, UserMessage: "command missing", Repairable: true})
	plan := BuildSkillMaintenancePlan([]corelib.NLSkillEntry{
		{
			Name:         "file-repairable",
			Status:       "active",
			Source:       "file",
			SkillDir:     t.TempDir(),
			UsageCount:   4,
			SuccessCount: 0,
			FailureCount: 4,
			LastError:    formatted,
		},
	}, SkillMaintenancePlanOptions{Now: now})

	// File-backed still surfaces attempt_repair, but only as a review-only patch draft
	// path (execute never queues SelfRepair / never rewrites YAML automatically).
	if !hasMaintenanceAction(plan, MaintenanceActionAttemptRepair, "file-repairable") {
		t.Fatalf("expected file-backed repairable failure to produce attempt_repair draft action: %#v", plan.Actions)
	}
	for _, action := range plan.Actions {
		if action.Skill == "file-repairable" && action.Action == MaintenanceActionAttemptRepair {
			if !strings.Contains(action.RecommendedAction, "patch draft") && !strings.Contains(action.RecommendedAction, "file-backed") {
				t.Fatalf("recommended_action should point at review draft, got %q", action.RecommendedAction)
			}
		}
	}
}

func TestBuildSkillMaintenancePlanDetectsDuplicateCraftedSkills(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	plan := BuildSkillMaintenancePlan([]corelib.NLSkillEntry{
		{Name: "collect-logs", Source: "crafted", Description: "collect nginx error logs from server", Triggers: []string{"logs", "nginx"}},
		{Name: "collect-server-logs", Source: "learned", Description: "collect nginx error logs from server", Triggers: []string{"logs", "nginx"}},
	}, SkillMaintenancePlanOptions{Now: now})

	if !hasMaintenanceAction(plan, MaintenanceActionMergeDuplicate, "collect-logs") {
		t.Fatalf("expected merge duplicate action, got %#v", plan.Actions)
	}
}

func TestBuildSkillMaintenancePlanFlagsStaleLearnedSkill(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	plan := BuildSkillMaintenancePlan([]corelib.NLSkillEntry{
		{
			Name:      "old-draft",
			Source:    "learned",
			CreatedAt: now.AddDate(0, 0, -120).Format(time.RFC3339),
		},
	}, SkillMaintenancePlanOptions{Now: now, StaleAfterDays: 90})

	if !hasMaintenanceAction(plan, MaintenanceActionArchiveStale, "old-draft") {
		t.Fatalf("expected archive stale action, got %#v", plan.Actions)
	}
}

func TestBuildSkillMaintenancePlanSkipsDisabledSkills(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	plan := BuildSkillMaintenancePlan([]corelib.NLSkillEntry{
		{
			Name:      "old-disabled",
			Source:    "learned",
			Status:    "disabled",
			CreatedAt: now.AddDate(0, 0, -120).Format(time.RFC3339),
		},
		{Name: "dup-disabled", Source: "crafted", Status: "disabled", Description: "collect nginx error logs", Triggers: []string{"logs"}},
		{Name: "dup-active", Source: "crafted", Status: "active", Description: "collect nginx error logs", Triggers: []string{"logs"}},
	}, SkillMaintenancePlanOptions{Now: now, StaleAfterDays: 90})

	if len(plan.Actions) != 0 {
		t.Fatalf("expected disabled skills to be skipped, got %#v", plan.Actions)
	}
}

func TestBuildSkillMaintenancePlanSkipsInstructionOnlyAppContainers(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	plan := BuildSkillMaintenancePlan([]corelib.NLSkillEntry{
		{
			Name:         "paper-translator-app",
			Type:         "instruction",
			Source:       "learned",
			Status:       "active",
			UsageCount:   4,
			SuccessCount: 0,
			FailureCount: 4,
			CreatedAt:    now.AddDate(0, 0, -120).Format(time.RFC3339),
		},
	}, SkillMaintenancePlanOptions{Now: now, StaleAfterDays: 90})

	if len(plan.Actions) != 0 {
		t.Fatalf("instruction-only app container must not enter maintenance actions: %#v", plan.Actions)
	}
}

func TestBuildSkillMaintenancePlanSkipsUnnamedSkills(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	plan := BuildSkillMaintenancePlan([]corelib.NLSkillEntry{{
		Source:       "learned",
		UsageCount:   3,
		FailureCount: 3,
		SuccessCount: 0,
		CreatedAt:    now.AddDate(0, 0, -120).Format(time.RFC3339),
	}}, SkillMaintenancePlanOptions{Now: now, StaleAfterDays: 90})

	if len(plan.Actions) != 0 {
		t.Fatalf("expected unnamed skills to be skipped, got %#v", plan.Actions)
	}
}

func TestBuildSkillMaintenancePlanFlagsMissingParamContract(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	plan := BuildSkillMaintenancePlan([]corelib.NLSkillEntry{
		{
			Name:   "legacy-runner",
			Source: "manual",
			Status: "active",
			Steps: []corelib.NLSkillStep{
				{Action: "bash", Params: map[string]interface{}{"command": "echo {{input}}"}},
			},
		},
	}, SkillMaintenancePlanOptions{Now: now})

	if !hasMaintenanceAction(plan, MaintenanceActionImproveContract, "legacy-runner") {
		t.Fatalf("expected improve contract action, got %#v", plan.Actions)
	}
}

func TestBuildSkillMaintenancePlanFlagsPartialParamContract(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	plan := BuildSkillMaintenancePlan([]corelib.NLSkillEntry{
		{
			Name:   "partial-runner",
			Source: "manual",
			Status: "active",
			Params: []corelib.NLSkillParam{{Name: "input"}},
			Steps: []corelib.NLSkillStep{
				{Action: "bash", Params: map[string]interface{}{"command": "convert {{input}} {{output}}"}},
			},
		},
	}, SkillMaintenancePlanOptions{Now: now})

	if !hasMaintenanceAction(plan, MaintenanceActionImproveContract, "partial-runner") {
		t.Fatalf("expected improve contract action for partial schema, got %#v", plan.Actions)
	}
}

func TestBuildSkillMaintenancePlanFlagsLegacyRequiredArgsContract(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	plan := BuildSkillMaintenancePlan([]corelib.NLSkillEntry{
		{
			Name:         "legacy-required-runner",
			Source:       "manual",
			Status:       "active",
			RequiredArgs: []string{"input"},
			Steps: []corelib.NLSkillStep{
				{Action: "bash", Params: map[string]interface{}{"command": "cat {{input}}"}},
			},
		},
	}, SkillMaintenancePlanOptions{Now: now})

	if !hasMaintenanceAction(plan, MaintenanceActionImproveContract, "legacy-required-runner") {
		t.Fatalf("expected improve contract action for required_args-only schema, got %#v", plan.Actions)
	}
}

func TestBuildSkillMaintenancePlanDoesNotFlagContractWithoutTemplates(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	plan := BuildSkillMaintenancePlan([]corelib.NLSkillEntry{
		{
			Name:   "static-runner",
			Source: "manual",
			Status: "active",
			Steps: []corelib.NLSkillStep{
				{Action: "bash", Params: map[string]interface{}{"command": "echo ok"}},
			},
		},
	}, SkillMaintenancePlanOptions{Now: now})

	if hasMaintenanceAction(plan, MaintenanceActionImproveContract, "static-runner") {
		t.Fatalf("static runner should not need contract synthesis: %#v", plan.Actions)
	}
}

func TestBuildSkillMaintenancePlanSuggestsIndexRefreshAfterRepair(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	plan := BuildSkillMaintenancePlan([]corelib.NLSkillEntry{
		{
			Name:         "repaired-skill",
			Source:       "hub",
			Status:       "active",
			SkillDir:     t.TempDir(),
			LastRepairAt: now.Add(-time.Hour).Format(time.RFC3339),
		},
	}, SkillMaintenancePlanOptions{Now: now})

	if !hasMaintenanceAction(plan, MaintenanceActionRefreshIndex, "repaired-skill") {
		t.Fatalf("expected refresh index action, got %#v", plan.Actions)
	}
}

func TestBuildSkillMaintenancePlanLeavesHealthySkillAlone(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	plan := BuildSkillMaintenancePlan([]corelib.NLSkillEntry{
		{
			Name:         "healthy",
			Source:       "hub",
			Status:       "active",
			UsageCount:   10,
			SuccessCount: 9,
			FailureCount: 1,
			Params:       []corelib.NLSkillParam{{Name: "input", Required: true}},
			Steps:        []corelib.NLSkillStep{{Action: "bash"}},
		},
	}, SkillMaintenancePlanOptions{Now: now})

	if len(plan.Actions) != 0 {
		t.Fatalf("expected no actions, got %#v", plan.Actions)
	}
	if plan.Summary != "0 actions, skill library looks healthy" {
		t.Fatalf("unexpected summary: %q", plan.Summary)
	}
}

func hasMaintenanceAction(plan SkillMaintenancePlan, actionName, skillName string) bool {
	for _, action := range plan.Actions {
		if action.Action == actionName && action.Skill == skillName {
			return true
		}
	}
	return false
}
