package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsClaudeSKILLMD(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name: "claude format with allowed-tools",
			content: `---
name: test-skill
description: A test skill
allowed-tools:
  - bash
  - python
---
# Instructions
Do something.`,
			expected: true,
		},
		{
			name: "claude format with tools",
			content: `---
name: comic-generator
description: Generates comics
tools:
  - name: generate_comic
    script: scripts/generate.py
---
# Comic Generator`,
			expected: true,
		},
		{
			name: "maclaw format frontmatter",
			content: `---
name: my-skill
description: A skill
---
# My Skill`,
			expected: false,
		},
		{
			name: "no frontmatter",
			content: `# Just a markdown file
Some content here.`,
			expected: false,
		},
		{
			name:     "empty",
			content:  "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsClaudeSKILLMD([]byte(tt.content))
			if got != tt.expected {
				t.Errorf("IsClaudeSKILLMD() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParseClaudeSKILLMD(t *testing.T) {
	// Create a temp skill directory with a script.
	tmpDir := t.TempDir()
	scriptsDir := filepath.Join(tmpDir, "scripts")
	os.MkdirAll(scriptsDir, 0755)
	os.WriteFile(filepath.Join(scriptsDir, "generate.py"), []byte("print('hello')"), 0644)

	content := `---
name: test-claude-skill
description: A Claude skill for testing
allowed-tools:
  - bash
  - python
tools:
  - name: generate
    script: scripts/generate.py
    description: Generate something
---
# Test Claude Skill

This skill generates things.`

	entry, err := ParseClaudeSKILLMD(tmpDir, []byte(content))
	if err != nil {
		t.Fatalf("ParseClaudeSKILLMD() error = %v", err)
	}

	if entry.Name != "test-claude-skill" {
		t.Errorf("Name = %q, want %q", entry.Name, "test-claude-skill")
	}
	if entry.Description != "A Claude skill for testing" {
		t.Errorf("Description = %q, want %q", entry.Description, "A Claude skill for testing")
	}
	if entry.Status != "active" {
		t.Errorf("Status = %q, want %q", entry.Status, "active")
	}
	if entry.Source != "file" {
		t.Errorf("Source = %q, want %q", entry.Source, "file")
	}
	if len(entry.Steps) == 0 {
		t.Fatal("expected at least one step")
	}
	if entry.Steps[0].Action != "bash" {
		t.Errorf("Steps[0].Action = %q, want %q", entry.Steps[0].Action, "bash")
	}
	cmd, _ := entry.Steps[0].Params["command"].(string)
	if cmd == "" {
		t.Error("Steps[0].Params[command] is empty")
	}
}

func TestParseClaudeSKILLMD_AutoDiscoverScripts(t *testing.T) {
	// Create a temp skill directory with scripts but no tools in frontmatter.
	tmpDir := t.TempDir()
	scriptsDir := filepath.Join(tmpDir, "scripts")
	os.MkdirAll(scriptsDir, 0755)
	os.WriteFile(filepath.Join(scriptsDir, "run.sh"), []byte("echo hi"), 0644)
	os.WriteFile(filepath.Join(scriptsDir, "process.py"), []byte("print('ok')"), 0644)

	content := `---
name: auto-discover-skill
description: Skill with auto-discovered scripts
allowed-tools:
  - bash
---
# Auto Discover

Run the scripts.`

	entry, err := ParseClaudeSKILLMD(tmpDir, []byte(content))
	if err != nil {
		t.Fatalf("ParseClaudeSKILLMD() error = %v", err)
	}

	if len(entry.Steps) < 2 {
		t.Errorf("expected at least 2 steps from auto-discovery, got %d", len(entry.Steps))
	}
}

func TestReplaceClaudePaths(t *testing.T) {
	input := "Save files to ~/.claude/skills/my-skill and read from ~/.claude/config"
	got := replaceClaudePaths(input)
	if got != "Save files to ~/.maclaw/data/skills/my-skill and read from ~/.maclaw/data/config" {
		t.Errorf("replaceClaudePaths() = %q", got)
	}
}

func TestBuildCraftToolFallback_BasicMarkdown(t *testing.T) {
	tmpDir := t.TempDir()

	content := `# My Custom Skill

This skill does something special.

## Usage

Run the following command to process files.`

	entry := buildCraftToolFallback(tmpDir, "my-custom-skill", []byte(content))
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
	if entry.Name != "My Custom Skill" {
		t.Errorf("Name = %q, want %q", entry.Name, "My Custom Skill")
	}
	if entry.DirName != "my-custom-skill" {
		t.Errorf("DirName = %q, want %q", entry.DirName, "my-custom-skill")
	}
	if len(entry.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(entry.Steps))
	}
	if entry.Steps[0].Action != "craft_tool" {
		t.Errorf("Steps[0].Action = %q, want %q", entry.Steps[0].Action, "craft_tool")
	}
	instructions, _ := entry.Steps[0].Params["instructions"].(string)
	if instructions == "" {
		t.Error("instructions should not be empty")
	}
	workDir, _ := entry.Steps[0].Params["working_dir"].(string)
	if workDir != tmpDir {
		t.Errorf("working_dir = %q, want %q", workDir, tmpDir)
	}
}

func TestBuildCraftToolFallback_WithFrontmatter(t *testing.T) {
	content := `---
name: exotic-skill
description: A skill with unknown frontmatter format
custom_field: some_value
---
# Exotic Skill

Do exotic things.`

	entry := buildCraftToolFallback("/tmp/skills/exotic", "exotic-dir", []byte(content))
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
	if entry.Name != "exotic-skill" {
		t.Errorf("Name = %q, want %q", entry.Name, "exotic-skill")
	}
	if entry.Description != "A skill with unknown frontmatter format" {
		t.Errorf("Description = %q, want %q", entry.Description, "A skill with unknown frontmatter format")
	}
}

func TestBuildCraftToolFallback_EmptyContent(t *testing.T) {
	entry := buildCraftToolFallback("/tmp", "empty", []byte(""))
	if entry != nil {
		t.Error("expected nil for empty content")
	}

	entry = buildCraftToolFallback("/tmp", "whitespace", []byte("   \n  \n  "))
	if entry != nil {
		t.Error("expected nil for whitespace-only content")
	}
}

func TestBuildCraftToolFallback_ClaudePathReplacement(t *testing.T) {
	content := `# Skill
Save output to ~/.claude/skills/my-skill/output.pdf`

	entry := buildCraftToolFallback("/tmp/skills/test", "test", []byte(content))
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
	instructions, _ := entry.Steps[0].Params["instructions"].(string)
	if instructions == "" {
		t.Fatal("instructions empty")
	}
	if got := instructions; got != "# Skill\nSave output to ~/.maclaw/data/skills/my-skill/output.pdf" {
		t.Errorf("Claude paths not replaced: %q", got)
	}
}

func TestLoadSkillFromDir_LLMFallbackForUnknownFormat(t *testing.T) {
	// Create a skill directory with a SKILL.md that has an unknown format
	// (not our frontmatter, not Claude format, no bash blocks).
	// The LLM fallback should kick in and create a craft_tool step.
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "unknown-skill")
	os.MkdirAll(skillDir, 0755)

	content := `---
title: Unknown Format Skill
author: someone
version: 1.0
---
# Unknown Format

This skill uses a format we don't recognize.
It should still be loadable via LLM fallback.`

	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644)

	entry, defPath, err := loadSkillFromDir(skillDir, "unknown-skill")
	if err != nil {
		t.Fatalf("loadSkillFromDir() error = %v", err)
	}
	if entry == nil {
		t.Fatal("expected non-nil entry from LLM fallback")
	}
	if defPath == "" {
		t.Error("expected non-empty defPath")
	}
	if len(entry.Steps) != 1 {
		t.Fatalf("expected 1 craft_tool step, got %d", len(entry.Steps))
	}
	if entry.Steps[0].Action != "craft_tool" {
		t.Errorf("expected craft_tool action, got %q", entry.Steps[0].Action)
	}
	// Verify the skill is usable (has name, description, etc.)
	if entry.Name == "" {
		t.Error("expected non-empty name")
	}
	if entry.Status != "active" {
		t.Errorf("Status = %q, want active", entry.Status)
	}
}
