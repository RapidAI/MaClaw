package plugin

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// PluginRegistry manages all registered plugins and their lifecycle.
type PluginRegistry struct {
	mu      sync.RWMutex
	plugins map[string]*pluginEntry
	toolReg *tool.Registry
	cancel  context.CancelFunc // for health check goroutine
}

// pluginEntry tracks a single plugin's state within the registry.
type pluginEntry struct {
	plugin   Plugin
	manifest PluginManifest
	status   string // "registered", "running", "stopped", "error"
	err      error
	tools    []string     // tool names registered by this plugin
	health   HealthStatus // latest health check result
}

// NewPluginRegistry creates a PluginRegistry backed by the given tool.Registry.
func NewPluginRegistry(toolReg *tool.Registry) *PluginRegistry {
	return &PluginRegistry{
		plugins: make(map[string]*pluginEntry),
		toolReg: toolReg,
	}
}

// Register adds a plugin to the registry with status "registered".
// Returns an error if a plugin with the same name is already registered.
func (pr *PluginRegistry) Register(p Plugin) error {
	if p == nil {
		return fmt.Errorf("plugin: cannot register nil plugin")
	}
	m := p.Manifest()
	if m.Name == "" {
		return fmt.Errorf("plugin: manifest name must be non-empty")
	}

	pr.mu.Lock()
	defer pr.mu.Unlock()

	if _, exists := pr.plugins[m.Name]; exists {
		return fmt.Errorf("plugin: %q is already registered", m.Name)
	}

	pr.plugins[m.Name] = &pluginEntry{
		plugin:   p,
		manifest: m,
		status:   "registered",
	}
	return nil
}

// Unregister stops a plugin (with a 10-second timeout), removes its tools
// from the tool.Registry, and deletes it from the plugin map.
func (pr *PluginRegistry) Unregister(name string) error {
	pr.mu.Lock()
	entry, exists := pr.plugins[name]
	if !exists {
		pr.mu.Unlock()
		return fmt.Errorf("plugin: %q is not registered", name)
	}
	// Snapshot tool names and plugin ref before releasing the lock.
	toolNames := entry.tools
	p := entry.plugin
	delete(pr.plugins, name)
	pr.mu.Unlock()

	// Stop with a 10-second timeout (Task 6.3).
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()

	if err := p.Stop(stopCtx); err != nil {
		if stopCtx.Err() == context.DeadlineExceeded {
			log.Printf("ERROR: plugin %q Stop timed out after 10s, force cancelled", name)
		} else {
			log.Printf("ERROR: plugin %q Stop failed: %v", name, err)
		}
	}

	// Remove all tools this plugin registered from the tool.Registry.
	for _, tn := range toolNames {
		pr.toolReg.Unregister(tn)
	}

	return nil
}

// List returns info for all registered plugins.
func (pr *PluginRegistry) List() []PluginInfo {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	out := make([]PluginInfo, 0, len(pr.plugins))
	for _, e := range pr.plugins {
		info := PluginInfo{
			Name:        e.manifest.Name,
			Version:     e.manifest.Version,
			Description: e.manifest.Description,
			Type:        e.manifest.Type,
			Scope:       e.manifest.Scope,
			Status:      e.status,
			ToolCount:   len(e.tools),
			Health:      e.health,
		}
		if hp, ok := e.plugin.(HookProvider); ok {
			info.HookCount = len(hp.Hooks())
		}
		if e.err != nil {
			info.Error = e.err.Error()
		}
		out = append(out, info)
	}
	return out
}

// Get returns info for a single plugin by name.
func (pr *PluginRegistry) Get(name string) (*PluginInfo, bool) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	e, ok := pr.plugins[name]
	if !ok {
		return nil, false
	}
	info := &PluginInfo{
		Name:        e.manifest.Name,
		Version:     e.manifest.Version,
		Description: e.manifest.Description,
		Type:        e.manifest.Type,
		Scope:       e.manifest.Scope,
		Status:      e.status,
		ToolCount:   len(e.tools),
		Health:      e.health,
	}
	if hp, ok := e.plugin.(HookProvider); ok {
		info.HookCount = len(hp.Hooks())
	}
	if e.err != nil {
		info.Error = e.err.Error()
	}
	return info, true
}

