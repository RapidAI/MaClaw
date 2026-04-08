package main

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestToolRunSkill_MissingName(t *testing.T) {
	app := &App{}
	app.skillRunner = NewSkillRunner(&SkillExecutor{app: app})
	h := &IMMessageHandler{app: app}
	if got := h.toolRunSkill(map[string]interface{}{}, nil); got != "缺少 name 参数" {
		t.Fatalf("expected missing name error, got %s", got)
	}
}

func TestToolRunSkill_StartFailure(t *testing.T) {
	app := &App{}
	app.skillRunner = NewSkillRunner(&SkillExecutor{app: app})
	h := &IMMessageHandler{app: app}
	got := h.toolRunSkill(map[string]interface{}{"name": "missing-skill"}, nil)
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
	got := h.toolRunSkill(map[string]interface{}{"name": "missing-skill", "wait_seconds": float64(99)}, nil)
	if !strings.Contains(got, "Skill 启动失败") {
		t.Fatalf("expected start failure message even with wait_seconds, got %s", got)
	}
}

func TestToolRunSkill_BuildsRunArgs(t *testing.T) {
	raw := map[string]interface{}{
		"name":   "demo-skill",
		"input":  "report.md",
		"output": "report.pdf",
		"args":   map[string]interface{}{"format": "A4", "count": 2},
	}
	got := buildRunSkillArgs(raw)
	argsMap, _ := got["args"].(map[string]interface{})
	if got["input"] != "report.md" || got["output"] != "report.pdf" {
		t.Fatalf("buildRunSkillArgs() = %#v, want input/output preserved", got)
	}
	if argsMap["format"] != "A4" {
		t.Fatalf("buildRunSkillArgs args = %#v, want format preserved", argsMap)
	}
}

