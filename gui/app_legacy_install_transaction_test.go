package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

// TestInstallSkillUsesOneOuterCompensationAndOneCommitAudit pins the legacy
// GUI install boundary. InstallSkill composes extraction, metadata and index
// publication, but it must expose one durable recovery record and one final
// commit audit. The metadata helper may not emit an intermediate "installed"
// audit while the outer transaction is still capable of rolling back.
func TestInstallSkillUsesOneOuterCompensationAndOneCommitAudit(t *testing.T) {
	t.Helper()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(filepath.Join(tempHome, ".maclaw"))
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	app := &App{testHomeDir: tempHome}
	app.toolRouter = NewToolRouter(nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })

	zipPath := filepath.Join(t.TempDir(), "legacy-install.zip")
	createSkillZip(t, zipPath, map[string]string{
		"legacy-install/skill.md":   "# legacy-install\n\nA safe legacy package.\n",
		"legacy-install/skill.yaml": "name: legacy-install\ndescription: legacy install\nsteps: []\n",
	})
	projectPath := t.TempDir()
	result := app.InstallSkillDetailed("legacy-install", "legacy install", "zip", zipPath, "project", projectPath, "codex")
	if !result.OK || result.State != "committed" || result.CleanupStatus != "clear" {
		t.Fatalf("InstallSkillDetailed() result = %#v, want committed/clear", result)
	}

	// The package registry is still a legacy file, but its successful write must
	// not leave the durable recovery queue behind after the outer commit.
	metadataPath := filepath.Join(app.GetSkillsDir("codex"), "metadata.json")
	metadata, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	var entries []corelib.Skill
	if err := json.Unmarshal(metadata, &entries); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "legacy-install" {
		t.Fatalf("metadata entries = %#v, want one legacy-install entry", entries)
	}

	if summaries, err := skill.ListEvolutionCompensationSummaries(); err != nil {
		t.Fatalf("read compensation queue: %v", err)
	} else if len(summaries) != 0 {
		t.Fatalf("compensation queue = %#v, want empty after committed cleanup", summaries)
	}

	events, err := skill.ListEvolutionAudit(skill.DefaultEvolutionAuditPath(), skill.EvolutionAuditMaxKeep)
	if err != nil {
		t.Fatalf("read evolution audit: %v", err)
	}
	var committed, intermediate int
	for _, event := range events {
		if event.RequestID == "" || !strings.EqualFold(event.Skill, "legacy-install") {
			continue
		}
		switch event.Kind {
		case "legacy_install_committed":
			committed++
		case "legacy_installed":
			intermediate++
		}
	}
	if committed != 1 {
		t.Fatalf("legacy install commit audits = %d, want exactly one; events=%#v", committed, events)
	}
	if intermediate != 0 {
		t.Fatalf("legacy install emitted %d intermediate installed audits, want zero", intermediate)
	}
}

