package guiapp

import (
	"context"
	"encoding/json"
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
	// Keep the process-global Skill queue aligned with the App's effective
	// base directory. LoadConfig applies HOME/.maclaw when DataDir is empty;
	// using an unrelated temp root here would make queue assertions observe a
	// different file after the first config read.
	corelib.SetMaclawBaseDir(filepath.Join(tempHome, ".maclaw"))
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: tempHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.toolRouter = NewToolRouter(nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	return app
}

func TestSkillExecutorRenameRejectsDetachedExecutors(t *testing.T) {
	if err := NewSkillExecutor(nil, nil, nil).Rename("old", "new"); err == nil || !strings.Contains(err.Error(), "app not initialized") {
		t.Fatalf("detached executor error = %v, want app initialization failure", err)
	}

	app := &App{}
	canonical := NewSkillExecutor(app, nil, nil)
	app.skillExecutor = canonical
	stale := NewSkillExecutor(app, nil, nil)
	if err := stale.Rename("old", "new"); err == nil || !strings.Contains(err.Error(), "app-owned executor") {
		t.Fatalf("stale executor error = %v, want app-owned executor failure", err)
	}
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
		Name: "rename-source", SkillDir: dir + string(os.PathSeparator), Source: "file", Status: "active",
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

func TestRenameNLSkillResolvesStableAliasBeforeCommit(t *testing.T) {
	app := setupNLSkillTransactionApp(t)
	app.cachedSkillScanner = nil
	dir := filepath.Join(app.GetDataDir(), "skills", "rename-alias")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: canonical-name\nsteps: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{
		Name: "canonical-name", SkillID: "publisher:stable-alias", SkillDir: dir, Source: "file", Status: "active",
	}}}); err != nil {
		t.Fatal(err)
	}
	newDir := filepath.Join(filepath.Dir(dir), "rename-alias-target")
	// MatchesName accepts stable IDs, while SkillCommitter requires the
	// canonical registry Name. This exercises the alias-to-canonical bridge in
	// RenameNLSkill and guards against a false skill_not_found result.
	if err := app.RenameNLSkill("publisher:stable-alias", "rename-alias-target"); err != nil {
		t.Fatalf("RenameNLSkill(alias) error = %v", err)
	}
	if _, err := os.Stat(newDir); err != nil {
		t.Fatalf("renamed directory missing: %v", err)
	}
	entries := app.skillExecutor.loadSkills()
	if len(entries) != 1 || entries[0].Name != "rename-alias-target" || filepath.Clean(entries[0].SkillDir) != filepath.Clean(newDir) {
		t.Fatalf("renamed alias entry = %#v", entries)
	}
	events, err := skill.ListEvolutionAudit(skill.DefaultEvolutionAuditPath(), 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == "definition_renamed" {
			if event.Skill != "canonical-name" {
				t.Fatalf("rename audit used non-canonical identity: %+v", event)
			}
			return
		}
	}
	t.Fatal("missing definition_renamed audit event")
}

func TestRenameNLSkillAliasHonorsCanonicalPendingCompensation(t *testing.T) {
	app := setupNLSkillTransactionApp(t)
	app.cachedSkillScanner = nil
	dir := filepath.Join(app.GetDataDir(), "skills", "rename-alias-blocked")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: canonical-blocked\nsteps: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{
		Name: "canonical-blocked", SkillID: "publisher:blocked-alias", SkillDir: dir, Source: "file", Status: "active",
	}}}); err != nil {
		t.Fatal(err)
	}
	record := skill.NewEvolutionCompensationRecord("rename-alias-pending", "canonical-blocked", "rename", "", nil, false, nil, "pending")
	if err := skill.PersistEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = skill.ClearEvolutionCompensation(record.RequestID, record.Skill, record.Action) })
	if err := app.RenameNLSkill("publisher:blocked-alias", "rename-alias-blocked-target"); err == nil || !strings.Contains(err.Error(), "pending evolution compensation") {
		t.Fatalf("RenameNLSkill(alias) error = %v, want pending-compensation admission failure", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "rename-alias-blocked-target")); !os.IsNotExist(err) {
		t.Fatalf("rename destination changed despite blocked admission: %v", err)
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

func TestRenameNLSkillRollsBackWhenFinalAuditFails(t *testing.T) {
	app := setupNLSkillTransactionApp(t)
	app.cachedSkillScanner = nil
	dir := filepath.Join(app.GetDataDir(), "skills", "rename-audit-failure")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	yamlPath := filepath.Join(dir, "skill.yaml")
	original := []byte("name: rename-audit-failure\ndescription: original\nsteps: []\n")
	if err := os.WriteFile(yamlPath, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{
		Name: "rename-audit-failure", SkillDir: dir, Source: "file", Status: "active",
	}}}); err != nil {
		t.Fatal(err)
	}
	app.evolutionAuditWriter = func(event string, _ map[string]string) error {
		if event == "skill:definition_renamed" {
			return os.ErrPermission
		}
		return nil
	}
	if err := app.RenameNLSkill("rename-audit-failure", "rename-audit-failure-new"); err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("RenameNLSkill() error = %v, want final-audit rollback", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("original directory was not restored after audit failure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "rename-audit-failure-new")); !os.IsNotExist(err) {
		t.Fatalf("renamed directory remains after audit failure: %v", err)
	}
	if data, err := os.ReadFile(yamlPath); err != nil || string(data) != string(original) {
		t.Fatalf("YAML after audit rollback = %q, err=%v", data, err)
	}
	if entries := app.skillExecutor.loadSkills(); len(entries) != 1 || entries[0].Name != "rename-audit-failure" {
		t.Fatalf("registry after audit rollback = %#v", entries)
	}
}

