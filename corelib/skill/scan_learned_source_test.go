package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanSkillDirClassifiesCraftPrefixAsLearned(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "craft-task-2c94a115")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := []byte(`name: craft_task_2c94a115
description: 录制会议
status: active
triggers:
  - 录制会议
steps:
  - action: record_audio
    params:
      purpose: 录制会议内容
`)
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), yaml, 0o644); err != nil {
		t.Fatal(err)
	}

	entries := ScanSkillDir(root)
	if len(entries) != 1 {
		t.Fatalf("ScanSkillDir len = %d, want 1; entries=%+v", len(entries), entries)
	}
	if entries[0].Source != "learned" {
		t.Fatalf("Source = %q, want learned", entries[0].Source)
	}
	if entries[0].Name != "craft_task_2c94a115" {
		t.Fatalf("Name = %q, want craft_task_2c94a115", entries[0].Name)
	}
}

func TestScanSkillDirClassifiesExplicitCraftedSource(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "meeting-notes")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := []byte(`name: meeting-notes
description: summarize meeting
source: crafted
status: active
triggers:
  - meeting
steps:
  - action: bash
    params:
      command: echo ok
`)
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), yaml, 0o644); err != nil {
		t.Fatal(err)
	}

	entries := ScanSkillDir(root)
	if len(entries) != 1 {
		t.Fatalf("ScanSkillDir len = %d, want 1", len(entries))
	}
	if entries[0].Source != "crafted" {
		t.Fatalf("Source = %q, want crafted", entries[0].Source)
	}
}
