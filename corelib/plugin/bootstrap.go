package plugin

import (
	"context"
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// Plugin tools are registered into the shared tool.Registry via pluginToolToRegistered(),
// making them available through the same DynamicToolBuilder/Router path as builtin tools.
// This means any tool registered by a plugin is automatically discoverable and callable
// through the existing Agent tool-call pipeline without additional wiring.

// Bootstrap creates a DiscoveryManager and PluginRegistry, discovers all plugins,
// loads and starts them, and starts the health check goroutine.
// Returns the PluginRegistry for use by the application.
func Bootstrap(ctx context.Context, toolReg *tool.Registry, projectDir string) (*PluginRegistry, error) {
	dm := NewDiscoveryManager()
	manifests, err := dm.DiscoverAll(projectDir)
	if err != nil {
		return nil, fmt.Errorf("plugin discovery: %w", err)
	}

	pr := NewPluginRegistry(toolReg)
	if err := pr.LoadAndStart(ctx, manifests); err != nil {
		return nil, fmt.Errorf("plugin load: %w", err)
	}

	pr.StartHealthCheck(ctx)
	return pr, nil
}
