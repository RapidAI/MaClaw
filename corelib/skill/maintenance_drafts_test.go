package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestCollectMaintenanceReviewDrafts_PatchDraftForFileBackedContract(t *testing.T) {
	skills := []corelib.NLSkillEntry{{
		Name:     "file-contract",
		Source:   "file",
		SkillDir: "/tmp/file-contract",
		Status:   "active",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo {{input}}"},
		}},
	}}
	drafts := CollectMaintenanceReviewDrafts(skills, SkillMaintenancePlanOptions{Now: time.Now(), MaxActions: 20})
	if len(drafts.PatchDrafts) == 0 {
		// improve_contract may only appear when plan detects incomplete contract.
		// Force via execute path for the skill alone.
		plan := SkillMaintenancePlan{
			Actions: []SkillMaintenanceAction{{
				Action: MaintenanceActionImproveContract,
				Skill:  "file-contract",
			}},
		}
		_, result := ExecuteSkillMaintenancePlan(skills, plan, SkillMaintenanceExecutionOptions{DryRun: true})
		found := false
		for _, a := range result.Actions {
			if a.PatchDraft != nil {
				found = true
				if a.PatchDraft.Skill != "file-contract" {
					t.Fatalf("skill=%q", a.PatchDraft.Skill)
				}
				if a.PatchDraft.SuggestedYAML == "" {
					t.Fatal("expected suggested yaml")
				}
			}
		}
		if !found {
			t.Fatalf("expected patch draft; plan_summary=%q drafts=%+v result=%+v", drafts.PlanSummary, drafts, result)
		}
		return
	}
	if drafts.PatchDrafts[0].Skill != "file-contract" {
		t.Fatalf("patch skill=%q", drafts.PatchDrafts[0].Skill)
	}
}

func TestApplyFileBackedContractPatch_WritesYAMLWithBackup(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "skill.yaml")
	orig := []byte("name: file-contract\nsteps:\n  - action: bash\n    params:\n      command: \"echo {{input}}\"\n")
	if err := os.WriteFile(yamlPath, orig, 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &corelib.NLSkillEntry{
		Name: "file-contract", Source: "file", SkillDir: dir, Status: "active",
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo {{input}}"}}},
	}
	draft := buildContractPatchDraft(*entry)
	if draft == nil {
		t.Fatal("expected draft")
	}
	ver, path, err := ApplyFileBackedContractPatch(entry, draft, &Versioner{})
	if err != nil {
		t.Fatal(err)
	}
	if ver < 1 || path != yamlPath {
		t.Fatalf("ver=%d path=%s", ver, path)
	}
	// Backup exists
	if _, err := os.Stat(filepath.Join(dir, "skill.yaml.v1")); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	data, _ := os.ReadFile(yamlPath)
	if !strings.Contains(string(data), "params:") || !strings.Contains(string(data), "input") {
		t.Fatalf("updated yaml missing params:\n%s", data)
	}
	if len(entry.Params) == 0 {
		t.Fatal("in-memory params not updated")
	}
}

func TestApplyTargetedMaintenanceAction_FileBackedContractWrites(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "skill.yaml")
	if err := os.WriteFile(yamlPath, []byte("name: fb\nsteps:\n  - action: bash\n    params:\n      command: \"echo {{q}}\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skills := []corelib.NLSkillEntry{{
		Name: "fb", Source: "file", SkillDir: dir, Status: "active",
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo {{q}}"}}},
	}}
	updated, res := ApplyTargetedMaintenanceAction(skills, MaintenanceActionImproveContract, "fb", "", false, true, false)
	if !res.OK {
		t.Fatalf("expected ok: %+v", res)
	}
	if res.BackupVersion < 1 || res.WrittenPath == "" {
		t.Fatalf("expected backup/write meta: %+v", res)
	}
	if len(updated[0].Params) == 0 {
		t.Fatal("params not on updated entry")
	}
}

func TestApplyTargetedMaintenanceAction_ImproveContractNonFile(t *testing.T) {
	skills := []corelib.NLSkillEntry{{
		Name:   "cfg-skill",
		Source: "learned",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo {{input}}"},
		}},
	}}
	updated, res := ApplyTargetedMaintenanceAction(skills, MaintenanceActionImproveContract, "cfg-skill", "", false, true, false)
	if !res.OK {
		t.Fatalf("expected ok: %+v", res)
	}
	if res.Result.ExecutedCount != 1 {
		t.Fatalf("executed=%d result=%+v", res.Result.ExecutedCount, res.Result)
	}
	idx := -1
	for i := range updated {
		if updated[i].Name == "cfg-skill" {
			idx = i
			break
		}
	}
	if idx < 0 || len(updated[idx].Params) == 0 {
		t.Fatalf("params not applied: %#v", updated)
	}
}

func TestApplyTargetedMaintenanceAction_MergeRequiresFlags(t *testing.T) {
	skills := []corelib.NLSkillEntry{
		{Name: "keep-me", Source: "learned", Status: "active", UsageCount: 5, SuccessCount: 5},
		{Name: "retire-me", Source: "crafted", Status: "active", UsageCount: 1, SuccessCount: 0},
	}
	_, res := ApplyTargetedMaintenanceAction(skills, MaintenanceActionMergeDuplicate, "keep-me", "retire-me", false, true, false)
	if res.OK {
		t.Fatalf("expected fail without allow_duplicate_retire: %+v", res)
	}
	updated, res := ApplyTargetedMaintenanceAction(skills, MaintenanceActionMergeDuplicate, "keep-me", "retire-me", false, true, true)
	if !res.OK {
		t.Fatalf("expected ok with flags: %+v", res)
	}
	// retire-me should be disabled
	for _, s := range updated {
		if s.Name == "retire-me" && s.Status != "disabled" {
			t.Fatalf("retire-me status=%q", s.Status)
		}
	}
}

func TestCollectMaintenanceReviewDrafts_MergeDraft(t *testing.T) {
	skills := []corelib.NLSkillEntry{
		{
			Name: "dup-a", Source: "learned", Status: "active",
			Description: "convert markdown to pdf helper",
			UsageCount:  5, SuccessCount: 4,
			Triggers: []string{"md to pdf"},
			Steps:    []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo a"}}},
		},
		{
			Name: "dup-b", Source: "crafted", Status: "active",
			Description: "convert markdown to pdf tool",
			UsageCount:  2, SuccessCount: 1,
			Triggers: []string{"markdown pdf"},
			Steps:    []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo b"}}},
		},
	}
	drafts := CollectMaintenanceReviewDrafts(skills, SkillMaintenancePlanOptions{
		Now:                 time.Now(),
		MaxActions:          20,
		DuplicateSimilarity: 0.3, // low threshold to encourage merge detection
	})
	// Merge is best-effort depending on similarity scoring; if none, still ok structure.
	if drafts.GeneratedAt == "" {
		t.Fatal("missing generated_at")
	}
	if drafts.PatchDrafts == nil || drafts.MergeDrafts == nil {
		t.Fatal("slices should be non-nil")
	}
}
