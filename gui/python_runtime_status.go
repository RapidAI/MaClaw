package main

import (
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/pyenv"
)

func (a *App) ListPythonRuntimes() ([]pyenv.SharedPythonRuntimeStatus, error) {
	if a == nil {
		return nil, fmt.Errorf("app is nil")
	}
	return pyenv.ListSharedPythonRuntimes(a.GetDataDir())
}
