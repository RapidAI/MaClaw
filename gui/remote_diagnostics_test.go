package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestCheckRemoteClaudeReadinessReadyState(t *testing.T) {
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
	claudePath := filepath.Join(toolsDir, claudeExe)
	if err := os.WriteFile(claudePath, []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile(claudePath) error = %v", err)
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
	cfg.RemoteHubURL = "https://hub.example.com"
	cfg.RemoteMachineID = "m-1"
	cfg.RemoteMachineToken = "token-1"
	cfg.CurrentProject = "p1"
	cfg.Projects = []corelib.ProjectConfig{{
		Id:   "p1",
		Name: "Project",
		Path: projectDir,
	}}
	cfg.Claude.CurrentModel = "Original"
	cfg.Claude.Models = []corelib.ModelConfig{{
		ModelName: "Original",
		ModelId:   "",
		IsBuiltin: true,
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	oldProbe := remotePTYCapabilityProbe
	remotePTYCapabilityProbe = func() (bool, string) { return true, "ok" }
	defer func() { remotePTYCapabilityProbe = oldProbe }()

	readiness := app.CheckRemoteClaudeReadiness(projectDir, false)
	if !readiness.Ready {
		t.Fatalf("CheckRemoteClaudeReadiness() ready = false, issues = %#v", readiness.Issues)
	}
	if !readiness.ToolInstalled {
		t.Fatalf("ToolInstalled = false, want true")
	}
	if !readiness.ModelConfigured {
		t.Fatalf("ModelConfigured = false, want true")
	}
	if readiness.CommandPath == "" {
		t.Fatalf("CommandPath is empty")
	}
}

func TestCheckRemoteClaudeReadinessMissingTool(t *testing.T) {
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
	cfg.Claude.CurrentModel = "Original"
	cfg.Claude.Models = []corelib.ModelConfig{{ModelName: "Original", IsBuiltin: true}}
	cfg.Projects = []corelib.ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	oldProbe := remotePTYCapabilityProbe
	remotePTYCapabilityProbe = func() (bool, string) { return true, "ok" }
	defer func() { remotePTYCapabilityProbe = oldProbe }()

	readiness := app.CheckRemoteClaudeReadiness(projectDir, false)
	if readiness.Ready {
		t.Fatalf("CheckRemoteClaudeReadiness() ready = true, want false")
	}
	if readiness.ToolInstalled {
		t.Fatalf("ToolInstalled = true, want false")
	}
	if len(readiness.Issues) == 0 {
		t.Fatalf("Issues is empty, want missing tool issue")
	}
}

func TestCheckRemoteClaudeReadinessMissingModel(t *testing.T) {
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
	cfg.Claude.CurrentModel = ""
	cfg.Claude.Models = nil
	cfg.Projects = []corelib.ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	oldProbe := remotePTYCapabilityProbe
	remotePTYCapabilityProbe = func() (bool, string) { return true, "ok" }
	defer func() { remotePTYCapabilityProbe = oldProbe }()

	readiness := app.CheckRemoteClaudeReadiness(projectDir, false)
	if readiness.Ready {
		t.Fatalf("CheckRemoteClaudeReadiness() ready = true, want false")
	}
	if readiness.ModelConfigured {
		t.Fatalf("ModelConfigured = true, want false")
	}
	if len(readiness.Issues) == 0 {
		t.Fatalf("Issues is empty, want missing model issue")
	}
}

func TestCheckRemotePTYProbeReturnsCapabilityFailure(t *testing.T) {
	app := &App{}

	oldCapability := remotePTYCapabilityProbe
	oldInteractive := remotePTYInteractiveProbe
	remotePTYCapabilityProbe = func() (bool, string) { return false, "not supported" }
	remotePTYInteractiveProbe = func() (bool, string) { return true, "should not run" }
	defer func() {
		remotePTYCapabilityProbe = oldCapability
		remotePTYInteractiveProbe = oldInteractive
	}()

	got := app.CheckRemotePTYProbe()
	if got.Supported {
		t.Fatalf("Supported = true, want false")
	}
	if got.Ready {
		t.Fatalf("Ready = true, want false")
	}
	if got.Message != "not supported" {
		t.Fatalf("Message = %q, want %q", got.Message, "not supported")
	}
}

func TestCheckRemotePTYProbeRunsInteractiveProbeWhenSupported(t *testing.T) {
	app := &App{}

	oldCapability := remotePTYCapabilityProbe
	oldInteractive := remotePTYInteractiveProbe
	remotePTYCapabilityProbe = func() (bool, string) { return true, "supported" }
	remotePTYInteractiveProbe = func() (bool, string) { return true, "interactive ok" }
	defer func() {
		remotePTYCapabilityProbe = oldCapability
		remotePTYInteractiveProbe = oldInteractive
	}()

	got := app.CheckRemotePTYProbe()
	if !got.Supported {
		t.Fatalf("Supported = false, want true")
	}
	if !got.Ready {
		t.Fatalf("Ready = false, want true")
	}
	if got.Message != "interactive ok" {
		t.Fatalf("Message = %q, want %q", got.Message, "interactive ok")
	}
}

func TestCheckRemoteClaudeLaunchProbeReturnsCapabilityFailure(t *testing.T) {
	app := &App{}

	oldCapability := remotePTYCapabilityProbe
	oldLaunch := remoteClaudeLaunchProbe
	remotePTYCapabilityProbe = func() (bool, string) { return false, "not supported" }
	remoteClaudeLaunchProbe = func(cmd CommandSpec) (bool, string) {
		t.Fatalf("remoteClaudeLaunchProbe should not run when capability probe fails")
		return false, ""
	}
	defer func() {
		remotePTYCapabilityProbe = oldCapability
		remoteClaudeLaunchProbe = oldLaunch
	}()

	got := app.CheckRemoteClaudeLaunchProbe("", false)
	if got.Supported {
		t.Fatalf("Supported = true, want false")
	}
	if got.Ready {
		t.Fatalf("Ready = true, want false")
	}
	if got.Message != "not supported" {
		t.Fatalf("Message = %q, want %q", got.Message, "not supported")
	}
}

func TestCheckRemoteClaudeLaunchProbeRunsLaunchProbe(t *testing.T) {
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
	claudePath := filepath.Join(toolsDir, claudeExe)
	if err := os.WriteFile(claudePath, []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile(claudePath) error = %v", err)
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
	cfg.Claude.Models = []corelib.ModelConfig{{ModelName: "Original", IsBuiltin: true}}
	cfg.Projects = []corelib.ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	oldCapability := remotePTYCapabilityProbe
	oldLaunch := remoteClaudeLaunchProbe
	remotePTYCapabilityProbe = func() (bool, string) { return true, "supported" }
	remoteClaudeLaunchProbe = func(cmd CommandSpec) (bool, string) {
		if cmd.Command == "" {
			t.Fatalf("launch probe received empty command")
		}
		if cmd.Cwd != projectDir {
			t.Fatalf("launch probe cwd = %q, want %q", cmd.Cwd, projectDir)
		}
		return true, "launch ok"
	}
	defer func() {
		remotePTYCapabilityProbe = oldCapability
		remoteClaudeLaunchProbe = oldLaunch
	}()

	got := app.CheckRemoteClaudeLaunchProbe(projectDir, false)
	if !got.Supported {
		t.Fatalf("Supported = false, want true")
	}
	if !got.Ready {
		t.Fatalf("Ready = false, want true")
	}
	if got.Message != "launch ok" {
		t.Fatalf("Message = %q, want %q", got.Message, "launch ok")
	}
	if got.CommandPath == "" {
		t.Fatalf("CommandPath is empty")
	}
}

func TestCheckRemoteToolReadinessSupportsCodex(t *testing.T) {
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
	cfg.Codex.Models = []corelib.ModelConfig{{ModelName: "Original", ModelId: "gpt-5.2-codex", IsBuiltin: true}}
	cfg.Projects = []corelib.ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	oldProbe := remotePTYCapabilityProbe
	remotePTYCapabilityProbe = func() (bool, string) { return true, "ok" }
	defer func() { remotePTYCapabilityProbe = oldProbe }()

	readiness := app.CheckRemoteToolReadiness("codex", projectDir, false)
	if readiness.Tool != "codex" {
		t.Fatalf("Tool = %q, want codex", readiness.Tool)
	}
	if !readiness.Ready {
		t.Fatalf("CheckRemoteToolReadiness(codex) ready = false, issues = %#v", readiness.Issues)
	}
	if readiness.CommandPath == "" {
		t.Fatalf("CommandPath is empty")
	}
}

func TestCheckRemoteToolLaunchProbeRunsForCodex(t *testing.T) {
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
	cfg.Codex.Models = []corelib.ModelConfig{{ModelName: "Original", IsBuiltin: true}}
	cfg.Projects = []corelib.ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	oldCapability := remotePTYCapabilityProbe
	oldLaunch := remoteClaudeLaunchProbe
	remotePTYCapabilityProbe = func() (bool, string) { return true, "supported" }
	remoteClaudeLaunchProbe = func(cmd CommandSpec) (bool, string) {
		if cmd.Command == "" {
			t.Fatalf("launch probe received empty command")
		}
		if cmd.Cwd != projectDir {
			t.Fatalf("launch probe cwd = %q, want %q", cmd.Cwd, projectDir)
		}
		return true, "launch ok"
	}
	defer func() {
		remotePTYCapabilityProbe = oldCapability
		remoteClaudeLaunchProbe = oldLaunch
	}()

	got := app.CheckRemoteToolLaunchProbe("codex", projectDir, false)
	if got.Tool != "codex" {
		t.Fatalf("Tool = %q, want codex", got.Tool)
	}
	if !got.Supported || !got.Ready {
		t.Fatalf("probe = %#v, want supported and ready", got)
	}
	if got.Message != "launch ok" {
		t.Fatalf("Message = %q, want launch ok", got.Message)
	}
}

func TestBuildRemoteLaunchSpecDryRunDoesNotWriteNativeCodexConfig(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	projectDir := filepath.Join(tempHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectDir) error = %v", err)
	}

	app := &App{testHomeDir: tempHome}
	cfg := corelib.AppConfig{
		Codex: corelib.ToolConfig{
			CurrentModel: "Custom",
			Models: []corelib.ModelConfig{{
				ModelName: "Custom",
				ModelId:   "gpt-test",
				ModelUrl:  "https://api.example.com/v1",
				ApiKey:    "sk-probe",
				WireApi:   "responses",
			}},
		},
		Projects:       []corelib.ProjectConfig{{Id: "p1", Path: projectDir}},
		CurrentProject: "p1",
	}

	spec, err := app.buildRemoteLaunchSpec("codex", cfg, false, false, "", projectDir, false, "", probeNativeToolConfig)
	if err != nil {
		t.Fatalf("buildRemoteLaunchSpec dry-run error = %v", err)
	}
	if got := spec.Env["OPENAI_API_KEY"]; got != "sk-probe" {
		t.Fatalf("OPENAI_API_KEY = %q, want sk-probe", got)
	}
	if _, err := os.Stat(filepath.Join(tempHome, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("dry-run must not write ~/.codex/config.toml, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempHome, ".codex", "auth.json")); !os.IsNotExist(err) {
		t.Fatalf("dry-run must not write ~/.codex/auth.json, stat err = %v", err)
	}
}

func TestCheckRemoteToolReadinessDoesNotWriteNativeCodexConfig(t *testing.T) {
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
	cfg.Codex.CurrentModel = "Custom"
	cfg.Codex.Models = []corelib.ModelConfig{{
		ModelName: "Custom",
		ModelId:   "gpt-test",
		ModelUrl:  "https://api.example.com/v1",
		ApiKey:    "sk-readiness",
		WireApi:   "responses",
	}}
	cfg.Projects = []corelib.ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	oldProbe := remotePTYCapabilityProbe
	remotePTYCapabilityProbe = func() (bool, string) { return true, "ok" }
	defer func() { remotePTYCapabilityProbe = oldProbe }()

	_ = app.CheckRemoteToolReadiness("codex", projectDir, false)
	if _, err := os.Stat(filepath.Join(tempHome, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("readiness check must not write ~/.codex/config.toml, stat err = %v", err)
	}
}

func TestBuildRemoteLaunchSpecWriteNativeCreatesCodexConfig(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	projectDir := filepath.Join(tempHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectDir) error = %v", err)
	}

	app := &App{testHomeDir: tempHome}
	cfg := corelib.AppConfig{
		Codex: corelib.ToolConfig{
			CurrentModel: "Custom",
			Models: []corelib.ModelConfig{{
				ModelName: "Custom",
				ModelId:   "gpt-test",
				ModelUrl:  "https://api.example.com/v1",
				ApiKey:    "sk-launch",
				WireApi:   "responses",
			}},
		},
		Projects:       []corelib.ProjectConfig{{Id: "p1", Path: projectDir}},
		CurrentProject: "p1",
	}

	if _, err := app.buildRemoteLaunchSpec("codex", cfg, false, false, "", projectDir, false, "", persistNativeToolConfig); err != nil {
		t.Fatalf("buildRemoteLaunchSpec write-native error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempHome, ".codex", "config.toml")); err != nil {
		t.Fatalf("launch path should write ~/.codex/config.toml: %v", err)
	}
}

func TestLaunchToolLocalUsesSharedEnvBuilderForCodexConfig(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	projectDir := filepath.Join(tempHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectDir) error = %v", err)
	}

	app := &App{testHomeDir: tempHome}
	cfg := corelib.AppConfig{
		DefaultLaunchMode: "local",
		Codex: corelib.ToolConfig{
			CurrentModel: "Custom",
			Models: []corelib.ModelConfig{{
				ModelName: "Custom",
				ModelId:   "gpt-test",
				ModelUrl:  "https://api.example.com/v1",
				ApiKey:    "sk-local-launch",
				WireApi:   "responses",
			}},
		},
		Projects:       []corelib.ProjectConfig{{Id: "p1", Path: projectDir}},
		CurrentProject: "p1",
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	// Launch will fail later (binary missing) after native config is prepared.
	_ = app.LaunchTool("codex", false, false, false, "", projectDir, false)

	if _, err := os.Stat(filepath.Join(tempHome, ".codex", "config.toml")); err != nil {
		t.Fatalf("local LaunchTool should write ~/.codex via shared builder: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(tempHome, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	if !strings.Contains(string(data), "api.example.com") {
		t.Fatalf("config.toml missing provider URL:\n%s", data)
	}
}
