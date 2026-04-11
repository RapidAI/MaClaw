package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestGetRemoteClaudeReadinessDelegatesToDiagnosticCheck(t *testing.T) {
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
	cfg.Claude.CurrentModel = "Original"
	cfg.Claude.Models = []ModelConfig{{ModelName: "Original", IsBuiltin: true}}
	cfg.Projects = []ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"
	cfg.RemoteHubURL = "https://hub.example.com"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	oldProbe := remotePTYCapabilityProbe
	remotePTYCapabilityProbe = func() (bool, string) { return true, "ok" }
	defer func() { remotePTYCapabilityProbe = oldProbe }()

	got := app.GetRemoteClaudeReadiness(projectDir, false)
	if !got.Ready {
		t.Fatalf("GetRemoteClaudeReadiness() ready = false, issues = %#v", got.Issues)
	}
}

func TestGetRemoteConnectionStatusBootstrapsDefaultConfig(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	status := app.GetRemoteConnectionStatus()
	if status.LastError != "" {
		t.Fatalf("GetRemoteConnectionStatus() LastError = %q, want empty", status.LastError)
	}
	if status.HubURL != "" {
		t.Fatalf("GetRemoteConnectionStatus() HubURL = %q, want empty", status.HubURL)
	}
}

func TestListRemoteToolMetadataReturnsKnownTools(t *testing.T) {
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

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.ShowKilo = false
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	tools := app.ListRemoteToolMetadata()
	if len(tools) < 6 {
		t.Fatalf("tool count = %d, want at least 6", len(tools))
	}
	if tools[0].Name != "claude" {
		t.Fatalf("first tool = %q, want claude", tools[0].Name)
	}
	if !tools[0].Installed || !tools[0].CanStart {
		t.Fatalf("claude metadata = %#v, want installed and can_start", tools[0])
	}
	foundKilo := false
	for _, tool := range tools {
		if tool.Name == "kilo" {
			foundKilo = true
			if tool.Visible {
				t.Fatal("expected kilo to be hidden when show_kilo=false")
			}
			if tool.CanStart {
				t.Fatal("expected kilo can_start=false when hidden")
			}
		}
	}
	if !foundKilo {
		t.Fatal("expected kilo metadata to be present")
	}
}

func TestGetRemotePTYProbeDelegatesToDiagnosticCheck(t *testing.T) {
	app := &App{}

	oldCapability := remotePTYCapabilityProbe
	oldInteractive := remotePTYInteractiveProbe
	remotePTYCapabilityProbe = func() (bool, string) { return true, "supported" }
	remotePTYInteractiveProbe = func() (bool, string) { return false, "interactive failed" }
	defer func() {
		remotePTYCapabilityProbe = oldCapability
		remotePTYInteractiveProbe = oldInteractive
	}()

	got := app.GetRemotePTYProbe()
	if !got.Supported {
		t.Fatalf("Supported = false, want true")
	}
	if got.Ready {
		t.Fatalf("Ready = true, want false")
	}
	if got.Message != "interactive failed" {
		t.Fatalf("Message = %q, want %q", got.Message, "interactive failed")
	}
}

func TestGetRemoteClaudeLaunchProbeDelegatesToDiagnosticCheck(t *testing.T) {
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
	cfg.Claude.CurrentModel = "Original"
	cfg.Claude.Models = []ModelConfig{{ModelName: "Original", IsBuiltin: true}}
	cfg.Projects = []ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	oldCapability := remotePTYCapabilityProbe
	oldLaunch := remoteClaudeLaunchProbe
	remotePTYCapabilityProbe = func() (bool, string) { return true, "supported" }
	remoteClaudeLaunchProbe = func(cmd CommandSpec) (bool, string) {
		return true, "launch ok"
	}
	defer func() {
		remotePTYCapabilityProbe = oldCapability
		remoteClaudeLaunchProbe = oldLaunch
	}()

	got := app.GetRemoteClaudeLaunchProbe(projectDir, false)
	if !got.Supported {
		t.Fatalf("Supported = false, want true")
	}
	if !got.Ready {
		t.Fatalf("Ready = false, want true")
	}
	if got.Message != "launch ok" {
		t.Fatalf("Message = %q, want %q", got.Message, "launch ok")
	}
}

