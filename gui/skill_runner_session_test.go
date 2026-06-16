package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestSkillRunnerCreateSessionDisabled(t *testing.T) {
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
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	provider := &fakeProviderAdapter{cmd: CommandSpec{Command: "claude.exe"}}
	app.remoteSessions = NewRemoteSessionManager(app)
	app.remoteSessions.providerFactory = func(tool string) (ProviderAdapter, error) {
		return provider, nil
	}
	app.remoteSessions.executionFactory = func(spec LaunchSpec) (ExecutionStrategy, error) {
		return &fakeExecutionStrategy{handle: newFakeExecutionHandle(300)}, nil
	}
	app.skillExecutor = NewSkillExecutor(app, nil, app.remoteSessions)
	app.sessionStarter = NewCodingSessionStarter(app)

	runner := NewSkillRunner(app.skillExecutor)
	runID := "run-meta"
	runner.runs[runID] = &skillRun{status: SkillRunStatus{RunID: runID, Skill: "demo", Status: "running"}}

	output, err := runner.executeStepWithContext(context.Background(), runID, corelib.NLSkillStep{
		Action: "create_session",
		Params: map[string]interface{}{
			"tool":              "claude",
			"project_id":        "proj-1",
			"resume_session_id": "resume-xyz",
		},
	}, "")
	if err == nil {
		t.Fatalf("expected create_session to be disabled, got output %q", output)
	}
	if !strings.Contains(err.Error(), "external coding sessions") {
		t.Fatalf("err = %v", err)
	}

	status, err := runner.GetRunStatus(runID)
	if err != nil {
		t.Fatalf("GetRunStatus() error = %v", err)
	}
	if status.Session != nil {
		t.Fatalf("session metadata should not be set: %#v", status.Session)
	}
}

func TestCodingSessionStarterDisabled(t *testing.T) {
	starter := NewCodingSessionStarter(&App{})
	_, err := starter.Start(CodingSessionStartRequest{Tool: "claude"})
	if err == nil {
		t.Fatal("expected external coding session start to be disabled")
	}
	if !strings.Contains(err.Error(), "disabled") || !strings.Contains(err.Error(), "CodingSubAgent") {
		t.Fatalf("expected disabled CodingSubAgent guidance, got: %v", err)
	}
}

func TestCodingSessionStarterLinksTraceRuns(t *testing.T) {
	t.Skip("legacy external coding session start is disabled; covered by TestCodingSessionStarterDisabled")

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
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	provider := &fakeProviderAdapter{cmd: CommandSpec{Command: "claude.exe"}}
	app.remoteSessions = NewRemoteSessionManager(app)
	app.remoteSessions.providerFactory = func(tool string) (ProviderAdapter, error) {
		return provider, nil
	}
	app.remoteSessions.executionFactory = func(spec LaunchSpec) (ExecutionStrategy, error) {
		return &fakeExecutionStrategy{handle: newFakeExecutionHandle(301)}, nil
	}
	app.ensureAITrace()
	_, parentRun := app.aiTrace.StartJobRun(TraceJobKindAIAssistant, "parent", "ai", "", tempHome)
	if parentRun.RunID == "" {
		t.Fatal("expected parent trace run id")
	}

	starter := NewCodingSessionStarter(app)
	result, err := starter.Start(CodingSessionStartRequest{
		Tool:         "claude",
		ProjectID:    "proj-1",
		LaunchSource: RemoteLaunchSourceAI,
		ParentRunID:  parentRun.RunID,
	})
	if err != nil {
		t.Fatalf("starter.Start() error = %v", err)
	}
	if result.View.RunID == "" {
		t.Fatal("expected remote run id")
	}
	traceView, ok := app.aiTrace.GetTrace(parentRun.RunID)
	if !ok {
		t.Fatalf("GetTrace() returned !ok for %s", parentRun.RunID)
	}
	found := false
	for _, linked := range traceView.LinkedRunIDs {
		if linked == result.View.RunID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected parent run to link child run %q, got %#v", result.View.RunID, traceView.LinkedRunIDs)
	}
}

