package main

import (
	"fmt"
	"log"

	"github.com/RapidAI/CodeClaw/corelib/pyenv"
)

func (a *App) ListPythonRuntimes() ([]pyenv.SharedPythonRuntimeStatus, error) {
	if a == nil {
		return nil, fmt.Errorf("app is nil")
	}
	return pyenv.ListSharedPythonRuntimes(a.GetDataDir())
}

func missingPythonRuntimeComponent(st pyenv.Status) string {
	if !st.Available {
		return "Python"
	}
	if !st.IsPrivate {
		return "Private Python"
	}
	if !st.UVAvailable {
		return "uv"
	}
	if !st.UVIsPrivate {
		return "Private uv"
	}
	if !st.VenvReady {
		return "Python venv"
	}
	return ""
}

func pythonRuntimeReady(st pyenv.Status) bool {
	return missingPythonRuntimeComponent(st) == ""
}

func (a *App) logPythonRuntimeCheck(message string) {
	log.Printf("[env-check] %s", message)
	a.log(message)
}

func (a *App) ensurePythonRuntimeForEnvironmentCheck(checkMessage string) pyenv.Status {
	pyenv.SetUseChinaMirror(normalizeAppLanguageKind(a.CurrentLanguage).IsChinese())

	if checkMessage != "" {
		a.logPythonRuntimeCheck(a.tr(checkMessage))
	}
	pySt := pyenv.Detect()
	if pythonRuntimeReady(pySt) {
		label := "system"
		if pySt.IsPrivate {
			label = "private"
		}
		a.logPythonRuntimeCheck(a.tr("Python environment ready: v%s (%s) -> %s; uv: %s; venv: %s", pySt.Version, label, pySt.PythonPath, pySt.UVPath, pySt.VenvPath))
		return pySt
	}

	missing := missingPythonRuntimeComponent(pySt)
	a.logPythonRuntimeCheck(a.tr("Python environment incomplete (%s). Installing missing Python runtime components ...", missing))
	a.emitEvent("python-install-start")
	pySt = pyenv.EnsureEnvironment(func(stage string, pct int, msg string) {
		a.logPythonRuntimeCheck(fmt.Sprintf("[python-env] [%s] %d%% %s", stage, pct, msg))
		a.emitEvent("python-install-progress", map[string]interface{}{
			"stage": stage, "pct": pct, "msg": msg,
		})
	})
	if pySt.Error != "" {
		a.logPythonRuntimeCheck(a.tr("WARNING: Python environment setup failed: %s", pySt.Error))
	} else {
		a.logPythonRuntimeCheck(a.tr("Python runtime ready: v%s, venv: %s", pySt.Version, pySt.VenvPath))
	}
	a.emitEvent("python-install-done", map[string]interface{}{
		"available": pySt.Available,
		"version":   pySt.Version,
		"error":     pySt.Error,
	})
	return pySt
}

func (a *App) ensurePythonRuntimeForEnvironmentCheckCLI() pyenv.Status {
	pyenv.SetUseChinaMirror(normalizeAppLanguageKind(a.CurrentLanguage).IsChinese())

	pySt := pyenv.Detect()
	if pythonRuntimeReady(pySt) {
		label := "system"
		if pySt.IsPrivate {
			label = "private"
		}
		fmt.Printf("-Python runtime ready: v%s (%s) -> %s\n", pySt.Version, label, pySt.PythonPath)
		fmt.Printf("  uv:   %s\n", pySt.UVPath)
		fmt.Printf("  venv: %s\n", pySt.VenvPath)
		return pySt
	}

	fmt.Printf("Python environment incomplete (%s). Installing missing components...\n", missingPythonRuntimeComponent(pySt))
	pySt = pyenv.EnsureEnvironment(func(stage string, pct int, msg string) {
		fmt.Printf("[python-env] [%s] %d%% %s\n", stage, pct, msg)
	})
	if pySt.Error != "" {
		fmt.Printf("-WARNING: Python environment setup failed: %s\n", pySt.Error)
	} else {
		fmt.Printf("-Python runtime ready: v%s, venv: %s\n", pySt.Version, pySt.VenvPath)
	}
	return pySt
}
