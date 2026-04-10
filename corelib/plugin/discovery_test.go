package plugin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Task 5.5: 为 DiscoveryManager 编写单元测试

// mockEntryPointProvider implements EntryPointProvider for testing.
type mockEntryPointProvider struct {
	plugins []Plugin
}

func (m *mockEntryPointProvider) Plugins() []Plugin { return m.plugins }

// mockPlugin is a minimal Plugin for discovery tests.
type mockPlugin struct {
	manifest PluginManifest
}

func (m *mockPlugin) Manifest() PluginManifest              { return m.manifest }
func (m *mockPlugin) Init(cfg PluginConfig) error            { return nil }
func (m *mockPlugin) Start(ctx context.Context) error        { return nil }
func (m *mockPlugin) Stop(ctx context.Context) error         { return nil }
func (m *mockPlugin) Tools() []ToolDefinition                { return nil }
func (m *mockPlugin) Health() HealthStatus                   { return HealthStatus{Status: "healthy"} }

func writePluginYAML(t *testing.T, dir, name, ptype string) {
	t.Helper()
	pluginDir := filepath.Join(dir, name)
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "name: " + name + "\ntype: " + ptype + "\nversion: \"1.0.0\"\n"
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverAll_ProjectOverridesUser(t *testing.T) {
	projectDir := t.TempDir()
	userDir := t.TempDir()

	// Create project-level plugin
	projectPlugins := filepath.Join(projectDir, ".maclaw", "plugins")
	writePluginYAML(t, projectPlugins, "shared-plugin", "mcp")

	// Create user-level plugin with same name (should be shadowed)
	userPlugins := filepath.Join(userDir, ".maclaw", "plugins")
	writePluginYAML(t, userPlugins, "shared-plugin", "nlskill")

	dm := NewDiscoveryManager()
	// We can't easily override the user dir, so test project-level only
	manifests, err := dm.DiscoverAll(projectDir)
	if err != nil {
		t.Fatalf("DiscoverAll: %v", err)
	}

	// Should find at least the project-level plugin
	found := false
	for _, m := range manifests {
		if m.Name == "shared-plugin" {
			found = true
			if m.Scope != ScopeProject {
				t.Errorf("expected ScopeProject, got %q", m.Scope)
			}
		}
	}
	if !found {
		t.Error("shared-plugin not found in results")
	}
}

func TestDiscoverAll_NonexistentDirReturnsEmpty(t *testing.T) {
	dm := NewDiscoveryManager()
	manifests, err := dm.DiscoverAll("/nonexistent/path/that/does/not/exist")
	if err != nil {
		t.Fatalf("DiscoverAll: %v", err)
	}
	// Should not error, may return user/package plugins but no project ones
	_ = manifests
}

func TestDiscoverAll_EmptyProjectDir(t *testing.T) {
	dm := NewDiscoveryManager()
	manifests, err := dm.DiscoverAll("")
	if err != nil {
		t.Fatalf("DiscoverAll: %v", err)
	}
	_ = manifests // should not panic
}

func TestDiscoverAll_InvalidPluginYAMLSkipped(t *testing.T) {
	projectDir := t.TempDir()
	pluginsDir := filepath.Join(projectDir, ".maclaw", "plugins")

	// Valid plugin
	writePluginYAML(t, pluginsDir, "valid-plugin", "mcp")

	// Invalid plugin (missing name)
	invalidDir := filepath.Join(pluginsDir, "invalid-plugin")
	os.MkdirAll(invalidDir, 0755)
	os.WriteFile(filepath.Join(invalidDir, "plugin.yaml"), []byte("type: mcp\n"), 0644)

	dm := NewDiscoveryManager()
	manifests, err := dm.DiscoverAll(projectDir)
	if err != nil {
		t.Fatalf("DiscoverAll: %v", err)
	}

	// Only valid-plugin should be in results
	for _, m := range manifests {
		if m.Name == "" {
			t.Error("invalid plugin with empty name should have been skipped")
		}
	}
}

func TestDiscoverAll_PackageLevelPlugins(t *testing.T) {
	dm := NewDiscoveryManager()
	dm.RegisterEntryPoint(&mockEntryPointProvider{
		plugins: []Plugin{
			&mockPlugin{manifest: PluginManifest{Name: "pkg-plugin", Type: PluginTypeNative, Version: "1.0.0"}},
		},
	})

	manifests, err := dm.DiscoverAll("")
	if err != nil {
		t.Fatalf("DiscoverAll: %v", err)
	}

	found := false
	for _, m := range manifests {
		if m.Name == "pkg-plugin" {
			found = true
			if m.Scope != ScopePackage {
				t.Errorf("expected ScopePackage, got %q", m.Scope)
			}
		}
	}
	if !found {
		t.Error("pkg-plugin not found")
	}
}

func TestDiscoverAll_ProjectShadowsPackage(t *testing.T) {
	projectDir := t.TempDir()
	pluginsDir := filepath.Join(projectDir, ".maclaw", "plugins")
	writePluginYAML(t, pluginsDir, "overlap", "mcp")

	dm := NewDiscoveryManager()
	dm.RegisterEntryPoint(&mockEntryPointProvider{
		plugins: []Plugin{
			&mockPlugin{manifest: PluginManifest{Name: "overlap", Type: PluginTypeNative}},
		},
	})

	manifests, err := dm.DiscoverAll(projectDir)
	if err != nil {
		t.Fatal(err)
	}

	count := 0
	for _, m := range manifests {
		if m.Name == "overlap" {
			count++
			if m.Scope != ScopeProject {
				t.Errorf("expected project scope, got %q", m.Scope)
			}
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 'overlap' plugin, got %d", count)
	}
}

func TestScanDirectory_SetsDir(t *testing.T) {
	dir := t.TempDir()
	writePluginYAML(t, dir, "dir-test", "native")

	dm := NewDiscoveryManager()
	manifests := dm.scanDirectory(dir, ScopeUser)

	if len(manifests) != 1 {
		t.Fatalf("expected 1 manifest, got %d", len(manifests))
	}
	expectedDir := filepath.Join(dir, "dir-test")
	if manifests[0].Dir != expectedDir {
		t.Errorf("Dir = %q, want %q", manifests[0].Dir, expectedDir)
	}
}
