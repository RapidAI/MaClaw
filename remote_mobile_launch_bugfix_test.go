package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestBugCondition_NonDesktopSourceBlockedByRemoteEnabled demonstrates the bug:
// StartRemoteSessionForProject() unconditionally checks cfg.RemoteEnabled and
// returns "remote mode is disabled" for ALL launch sources, including non-desktop
// sources (mobile, ai, handoff) that should bypass this check.
//
// **Validates: Requirements 1.1, 1.2, 1.3, 1.4, 2.1**
//
// EXPECTED: This test FAILS on unfixed code, confirming the bug exists.
func TestBugCondition_NonDesktopSourceBlockedByRemoteEnabled(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	// Create tool stubs so remoteToolSupported() returns true
	toolsDir := filepath.Join(tempHome, ".cceasy", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(toolsDir) error = %v", err)
	}
	toolBinaries := []string{"claude", "codex", "opencode", "gemini"}
	for _, bin := range toolBinaries {
		name := bin
		if runtime.GOOS == "windows" {
			name = bin + ".exe"
		}
		if err := os.WriteFile(filepath.Join(toolsDir, name), []byte("stub"), 0o755); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
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
	// KEY: RemoteEnabled is false — this is the bug condition
	cfg.RemoteEnabled = false
	cfg.Claude = ToolConfig{
		CurrentModel: "Default",
		Models:       []ModelConfig{{ModelName: "Default", ModelId: "claude-sonnet", IsBuiltin: true}},
	}
	cfg.Codex = ToolConfig{
		CurrentModel: "Default",
		Models:       []ModelConfig{{ModelName: "Default", ModelId: "gpt-5.2-codex", IsBuiltin: true}},
	}
	cfg.Opencode = ToolConfig{
		CurrentModel: "Default",
		Models:       []ModelConfig{{ModelName: "Default", ModelId: "opencode-v1", IsBuiltin: true}},
	}
	cfg.Gemini = ToolConfig{
		CurrentModel: "Default",
		Models:       []ModelConfig{{ModelName: "Default", ModelId: "gemini-2.5-pro", IsBuiltin: true}},
	}
	cfg.Projects = []ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	resetSessionManager := func() {
		app.remoteSessions = NewRemoteSessionManager(app)
		app.remoteSessions.executionFactory = func(spec LaunchSpec) (ExecutionStrategy, error) {
			return &fakeExecutionStrategy{handle: newFakeExecutionHandle(300)}, nil
		}
	}

	// Non-desktop launch sources that should bypass RemoteEnabled check
	nonDesktopSources := []RemoteLaunchSource{
		RemoteLaunchSourceMobile,
		RemoteLaunchSourceAI,
		RemoteLaunchSourceHandoff,
	}

	// Supported coding tools
	tools := []string{"claude", "codex", "opencode", "gemini"}

	for _, source := range nonDesktopSources {
		for _, tool := range tools {
			name := string(source) + "/" + tool
			t.Run(name, func(t *testing.T) {
				resetSessionManager()
				_, err := app.StartRemoteSessionForProject(RemoteStartSessionRequest{
					Tool:         tool,
					ProjectID:    "p1",
					LaunchSource: source,
				})
				// Expected behavior: non-desktop sources should NOT get
				// "remote mode is disabled" error even when RemoteEnabled=false.
				// On UNFIXED code this will fail because the check is unconditional.
				if err != nil && strings.Contains(err.Error(), "remote mode is disabled") {
					t.Errorf("StartRemoteSessionForProject(tool=%q, source=%q) returned "+
						"'remote mode is disabled' — non-desktop sources should bypass "+
						"RemoteEnabled check. Got error: %v", tool, source, err)
				}
			})
		}
	}
}
