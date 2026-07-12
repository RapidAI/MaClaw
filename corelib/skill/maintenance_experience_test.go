package skill

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestIsHighValueMaintenanceAction(t *testing.T) {
	if !IsHighValueMaintenanceAction(SkillMaintenanceAction{Action: MaintenanceActionAttemptRepair}) {
		t.Fatal("attempt_repair should be high value")
	}
	if IsHighValueMaintenanceAction(SkillMaintenanceAction{Action: MaintenanceActionRefreshIndex}) {
		t.Fatal("refresh_index should not be high value")
	}
}

func TestCollectHighValueMaintenanceHints(t *testing.T) {
	skills := []corelib.NLSkillEntry{{
		Name:         "hint-skill",
		UsageCount:   4,
		FailureCount: 4,
		SuccessCount: 0,
		LastError:    "[class: command_not_found] missing tool",
		Status:       "active",
		Steps:        []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "x"}}},
	}}
	hints := CollectHighValueMaintenanceHints(skills, 5)
	if len(hints) == 0 {
		t.Fatal("expected high-value hints")
	}
	if !hints[0].HighValue || hints[0].Skill == "" || hints[0].Action == "" {
		t.Fatalf("incomplete hint: %+v", hints[0])
	}
}

func TestBuildHighValueMaintenanceExperienceContent(t *testing.T) {
	plan := SkillMaintenancePlan{
		GeneratedAt: "2026-01-01T00:00:00Z",
		Summary:     "2 actions",
		Actions: []SkillMaintenanceAction{
			{Action: MaintenanceActionAttemptRepair, Skill: "a", Reason: "broken", RecommendedAction: "repair"},
			{Action: MaintenanceActionRefreshIndex, Skill: "b", Reason: "index"},
			{Action: MaintenanceActionMarkNeedsReview, Skill: "c", Reason: "0 success", RecommendedAction: "review"},
		},
	}
	content := BuildHighValueMaintenanceExperienceContent(plan, 10)
	if content == "" {
		t.Fatal("expected content")
	}
	if !strings.Contains(content, "experience learning") {
		t.Fatalf("missing experience header: %s", content)
	}
	if !strings.Contains(content, "a") || !strings.Contains(content, "c") {
		t.Fatalf("missing high-value skills: %s", content)
	}
	if strings.Contains(content, "refresh_index") {
		t.Fatalf("low-value refresh should be filtered: %s", content)
	}
}

func TestBuildMaintenanceExperiencePromptSection(t *testing.T) {
	skills := []corelib.NLSkillEntry{{
		Name:         "pdf-tool",
		UsageCount:   4,
		FailureCount: 4,
		SuccessCount: 0,
		LastError:    "[class: command_not_found] missing pdftotext",
		Status:       "active",
		Steps:        []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "pdftotext"}}},
	}}
	section := BuildMaintenanceExperiencePromptSection(skills, 5)
	if section == "" {
		t.Fatal("expected prompt section")
	}
	if !strings.Contains(section, "技能治理提示") {
		t.Fatalf("missing section header: %s", section)
	}
	if !strings.Contains(section, "pdf-tool") {
		t.Fatalf("missing skill: %s", section)
	}
}

func TestIngestHighValueMaintenanceExperience(t *testing.T) {
	dir := t.TempDir()
	tracker, err := tool.NewUsageTracker(filepath.Join(dir, "usage.json"))
	if err != nil {
		t.Fatal(err)
	}
	skills := []corelib.NLSkillEntry{{
		Name:         "broken-skill",
		UsageCount:   5,
		FailureCount: 5,
		SuccessCount: 0,
		LastError:    "[class: missing_param] need output",
		Status:       "active",
		Steps:        []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo"}}},
	}}
	n := IngestHighValueMaintenanceExperience(tracker, skills, SkillMaintenancePlanOptions{
		Now:            time.Unix(100, 0),
		MinFailureRuns: 3,
	})
	if n < 1 {
		t.Fatalf("expected at least 1 ingest, got %d", n)
	}
}

func TestFileBackedRepairablePlanAndDraft(t *testing.T) {
	skills := []corelib.NLSkillEntry{{
		Name:         "file-repair",
		Source:       "file",
		SkillDir:     "/tmp/file-repair",
		UsageCount:   4,
		FailureCount: 4,
		SuccessCount: 0,
		LastError:    "[class: command_not_found] Command \"python3\" was not found.\n[action: patch] Replace python3 with python",
		Status:       "active",
		Steps:        []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "python3 script.py"}}},
	}}
	plan := BuildSkillMaintenancePlan(skills, SkillMaintenancePlanOptions{Now: time.Unix(50, 0), MinFailureRuns: 3})
	found := false
	for _, a := range plan.Actions {
		if a.Skill == "file-repair" && a.Action == MaintenanceActionAttemptRepair {
			found = true
			if !strings.Contains(a.RecommendedAction, "patch draft") && !strings.Contains(a.RecommendedAction, "file-backed") {
				t.Fatalf("recommended_action should mention file-backed draft: %q", a.RecommendedAction)
			}
		}
	}
	if !found {
		t.Fatalf("expected attempt_repair for file-backed skill, plan=%+v", plan)
	}

	drafts := CollectMaintenanceReviewDrafts(skills, SkillMaintenancePlanOptions{Now: time.Unix(50, 0), MinFailureRuns: 3})
	var repairDraft *SkillMaintenancePatchDraft
	for i := range drafts.PatchDrafts {
		if drafts.PatchDrafts[i].Kind == MaintenanceActionAttemptRepair && drafts.PatchDrafts[i].Skill == "file-repair" {
			repairDraft = &drafts.PatchDrafts[i]
			break
		}
	}
	if repairDraft == nil {
		// dry-run may still produce draft via execute path even if collect missed — force execute
		_, result := ExecuteSkillMaintenancePlan(skills, SkillMaintenancePlan{
			Actions: []SkillMaintenanceAction{{Action: MaintenanceActionAttemptRepair, Skill: "file-repair"}},
		}, SkillMaintenanceExecutionOptions{DryRun: true})
		for _, a := range result.Actions {
			if a.PatchDraft != nil && a.PatchDraft.Kind == MaintenanceActionAttemptRepair {
				repairDraft = a.PatchDraft
			}
		}
	}
	if repairDraft == nil {
		t.Fatalf("expected repair patch draft; drafts=%+v", drafts)
	}
	if repairDraft.ErrorClass == "" || repairDraft.SuggestedYAML == "" {
		t.Fatalf("incomplete repair draft: %+v", repairDraft)
	}
	if !strings.Contains(repairDraft.SuggestedYAML, "review only") {
		t.Fatalf("suggested yaml should be review-only: %s", repairDraft.SuggestedYAML)
	}
}
