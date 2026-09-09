package guiapp

// ImportExternalAgents returns a safe empty import result when no external-agent
// importer is available in the current desktop build. The frontend can still
// render the provider configuration flow without calling an undefined binding.
func (a *App) ImportExternalAgents() map[string]any {
	return map[string]any{"imported": []string{}, "skipped": []any{}, "current": ""}
}

// StartOpenCodeZenLogin keeps the OpenCode login action available to generated
// Wails bindings. The provider dialog handles the returned message and allows
// the user to paste an API key manually.
func (a *App) StartOpenCodeZenLogin() map[string]any {
	return map[string]any{"message": "OpenCode API key can be entered manually."}
}
