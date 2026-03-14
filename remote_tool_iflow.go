package main

import (
	"fmt"
)

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
	return ExecModePTY
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
	env := buildOpenAICompatibleCommandEnv(spec.Env, extra)

	return CommandSpec{
		Command: resolveWindowsSidecarExecutable(status.Path, []string{"iflow.exe"}),
		Cwd:     spec.ProjectPath,
		Env:     env,
		Cols:    120,
		Rows:    32,
	}, nil
}
