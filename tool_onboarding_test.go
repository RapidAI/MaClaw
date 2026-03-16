//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureGeminiOnboardingCreatesSettings(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{}
	if err := ensureGeminiOnboardingComplete(app); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	configPath := filepath.Join(tmpHome, ".gemini", "settings.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("settings file not created: %v", err)
	}

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	ui, ok := config["ui"].(map[string]any)
	if !ok {
		t.Fatal("ui section missing")
	}
	if ui["theme"] != "Default Dark" {
		t.Errorf("theme = %v, want Default Dark", ui["theme"])
	}
}

func TestEnsureGeminiOnboardingPreservesExisting(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	dir := filepath.Join(tmpHome, ".gemini")
	os.MkdirAll(dir, 0o755)
	configPath := filepath.Join(dir, "settings.json")

	existing := map[string]any{
		"ui": map[string]any{
			"theme":    "Solarized",
			"hideTips": true,
		},
		"general": map[string]any{
			"vimMode": true,
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(configPath, data, 0o644)

	app := &App{}
	if err := ensureGeminiOnboardingComplete(app); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, _ := os.ReadFile(configPath)
	var config map[string]any
	json.Unmarshal(updated, &config)

	ui := config["ui"].(map[string]any)
	if ui["theme"] != "Solarized" {
		t.Errorf("theme was overwritten: got %v, want Solarized", ui["theme"])
	}
	if ui["hideTips"] != true {
		t.Error("hideTips was lost")
	}

	general := config["general"].(map[string]any)
	if general["vimMode"] != true {
		t.Error("general.vimMode was lost")
	}
}

func TestEnsureGeminiOnboardingIdempotent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	dir := filepath.Join(tmpHome, ".gemini")
	os.MkdirAll(dir, 0o755)
	configPath := filepath.Join(dir, "settings.json")

	existing := map[string]any{
		"ui": map[string]any{
			"theme": "GitHub",
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(configPath, data, 0o644)

	beforeStat, _ := os.Stat(configPath)

	app := &App{}
	if err := ensureGeminiOnboardingComplete(app); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	afterStat, _ := os.Stat(configPath)
	if !afterStat.ModTime().Equal(beforeStat.ModTime()) {
		t.Error("file was rewritten even though no changes were needed")
	}
}

func TestEnsureGeminiOnboardingHandlesCorruptFile(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	dir := filepath.Join(tmpHome, ".gemini")
	os.MkdirAll(dir, 0o755)
	configPath := filepath.Join(dir, "settings.json")
	os.WriteFile(configPath, []byte("not valid json{{{"), 0o644)

	app := &App{}
	if err := ensureGeminiOnboardingComplete(app); err != nil {
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
	ui := config["ui"].(map[string]any)
	if ui["theme"] != "Default Dark" {
		t.Errorf("theme = %v, want Default Dark", ui["theme"])
	}
}

func TestEnsureKodeOnboardingCreatesConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{}
	if err := ensureKodeOnboardingComplete(app, `D:\projects\myapp`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	configPath := filepath.Join(tmpHome, ".kode.json")
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

	projects, ok := config["projects"].(map[string]any)
	if !ok {
		t.Fatal("projects map missing")
	}
	entry, ok := projects["D:/projects/myapp"].(map[string]any)
	if !ok {
		t.Fatal("project entry missing")
	}
	if !isTruthy(entry["hasTrustDialogAccepted"]) {
		t.Error("hasTrustDialogAccepted should be true")
	}
}

func TestEnsureKodeOnboardingPreservesExisting(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	configPath := filepath.Join(tmpHome, ".kode.json")
	existing := map[string]any{
		"hasCompletedOnboarding": true,
		"theme":                  "light",
		"customKey":              "keep-me",
	}
	data, _ := json.Marshal(existing)
	os.WriteFile(configPath, data, 0o644)

	app := &App{}
	if err := ensureKodeOnboardingComplete(app, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, _ := os.ReadFile(configPath)
	var config map[string]any
	json.Unmarshal(updated, &config)

	if config["theme"] != "light" {
		t.Errorf("theme was overwritten: got %v, want light", config["theme"])
	}
	if config["customKey"] != "keep-me" {
		t.Error("customKey was lost")
	}
}

func TestEnsureKodeOnboardingIdempotent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	configPath := filepath.Join(tmpHome, ".kode.json")
	existing := map[string]any{
		"hasCompletedOnboarding": true,
		"theme":                  "dark",
		"projects": map[string]any{
			"D:/test": map[string]any{
				"hasTrustDialogAccepted": true,
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(configPath, data, 0o644)

	beforeStat, _ := os.Stat(configPath)

	app := &App{}
	if err := ensureKodeOnboardingComplete(app, `D:\test`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	afterStat, _ := os.Stat(configPath)
	if !afterStat.ModTime().Equal(beforeStat.ModTime()) {
		t.Error("file was rewritten even though no changes were needed")
	}
}

func TestEnsureKodeOnboardingHandlesCorruptFile(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	configPath := filepath.Join(tmpHome, ".kode.json")
	os.WriteFile(configPath, []byte("not valid json{{{"), 0o644)

	app := &App{}
	if err := ensureKodeOnboardingComplete(app, `D:\test`); err != nil {
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

func TestEnsureToolOnboardingDispatch(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{}

	// Should not panic for unknown tools.
	ensureToolOnboardingComplete(app, "unknown-tool", "/some/path")

	// Should handle claude.
	ensureToolOnboardingComplete(app, "claude", `D:\test`)
	if _, err := os.Stat(filepath.Join(tmpHome, ".claude.json")); os.IsNotExist(err) {
		t.Error("claude onboarding should have created .claude.json")
	}

	// Should handle gemini.
	ensureToolOnboardingComplete(app, "gemini", "")
	if _, err := os.Stat(filepath.Join(tmpHome, ".gemini", "settings.json")); os.IsNotExist(err) {
		t.Error("gemini onboarding should have created settings.json")
	}

	// Should handle kode.
	ensureToolOnboardingComplete(app, "kode", `D:\test`)
	if _, err := os.Stat(filepath.Join(tmpHome, ".kode.json")); os.IsNotExist(err) {
		t.Error("kode onboarding should have created .kode.json")
	}

	// Should handle codebuddy.
	ensureToolOnboardingComplete(app, "codebuddy", `D:\test`)
	if _, err := os.Stat(filepath.Join(tmpHome, ".codebuddy.json")); os.IsNotExist(err) {
		t.Error("codebuddy onboarding should have created .codebuddy.json")
	}

	// Should be a no-op for tools without onboarding.
	ensureToolOnboardingComplete(app, "codex", "")
	ensureToolOnboardingComplete(app, "iflow", "")
	ensureToolOnboardingComplete(app, "kilo", "")
	ensureToolOnboardingComplete(app, "cursor", "")
}

func TestEnsureCodeBuddyOnboardingCreatesConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{}
	if err := ensureCodeBuddyOnboardingComplete(app, `D:\projects\myapp`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	configPath := filepath.Join(tmpHome, ".codebuddy.json")
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

	projects, ok := config["projects"].(map[string]any)
	if !ok {
		t.Fatal("projects map missing")
	}
	entry, ok := projects["D:/projects/myapp"].(map[string]any)
	if !ok {
		t.Fatal("project entry missing")
	}
	if !isTruthy(entry["hasTrustDialogAccepted"]) {
		t.Error("hasTrustDialogAccepted should be true")
	}
}

func TestEnsureCodeBuddyOnboardingPreservesExisting(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	configPath := filepath.Join(tmpHome, ".codebuddy.json")
	existing := map[string]any{
		"hasCompletedOnboarding": true,
		"theme":                  "light",
		"customKey":              "keep-me",
	}
	data, _ := json.Marshal(existing)
	os.WriteFile(configPath, data, 0o644)

	app := &App{}
	if err := ensureCodeBuddyOnboardingComplete(app, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, _ := os.ReadFile(configPath)
	var config map[string]any
	json.Unmarshal(updated, &config)

	if config["theme"] != "light" {
		t.Errorf("theme was overwritten: got %v, want light", config["theme"])
	}
	if config["customKey"] != "keep-me" {
		t.Error("customKey was lost")
	}
}

func TestEnsureCodeBuddyOnboardingIdempotent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	configPath := filepath.Join(tmpHome, ".codebuddy.json")
	existing := map[string]any{
		"hasCompletedOnboarding": true,
		"theme":                  "dark",
		"projects": map[string]any{
			"D:/test": map[string]any{
				"hasTrustDialogAccepted": true,
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(configPath, data, 0o644)

	beforeStat, _ := os.Stat(configPath)

	app := &App{}
	if err := ensureCodeBuddyOnboardingComplete(app, `D:\test`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	afterStat, _ := os.Stat(configPath)
	if !afterStat.ModTime().Equal(beforeStat.ModTime()) {
		t.Error("file was rewritten even though no changes were needed")
	}
}

func TestEnsureCodeBuddyOnboardingHandlesCorruptFile(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	configPath := filepath.Join(tmpHome, ".codebuddy.json")
	os.WriteFile(configPath, []byte("not valid json{{{"), 0o644)

	app := &App{}
	if err := ensureCodeBuddyOnboardingComplete(app, `D:\test`); err != nil {
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

// --- Backup / Restore tests ---

func TestBackupToolConfigsRestoresExistingFile(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	// Create an existing claude config with user's own auth token.
	configPath := filepath.Join(tmpHome, ".claude.json")
	original := map[string]any{
		"oauthAccessToken": "user-secret-token",
		"theme":            "solarized",
	}
	origData, _ := json.MarshalIndent(original, "", "  ")
	os.WriteFile(configPath, origData, 0o644)

	app := &App{}
	restore := backupToolConfigs(app, "claude")

	// Simulate onboarding modifying the file.
	ensureClaudeOnboardingComplete(app, `D:\test`)

	// Verify onboarding changed the file.
	modified, _ := os.ReadFile(configPath)
	var modConfig map[string]any
	json.Unmarshal(modified, &modConfig)
	if !isTruthy(modConfig["hasCompletedOnboarding"]) {
		t.Fatal("onboarding should have added hasCompletedOnboarding")
	}

	// Restore.
	restore()

	// Verify original content is back.
	restored, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config file missing after restore: %v", err)
	}
	var restoredConfig map[string]any
	json.Unmarshal(restored, &restoredConfig)

	if restoredConfig["oauthAccessToken"] != "user-secret-token" {
		t.Errorf("oauthAccessToken lost after restore: %v", restoredConfig["oauthAccessToken"])
	}
	if restoredConfig["theme"] != "solarized" {
		t.Errorf("theme changed after restore: %v", restoredConfig["theme"])
	}
	if restoredConfig["hasCompletedOnboarding"] != nil {
		t.Error("hasCompletedOnboarding should not exist after restore")
	}
}

func TestBackupToolConfigsRemovesNewlyCreatedFile(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	configPath := filepath.Join(tmpHome, ".kode.json")

	// No config file exists yet.
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatal("config should not exist before test")
	}

	app := &App{}
	restore := backupToolConfigs(app, "kode")

	// Onboarding creates the file.
	ensureKodeOnboardingComplete(app, `D:\test`)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("onboarding should have created config")
	}

	// Restore removes the file.
	restore()

	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Error("config file should be removed after restore (it didn't exist before)")
	}
}

func TestBackupToolConfigsGeminiRestore(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	dir := filepath.Join(tmpHome, ".gemini")
	os.MkdirAll(dir, 0o755)
	configPath := filepath.Join(dir, "settings.json")

	original := map[string]any{
		"ui": map[string]any{
			"theme": "Monokai",
		},
		"auth": map[string]any{
			"token": "google-oauth-token",
		},
	}
	origData, _ := json.MarshalIndent(original, "", "  ")
	os.WriteFile(configPath, origData, 0o644)

	app := &App{}
	restore := backupToolConfigs(app, "gemini")

	// Onboarding should not change theme (already set), but let's
	// simulate a scenario where it does by writing directly.
	modified := map[string]any{
		"ui": map[string]any{"theme": "Default Dark"},
	}
	modData, _ := json.MarshalIndent(modified, "", "  ")
	os.WriteFile(configPath, modData, 0o644)

	restore()

	restored, _ := os.ReadFile(configPath)
	var restoredConfig map[string]any
	json.Unmarshal(restored, &restoredConfig)

	ui := restoredConfig["ui"].(map[string]any)
	if ui["theme"] != "Monokai" {
		t.Errorf("theme not restored: got %v", ui["theme"])
	}
	auth := restoredConfig["auth"].(map[string]any)
	if auth["token"] != "google-oauth-token" {
		t.Errorf("auth token lost: got %v", auth["token"])
	}
}

func TestBackupToolConfigsNoopForUnknownTool(t *testing.T) {
	restore := backupToolConfigs(nil, "codex")
	// Should not panic.
	restore()
}

func TestBackupToolConfigsDoubleRestoreIsSafe(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	configPath := filepath.Join(tmpHome, ".claude.json")
	os.WriteFile(configPath, []byte(`{"theme":"light"}`), 0o644)

	app := &App{}
	restore := backupToolConfigs(app, "claude")

	// Modify.
	os.WriteFile(configPath, []byte(`{"theme":"dark","hasCompletedOnboarding":true}`), 0o644)

	restore()
	restore() // second call should be safe

	data, _ := os.ReadFile(configPath)
	var config map[string]any
	json.Unmarshal(data, &config)
	if config["theme"] != "light" {
		t.Errorf("theme should be light after restore, got %v", config["theme"])
	}
}

func TestToolConfigPathsReturnsCorrectPaths(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	tests := []struct {
		tool     string
		wantNil  bool
		contains string
	}{
		{"claude", false, ".claude.json"},
		{"kode", false, ".kode.json"},
		{"codebuddy", false, ".codebuddy.json"},
		{"gemini", false, "settings.json"},
		{"codex", true, ""},
		{"cursor", true, ""},
		{"unknown", true, ""},
	}

	for _, tt := range tests {
		paths := toolConfigPaths(tt.tool)
		if tt.wantNil && paths != nil {
			t.Errorf("toolConfigPaths(%q) should be nil", tt.tool)
		}
		if !tt.wantNil {
			if len(paths) == 0 {
				t.Errorf("toolConfigPaths(%q) should not be empty", tt.tool)
			} else if !strings.Contains(paths[0], tt.contains) {
				t.Errorf("toolConfigPaths(%q) = %v, want path containing %q", tt.tool, paths, tt.contains)
			}
		}
	}
}
