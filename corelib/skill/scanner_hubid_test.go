package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanSkillDirDoesNotPromoteSubmissionIDToHubSkillID(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "Paper PDF Translator")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yamlBody := "name: paper_pdf_translator\ndescription: test\nstatus: active\nsteps:\n  - action: bash\n    params:\n      command: echo ok\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(yamlBody), 0o644); err != nil {
		t.Fatal(err)
	}
	const submission = "sub-1783856848170-cbee8cd2135b3c8e;enterprise_hub=enterprise_hub:skill:paper_pdf_translator@6c2a9af36010"
	if err := os.WriteFile(filepath.Join(skillDir, "upload_status.json"), []byte(`{"submission_id":"`+submission+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	skills := ScanSkillDir(root)
	if len(skills) != 1 {
		t.Fatalf("ScanSkillDir len=%d, want 1: %#v", len(skills), skills)
	}
	got := skills[0]
	if got.HubSkillID != "paper_pdf_translator" {
		t.Fatalf("HubSkillID = %q, want package id paper_pdf_translator (not raw submission)", got.HubSkillID)
	}
	if got.Name != "paper_pdf_translator" {
		t.Fatalf("Name = %q", got.Name)
	}
	if ref := got.PreferredRuntimeSkillRef(); ref != "paper_pdf_translator" {
		t.Fatalf("PreferredRuntimeSkillRef = %q", ref)
	}
}

func TestScanSkillDirIgnoresBareSubmissionWithoutPackage(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "local-tool")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yamlBody := "name: local-tool\ndescription: test\nstatus: active\nsteps:\n  - action: bash\n    params:\n      command: echo ok\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(yamlBody), 0o644); err != nil {
		t.Fatal(err)
	}
	// Lifecycle submission shape: sub-<digits>… without an embedded :skill: package.
	if err := os.WriteFile(filepath.Join(skillDir, "upload_status.json"), []byte(`{"submission_id":"sub-1783856848170-cbee8cd2135b3c8e"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	skills := ScanSkillDir(root)
	if len(skills) != 1 {
		t.Fatalf("ScanSkillDir len=%d", len(skills))
	}
	if skills[0].HubSkillID != "" {
		t.Fatalf("HubSkillID = %q, want empty for bare submission without package segment", skills[0].HubSkillID)
	}
	if ref := skills[0].PreferredRuntimeSkillRef(); ref != "local-tool" {
		t.Fatalf("PreferredRuntimeSkillRef = %q, want skill name", ref)
	}
}
