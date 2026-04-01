package main

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallHubSkillSucceedsWhenHubExtractsFileBackedSkillDir(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	skillBody := fmt.Sprintf(`{
		"id": "hub-demo",
		"name": "demo-skill",
		"description": "from hub",
		"version": "1.0.0",
		"trust_level": "trusted",
		"triggers": ["demo"],
		"steps": [{"action": "noop", "params": {}, "on_error": "stop"}],
		"files": {
			"skill.yaml": %q,
			"SKILL.md": %q
		}
	}`,
		fmt.Sprintf("%q", base64.StdEncoding.EncodeToString([]byte("name: demo-skill\ndescription: from files\n"))),
		fmt.Sprintf("%q", base64.StdEncoding.EncodeToString([]byte("# Demo Skill\n"))),
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/skills/hub-demo/download" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(skillBody))
	}))
	defer server.Close()

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteHubCenterURL = server.URL
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.skillHubClient = NewSkillHubClient(app)

	if err := app.InstallHubSkill("hub-demo", server.URL); err != nil {
		t.Fatalf("InstallHubSkill() error = %v", err)
	}

	skills := app.skillExecutor.loadSkills()
	var hubCount, fileCount int
	for _, s := range skills {
		if s.Name != "demo-skill" {
			continue
		}
		switch s.Source {
		case "hub":
			hubCount++
		case "file":
			fileCount++
		}
	}
	if hubCount != 1 {
		t.Fatalf("hub entry count = %d, want 1; skills = %#v", hubCount, skills)
	}
	if fileCount != 0 {
		t.Fatalf("file entry count = %d, want 0; skills = %#v", fileCount, skills)
	}
}

func TestSkillExecutorRegisterAllowsHubSkillWhenPrimaryExtractedFilesExist(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	executor := NewSkillExecutor(app, nil, nil)

	primaryRoot := filepath.Join(tempHome, ".maclaw", "data", "skills")
	skillDir := filepath.Join(primaryRoot, "demo-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: demo-skill\ndescription: from files\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}

	entry := NLSkillEntry{
		Name:        "demo-skill",
		Description: "from hub",
		Source:      "hub",
		HubSkillID:  "hub-demo",
	}
	if err := executor.Register(entry); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	skills := executor.loadSkills()
	var hubCount, fileCount int
	for _, s := range skills {
		if s.Name != "demo-skill" {
			continue
		}
		switch s.Source {
		case "hub":
			hubCount++
		case "file":
			fileCount++
		}
	}
	if hubCount != 1 {
		t.Fatalf("hub entry count = %d, want 1; skills = %#v", hubCount, skills)
	}
	if fileCount != 0 {
		t.Fatalf("file entry count = %d, want 0; skills = %#v", fileCount, skills)
	}
}

func TestSkillExecutorDeleteRemovesExternalSkillDirs(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	externalRoot := filepath.Join(tempHome, "external-skills")
	cfg.ExternalSkillDirs = []string{externalRoot}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	executor := NewSkillExecutor(app, nil, nil)

	skillDir := filepath.Join(externalRoot, "demo-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: demo-skill\ndescription: external\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}

	if err := executor.Delete("demo-skill"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Fatalf("skillDir still exists after Delete(): err = %v", err)
	}
}
