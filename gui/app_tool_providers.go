package main

// ToolProviderView is a frontend-friendly representation of a single provider
// entry within a tool's model list. Used by the Settings UI to populate the
// default provider dropdown.
type ToolProviderView struct {
	Name    string `json:"name"`
	Valid   bool   `json:"valid"`
	Builtin bool   `json:"builtin"`
}

// ListToolProviders returns the provider list for a given tool, suitable for
// populating the default provider dropdown in settings.
func (a *App) ListToolProviders(toolName string) []ToolProviderView {
	cfg, err := a.LoadConfig()
	if err != nil {
		return []ToolProviderView{}
	}
	toolCfg, err := remoteToolConfig(cfg, toolName)
	if err != nil {
		return []ToolProviderView{}
	}
	out := make([]ToolProviderView, 0, len(toolCfg.Models))
	for _, m := range toolCfg.Models {
		out = append(out, ToolProviderView{
			Name:    m.ModelName,
			Valid:   isValidProvider(m),
			Builtin: m.IsBuiltin,
		})
	}
	return out
}
