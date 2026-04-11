package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStartRemoteSessionForProjectResumeSessionIDPassThrough(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	toolsDir := filepath.Join(tempHome, ".maclaw", "data", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(toolsDir) error = %v", err)
	}

	claudeExe := "claude"
	if runtime.GOOS == "windows" {
		claudeExe = "claude.exe"
	}
	if err := os.WriteFile(filepath.Join(toolsDir, claudeExe), []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile(claude) error = %v", err)
	}

	projectDir := filepath.Join(tempHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectDir) error = %v", err)
	}

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEnabled = true
	cfg.Claude = ToolConfig{
		CurrentModel: "Original",
		Models:       []ModelConfig{{ModelName: "Original", ModelId: "claude-sonnet", IsBuiltin: true}},
	}
	cfg.Projects = []ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	provider := &fakeProviderAdapter{cmd: CommandSpec{Command: "claude.exe"}}
	app.remoteSessions = NewRemoteSessionManager(app)
	app.remoteSessions.providerFactory = func(tool string) (ProviderAdapter, error) {
		return provider, nil
	}
	app.remoteSessions.executionFactory = func(spec LaunchSpec) (ExecutionStrategy, error) {
		return &fakeExecutionStrategy{handle: newFakeExecutionHandle(200)}, nil
	}

	_, err = app.StartRemoteSessionForProject(RemoteStartSessionRequest{
		Tool:            "claude",
		ProjectID:       "p1",
		ResumeSessionID: "resume-123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.lastSpec.ResumeSessionID != "resume-123" {
		t.Fatalf("ResumeSessionID = %q, want %q", provider.lastSpec.ResumeSessionID, "resume-123")
	}
	if !app.IsAIAssistantReady() {
		t.Fatalf("expected AI assistant to be ready after mobile remote start, status=%q", app.GetAIAssistantInitStatus())
	}
}