func TestGetRemoteToolReadinessDelegatesToDiagnosticCheck(t *testing.T) {
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
		codexExe = "codex.exe"
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
	cfg.Codex.CurrentModel = "Original"
	cfg.Codex.Models = []ModelConfig{{ModelName: "Original", IsBuiltin: true}}
	cfg.Projects = []ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"
	cfg.RemoteHubURL = "https://hub.example.com"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	oldProbe := remotePTYCapabilityProbe
	remotePTYCapabilityProbe = func() (bool, string) { return true, "ok" }
	defer func() { remotePTYCapabilityProbe = oldProbe }()

	got := app.GetRemoteToolReadiness("codex", projectDir, false)
	if got.Tool != "codex" {
		t.Fatalf("Tool = %q, want codex", got.Tool)
	}
	if !got.Ready {
		t.Fatalf("GetRemoteToolReadiness() ready = false, issues = %#v", got.Issues)
	}
}

func TestGetRemoteToolLaunchProbeDelegatesToDiagnosticCheck(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	toolsDir := filepath.Join(tempHome, ".maclaw", "data", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(toolsDir) error = %v", err)
	}

	kiloExe := "kilo"
	if runtime.GOOS == "windows" {
		kiloExe = "kilo.exe"
	}
	if err := os.WriteFile(filepath.Join(toolsDir, kiloExe), []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile(kilo) error = %v", err)
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
	cfg.Kilo.CurrentModel = "Original"
	cfg.Kilo.Models = []ModelConfig{{ModelName: "Original", IsBuiltin: true}}
	cfg.Projects = []ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	oldCapability := remotePTYCapabilityProbe
	oldLaunch := remoteClaudeLaunchProbe
	remotePTYCapabilityProbe = func() (bool, string) { return true, "supported" }
	remoteClaudeLaunchProbe = func(cmd CommandSpec) (bool, string) {
		return true, "launch ok"
	}
	defer func() {
		remotePTYCapabilityProbe = oldCapability
		remoteClaudeLaunchProbe = oldLaunch
	}()

	got := app.GetRemoteToolLaunchProbe("kilo", projectDir, false)
	if got.Tool != "kilo" {
		t.Fatalf("Tool = %q, want kilo", got.Tool)
	}
	if !got.Supported || !got.Ready {
		t.Fatalf("probe = %#v, want supported and ready", got)
	}
}

func TestListRemoteSessionsIncludesAIAgentLoopSession(t *testing.T) {
	app := &App{}
	now := time.Now()
	app.remoteSessions = NewRemoteSessionManager(app)
	loopCtx := NewLoopContext("ai-bg", 4, nil)
	app.remoteSessions.sessions["ai-bg-1"] = &RemoteSession{
		ID:           "ai-bg-1",
		Tool:         "ai-assistant",
		Title:        "Background task",
		LaunchSource: RemoteLaunchSourceAI,
		ProjectPath:  `D:\workprj\demo`,
		ModelID:      "maclaw-ai-assistant",
		Status:       SessionBusy,
		CreatedAt:    now,
		UpdatedAt:    now,
		AgentLoop:    loopCtx,
		Summary:      SessionSummary{SessionID: "ai-bg-1", Source: string(RemoteLaunchSourceAI), Status: string(SessionBusy)},
		Preview:      SessionPreview{SessionID: "ai-bg-1"},
	}

	sessions := app.ListRemoteSessions()
	if len(sessions) != 1 {
		t.Fatalf("session count = %d, want 1", len(sessions))
	}
	if sessions[0].LaunchSource != string(RemoteLaunchSourceAI) {
		t.Fatalf("launch source = %q, want %q", sessions[0].LaunchSource, RemoteLaunchSourceAI)
	}
	if sessions[0].Status != SessionBusy {
		t.Fatalf("status = %q, want %q", sessions[0].Status, SessionBusy)
	}
}

func TestListRemoteSessionsIncludesWorkspaceFields(t *testing.T) {
	app := &App{}
	now := time.Now()
	app.remoteSessions = NewRemoteSessionManager(app)
	app.remoteSessions.sessions["sess-1"] = &RemoteSession{
		ID:             "sess-1",
		Tool:           "claude",
		Title:          "demo",
		ProjectPath:    `D:\workprj\demo`,
		WorkspacePath:  `D:\tmp\maclaw-worktrees\sess-1`,
		WorkspaceRoot:  `D:\tmp\maclaw-worktrees\sess-1`,
		WorkspaceMode:  WorkspaceModeGitWorktree,
		WorkspaceIsGit: true,
		ModelID:        "m1",
		Status:         SessionBusy,
		PID:            123,
		CreatedAt:      now,
		UpdatedAt:      now,
		Summary:        SessionSummary{SessionID: "sess-1"},
		Preview:        SessionPreview{SessionID: "sess-1"},
		Events:         []ImportantEvent{{Type: "session.init", Summary: "Session started", Count: 1}, {Type: "file.change", Summary: "Updated 3 files", Count: 3, Grouped: true}},
	}

	sessions := app.ListRemoteSessions()
	if len(sessions) != 1 {
		t.Fatalf("session count = %d, want 1", len(sessions))
	}
	if sessions[0].WorkspacePath != `D:\tmp\maclaw-worktrees\sess-1` {
		t.Fatalf("workspace path = %q", sessions[0].WorkspacePath)
	}
	if sessions[0].WorkspaceMode != WorkspaceModeGitWorktree {
		t.Fatalf("workspace mode = %q, want %q", sessions[0].WorkspaceMode, WorkspaceModeGitWorktree)
	}
	if !sessions[0].WorkspaceIsGit {
		t.Fatal("expected workspace_is_git to be true")
	}
	if len(sessions[0].Events) != 2 || sessions[0].Events[0].Type != "session.init" {
		t.Fatalf("events = %#v, want preserved important events", sessions[0].Events)
	}
	if sessions[0].Events[1].Count != 3 || !sessions[0].Events[1].Grouped {
		t.Fatalf("grouped event = %#v, want count=3 grouped=true", sessions[0].Events[1])
	}
}

func TestListRemoteSessionsIncludesBrowserSessions(t *testing.T) {
	app := &App{}
	app.ensureAITrace()
	app.browserSessions = NewBrowserAgentManager(app)
	app.browserSessions.mapViews["browser-1"] = &BrowserSessionView{
		ID:             "browser-1",
		Tool:           "browser",
		Title:          "Example",
		Status:         SessionRunning,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		RunID:          "run-browser-1",
		CurrentURL:     "https://example.com",
		CurrentTitle:   "Example Domain",
		ReadyState:     "complete",
		LastSnapshotID: "snap-1",
		Summary:        SessionSummary{SessionID: "browser-1", Tool: "browser", Status: string(SessionRunning), ProgressSummary: "Browser session active"},
		Preview:        SessionPreview{SessionID: "browser-1", PreviewLines: []string{"Example Domain", "https://example.com"}},
		Events:         []ImportantEvent{{Type: "browser.observe", Summary: "Observed page", CreatedAt: time.Now().UnixMilli()}},
		RawOutputLines: []string{"Observed page", "https://example.com"},
		OutputImages:   []SessionOutputImage{{ImageID: "snap-1", MediaType: "image/png", Data: "abc", AfterLineIdx: 1}},
	}

	sessions := app.ListRemoteSessions()
	if len(sessions) != 1 {
		t.Fatalf("session count = %d, want 1", len(sessions))
	}
	if sessions[0].LaunchSource != "browser" {
		t.Fatalf("launch source = %q, want browser", sessions[0].LaunchSource)
	}
	if sessions[0].RunID != "run-browser-1" {
		t.Fatalf("run id = %q", sessions[0].RunID)
	}
	if sessions[0].CurrentURL != "https://example.com" {
		t.Fatalf("current url = %q", sessions[0].CurrentURL)
	}
	if sessions[0].LastSnapshotID != "snap-1" {
		t.Fatalf("last snapshot id = %q", sessions[0].LastSnapshotID)
	}
}

func TestBuildRemoteLaunchSpecSupportsCodex(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	projectDir := filepath.Join(tempHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectDir) error = %v", err)
	}

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.Codex.CurrentModel = "Original"
	cfg.Codex.Models = []ModelConfig{{ModelName: "Original", ModelId: "gpt-5.2-codex", IsBuiltin: true}}
	cfg.Projects = []ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"

	spec, err := app.buildRemoteLaunchSpec("codex", cfg, false, false, "", projectDir, false, "")
	if err != nil {
		t.Fatalf("buildRemoteLaunchSpec(codex) error = %v", err)
	}
	if spec.Tool != "codex" {
		t.Fatalf("spec.Tool = %q, want %q", spec.Tool, "codex")
	}
	if spec.BinaryName != "codex" {
		t.Fatalf("spec.BinaryName = %q, want %q", spec.BinaryName, "codex")
	}
	// Original mode: env vars should NOT be injected; Codex uses its own auth.
	if spec.Env["OPENAI_MODEL"] != "" {
		t.Fatalf("OPENAI_MODEL = %q, want empty (original mode)", spec.Env["OPENAI_MODEL"])
	}
	// ModelID should still be available in the spec for metadata.
	if spec.ModelID != "gpt-5.2-codex" {
		t.Fatalf("spec.ModelID = %q, want %q", spec.ModelID, "gpt-5.2-codex")
	}
}

