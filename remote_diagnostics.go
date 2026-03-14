package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var remotePTYCapabilityProbe = remotePTYCapability
var remotePTYInteractiveProbe = remotePTYInteractiveSmokeProbe
var remoteClaudeLaunchProbe = remoteClaudeLaunchSmokeProbe

type RemoteToolLaunchProbeResult struct {
	Tool        string `json:"tool"`
	Supported   bool   `json:"supported"`
	Ready       bool   `json:"ready"`
	Message     string `json:"message"`
	CommandPath string `json:"command_path"`
	ProjectPath string `json:"project_path"`
}

type RemoteToolReadiness struct {
	Tool            string   `json:"tool"`
	Ready           bool     `json:"ready"`
	RemoteEnabled   bool     `json:"remote_enabled"`
	ToolInstalled   bool     `json:"tool_installed"`
	ModelConfigured bool     `json:"model_configured"`
	ProjectPath     string   `json:"project_path"`
	ToolPath        string   `json:"tool_path"`
	CommandPath     string   `json:"command_path"`
	HubURL          string   `json:"hub_url"`
	PTYSupported    bool     `json:"pty_supported"`
	PTYMessage      string   `json:"pty_message"`
	SelectedModel   string   `json:"selected_model"`
	SelectedModelID string   `json:"selected_model_id"`
	Issues          []string `json:"issues"`
	Warnings        []string `json:"warnings"`
}

type RemotePTYProbeResult struct {
	Supported bool   `json:"supported"`
	Ready     bool   `json:"ready"`
	Message   string `json:"message"`
}

type RemoteClaudeLaunchProbeResult = RemoteToolLaunchProbeResult

type RemoteClaudeReadiness = RemoteToolReadiness

func toolEnvHasCredential(env map[string]string) bool {
	for key, value := range env {
		if strings.TrimSpace(value) == "" {
			continue
		}
		upperKey := strings.ToUpper(strings.TrimSpace(key))
		if strings.Contains(upperKey, "API_KEY") || strings.Contains(upperKey, "AUTH_TOKEN") {
			return true
		}
	}
	return false
}

func ensureDiagnosticSessionSpec(spec LaunchSpec) LaunchSpec {
	if strings.TrimSpace(spec.SessionID) == "" {
		spec.SessionID = "diag"
	}
	return spec
}

func (a *App) CheckRemoteToolReadiness(toolName, projectDir string, useProxy bool) RemoteToolReadiness {
	tool := normalizeRemoteToolName(toolName)
	toolLabel := remoteToolDisplayName(tool)
	readiness := RemoteToolReadiness{
		Tool:     tool,
		Issues:   []string{},
		Warnings: []string{},
	}

	cfg, err := a.LoadConfig()
	if err != nil {
		readiness.Issues = append(readiness.Issues, fmt.Sprintf("load config: %v", err))
		return readiness
	}

	readiness.RemoteEnabled = cfg.RemoteEnabled
	readiness.HubURL = strings.TrimSpace(cfg.RemoteHubURL)

	if strings.TrimSpace(projectDir) == "" {
		projectDir = a.GetCurrentProjectPath()
	}
	projectDir = filepath.Clean(strings.TrimSpace(projectDir))
	readiness.ProjectPath = projectDir

	if projectDir == "" {
		readiness.Issues = append(readiness.Issues, "project path is empty")
	}

	if info, err := os.Stat(projectDir); err != nil {
		readiness.Issues = append(readiness.Issues, fmt.Sprintf("project path not accessible: %v", err))
	} else if !info.IsDir() {
		readiness.Issues = append(readiness.Issues, "project path is not a directory")
	}

	status := NewToolManager(a).GetToolStatus(tool)
	readiness.ToolInstalled = status.Installed
	readiness.ToolPath = status.Path
	if !status.Installed || strings.TrimSpace(status.Path) == "" {
		readiness.Issues = append(readiness.Issues, fmt.Sprintf("%s is not installed in ~/.cceasy/tools", tool))
	}

	ptySupported, ptyMessage := remotePTYCapabilityProbe()
	readiness.PTYSupported = ptySupported
	readiness.PTYMessage = ptyMessage
	if !ptySupported {
		readiness.Issues = append(readiness.Issues, ptyMessage)
	}

	selectedModel := a.findSelectedModel(cfg, tool)
	if selectedModel != nil {
		readiness.ModelConfigured = true
		readiness.SelectedModel = selectedModel.ModelName
		readiness.SelectedModelID = selectedModel.ModelId
	} else {
		readiness.Issues = append(readiness.Issues, fmt.Sprintf("no %s provider/model is selected", toolLabel))
	}

	spec, err := a.buildRemoteLaunchSpec(tool, cfg, false, false, "", projectDir, useProxy)
	if err != nil {
		readiness.Issues = append(readiness.Issues, fmt.Sprintf("build %s launch spec failed: %v", toolLabel, err))
		return readiness
	}
	spec = ensureDiagnosticSessionSpec(spec)

	readiness.SelectedModel = spec.ModelName
	readiness.SelectedModelID = spec.ModelID

	if strings.ToLower(strings.TrimSpace(spec.ModelName)) != "original" && !toolEnvHasCredential(spec.Env) {
		readiness.Issues = append(readiness.Issues, fmt.Sprintf("%s provider API key is empty", toolLabel))
	}

	adapter, err := a.remoteProviderAdapter(tool)
	if err != nil {
		readiness.Issues = append(readiness.Issues, err.Error())
		return readiness
	}

	cmd, err := adapter.BuildCommand(spec)
	if err != nil {
		readiness.Issues = append(readiness.Issues, fmt.Sprintf("build %s command failed: %v", toolLabel, err))
		return readiness
	}

	readiness.CommandPath = cmd.Command
	if info, err := os.Stat(cmd.Command); err != nil {
		readiness.Issues = append(readiness.Issues, fmt.Sprintf("%s command path not accessible: %v", toolLabel, err))
	} else if info.IsDir() {
		readiness.Issues = append(readiness.Issues, fmt.Sprintf("%s command path points to a directory", toolLabel))
	}

	if strings.TrimSpace(cmd.Cwd) == "" {
		readiness.Issues = append(readiness.Issues, fmt.Sprintf("%s working directory is empty", toolLabel))
	} else if info, err := os.Stat(cmd.Cwd); err != nil {
		readiness.Issues = append(readiness.Issues, fmt.Sprintf("%s working directory not accessible: %v", toolLabel, err))
	} else if !info.IsDir() {
		readiness.Issues = append(readiness.Issues, fmt.Sprintf("%s working directory is not a directory", toolLabel))
	}

	if strings.TrimSpace(readiness.HubURL) == "" {
		readiness.Warnings = append(readiness.Warnings, "remote hub URL is not configured yet")
	}
	if strings.TrimSpace(cfg.RemoteMachineID) == "" || strings.TrimSpace(cfg.RemoteMachineToken) == "" {
		readiness.Warnings = append(readiness.Warnings, "remote machine is not registered yet")
	}

	readiness.Ready = len(readiness.Issues) == 0
	return readiness
}

