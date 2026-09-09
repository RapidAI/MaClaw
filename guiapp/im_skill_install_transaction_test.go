package guiapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

func TestIMInstallOnlyConfigDefinitionUsesSharedCommitter(t *testing.T) {
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
	h := &IMMessageHandler{app: app}
	result := h.registerSkillWithoutExecution(context.Background(), &corelib.NLSkillEntry{
		Name: "im-config-only", Description: "config-only import", Source: "auto_github", Status: "active",
	}, "im-config-only", "auto_github", "desktop", "user", "user", func(string) {})
	if !result.Success {
		t.Fatalf("registerSkillWithoutExecution() = %+v", result)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.NLSkills) != 1 || cfg.NLSkills[0].Name != "im-config-only" {
		t.Fatalf("config-only install was not durably registered: %#v", cfg.NLSkills)
	}
	if summaries, err := cskill.ListEvolutionCompensationSummaries(); err != nil {
		t.Fatalf("ListEvolutionCompensationSummaries() error = %v", err)
	} else if len(summaries) != 0 {
		t.Fatalf("successful config-only install left compensation records: %#v", summaries)
	}
	auditPath := cskill.DefaultEvolutionAuditPath()
	audit, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read evolution audit: %v", err)
	}
	if !strings.Contains(string(audit), "\"kind\":\"definition_installed\"") {
		t.Fatalf("shared install audit missing definition_installed event: %s", audit)
	}
}

func TestIMInitialExecutionFailureUsesDurableStatusCommitter(t *testing.T) {
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
	command := "exit 7"
	if os.PathSeparator == '\\' {
		command = "cmd /c exit 7"
	}
	installed := corelib.NLSkillEntry{
		Name: "im-status-committer", Source: "manual", Status: "active",
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": command}}},
	}
	if err := app.skillExecutor.Register(installed); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	h := &IMMessageHandler{app: app}
	result := h.executeInstalledSkill(context.Background(), &installed, installed.Name, "desktop", "user", "user", nil)
	if !strings.Contains(result.Text, "marked as needs_setup") {
		t.Fatalf("executeInstalledSkill() = %+v", result)
	}
	entries := app.skillExecutor.loadSkills()
	if len(entries) != 1 || entries[0].Status != "needs_setup" {
		t.Fatalf("status after failed initial execution = %#v", entries)
	}
	audit, err := os.ReadFile(cskill.DefaultEvolutionAuditPath())
	if err != nil {
		t.Fatalf("read evolution audit: %v", err)
	}
	if !strings.Contains(string(audit), "status_changed") {
		t.Fatalf("durable status audit missing: %s", audit)
	}
	if summaries, err := cskill.ListEvolutionCompensationSummaries(); err != nil {
		t.Fatalf("ListEvolutionCompensationSummaries() error = %v", err)
	} else if len(summaries) != 0 {
		t.Fatalf("successful status transaction left compensation records: %#v", summaries)
	}
}