func TestStartRemoteSessionForProjectCodexResumeSessionIDPassThrough(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	toolsDir := filepath.Join(tempHome, ".maclaw", "data", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(toolsDir) error = %v", err)
	}

	codexExe := "codex"
	if runtime.GOOS == "windows" {
		codexExe = "codex.cmd"
	}
	if err := os.WriteFile(filepath.Join(toolsDir, codexExe), []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile(codex) error = %v", err)
	}

	projectDir := filepath.Join(tempHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectDir) error = %v", err)
	}

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEnabled = true
	cfg.Codex = ToolConfig{
		CurrentModel: "Original",
		Models:       []ModelConfig{{ModelName: "Original", ModelId: "gpt-5.2-codex", IsBuiltin: true}},
	}
	cfg.Projects = []ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	provider := &fakeProviderAdapter{cmd: CommandSpec{Command: "codex.exe"}}
	app.remoteSessions = NewRemoteSessionManager(app)
	app.remoteSessions.providerFactory = func(tool string) (ProviderAdapter, error) {
		return provider, nil
	}
	app.remoteSessions.executionFactory = func(spec LaunchSpec) (ExecutionStrategy, error) {
		return &fakeExecutionStrategy{handle: newFakeExecutionHandle(201)}, nil
	}

	_, err = app.StartRemoteSessionForProject(RemoteStartSessionRequest{
		Tool:            "codex",
		ProjectID:       "p1",
		ResumeSessionID: "thread-abc",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.lastSpec.ResumeSessionID != "thread-abc" {
		t.Fatalf("ResumeSessionID = %q, want %q", provider.lastSpec.ResumeSessionID, "thread-abc")
	}
	if provider.lastSpec.Tool != "codex" {
		t.Fatalf("Tool = %q, want %q", provider.lastSpec.Tool, "codex")
	}
}

func TestStartRemoteSessionForProjectCarriesInjectResumePrompt(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	toolsDir := filepath.Join(tempHome, ".maclaw", "data", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(toolsDir) error = %v", err)
	}

	claudeExe := "claude"
	if runtime.GOOS == "windows" {
		claudeExe = "claude.exe"
	}
	if err := os.WriteFile(filepath.Join(toolsDir, claudeExe), []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile(claude) error = %v", err)
	}

	projectDir := filepath.Join(tempHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectDir) error = %v", err)
	}

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEnabled = true
	cfg.Claude = ToolConfig{
		CurrentModel: "Original",
		Models:       []ModelConfig{{ModelName: "Original", ModelId: "claude-sonnet", IsBuiltin: true}},
	}
	cfg.Projects = []ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	provider := &fakeProviderAdapter{cmd: CommandSpec{Command: "claude.exe"}}
	app.remoteSessions = NewRemoteSessionManager(app)
	app.remoteSessions.providerFactory = func(tool string) (ProviderAdapter, error) {
		return provider, nil
	}
	app.remoteSessions.executionFactory = func(spec LaunchSpec) (ExecutionStrategy, error) {
		return &fakeExecutionStrategy{handle: newFakeExecutionHandle(202)}, nil
	}

	view, err := app.StartRemoteSessionForProject(RemoteStartSessionRequest{
		Tool:               "claude",
		ProjectID:          "p1",
		InjectResumePrompt: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !provider.lastSpec.InjectResumePrompt {
		t.Fatal("expected provider last spec to keep InjectResumePrompt")
	}
	session, ok := app.remoteSessions.Get(view.ID)
	if !ok || session == nil {
		t.Fatal("expected created session")
	}
	if !session.InjectResumePrompt {
		t.Fatal("expected created session to keep InjectResumePrompt")
	}
}

func TestStartRemoteSessionForProjectProviderField(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	toolsDir := filepath.Join(tempHome, ".maclaw", "data", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(toolsDir) error = %v", err)
	}

	claudeExe := "claude"
	if runtime.GOOS == "windows" {
		claudeExe = "claude.exe"
	}
	if err := os.WriteFile(filepath.Join(toolsDir, claudeExe), []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile(claude) error = %v", err)
	}

	projectDir := filepath.Join(tempHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectDir) error = %v", err)
	}

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEnabled = true
	cfg.Claude = ToolConfig{
		CurrentModel: "Original",
		Models: []ModelConfig{
			{ModelName: "Original", ModelId: "claude-sonnet", IsBuiltin: true},
			{ModelName: "DeepSeek", ModelId: "deepseek-v3", ApiKey: "sk-abc"},
			{ModelName: "EmptyKey", ModelId: "empty-model", ApiKey: ""},
		},
	}
	cfg.Projects = []ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	resetSessionManager := func() {
		app.remoteSessions = NewRemoteSessionManager(app)
		app.remoteSessions.executionFactory = func(spec LaunchSpec) (ExecutionStrategy, error) {
			return &fakeExecutionStrategy{handle: newFakeExecutionHandle(200)}, nil
		}
	}

	t.Run("empty provider uses default CurrentModel", func(t *testing.T) {
		resetSessionManager()
		view, err := app.StartRemoteSessionForProject(RemoteStartSessionRequest{
			Tool:      "claude",
			ProjectID: "p1",
			Provider:  "",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if view.Tool != "claude" {
			t.Errorf("Tool = %q, want %q", view.Tool, "claude")
		}
		if view.ModelID != "claude-sonnet" {
			t.Errorf("ModelID = %q, want %q", view.ModelID, "claude-sonnet")
		}
	})

	t.Run("valid provider overrides default", func(t *testing.T) {
		resetSessionManager()
		view, err := app.StartRemoteSessionForProject(RemoteStartSessionRequest{
			Tool:      "claude",
			ProjectID: "p1",
			Provider:  "DeepSeek",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if view.Tool != "claude" {
			t.Errorf("Tool = %q, want %q", view.Tool, "claude")
		}
		if view.ModelID != "deepseek-v3" {
			t.Errorf("ModelID = %q, want %q", view.ModelID, "deepseek-v3")
		}
	})

	t.Run("invalid provider returns error", func(t *testing.T) {
		resetSessionManager()
		_, err := app.StartRemoteSessionForProject(RemoteStartSessionRequest{
			Tool:      "claude",
			ProjectID: "p1",
			Provider:  "EmptyKey",
		})
		if err == nil {
			t.Fatal("expected error for invalid provider, got nil")
		}
		if !strings.Contains(err.Error(), "has no API key configured") {
			t.Errorf("error = %q, want it to contain 'has no API key configured'", err.Error())
		}
	})

	t.Run("nonexistent provider returns error", func(t *testing.T) {
		resetSessionManager()
		_, err := app.StartRemoteSessionForProject(RemoteStartSessionRequest{
			Tool:      "claude",
			ProjectID: "p1",
			Provider:  "NonExistent",
		})
		if err == nil {
			t.Fatal("expected error for nonexistent provider, got nil")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error = %q, want it to contain 'not found'", err.Error())
		}
	})
}