func (a *App) CheckRemoteClaudeReadiness(projectDir string, useProxy bool) RemoteClaudeReadiness {
	return a.CheckRemoteToolReadiness("claude", projectDir, useProxy)
}

func (a *App) findSelectedModel(cfg AppConfig, toolName string) *ModelConfig {
	toolCfg, err := remoteToolConfig(cfg, toolName)
	if err != nil {
		return nil
	}
	current := strings.TrimSpace(toolCfg.CurrentModel)
	if current == "" {
		return nil
	}
	for _, m := range toolCfg.Models {
		if strings.TrimSpace(m.ModelName) == current {
			model := m
			return &model
		}
	}
	return nil
}

func (a *App) CheckRemotePTYProbe() RemotePTYProbeResult {
	supported, message := remotePTYCapabilityProbe()
	if !supported {
		return RemotePTYProbeResult{
			Supported: false,
			Ready:     false,
			Message:   message,
		}
	}

	ready, probeMessage := remotePTYInteractiveProbe()
	return RemotePTYProbeResult{
		Supported: true,
		Ready:     ready,
		Message:   probeMessage,
	}
}

func (a *App) CheckRemoteToolLaunchProbe(toolName, projectDir string, useProxy bool) RemoteToolLaunchProbeResult {
	tool := normalizeRemoteToolName(toolName)
	toolLabel := remoteToolDisplayName(tool)
	result := RemoteToolLaunchProbeResult{
		Tool: tool,
	}

	cfg, err := a.LoadConfig()
	if err != nil {
		result.Message = "load config failed: " + err.Error()
		return result
	}

	if strings.TrimSpace(projectDir) == "" {
		projectDir = a.GetCurrentProjectPath()
	}
	projectDir = filepath.Clean(strings.TrimSpace(projectDir))
	result.ProjectPath = projectDir

	supported, message := remotePTYCapabilityProbe()
	result.Supported = supported
	if !supported {
		result.Message = message
		return result
	}

	spec, err := a.buildRemoteLaunchSpec(tool, cfg, false, false, "", projectDir, useProxy)
	if err != nil {
		result.Message = fmt.Sprintf("build %s launch spec failed: %v", toolLabel, err)
		return result
	}
	spec = ensureDiagnosticSessionSpec(spec)

	adapter, err := a.remoteProviderAdapter(tool)
	if err != nil {
		result.Message = err.Error()
		return result
	}

	cmd, err := adapter.BuildCommand(spec)
	if err != nil {
		result.Message = fmt.Sprintf("build %s command failed: %v", toolLabel, err)
		return result
	}
	result.CommandPath = cmd.Command

	ready, probeMessage := remoteClaudeLaunchProbe(cmd)
	result.Ready = ready
	result.Message = probeMessage
	return result
}

func (a *App) CheckRemoteClaudeLaunchProbe(projectDir string, useProxy bool) RemoteClaudeLaunchProbeResult {
	return a.CheckRemoteToolLaunchProbe("claude", projectDir, useProxy)
}
