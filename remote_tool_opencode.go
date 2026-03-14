package main

import (
	"fmt"
)

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
	return ExecModePTY
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
	env := buildOpenAICompatibleCommandEnv(spec.Env, extra)

	return CommandSpec{
		Command: resolveWindowsSidecarExecutable(status.Path, []string{"opencode.exe"}),
		Cwd:     spec.ProjectPath,
		Env:     env,
		Cols:    120,
		Rows:    32,
	}, nil
}