func TestToolRunSkill_RejectsSkillWithoutExecutableSteps(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []NLSkillEntry{{Name: "doc-only", Status: "active", Steps: nil}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.skillRunner = NewSkillRunner(app.skillExecutor)
	h := &IMMessageHandler{app: app}

	got := h.toolRunSkill(map[string]interface{}{"name": "doc-only"}, nil)
	if !strings.Contains(got, "Skill 启动失败") || !strings.Contains(got, "has no executable steps") {
		t.Fatalf("expected no executable steps failure, got %s", got)
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
	got := h.toolRunSkill(map[string]interface{}{"name": "demo-skill"}, nil)
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
	if !strings.Contains(got, "get_skill_run(run_id)") {
		t.Fatalf("expected get_skill_run next step, got %s", got)
	}
}

func TestToolRunSkill_EmitsProgressStages(t *testing.T) {
	app := &App{}
	app.skillRunner = NewSkillRunner(&SkillExecutor{app: app})
	h := &IMMessageHandler{app: app}
	var progress []string
	got := h.toolRunSkill(map[string]interface{}{"name": "missing-skill"}, func(msg string) {
		progress = append(progress, msg)
	})
	if !strings.Contains(got, "Skill 启动失败") {
		t.Fatalf("expected start failure message, got %s", got)
	}
	if len(progress) == 0 || !strings.Contains(progress[0], "🚀 正在启动 Skill") {
		t.Fatalf("progress = %#v, want startup progress", progress)
	}
}

func TestToolGetSkillRun_MissingRunID(t *testing.T) {
	app := &App{}
	app.skillRunner = NewSkillRunner(&SkillExecutor{app: app})
	h := &IMMessageHandler{app: app}
	if got := h.toolGetSkillRun(map[string]interface{}{}); got != "缺少 run_id 参数" {
		t.Fatalf("expected missing run_id error, got %s", got)
	}
}

func TestToolGetSkillRun_UnknownRunID(t *testing.T) {
	app := &App{}
	app.skillRunner = NewSkillRunner(&SkillExecutor{app: app})
	h := &IMMessageHandler{app: app}
	got := h.toolGetSkillRun(map[string]interface{}{"run_id": "run-missing"})
	if !strings.Contains(got, "读取 Skill 状态失败") || !strings.Contains(got, `run "run-missing" not found`) {
		t.Fatalf("expected missing run status error, got %s", got)
	}
}

func TestToolGetSkillRun_ReturnsStatusSummary(t *testing.T) {
	app := &App{}
	app.skillRunner = NewSkillRunner(&SkillExecutor{app: app, mu: sync.RWMutex{}})
	h := &IMMessageHandler{app: app}
	app.skillRunner.runs["run-123"] = &skillRun{status: SkillRunStatus{
		RunID:  "run-123",
		Skill:  "demo-skill",
		Status: "running",
		Steps:  []StepResult{{Index: 0, Action: "create_session", Status: "running"}},
	}}
	got := h.toolGetSkillRun(map[string]interface{}{"run_id": "run-123", "wait_seconds": float64(0.01)})
	if !strings.Contains(got, "🔎 Skill 状态查询结果") {
		t.Fatalf("expected status title, got %s", got)
	}
	if !strings.Contains(got, "- skill: demo-skill") || !strings.Contains(got, "- status: running") {
		t.Fatalf("expected structured skill status, got %s", got)
	}
}

func TestToolGetSkillRun_ReportsSessionFallbackFromUnknownExplicitSessionID(t *testing.T) {
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
		Steps: []NLSkillStep{
			{
				Action: "create_session",
				Params: map[string]interface{}{
					"tool":       "claude",
					"project_id": "proj-1",
					"task":       "修复代码中的 bug",
				},
			},
			{
				Action: "send_and_observe",
				Params: map[string]interface{}{
					"session_id":      "skill-runner",
					"text":            "继续执行",
					"timeout_seconds": float64(0.01),
				},
			},
		},
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
		return &fakeExecutionStrategy{handle: newFakeExecutionHandle(305)}, nil
	}
	app.skillExecutor = NewSkillExecutor(app, nil, app.remoteSessions)
	app.skillRunner = NewSkillRunner(app.skillExecutor)
	app.sessionStarter = NewCodingSessionStarter(app)

	h := &IMMessageHandler{app: app}
	started := h.toolRunSkill(map[string]interface{}{"name": "demo-skill", "wait_seconds": float64(0.01)}, nil)
	if !strings.Contains(started, "- run_id:") {
		t.Fatalf("expected run_id in start output, got %s", started)
	}
	runID := ""
	for _, line := range strings.Split(started, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- run_id:") {
			runID = strings.TrimSpace(strings.TrimPrefix(line, "- run_id:"))
			break
		}
	}
	if runID == "" {
		t.Fatalf("failed to parse run_id from %s", started)
	}

	status := h.toolGetSkillRun(map[string]interface{}{"run_id": runID, "wait_seconds": float64(0.01)})
	if !strings.Contains(status, "🔎 Skill 状态查询结果") {
		t.Fatalf("expected status title, got %s", status)
	}
	if strings.Contains(status, "会话 skill-runner 不存在") {
		t.Fatalf("expected fallback session reuse, got %s", status)
	}
	if !strings.Contains(status, "session_id:") {
		t.Fatalf("expected session metadata in status, got %s", status)
	}
}

func TestAppendSkillRunSummary_ExplainsInstructionOnlySkill(t *testing.T) {
	var b strings.Builder
	appendSkillRunSummary(&b, &SkillRunStatus{
		Skill:  "pptx-generator",
		Status: "success",
		Summary: SkillRunSummary{
			NeedsArtifactVerification: true,
		},
		Steps: []StepResult{{Action: "craft_tool", Status: "success"}},
	}, "run-123")
	got := b.String()
	if !strings.Contains(got, "## 结果说明") {
		t.Fatalf("expected explanation section, got %s", got)
	}
	if !strings.Contains(got, "尚未自动验证目标产物是否生成") {
		t.Fatalf("expected artifact verification warning, got %s", got)
	}
}
