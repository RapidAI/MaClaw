package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImportAgentSkill_UsesCraftToolWhenNoScripts(t *testing.T) {
	skillDir := t.TempDir()
	content := "---\nname: PPTX Generator\ndescription: Build decks\ncompatibility: claude\n---\n\n# PPTX Generator\n\nGenerate presentations."
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}

	entry, err := ImportAgentSkill(skillDir)
	if err != nil {
		t.Fatalf("ImportAgentSkill() error = %v", err)
	}
	if entry.Source != "agent_skill" {
		t.Fatalf("Source = %q, want %q", entry.Source, "agent_skill")
	}
	if len(entry.Steps) != 1 || entry.Steps[0].Action != "craft_tool" {
		t.Fatalf("unexpected steps: %+v", entry.Steps)
	}
	if got := entry.Steps[0].Params["working_dir"]; got != skillDir {
		t.Fatalf("working_dir = %#v, want %q", got, skillDir)
	}
	if entry.SourceProject != "claude" {
		t.Fatalf("SourceProject = %q, want %q", entry.SourceProject, "claude")
	}
}

func TestImportAgentSkill_UsesScriptsAsBashSteps(t *testing.T) {
	skillDir := t.TempDir()
	scriptsDir := filepath.Join(skillDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Browser Skill\n\nAutomate browser."), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "step_01.sh"), []byte("echo one"), 0o755); err != nil {
		t.Fatalf("WriteFile(step_01.sh) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "step_02.sh"), []byte("echo two"), 0o755); err != nil {
		t.Fatalf("WriteFile(step_02.sh) error = %v", err)
	}

	entry, err := ImportAgentSkill(skillDir)
	if err != nil {
		t.Fatalf("ImportAgentSkill() error = %v", err)
	}
	if len(entry.Steps) != 2 {
		t.Fatalf("Steps len = %d, want 2", len(entry.Steps))
	}
	for i, step := range entry.Steps {
		if step.Action != "bash" {
			t.Fatalf("step %d action = %q, want bash", i, step.Action)
		}
		if got := step.Params["working_dir"]; got != skillDir {
			t.Fatalf("step %d working_dir = %#v, want %q", i, got, skillDir)
		}
	}
}
