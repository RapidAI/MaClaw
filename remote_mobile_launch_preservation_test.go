package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestPreservation_DesktopRemoteEnabledFalse_ReturnsDisabledError verifies that
// desktop-source requests (empty or "desktop" LaunchSource) with RemoteEnabled=false
// return "remote mode is disabled" for all three session creation functions.
// This captures the EXISTING behavior on UNFIXED code that must be preserved.
//
// **Validates: Requirements 3.1, 3.2, 3.4**
func TestPreservation_DesktopRemoteEnabledFalse_ReturnsDisabledError(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	toolsDir := filepath.Join(tempHome, ".cceasy", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(toolsDir) error = %v", err)
	}
	for _, bin := range []string{"claude", "codex", "opencode", "gemini"} {
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
			return &fakeExecutionStrategy{handle: newFakeExecutionHandle(400)}, nil
		}
	}

	// Property: For all desktop-source requests with RemoteEnabled=false,
	// StartRemoteSessionForProject SHALL return "remote mode is disabled".
	desktopSources := []RemoteLaunchSource{"", RemoteLaunchSourceDesktop}
	tools := []string{"claude", "codex", "opencode", "gemini"}

	for _, source := range desktopSources {
		for _, tool := range tools {
			label := "empty"
			if source != "" {
				label = string(source)
			}
			name := "StartRemoteSessionForProject/" + label + "/" + tool
			t.Run(name, func(t *testing.T) {
				resetSessionManager()
				_, err := app.StartRemoteSessionForProject(RemoteStartSessionRequest{
					Tool:         tool,
					ProjectID:    "p1",
					LaunchSource: source,
				})
				if err == nil || !strings.Contains(err.Error(), "remote mode is disabled") {
					t.Errorf("expected 'remote mode is disabled' error for desktop source %q, tool %q; got: %v",
						label, tool, err)
				}
			})
		}
	}

	// Property: For all requests with RemoteEnabled=false,
	// StartRemoteSession SHALL return "remote mode is disabled".
	// (On unfixed code, this function has no LaunchSource param and always checks RemoteEnabled.)
	for _, tool := range tools {
		name := "StartRemoteSession/" + tool
		t.Run(name, func(t *testing.T) {
			resetSessionManager()
			_, err := app.StartRemoteSession(tool, projectDir, false, "", RemoteLaunchSourceDesktop)
			if err == nil || !strings.Contains(err.Error(), "remote mode is disabled") {
				t.Errorf("expected 'remote mode is disabled' error for StartRemoteSession(tool=%q); got: %v",
					tool, err)
			}
		})
	}

	// Property: For all requests with RemoteEnabled=false,
	// StartRemoteHandoffSession SHALL return "remote mode is disabled".
	// (On unfixed code, this function has no LaunchSource param and always checks RemoteEnabled.)
	for _, tool := range tools {
		name := "StartRemoteHandoffSession/" + tool
		t.Run(name, func(t *testing.T) {
			resetSessionManager()
			_, err := app.StartRemoteHandoffSession(tool, projectDir, false, "", RemoteLaunchSourceDesktop)
			if err == nil || !strings.Contains(err.Error(), "remote mode is disabled") {
				t.Errorf("expected 'remote mode is disabled' error for StartRemoteHandoffSession(tool=%q); got: %v",
					tool, err)
			}
		})
	}
}

// TestPreservation_RemoteEnabledTrue_NoDisabledError verifies that when
// RemoteEnabled=true, none of the session creation functions return
// "remote mode is disabled" error. They may fail for other reasons
// (missing infra, etc.) but NOT due to the RemoteEnabled check.
//
// **Validates: Requirements 3.2, 3.4**
func TestPreservation_RemoteEnabledTrue_NoDisabledError(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	toolsDir := filepath.Join(tempHome, ".cceasy", "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(toolsDir) error = %v", err)
	}
	for _, bin := range []string{"claude", "codex", "opencode", "gemini"} {
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
	cfg.RemoteEnabled = true
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
			return &fakeExecutionStrategy{handle: newFakeExecutionHandle(401)}, nil
		}
	}

	// Property: For all requests (any source) with RemoteEnabled=true,
	// StartRemoteSessionForProject SHALL NOT return "remote mode is disabled".
	allSources := []RemoteLaunchSource{
		"",
		RemoteLaunchSourceDesktop,
		RemoteLaunchSourceMobile,
		RemoteLaunchSourceAI,
		RemoteLaunchSourceHandoff,
	}
	tools := []string{"claude", "codex", "opencode", "gemini"}

	for _, source := range allSources {
		for _, tool := range tools {
			label := "empty"
			if source != "" {
				label = string(source)
			}
			name := "StartRemoteSessionForProject/" + label + "/" + tool
			t.Run(name, func(t *testing.T) {
				resetSessionManager()
				_, err := app.StartRemoteSessionForProject(RemoteStartSessionRequest{
					Tool:         tool,
					ProjectID:    "p1",
					LaunchSource: source,
				})
				if err != nil && strings.Contains(err.Error(), "remote mode is disabled") {
					t.Errorf("RemoteEnabled=true should never return 'remote mode is disabled'; "+
						"source=%q, tool=%q, err=%v", label, tool, err)
				}
			})
		}
	}

	// Property: For all tools with RemoteEnabled=true,
	// StartRemoteSession SHALL NOT return "remote mode is disabled".
	for _, tool := range tools {
		name := "StartRemoteSession/" + tool
		t.Run(name, func(t *testing.T) {
			resetSessionManager()
			_, err := app.StartRemoteSession(tool, projectDir, false, "", RemoteLaunchSourceDesktop)
			if err != nil && strings.Contains(err.Error(), "remote mode is disabled") {
				t.Errorf("RemoteEnabled=true should never return 'remote mode is disabled'; "+
					"tool=%q, err=%v", tool, err)
			}
		})
	}

	// Property: For all tools with RemoteEnabled=true,
	// StartRemoteHandoffSession SHALL NOT return "remote mode is disabled".
	for _, tool := range tools {
		name := "StartRemoteHandoffSession/" + tool
		t.Run(name, func(t *testing.T) {
			resetSessionManager()
			_, err := app.StartRemoteHandoffSession(tool, projectDir, false, "", RemoteLaunchSourceDesktop)
			if err != nil && strings.Contains(err.Error(), "remote mode is disabled") {
				t.Errorf("RemoteEnabled=true should never return 'remote mode is disabled'; "+
					"tool=%q, err=%v", tool, err)
			}
		})
	}
}