func TestInstallSkillIndexFailureRestoresProjectDirectoryAndMetadata(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(filepath.Join(tempHome, ".maclaw"))
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	app := &App{testHomeDir: tempHome}
	app.toolRouter = NewToolRouter(nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })

	zipPath := filepath.Join(t.TempDir(), "legacy-index-failure.zip")
	createSkillZip(t, zipPath, map[string]string{
		"legacy-index-failure/skill.md":   "# legacy-index-failure\n\nA safe package.\n",
		"legacy-index-failure/skill.yaml": "name: legacy-index-failure\ndescription: index failure\nsteps: []\n",
	})
	projectPath := t.TempDir()
	refreshCalls := 0
	app.toolRouter.refreshSkillIndexOverride = func() error {
		refreshCalls++
		if refreshCalls == 1 {
			return os.ErrPermission
		}
		return nil
	}

	err := app.InstallSkill("legacy-index-failure", "index failure", "zip", zipPath, "project", projectPath, "codex")
	if err == nil {
		t.Fatal("InstallSkill() error = nil, want checked-index failure")
	}
	if refreshCalls < 2 {
		t.Fatalf("checked index refresh calls = %d, want failed publish plus rollback refresh", refreshCalls)
	}
	projectSkillDir := filepath.Join(projectPath, getToolConfigDirName("codex"), "skills", "legacy-index-failure")
	if _, statErr := os.Stat(projectSkillDir); !os.IsNotExist(statErr) {
		t.Fatalf("project skill directory survived rollback: %v", statErr)
	}
	metadataPath := filepath.Join(app.GetSkillsDir("codex"), "metadata.json")
	if _, statErr := os.Stat(metadataPath); !os.IsNotExist(statErr) {
		t.Fatalf("metadata registry survived rollback: %v", statErr)
	}
	if summaries, readErr := skill.ListEvolutionCompensationSummaries(); readErr != nil {
		t.Fatalf("read compensation queue: %v", readErr)
	} else if len(summaries) != 0 {
		t.Fatalf("compensation queue after rollback = %#v, want empty", summaries)
	}
	events, readErr := skill.ListEvolutionAudit(skill.DefaultEvolutionAuditPath(), skill.EvolutionAuditMaxKeep)
	if readErr != nil {
		t.Fatalf("read evolution audit: %v", readErr)
	}
	for _, event := range events {
		if strings.EqualFold(event.Skill, "legacy-index-failure") && event.Kind == "legacy_install_committed" {
			t.Fatalf("failed install emitted committed audit: %#v", event)
		}
	}
}

func TestDeleteSkillIndexFailureRestoresPackageAndMetadata(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(filepath.Join(tempHome, ".maclaw"))
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	app := &App{testHomeDir: tempHome}
	app.toolRouter = NewToolRouter(nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })

	skillsDir := app.GetSkillsDir("codex")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatal(err)
	}
	packagePath := filepath.Join(skillsDir, "delete-index-failure.zip")
	if err := os.WriteFile(packagePath, []byte("zip-placeholder"), 0644); err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(skillsDir, "metadata.json")
	original := []corelib.Skill{{Name: "delete-index-failure", Description: "keep", Type: "zip", Value: filepath.Base(packagePath)}}
	data, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	refreshCalls := 0
	app.toolRouter.refreshSkillIndexOverride = func() error {
		refreshCalls++
		if refreshCalls == 1 {
			return os.ErrPermission
		}
		return nil
	}
	err = app.DeleteSkill("delete-index-failure", "codex")
	if err == nil {
		t.Fatal("DeleteSkill() error = nil, want checked-index failure")
	}
	if refreshCalls < 2 {
		t.Fatalf("checked index refresh calls = %d, want failed publish plus rollback refresh", refreshCalls)
	}
	if _, statErr := os.Stat(packagePath); statErr != nil {
		t.Fatalf("package was not restored after index failure: %v", statErr)
	}
	got, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatalf("metadata was not restored after index failure: %q", got)
	}
	if summaries, readErr := skill.ListEvolutionCompensationSummaries(); readErr != nil {
		t.Fatal(readErr)
	} else if len(summaries) != 0 {
		t.Fatalf("compensation queue after rollback = %#v, want empty", summaries)
	}
	events, readErr := skill.ListEvolutionAudit(skill.DefaultEvolutionAuditPath(), skill.EvolutionAuditMaxKeep)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, event := range events {
		if strings.EqualFold(event.Skill, "delete-index-failure") && event.Kind == "legacy_deleted" {
			t.Fatalf("failed delete emitted committed audit: %#v", event)
		}
	}
}

