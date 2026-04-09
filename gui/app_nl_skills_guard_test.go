package main

import (
	"path/filepath"
	"strings"
	"testing"
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
	cfg.LocalMCPServers = []LocalMCPServerEntry{newHelperLocalMCPServerEntry("enabled-no-autostart", false, false)}
	cfg.LocalMCPServers[0].Name = "brave-search"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.mcpRegistry = NewMCPRegistry(app)
	app.localMCPManager = NewLocalMCPManager(app.mcpRegistry)
	defer app.localMCPManager.StopAll()
	app.localMCPManager.SyncFromConfig()

	executor := NewSkillExecutor(app, app.mcpRegistry, nil)
	result, err := executor.executeStep(NLSkillStep{
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
	cfg.LocalMCPServers = []LocalMCPServerEntry{
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
	_, err = executor.executeStep(NLSkillStep{
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

func TestSkillCreateSessionGuard_BlocksSSHIntent(t *testing.T) {
	hint := skillCreateSessionGuard("", NLSkillStep{
		Action: "create_session",
		Params: map[string]interface{}{
			"tool": "claude",
			"task": "ssh 到 10.0.0.8 看 nginx 日志",
		},
	})
	if !strings.Contains(hint, "SSH/服务器操作任务") || !strings.Contains(hint, "ssh(action=\"connect\"") || !strings.Contains(hint, "exec_background") {
		t.Fatalf("expected structured ssh redirect hint, got: %s", hint)
	}
}

func TestSkillCreateSessionGuard_BlocksAmbiguousIntent(t *testing.T) {
	hint := skillCreateSessionGuard("处理一下线上问题", NLSkillStep{
		Action: "create_session",
		Params: map[string]interface{}{
			"tool": "claude",
		},
	})
	if !strings.Contains(hint, "不能确定") || !strings.Contains(hint, "编程会话") {
		t.Fatalf("expected ambiguous guard hint, got: %s", hint)
	}
}

func TestSkillCreateSessionGuard_BlocksNonCodingPresentationIntent(t *testing.T) {
	hint := skillCreateSessionGuard("生成宣传PPT", NLSkillStep{
		Action: "create_session",
		Params: map[string]interface{}{
			"tool": "claude",
		},
	})
	if !strings.Contains(hint, "不是编程任务") || !strings.Contains(hint, "ppt") {
		t.Fatalf("expected non-coding guard hint for presentation task, got: %s", hint)
	}
}

func TestSkillCreateSessionGuard_AllowsCodingIntent(t *testing.T) {
	hint := skillCreateSessionGuard("修复 Go 项目的 bug 并修改代码", NLSkillStep{
		Action: "create_session",
		Params: map[string]interface{}{
			"tool": "claude",
		},
	})
	if hint != "" {
		t.Fatalf("expected coding task to pass guard, got: %s", hint)
	}
}

func TestResolveSkillTaskText_PrefersStepTaskFields(t *testing.T) {
	text := resolveSkillTaskText("翻译论文", NLSkillStep{
		Params: map[string]interface{}{
			"task_description": "ssh 到服务器执行 journalctl",
		},
	})
	if text != "ssh 到服务器执行 journalctl" {
		t.Fatalf("unexpected task text: %q", text)
	}
}

func TestSkillExecutorExecute_BlocksCreateSessionForSSHSkill(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	executor := NewSkillExecutor(app, nil, nil)
	entry := NLSkillEntry{
		Name:        "ssh-skill",
		Description: "ssh 到 10.0.0.8 看 nginx 日志",
		Status:      "active",
		Steps: []NLSkillStep{{
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
	if !strings.Contains(output, "SSH/服务器操作任务") || !strings.Contains(output, "ssh(action=\"connect\"") || !strings.Contains(err.Error(), "skill execution stopped") {
		t.Fatalf("unexpected output=%q err=%v", output, err)
	}
}

func TestSkillExecutorExecuteStep_SSHListWithoutSession(t *testing.T) {
	executor := NewSkillExecutor(&App{}, nil, nil)
	result, err := executor.executeStep(NLSkillStep{
		Action: "ssh",
		Params: map[string]interface{}{
			"action": "list",
		},
	}, "")
	if err != nil {
		t.Fatalf("executeStep() error = %v", err)
	}
	if !strings.Contains(result, "当前无活跃 SSH 会话") {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestSkillExecutorExecuteStep_SSHListTasksWithoutTasks(t *testing.T) {
	executor := NewSkillExecutor(&App{}, nil, nil)
	result, err := executor.executeStep(NLSkillStep{
		Action: "ssh",
		Params: map[string]interface{}{
			"action": "list_tasks",
		},
	}, "")
	if err != nil {
		t.Fatalf("executeStep() error = %v", err)
	}
	if !strings.Contains(result, "当前无后台任务") {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestSkillExecutorExecuteStep_SSHUnknownAction(t *testing.T) {
	executor := NewSkillExecutor(&App{}, nil, nil)
	_, err := executor.executeStep(NLSkillStep{
		Action: "ssh",
		Params: map[string]interface{}{
			"action": "boom",
		},
	}, "")
	if err == nil || !strings.Contains(err.Error(), "未知 SSH 操作") {
		t.Fatalf("expected unknown ssh action error, got: %v", err)
	}
}

func TestSkillExecutorExecuteStep_CraftToolAcceptsLegacyInstructions(t *testing.T) {
	executor := NewSkillExecutor(nil, nil, nil)
	_, err := executor.executeStep(NLSkillStep{
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
	_, err := executor.executeStep(NLSkillStep{
		Action: "craft_tool",
		Params: map[string]interface{}{},
	}, "")
	if err == nil || !strings.Contains(err.Error(), "missing task parameter") {
		t.Fatalf("expected missing task error, got: %v", err)
	}
}
