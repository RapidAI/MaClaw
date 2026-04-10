package plugin

import (
	"context"
	"math/rand"
	"testing"
	"testing/quick"
)

// Task 5.4: Property 1 — 名称唯一性
// 任意数量的 manifest 经过 DiscoverAll 后，结果中 Name 唯一

func TestProperty_DiscoverAll_NameUniqueness(t *testing.T) {
	f := func(seed int64) bool {
		r := rand.New(rand.NewSource(seed))
		n := r.Intn(20) + 1

		// Generate random plugins with possible name collisions
		var plugins []Plugin
		for i := 0; i < n; i++ {
			name := randomString(r, 3+r.Intn(5))
			plugins = append(plugins, &mockPlugin{
				manifest: PluginManifest{
					Name: name,
					Type: PluginTypeNative,
				},
			})
		}

		dm := NewDiscoveryManager()
		dm.RegisterEntryPoint(&mockEntryPointProvider{plugins: plugins})

		manifests, err := dm.DiscoverAll("")
		if err != nil {
			return false
		}

		// Check uniqueness
		seen := make(map[string]bool)
		for _, m := range manifests {
			if seen[m.Name] {
				t.Logf("duplicate name: %q", m.Name)
				return false
			}
			seen[m.Name] = true
		}
		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Error(err)
	}
}

// mockPlugin for property tests is defined in discovery_test.go
// (same package, so it's accessible)

// Verify that DiscoverAll never returns more manifests than unique names provided
func TestProperty_DiscoverAll_CountBound(t *testing.T) {
	f := func(seed int64) bool {
		r := rand.New(rand.NewSource(seed))
		n := r.Intn(30) + 1

		names := make(map[string]bool)
		var plugins []Plugin
		for i := 0; i < n; i++ {
			name := randomString(r, 2+r.Intn(4))
			names[name] = true
			plugins = append(plugins, &mockPlugin{
				manifest: PluginManifest{Name: name, Type: PluginTypeNative},
			})
		}

		dm := NewDiscoveryManager()
		dm.RegisterEntryPoint(&mockEntryPointProvider{plugins: plugins})

		manifests, _ := dm.DiscoverAll("")
		return len(manifests) <= len(names)
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Error(err)
	}
}

// Ensure mockPlugin satisfies Plugin interface at compile time
var _ Plugin = (*mockPlugin)(nil)
var _ EntryPointProvider = (*mockEntryPointProvider)(nil)

// Ensure mockHealthPlugin satisfies Plugin interface (from registry_health_test.go)
var _ Plugin = (*mockHealthPlugin)(nil)

// mockRegistryPlugin for registry tests
type mockRegistryPlugin struct {
	name      string
	initErr   error
	startErr  error
	stopErr   error
	tools     []ToolDefinition
	health    HealthStatus
	stopped   bool
}

func (m *mockRegistryPlugin) Manifest() PluginManifest {
	return PluginManifest{Name: m.name, Type: PluginTypeNative}
}
func (m *mockRegistryPlugin) Init(cfg PluginConfig) error    { return m.initErr }
func (m *mockRegistryPlugin) Start(ctx context.Context) error { return m.startErr }
func (m *mockRegistryPlugin) Stop(ctx context.Context) error {
	m.stopped = true
	return m.stopErr
}
func (m *mockRegistryPlugin) Tools() []ToolDefinition { return m.tools }
func (m *mockRegistryPlugin) Health() HealthStatus     { return m.health }