func TestStartRemoteSessionSupportsCodex(t *testing.T) {
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
		codexExe = "codex.exe"
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
	cfg.Codex.CurrentModel = "Original"
	cfg.Codex.Models = []ModelConfig{{ModelName: "Original", ModelId: "gpt-5.2-codex", IsBuiltin: true}}
	cfg.Projects = []ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.remoteSessions = NewRemoteSessionManager(app)
	app.remoteSessions.executionFactory = func(spec LaunchSpec) (ExecutionStrategy, error) {
		return &fakeExecutionStrategy{handle: newFakeExecutionHandle(99)}, nil
	}

	session, err := app.StartRemoteSession("codex", projectDir, false, "", RemoteLaunchSourceDesktop)
	if err != nil {
		t.Fatalf("StartRemoteSession(codex) error = %v", err)
	}
	if session.Tool != "codex" {
		t.Fatalf("session.Tool = %q, want %q", session.Tool, "codex")
	}
	if session.ModelID != "gpt-5.2-codex" {
		t.Fatalf("session.ModelID = %q, want %q", session.ModelID, "gpt-5.2-codex")
	}
	if !app.IsAIAssistantReady() {
		t.Fatalf("expected AI assistant to be ready after desktop remote start, status=%q", app.GetAIAssistantInitStatus())
	}
}

