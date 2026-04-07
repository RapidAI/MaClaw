package main

import (
	"context"
	"strings"
	"testing"
)

func TestSkillRunnerCreateSessionStoresSessionMeta(t *testing.T) {
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

	output, err := runner.executeStepWithContext(context.Background(), runID, NLSkillStep{
		Action: "create_session",
		Params: map[string]interface{}{
			"tool":              "claude",
			"project_id":        "proj-1",
			"resume_session_id": "resume-xyz",
		},
	}, "")
	if err != nil {
		t.Fatalf("executeStepWithContext(create_session) error = %v", err)
	}
	if output == "" {
		t.Fatal("expected non-empty output")
	}

	status, err := runner.GetRunStatus(runID)
	if err != nil {
		t.Fatalf("GetRunStatus() error = %v", err)
	}
	if status.Session == nil {
		t.Fatal("expected session metadata to be set")
	}
	if status.Session.SessionID == "" {
		t.Fatal("expected session_id to be set")
	}
	if status.Session.Tool != "claude" {
		t.Fatalf("Tool = %q, want %q", status.Session.Tool, "claude")
	}
	if status.Session.ProjectPath == "" {
		t.Fatal("expected project path to be recorded")
	}
	if status.Session.ResumeSessionID != "resume-xyz" {
		t.Fatalf("ResumeSessionID = %q, want %q", status.Session.ResumeSessionID, "resume-xyz")
	}
	if status.Session.RunID == "" {
		t.Fatal("expected remote run id to be recorded")
	}
}

func TestCodingSessionStarterLinksTraceRuns(t *testing.T) {
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

func TestWriteAutoResumeHintPrefersResumeSessionID(t *testing.T) {
	var b strings.Builder
	writeAutoResumeHint(&b, &SessionResumeContext{
		Tool:            "claude",
		ProjectPath:     "/tmp/project",
		ClaudeSessionID: "claude-session-123",
		ResumeCount:     1,
	}, "编程工具因 token 耗尽正常退出，但任务可能未完成。")
	out := b.String()
	if !contains(out, "resume_session_id") {
		t.Fatalf("expected resume_session_id hint, got: %s", out)
	}
	if !contains(out, "claude-session-123") {
		t.Fatalf("expected Claude session id in hint, got: %s", out)
	}
}
