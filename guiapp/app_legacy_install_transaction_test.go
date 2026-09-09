package guiapp

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
	if entries[0].Value != filepath.Base(zipPath) {
		t.Fatalf("metadata package value = %q, want stable source basename %q", entries[0].Value, filepath.Base(zipPath))
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

func TestInstallSkillBlocksBeforeMutationWhenEvolutionQueueUnreadable(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(filepath.Join(tempHome, ".maclaw"))
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	queuePath := skill.DefaultEvolutionCompensationPath()
	if err := os.MkdirAll(filepath.Dir(queuePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(queuePath, []byte("{malformed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "blocked-install.zip")
	createSkillZip(t, zipPath, map[string]string{
		"blocked-install/skill.md":   "# blocked-install\n",
		"blocked-install/skill.yaml": "name: blocked-install\ndescription: blocked\nsteps: []\n",
	})
	app := &App{testHomeDir: tempHome}
	app.toolRouter = NewToolRouter(nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	result := app.InstallSkillDetailed("blocked-install", "blocked", "zip", zipPath, "user", "", "codex")
	if result.OK || !strings.Contains(result.FailureReason, "compensation queue unavailable") {
		t.Fatalf("queue-blocked InstallSkill result = %#v", result)
	}
	metadataPath := filepath.Join(app.GetSkillsDir("codex"), "metadata.json")
	if _, err := os.Stat(metadataPath); !os.IsNotExist(err) {
		t.Fatalf("queue-blocked install mutated metadata: %v", err)
	}
}

func TestAddSkillBlocksBeforeMutationWhenEvolutionQueueUnreadable(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(filepath.Join(tempHome, ".maclaw"))
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	queuePath := skill.DefaultEvolutionCompensationPath()
	if err := os.MkdirAll(filepath.Dir(queuePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(queuePath, []byte("{malformed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := &App{testHomeDir: tempHome}
	app.toolRouter = NewToolRouter(nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	result := app.AddSkillDetailed("blocked-add", "blocked", "address", "publisher/blocked-add", "claude")
	if result.OK || !strings.Contains(result.FailureReason, "compensation queue unavailable") {
		t.Fatalf("queue-blocked AddSkill result = %#v", result)
	}
	metadataPath := filepath.Join(app.GetSkillsDir("claude"), "metadata.json")
	if _, err := os.Stat(metadataPath); !os.IsNotExist(err) {
		t.Fatalf("queue-blocked add mutated metadata: %v", err)
	}
}

func TestInstallSkillDoesNotRewriteExistingSkillScanCache(t *testing.T) {
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

	// The user-wide Skills root is intentionally shared. Its existing cache is
	// unrelated to the package being installed and must remain byte-for-byte
	// intact after the new package receives its own scan evidence.
	existingDir := filepath.Join(tempHome, ".codex", "skills", "already-installed")
	if err := os.MkdirAll(existingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(existingDir, "skill.md"), []byte("# existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	existingCachePath := skillScanCachePath(existingDir, "already-installed")
	existingCache := []byte(`{"skill_name":"already-installed","evidence":"do-not-rewrite"}`)
	if err := os.WriteFile(existingCachePath, existingCache, 0o600); err != nil {
		t.Fatal(err)
	}

	zipPath := filepath.Join(t.TempDir(), "new-skill.zip")
	createSkillZip(t, zipPath, map[string]string{
		"new-skill/skill.md":   "# new-skill\n",
		"new-skill/skill.yaml": "name: new-skill\ndescription: new\nsteps: []\n",
	})
	if err := app.InstallSkill("new-skill", "new", "zip", zipPath, "user", "", "codex"); err != nil {
		t.Fatalf("InstallSkill() error = %v", err)
	}

	gotExistingCache, err := os.ReadFile(existingCachePath)
	if err != nil {
		t.Fatalf("read existing scan cache: %v", err)
	}
	if string(gotExistingCache) != string(existingCache) {
		t.Fatalf("existing Skill scan cache was rewritten: got %q want %q", gotExistingCache, existingCache)
	}
	if _, err := os.Stat(skillScanCachePath(filepath.Join(tempHome, ".codex", "skills", "new-skill"), "new-skill")); err != nil {
		t.Fatalf("new Skill scan cache missing: %v", err)
	}
}

func TestInstallSkillRejectsUnknownLocationBeforeAnyMutation(t *testing.T) {
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
	zipPath := filepath.Join(t.TempDir(), "invalid-location.zip")
	createSkillZip(t, zipPath, map[string]string{
		"invalid-location/skill.md":   "# invalid-location\n",
		"invalid-location/skill.yaml": "name: invalid-location\ndescription: invalid\nsteps: []\n",
	})

	err := app.InstallSkill("invalid-location", "invalid", "zip", zipPath, "workspace", "", "codex")
	if err == nil || !strings.Contains(err.Error(), "location") {
		t.Fatalf("InstallSkill() error = %v, want invalid location", err)
	}
	metadataPath := filepath.Join(app.GetSkillsDir("codex"), "metadata.json")
	if _, statErr := os.Stat(metadataPath); !os.IsNotExist(statErr) {
		t.Fatalf("unknown location wrote metadata: %v", statErr)
	}
	packagePath := filepath.Join(app.GetSkillsDir("codex"), filepath.Base(zipPath))
	if _, statErr := os.Stat(packagePath); !os.IsNotExist(statErr) {
		t.Fatalf("unknown location published package: %v", statErr)
	}
	if summaries, readErr := skill.ListEvolutionCompensationSummaries(); readErr != nil {
		t.Fatal(readErr)
	} else if len(summaries) != 0 {
		t.Fatalf("unknown location left compensation records: %#v", summaries)
	}
}

func TestSkillEvolutionCompensationManualEndpointsRequireConfirmation(t *testing.T) {
	var app App
	retry := app.RetrySkillEvolutionCompensation("req", "skill", "action", false)
	if retry["ok"] == true || retry["fail_closed"] != true {
		t.Fatalf("retry without confirmation = %#v", retry)
	}
	clear := app.ClearSkillEvolutionCompensation("req", "skill", "action", false)
	if clear["ok"] == true || clear["fail_closed"] != true {
		t.Fatalf("clear without confirmation = %#v", clear)
	}
}

func TestSnapshotSkillZipForInstallPreservesSourceBasename(t *testing.T) {
	sourceDir := t.TempDir()
	source := filepath.Join(sourceDir, "stable-package.zip")
	if err := os.WriteFile(source, []byte("zip-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, cleanup, err := snapshotSkillZipForInstall(source)
	if err != nil {
		t.Fatalf("snapshotSkillZipForInstall() error = %v", err)
	}
	defer cleanup()
	if filepath.Base(snapshot) != filepath.Base(source) {
		t.Fatalf("snapshot basename = %q, want %q", filepath.Base(snapshot), filepath.Base(source))
	}
	data, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "zip-bytes" {
		t.Fatalf("snapshot contents = %q, want original bytes", data)
	}
	cleanup()
	if _, err := os.Stat(snapshot); !os.IsNotExist(err) {
		t.Fatalf("snapshot remains after cleanup: %v", err)
	}
}

func TestLegacySkillInstallCreatedPathsTracksRootFilesInEmptyDestination(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "root-files.zip")
	createSkillZip(t, archivePath, map[string]string{
		"skill.yaml": "name: root-files\ndescription: root\nsteps: []\n",
		"README.md":  "# root-files\n",
	})
	destination := t.TempDir() // Existing but empty destination is safe to merge.
	if err := ensureSkillZipDoesNotOverwriteExisting(archivePath, destination); err != nil {
		t.Fatalf("ensureSkillZipDoesNotOverwriteExisting() error = %v", err)
	}
	paths, err := legacySkillInstallCreatedPaths(archivePath, destination)
	if err != nil {
		t.Fatalf("legacySkillInstallCreatedPaths() error = %v", err)
	}
	seen := map[string]bool{}
	for _, path := range paths {
		seen[filepath.Clean(path)] = true
	}
	for _, file := range []string{"skill.yaml", "README.md"} {
		if !seen[filepath.Clean(filepath.Join(destination, file))] {
			t.Fatalf("created paths = %#v, missing root file %q", paths, file)
		}
	}
}

func TestInstallSkillRootFilesInEmptyDestinationRollbackRemovesOnlyCreatedFiles(t *testing.T) {
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
	zipPath := filepath.Join(t.TempDir(), "root-rollback.zip")
	createSkillZip(t, zipPath, map[string]string{
		"skill.yaml": "name: root-rollback\ndescription: root rollback\nsteps: []\n",
		"README.md":  "# root-rollback\n",
	})
	projectPath := t.TempDir()
	destination := filepath.Join(projectPath, getToolConfigDirName("codex"), "skills")
	if err := os.MkdirAll(destination, 0o755); err != nil {
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
	if err := app.InstallSkill("root-rollback", "root rollback", "zip", zipPath, "project", projectPath, "codex"); err == nil {
		t.Fatal("InstallSkill() error = nil, want checked-index failure")
	}
	for _, file := range []string{"skill.yaml", "README.md"} {
		if _, statErr := os.Stat(filepath.Join(destination, file)); !os.IsNotExist(statErr) {
			t.Fatalf("created root file %q survived rollback: %v", file, statErr)
		}
	}
	if _, statErr := os.Stat(destination); statErr != nil {
		t.Fatalf("pre-existing empty destination was removed: %v", statErr)
	}
	if summaries, err := skill.ListEvolutionCompensationSummaries(); err != nil {
		t.Fatal(err)
	} else if len(summaries) != 0 {
		t.Fatalf("compensation queue after root-file rollback = %#v, want empty", summaries)
	}
}

func TestAddSkillLockedWithRecordPreservesOuterCleanupTargets(t *testing.T) {
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
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	packagePath := filepath.Join(skillsDir, "outer-cleanup.zip")
	if err := os.WriteFile(packagePath, []byte("old-package"), 0o644); err != nil {
		t.Fatal(err)
	}
	metadata, err := json.Marshal([]corelib.Skill{{Name: "outer-cleanup", Description: "old", Type: "zip", Value: filepath.Base(packagePath)}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "metadata.json"), metadata, 0o644); err != nil {
		t.Fatal(err)
	}
	newSource := filepath.Join(t.TempDir(), filepath.Base(packagePath))
	createSkillZip(t, newSource, map[string]string{
		"outer-cleanup/skill.md":   "# outer-cleanup\n",
		"outer-cleanup/skill.yaml": "name: outer-cleanup\ndescription: new\nsteps: []\n",
	})
	shared := skill.NewEvolutionCompensationRecord("outer-cleanup-request", "outer-cleanup", "legacy_gui_skill_install", "", nil, false, nil, "test")
	zipSnapshot := filepath.Join(t.TempDir(), "zip-snapshot.zip")
	shared.SetPostCommitCleanupPaths([]string{zipSnapshot})
	if err := app.addSkillLockedWithRecord("outer-cleanup", "new", "zip", newSource, "codex", &shared); err != nil {
		t.Fatalf("addSkillLockedWithRecord() error = %v", err)
	}
	if len(shared.PostCommitCleanupPaths) != 2 || shared.PostCommitCleanupPaths[0] != zipSnapshot || shared.PostCommitCleanupPaths[1] != packagePath+".prev" {
		t.Fatalf("outer cleanup targets = %#v, want input snapshot plus package backup", shared.PostCommitCleanupPaths)
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

// TestInstallSkillPendingRollbackRecoversAfterRestart verifies the crash
// boundary that cannot be proven by an in-process defer: both the forward
// checked-index publication and the first rollback index rebuild fail, so the
// durable outer install record must remain pending and a fresh App instance
// must recover it before accepting another mutation.
func TestInstallSkillPendingRollbackRecoversAfterRestart(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(filepath.Join(tempHome, ".maclaw"))
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	app := &App{testHomeDir: tempHome}
	app.toolRouter = NewToolRouter(nil)
	zipPath := filepath.Join(t.TempDir(), "legacy-restart.zip")
	createSkillZip(t, zipPath, map[string]string{
		"legacy-restart/skill.md":   "# legacy-restart\n",
		"legacy-restart/skill.yaml": "name: legacy-restart\ndescription: restart\nsteps: []\n",
	})
	projectPath := t.TempDir()
	refreshCalls := 0
	app.toolRouter.refreshSkillIndexOverride = func() error {
		refreshCalls++
		// Call 1 is the forward publication; call 2 is the rollback rebuild.
		if refreshCalls <= 2 {
			return os.ErrPermission
		}
		return nil
	}
	err := app.InstallSkill("legacy-restart", "restart", "zip", zipPath, "project", projectPath, "codex")
	if err == nil {
		t.Fatal("InstallSkill() error = nil, want pending rollback")
	}
	if refreshCalls < 2 {
		t.Fatalf("checked index refresh calls = %d, want forward and rollback failures", refreshCalls)
	}
	summaries, err := skill.ListEvolutionCompensationSummaries()
	if err != nil {
		t.Fatalf("read pending compensation: %v", err)
	}
	if len(summaries) != 1 || summaries[0].Skill != "legacy-restart" {
		t.Fatalf("pending compensation = %#v, want one legacy-restart record", summaries)
	}

	// Simulate a process restart with a fresh App/router. Recovery must use the
	// durable record rather than process-local state and must rebuild the index
	// before removing the record.
	app.shutdown(context.Background())
	restarted := &App{testHomeDir: tempHome}
	restarted.toolRouter = NewToolRouter(nil)
	recovered, pending, err := skill.RecoverPendingEvolutionCompensationsForActionPrefixAndSkill(
		"legacy_gui_skill_install", "legacy-restart", nil,
		func() error { return restarted.refreshSkillIndexesAfterMutationChecked("legacy-restart") },
	)
	if err != nil || recovered != 1 || pending != 0 {
		t.Fatalf("restart recovery = recovered %d pending %d err %v, want 1/0/nil", recovered, pending, err)
	}
	projectSkillDir := filepath.Join(projectPath, getToolConfigDirName("codex"), "skills", "legacy-restart")
	if _, statErr := os.Stat(projectSkillDir); !os.IsNotExist(statErr) {
		t.Fatalf("project skill directory survived restart recovery: %v", statErr)
	}
	metadataPath := filepath.Join(restarted.GetSkillsDir("codex"), "metadata.json")
	if _, statErr := os.Stat(metadataPath); !os.IsNotExist(statErr) {
		t.Fatalf("metadata registry survived restart recovery: %v", statErr)
	}
	if summaries, readErr := skill.ListEvolutionCompensationSummaries(); readErr != nil {
		t.Fatalf("read compensation after restart recovery: %v", readErr)
	} else if len(summaries) != 0 {
		t.Fatalf("compensation queue after restart recovery = %#v, want empty", summaries)
	}
	restarted.shutdown(context.Background())
}

func TestInstallSkillPendingRollbackRestoresPluginSettingsAfterRestart(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(filepath.Join(tempHome, ".maclaw"))
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	app := &App{testHomeDir: tempHome}
	app.toolRouter = NewToolRouter(nil)
	settingsPath := filepath.Join(tempHome, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	originalSettings := []byte(`{"enabledPlugins":{"existing@market":true},"custom":{"keep":true}}`)
	if err := os.WriteFile(settingsPath, originalSettings, 0o644); err != nil {
		t.Fatal(err)
	}
	refreshCalls := 0
	app.toolRouter.refreshSkillIndexOverride = func() error {
		refreshCalls++
		if refreshCalls <= 2 {
			return os.ErrPermission
		}
		return nil
	}
	if err := app.InstallSkill("legacy-plugin-restart", "plugin", "address", "legacy-plugin-restart@market", "user", "", "claude"); err == nil {
		t.Fatal("InstallSkill() error = nil, want pending rollback")
	}
	app.shutdown(context.Background())

	// A fresh process must restore the exact settings pre-image from the durable
	// file snapshot before it removes the compensation row.
	restarted := &App{testHomeDir: tempHome}
	restarted.toolRouter = NewToolRouter(nil)
	if _, pending, err := skill.RecoverPendingEvolutionCompensationsForActionPrefixAndSkill(
		"legacy_gui_skill_install", "legacy-plugin-restart", nil,
		func() error { return restarted.refreshSkillIndexesAfterMutationChecked("legacy-plugin-restart") },
	); err != nil || pending != 0 {
		t.Fatalf("restart recovery pending=%d err=%v", pending, err)
	}
	got, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(originalSettings) {
		t.Fatalf("settings after restart recovery = %q, want original %q", got, originalSettings)
	}
	if summaries, err := skill.ListEvolutionCompensationSummaries(); err != nil {
		t.Fatal(err)
	} else if len(summaries) != 0 {
		t.Fatalf("compensation queue after settings recovery = %#v", summaries)
	}
	restarted.shutdown(context.Background())
}

// TestInstallSkillBlocksOnCorruptDurableRollback verifies that an
// unreadable restore payload is retained as a durable admission blocker. A
// subsequent install must not proceed merely because the failed rollback was
// discovered during the current process; repeated retries eventually become
// needs_review instead of dropping the queue row.
func TestInstallSkillBlocksOnCorruptDurableRollback(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(filepath.Join(tempHome, ".maclaw"))
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	t.Cleanup(func() { _ = os.Remove(skill.DefaultEvolutionCompensationPath()) })

	createdDir := filepath.Join(tempHome, "created-before-recovery")
	if err := os.MkdirAll(createdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yamlPath := filepath.Join(tempHome, "rollback", "skill.yaml")
	record := skill.NewEvolutionCompensationRecord(
		"corrupt-gui-rollback", "corrupt-gui-rollback", "legacy_gui_skill_install",
		yamlPath, nil, true, nil, "test",
	)
	record.YAMLBackup = "%%%not-base64%%%"
	record.SetCreatedDirectories([]string{createdDir})
	if err := skill.PersistEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}

	app := &App{testHomeDir: tempHome}
	app.toolRouter = NewToolRouter(nil)
	defer app.shutdown(context.Background())
	for attempt := 1; attempt <= 3; attempt++ {
		err := app.InstallSkill("corrupt-gui-rollback", "ignored", "address", "ignored@market", "user", "", "claude")
		if err == nil || !strings.Contains(err.Error(), "compensation") {
			t.Fatalf("InstallSkill() attempt %d error = %v, want compensation blocker", attempt, err)
		}
		if _, statErr := os.Stat(createdDir); statErr != nil {
			t.Fatalf("durable rollback failure removed created directory on attempt %d: %v", attempt, statErr)
		}
	}
	summaries, err := skill.ListEvolutionCompensationSummaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].Attempts != 3 || summaries[0].Status != "needs_review" {
		t.Fatalf("durable rollback summary = %#v, want one needs_review record at attempt 3", summaries)
	}
	// needs_review is terminal for automatic recovery; a fourth mutation is
	// blocked without changing the record or touching any install target.
	if err := app.InstallSkill("corrupt-gui-rollback", "ignored", "address", "ignored@market", "user", "", "claude"); err == nil || !strings.Contains(err.Error(), "pending compensation") {
		t.Fatalf("InstallSkill() after needs_review error = %v, want pending compensation blocker", err)
	}
	summaries, err = skill.ListEvolutionCompensationSummaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].Attempts != 3 {
		t.Fatalf("needs_review record changed after blocked retry = %#v", summaries)
	}
}

// TestAddSkillRunsLegacyRecoveryBeforeAdmission keeps the bounded retry
// accounting reachable for the metadata-only adapter. Admission must not
// short-circuit a corrupt legacy rollback before recovery has recorded the
// failed attempt and escalated it to needs_review.
func TestAddSkillRunsLegacyRecoveryBeforeAdmission(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(filepath.Join(tempHome, ".maclaw"))
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	t.Cleanup(func() { _ = os.Remove(skill.DefaultEvolutionCompensationPath()) })

	createdDir := filepath.Join(tempHome, "created-before-recovery")
	if err := os.MkdirAll(createdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	record := skill.NewEvolutionCompensationRecord(
		"corrupt-gui-add", "corrupt-gui-add", "legacy_gui_skill_add",
		filepath.Join(tempHome, "rollback", "skill.yaml"), nil, true, nil, "test",
	)
	record.YAMLBackup = "%%%not-base64%%%"
	record.SetCreatedDirectories([]string{createdDir})
	if err := skill.PersistEvolutionCompensation(record); err != nil {
		t.Fatal(err)
	}

	app := &App{testHomeDir: tempHome}
	app.toolRouter = NewToolRouter(nil)
	defer app.shutdown(context.Background())
	for attempt := 1; attempt <= 3; attempt++ {
		err := app.AddSkill("corrupt-gui-add", "ignored", "address", "ignored@market", "claude")
		if err == nil || !strings.Contains(err.Error(), "compensation") {
			t.Fatalf("AddSkill() attempt %d error = %v, want compensation blocker", attempt, err)
		}
		if _, statErr := os.Stat(createdDir); statErr != nil {
			t.Fatalf("durable rollback failure removed created directory on attempt %d: %v", attempt, statErr)
		}
	}
	summaries, err := skill.ListEvolutionCompensationSummaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].Attempts != 3 || summaries[0].Status != "needs_review" {
		t.Fatalf("durable rollback summary = %#v, want one needs_review record at attempt 3", summaries)
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

// TestDeleteSkillDetailedReportsCommittedCleanupPending verifies that a
// cleanup failure after the strict delete audit never rolls back the already
// committed metadata decision. The durable record remains pending so startup
// recovery (or an operator) can retry cleanup safely.
func TestDeleteSkillDetailedReportsCommittedCleanupPending(t *testing.T) {
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
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	packagePath := filepath.Join(skillsDir, "delete-cleanup-pending.zip")
	if err := os.WriteFile(packagePath, []byte("zip-placeholder"), 0o644); err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(skillsDir, "metadata.json")
	data, err := json.MarshalIndent([]corelib.Skill{{Name: "delete-cleanup-pending", Description: "keep", Type: "zip", Value: filepath.Base(packagePath)}}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadataPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	app.legacySkillClearCompensation = func(string, string, string) error {
		return errors.New("injected delete cleanup failure")
	}
	result := app.DeleteSkillDetailed("delete-cleanup-pending", "codex")
	if result.State != "committed" || result.CleanupStatus != "pending" || !result.RollbackComplete {
		t.Fatalf("DeleteSkillDetailed() = %#v, want committed/pending", result)
	}
	if _, statErr := os.Stat(packagePath); !os.IsNotExist(statErr) {
		t.Fatalf("deleted package still present after committed delete: %v", statErr)
	}
	summaries, err := skill.ListEvolutionCompensationSummaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].TransactionState != "committed" || summaries[0].CleanupStatus != "pending" {
		t.Fatalf("compensation summaries = %#v, want committed/pending", summaries)
	}
}

func TestAddSkillDetailedReportsCommittedCleanupPending(t *testing.T) {
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
	zipPath := filepath.Join(t.TempDir(), "legacy-add-cleanup-pending.zip")
	createSkillZip(t, zipPath, map[string]string{
		"legacy-add-cleanup-pending/skill.md":   "# legacy-add-cleanup-pending\n",
		"legacy-add-cleanup-pending/skill.yaml": "name: legacy-add-cleanup-pending\ndescription: cleanup\nsteps: []\n",
	})
	app.legacySkillClearCompensation = func(string, string, string) error {
		return errors.New("injected add cleanup failure")
	}
	result := app.AddSkillDetailed("legacy-add-cleanup-pending", "cleanup", "zip", zipPath, "codex")
	if result.OK || result.State != "committed" || result.CleanupStatus != "pending" {
		t.Fatalf("AddSkillDetailed() = %#v, want committed/pending failure", result)
	}
	if result.RequestID == "" || result.FailureReason == "" || !result.RollbackComplete {
		t.Fatalf("AddSkillDetailed() missing committed cleanup metadata: %#v", result)
	}
	packagePath := filepath.Join(app.GetSkillsDir("codex"), filepath.Base(zipPath))
	if _, err := os.Stat(packagePath); err != nil {
		t.Fatalf("committed package missing after cleanup failure: %v", err)
	}
	summaries, err := skill.ListEvolutionCompensationSummaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].TransactionState != "committed" || summaries[0].CleanupStatus != "pending" {
		t.Fatalf("compensation summaries = %#v, want committed/pending", summaries)
	}
	if err := skill.ClearEvolutionCompensation(result.RequestID, "legacy-add-cleanup-pending", "legacy_gui_skill_add"); err != nil {
		t.Fatalf("clear test compensation: %v", err)
	}
}

func TestLegacyDetailedMutationReportsNoChangeWithoutDurableSideEffects(t *testing.T) {
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
	if err := app.AddSkill("legacy-noop", "same", "address", "local", "claude"); err != nil {
		t.Fatalf("initial AddSkill() error = %v", err)
	}
	added := app.AddSkillDetailed("legacy-noop", "same", "address", "local", "claude")
	if !added.OK || added.State != "skipped" || added.CleanupStatus != "clear" || added.FailureReason != "no_change" {
		t.Fatalf("AddSkillDetailed() no-op = %#v, want skipped/clear", added)
	}
	deleted := app.DeleteSkillDetailed("missing-legacy-noop", "claude")
	if !deleted.OK || deleted.State != "skipped" || deleted.CleanupStatus != "clear" || deleted.FailureReason != "no_change" {
		t.Fatalf("DeleteSkillDetailed() no-op = %#v, want skipped/clear", deleted)
	}
	zipPath := filepath.Join(t.TempDir(), "legacy-zip-noop.zip")
	createSkillZip(t, zipPath, map[string]string{
		"legacy-zip-noop/skill.md":   "# legacy-zip-noop\n",
		"legacy-zip-noop/skill.yaml": "name: legacy-zip-noop\ndescription: zip\nsteps: []\n",
	})
	if err := app.AddSkill("legacy-zip-noop", "zip", "zip", zipPath, "codex"); err != nil {
		t.Fatalf("initial ZIP AddSkill() error = %v", err)
	}
	zipNoop := app.AddSkillDetailed("legacy-zip-noop", "zip", "zip", zipPath, "codex")
	if !zipNoop.OK || zipNoop.State != "skipped" || zipNoop.CleanupStatus != "clear" || zipNoop.FailureReason != "no_change" {
		t.Fatalf("AddSkillDetailed() ZIP no-op = %#v, want skipped/clear", zipNoop)
	}
	if summaries, err := skill.ListEvolutionCompensationSummaries(); err != nil {
		t.Fatalf("read compensation queue: %v", err)
	} else if len(summaries) != 0 {
		t.Fatalf("no-op mutations left compensation records: %#v", summaries)
	}
}

func TestInstallSkillDetailedReportsAlreadyCurrentPlugin(t *testing.T) {
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
	if err := app.AddSkill("plugin-current", "plugin", "address", "publisher/plugin", "claude"); err != nil {
		t.Fatalf("initial AddSkill() error = %v", err)
	}
	settingsPath := filepath.Join(tempHome, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		t.Fatal(err)
	}
	settings := []byte(`{"enabledPlugins":{"publisher/plugin":true}}`)
	if err := os.WriteFile(settingsPath, settings, 0644); err != nil {
		t.Fatal(err)
	}
	result := app.InstallSkillDetailed("plugin-current", "plugin", "address", "publisher/plugin", "user", "", "claude")
	if !result.OK || result.State != "skipped" || result.CleanupStatus != "clear" || result.FailureReason != "already_current" {
		t.Fatalf("InstallSkillDetailed() = %#v, want skipped/already_current", result)
	}
	got, err := os.ReadFile(settingsPath)
	if err != nil || string(got) != string(settings) {
		t.Fatalf("settings changed on no-op: err=%v data=%q", err, got)
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

func TestInstallSkillMarketplaceMutationRollsBackWithOuterInstall(t *testing.T) {
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
	settingsPath := filepath.Join(tempHome, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	originalSettings := []byte(`{"enabledPlugins":{}}`)
	if err := os.WriteFile(settingsPath, originalSettings, 0o644); err != nil {
		t.Fatal(err)
	}
	// Make only the final strict audit unavailable. The outer durable install
	// record must restore both marketplace registration and plugin enablement;
	// a nested marketplace transaction must not survive this failure.
	auditPath := skill.DefaultEvolutionAuditPath()
	if err := os.MkdirAll(auditPath, 0o755); err != nil {
		t.Fatal(err)
	}
	result := app.InstallSkillDetailed("marketplace-rollback", "plugin", "address", "publisher/marketplace-rollback", "user", "", "claude")
	if result.OK || result.State == "committed" {
		t.Fatalf("InstallSkillDetailed() = %#v, want rollback after final audit failure", result)
	}
	got, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(originalSettings) {
		t.Fatalf("settings after marketplace rollback = %q, want original %q", got, originalSettings)
	}
	metadataPath := filepath.Join(app.GetSkillsDir("claude"), "metadata.json")
	if _, statErr := os.Stat(metadataPath); !os.IsNotExist(statErr) {
		t.Fatalf("metadata registry survived marketplace rollback: %v", statErr)
	}
	if summaries, readErr := skill.ListEvolutionCompensationSummaries(); readErr != nil {
		t.Fatal(readErr)
	} else if len(summaries) != 0 {
		t.Fatalf("compensation queue after marketplace rollback = %#v, want empty", summaries)
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

func TestInstallDefaultMarketplaceWriteFailureRollsBackDurableState(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(filepath.Join(tempHome, ".maclaw"))
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	app := &App{testHomeDir: tempHome}
	app.toolRouter = NewToolRouter(nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	settingsPath := filepath.Join(tempHome, ".claude", "settings.json")
	app.legacySkillWriteFile = func(path string, data []byte) error {
		if filepath.Clean(path) == filepath.Clean(settingsPath) {
			return os.ErrPermission
		}
		return atomicWriteFile(path, data)
	}
	if err := app.InstallDefaultMarketplace(); err == nil {
		t.Fatal("InstallDefaultMarketplace() error = nil, want settings write failure")
	}
	if _, err := os.Stat(settingsPath); !os.IsNotExist(err) {
		t.Fatalf("settings file after failed marketplace write exists: %v", err)
	}
	if summaries, err := skill.ListEvolutionCompensationSummaries(); err != nil {
		t.Fatal(err)
	} else if len(summaries) != 0 {
		t.Fatalf("compensation queue after marketplace rollback = %#v, want empty", summaries)
	}
}

func TestInstallDefaultMarketplaceAuditFailureRollsBack(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(filepath.Join(tempHome, ".maclaw"))
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	app := &App{testHomeDir: tempHome}
	app.toolRouter = NewToolRouter(nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	auditPath := skill.DefaultEvolutionAuditPath()
	if err := os.MkdirAll(auditPath, 0o755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(tempHome, ".claude", "settings.json")
	if err := app.InstallDefaultMarketplace(); err == nil {
		t.Fatal("InstallDefaultMarketplace() error = nil, want final-audit failure")
	}
	if _, err := os.Stat(settingsPath); !os.IsNotExist(err) {
		t.Fatalf("settings file survived final-audit rollback: %v", err)
	}
	if summaries, err := skill.ListEvolutionCompensationSummaries(); err != nil {
		t.Fatal(err)
	} else if len(summaries) != 0 {
		t.Fatalf("compensation queue after marketplace audit rollback = %#v, want empty", summaries)
	}
}

func TestInstallDefaultMarketplaceSecondCallIsNoOp(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(filepath.Join(tempHome, ".maclaw"))
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	app := &App{testHomeDir: tempHome}
	app.toolRouter = NewToolRouter(nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	if err := app.InstallDefaultMarketplace(); err != nil {
		t.Fatalf("first InstallDefaultMarketplace() error = %v", err)
	}
	settingsPath := filepath.Join(tempHome, ".claude", "settings.json")
	first, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.InstallDefaultMarketplace(); err != nil {
		t.Fatalf("second InstallDefaultMarketplace() error = %v", err)
	}
	second, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("settings changed on no-op retry: first=%q second=%q", first, second)
	}
	if summaries, err := skill.ListEvolutionCompensationSummaries(); err != nil {
		t.Fatal(err)
	} else if len(summaries) != 0 {
		t.Fatalf("compensation queue after no-op retry = %#v, want empty", summaries)
	}
	events, err := skill.ListEvolutionAudit(skill.DefaultEvolutionAuditPath(), skill.EvolutionAuditMaxKeep)
	if err != nil {
		t.Fatal(err)
	}
	commits := 0
	for _, event := range events {
		if event.Kind == "legacy_marketplace_committed" && event.Skill == legacyMarketplaceSkill {
			commits++
		}
	}
	if commits != 1 {
		t.Fatalf("marketplace commit audit count = %d, want exactly one", commits)
	}
}

func TestInstallDefaultMarketplaceConflictingEntryFailsClosed(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(filepath.Join(tempHome, ".maclaw"))
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	app := &App{testHomeDir: tempHome}
	app.toolRouter = NewToolRouter(nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	settingsPath := filepath.Join(tempHome, ".claude", "settings.json")
	original := []byte(`{"extraKnownMarketplaces":{"anthropic-agent-skills":{"source":{"source":"gitlab","repo":"other/skills"}}}}`)
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.InstallDefaultMarketplace(); err == nil {
		t.Fatal("InstallDefaultMarketplace() error = nil, want conflicting source rejection")
	}
	got, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("conflicting settings changed: got=%q want=%q", got, original)
	}
	if summaries, err := skill.ListEvolutionCompensationSummaries(); err != nil {
		t.Fatal(err)
	} else if len(summaries) != 0 {
		t.Fatalf("compensation queue after conflicting source = %#v, want empty", summaries)
	}
}

func TestInstallDefaultMarketplaceCleanupFailureKeepsCommittedPending(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(filepath.Join(tempHome, ".maclaw"))
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	app := &App{testHomeDir: tempHome}
	app.toolRouter = NewToolRouter(nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	app.legacySkillClearCompensation = func(string, string, string) error { return os.ErrPermission }
	err := app.InstallDefaultMarketplace()
	if err == nil {
		t.Fatal("InstallDefaultMarketplace() error = nil, want cleanup failure")
	}
	var committedErr *legacySkillCommitError
	if !errors.As(err, &committedErr) || !committedErr.committed {
		t.Fatalf("InstallDefaultMarketplace() error = %v, want committed cleanup error", err)
	}
	if summaries, readErr := skill.ListEvolutionCompensationSummaries(); readErr != nil {
		t.Fatal(readErr)
	} else if len(summaries) != 1 || summaries[0].TransactionState != "committed" || summaries[0].CleanupStatus != "pending" {
		t.Fatalf("marketplace cleanup-pending summaries = %#v, want committed/pending", summaries)
	}
}

func TestInstallSkillDetailedReportsCommittedCleanupPending(t *testing.T) {
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
	zipPath := filepath.Join(t.TempDir(), "legacy-cleanup-pending.zip")
	createSkillZip(t, zipPath, map[string]string{
		"legacy-cleanup-pending/skill.md":   "# legacy-cleanup-pending\n",
		"legacy-cleanup-pending/skill.yaml": "name: legacy-cleanup-pending\ndescription: cleanup\nsteps: []\n",
	})
	app.legacySkillClearCompensation = func(string, string, string) error {
		return os.ErrPermission
	}
	result := app.InstallSkillDetailed("legacy-cleanup-pending", "cleanup", "zip", zipPath, "project", t.TempDir(), "codex")
	if result.OK || result.State != "committed" || result.CleanupStatus != "pending" {
		t.Fatalf("InstallSkillDetailed() = %#v, want committed/pending failure", result)
	}
	if result.RequestID == "" || result.FailureReason == "" {
		t.Fatalf("cleanup-pending result missing request/error: %#v", result)
	}
	summaries, err := skill.ListEvolutionCompensationSummaries()
	if err != nil {
		t.Fatalf("read cleanup-pending queue: %v", err)
	}
	if len(summaries) != 1 || summaries[0].TransactionState != "committed" || summaries[0].CleanupStatus != "pending" {
		t.Fatalf("cleanup-pending summaries = %#v, want committed/pending", summaries)
	}
	// The business result is committed; cleanup failure must not remove the
	// installed metadata or project directory and must not be reported as a
	// rollback. Remove only the test's durable queue row after asserting it.
	if err := skill.ClearEvolutionCompensation(result.RequestID, "legacy-cleanup-pending", "legacy_gui_skill_install"); err != nil {
		t.Fatalf("clear test compensation: %v", err)
	}
}