func TestStartRemoteHandoffSessionInitializesAIAssistantWhenCreatingHubClient(t *testing.T) {
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
		codexExe = "codex.exe"
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
	cfg.Codex.CurrentModel = "Original"
	cfg.Codex.Models = []ModelConfig{{ModelName: "Original", ModelId: "gpt-5.2-codex", IsBuiltin: true}}
	cfg.Projects = []ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.remoteSessions = NewRemoteSessionManager(app)
	app.remoteSessions.executionFactory = func(spec LaunchSpec) (ExecutionStrategy, error) {
		return &fakeExecutionStrategy{handle: newFakeExecutionHandle(103)}, nil
	}

	session, err := app.StartRemoteHandoffSession("codex", projectDir, false, "", RemoteLaunchSourceDesktop)
	if err != nil {
		t.Fatalf("StartRemoteHandoffSession(codex) error = %v", err)
	}
	if session.Tool != "codex" {
		t.Fatalf("session.Tool = %q, want %q", session.Tool, "codex")
	}
	if !app.IsAIAssistantReady() {
		t.Fatalf("expected AI assistant to be ready after handoff start, status=%q", app.GetAIAssistantInitStatus())
	}
}

func TestBuildRemoteLaunchSpecSupportsOpencode(t *testing.T) {
	tempHome := t.TempDir()
	projectDir := filepath.Join(tempHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectDir) error = %v", err)
	}

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.Opencode.CurrentModel = "Original"
	cfg.Opencode.Models = []ModelConfig{{ModelName: "Original", ModelId: "opencode-1.0", IsBuiltin: true}}
	cfg.Projects = []ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"

	spec, err := app.buildRemoteLaunchSpec("opencode", cfg, false, false, "", projectDir, false, "")
	if err != nil {
		t.Fatalf("buildRemoteLaunchSpec(opencode) error = %v", err)
	}
	if spec.Tool != "opencode" {
		t.Fatalf("spec.Tool = %q, want %q", spec.Tool, "opencode")
	}
	if spec.BinaryName != "opencode" {
		t.Fatalf("spec.BinaryName = %q, want %q", spec.BinaryName, "opencode")
	}
	if spec.Env["OPENCODE_MODEL"] != "opencode-1.0" {
		t.Fatalf("OPENCODE_MODEL = %q, want %q", spec.Env["OPENCODE_MODEL"], "opencode-1.0")
	}
}

