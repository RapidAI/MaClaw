package plugin

import "context"

// MCPPluginAdapter wraps a remote MCP Server as a Plugin implementation.
// Real MCP client integration is deferred; Start sets healthy=true as a placeholder.
type MCPPluginAdapter struct {
	manifest PluginManifest
	config   PluginConfig
	tools    []ToolDefinition
	healthy  bool
}

// NewMCPPluginAdapter creates a new MCPPluginAdapter for the given manifest.
func NewMCPPluginAdapter(manifest PluginManifest) *MCPPluginAdapter {
	return &MCPPluginAdapter{
		manifest: manifest,
	}
}

// Manifest returns the plugin's static metadata.
func (a *MCPPluginAdapter) Manifest() PluginManifest { return a.manifest }

// Init stores the runtime configuration.
func (a *MCPPluginAdapter) Init(cfg PluginConfig) error {
	a.config = cfg
	return nil
}

// Start is a placeholder that marks the adapter as healthy.
// Real MCP client connection will be implemented in a future task.
func (a *MCPPluginAdapter) Start(_ context.Context) error {
	a.healthy = true
	return nil
}

// Stop marks the adapter as unhealthy.
func (a *MCPPluginAdapter) Stop(_ context.Context) error {
	a.healthy = false
	return nil
}

// Tools returns the tool definitions. Returns empty if not healthy.
func (a *MCPPluginAdapter) Tools() []ToolDefinition {
	if !a.healthy {
		return nil
	}
	return a.tools
}

// Health returns "healthy" when the adapter is running, "unhealthy" otherwise.
func (a *MCPPluginAdapter) Health() HealthStatus {
	if a.healthy {
		return HealthStatus{Status: "healthy"}
	}
	return HealthStatus{Status: "unhealthy"}
}
