package main

import (
	"fmt"
	"strings"
)

// GeminiAdapter launches the Gemini CLI (google-gemini/gemini-cli).
// Gemini CLI supports --experimental-acp which exposes a JSON-RPC based
// Agent Communication Protocol on stdin/stdout for structured bidirectional
// communication.  This is similar to Claude Code's --input-format stream-json
// but uses JSON-RPC instead of stream-json.
//
// The ACP protocol flow:
//  1. initialize → handshake with protocol version
//  2. session/new → create a new session
//  3. session/prompt → send user messages (streams session/update notifications)
//  4. session/cancel → interrupt current prompt
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
	return ExecModeGeminiACP
}

func (a *GeminiAdapter) BuildCommand(spec LaunchSpec) (CommandSpec, error) {
	tm := NewToolManager(a.app)
	status := tm.GetToolStatus("gemini")
	if !status.Installed || status.Path == "" {
		return CommandSpec{}, fmt.Errorf("gemini is not installed")
	}

	// Ensure Gemini CLI's first-run settings are pre-configured
	// so it doesn't block with interactive prompts.
	if err := ensureGeminiOnboardingComplete(a.app); err != nil {
		if a.app != nil {
			a.app.log(fmt.Sprintf("[gemini-adapter] onboarding pre-check warning: %v", err))
		}
	}

	// In original (Google native) mode, don't inject model env or args
	// so Gemini CLI uses its own Google OAuth login and default settings.
	isOriginal := strings.ToLower(strings.TrimSpace(spec.ModelName)) == "original"

	env := buildOpenAICompatibleCommandEnv(spec.Env, map[string]string{})

	// ACP mode: use --experimental-acp for structured JSON-RPC communication
	args := []string{"--experimental-acp"}

	if !isOriginal && spec.ModelID != "" {
		args = append(args, "--model", spec.ModelID)
	}
	if spec.YoloMode {
		args = append(args, "--yolo")
	}

	return CommandSpec{
		Command: resolveWindowsSidecarExecutable(status.Path, []string{"gemini.exe"}),
		Args:    args,
		Cwd:     spec.ProjectPath,
		Env:     env,
	}, nil
}
