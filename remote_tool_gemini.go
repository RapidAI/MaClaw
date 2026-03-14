package main

import (
	"fmt"
)

// GeminiAdapter launches the Gemini CLI (google-gemini/gemini-cli).
// Gemini CLI is a TUI tool similar to Claude Code but from Google.
// It runs in PTY mode as it does not have a stream-json SDK protocol.
type GeminiAdapter struct {
	app *App
}

func NewGeminiAdapter(app *App) *GeminiAdapter {
	return &GeminiAdapter{app: app}
}

func (a *GeminiAdapter) ProviderName() string {
	return "gemini"
}

func (a *GeminiAdapter) ExecutionMode() ExecutionMode {
	return ExecModePTY
}

func (a *GeminiAdapter) BuildCommand(spec LaunchSpec) (CommandSpec, error) {
	tm := NewToolManager(a.app)
	status := tm.GetToolStatus("gemini")
	if !status.Installed || status.Path == "" {
		return CommandSpec{}, fmt.Errorf("gemini is not installed")
	}

	extra := map[string]string{}
	if spec.ModelID != "" {
		extra["GEMINI_MODEL"] = spec.ModelID
	}
	env := buildOpenAICompatibleCommandEnv(spec.Env, extra)

	args := make([]string, 0, 4)
	if spec.ModelID != "" {
		args = append(args, "--model", spec.ModelID)
	}
	if spec.YoloMode {
		args = append(args, "--sandbox", "none")
	}

	return CommandSpec{
		Command: resolveWindowsSidecarExecutable(status.Path, []string{"gemini.exe"}),
		Args:    args,
		Cwd:     spec.ProjectPath,
		Env:     env,
		Cols:    120,
		Rows:    32,
	}, nil
}
