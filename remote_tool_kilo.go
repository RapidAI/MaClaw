package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type KiloAdapter struct {
	app *App
}

func NewKiloAdapter(app *App) *KiloAdapter {
	return &KiloAdapter{app: app}
}

func (a *KiloAdapter) ProviderName() string {
	return "kilo"
}

func (a *KiloAdapter) ExecutionMode() ExecutionMode {
	return ExecModePTY
}

func (a *KiloAdapter) BuildCommand(spec LaunchSpec) (CommandSpec, error) {
	if spec.SessionID == "" {
		return CommandSpec{}, fmt.Errorf("kilo session id is required")
	}

	cfg, err := a.app.LoadConfig()
	if err != nil {
		return CommandSpec{}, err
	}
	if err := a.app.syncToKiloSettings(cfg, spec.ProjectPath, spec.SessionID); err != nil {
		return CommandSpec{}, err
	}

	tm := NewToolManager(a.app)
	status := tm.GetToolStatus("kilo")
	if !status.Installed || status.Path == "" {
		return CommandSpec{}, fmt.Errorf("kilo is not installed")
	}

	env := buildOpenAICompatibleCommandEnv(spec.Env, map[string]string{
		"KILO_MODEL": spec.ModelID,
	})
	return CommandSpec{
		Command: resolveWindowsSidecarExecutable(status.Path, []string{"kilo.exe", "kilocode.exe"}),
		Cwd:     spec.ProjectPath,
		Env:     env,
		Cols:    120,
		Rows:    32,
	}, nil
}

type KodeAdapter struct {
	app *App
}

func NewKodeAdapter(app *App) *KodeAdapter {
	return &KodeAdapter{app: app}
}

func (a *KodeAdapter) ProviderName() string {
	return "kode"
}

func (a *KodeAdapter) ExecutionMode() ExecutionMode {
	return ExecModePTY
}

func (a *KodeAdapter) BuildCommand(spec LaunchSpec) (CommandSpec, error) {
	if spec.SessionID == "" {
		return CommandSpec{}, fmt.Errorf("kode session id is required")
	}

	cfg, err := a.app.LoadConfig()
	if err != nil {
		return CommandSpec{}, err
	}
	if err := a.app.syncToKodeSettings(cfg, spec.ProjectPath, spec.SessionID); err != nil {
		return CommandSpec{}, err
	}

	tm := NewToolManager(a.app)
	status := tm.GetToolStatus("kode")
	if !status.Installed || status.Path == "" {
		return CommandSpec{}, fmt.Errorf("kode is not installed")
	}

	env := buildOpenAICompatibleCommandEnv(spec.Env, nil)
	return CommandSpec{
		Command: resolveWindowsSidecarExecutable(status.Path, []string{"kode.exe"}),
		Cwd:     spec.ProjectPath,
		Env:     env,
		Cols:    120,
		Rows:    32,
	}, nil
}

func buildOpenAICompatibleCommandEnv(base map[string]string, extra map[string]string) map[string]string {
	env := map[string]string{}
	for k, v := range base {
		env[k] = v
	}

	home, _ := os.UserHomeDir()
	localToolPath := filepath.Join(home, ".cceasy", "tools")
	npmPath := filepath.Join(os.Getenv("AppData"), "npm")
	nodePath := `C:\Program Files\nodejs`
	gitCmdPath := `C:\Program Files\Git\cmd`
	gitBinPath := `C:\Program Files\Git\bin`
	gitUsrBinPath := `C:\Program Files\Git\usr\bin`

	basePath := env["PATH"]
	if strings.TrimSpace(basePath) == "" {
		basePath = os.Getenv("PATH")
	}
	env["PATH"] = strings.Join([]string{
		localToolPath,
		npmPath,
		nodePath,
		gitCmdPath,
		gitBinPath,
		gitUsrBinPath,
		basePath,
	}, ";")

	for k, v := range extra {
		if strings.TrimSpace(v) == "" {
			continue
		}
		if env[k] == "" {
			env[k] = v
		}
	}

	return env
}

func resolveWindowsSidecarExecutable(path string, sidecars []string) string {
	cleaned := filepath.Clean(path)
	if runtime.GOOS != "windows" {
		return cleaned
	}
	ext := strings.ToLower(filepath.Ext(cleaned))
	if ext == ".cmd" || ext == ".bat" || ext == ".ps1" {
		for _, candidate := range sidecars {
			exePath := filepath.Join(filepath.Dir(cleaned), candidate)
			if _, err := os.Stat(exePath); err == nil {
				return exePath
			}
		}
	}
	return cleaned
}
