package plugin

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// mockHealthPlugin is a minimal Plugin implementation for health check tests.
type mockHealthPlugin struct {
	mu     sync.Mutex
	name   string
	health HealthStatus
}

func (m *mockHealthPlugin) Manifest() PluginManifest {
	return PluginManifest{Name: m.name, Type: PluginTypeNative}
}
func (m *mockHealthPlugin) Init(cfg PluginConfig) error    { return nil }
func (m *mockHealthPlugin) Start(ctx context.Context) error { return nil }
func (m *mockHealthPlugin) Stop(ctx context.Context) error  { return nil }
func (m *mockHealthPlugin) Tools() []ToolDefinition         { return nil }
func (m *mockHealthPlugin) Health() HealthStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.health
}
func (m *mockHealthPlugin) setHealth(hs HealthStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.health = hs
}

func TestStartHealthCheck_StopsOnCancel(t *testing.T) {
	toolReg := tool.NewRegistry()
	pr := NewPluginRegistry(toolReg)

	ctx, cancel := context.WithCancel(context.Background())
	pr.StartHealthCheck(ctx)

	// Cancel immediately — goroutine should exit without panic.
	cancel()
	// Give goroutine a moment to exit.
	time.Sleep(50 * time.Millisecond)
}

func TestRunHealthChecks_UpdatesEntryHealth(t *testing.T) {
	toolReg := tool.NewRegistry()
	pr := NewPluginRegistry(toolReg)

	mp := &mockHealthPlugin{
		name:   "test-plugin",
		health: HealthStatus{Status: "healthy"},
	}

	// Manually insert a running entry.
	pr.mu.Lock()
	pr.plugins["test-plugin"] = &pluginEntry{
		plugin:   mp,
		manifest: mp.Manifest(),
		status:   "running",
	}
	pr.mu.Unlock()

	// Run health checks directly.
	pr.runHealthChecks()

	// Verify health was updated.
	pr.mu.RLock()
	entry := pr.plugins["test-plugin"]
	got := entry.health
	pr.mu.RUnlock()

	if got.Status != "healthy" {
		t.Errorf("expected health status %q, got %q", "healthy", got.Status)
	}

	// Change the plugin's health and re-check.
	mp.setHealth(HealthStatus{Status: "unhealthy", Message: "connection lost"})
	pr.runHealthChecks()

	pr.mu.RLock()
	got = pr.plugins["test-plugin"].health
	pr.mu.RUnlock()

	if got.Status != "unhealthy" {
		t.Errorf("expected health status %q, got %q", "unhealthy", got.Status)
	}
	if got.Message != "connection lost" {
		t.Errorf("expected message %q, got %q", "connection lost", got.Message)
	}
}

func TestRunHealthChecks_SkipsNonRunningPlugins(t *testing.T) {
	toolReg := tool.NewRegistry()
	pr := NewPluginRegistry(toolReg)

	mp := &mockHealthPlugin{
		name:   "stopped-plugin",
		health: HealthStatus{Status: "healthy"},
	}

	// Insert as "error" status — should be skipped.
	pr.mu.Lock()
	pr.plugins["stopped-plugin"] = &pluginEntry{
		plugin:   mp,
		manifest: mp.Manifest(),
		status:   "error",
	}
	pr.mu.Unlock()

	pr.runHealthChecks()

	// Health should remain zero-value since the plugin was skipped.
	pr.mu.RLock()
	got := pr.plugins["stopped-plugin"].health
	pr.mu.RUnlock()

	if got.Status != "" {
		t.Errorf("expected empty health status for non-running plugin, got %q", got.Status)
	}
}

func TestListAndGet_IncludeHealthStatus(t *testing.T) {
	toolReg := tool.NewRegistry()
	pr := NewPluginRegistry(toolReg)

	mp := &mockHealthPlugin{
		name:   "health-plugin",
		health: HealthStatus{Status: "degraded", Message: "slow"},
	}

	pr.mu.Lock()
	pr.plugins["health-plugin"] = &pluginEntry{
		plugin:   mp,
		manifest: mp.Manifest(),
		status:   "running",
		health:   HealthStatus{Status: "degraded", Message: "slow"},
	}
	pr.mu.Unlock()

	// Test List()
	list := pr.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(list))
	}
	if list[0].Health.Status != "degraded" {
		t.Errorf("List: expected health %q, got %q", "degraded", list[0].Health.Status)
	}

	// Test Get()
	info, ok := pr.Get("health-plugin")
	if !ok {
		t.Fatal("Get: plugin not found")
	}
	if info.Health.Status != "degraded" {
		t.Errorf("Get: expected health %q, got %q", "degraded", info.Health.Status)
	}
	if info.Health.Message != "slow" {
		t.Errorf("Get: expected message %q, got %q", "slow", info.Health.Message)
	}
}

func TestStartHealthCheck_CancelViaRegistryCancel(t *testing.T) {
	toolReg := tool.NewRegistry()
	pr := NewPluginRegistry(toolReg)

	ctx := context.Background()
	pr.StartHealthCheck(ctx)

	// The registry should have stored a cancel func.
	if pr.cancel == nil {
		t.Fatal("expected pr.cancel to be set after StartHealthCheck")
	}

	// Calling cancel should stop the goroutine without panic.
	pr.cancel()
	time.Sleep(50 * time.Millisecond)
}