func TestStartRemoteSessionSupportsOpencode(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	toolsDir := filepath.Join(tempHome, ".maclaw", "data", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(toolsDir) error = %v", err)
	}

	opencodeExe := "opencode"
	if runtime.GOOS == "windows" {
		opencodeExe = "opencode.exe"
	}
	if err := os.WriteFile(filepath.Join(toolsDir, opencodeExe), []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile(opencode) error = %v", err)
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
	cfg.Opencode.CurrentModel = "Original"
	cfg.Opencode.Models = []ModelConfig{{ModelName: "Original", ModelId: "opencode-1.0", IsBuiltin: true}}
	cfg.Projects = []ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.remoteSessions = NewRemoteSessionManager(app)
	app.remoteSessions.executionFactory = func(spec LaunchSpec) (ExecutionStrategy, error) {
		return &fakeExecutionStrategy{handle: newFakeExecutionHandle(100)}, nil
	}

	session, err := app.StartRemoteSession("opencode", projectDir, false, "", RemoteLaunchSourceDesktop)
	if err != nil {
		t.Fatalf("StartRemoteSession(opencode) error = %v", err)
	}
	if session.Tool != "opencode" {
		t.Fatalf("session.Tool = %q, want %q", session.Tool, "opencode")
	}
	if session.ModelID != "opencode-1.0" {
		t.Fatalf("session.ModelID = %q, want %q", session.ModelID, "opencode-1.0")
	}
}

func TestBuildRemoteLaunchSpecSupportsIFlow(t *testing.T) {
	tempHome := t.TempDir()
	projectDir := filepath.Join(tempHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectDir) error = %v", err)
	}

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.IFlow.CurrentModel = "Original"
	cfg.IFlow.Models = []ModelConfig{{ModelName: "Original", ModelId: "gpt-4o", IsBuiltin: true}}
	cfg.Projects = []ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"

	spec, err := app.buildRemoteLaunchSpec("iflow", cfg, false, false, "", projectDir, false, "")
	if err != nil {
		t.Fatalf("buildRemoteLaunchSpec(iflow) error = %v", err)
	}
	if spec.Tool != "iflow" {
		t.Fatalf("spec.Tool = %q, want %q", spec.Tool, "iflow")
	}
	if spec.BinaryName != "iflow" {
		t.Fatalf("spec.BinaryName = %q, want %q", spec.BinaryName, "iflow")
	}
	if spec.Env["IFLOW_MODEL"] != "gpt-4o" {
		t.Fatalf("IFLOW_MODEL = %q, want %q", spec.Env["IFLOW_MODEL"], "gpt-4o")
	}
}

