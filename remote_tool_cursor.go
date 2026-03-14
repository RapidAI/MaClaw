package main

import (
	"fmt"
)

// CursorAdapter launches the Cursor Agent CLI (`cursor-agent`).
// Cursor Agent is a CLI tool from Cursor that runs in PTY mode.
// It supports --model, --yolo (auto-approve), and other flags.
type CursorAdapter struct {
	app *App
}

func NewCursorAdapter(app *App) *CursorAdapter {
	return &CursorAdapter{app: app}
}

func (a *CursorAdapter) ProviderName() string {
	return "cursor"
}

func (a *CursorAdapter) ExecutionMode() ExecutionMode {
	return ExecModePTY
}

func (a *CursorAdapter) BuildCommand(spec LaunchSpec) (CommandSpec, error) {
	tm := NewToolManager(a.app)
	status := tm.GetToolStatus("cursor")
	if !status.Installed || status.Path == "" {
		return CommandSpec{}, fmt.Errorf("cursor agent is not installed")
	}

	env := buildOpenAICompatibleCommandEnv(spec.Env, nil)

	args := make([]string, 0, 4)
	if spec.ModelID != "" {
		args = append(args, "--model", spec.ModelID)
	}
	if spec.YoloMode {
		args = append(args, "--yolo")
	}

	return CommandSpec{
		Command: resolveWindowsSidecarExecutable(status.Path, []string{"cursor-agent.exe", "agent.exe"}),
		Args:    args,
		Cwd:     spec.ProjectPath,
		Env:     env,
		Cols:    120,
		Rows:    32,
	}, nil
}
