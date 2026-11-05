package guiapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

func TestSkillInitialExecutionFailureNoteSkipsRetryOnTruncatedModel(t *testing.T) {
	note := skillInitialExecutionFailureNote("Interview", fmt.Errorf("skill execution stopped at step 1: parse response: unexpected end of JSON input"))
	if !strings.Contains(note, "marked as needs_setup") || !strings.Contains(note, "truncated") {
		t.Fatalf("note = %q", note)
	}
	if strings.Contains(note, "manage_skill") || strings.Contains(note, "search_and_install_skill") {
		t.Fatalf("truncated model response still names a skill tool: %q", note)
	}
	unpersisted := skillInitialExecutionUnpersistedNote("Interview", fmt.Errorf("skill execution stopped at step 1: parse response: unexpected end of JSON input"), fmt.Errorf("durable App status transaction unavailable"))
	if strings.Contains(unpersisted, "manage_skill") || strings.Contains(unpersisted, "before retrying") || strings.Contains(unpersisted, "marked as needs_setup") {
		t.Fatalf("unpersisted truncated note = %q", unpersisted)
	}
	retry := skillInitialExecutionFailureNote("Interview", fmt.Errorf("exit status 7"))
	if !strings.Contains(retry, `manage_skill(action="run", name="Interview")`) {
		t.Fatalf("ordinary failure note = %q", retry)
	}
	download := skillInitialExecutionFailureNote("Interview", fmt.Errorf("download skill failed: unexpected EOF"))
	if !strings.Contains(download, `manage_skill(action="run", name="Interview")`) {
		t.Fatalf("command EOF was treated as a truncated model body: %q", download)
	}
}

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

func TestExecuteInstalledSkillSkipsUnrunnablePackage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("VOLC_IMAGE_KEY", "")
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(t.TempDir())
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })

	app := &App{testHomeDir: home}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	h := &IMMessageHandler{app: app}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".maclaw_scan_status.db"), []byte("scan"), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := corelib.NLSkillEntry{
		Name: "empty-picture", Source: "auto_clawhub", Status: "active", SkillDir: dir,
		Steps: []corelib.NLSkillStep{{Action: "craft_tool", Params: map[string]interface{}{"instructions": "draw a cat"}}},
	}
	if err := app.skillExecutor.Register(empty); err != nil {
		t.Fatal(err)
	}
	skipped := h.executeInstalledSkill(context.Background(), &empty, empty.Name, "desktop", "user", "user", func(string) {
		t.Fatal("empty package announced execution")
	})
	if skipped.Success || strings.Contains(skipped.Text, "manage_skill") || strings.Contains(skipped.Text, "search_and_install_skill") || !strings.Contains(skipped.Text, "package has no runnable program") || !strings.Contains(skipped.Text, "needs_setup") || !strings.Contains(skipped.Text, "craft_tool") || !strings.HasPrefix(skipped.Text, "failed:") {
		t.Fatalf("empty package = %+v", skipped)
	}
	if agentruntime.RegisteredToolTextOutcome(skipped.Text) != agentruntime.RegisteredToolOutcomeFailed {
		t.Fatalf("unrunnable note is not a tool failure: %q", skipped.Text)
	}
	unpersisted := skillInitialExecutionUnpersistedUnrunnableNote(empty.Name, "package has no runnable program", fmt.Errorf("durable App status transaction unavailable"))
	if !strings.HasPrefix(unpersisted, "failed:") || !strings.Contains(unpersisted, "craft_tool") || strings.Contains(unpersisted, "manage_skill") || strings.Contains(unpersisted, "search_and_install_skill") {
		t.Fatalf("unpersisted note = %q", unpersisted)
	}

	missing := corelib.NLSkillEntry{
		Name: "missing-key", Source: "manual", Status: "active",
		RequiredEnv: []string{"VOLC_IMAGE_KEY"},
		Steps:       []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "cmd /c exit 7"}}},
	}
	if err := app.skillExecutor.Register(missing); err != nil {
		t.Fatal(err)
	}
	refused := h.executeInstalledSkill(context.Background(), &missing, missing.Name, "desktop", "user", "user", nil)
	if refused.Success || strings.Contains(refused.Text, "manage_skill") || strings.Contains(refused.Text, "search_and_install_skill") || strings.Contains(refused.Text, "craft_tool") || strings.Contains(refused.Text, "exit status") || !strings.Contains(refused.Text, "required credentials are empty") || !strings.HasPrefix(refused.Text, "failed:") {
		t.Fatalf("missing credential = %+v", refused)
	}
	entries := app.skillExecutor.loadSkills()
	status := map[string]string{}
	for _, entry := range entries {
		status[entry.Name] = entry.Status
	}
	if status["empty-picture"] != "needs_setup" || status["missing-key"] != "needs_setup" {
		t.Fatalf("status = %#v", entries)
	}
}

func TestInstalledSkillAutoRunRefusal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".maclaw_scan_status.db"), []byte("scan"), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := &corelib.NLSkillEntry{
		Name: "pic", Source: "auto_clawhub", Status: "active", SkillDir: dir,
		Steps: []corelib.NLSkillStep{{Action: "craft_tool", Params: map[string]interface{}{"instructions": "draw"}}},
	}
	if got := installedSkillAutoRunRefusal(empty); got != "package has no runnable program" {
		t.Fatalf("empty package = %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "draw.py"), []byte("print(1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := installedSkillAutoRunRefusal(empty); got != "" {
		t.Fatalf("scripted package = %q", got)
	}
	t.Setenv("VOLC_IMAGE_KEY", "")
	cred := &corelib.NLSkillEntry{
		Name: "cred", Status: "active", RequiredEnv: []string{"VOLC_IMAGE_KEY"},
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}}},
	}
	if got := installedSkillAutoRunRefusal(cred); got != "required credentials are empty" {
		t.Fatalf("missing credential = %q", got)
	}
	t.Setenv("VOLC_IMAGE_KEY", "present")
	if got := installedSkillAutoRunRefusal(cred); got != "" {
		t.Fatalf("present credential = %q", got)
	}
	t.Setenv("OPENAI_API_KEY", "")
	proxy := &corelib.NLSkillEntry{
		Name: "proxy", Status: "active", RequiredEnv: []string{"OPENAI_API_KEY"},
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}}},
	}
	if got := installedSkillAutoRunRefusal(proxy); got != "" {
		t.Fatalf("proxy-provided OpenAI key = %q", got)
	}
	proxy.NoLLMAPI = true
	if got := installedSkillAutoRunRefusal(proxy); got != "required credentials are empty" {
		t.Fatalf("no_llm_api OpenAI key = %q", got)
	}
	command := &corelib.NLSkillEntry{
		Name: "py", Status: "active", RequiredEnv: []string{"python"},
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}}},
	}
	if got := installedSkillAutoRunRefusal(command); got != "" {
		t.Fatalf("command-shaped required env = %q", got)
	}
	t.Setenv("VOLC_IMAGE_KEY", "")
	stepEnv := &corelib.NLSkillEntry{
		Name: "step-env", Status: "active",
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{
			"command":      "echo hi",
			"required_env": []string{"VOLC_IMAGE_KEY"},
		}}},
	}
	if got := installedSkillAutoRunRefusal(stepEnv); got != "required credentials are empty" {
		t.Fatalf("step required env = %q", got)
	}
	if got := installedSkillAutoRunRefusal(&corelib.NLSkillEntry{Name: "none", Status: "active"}); got != "no runnable steps" {
		t.Fatalf("no steps = %q", got)
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
