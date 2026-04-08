package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportMarkdownSkillDir_CreatesCraftToolWhenNoScripts(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "pptx-generator")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	content := "---\nname: PPTX Generator\ndescription: Build decks\ncompatibility: claude\n---\n\n# PPTX Generator\n\nGenerate presentations."
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}

	entry, err := ImportMarkdownSkillDir(skillDir, MarkdownSkillOptions{Source: "file", SkillDir: skillDir})
	if err != nil {
		t.Fatalf("ImportMarkdownSkillDir() error = %v", err)
	}
	if entry.Name != "PPTX Generator" {
		t.Fatalf("Name = %q, want %q", entry.Name, "PPTX Generator")
	}
	if entry.Description != "Build decks" {
		t.Fatalf("Description = %q, want %q", entry.Description, "Build decks")
	}
	if len(entry.Steps) != 1 || entry.Steps[0].Action != "craft_tool" {
		t.Fatalf("unexpected steps: %+v", entry.Steps)
	}
	if got := entry.Steps[0].Params["instructions"]; got != strings.TrimSpace(content) {
		t.Fatalf("unexpected instructions: %#v", got)
	}
	if got := entry.Steps[0].Params["working_dir"]; got != skillDir {
		t.Fatalf("working_dir = %#v, want %q", got, skillDir)
	}
	if entry.SourceProject != "claude" {
		t.Fatalf("SourceProject = %q, want %q", entry.SourceProject, "claude")
	}
}

func TestImportMarkdownSkillDir_UsesScriptsAsBashSteps(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "browser-skill")
	scriptsDir := filepath.Join(skillDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Browser Skill\n\nAutomate browser."), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "01_setup.sh"), []byte("echo setup"), 0o755); err != nil {
		t.Fatalf("WriteFile(script1) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "02_run.sh"), []byte("echo run"), 0o755); err != nil {
		t.Fatalf("WriteFile(script2) error = %v", err)
	}

	entry, err := ImportMarkdownSkillDir(skillDir, MarkdownSkillOptions{Source: "agent_skill", SkillDir: skillDir})
	if err != nil {
		t.Fatalf("ImportMarkdownSkillDir() error = %v", err)
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

func TestScanSkillDir_RecognizesMarkdownSkillDirectory(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "pptx-generator")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# PPTX Generator\n\nGenerate slides."), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}

	skills := ScanSkillDir(root)
	if len(skills) != 1 {
		t.Fatalf("ScanSkillDir() returned %d skills, want 1", len(skills))
	}
	if skills[0].Name != "PPTX Generator" {
		t.Fatalf("Name = %q, want %q", skills[0].Name, "PPTX Generator")
	}
	if len(skills[0].Steps) != 1 || skills[0].Steps[0].Action != "craft_tool" {
		t.Fatalf("unexpected steps: %+v", skills[0].Steps)
	}
}

func TestScanSkillDir_PrefersYAMLOverMarkdown(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: yaml-skill\ndescription: yaml\nsteps:\n  - action: bash\n    params:\n      command: echo yaml\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Markdown Skill\n\nmarkdown"), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}

	skills := ScanSkillDir(root)
	if len(skills) != 1 {
		t.Fatalf("ScanSkillDir() returned %d skills, want 1", len(skills))
	}
	if skills[0].Name != "yaml-skill" {
		t.Fatalf("Name = %q, want %q", skills[0].Name, "yaml-skill")
	}
	if len(skills[0].Steps) != 1 || skills[0].Steps[0].Action != "bash" {
		t.Fatalf("unexpected steps: %+v", skills[0].Steps)
	}
}

func TestScanSkillDir_FallsBackToMarkdownWhenYAMLHasNoSteps(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "pptx-generator")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: pptx-generator\ndescription: yaml metadata only\nversion: \"1.0\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: PPTX Generator\ndescription: Build decks\n---\n\n# PPTX Generator\n\nGenerate slides."), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}

	skills := ScanSkillDir(root)
	if len(skills) != 1 {
		t.Fatalf("ScanSkillDir() returned %d skills, want 1", len(skills))
	}
	if len(skills[0].Steps) != 1 || skills[0].Steps[0].Action != "craft_tool" {
		t.Fatalf("unexpected steps: %+v", skills[0].Steps)
	}
	if got := skills[0].Steps[0].Params["working_dir"]; got != skillDir {
		t.Fatalf("working_dir = %#v, want %q", got, skillDir)
	}
}

func TestValidateExternalSkillDir_AcceptsSkillMarkdownVariants(t *testing.T) {
	root := t.TempDir()
	upperDir := filepath.Join(root, "upper")
	lowerDir := filepath.Join(root, "lower")
	if err := os.MkdirAll(upperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(upper) error = %v", err)
	}
	if err := os.MkdirAll(lowerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(lower) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(upperDir, "SKILL.md"), []byte("# Upper\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(upper) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(lowerDir, "skill.md"), []byte("# Lower\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(lower) error = %v", err)
	}

	count, err := ValidateExternalSkillDir(root)
	if err != nil {
		t.Fatalf("ValidateExternalSkillDir() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
}
