package main

import (
	"strings"
	"testing"
	"time"
)

func TestToolRunSkill_MissingName(t *testing.T) {
	app := &App{}
	app.skillRunner = NewSkillRunner(&SkillExecutor{app: app})
	h := &IMMessageHandler{app: app}
	if got := h.toolRunSkill(map[string]interface{}{}); got != "缺少 name 参数" {
		t.Fatalf("expected missing name error, got %s", got)
	}
}

func TestToolRunSkill_StartFailure(t *testing.T) {
	app := &App{}
	app.skillRunner = NewSkillRunner(&SkillExecutor{app: app})
	h := &IMMessageHandler{app: app}
	got := h.toolRunSkill(map[string]interface{}{"name": "missing-skill"})
	if !strings.Contains(got, "Skill 启动失败") {
		t.Fatalf("expected start failure message, got %s", got)
	}
}

func TestInstallSkillHub_AutoRunAcceptsWaitSeconds(t *testing.T) {
	if got := normalizeSkillRunWaitSeconds(float64(99)); got > 30*time.Second {
		t.Fatalf("expected wait_seconds clamp to 30s, got %v", got)
	}
}

func TestToolRunSkill_WaitSecondsInStructuredOutput(t *testing.T) {
	app := &App{}
	app.skillRunner = NewSkillRunner(&SkillExecutor{app: app})
	h := &IMMessageHandler{app: app}
	got := h.toolRunSkill(map[string]interface{}{"name": "missing-skill", "wait_seconds": float64(99)})
	if !strings.Contains(got, "Skill 启动失败") {
		t.Fatalf("expected start failure message even with wait_seconds, got %s", got)
	}
}

func TestToolRunSkill_ReportsRunAndSessionMeta(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEnabled = true
	cfg.Projects = []ProjectConfig{{Id: "proj-1", Name: "Demo", Path: tempHome}}
	cfg.CurrentProject = "proj-1"
	cfg.Claude = ToolConfig{
		CurrentModel: "Original",
		Models:       []ModelConfig{{ModelName: "Original", ModelId: "claude-sonnet", IsBuiltin: true}},
	}
	cfg.NLSkills = []NLSkillEntry{{
		Name:        "demo-skill",
		Description: "demo",
		Status:      "active",
		Steps: []NLSkillStep{{
			Action: "create_session",
			Params: map[string]interface{}{
				"tool":       "claude",
				"project_id": "proj-1",
				"task":       "修复代码中的 bug",
			},
		}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	provider := &fakeProviderAdapter{cmd: CommandSpec{Command: "claude.exe"}}
	app.remoteSessions = NewRemoteSessionManager(app)
	app.remoteSessions.providerFactory = func(tool string) (ProviderAdapter, error) {
		return provider, nil
	}
	app.remoteSessions.executionFactory = func(spec LaunchSpec) (ExecutionStrategy, error) {
		return &fakeExecutionStrategy{handle: newFakeExecutionHandle(302)}, nil
	}
	app.skillExecutor = NewSkillExecutor(app, nil, app.remoteSessions)
	app.skillRunner = NewSkillRunner(app.skillExecutor)
	app.sessionStarter = NewCodingSessionStarter(app)

	h := &IMMessageHandler{app: app}
	got := h.toolRunSkill(map[string]interface{}{"name": "demo-skill"})
	if !strings.Contains(got, "✅ Skill 已启动") {
		t.Fatalf("expected started message, got %s", got)
	}
	if !strings.Contains(got, "## 运行信息") {
		t.Fatalf("expected structured run info, got %s", got)
	}
	if !strings.Contains(got, "- run_id:") {
		t.Fatalf("expected run_id field, got %s", got)
	}
	if !strings.Contains(got, "- skill: demo-skill") {
		t.Fatalf("expected skill field, got %s", got)
	}
	if !strings.Contains(got, "## 下一步") {
		t.Fatalf("expected next-step section, got %s", got)
	}
}