func TestInstallSkillFinalAuditFailureRollsBackWithoutCommittedResult(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	base := filepath.Join(tempHome, ".maclaw")
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	app := &App{testHomeDir: tempHome}
	app.toolRouter = NewToolRouter(nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })

	zipPath := filepath.Join(t.TempDir(), "legacy-audit-failure.zip")
	createSkillZip(t, zipPath, map[string]string{
		"legacy-audit-failure/skill.md":   "# legacy-audit-failure\n\nA safe package.\n",
		"legacy-audit-failure/skill.yaml": "name: legacy-audit-failure\ndescription: audit failure\nsteps: []\n",
	})
	// Make the strict evolution-audit sink unavailable only at the final
	// business boundary. The durable compensation queue remains writable so
	// rollback can prove that the failed install was not committed.
	auditPath := skill.DefaultEvolutionAuditPath()
	if err := os.MkdirAll(auditPath, 0755); err != nil {
		t.Fatal(err)
	}
	projectPath := t.TempDir()
	err := app.InstallSkill("legacy-audit-failure", "audit failure", "zip", zipPath, "project", projectPath, "codex")
	if err == nil {
		t.Fatal("InstallSkill() error = nil, want final-audit failure")
	}
	var committedErr *legacySkillCommitError
	if errors.As(err, &committedErr) && committedErr.committed {
		t.Fatalf("final-audit failure reported committed result: %v", err)
	}
	projectSkillDir := filepath.Join(projectPath, getToolConfigDirName("codex"), "skills", "legacy-audit-failure")
	if _, statErr := os.Stat(projectSkillDir); !os.IsNotExist(statErr) {
		t.Fatalf("project skill directory survived final-audit rollback: %v", statErr)
	}
	metadataPath := filepath.Join(app.GetSkillsDir("codex"), "metadata.json")
	if _, statErr := os.Stat(metadataPath); !os.IsNotExist(statErr) {
		t.Fatalf("metadata registry survived final-audit rollback: %v", statErr)
	}
	if summaries, readErr := skill.ListEvolutionCompensationSummaries(); readErr != nil {
		t.Fatal(readErr)
	} else if len(summaries) != 0 {
		t.Fatalf("compensation queue after final-audit rollback = %#v, want empty", summaries)
	}
}

func TestInstallSkillDetailedReportsSettingsFailureAndRollsBack(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(filepath.Join(tempHome, ".maclaw"))
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	app := &App{testHomeDir: tempHome}
	app.toolRouter = NewToolRouter(nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	zipPath := filepath.Join(t.TempDir(), "plugin-settings-failure.zip")
	createSkillZip(t, zipPath, map[string]string{
		"plugin-settings-failure/skill.yaml": "name: plugin-settings-failure\ndescription: plugin\nsteps: []\n",
	})
	settingsPath := filepath.Join(tempHome, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	originalSettings := []byte(`{"enabledPlugins":{"existing@market":true},"extraKnownMarketplaces":{"anthropic-agent-skills":{"source":{"source":"github","repo":"anthropics/skills"}},"superpowers-marketplace":{"source":{"source":"github","repo":"obra/superpowers-marketplace"}}}}`)
	if err := os.WriteFile(settingsPath, originalSettings, 0o644); err != nil {
		t.Fatal(err)
	}
	app.legacySkillWriteFile = func(path string, data []byte) error {
		if filepath.Clean(path) == filepath.Clean(settingsPath) {
			return os.ErrPermission
		}
		return atomicWriteFile(path, data)
	}
	result := app.InstallSkillDetailed("plugin-settings-failure", "plugin", "address", "plugin-settings-failure@market", "user", "", "claude")
	if result.OK || result.State != "rolled_back" {
		t.Fatalf("structured result = %#v, want rolled_back failure", result)
	}
	if result.RequestID == "" || result.FailureReason == "" {
		t.Fatalf("structured result missing request/error: %#v", result)
	}
	got, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(originalSettings) {
		t.Fatalf("settings changed after failed install: %q", got)
	}
	if summaries, err := skill.ListEvolutionCompensationSummaries(); err != nil {
		t.Fatal(err)
	} else if len(summaries) != 0 {
		t.Fatalf("compensation queue after settings rollback = %#v, want empty", summaries)
	}
}
