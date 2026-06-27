package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

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
