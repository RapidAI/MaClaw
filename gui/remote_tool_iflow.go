package main

import (
	"fmt"
	"net"
	"strconv"
)

// IFlowAdapter launches the iFlow CLI in ACP WebSocket mode.
// iFlow supports --experimental-acp --port <PORT>, exposing an ACP WebSocket
// endpoint at ws://localhost:<PORT>/acp for structured communication.
// It runs with ExecModeIFlowSDK using the IFlowSDKExecutionStrategy.
type IFlowAdapter struct {
	app *App
}

func NewIFlowAdapter(app *App) *IFlowAdapter {
	return &IFlowAdapter{app: app}
}

func (a *IFlowAdapter) ProviderName() string {
	return "iflow"
}

func (a *IFlowAdapter) ExecutionMode() ExecutionMode {
	return ExecModeIFlowSDK
}

func (a *IFlowAdapter) BuildCommand(spec LaunchSpec) (CommandSpec, error) {
	if spec.SessionID == "" {
		return CommandSpec{}, fmt.Errorf("iflow session id is required")
	}

	cfg, err := a.app.LoadConfig()
	if err != nil {
		return CommandSpec{}, err
	}
	if err := a.app.syncToIFlowSettings(cfg, spec.ProjectPath, spec.SessionID); err != nil {
		return CommandSpec{}, err
	}

	tm := NewToolManager(a.app)
	status := tm.GetToolStatus("iflow")
	if !status.Installed || status.Path == "" {
		return CommandSpec{}, fmt.Errorf("iflow is not installed")
	}

	extra := map[string]string{}
	if spec.ModelID != "" {
		extra["IFLOW_MODEL"] = spec.ModelID
	}
	env := buildOpenAICompatibleCommandEnv(spec.Env, privateToolsDirForApp(a.app), extra)

	port, err := findFreePort()
	if err != nil {
		return CommandSpec{}, fmt.Errorf("failed to find free port for iflow ACP: %w", err)
	}
	env["IFLOW_ACP_PORT"] = strconv.Itoa(port)

	args := []string{"--experimental-acp", "--port", strconv.Itoa(port)}

	return CommandSpec{
		Command: resolveWindowsSidecarExecutable(status.Path, []string{"iflow.exe"}),
		Args:    args,
		Cwd:     spec.ProjectPath,
		Env:     env,
	}, nil
}

// findFreePort asks the OS for an available TCP port by binding to ":0",
// retrieving the assigned port, and closing the listener.
func findFreePort() (int, error) {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port, nil
}
