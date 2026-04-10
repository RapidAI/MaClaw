package plugin

import "context"

// LocalMCPPluginAdapter wraps a local stdio MCP Server as a Plugin implementation.
// Real process management is deferred; Start sets healthy=true as a placeholder.
type LocalMCPPluginAdapter struct {
	manifest PluginManifest
	config   PluginConfig
	tools    []ToolDefinition
	healthy  bool
}

// NewLocalMCPPluginAdapter creates a new LocalMCPPluginAdapter for the given manifest.
func NewLocalMCPPluginAdapter(manifest PluginManifest) *LocalMCPPluginAdapter {
	return &LocalMCPPluginAdapter{
		manifest: manifest,
	}
}

// Manifest returns the plugin's static metadata.
func (a *LocalMCPPluginAdapter) Manifest() PluginManifest { return a.manifest }

// Init stores the runtime configuration.
func (a *LocalMCPPluginAdapter) Init(cfg PluginConfig) error {
	a.config = cfg
	return nil
}

// Start is a placeholder that marks the adapter as healthy.
// Real local process startup will be implemented in a future task.
func (a *LocalMCPPluginAdapter) Start(_ context.Context) error {
	a.healthy = true
	return nil
}

// Stop marks the adapter as unhealthy.
func (a *LocalMCPPluginAdapter) Stop(_ context.Context) error {
	a.healthy = false
	return nil
}

// Tools returns the tool definitions. Returns empty if not healthy.
func (a *LocalMCPPluginAdapter) Tools() []ToolDefinition {
	if !a.healthy {
		return nil
	}
	return a.tools
}

// Health returns "healthy" when the adapter is running, "unhealthy" otherwise.
func (a *LocalMCPPluginAdapter) Health() HealthStatus {
	if a.healthy {
		return HealthStatus{Status: "healthy"}
	}
	return HealthStatus{Status: "unhealthy"}
}
