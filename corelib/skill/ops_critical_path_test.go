package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

// TestOpsCritical_ApplyContractThenRestoreYAML is the end-to-end ops path:
// human-approved improve_contract writes skill.yaml with a Versioner backup,
// then RestoreVersion brings the original content back (with pre-backup of the patched file).
func TestOpsCritical_ApplyContractThenRestoreYAML(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "skill.yaml")
	orig := []byte("name: ops-path\nsteps:\n  - action: bash\n    params:\n      command: \"echo {{input}}\"\n")
	if err := os.WriteFile(yamlPath, orig, 0o644); err != nil {
		t.Fatal(err)
	}

	skills := []corelib.NLSkillEntry{{
		Name:     "ops-path",
		Source:   "file",
		SkillDir: dir,
		Status:   "active",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo {{input}}"},
		}},
	}}

	// Apply without confirm must fail (approval gate).
	_, res := ApplyTargetedMaintenanceAction(skills, MaintenanceActionImproveContract, "ops-path", "", false, false, false)
	if res.OK {
		t.Fatalf("expected confirm gate failure: %+v", res)
	}

	updated, res := ApplyTargetedMaintenanceAction(skills, MaintenanceActionImproveContract, "ops-path", "", false, true, false)
	if !res.OK {
		t.Fatalf("apply: %+v", res)
	}
	if res.BackupVersion < 1 {
		t.Fatalf("expected backup version, got %+v", res)
	}
	if len(updated) == 0 || len(updated[0].Params) == 0 {
		t.Fatalf("expected in-memory params after apply: %#v", updated)
	}
	patched, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(patched), "params:") {
		t.Fatalf("patched yaml missing params:\n%s", patched)
	}
	backupPath := filepath.Join(dir, "skill.yaml.v1")
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	bak, _ := os.ReadFile(backupPath)
	if string(bak) != string(orig) {
		t.Fatalf("v1 backup should equal original\ngot:\n%s\nwant:\n%s", bak, orig)
	}

	// Restore v1 (pre-backs up current patched yaml first).
	v := &Versioner{}
	written, pre, err := v.RestoreVersion(dir, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if written != yamlPath {
		t.Fatalf("written=%s", written)
	}
	if pre < 2 {
		// v1 was original; restore should snapshot patched content as a newer version.
		t.Fatalf("expected pre_backup version >= 2, got %d", pre)
	}
	restored, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(orig) {
		t.Fatalf("restored yaml != original\ngot:\n%s\nwant:\n%s", restored, orig)
	}
}

// TestOpsCritical_MergeRetireMarkersDocumentsLastErrorPrefix keeps unretire
// string markers stable for GUI SetNLSkillStatus clear logic.
func TestOpsCritical_MergeRetireMarkersDocumentsLastErrorPrefix(t *testing.T) {
	skills := []corelib.NLSkillEntry{
		{Name: "keep-me", Source: "learned", Status: "active", UsageCount: 5, SuccessCount: 5, Description: "same helper"},
		{Name: "retire-me", Source: "crafted", Status: "active", UsageCount: 1, SuccessCount: 0, Description: "same helper"},
	}
	updated, res := ApplyTargetedMaintenanceAction(skills, MaintenanceActionMergeDuplicate, "keep-me", "retire-me", false, true, true)
	if !res.OK {
		t.Fatalf("merge retire: %+v", res)
	}
	var retired *corelib.NLSkillEntry
	for i := range updated {
		if updated[i].Name == "retire-me" {
			retired = &updated[i]
			break
		}
	}
	if retired == nil {
		t.Fatal("retire-me missing")
	}
	if retired.Status != "disabled" {
		t.Fatalf("status=%q", retired.Status)
	}
	if !strings.Contains(retired.LastError, "retired_by_maintenance") {
		t.Fatalf("last_error must contain retired_by_maintenance for unretire clear path, got %q", retired.LastError)
	}
}
