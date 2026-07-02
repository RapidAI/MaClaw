package main

import (
	"fmt"
	"strconv"
)

// OpencodeAdapter launches the OpenCode CLI in HTTP server + SSE mode.
// OpenCode supports --server --port <PORT>, exposing an HTTP API endpoint
// with SSE event streaming for structured communication.
// It runs with ExecModeOpenCodeSDK using the OpenCodeSDKExecutionStrategy.
type OpencodeAdapter struct {
	app *App
}

func NewOpencodeAdapter(app *App) *OpencodeAdapter {
	return &OpencodeAdapter{app: app}
}

func (a *OpencodeAdapter) ProviderName() string {
	return "opencode"
}

func (a *OpencodeAdapter) ExecutionMode() ExecutionMode {
	return ExecModeOpenCodeSDK
}

func (a *OpencodeAdapter) BuildCommand(spec LaunchSpec) (CommandSpec, error) {
	if spec.SessionID == "" {
		return CommandSpec{}, fmt.Errorf("opencode session id is required")
	}

	cfg, err := a.app.LoadConfig()
	if err != nil {
		return CommandSpec{}, err
	}
	if err := a.app.syncToOpencodeSettings(cfg, spec.ProjectPath, spec.SessionID); err != nil {
		return CommandSpec{}, err
	}

	tm := NewToolManager(a.app)
	status := tm.GetToolStatus("opencode")
	if !status.Installed || status.Path == "" {
		return CommandSpec{}, fmt.Errorf("opencode is not installed")
	}

	extra := map[string]string{}
	if spec.ModelID != "" {
		extra["OPENCODE_MODEL"] = spec.ModelID
	}
	env := buildOpenAICompatibleCommandEnv(spec.Env, privateToolsDirForApp(a.app), extra)

	port, err := findFreePort()
	if err != nil {
		return CommandSpec{}, fmt.Errorf("failed to find free port for opencode server: %w", err)
	}
	env["OPENCODE_SERVER_PORT"] = strconv.Itoa(port)

	args := []string{"--server", "--port", strconv.Itoa(port)}

	return CommandSpec{
		Command: resolveWindowsSidecarExecutable(status.Path, []string{"opencode.exe"}),
		Args:    args,
		Cwd:     spec.ProjectPath,
		Env:     env,
	}, nil
}
