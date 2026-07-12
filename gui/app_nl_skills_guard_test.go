package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

func TestSetNLSkillStatusApprovesReviewedSkillAndExposesReason(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	if err := app.skillExecutor.Register(corelib.NLSkillEntry{
		Name:       "reviewed-skill",
		Status:     "needs_review",
		LastError:  "auto-repair blocked by security scan: level=high summary=uses shell",
		Source:     "manual",
		CreatedAt:  "2026-01-01T00:00:00Z",
		Triggers:   []string{"reviewed"},
		Steps:      []corelib.NLSkillStep{{Action: "noop", Params: map[string]interface{}{}, OnError: "stop"}},
		UsageCount: 1,
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	defs := app.ListNLSkills()
	reviewed := findNLSkillDefinitionForTest(defs, "reviewed-skill")
	if reviewed == nil || reviewed.Status != "needs_review" || !strings.Contains(reviewed.ReviewReason, "security scan") {
		t.Fatalf("expected review reason in skill list: %#v", defs)
	}
	if err := app.SetNLSkillStatus("reviewed-skill", "active"); err != nil {
		t.Fatalf("SetNLSkillStatus() error = %v", err)
	}
	defs = app.ListNLSkills()
	reviewed = findNLSkillDefinitionForTest(defs, "reviewed-skill")
	if reviewed == nil || reviewed.Status != "active" {
		t.Fatalf("expected approved skill to be active: %#v", defs)
	}
	if reviewed.LastError == "" {
		t.Fatalf("expected last_error evidence to remain available after approval")
	}
}

func findNLSkillDefinitionForTest(defs []NLSkillDefinition, name string) *NLSkillDefinition {
	for i := range defs {
		if defs[i].Name == name {
			return &defs[i]
		}
	}
	return nil
}

func TestBatchTriggerSkillSelfRepairEmptyNames(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	res := app.BatchTriggerSkillSelfRepair(nil, true)
	if res["error"] == nil && res["count"] != 0 {
		t.Fatalf("expected empty batch to report no names, got %#v", res)
	}
	if res["count"] != 0 {
		t.Fatalf("count=%v", res["count"])
	}
}

func TestBatchTriggerSkillOptimizeEmptyAndMissing(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	empty := app.BatchTriggerSkillOptimize(nil, true)
	if empty["count"] != 0 {
		t.Fatalf("empty count=%v", empty["count"])
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	missing := app.BatchTriggerSkillOptimize([]string{"no-opt-skill"}, true)
	failed, _ := missing["failed"].([]string)
	if len(failed) < 1 {
		t.Fatalf("expected failure: %#v", missing)
	}
}

func TestBatchTriggerSkillSelfRepairMissingSkills(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	app := &App{testHomeDir: tempHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	res := app.BatchTriggerSkillSelfRepair([]string{"no-such-skill-a", "no-such-skill-b"}, true)
	failed, _ := res["failed"].([]string)
	if len(failed) < 1 {
		t.Fatalf("expected failures for missing skills: %#v", res)
	}
	if res["count"] != 0 {
		t.Fatalf("count should be 0, got %#v", res)
	}
}

func TestBatchSetNLSkillStatusMarksMultipleNeedsReview(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	corelib.SetMaclawBaseDir(tempHome)
	t.Cleanup(func() { corelib.SetMaclawBaseDir("") })

	app := &App{testHomeDir: tempHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	for _, name := range []string{"batch-a", "batch-b"} {
		if err := app.skillExecutor.Register(corelib.NLSkillEntry{
			Name:      name,
			Status:    "active",
			LastError: "[class: command_not_found] missing",
			Source:    "crafted",
			CreatedAt: "2026-01-01T00:00:00Z",
			Steps:     []corelib.NLSkillStep{{Action: "noop", Params: map[string]interface{}{}, OnError: "stop"}},
		}); err != nil {
			t.Fatalf("Register %s: %v", name, err)
		}
	}
	res := app.BatchSetNLSkillStatus([]string{"batch-a", "batch-b", "batch-a", ""}, "needs_review")
	if res["count"] != 2 {
		t.Fatalf("count=%v res=%#v", res["count"], res)
	}
	defs := app.ListNLSkills()
	for _, name := range []string{"batch-a", "batch-b"} {
		got := findNLSkillDefinitionForTest(defs, name)
		if got == nil || got.Status != "needs_review" {
			t.Fatalf("%s status=%#v", name, got)
		}
	}
	events, err := skill.ListEvolutionAudit(skill.DefaultEvolutionAuditPath(), 50)
	if err != nil {
		t.Fatal(err)
	}
	hits := 0
	for _, ev := range events {
		if ev.Kind == "mark_needs_review" && (ev.Skill == "batch-a" || ev.Skill == "batch-b") {
			hits++
		}
	}
	if hits < 2 {
		t.Fatalf("expected >=2 mark_needs_review audits, got %d: %#v", hits, events)
	}
}

func TestSetNLSkillStatusNeedsReviewWritesEvolutionAudit(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	corelib.SetMaclawBaseDir(tempHome)
	t.Cleanup(func() { corelib.SetMaclawBaseDir("") })

	app := &App{testHomeDir: tempHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	if err := app.skillExecutor.Register(corelib.NLSkillEntry{
		Name:      "to-review",
		Status:    "active",
		LastError: "[class: command_not_found] missing tool",
		Source:    "file",
		CreatedAt: "2026-01-01T00:00:00Z",
		Steps:     []corelib.NLSkillStep{{Action: "noop", Params: map[string]interface{}{}, OnError: "stop"}},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := app.SetNLSkillStatus("to-review", "needs_review"); err != nil {
		t.Fatalf("SetNLSkillStatus: %v", err)
	}
	defs := app.ListNLSkills()
	got := findNLSkillDefinitionForTest(defs, "to-review")
	if got == nil || got.Status != "needs_review" {
		t.Fatalf("expected needs_review: %#v", got)
	}
	events, err := skill.ListEvolutionAudit(skill.DefaultEvolutionAuditPath(), 20)
	if err != nil {
		t.Fatalf("ListEvolutionAudit: %v", err)
	}
	found := false
	for _, ev := range events {
		if ev.Skill == "to-review" && ev.Kind == "mark_needs_review" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected mark_needs_review audit row, path=%s events=%#v", skill.DefaultEvolutionAuditPath(), events)
	}
}

func TestSetNLSkillStatusClearsMaintenanceRetirementMarker(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	if err := app.skillExecutor.Register(corelib.NLSkillEntry{
		Name:      "retired-dup",
		Status:    "disabled",
		LastError: "retired_by_maintenance_duplicate: kept other-skill at 2026-01-01T00:00:00Z",
		Source:    "crafted",
		CreatedAt: "2026-01-01T00:00:00Z",
		Steps:     []corelib.NLSkillStep{{Action: "noop", Params: map[string]interface{}{}, OnError: "stop"}},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := app.SetNLSkillStatus("retired-dup", "active"); err != nil {
		t.Fatalf("SetNLSkillStatus: %v", err)
	}
	defs := app.ListNLSkills()
	got := findNLSkillDefinitionForTest(defs, "retired-dup")
	if got == nil || got.Status != "active" {
		t.Fatalf("expected active: %#v", got)
	}
	if strings.TrimSpace(got.LastError) != "" {
		t.Fatalf("expected maintenance last_error cleared, got %q", got.LastError)
	}
}

func TestSkillExecutorExecuteStep_CallMCPToolResolvesName(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.LocalMCPServers = []corelib.LocalMCPServerEntry{newHelperLocalMCPServerEntry("enabled-no-autostart", false, false)}
	cfg.LocalMCPServers[0].Name = "brave-search"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.mcpRegistry = NewMCPRegistry(app)
	app.localMCPManager = NewLocalMCPManager(app.mcpRegistry)
	defer app.localMCPManager.StopAll()
	app.localMCPManager.SyncFromConfig()

	executor := NewSkillExecutor(app, app.mcpRegistry, nil)
	result, err := executor.executeStep(corelib.NLSkillStep{
		Action: "call_mcp_tool",
		Params: map[string]interface{}{
			"server_id": "brave-search",
			"tool_name": "ping",
			"arguments": map[string]interface{}{},
		},
	}, "")
	if err != nil {
		t.Fatalf("executeStep() error = %v", err)
	}
	if strings.TrimSpace(result) != "{}" {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestSkillExecutorExecuteStep_CallMCPToolRejectsAmbiguousName(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.LocalMCPServers = []corelib.LocalMCPServerEntry{
		newHelperLocalMCPServerEntry("server-a", false, false),
		newHelperLocalMCPServerEntry("server-b", false, false),
	}
	cfg.LocalMCPServers[0].Name = "brave-search"
	cfg.LocalMCPServers[1].Name = "brave-search"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.mcpRegistry = NewMCPRegistry(app)
	app.localMCPManager = NewLocalMCPManager(app.mcpRegistry)
	defer app.localMCPManager.StopAll()
	app.localMCPManager.SyncFromConfig()

	executor := NewSkillExecutor(app, app.mcpRegistry, nil)
	_, err = executor.executeStep(corelib.NLSkillStep{
		Action: "call_mcp_tool",
		Params: map[string]interface{}{
			"server_id": "brave-search",
			"tool_name": "ping",
			"arguments": map[string]interface{}{},
		},
	}, "")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous name error, got: %v", err)
	}
}

func TestSkillCreateSessionGuard_DoesNotGuessSSHIntentWithoutSemantic(t *testing.T) {
	setUnifiedClassifierForIM(nil)
	t.Cleanup(func() { setUnifiedClassifierForIM(nil) })
	hint := skillCreateSessionGuard("", corelib.NLSkillStep{
		Action: "create_session",
		Params: map[string]interface{}{
			"tool": "claude",
			"task": "ssh to 10.0.0.8 and inspect nginx logs",
		},
	})
	if !strings.Contains(hint, "Cannot determine whether this needs a coding session") {
		t.Fatalf("expected ambiguous guard hint without semantic classifier, got: %s", hint)
	}
}

func TestSkillCreateSessionGuard_BlocksAmbiguousIntent(t *testing.T) {
	setUnifiedClassifierForIM(nil)
	t.Cleanup(func() { setUnifiedClassifierForIM(nil) })
	hint := skillCreateSessionGuard("handle the production issue", corelib.NLSkillStep{
		Action: "create_session",
		Params: map[string]interface{}{"tool": "claude"},
	})
	if !strings.Contains(hint, "Cannot determine whether this needs a coding session") {
		t.Fatalf("expected ambiguous guard hint, got: %s", hint)
	}
}

func TestSkillCreateSessionGuard_DoesNotGuessNonCodingPresentationIntentWithoutSemantic(t *testing.T) {
	setUnifiedClassifierForIM(nil)
	t.Cleanup(func() { setUnifiedClassifierForIM(nil) })
	hint := skillCreateSessionGuard("generate a presentation PPT", corelib.NLSkillStep{
		Action: "create_session",
		Params: map[string]interface{}{"tool": "claude"},
	})
	if !strings.Contains(hint, "Cannot determine whether this needs a coding session") {
		t.Fatalf("expected ambiguous guard hint without semantic classifier, got: %s", hint)
	}
}

func TestSkillCreateSessionGuard_DoesNotGuessCodingIntentWithoutSemantic(t *testing.T) {
	setUnifiedClassifierForIM(nil)
	t.Cleanup(func() { setUnifiedClassifierForIM(nil) })
	hint := skillCreateSessionGuard("fix the Go project bug and modify code", corelib.NLSkillStep{
		Action: "create_session",
		Params: map[string]interface{}{"tool": "claude"},
	})
	if !strings.Contains(hint, "Cannot determine whether this needs a coding session") {
		t.Fatalf("expected ambiguous guard hint without semantic classifier, got: %s", hint)
	}
}

func TestResolveSkillTaskText_PrefersStepTaskFields(t *testing.T) {
	text := resolveSkillTaskText("translate the paper", corelib.NLSkillStep{
		Params: map[string]interface{}{
			"task_description": "ssh to the server and run journalctl",
		},
	})
	if text != "ssh to the server and run journalctl" {
		t.Fatalf("unexpected task text: %q", text)
	}
}

func TestSkillExecutorExecute_BlocksCreateSessionWhenSemanticUnavailable(t *testing.T) {
	setUnifiedClassifierForIM(nil)
	t.Cleanup(func() { setUnifiedClassifierForIM(nil) })
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	executor := NewSkillExecutor(app, nil, nil)
	entry := corelib.NLSkillEntry{
		Name:        "ssh-skill",
		Description: "ssh to 10.0.0.8 and inspect nginx logs",
		Status:      "active",
		Steps: []corelib.NLSkillStep{{
			Action: "create_session",
			Params: map[string]interface{}{
				"tool":         "claude",
				"project_path": tempHome,
			},
		}},
	}
	if err := executor.Register(entry); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	output, err := executor.Execute("ssh-skill")
	if err == nil {
		t.Fatal("expected Execute to fail for ssh intent")
	}
	if output != "" || !strings.Contains(err.Error(), "unsupported_step_action") || !strings.Contains(err.Error(), "external coding sessions") {
		t.Fatalf("unexpected output=%q err=%v", output, err)
	}
}

func TestSkillExecutorExecuteStep_SSHListWithoutSession(t *testing.T) {
	executor := NewSkillExecutor(&App{}, nil, nil)
	result, err := executor.executeStep(corelib.NLSkillStep{
		Action: "ssh",
		Params: map[string]interface{}{
			"action": "list",
		},
	}, "")
	if err != nil {
		t.Fatalf("executeStep() error = %v", err)
	}
	if strings.TrimSpace(result) == "" {
		t.Fatalf("unexpected empty result")
	}
}

func TestSkillExecutorExecuteStep_SSHListTasksWithoutTasks(t *testing.T) {
	executor := NewSkillExecutor(&App{}, nil, nil)
	result, err := executor.executeStep(corelib.NLSkillStep{
		Action: "ssh",
		Params: map[string]interface{}{
			"action": "list_tasks",
		},
	}, "")
	if err != nil {
		t.Fatalf("executeStep() error = %v", err)
	}
	if strings.TrimSpace(result) == "" {
		t.Fatalf("unexpected empty result")
	}
}

func TestSkillExecutorExecuteStep_SSHWaitTaskIsRecognized(t *testing.T) {
	executor := NewSkillExecutor(&App{}, nil, nil)
	result, err := executor.executeStep(corelib.NLSkillStep{
		Action: "ssh",
		Params: map[string]interface{}{
			"action":  "wait_task",
			"task_id": "bg_missing",
		},
	}, "")
	if err != nil {
		t.Fatalf("executeStep() error = %v", err)
	}
	if !strings.Contains(result, "background task manager is not initialized") {
		t.Fatalf("wait_task did not route to SSH wait handler: %q", result)
	}
}

func TestSkillExecutorExecuteStep_SSHUnknownAction(t *testing.T) {
	executor := NewSkillExecutor(&App{}, nil, nil)
	_, err := executor.executeStep(corelib.NLSkillStep{
		Action: "ssh",
		Params: map[string]interface{}{
			"action": "boom",
		},
	}, "")
	if err == nil || !strings.Contains(err.Error(), "supported: connect") {
		t.Fatalf("expected unknown ssh action error, got: %v", err)
	}
}

func TestSkillExecutorExecuteStep_CraftToolAcceptsLegacyInstructions(t *testing.T) {
	executor := NewSkillExecutor(nil, nil, nil)
	_, err := executor.executeStep(corelib.NLSkillStep{
		Action: "craft_tool",
		Params: map[string]interface{}{
			"instructions": "legacy task",
		},
	}, "")
	if err == nil {
		t.Fatal("expected craft_tool to return a validation error")
	}
	if strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("craft_tool should not fall through unknown action: %v", err)
	}
}

func TestSkillExecutorExecuteStep_CraftToolMissingTask(t *testing.T) {
	executor := NewSkillExecutor(&App{}, nil, nil)
	_, err := executor.executeStep(corelib.NLSkillStep{
		Action: "craft_tool",
		Params: map[string]interface{}{},
	}, "")
	if err == nil || !strings.Contains(err.Error(), "missing task parameter") {
		t.Fatalf("expected missing task error, got: %v", err)
	}
}