// LoadAndStart iterates over the given manifests, creates adapters, initialises
// and starts each plugin, and registers their tools into the tool.Registry.
// A single plugin failure does not affect the others.
func (pr *PluginRegistry) LoadAndStart(ctx context.Context, manifests []PluginManifest) error {
	home, _ := os.UserHomeDir()

	for _, manifest := range manifests {
		// 1. Create adapter via factory.
		p := CreateAdapter(manifest)
		if p == nil {
			log.Printf("WARN: skipping plugin %q: no adapter for type %q", manifest.Name, manifest.Type)
			continue
		}

		// 2. Build PluginConfig.
		dataDir := filepath.Join(home, ".maclaw", "data", "plugins", manifest.Name)
		cfg := PluginConfig{
			DataDir:  dataDir,
			Settings: ResolveEnvVars(manifest.Settings),
			Logger:   defaultPluginLogger{},
		}

		// 3. Init.
		if err := p.Init(cfg); err != nil {
			log.Printf("ERROR: plugin %q Init failed: %v", manifest.Name, err)
			pr.mu.Lock()
			pr.plugins[manifest.Name] = &pluginEntry{
				plugin:   p,
				manifest: manifest,
				status:   "error",
				err:      err,
			}
			pr.mu.Unlock()
			continue
		}

		// 4. Start.
		if err := p.Start(ctx); err != nil {
			log.Printf("ERROR: plugin %q Start failed: %v", manifest.Name, err)
			pr.mu.Lock()
			pr.plugins[manifest.Name] = &pluginEntry{
				plugin:   p,
				manifest: manifest,
				status:   "error",
				err:      err,
			}
			pr.mu.Unlock()
			continue
		}

		// 5. Register tools into tool.Registry.
		var toolNames []string
		for _, td := range p.Tools() {
			rt := pluginToolToRegistered(td, manifest)
			if err := pr.toolReg.Register(rt); err != nil {
				log.Printf("WARN: plugin %q: failed to register tool %q: %v", manifest.Name, td.Name, err)
				continue
			}
			toolNames = append(toolNames, td.Name)
		}

		// 6. Check optional providers (HookProvider, CLIProvider).
		if hp, ok := p.(HookProvider); ok {
			hooks := hp.Hooks()
			log.Printf("INFO: plugin %q provides %d hooks", manifest.Name, len(hooks))
		}
		if cp, ok := p.(CLIProvider); ok {
			cmds := cp.Commands()
			log.Printf("INFO: plugin %q provides %d CLI commands", manifest.Name, len(cmds))
		}

		// 7. Record as running.
		pr.mu.Lock()
		pr.plugins[manifest.Name] = &pluginEntry{
			plugin:   p,
			manifest: manifest,
			status:   "running",
			tools:    toolNames,
		}
		pr.mu.Unlock()
	}
	return nil
}

// StartHealthCheck launches a background goroutine that periodically calls
// Health() on all "running" plugins and updates their cached health status.
// The goroutine stops when ctx is cancelled. The cancel func is stored in
// pr.cancel so it can be stopped externally.
func (pr *PluginRegistry) StartHealthCheck(ctx context.Context) {
	ctx, pr.cancel = context.WithCancel(ctx)

	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pr.runHealthChecks()
			}
		}
	}()
}

// runHealthChecks iterates over all running plugins, calls Health(), and
// updates the cached health status in each entry.
func (pr *PluginRegistry) runHealthChecks() {
	// Snapshot running plugins under read lock.
	pr.mu.RLock()
	type snapshot struct {
		name   string
		plugin Plugin
	}
	var targets []snapshot
	for name, e := range pr.plugins {
		if e.status == "running" {
			targets = append(targets, snapshot{name: name, plugin: e.plugin})
		}
	}
	pr.mu.RUnlock()

	// Call Health() outside the lock, then update each entry individually.
	for _, t := range targets {
		hs := t.plugin.Health()

		pr.mu.Lock()
		if e, ok := pr.plugins[t.name]; ok {
			e.health = hs
		}
		pr.mu.Unlock()
	}
}

// ---------- helpers ----------

// CreateAdapter returns the appropriate Plugin adapter for the given manifest type.
// PluginTypeNative returns nil because native plugins are registered via EntryPointProvider.
func CreateAdapter(manifest PluginManifest) Plugin {
	switch manifest.Type {
	case PluginTypeMCP:
		return NewMCPPluginAdapter(manifest)
	case PluginTypeLocalMCP:
		return NewLocalMCPPluginAdapter(manifest)
	case PluginTypeNLSkill:
		return NewNLSkillPluginAdapter(manifest)
	case PluginTypeNative:
		return nil // native plugins are registered via EntryPointProvider
	default:
		return nil
	}
}

// pluginToolToRegistered converts a plugin ToolDefinition to a tool.RegisteredTool.
func pluginToolToRegistered(td ToolDefinition, manifest PluginManifest) tool.RegisteredTool {
	return tool.RegisteredTool{
		Name:        td.Name,
		Description: td.Description,
		Category:    tool.CategoryMCP,
		Tags:        td.Tags,
		InputSchema: td.InputSchema,
		Required:    td.Required,
		Source:      "plugin:" + manifest.Name,
		Status:      tool.StatusAvailable,
		Handler: func(args map[string]interface{}) string {
			result, err := td.Handler(args)
			if err != nil {
				return fmt.Sprintf("[error] %v", err)
			}
			return result
		},
	}
}

// defaultPluginLogger wraps the standard log package to satisfy PluginLogger.
type defaultPluginLogger struct{}

func (defaultPluginLogger) Info(msg string, args ...interface{})  { log.Printf("INFO: "+msg, args...) }
func (defaultPluginLogger) Warn(msg string, args ...interface{})  { log.Printf("WARN: "+msg, args...) }
func (defaultPluginLogger) Error(msg string, args ...interface{}) { log.Printf("ERROR: "+msg, args...) }
