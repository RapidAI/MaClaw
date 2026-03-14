//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureClaudeOnboardingCreatesFileWhenMissing(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{}
	if err := ensureClaudeOnboardingComplete(app, `D:\workprj\test`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	configPath := filepath.Join(tmpHome, ".claude.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config file not created: %v", err)
	}

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if !isTruthy(config["hasCompletedOnboarding"]) {
		t.Error("hasCompletedOnboarding should be true")
	}
	if config["theme"] != "dark" {
		t.Errorf("theme = %v, want dark", config["theme"])
	}

	// Check project trust
	projects, ok := config["projects"].(map[string]any)
	if !ok {
		t.Fatal("projects map missing")
	}
	entry, ok := projects["D:/workprj/test"].(map[string]any)
	if !ok {
		t.Fatal("project entry missing for D:/workprj/test")
	}
	if !isTruthy(entry["hasTrustDialogAccepted"]) {
		t.Error("hasTrustDialogAccepted should be true")
	}
}

func TestEnsureClaudeOnboardingPreservesExistingConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	configPath := filepath.Join(tmpHome, ".claude.json")
	existing := map[string]any{
		"theme":                  "light",
		"hasCompletedOnboarding": true,
		"customSetting":          "keep-me",
		"oauthAccessToken":       "secret-token",
		"projects": map[string]any{
			"D:/existing/project": map[string]any{
				"hasTrustDialogAccepted": true,
				"allowedTools":           []any{"Bash(git *)"},
			},
		},
	}
	data, _ := json.Marshal(existing)
	os.WriteFile(configPath, data, 0o644)

	app := &App{}
	if err := ensureClaudeOnboardingComplete(app, `D:\new\project`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, _ := os.ReadFile(configPath)
	var config map[string]any
	json.Unmarshal(updated, &config)

	// Should preserve existing values
	if config["theme"] != "light" {
		t.Errorf("theme was overwritten: got %v, want light", config["theme"])
	}
	if config["customSetting"] != "keep-me" {
		t.Error("customSetting was lost")
	}
	if config["oauthAccessToken"] != "secret-token" {
		t.Error("oauthAccessToken was lost")
	}

	// Should preserve existing project entry
	projects := config["projects"].(map[string]any)
	existingEntry := projects["D:/existing/project"].(map[string]any)
	if !isTruthy(existingEntry["hasTrustDialogAccepted"]) {
		t.Error("existing project trust was lost")
	}

	// Should add new project entry
	newEntry, ok := projects["D:/new/project"].(map[string]any)
	if !ok {
		t.Fatal("new project entry not created")
	}
	if !isTruthy(newEntry["hasTrustDialogAccepted"]) {
		t.Error("new project hasTrustDialogAccepted should be true")
	}
}

func TestEnsureClaudeOnboardingIdempotent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	configPath := filepath.Join(tmpHome, ".claude.json")
	existing := map[string]any{
		"hasCompletedOnboarding": true,
		"theme":                  "solarized",
		"projects": map[string]any{
			"D:/workprj/test": map[string]any{
				"hasTrustDialogAccepted": true,
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(configPath, data, 0o644)

	beforeStat, _ := os.Stat(configPath)

	app := &App{}
	if err := ensureClaudeOnboardingComplete(app, `D:\workprj\test`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	afterStat, _ := os.Stat(configPath)

	if !afterStat.ModTime().Equal(beforeStat.ModTime()) {
		t.Error("file was rewritten even though no changes were needed")
	}
}

func TestEnsureClaudeOnboardingHandlesCorruptFile(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	configPath := filepath.Join(tmpHome, ".claude.json")
	os.WriteFile(configPath, []byte("not valid json{{{"), 0o644)

	app := &App{}
	if err := ensureClaudeOnboardingComplete(app, `D:\test`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	backupPath := configPath + ".bak"
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Error("corrupt file was not backed up")
	}

	data, _ := os.ReadFile(configPath)
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("new config is not valid JSON: %v", err)
	}
	if !isTruthy(config["hasCompletedOnboarding"]) {
		t.Error("hasCompletedOnboarding should be true")
	}
}

func TestEnsureProjectTrustWithExistingUntrustedEntry(t *testing.T) {
	config := map[string]any{
		"projects": map[string]any{
			"D:/workprj/test": map[string]any{
				"allowedTools": []any{},
				// hasTrustDialogAccepted is missing
			},
		},
	}

	changed := ensureProjectTrust(config, `D:\workprj\test`)
	if !changed {
		t.Error("expected change when trust was missing")
	}

	projects := config["projects"].(map[string]any)
	entry := projects["D:/workprj/test"].(map[string]any)
	if !isTruthy(entry["hasTrustDialogAccepted"]) {
		t.Error("hasTrustDialogAccepted should be true")
	}
	// Should preserve existing allowedTools
	if entry["allowedTools"] == nil {
		t.Error("allowedTools was lost")
	}
}
