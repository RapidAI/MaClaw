package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSkillRunnerExecuteStepWithContext_SendAndObserveUsesSharedHelper(t *testing.T) {
	session := &RemoteSession{
		ID:        "sess-observe-1",
		Tool:      "claude-code",
		Title:     "observe-session",
		Status:    SessionBusy,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Exec:      newFakeExecutionHandle(500),
		RawOutputLines: []string{
			"❯ do work",
		},
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
		session.mu.Lock()
		session.Status = SessionWaitingInput
		session.Summary.WaitingForUser = true
		session.RawOutputLines = append(session.RawOutputLines, "done")
		session.mu.Unlock()
	}()

	app := &App{}
	mgr := &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{"sess-observe-1": session}}
	runner := NewSkillRunner(&SkillExecutor{app: app, manager: mgr})

	result, err := runner.executeStepWithContext(context.Background(), "run-observe", NLSkillStep{
		Action: "send_and_observe",
		Params: map[string]interface{}{
			"session_id": "sess-observe-1",
			"text":       "do work",
		},
	}, "")
	if err != nil {
		t.Fatalf("executeStepWithContext(send_and_observe) error = %v", err)
	}
	if !strings.Contains(result, "会话 sess-observe-1 状态") {
		t.Fatalf("expected formatted session output, got %s", result)
	}
	if !strings.Contains(result, "等待用户输入") && !strings.Contains(result, "done") {
		t.Fatalf("expected observed progress in result, got %s", result)
	}
}

func TestSkillRunnerExecuteStepWithContext_SendAndObserveUsesImplicitSessionID(t *testing.T) {
	session := &RemoteSession{
		ID:        "sess-observe-implicit-1",
		Tool:      "claude-code",
		Title:     "observe-session-implicit",
		Status:    SessionWaitingInput,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Exec:      newFakeExecutionHandle(502),
		RawOutputLines: []string{
			"done",
		},
		Summary: SessionSummary{WaitingForUser: true},
	}
	app := &App{}
	mgr := &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{"sess-observe-implicit-1": session}}
	runner := NewSkillRunner(&SkillExecutor{app: app, manager: mgr})
	runner.runs["run-implicit"] = &skillRun{status: SkillRunStatus{
		RunID:  "run-implicit",
		Skill:  "demo",
		Status: "running",
		Session: &SkillRunSessionMeta{
			SessionID: "sess-observe-implicit-1",
		},
	}}

	result, err := runner.executeStepWithContext(context.Background(), "run-implicit", NLSkillStep{
		Action: "send_and_observe",
		Params: map[string]interface{}{
			"text": "continue",
		},
	}, "")
	if err != nil {
		t.Fatalf("executeStepWithContext(send_and_observe implicit) error = %v", err)
	}
	if !strings.Contains(result, "会话 sess-observe-implicit-1 状态") {
		t.Fatalf("expected implicit session reuse, got %s", result)
	}
}

func TestSkillRunnerExecuteStepWithContext_SendAndObserveFallsBackFromUnknownExplicitSessionID(t *testing.T) {
	session := &RemoteSession{
		ID:        "sess-observe-fallback-1",
		Tool:      "claude-code",
		Title:     "observe-session-fallback",
		Status:    SessionWaitingInput,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Exec:      newFakeExecutionHandle(503),
		RawOutputLines: []string{
			"done",
		},
		Summary: SessionSummary{WaitingForUser: true},
	}
	app := &App{}
	mgr := &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{"sess-observe-fallback-1": session}}
	runner := NewSkillRunner(&SkillExecutor{app: app, manager: mgr})
	runner.runs["run-fallback"] = &skillRun{status: SkillRunStatus{
		RunID:  "run-fallback",
		Skill:  "demo",
		Status: "running",
		Session: &SkillRunSessionMeta{
			SessionID: "sess-observe-fallback-1",
		},
	}}

	result, err := runner.executeStepWithContext(context.Background(), "run-fallback", NLSkillStep{
		Action: "send_and_observe",
		Params: map[string]interface{}{
			"session_id": "skill-runner",
			"text":       "continue",
		},
	}, "")
	if err != nil {
		t.Fatalf("executeStepWithContext(send_and_observe fallback) error = %v", err)
	}
	if !strings.Contains(result, "会话 sess-observe-fallback-1 状态") {
		t.Fatalf("expected fallback session reuse, got %s", result)
	}
}

func TestSkillExecutorExecute_ImplicitSessionReuse(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEnabled = true
	cfg.Projects = []ProjectConfig{{Id: "proj-implicit", Name: "Implicit", Path: tempHome}}
	cfg.CurrentProject = "proj-implicit"
	cfg.Claude = ToolConfig{
		CurrentModel: "Original",
		Models:       []ModelConfig{{ModelName: "Original", ModelId: "claude-sonnet", IsBuiltin: true}},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	provider := &fakeProviderAdapter{cmd: CommandSpec{Command: "claude.exe"}}
	mgr := NewRemoteSessionManager(app)
	mgr.providerFactory = func(tool string) (ProviderAdapter, error) {
		return provider, nil
	}
	mgr.executionFactory = func(spec LaunchSpec) (ExecutionStrategy, error) {
		return &fakeExecutionStrategy{handle: newFakeExecutionHandle(504)}, nil
	}
	app.remoteSessions = mgr
	app.sessionStarter = NewCodingSessionStarter(app)
	exec := NewSkillExecutor(app, nil, mgr)

	output, err := exec.executeSkillSteps(&NLSkillEntry{
		Name:        "seq-skill",
		Description: "修复 Go 项目中的 bug 并继续完成之前的任务",
		Steps: []NLSkillStep{
			{
				Action: "create_session",
				Params: map[string]interface{}{
					"tool":       "claude",
					"project_id": "proj-implicit",
					"task":       "修复 bug 并修改代码",
				},
			},
			{
				Action: "send_and_observe",
				Params: map[string]interface{}{
					"text": "continue implicit",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("executeSkillSteps() error = %v", err)
	}
	if !strings.Contains(output, "会话已创建: ID=") {
		t.Fatalf("expected create_session output, got %s", output)
	}
	if !strings.Contains(output, "会话 ") {
		t.Fatalf("expected send_and_observe output, got %s", output)
	}
}
