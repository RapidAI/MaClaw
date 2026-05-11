package plugin

import "context"

// NLSkillPluginAdapter wraps an NLSkill as a Plugin implementation.
type NLSkillPluginAdapter struct {
	manifest PluginManifest
	config   PluginConfig
	tools    []ToolDefinition
}

// NewNLSkillPluginAdapter creates a new NLSkillPluginAdapter for the given manifest.
func NewNLSkillPluginAdapter(manifest PluginManifest) *NLSkillPluginAdapter {
	return &NLSkillPluginAdapter{
		manifest: manifest,
	}
}

// Manifest returns the plugin's static metadata.
func (a *NLSkillPluginAdapter) Manifest() PluginManifest { return a.manifest }

// Init stores the runtime configuration.
func (a *NLSkillPluginAdapter) Init(cfg PluginConfig) error {
	a.config = cfg
	return nil
}

// Start creates a single ToolDefinition from the manifest metadata.
func (a *NLSkillPluginAdapter) Start(_ context.Context) error {
	a.tools = []ToolDefinition{
		{
			Name:        a.manifest.Name,
			Description: a.manifest.Description,
			Handler: func(args map[string]interface{}) (string, error) {
				return "nlskill execution placeholder", nil
			},
		},
	}
	return nil
}

// Stop is a no-op for NLSkill adapters.
func (a *NLSkillPluginAdapter) Stop(_ context.Context) error {
	return nil
}

// Tools returns the tool definitions.
func (a *NLSkillPluginAdapter) Tools() []ToolDefinition {
	return a.tools
}

// Health always returns healthy for NLSkill adapters.
func (a *NLSkillPluginAdapter) Health() HealthStatus {
	return HealthStatus{Status: PluginHealthHealthy}
}
