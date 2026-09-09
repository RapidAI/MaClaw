package guiapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

func TestCreateNLSkillUsesSharedCommitterAndRollsBackIndexFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(t.TempDir())
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: home}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.toolRouter = NewToolRouter(nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	refreshCalls := 0
	app.toolRouter.refreshSkillIndexOverride = func() error {
		refreshCalls++
		if refreshCalls == 1 {
			return errors.New("injected index provider failure")
		}
		return nil
	}
	err := app.CreateNLSkill(corelib.NLSkillEntry{
		Name: "committer-create", Description: "test", Source: "manual", Status: "active",
		Triggers: []string{"test"}, Steps: []corelib.NLSkillStep{{Action: "noop", Params: map[string]interface{}{}}},
	})
	if err == nil || !strings.Contains(err.Error(), "not committed") {
		t.Fatalf("CreateNLSkill() error = %v, want commit failure", err)
	}
	if app.skillNameAlreadyRegistered("committer-create") {
		t.Fatal("failed create remained registered after index rollback")
	}
	if refreshCalls < 2 {
		t.Fatalf("refresh calls = %d, want forward failure and rollback refresh", refreshCalls)
	}
}

func TestDirectorySkillCommitWaitsForInstallMutex(t *testing.T) {
	app := &App{skillExecutor: &SkillExecutor{}}
	app.installMutex.Lock()
	done := make(chan error, 1)
	go func() {
		done <- app.commitStagedSkillInstall(
			context.Background(),
			&corelib.NLSkillEntry{Name: "serialized-directory-commit"},
			"", "test", nil, "req", "rev",
		)
	}()
	select {
	case err := <-done:
		t.Fatalf("directory commit completed while install mutex was held: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	app.installMutex.Unlock()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "empty name or staging directory") {
			t.Fatalf("directory commit error = %v, want validation after lock release", err)
		}
	case <-time.After(time.Second):
		t.Fatal("directory commit did not resume after install mutex was released")
	}
}

func TestConfigSkillCommitWaitsForInstallMutex(t *testing.T) {
	app := &App{skillExecutor: &SkillExecutor{}}
	app.installMutex.Lock()
	done := make(chan error, 1)
	go func() {
		done <- app.commitNLSkillDefinitionAfterAdmission(
			context.Background(), corelib.NLSkillEntry{}, "install",
			"skill:definition_installed", true, "test", nil,
		)
	}()
	select {
	case err := <-done:
		t.Fatalf("config commit completed while install mutex was held: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	app.installMutex.Unlock()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "skill name is required") {
			t.Fatalf("config commit error = %v, want validation after lock release", err)
		}
	case <-time.After(time.Second):
		t.Fatal("config commit did not resume after install mutex was released")
	}
}