func TestStartRemoteSessionSupportsIFlow(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	toolsDir := filepath.Join(tempHome, ".maclaw", "data", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(toolsDir) error = %v", err)
	}

	iflowExe := "iflow"
	if runtime.GOOS == "windows" {
		iflowExe = "iflow.exe"
	}
	if err := os.WriteFile(filepath.Join(toolsDir, iflowExe), []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile(iflow) error = %v", err)
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
	cfg.IFlow.CurrentModel = "Original"
	cfg.IFlow.Models = []ModelConfig{{ModelName: "Original", ModelId: "gpt-4o", IsBuiltin: true}}
	cfg.Projects = []ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.remoteSessions = NewRemoteSessionManager(app)
	app.remoteSessions.executionFactory = func(spec LaunchSpec) (ExecutionStrategy, error) {
		return &fakeExecutionStrategy{handle: newFakeExecutionHandle(101)}, nil
	}

	session, err := app.StartRemoteSession("iflow", projectDir, false, "", RemoteLaunchSourceDesktop)
	if err != nil {
		t.Fatalf("StartRemoteSession(iflow) error = %v", err)
	}
	if session.Tool != "iflow" {
		t.Fatalf("session.Tool = %q, want %q", session.Tool, "iflow")
	}
	if session.ModelID != "gpt-4o" {
		t.Fatalf("session.ModelID = %q, want %q", session.ModelID, "gpt-4o")
	}
}

func TestBuildRemoteLaunchSpecSupportsKilo(t *testing.T) {
	tempHome := t.TempDir()
	projectDir := filepath.Join(tempHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectDir) error = %v", err)
	}

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.Kilo.CurrentModel = "Original"
	cfg.Kilo.Models = []ModelConfig{{ModelName: "Original", ModelId: "gpt-4o", IsBuiltin: true}}
	cfg.Projects = []ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"

	spec, err := app.buildRemoteLaunchSpec("kilo", cfg, false, false, "", projectDir, false, "")
	if err != nil {
		t.Fatalf("buildRemoteLaunchSpec(kilo) error = %v", err)
	}
	if spec.Tool != "kilo" {
		t.Fatalf("spec.Tool = %q, want %q", spec.Tool, "kilo")
	}
	if spec.BinaryName != "kilo" {
		t.Fatalf("spec.BinaryName = %q, want %q", spec.BinaryName, "kilo")
	}
	if spec.Env["KILO_MODEL"] != "gpt-4o" {
		t.Fatalf("KILO_MODEL = %q, want %q", spec.Env["KILO_MODEL"], "gpt-4o")
	}
}