func TestIMPatchTextUsesSharedCommitter(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(t.TempDir())
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	skillDir := t.TempDir()
	definition := "id: test.patchable\nname: patchable\ndescription: before\nsource: learned\nstatus: active\nsteps:\n  - action: shell\n    params:\n      command: echo old\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(definition), 0644); err != nil {
		t.Fatalf("write skill definition: %v", err)
	}

	app := &App{testHomeDir: home}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	if err := app.skillExecutor.Register(corelib.NLSkillEntry{
		SkillID: "test.patchable", Name: "patchable", Description: "before", Status: "active",
		Source: "learned", SkillDir: skillDir,
		Steps: []corelib.NLSkillStep{{Action: "shell", Params: map[string]interface{}{"command": "echo old"}}},
	}); err != nil {
		t.Fatalf("register skill: %v", err)
	}

	h := &IMMessageHandler{app: app}
	result := h.toolPatchSkill(map[string]interface{}{
		"skill_name": "patchable", "find": "echo old", "replace": "echo new", "reason": "test patch",
	})
	if !strings.Contains(result, "patched successfully") {
		t.Fatalf("toolPatchSkill() = %q", result)
	}
	structuredResult := h.toolPatchSkill(map[string]interface{}{
		"skill_name": "patchable", "mode": "step", "step_index": 0,
		"field": "command", "value": "echo structured", "reason": "structured test patch",
	})
	if !strings.Contains(structuredResult, "updated to") {
		t.Fatalf("structured toolPatchSkill() = %q", structuredResult)
	}
	patched, err := os.ReadFile(filepath.Join(skillDir, "skill.yaml"))
	if err != nil || !strings.Contains(string(patched), "echo structured") {
		t.Fatalf("patched YAML = %q, err=%v", patched, err)
	}
	entries := app.skillExecutor.loadSkills()
	if len(entries) != 1 || entries[0].Name != "patchable" || entries[0].SkillDir != skillDir {
		t.Fatalf("config identity was not preserved by patch: %#v", entries)
	}
	loaded, err := loadImportedSkillEntry(skillDir)
	if err != nil || loaded == nil || len(loaded.Steps) != 1 || loaded.Steps[0].Params["command"] != "echo structured" {
		t.Fatalf("patched definition did not reload: entry=%#v err=%v", loaded, err)
	}
	patches, err := os.ReadFile(filepath.Join(skillDir, ".patches.json"))
	if err != nil || !strings.Contains(string(patches), "test patch") {
		t.Fatalf("patch history = %q, err=%v", patches, err)
	}
	if summaries, err := cskill.ListEvolutionCompensationSummaries(); err != nil {
		t.Fatalf("ListEvolutionCompensationSummaries() error = %v", err)
	} else if len(summaries) != 0 {
		t.Fatalf("successful patch left compensation records: %#v", summaries)
	}
	audit, err := os.ReadFile(cskill.DefaultEvolutionAuditPath())
	if err != nil {
		t.Fatalf("read evolution audit: %v", err)
	}
	if !strings.Contains(string(audit), "definition_patched") {
		t.Fatalf("patch audit missing definition_patched event: %s", audit)
	}
}

func TestIMPatchRollsBackWhenCheckedIndexRefreshFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(t.TempDir())
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	skillDir := t.TempDir()
	original := "id: test.patch-failure\nname: patch-failure\ndescription: before\nsource: learned\nstatus: active\nsteps:\n  - action: shell\n    params:\n      command: echo old\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	app := &App{testHomeDir: home}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.toolRouter = NewToolRouter(nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	if err := app.skillExecutor.Register(corelib.NLSkillEntry{
		SkillID: "test.patch-failure", Name: "patch-failure", Description: "before", Status: "active",
		Source: "learned", SkillDir: skillDir,
		Steps: []corelib.NLSkillStep{{Action: "shell", Params: map[string]interface{}{"command": "echo old"}}},
	}); err != nil {
		t.Fatal(err)
	}
	refreshCalls := 0
	app.toolRouter.refreshSkillIndexOverride = func() error {
		refreshCalls++
		if refreshCalls == 1 {
			return errors.New("injected index failure")
		}
		return nil
	}
	h := &IMMessageHandler{app: app}
	got := h.toolPatchSkill(map[string]interface{}{
		"skill_name": "patch-failure", "find": "echo old", "replace": "echo new", "reason": "failure test",
	})
	if !strings.Contains(got, "not committed") || !strings.Contains(got, "index_refresh_failed") {
		t.Fatalf("toolPatchSkill() = %q, want checked-index failure", got)
	}
	data, err := os.ReadFile(filepath.Join(skillDir, "skill.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Fatalf("YAML changed after failed patch\n got: %q\nwant: %q", data, original)
	}
	if _, err := os.Stat(filepath.Join(skillDir, ".patches.json")); !os.IsNotExist(err) {
		t.Fatalf("patch history should be absent after rollback, stat err=%v", err)
	}
	if summaries, err := cskill.ListEvolutionCompensationSummaries(); err != nil {
		t.Fatal(err)
	} else if len(summaries) != 0 {
		t.Fatalf("failed patch left compensation records: %#v", summaries)
	}
}
