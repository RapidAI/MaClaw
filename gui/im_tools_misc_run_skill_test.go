package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
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
		"name":         "demo-skill",
		"action":       "run",
		"input":        "report.md",
		"output":       "report.pdf",
		"query":        "weather in Chengdu",
		"mode":         "advanced",
		"operation":    "safe",
		"steps":        []interface{}{"safe-step"},
		"wait_seconds": 30,
		"auto_run":     true,
		"auto_fix":     true,
		"force":        true,
		"field":        "command",
		"value":        "patched",
		"find":         "old",
		"replace":      "new",
		"reason":       "patch test",
		"args":         map[string]interface{}{"format": "A4", "count": 2, "operation": "nested-safe"},
	}
	got := buildRunSkillArgs(raw)
	argsMap, _ := got["args"].(map[string]interface{})
	if got["input"] != "report.md" || got["output"] != "report.pdf" {
		t.Fatalf("buildRunSkillArgs() = %#v, want input/output preserved", got)
	}
	if got["query"] != "weather in Chengdu" {
		t.Fatalf("buildRunSkillArgs() = %#v, want query preserved for run-time inference", got)
	}
	if got["mode"] != "advanced" {
		t.Fatalf("buildRunSkillArgs() = %#v, want mode preserved for run-time conditions", got)
	}
	if got["operation"] != "safe" || len(got["steps"].([]interface{})) != 1 {
		t.Fatalf("buildRunSkillArgs() = %#v, want workflow selectors preserved", got)
	}
	for _, key := range []string{"name", "action", "wait_seconds", "auto_run", "auto_fix", "force", "field", "value", "find", "replace", "reason"} {
		if _, ok := got[key]; ok {
			t.Fatalf("buildRunSkillArgs() = %#v, want %s control key stripped", got, key)
		}
	}
	if argsMap["format"] != "A4" || argsMap["operation"] != "nested-safe" {
		t.Fatalf("buildRunSkillArgs args = %#v, want nested runtime args preserved", argsMap)
	}
}

func TestToolRunSkill_OpensAgentViewForMissingRequiredParams(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:        "needs-input",
		Description: "requires input",
		Status:      "active",
		Params: []corelib.NLSkillParam{{
			Name:        "input",
			Description: "Input file",
			Required:    true,
		}},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo {{input}}"},
		}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.skillRunner = NewSkillRunner(app.skillExecutor)
	h := &IMMessageHandler{app: app}

	got := h.toolRunSkill(map[string]interface{}{"name": "needs-input"}, nil)
	// After mechanism fix (#97): missing params return a structured error for
	// the LLM to auto-fill, instead of popping an AgentView form.
	if !strings.Contains(got, "缺少必要参数") {
		t.Fatalf("expected missing params error, got %s", got)
	}
	if !strings.Contains(got, "input") {
		t.Fatalf("expected 'input' in missing params list, got %s", got)
	}
	if !strings.Contains(got, "[action: provide_args]") {
		t.Fatalf("expected [action: provide_args] marker, got %s", got)
	}
}

func TestBuildSkillRunAgentViewIncludesHiddenRunArgs(t *testing.T) {
	view := buildSkillRunAgentView(corelib.NLSkillEntry{
		Name:        "demo",
		Description: "demo skill",
	}, map[string]interface{}{"operation": "convert"}, []corelib.NLSkillParam{{
		Name:     "input",
		Required: true,
	}}, []string{"input"})
	if view == nil || view["type"] != "form" || view["id"] != "skill:run:demo" {
		t.Fatalf("unexpected view: %#v", view)
	}
	fields, ok := view["fields"].([]map[string]interface{})
	if !ok || len(fields) < 2 {
		t.Fatalf("unexpected fields: %#v", view["fields"])
	}
	foundRunArgs := false
	for _, field := range fields {
		if field["type"] == "hidden" && field["name"] == "_run_args" {
			foundRunArgs = true
			break
		}
	}
	if !foundRunArgs {
		t.Fatalf("expected hidden run args field, got %#v", fields)
	}
}

func TestToolRunSkill_ForwardsModeToWhenCondition(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:   "mode-skill",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			When:   "{{mode}} == advanced",
			Params: map[string]interface{}{"command": "echo advanced-mode"},
		}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.skillRunner = NewSkillRunner(app.skillExecutor)
	h := &IMMessageHandler{app: app}

	got := h.toolRunSkill(map[string]interface{}{"name": "mode-skill", "mode": "advanced", "wait_seconds": float64(2)}, nil)
	if !strings.Contains(got, "advanced-mode") {
		t.Fatalf("mode was not forwarded into when-conditioned step, got %s", got)
	}
}