func TestSetNLSkillStatusWaitsForInstallMutex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(t.TempDir())
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: home}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	if err := app.skillExecutor.Register(corelib.NLSkillEntry{
		Name: "serialized-status", Status: "needs_review", Source: "manual",
		Steps: []corelib.NLSkillStep{{Action: "noop", Params: map[string]interface{}{}}},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	app.installMutex.Lock()
	done := make(chan error, 1)
	go func() { done <- app.SetNLSkillStatus("serialized-status", "disabled") }()
	select {
	case err := <-done:
		t.Fatalf("status mutation completed while install mutex was held: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	app.installMutex.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SetNLSkillStatus() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("status mutation did not resume after install mutex was released")
	}
}

func TestIMPatchCommitWaitsForInstallMutex(t *testing.T) {
	app := setupNLSkillTransactionApp(t)
	h := &IMMessageHandler{app: app}
	skillDir := t.TempDir()
	defPath := filepath.Join(skillDir, "skill.yaml")
	valid := []byte("name: serialized-patch\nsteps: []\n")
	app.installMutex.Lock()
	done := make(chan error, 1)
	go func() {
		done <- h.commitIMSkillDefinitionPatch(
			context.Background(),
			&corelib.NLSkillEntry{Name: "serialized-patch", SkillDir: skillDir},
			defPath, "yaml", valid,
			valid, patchRecord{},
		)
	}()
	select {
	case err := <-done:
		t.Fatalf("IM patch commit completed while install mutex was held: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	app.installMutex.Unlock()
	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("IM patch commit unexpectedly succeeded after lock release")
		}
	case <-time.After(time.Second):
		t.Fatal("IM patch commit did not resume after install mutex was released")
	}
}

func TestInstalledSkillForInstallRejectsStableIdentityCollision(t *testing.T) {
	app := &App{}
	installed := []corelib.NLSkillEntry{{Name: "same-name", SkillID: "publisher.existing", HubSkillID: "hub-existing"}}
	app.nlSkillsSnap.Store(&installed)
	app.skillExecutor = &SkillExecutor{app: app}
	if got := app.installedSkillForInstall(&corelib.NLSkillEntry{
		Name: "same-name", SkillID: "publisher.other", HubSkillID: "hub-other",
	}); got != nil {
		t.Fatalf("installedSkillForInstall() matched conflicting stable identity: %#v", got)
	}
}

func TestUpdateNLSkillSharedCommitterPersistsTriggers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(t.TempDir())
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: home}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })

	if err := app.CreateNLSkill(corelib.NLSkillEntry{
		Name: "definition-update", Description: "old description", Status: "active", Source: "manual",
		Triggers: []string{"old trigger"}, Steps: []corelib.NLSkillStep{{Action: "noop", Params: map[string]interface{}{}}},
	}); err != nil {
		t.Fatalf("CreateNLSkill() error = %v", err)
	}
	if err := app.UpdateNLSkill(corelib.NLSkillEntry{
		Name: "definition-update", Description: "new description", Status: "active", Source: "manual",
		Triggers: []string{"new trigger", "second trigger"}, Steps: []corelib.NLSkillStep{{Action: "noop", Params: map[string]interface{}{"mode": "updated"}}},
	}); err != nil {
		t.Fatalf("UpdateNLSkill() error = %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.NLSkills) != 1 {
		t.Fatalf("updated config = %#v, want one skill", cfg.NLSkills)
	}
	got := cfg.NLSkills[0]
	if got.Description != "new description" || strings.Join(got.Triggers, ",") != "new trigger,second trigger" || got.Steps[0].Params["mode"] != "updated" {
		t.Fatalf("definition fields lost by shared update commit: %#v", got)
	}
}

func TestApplySkillMaintenanceActionReturnsSkippedForNoChange(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(t.TempDir())
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: home}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	if err := app.skillExecutor.Register(corelib.NLSkillEntry{
		Name: "maintenance-clean", Status: "active", Source: "manual",
		Steps: []corelib.NLSkillStep{{Action: "noop", Params: map[string]interface{}{}}},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	auditPath := skill.DefaultEvolutionAuditPath()
	auditBefore, auditReadErr := os.ReadFile(auditPath)
	if auditReadErr != nil && !os.IsNotExist(auditReadErr) {
		t.Fatalf("read audit before no-op: %v", auditReadErr)
	}
	out := app.ApplySkillMaintenanceAction("improve_contract", "maintenance-clean", "", true, false)
	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("expected no-op maintenance to be accepted: %#v", out)
	}
	if out["state"] != "skipped" || out["cleanup_status"] != "clear" || out["message"] != "no changes required" {
		t.Fatalf("unexpected no-op result: %#v", out)
	}
	auditAfter, auditReadErr := os.ReadFile(auditPath)
	if auditReadErr != nil && !os.IsNotExist(auditReadErr) {
		t.Fatalf("read audit after no-op: %v", auditReadErr)
	}
	if string(auditAfter) != string(auditBefore) {
		t.Fatalf("no-op maintenance wrote audit events: before=%q after=%q", auditBefore, auditAfter)
	}
}

func TestCleanupStaleNLSkillsDoesNotBypassApprovedMaintenanceCommit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(t.TempDir())
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	app := &App{testHomeDir: home}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	staleCreatedAt := time.Now().Add(-31 * 24 * time.Hour).UTC().Format(time.RFC3339)
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{
		Name: "stale-direct-cleanup", Source: "learned", Status: "active", CreatedAt: staleCreatedAt,
	}}}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if disabled := app.CleanupStaleNLSkills(); len(disabled) != 0 {
		t.Fatalf("legacy stale cleanup disabled = %#v, want no mutation", disabled)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.NLSkills) != 1 || cfg.NLSkills[0].Status != "active" {
		t.Fatalf("legacy stale cleanup changed config: %#v", cfg.NLSkills)
	}
	if summaries, err := skill.ListEvolutionCompensationSummaries(); err != nil {
		t.Fatalf("ListEvolutionCompensationSummaries() error = %v", err)
	} else if len(summaries) != 0 {
		t.Fatalf("legacy stale cleanup created compensation records: %#v", summaries)
	}
}
