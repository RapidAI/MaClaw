package main

import (
	"fmt"
	"os"
	"strings"
)

// CodexAdapter launches the OpenAI Codex CLI in non-interactive SDK mode
// using `codex exec --json --full-auto`.  This avoids PTY confirmation
// prompts entirely by leveraging Codex's structured JSONL output protocol.
//
// When YoloMode is enabled, `--full-auto` allows file edits and commands.
// Otherwise, the default read-only sandbox is used.
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
	return ExecModeCodexSDK
}

func (a *CodexAdapter) BuildCommand(spec LaunchSpec) (CommandSpec, error) {
	tm := NewToolManager(a.app)
	status := tm.GetToolStatus("codex")
	if !status.Installed || status.Path == "" {
		return CommandSpec{}, fmt.Errorf("codex is not installed (installed=%v, path=%q)", status.Installed, status.Path)
	}

	resolvedPath := resolveWindowsSidecarExecutable(status.Path, []string{"codex.exe", "openai.exe"})
	if info, err := os.Stat(resolvedPath); err != nil {
		return CommandSpec{}, fmt.Errorf("codex binary not accessible: %s (resolved from %s): %w", resolvedPath, status.Path, err)
	} else if info.IsDir() {
		return CommandSpec{}, fmt.Errorf("codex path is a directory, not executable: %s", resolvedPath)
	}

	// In original (OpenAI native) mode, don't inject model or wire_api
	// so Codex uses its own `codex auth` login and default settings.
	isOriginal := strings.ToLower(strings.TrimSpace(spec.ModelName)) == "original"

	extra := map[string]string{}
	if !isOriginal {
		if spec.ModelID != "" {
			extra["OPENAI_MODEL"] = spec.ModelID
		}
		if spec.Env["WIRE_API"] == "" {
			extra["WIRE_API"] = "responses"
		}
	}
	env := buildOpenAICompatibleCommandEnv(spec.Env, extra)

	// Validate project path exists.
	if spec.ProjectPath != "" {
		if _, err := os.Stat(spec.ProjectPath); err != nil {
			return CommandSpec{}, fmt.Errorf("codex project path not accessible: %s: %w", spec.ProjectPath, err)
		}
	}

	// Use `codex exec` sub-command for non-interactive structured output.
	// --json streams JSONL events to stdout (thread.started, item.*, turn.*).
	// --full-auto allows file edits and command execution without prompts.
	args := []string{"exec", "--json"}

	if spec.YoloMode {
		args = append(args, "--full-auto")
	}

	if !isOriginal && spec.ModelID != "" {
		args = append(args, "--model", spec.ModelID)
	}

	return CommandSpec{
		Command: resolvedPath,
		Args:    args,
		Cwd:     spec.ProjectPath,
		Env:     env,
		Cols:    120,
		Rows:    32,
	}, nil
}