func TestRenameNLSkillDurableCompensationCapturesMovedDirectoryAndYAML(t *testing.T) {
	app := setupNLSkillTransactionApp(t)
	app.cachedSkillScanner = nil
	dir := filepath.Join(app.GetDataDir(), "skills", "rename-durable")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	yamlPath := filepath.Join(dir, "skill.yaml")
	original := []byte("name: rename-durable\ndescription: durable\nsteps: []\n")
	if err := os.WriteFile(yamlPath, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{
		Name: "rename-durable", SkillDir: dir, Source: "file", Status: "active",
	}}}); err != nil {
		t.Fatal(err)
	}
	// Fail both the forward and rollback index publication. The directory and
	// YAML rollback succeeds, but the durable record must retain enough intent
	// to recover the same move after a process crash.
	app.toolRouter.refreshSkillIndexOverride = func() error { return os.ErrPermission }
	if err := app.RenameNLSkill("rename-durable", "rename-durable-new"); err == nil {
		t.Fatal("RenameNLSkill unexpectedly succeeded with a permanently failing index")
	}
	data, err := os.ReadFile(skill.DefaultEvolutionCompensationPath())
	if err != nil {
		t.Fatalf("read durable rename compensation: %v", err)
	}
	var record map[string]interface{}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("decode durable rename compensation: %v", err)
	}
	newDir := filepath.Join(filepath.Dir(dir), "rename-durable-new")
	if record["action"] != "rename" {
		t.Fatalf("durable rename compensation action = %#v", record["action"])
	}
	for field, want := range map[string]string{"dir_path": dir, "dir_backup_path": newDir, "yaml_path": yamlPath} {
		got, _ := record[field].(string)
		if filepath.Clean(got) != filepath.Clean(want) {
			t.Fatalf("durable rename compensation %s = %q, want %q", field, got, want)
		}
	}
	if got, readErr := os.ReadFile(yamlPath); readErr != nil || string(got) != string(original) {
		t.Fatalf("synchronous rollback did not restore YAML: err=%v data=%q", readErr, got)
	}
}

func TestRenameNLSkillRejectsDotPathDestinations(t *testing.T) {
	app := setupNLSkillTransactionApp(t)
	app.cachedSkillScanner = nil
	dir := filepath.Join(app.GetDataDir(), "skills", "rename-safe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: rename-safe\nsteps: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{
		Name: "rename-safe", SkillDir: dir, Source: "file", Status: "active",
	}}}); err != nil {
		t.Fatal(err)
	}
	for _, badName := range []string{".", "..", filepath.Join("..", "escaped")} {
		if err := app.RenameNLSkill("rename-safe", badName); err == nil {
			t.Fatalf("RenameNLSkill(%q) unexpectedly succeeded", badName)
		}
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("original directory changed after rejected destinations: %v", err)
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

func TestDeleteNLSkillRollsBackWhenFinalAuditFails(t *testing.T) {
	app := setupNLSkillTransactionApp(t)
	app.cachedSkillScanner = nil
	dir := filepath.Join(app.GetDataDir(), "skills", "delete-audit-failure")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: delete-audit-failure\nsteps: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{
		Name: "delete-audit-failure", SkillDir: dir, Source: "file", Status: "active",
	}}}); err != nil {
		t.Fatal(err)
	}
	app.evolutionAuditWriter = func(event string, _ map[string]string) error {
		if event == "skill:definition_deleted" {
			return os.ErrPermission
		}
		return nil
	}
	if err := app.DeleteNLSkill("delete-audit-failure"); err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("DeleteNLSkill() error = %v, want final-audit rollback", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("original directory was not restored after audit failure: %v", err)
	}
	if entries := app.skillExecutor.loadSkills(); len(entries) != 1 || entries[0].Name != "delete-audit-failure" {
		t.Fatalf("registry after audit rollback = %#v", entries)
	}
	if summaries, err := skill.ListEvolutionCompensationSummaries(); err != nil {
		t.Fatal(err)
	} else if len(summaries) != 0 {
		t.Fatalf("compensation queue remained after complete audit rollback: %#v", summaries)
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

func TestDeleteNLSkillCleanupFailurePersistsAndRecoversAfterRestart(t *testing.T) {
	app := setupNLSkillTransactionApp(t)
	app.cachedSkillScanner = nil
	dir := filepath.Join(app.GetDataDir(), "skills", "delete-cleanup-pending")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: delete-cleanup-pending\nsteps: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{
		Name: "delete-cleanup-pending", SkillDir: dir, Source: "file", Status: "active",
	}}}); err != nil {
		t.Fatal(err)
	}
	app.skillDeleteClearCompensation = func(string, string, string) error {
		return os.ErrPermission
	}
	if err := app.DeleteNLSkill("delete-cleanup-pending"); err == nil || !strings.Contains(err.Error(), "cleanup") {
		t.Fatalf("DeleteNLSkill() error = %v, want committed cleanup-pending failure", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("deleted directory was restored after committed cleanup failure: %v", err)
	}
	entries := app.skillExecutor.loadSkills()
	for _, entry := range entries {
		if entry.Name == "delete-cleanup-pending" {
			t.Fatalf("deleted skill remained in registry after committed cleanup failure: %#v", entries)
		}
	}
	summaries, err := skill.ListEvolutionCompensationSummaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].TransactionState != "committed" || summaries[0].CleanupStatus != "pending" {
		t.Fatalf("cleanup-pending summary = %#v", summaries)
	}

	// Simulate a fresh process. The quarantine is already gone, so recovery must
	// only clear the committed queue row and never recreate the deleted Skill.
	restarted := skill.NewEvolutionPipeline()
	recovered, pending, err := restarted.RecoverPendingCompensations()
	if err != nil || recovered != 1 || pending != 0 {
		t.Fatalf("restart recovery = recovered:%d pending:%d err:%v", recovered, pending, err)
	}
	if summaries, err := skill.ListEvolutionCompensationSummaries(); err != nil {
		t.Fatal(err)
	} else if len(summaries) != 0 {
		t.Fatalf("committed cleanup record remained after restart recovery: %#v", summaries)
	}
}