func TestCodingSessionStarterAppliesCodexResumeSessionID(t *testing.T) {
	t.Skip("legacy external coding session start is disabled; covered by TestCodingSessionStarterDisabled")

	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEnabled = true
	cfg.Codex = corelib.ToolConfig{
		CurrentModel: "Original",
		Models:       []corelib.ModelConfig{{ModelName: "Original", ModelId: "gpt-5.2-codex", IsBuiltin: true}},
	}
	cfg.Projects = []corelib.ProjectConfig{{Id: "proj-1", Name: "Demo", Path: tempHome}}
	cfg.CurrentProject = "proj-1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	provider := &fakeProviderAdapter{cmd: CommandSpec{Command: "codex.exe"}}
	app.remoteSessions = NewRemoteSessionManager(app)
	app.remoteSessions.providerFactory = func(tool string) (ProviderAdapter, error) {
		return provider, nil
	}
	app.remoteSessions.executionFactory = func(spec LaunchSpec) (ExecutionStrategy, error) {
		return &fakeExecutionStrategy{handle: newFakeExecutionHandle(302)}, nil
	}

	starter := NewCodingSessionStarter(app)
	result, err := starter.Start(CodingSessionStartRequest{
		Tool:            "codex",
		ProjectID:       "proj-1",
		ResumeSessionID: "thread-resume-1",
	})
	if err != nil {
		t.Fatalf("starter.Start() error = %v", err)
	}
	if !result.ResumeApplied {
		t.Fatal("expected ResumeApplied to be true")
	}
	if result.ResumeSource != "explicit" {
		t.Fatalf("ResumeSource = %q, want %q", result.ResumeSource, "explicit")
	}
	if provider.lastSpec.ResumeSessionID != "thread-resume-1" {
		t.Fatalf("ResumeSessionID = %q, want %q", provider.lastSpec.ResumeSessionID, "thread-resume-1")
	}
	if result.View.Tool != "codex" {
		t.Fatalf("View.Tool = %q, want %q", result.View.Tool, "codex")
	}
}

func TestWriteAutoResumeHintPrefersResumeSessionID(t *testing.T) {
	var b strings.Builder
	writeAutoResumeHint(&b, &SessionResumeContext{
		Tool:            "codex",
		ProjectPath:     "/tmp/project",
		ResumeSessionID: "thread-789",
		ClaudeSessionID: "claude-session-123",
		ResumeCount:     1,
	}, "编程工具因 token 耗尽正常退出，但任务可能未完成。")
	out := b.String()
	if !containsText(out, "resume_session_id") {
		t.Fatalf("expected resume_session_id hint, got: %s", out)
	}
	if !containsText(out, "thread-789") {
		t.Fatalf("expected ResumeSessionID in hint, got: %s", out)
	}
	if containsText(out, "claude-session-123") {
		t.Fatalf("expected ClaudeSessionID fallback to be skipped when ResumeSessionID exists, got: %s", out)
	}
}

func TestWriteAutoResumeHintUsesConfiguredChineseLanguage(t *testing.T) {
	previousLang, _ := agentViewCurrentLang.Load().(string)
	setAgentViewLang("zh-Hans")
	t.Cleanup(func() { setAgentViewLang(previousLang) })

	var b strings.Builder
	writeAutoResumeHint(&b, &SessionResumeContext{
		Tool:            "codex",
		ProjectPath:     "/tmp/project",
		OriginalTask:    "修复任务接续",
		LastProgress:    "已定位续接提示",
		CompletedFiles:  []string{"gui/im_tools_session_resume_hint.go"},
		ResumeSessionID: "thread-789",
		ResumeCount:     1,
	}, "编程工具已正常退出，任务可能未完成。")
	out := b.String()
	for _, leaked := range []string{"Legacy resume context available", "Original task:", "Last progress:", "Completed files:", "for audit only"} {
		if containsText(out, leaked) {
			t.Fatalf("auto resume hint leaked English %q: %s", leaked, out)
		}
	}
	for _, want := range []string{"已有旧外部会话续接上下文", "原始任务：修复任务接续", "最近进度：已定位续接提示", "已完成文件：gui/im_tools_session_resume_hint.go", "旧 resume_session_id 仅用于审计：thread-789"} {
		if !containsText(out, want) {
			t.Fatalf("auto resume hint missing %q: %s", want, out)
		}
	}
}