func TestStartRemoteSessionSupportsKilo(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	toolsDir := filepath.Join(tempHome, ".maclaw", "data", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(toolsDir) error = %v", err)
	}

	kiloExe := "kilo"
	if runtime.GOOS == "windows" {
		kiloExe = "kilo.exe"
	}
	if err := os.WriteFile(filepath.Join(toolsDir, kiloExe), []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile(kilo) error = %v", err)
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
	cfg.Kilo.CurrentModel = "Original"
	cfg.Kilo.Models = []ModelConfig{{ModelName: "Original", ModelId: "gpt-4o", IsBuiltin: true}}
	cfg.Projects = []ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.remoteSessions = NewRemoteSessionManager(app)
	app.remoteSessions.executionFactory = func(spec LaunchSpec) (ExecutionStrategy, error) {
		return &fakeExecutionStrategy{handle: newFakeExecutionHandle(102)}, nil
	}

	session, err := app.StartRemoteSession("kilo", projectDir, false, "", RemoteLaunchSourceDesktop)
	if err != nil {
		t.Fatalf("StartRemoteSession(kilo) error = %v", err)
	}
	if session.Tool != "kilo" {
		t.Fatalf("session.Tool = %q, want %q", session.Tool, "kilo")
	}
}

func TestToRemoteSessionViewSanitizesPreviewAndEvents(t *testing.T) {
	session := &RemoteSession{
		ID:          "sess_1",
		Tool:        "claude",
		Title:       "demo",
		ProjectPath: `D:\workprj\aicoder`,
		ModelID:     "model",
		Status:      SessionBusy,
		PID:         123,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Summary: SessionSummary{
			SessionID:       "sess_1",
			MachineID:       "machine_1",
			Tool:            "claude",
			Title:           "demo\x00title",
			Status:          "busy",
			Severity:        "info",
			CurrentTask:     "Running\x00 task",
			ProgressSummary: "Line one\r\nLine two",
			LastResult:      "bad" + string([]byte{0xff}) + "result",
			SuggestedAction: "Continue\tplease",
			ImportantFiles:  []string{"a.go\x00", "b.go"},
			LastCommand:     "go test\r\n./...",
			UpdatedAt:       time.Now().Unix(),
		},
		Preview: SessionPreview{
			SessionID:    "sess_1",
			OutputSeq:    2,
			PreviewLines: []string{"bad\x00line", "bad" + string([]byte{0xff}) + "utf8"},
			UpdatedAt:    time.Now().Unix(),
		},
		Events: []ImportantEvent{
			{
				EventID:     "evt_1",
				SessionID:   "sess_1",
				MachineID:   "machine_1",
				Type:        "session.error",
				Severity:    "error",
				Title:       "Oops\x00",
				Summary:     "need\r\ncleanup",
				RelatedFile: "x.go\x00",
				Command:     "cmd\tgo",
				CreatedAt:   time.Now().Unix(),
			},
		},
	}

	view := toRemoteSessionView(session)

	for _, line := range view.Preview.PreviewLines {
		if strings.ContainsRune(line, '\x00') {
			t.Fatalf("preview line contains NUL after sanitize: %q", line)
		}
	}

	if strings.Contains(view.Summary.ProgressSummary, "\n") || strings.Contains(view.Summary.ProgressSummary, "\r") {
		t.Fatalf("summary progress still contains newline: %q", view.Summary.ProgressSummary)
	}
	if strings.ContainsRune(view.Summary.Title, '\x00') {
		t.Fatalf("summary title contains NUL after sanitize: %q", view.Summary.Title)
	}
	if strings.ContainsRune(view.Events[0].Title, '\x00') {
		t.Fatalf("event title contains NUL after sanitize: %q", view.Events[0].Title)
	}
}

func TestToRemoteSessionViewIncludesPendingQuestion(t *testing.T) {
	session := &RemoteSession{
		ID:          "sess_pending",
		Tool:        "claude",
		Title:       "Pending question",
		ProjectPath: `D:\\workprj\\demo`,
		Status:      SessionWaitingInput,
		Summary: SessionSummary{
			SessionID:      "sess_pending",
			Status:         string(SessionWaitingInput),
			WaitingForUser: true,
			PendingQuestion: &PendingQuestionView{
				ToolUseID: "call_q1",
				ToolName:  "AskUserQuestion",
				Question:  "Choose a template",
				Options:   []PendingQuestionOption{{Label: "Python"}, {Label: "TypeScript"}},
			},
		},
	}

	view := toRemoteSessionView(session)
	if view.Summary.PendingQuestion == nil {
		t.Fatal("expected pending question to be included in view")
	}
	if got := view.Summary.PendingQuestion.Question; got != "Choose a template" {
		t.Fatalf("pending question = %q, want %q", got, "Choose a template")
	}
	if len(view.Summary.PendingQuestion.Options) != 2 {
		t.Fatalf("pending question options = %d, want 2", len(view.Summary.PendingQuestion.Options))
	}
}

func TestBuildRemoteLaunchSpecReturnsErrorWhenConfigSelectorMissing(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	projectDir := filepath.Join(tempHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectDir) error = %v", err)
	}

	const toolName = "broken-launch-spec"
	original, existed := remoteToolCatalog[toolName]
	remoteToolCatalog[toolName] = RemoteToolMetadata{
		Name:            toolName,
		DisplayName:     "Broken Launch Tool",
		BinaryName:      toolName,
		DefaultTitle:    "Broken Launch Tool Session",
		SupportsRemote:  true,
		SupportsProxy:   true,
		ConfigSelector:  nil,
		ProviderFactory: nil,
	}
	defer func() {
		if existed {
			remoteToolCatalog[toolName] = original
		} else {
			delete(remoteToolCatalog, toolName)
		}
	}()

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	_, err = app.buildRemoteLaunchSpec(toolName, cfg, false, false, "", projectDir, false, "")
	if err == nil {
		t.Fatal("expected error when ConfigSelector is missing")
	}
	if !strings.Contains(err.Error(), "tool config unavailable for tool") {
		t.Fatalf("error = %q, want tool config unavailable", err.Error())
	}
}

