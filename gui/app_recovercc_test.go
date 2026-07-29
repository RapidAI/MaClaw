package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecoverCCDeletesOnlyConfigFiles(t *testing.T) {
	tmpHome := t.TempDir()
	app := &App{testHomeDir: tmpHome}

	claudeDir := filepath.Join(tmpHome, ".claude")
	hooksDir := filepath.Join(claudeDir, "hooks")
	skillsDir := filepath.Join(claudeDir, "skills")
	projectsDir := filepath.Join(claudeDir, "projects")
	otherDir := filepath.Join(claudeDir, "extensions")

	for _, dir := range []string{hooksDir, skillsDir, projectsDir, otherDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", dir, err)
		}
	}

	claudeJSON := filepath.Join(tmpHome, ".claude.json")
	settingsJSON := filepath.Join(claudeDir, "settings.json")
	hookJSON := filepath.Join(hooksDir, "pre-command.json")
	hookTXT := filepath.Join(hooksDir, "README.txt")
	skillFile := filepath.Join(skillsDir, "custom-skill.md")
	projectFile := filepath.Join(projectsDir, "state.json")
	otherFile := filepath.Join(otherDir, "plugin.js")

	for path, content := range map[string]string{
		claudeJSON:   "config",
		settingsJSON: "settings",
		hookJSON:     "hook",
		hookTXT:      "keep",
		skillFile:    "skill",
		projectFile:  "project",
		otherFile:    "extension",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
	}

	if err := app.RecoverCC(); err != nil {
		t.Fatalf("RecoverCC() error = %v", err)
	}

	for _, path := range []string{claudeJSON, settingsJSON, hookJSON} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %q to be removed, stat err = %v", path, err)
		}
	}

	for _, path := range []string{hookTXT, skillFile, projectFile, otherFile, skillsDir, projectsDir, otherDir, hooksDir, claudeDir} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %q to be preserved, stat err = %v", path, err)
		}
	}
}

func TestRecoverCCSkipsMissingTargets(t *testing.T) {
	tmpHome := t.TempDir()
	app := &App{testHomeDir: tmpHome}

	claudeDir := filepath.Join(tmpHome, ".claude")
	skillsDir := filepath.Join(claudeDir, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", skillsDir, err)
	}
	preservedFile := filepath.Join(skillsDir, "custom-skill.md")
	if err := os.WriteFile(preservedFile, []byte("skill"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", preservedFile, err)
	}

	if err := app.RecoverCC(); err != nil {
		t.Fatalf("RecoverCC() error = %v", err)
	}

	if _, err := os.Stat(preservedFile); err != nil {
		t.Fatalf("expected %q to remain, stat err = %v", preservedFile, err)
	}
}
