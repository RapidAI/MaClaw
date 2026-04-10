package plugin

import (
	"log"
	"os"
	"path/filepath"
)

// EntryPointProvider is the interface for package-level plugin registration.
// Go packages that want to expose built-in plugins implement this interface
// and register themselves with the DiscoveryManager.
type EntryPointProvider interface {
	Plugins() []Plugin
}

// DiscoveryManager scans three layers of plugin directories and collects
// plugins from EntryPointProviders. Priority order: project > user > package.
type DiscoveryManager struct {
	entryPoints []EntryPointProvider
}

// NewDiscoveryManager creates a new DiscoveryManager with no entry points.
func NewDiscoveryManager() *DiscoveryManager {
	return &DiscoveryManager{}
}

// RegisterEntryPoint adds an EntryPointProvider for package-level plugin discovery.
func (dm *DiscoveryManager) RegisterEntryPoint(ep EntryPointProvider) {
	dm.entryPoints = append(dm.entryPoints, ep)
}

// DiscoverAll scans all three layers and returns a name-unique manifest list.
// Priority: project (.maclaw/plugins/) > user (~/.maclaw/plugins/) > package (EntryPointProviders).
// Same-name plugins at a higher priority layer shadow lower priority ones.
// Missing directories produce an empty list with no error.
func (dm *DiscoveryManager) DiscoverAll(projectDir string) ([]PluginManifest, error) {
	seen := make(map[string]bool)
	var manifests []PluginManifest

	// Layer 1: Project-level (highest priority)
	if projectDir != "" {
		projectPluginDir := filepath.Join(projectDir, ".maclaw", "plugins")
		for _, m := range dm.scanDirectory(projectPluginDir, ScopeProject) {
			if !seen[m.Name] {
				manifests = append(manifests, m)
				seen[m.Name] = true
			}
		}
	}

	// Layer 2: User-level
	home, err := os.UserHomeDir()
	if err == nil {
		userPluginDir := filepath.Join(home, ".maclaw", "plugins")
		for _, m := range dm.scanDirectory(userPluginDir, ScopeUser) {
			if !seen[m.Name] {
				manifests = append(manifests, m)
				seen[m.Name] = true
			}
		}
	}

	// Layer 3: Package-level (lowest priority)
	for _, ep := range dm.entryPoints {
		for _, p := range ep.Plugins() {
			m := p.Manifest()
			m.Scope = ScopePackage
			if !seen[m.Name] {
				manifests = append(manifests, m)
				seen[m.Name] = true
			}
		}
	}

	return manifests, nil
}

// scanDirectory iterates subdirectories of dir, looking for plugin.yaml in each.
// Valid manifests are returned with Scope and Dir set. Invalid manifests are
// skipped with a WARN log. If dir does not exist, an empty slice is returned.
func (dm *DiscoveryManager) scanDirectory(dir string, scope PluginScope) []PluginManifest {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Directory doesn't exist or can't be read — not an error.
		return nil
	}

	var manifests []PluginManifest
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		yamlPath := filepath.Join(dir, entry.Name(), "plugin.yaml")
		m, err := ParseManifestFile(yamlPath)
		if err != nil {
			log.Printf("WARN: skipping plugin %s: %v", entry.Name(), err)
			continue
		}
		m.Scope = scope
		m.Dir = filepath.Join(dir, entry.Name())
		manifests = append(manifests, *m)
	}
	return manifests
}