func TestBuildRemoteLaunchSpecProviderOverride(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	projectDir := filepath.Join(tempHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectDir) error = %v", err)
	}

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
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

	t.Run("empty override uses CurrentModel", func(t *testing.T) {
		spec, err := app.buildRemoteLaunchSpec("claude", cfg, false, false, "", projectDir, false, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if spec.ModelName != "Original" {
			t.Errorf("ModelName = %q, want %q", spec.ModelName, "Original")
		}
	})

	t.Run("valid override replaces default", func(t *testing.T) {
		spec, err := app.buildRemoteLaunchSpec("claude", cfg, false, false, "", projectDir, false, "DeepSeek")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if spec.ModelName != "DeepSeek" {
			t.Errorf("ModelName = %q, want %q", spec.ModelName, "DeepSeek")
		}
		if spec.ModelID != "deepseek-v3" {
			t.Errorf("ModelID = %q, want %q", spec.ModelID, "deepseek-v3")
		}
	})

	t.Run("invalid provider returns error", func(t *testing.T) {
		_, err := app.buildRemoteLaunchSpec("claude", cfg, false, false, "", projectDir, false, "EmptyKey")
		if err == nil {
			t.Fatal("expected error for invalid provider, got nil")
		}
		if !strings.Contains(err.Error(), "has no API key configured") {
			t.Errorf("error = %q, want it to contain 'has no API key configured'", err.Error())
		}
	})

	t.Run("nonexistent provider returns error", func(t *testing.T) {
		_, err := app.buildRemoteLaunchSpec("claude", cfg, false, false, "", projectDir, false, "NonExistent")
		if err == nil {
			t.Fatal("expected error for nonexistent provider, got nil")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error = %q, want it to contain 'not found'", err.Error())
		}
	})
}

func TestListValidProviders(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.Claude = ToolConfig{
		CurrentModel: "Original",
		Models: []ModelConfig{
			{ModelName: "Original", ModelId: "claude-sonnet", IsBuiltin: true},
			{ModelName: "DeepSeek", ModelId: "deepseek-v3", ApiKey: "sk-abc"},
			{ModelName: "EmptyKey", ModelId: "empty-model", ApiKey: ""},
		},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	providers, err := app.ListValidProviders("claude")
	if err != nil {
		t.Fatalf("ListValidProviders() error = %v", err)
	}

	// Should have at least Original and DeepSeek, not EmptyKey
	if len(providers) < 2 {
		t.Fatalf("got %d providers, want at least 2", len(providers))
	}

	foundOriginal := false
	foundDeepSeek := false
	for _, p := range providers {
		if p.Name == "Original" {
			foundOriginal = true
			if !p.IsDefault {
				t.Error("Original should be marked as default")
			}
		}
		if p.Name == "DeepSeek" {
			foundDeepSeek = true
			if p.IsDefault {
				t.Error("DeepSeek should not be marked as default")
			}
			if p.ModelID != "deepseek-v3" {
				t.Errorf("DeepSeek ModelID = %q, want %q", p.ModelID, "deepseek-v3")
			}
		}
		if p.Name == "EmptyKey" {
			t.Error("EmptyKey should not be in valid providers")
		}
	}
	if !foundOriginal {
		t.Error("Original not found in providers")
	}
	if !foundDeepSeek {
		t.Error("DeepSeek not found in providers")
	}
}
