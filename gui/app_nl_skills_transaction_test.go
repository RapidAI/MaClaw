package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

func setupNLSkillTransactionApp(t *testing.T) *App {
	t.Helper()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(t.TempDir())
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: tempHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.toolRouter = NewToolRouter(nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	return app
}

func TestRenameNLSkillCommitsDirectoryYAMLConfigAndIndex(t *testing.T) {
	app := setupNLSkillTransactionApp(t)
	app.cachedSkillScanner = nil
	dir := filepath.Join(app.GetDataDir(), "skills", "rename-source")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	yamlPath := filepath.Join(dir, "skill.yaml")
	if err := os.WriteFile(yamlPath, []byte("name: rename-source\ndescription: test\nsteps: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{
		Name: "rename-source", SkillDir: dir, Source: "file", Status: "active",
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := app.RenameNLSkill("rename-source", "rename-target"); err != nil {
		t.Fatalf("RenameNLSkill() error = %v", err)
	}
	newDir := filepath.Join(filepath.Dir(dir), "rename-target")
	if _, err := os.Stat(newDir); err != nil {
		t.Fatalf("renamed directory missing: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(newDir, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "name: rename-target") {
		t.Fatalf("YAML name was not updated: %s", data)
	}
	var found bool
	for _, item := range app.skillExecutor.loadSkills() {
		if item.Name == "rename-target" {
			found = true
			if filepath.Clean(item.SkillDir) != filepath.Clean(newDir) {
				t.Fatalf("SkillDir = %q, want %q", item.SkillDir, newDir)
			}
		}
	}
	if !found {
		t.Fatalf("renamed skill not present after commit")
	}
}

func TestRenameNLSkillRollsBackWhenIndexRefreshFails(t *testing.T) {
	app := setupNLSkillTransactionApp(t)
	app.cachedSkillScanner = nil
	dir := filepath.Join(app.GetDataDir(), "skills", "rename-rollback")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	yamlPath := filepath.Join(dir, "skill.yaml")
	original := []byte("name: rename-rollback\ndescription: test\nsteps: []\n")
	if err := os.WriteFile(yamlPath, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{
		Name: "rename-rollback", SkillDir: dir, Source: "file", Status: "active",
	}}}); err != nil {
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
	err := app.RenameNLSkill("rename-rollback", "rename-new")
	if err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("RenameNLSkill() error = %v, want rollback", err)
	}
	if refreshCalls < 2 {
		t.Fatalf("refresh calls = %d, want failed commit plus rollback refresh", refreshCalls)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("original directory was not restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "rename-new")); !os.IsNotExist(err) {
		t.Fatalf("renamed directory still exists: err=%v", err)
	}
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatalf("YAML was not restored: %s", data)
	}
	if got := app.skillExecutor.loadSkills(); len(got) == 0 || got[0].Name != "rename-rollback" {
		t.Fatalf("config/list was not restored: %#v", got)
	}
}

func TestDeleteNLSkillRollsBackWhenIndexRefreshFails(t *testing.T) {
	app := setupNLSkillTransactionApp(t)
	app.cachedSkillScanner = nil
	if err := app.skillExecutor.Register(corelib.NLSkillEntry{
		Name: "delete-rollback", Source: "manual", Status: "active",
	}); err != nil {
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
	err := app.DeleteNLSkill("delete-rollback")
	if err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("DeleteNLSkill() error = %v, want rollback", err)
	}
	if refreshCalls < 2 {
		t.Fatalf("refresh calls = %d, want failed commit plus rollback refresh", refreshCalls)
	}
	found := false
	for _, item := range app.skillExecutor.loadSkills() {
		if item.Name == "delete-rollback" {
			found = true
		}
	}
	if !found {
		t.Fatalf("deleted skill was not restored after index failure")
	}
}

func TestDeleteNLSkillQuarantinesAndRemovesDirectoryOnCommit(t *testing.T) {
	app := setupNLSkillTransactionApp(t)
	app.cachedSkillScanner = nil
	dir := filepath.Join(app.GetDataDir(), "skills", "delete-commit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: delete-commit\nsteps: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{
		Name: "delete-commit", SkillDir: dir, Source: "file", Status: "active",
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := app.DeleteNLSkill("delete-commit"); err != nil {
		t.Fatalf("DeleteNLSkill() error = %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("directory still exists after committed delete: %v", err)
	}
	for _, item := range app.skillExecutor.loadSkills() {
		if item.Name == "delete-commit" {
			t.Fatalf("deleted skill remains in registry")
		}
	}
}

func TestRestoreSkillYAMLBackupRollsBackWhenIndexRefreshFails(t *testing.T) {
	app := setupNLSkillTransactionApp(t)
	app.cachedSkillScanner = nil
	dir := filepath.Join(app.GetDataDir(), "skills", "restore-rollback")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	yamlPath := filepath.Join(dir, "skill.yaml")
	original := []byte("name: restore-rollback\ndescription: current\nsteps: []\n")
	if err := os.WriteFile(yamlPath, original, 0o644); err != nil {
		t.Fatal(err)
	}
	v := &skill.Versioner{}
	if _, err := v.BackupCurrent(dir); err != nil {
		t.Fatal(err)
	}
	changed := []byte("name: restore-rollback\ndescription: changed\nsteps: []\n")
	if err := os.WriteFile(yamlPath, changed, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{
		Name: "restore-rollback", SkillDir: dir, Source: "file", Status: "active",
	}}}); err != nil {
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
	result := app.RestoreSkillYAMLBackup("restore-rollback", 1, true)
	if result["ok"] == true || result["state"] != "rolled_back" {
		t.Fatalf("restore result = %#v, want rolled_back", result)
	}
	if refreshCalls < 2 {
		t.Fatalf("refresh calls = %d, want commit and rollback refresh", refreshCalls)
	}
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(changed) {
		t.Fatalf("YAML was not restored after failed index publication: %q", data)
	}
}

func TestImportNLSkillZipRollsBackWhenIndexRefreshFails(t *testing.T) {
	app := setupNLSkillTransactionApp(t)
	app.cachedSkillScanner = nil
	zipPath := filepath.Join(t.TempDir(), "import-rollback.zip")
	createSkillZip(t, zipPath, map[string]string{
		"import-rollback/skill.yaml": "name: import-rollback\ndescription: test\nsteps: []\n",
	})
	refreshCalls := 0
	app.toolRouter.refreshSkillIndexOverride = func() error {
		refreshCalls++
		if refreshCalls == 1 {
			return os.ErrPermission
		}
		return nil
	}
	if _, err := app.importNLSkillZipPath(zipPath); err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("importNLSkillZipPath() error = %v, want rollback", err)
	}
	if refreshCalls < 2 {
		t.Fatalf("refresh calls = %d, want failed commit plus rollback refresh", refreshCalls)
	}
	dir := filepath.Join(app.GetDataDir(), "skills", "import-rollback")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("import directory remains after rollback: %v", err)
	}
	if summaries, err := skill.ListEvolutionCompensationSummaries(); err != nil {
		t.Fatalf("read compensation queue: %v", err)
	} else if len(summaries) != 0 {
		t.Fatalf("compensation queue not cleared after rollback: %#v", summaries)
	}
}