func TestDeleteNLSkillAliasClearsCanonicalConfigOnlyCache(t *testing.T) {
	app := setupNLSkillTransactionApp(t)
	entry := corelib.NLSkillEntry{
		Name: "config-only-canonical", SkillID: "publisher:config-only-alias", Source: "manual", Status: "active",
	}
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{entry}}); err != nil {
		t.Fatal(err)
	}
	// Warm both the scanner and executor caches before deleting through the
	// alias. A config-only entry has no directory identity to remove, so the
	// canonical-name eviction is the only reliable cache fence.
	app.ensureRemoteInfra()
	if got := app.skillExecutor.loadSkills(); len(got) != 1 || got[0].Name != entry.Name {
		t.Fatalf("pre-delete cache = %#v", got)
	}
	if err := app.DeleteNLSkill("publisher:config-only-alias"); err != nil {
		t.Fatalf("DeleteNLSkill(alias) error = %v", err)
	}
	for _, got := range app.skillExecutor.loadSkills() {
		if got.Name == entry.Name {
			t.Fatalf("canonical config-only Skill remained cached after alias delete: %#v", got)
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

func TestImportNLSkillZipDoesNotClaimOtherRecoveryScope(t *testing.T) {
	app := setupNLSkillTransactionApp(t)
	app.cachedSkillScanner = nil
	foreignRoot := filepath.Join(t.TempDir(), "foreign-skills")
	foreignDir := filepath.Join(foreignRoot, "foreign-pending")
	if err := os.MkdirAll(foreignDir, 0o755); err != nil {
		t.Fatal(err)
	}
	foreignRecord := skill.NewEvolutionCompensationRecord("foreign-import-scope", "foreign-pending", "import", "", nil, false, nil, "test")
	foreignRecord.SetRecoveryScope(foreignRoot)
	foreignRecord.SetCreatedDirectories([]string{foreignDir})
	if err := skill.PersistEvolutionCompensation(foreignRecord); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = skill.ClearEvolutionCompensation(foreignRecord.RequestID, foreignRecord.Skill, foreignRecord.Action)
		_ = os.RemoveAll(foreignRoot)
	})

	zipPath := filepath.Join(t.TempDir(), "scoped-import.zip")
	createSkillZip(t, zipPath, map[string]string{
		"scoped-import/skill.yaml": "name: scoped-import\ndescription: scoped\nsteps: []\n",
	})
	if _, err := app.importNLSkillZipPath(zipPath); err != nil {
		t.Fatalf("importNLSkillZipPath() error = %v", err)
	}
	foundForeign := false
	for _, summary := range mustReadCompensationSummaries(t) {
		if summary.RequestID == foreignRecord.RequestID {
			foundForeign = true
			break
		}
	}
	if !foundForeign {
		t.Fatal("import recovery claimed or removed a record belonging to another scope")
	}
	if _, err := os.Stat(foreignDir); err != nil {
		t.Fatalf("foreign recovery directory was mutated: %v", err)
	}
}

func mustReadCompensationSummaries(t *testing.T) []skill.EvolutionCompensationSummary {
	t.Helper()
	summaries, err := skill.ListEvolutionCompensationSummaries()
	if err != nil {
		t.Fatalf("read compensation summaries: %v", err)
	}
	return summaries
}
