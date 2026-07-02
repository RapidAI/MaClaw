package main

import (
	"fmt"
)

// CodeBuddyAdapter launches the CodeBuddy CLI (腾讯云代码助手).
// CodeBuddy supports `-p --output-format stream-json`, producing JSONL events
// compatible with Claude Code's stream-json protocol. It runs in SDK mode
// reusing the existing SDKExecutionStrategy.
type CodeBuddyAdapter struct {
	app *App
}

func NewCodeBuddyAdapter(app *App) *CodeBuddyAdapter {
	return &CodeBuddyAdapter{app: app}
}

func (a *CodeBuddyAdapter) ProviderName() string {
	return "codebuddy"
}

func (a *CodeBuddyAdapter) ExecutionMode() ExecutionMode {
	return ExecModeSDK
}

func (a *CodeBuddyAdapter) BuildCommand(spec LaunchSpec) (CommandSpec, error) {
	tm := NewToolManager(a.app)
	status := tm.GetToolStatus("codebuddy")
	if !status.Installed || status.Path == "" {
		return CommandSpec{}, fmt.Errorf("codebuddy is not installed")
	}

	// Ensure CodeBuddy's first-run onboarding (login method selection,
	// theme, project trust) is pre-configured so it doesn't block the
	// remote session with interactive prompts.
	if err := ensureCodeBuddyOnboardingComplete(a.app, spec.ProjectPath); err != nil {
		if a.app != nil {
			a.app.log(fmt.Sprintf("[codebuddy-adapter] onboarding pre-check warning: %v", err))
		}
	}

	env := buildOpenAICompatibleCommandEnv(spec.Env, privateToolsDirForApp(a.app), nil)

	args := make([]string, 0, 8)
	// SDK mode: always include -p for CodeBuddy SDK mode and stream-json output
	args = append(args, "-p", "--output-format", "stream-json")
	if spec.ModelID != "" {
		args = append(args, "--model", spec.ModelID)
	}
	if spec.YoloMode {
		args = append(args, "--yolo")
	}

	return CommandSpec{
		Command: resolveWindowsSidecarExecutable(status.Path, []string{"codebuddy.exe", "codebuddy-code.exe"}),
		Args:    args,
		Cwd:     spec.ProjectPath,
		Env:     env,
	}, nil
}