func TestToolRunSkill_ForwardsQueryToWhenCondition(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:   "query-skill",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			When:   "{{query}} contains Chengdu",
			Params: map[string]interface{}{"command": "echo query-city"},
		}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.skillRunner = NewSkillRunner(app.skillExecutor)
	h := &IMMessageHandler{app: app}

	got := h.toolRunSkill(map[string]interface{}{"name": "query-skill", "query": "weather in Chengdu", "wait_seconds": float64(2)}, nil)
	if !strings.Contains(got, "query-city") {
		t.Fatalf("query was not forwarded into when-conditioned step, got %s", got)
	}
}

func TestToolRunSkill_PipelineRecursionSurfacesInRunStatus(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:   "pipeline-self",
		Status: "active",
		Mode:   "pipeline",
		Pipeline: []corelib.SkillPipelineStep{{
			Skill: "pipeline-self",
		}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.skillRunner = NewSkillRunner(app.skillExecutor)
	h := &IMMessageHandler{app: app}

	started := h.toolRunSkill(map[string]interface{}{
		"name":         "pipeline-self",
		"wait_seconds": float64(2),
	}, nil)
	if !strings.Contains(started, "- run_id:") {
		t.Fatalf("expected run to start so async pipeline failure can surface, got %s", started)
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

	status := h.toolGetSkillRun(map[string]interface{}{"run_id": runID, "wait_seconds": float64(2)})
	if !strings.Contains(status, "pipeline recursion detected") {
		t.Fatalf("expected recursion to surface in run status, got %s", status)
	}
}

func TestToolRunSkill_ExternalPrivatePipelineStackDoesNotTripRecursion(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{
		{
			Name:   "child-stack",
			Status: "active",
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{"command": "echo child-ok"},
			}},
		},
		{
			Name:   "pipeline-stack",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{{
				Skill: "child-stack",
			}},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.skillRunner = NewSkillRunner(app.skillExecutor)
	h := &IMMessageHandler{app: app}

	got := h.toolRunSkill(map[string]interface{}{
		"name":                     "pipeline-stack",
		cskill.PipelineRunStackArg: []string{"pipeline-stack"},
		"wait_seconds":             float64(2),
	}, nil)
	if strings.Contains(got, "pipeline recursion detected") || !strings.Contains(got, "child-ok") {
		t.Fatalf("expected forged external stack to be ignored, got %s", got)
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
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "doc-only", Status: "active", Steps: nil}}
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
	cfg.Projects = []corelib.ProjectConfig{{Id: "proj-1", Name: "Demo", Path: tempHome}}
	cfg.CurrentProject = "proj-1"
	cfg.Claude = corelib.ToolConfig{
		CurrentModel: "Original",
		Models:       []corelib.ModelConfig{{ModelName: "Original", ModelId: "claude-sonnet", IsBuiltin: true}},
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:        "demo-skill",
		Description: "demo",
		Status:      "active",
		Steps: []corelib.NLSkillStep{{
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
		Status: skillRunStatusRunning,
		Steps:  []StepResult{{Index: 0, Action: "create_session", Status: skillStepStatusRunning}},
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
	cfg.Projects = []corelib.ProjectConfig{{Id: "proj-1", Name: "Demo", Path: tempHome}}
	cfg.CurrentProject = "proj-1"
	cfg.Claude = corelib.ToolConfig{
		CurrentModel: "Original",
		Models:       []corelib.ModelConfig{{ModelName: "Original", ModelId: "claude-sonnet", IsBuiltin: true}},
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:        "demo-skill",
		Description: "demo",
		Status:      "active",
		Steps: []corelib.NLSkillStep{
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
	started := h.toolRunSkill(map[string]interface{}{"name": "demo-skill", "wait_seconds": float64(1)}, nil)
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

	status := h.toolGetSkillRun(map[string]interface{}{"run_id": runID, "wait_seconds": float64(1)})
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
		Status: skillRunStatusSuccess,
		Summary: SkillRunSummary{
			NeedsArtifactVerification: true,
			ArtifactPath:              `C:\tmp\deck.pptx`,
			ArtifactStatus:            skillArtifactStatusMissing,
		},
		Steps: []StepResult{{Action: "craft_tool", Status: skillStepStatusSuccess}},
	}, "run-123")
	got := b.String()
	if !strings.Contains(got, "## 结果说明") {
		t.Fatalf("expected explanation section, got %s", got)
	}
	if !strings.Contains(got, "目标产物: C:\\tmp\\deck.pptx") {
		t.Fatalf("expected artifact path, got %s", got)
	}
	if !strings.Contains(got, "当前不能算成功交付") {
		t.Fatalf("expected delivery warning, got %s", got)
	}
	if !strings.Contains(got, "artifact_status: missing") {
		t.Fatalf("expected artifact status line, got %s", got)
	}
}

func TestToolRunSkill_RealXhMdToPdfVerifiesArtifact(t *testing.T) {
	homeDir := os.Getenv("USERPROFILE")
	if strings.TrimSpace(homeDir) == "" {
		homeDir = os.Getenv("HOME")
	}
	if strings.TrimSpace(homeDir) == "" {
		t.Skip("home directory not available")
	}
	skillDir := filepath.Join(homeDir, ".maclaw", "data", "skills", "xh-md-to-pdf")
	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
		t.Skipf("xh-md-to-pdf skill not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "scripts", "xh-md-to-pdf.mjs")); err != nil {
		t.Skipf("xh-md-to-pdf script missing: %v", err)
	}
	app := &App{testHomeDir: homeDir}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.skillRunner = NewSkillRunner(app.skillExecutor)
	h := &IMMessageHandler{app: app}

	workDir := t.TempDir()
	inputPath := filepath.Join(workDir, "sample.md")
	outputPath := filepath.Join(workDir, "sample.pdf")
	if err := os.WriteFile(inputPath, []byte("# Demo\n\n- one\n- two\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(input) error = %v", err)
	}

	started := h.toolRunSkill(map[string]interface{}{
		"name":         "xh-md-to-pdf",
		"input":        inputPath,
		"output":       outputPath,
		"wait_seconds": float64(20),
	}, nil)
	if !strings.Contains(started, "- run_id:") {
		t.Fatalf("expected run_id in start output, got %s", started)
	}
	if !strings.Contains(started, "artifact_path: "+outputPath) {
		t.Fatalf("expected artifact path in start output, got %s", started)
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

	status := h.toolGetSkillRun(map[string]interface{}{"run_id": runID, "wait_seconds": float64(20)})
	if !strings.Contains(status, "artifact_path: "+outputPath) {
		t.Fatalf("expected artifact path in status, got %s", status)
	}
	if !strings.Contains(status, "artifact_status: verified") {
		status = h.toolGetSkillRun(map[string]interface{}{"run_id": runID, "wait_seconds": float64(20)})
	}
	runnerStatus, err := app.skillRunner.GetRunStatus(runID)
	if err != nil {
		t.Fatalf("GetRunStatus() error = %v", err)
	}
	if !strings.Contains(status, "artifact_status: verified") {
		t.Fatalf("expected verified artifact status, got %s\nraw_step_output=%q\nraw_step_error=%q", status, runnerStatus.Steps[0].Output, runnerStatus.Steps[0].Error)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("expected output pdf to exist: %v", err)
	}
}

func TestAppendSkillRunSummary_IncludesStepOutput(t *testing.T) {
	var b strings.Builder
	appendSkillRunSummary(&b, &SkillRunStatus{
		Skill:  "weather-query",
		Status: skillRunStatusSuccess,
		Steps: []StepResult{
			{Index: 0, Action: "bash", Status: skillStepStatusSuccess, Output: "=== 北京 当前天气 ===\n天气：☀️ 晴  气温：26.9℃", DurationMs: 1200},
			{Index: 1, Action: "bash", Status: skillStepStatusSuccess, Output: "=== 北京 逐小时预报 ===\n14:00 ☀️ 27℃", DurationMs: 800},
			{Index: 2, Action: "bash", Status: skillStepStatusSuccess, Output: "=== 北京 一周预报 ===\n周一 晴 20~28℃", DurationMs: 900},
		},
	}, "run-weather-1")
	got := b.String()

	// Step outputs must be present in the summary so the LLM can see actual results.
	if !strings.Contains(got, "=== 北京 当前天气 ===") {
		t.Fatalf("expected step 1 output in summary, got %s", got)
	}
	if !strings.Contains(got, "=== 北京 逐小时预报 ===") {
		t.Fatalf("expected step 2 output in summary, got %s", got)
	}
	if !strings.Contains(got, "=== 北京 一周预报 ===") {
		t.Fatalf("expected step 3 output in summary, got %s", got)
	}
	// Completed skill should tell LLM to use the output directly.
	if !strings.Contains(got, "步骤输出已在上方显示") {
		t.Fatalf("expected direct-use guidance for completed skill, got %s", got)
	}
	// Should NOT mention session_ready for completed non-session skills.
	if strings.Contains(got, "session_ready") {
		t.Fatalf("completed non-session skill should not emit session_ready, got %s", got)
	}
}

func TestAppendSkillRunSummary_TruncatesLongOutput(t *testing.T) {
	longOutput := strings.Repeat("x", 3000)
	var b strings.Builder
	appendSkillRunSummary(&b, &SkillRunStatus{
		Skill:  "demo",
		Status: skillRunStatusSuccess,
		Steps: []StepResult{
			{Index: 0, Action: "bash", Status: skillStepStatusSuccess, Output: longOutput},
		},
	}, "run-trunc-1")
	got := b.String()
	if !strings.Contains(got, "... (truncated)") {
		t.Fatalf("expected truncation marker for long output, got len=%d", len(got))
	}
	// Output should be capped at maxStepOutputLen (2048 runes), not the full 3000.
	if strings.Contains(got, strings.Repeat("x", 2100)) {
		t.Fatalf("output was not truncated")
	}
}

func TestAppendSkillRunSummary_TotalOutputBudget(t *testing.T) {
	// 3 steps each with 2000 chars — total 6000 exceeds maxTotalOutputLen (4096).
	// Later steps should be truncated or omitted.
	stepOutput := strings.Repeat("a", 2000)
	var b strings.Builder
	appendSkillRunSummary(&b, &SkillRunStatus{
		Skill:  "demo",
		Status: skillRunStatusSuccess,
		Steps: []StepResult{
			{Index: 0, Action: "bash", Status: skillStepStatusSuccess, Output: stepOutput},
			{Index: 1, Action: "bash", Status: skillStepStatusSuccess, Output: stepOutput},
			{Index: 2, Action: "bash", Status: skillStepStatusSuccess, Output: stepOutput},
		},
	}, "run-budget-1")
	got := b.String()
	// All 3 step status lines should be present.
	if !strings.Contains(got, "step 3: bash") {
		t.Fatalf("expected all step status lines, got %s", got)
	}
	// Total output should be capped — step 3's full output should not appear.
	count := strings.Count(got, strings.Repeat("a", 2000))
	if count >= 3 {
		t.Fatalf("expected total output budget to cap later steps, but all 3 full outputs present")
	}
}

func TestAppendSkillRunSummary_UTF8SafeTruncation(t *testing.T) {
	// Chinese characters are multi-byte; truncation must not split them.
	chineseOutput := strings.Repeat("天气晴朗温度适宜", 400) // 3200 runes
	var b strings.Builder
	appendSkillRunSummary(&b, &SkillRunStatus{
		Skill:  "weather",
		Status: skillRunStatusSuccess,
		Steps: []StepResult{
			{Index: 0, Action: "bash", Status: skillStepStatusSuccess, Output: chineseOutput},
		},
	}, "run-utf8-1")
	got := b.String()
	if !strings.Contains(got, "... (truncated)") {
		t.Fatalf("expected truncation for long Chinese output")
	}
	// Verify no invalid UTF-8 sequences in output.
	for i, r := range got {
		if r == '\uFFFD' {
			t.Fatalf("found replacement character at position %d — UTF-8 truncation is broken", i)
		}
	}
}

func TestAppendSkillRunSummary_RunningSkillStillShowsPollingGuidance(t *testing.T) {
	var b strings.Builder
	appendSkillRunSummary(&b, &SkillRunStatus{
		Skill:  "demo",
		Status: skillRunStatusRunning,
		Steps: []StepResult{
			{Index: 0, Action: "bash", Status: skillStepStatusSuccess, Output: "step 1 done"},
			{Index: 1, Action: "bash", Status: skillStepStatusRunning},
		},
	}, "run-poll-1")
	got := b.String()
	// Running skill should still suggest polling.
	if !strings.Contains(got, "get_skill_run") {
		t.Fatalf("expected polling guidance for running skill, got %s", got)
	}
	// But completed step output should still be visible.
	if !strings.Contains(got, "step 1 done") {
		t.Fatalf("expected completed step output even while running, got %s", got)
	}
}
