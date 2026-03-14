package main

import (
	"fmt"
)

type CodexAdapter struct {
	app *App
}

func NewCodexAdapter(app *App) *CodexAdapter {
	return &CodexAdapter{app: app}
}

func (a *CodexAdapter) ProviderName() string {
	return "codex"
}

func (a *CodexAdapter) ExecutionMode() ExecutionMode {
	return ExecModePTY
}

func (a *CodexAdapter) BuildCommand(spec LaunchSpec) (CommandSpec, error) {
	tm := NewToolManager(a.app)
	status := tm.GetToolStatus("codex")
	if !status.Installed || status.Path == "" {
		return CommandSpec{}, fmt.Errorf("codex is not installed")
	}

	extra := map[string]string{}
	if spec.ModelID != "" {
		extra["OPENAI_MODEL"] = spec.ModelID
	}
	if spec.Env["WIRE_API"] == "" {
		extra["WIRE_API"] = "responses"
	}
	env := buildOpenAICompatibleCommandEnv(spec.Env, extra)

	return CommandSpec{
		Command: resolveWindowsSidecarExecutable(status.Path, []string{"codex.exe", "openai.exe"}),
		Cwd:     spec.ProjectPath,
		Env:     env,
		Cols:    120,
		Rows:    32,
	}, nil
}
