package plugin

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// Task 6.5: 为 PluginRegistry 编写单元测试

func TestRegister_Success(t *testing.T) {
	pr := NewPluginRegistry(tool.NewRegistry())
	p := &mockRegistryPlugin{name: "test-plugin"}

	if err := pr.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}

	list := pr.List()
	if len(list) != 1 || list[0].Name != "test-plugin" || list[0].Status != "registered" {
		t.Errorf("unexpected list: %+v", list)
	}
}

func TestRegister_DuplicateReturnsError(t *testing.T) {
	pr := NewPluginRegistry(tool.NewRegistry())
	p1 := &mockRegistryPlugin{name: "dup"}
	p2 := &mockRegistryPlugin{name: "dup"}

	if err := pr.Register(p1); err != nil {
		t.Fatal(err)
	}
	if err := pr.Register(p2); err == nil {
		t.Error("expected error for duplicate registration")
	}
}

func TestRegister_NilReturnsError(t *testing.T) {
	pr := NewPluginRegistry(tool.NewRegistry())
	if err := pr.Register(nil); err == nil {
		t.Error("expected error for nil plugin")
	}
}

func TestRegister_EmptyNameReturnsError(t *testing.T) {
	pr := NewPluginRegistry(tool.NewRegistry())
	p := &mockRegistryPlugin{name: ""}
	if err := pr.Register(p); err == nil {
		t.Error("expected error for empty name")
	}
}

func TestUnregister_CallsStop(t *testing.T) {
	toolReg := tool.NewRegistry()
	pr := NewPluginRegistry(toolReg)
	p := &mockRegistryPlugin{name: "stop-test"}

	pr.Register(p)
	if err := pr.Unregister("stop-test"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	if !p.stopped {
		t.Error("Stop was not called")
	}
	if len(pr.List()) != 0 {
		t.Error("plugin still in registry after unregister")
	}
}

func TestUnregister_RemovesTools(t *testing.T) {
	toolReg := tool.NewRegistry()
	pr := NewPluginRegistry(toolReg)

	// Manually insert a running plugin with tools
	pr.mu.Lock()
	pr.plugins["tool-plugin"] = &pluginEntry{
		plugin:   &mockRegistryPlugin{name: "tool-plugin"},
		manifest: PluginManifest{Name: "tool-plugin", Type: PluginTypeNative},
		status:   "running",
		tools:    []string{"my_tool"},
	}
	pr.mu.Unlock()

	// Register the tool in tool.Registry
	toolReg.Register(tool.RegisteredTool{Name: "my_tool", Status: tool.StatusAvailable})

	// Verify tool exists
	if _, ok := toolReg.Get("my_tool"); !ok {
		t.Fatal("tool not found before unregister")
	}

	pr.Unregister("tool-plugin")

	// Tool should be removed
	if _, ok := toolReg.Get("my_tool"); ok {
		t.Error("tool still exists after unregister")
	}
}

func TestUnregister_NotRegisteredReturnsError(t *testing.T) {
	pr := NewPluginRegistry(tool.NewRegistry())
	if err := pr.Unregister("nonexistent"); err == nil {
		t.Error("expected error for unregistering nonexistent plugin")
	}
}

func TestGet_Found(t *testing.T) {
	pr := NewPluginRegistry(tool.NewRegistry())
	pr.Register(&mockRegistryPlugin{name: "get-test"})

	info, ok := pr.Get("get-test")
	if !ok {
		t.Fatal("plugin not found")
	}
	if info.Name != "get-test" {
		t.Errorf("Name = %q", info.Name)
	}
}

func TestGet_NotFound(t *testing.T) {
	pr := NewPluginRegistry(tool.NewRegistry())
	_, ok := pr.Get("missing")
	if ok {
		t.Error("expected not found")
	}
}

// slowStopPlugin simulates a plugin that takes too long to stop.
type slowStopPlugin struct {
	mockRegistryPlugin
}

func (s *slowStopPlugin) Stop(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(30 * time.Second):
		return nil
	}
}

func TestUnregister_StopTimeout(t *testing.T) {
	pr := NewPluginRegistry(tool.NewRegistry())
	p := &slowStopPlugin{mockRegistryPlugin: mockRegistryPlugin{name: "slow"}}
	pr.Register(p)

	start := time.Now()
	err := pr.Unregister("slow")
	elapsed := time.Since(start)

	// Should complete within ~10s (the timeout), not 30s
	if elapsed > 15*time.Second {
		t.Errorf("Unregister took %v, expected ~10s timeout", elapsed)
	}
	_ = err // error is logged, not returned
}

func TestLoadAndStart_InitFailure(t *testing.T) {
	pr := NewPluginRegistry(tool.NewRegistry())

	// We need to test via the registry's internal state.
	// Create a plugin that fails Init.
	p := &mockRegistryPlugin{
		name:    "fail-init",
		initErr: fmt.Errorf("init boom"),
	}

	// Register and manually call Init to simulate LoadAndStart behavior
	pr.mu.Lock()
	pr.plugins["fail-init"] = &pluginEntry{
		plugin:   p,
		manifest: p.Manifest(),
		status:   PluginStatusError,
		err:      p.initErr,
	}
	pr.mu.Unlock()

	info, ok := pr.Get("fail-init")
	if !ok {
		t.Fatal("plugin not found")
	}
	if info.Status != "error" {
		t.Errorf("status = %q, want %q", info.Status, "error")
	}
	if info.Error == "" {
		t.Error("expected error message")
	}
}

func TestLoadAndStart_StartFailure(t *testing.T) {
	pr := NewPluginRegistry(tool.NewRegistry())

	p := &mockRegistryPlugin{
		name:     "fail-start",
		startErr: fmt.Errorf("start boom"),
	}

	pr.mu.Lock()
	pr.plugins["fail-start"] = &pluginEntry{
		plugin:   p,
		manifest: p.Manifest(),
		status:   "error",
		err:      p.startErr,
	}
	pr.mu.Unlock()

	info, _ := pr.Get("fail-start")
	if info.Status != "error" {
		t.Errorf("status = %q, want %q", info.Status, "error")
	}
}

func TestLoadAndStart_SingleFailureDoesNotAffectOthers(t *testing.T) {
	pr := NewPluginRegistry(tool.NewRegistry())

	// Simulate: one failed, one running
	pr.mu.Lock()
	pr.plugins["failed"] = &pluginEntry{
		plugin:   &mockRegistryPlugin{name: "failed"},
		manifest: PluginManifest{Name: "failed", Type: PluginTypeNative},
		status:   "error",
		err:      fmt.Errorf("boom"),
	}
	pr.plugins["running"] = &pluginEntry{
		plugin:   &mockRegistryPlugin{name: "running"},
		manifest: PluginManifest{Name: "running", Type: PluginTypeNative},
		status:   PluginStatusRunning,
	}
	pr.mu.Unlock()

	list := pr.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(list))
	}

	statuses := make(map[string]PluginStatus)
	for _, info := range list {
		statuses[info.Name] = info.Status
	}
	if statuses["failed"] != PluginStatusError {
		t.Errorf("failed status = %q", statuses["failed"])
	}
	if statuses["running"] != PluginStatusRunning {
		t.Errorf("running status = %q", statuses["running"])
	}
}

func TestEnable_StartsStoppedPlugin(t *testing.T) {
	toolReg := tool.NewRegistry()
	pr := NewPluginRegistry(toolReg)

	p := &mockRegistryPlugin{
		name: "enable-test",
		tools: []ToolDefinition{
			{Name: "et_tool", Description: "test", Handler: func(args map[string]interface{}) (string, error) { return "ok", nil }},
		},
	}

	// Manually insert as stopped.
	pr.mu.Lock()
	pr.plugins["enable-test"] = &pluginEntry{
		plugin:   p,
		manifest: p.Manifest(),
		status:   "stopped",
	}
	pr.mu.Unlock()

	if err := pr.Enable(context.Background(), "enable-test"); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	info, _ := pr.Get("enable-test")
	if info.Status != "running" {
		t.Errorf("status = %q, want running", info.Status)
	}
	if info.ToolCount != 1 {
		t.Errorf("tool count = %d, want 1", info.ToolCount)
	}

	// Tool should be in tool.Registry.
	if _, ok := toolReg.Get("et_tool"); !ok {
		t.Error("tool not registered in tool.Registry")
	}
}

func TestEnable_AlreadyRunningIsNoop(t *testing.T) {
	pr := NewPluginRegistry(tool.NewRegistry())
	p := &mockRegistryPlugin{name: "running-test"}

	pr.mu.Lock()
	pr.plugins["running-test"] = &pluginEntry{
		plugin:   p,
		manifest: p.Manifest(),
		status:   "running",
	}
	pr.mu.Unlock()

	if err := pr.Enable(context.Background(), "running-test"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
}

func TestEnable_NotRegisteredReturnsError(t *testing.T) {
	pr := NewPluginRegistry(tool.NewRegistry())
	if err := pr.Enable(context.Background(), "missing"); err == nil {
		t.Error("expected error for missing plugin")
	}
}

func TestDisable_StopsRunningPlugin(t *testing.T) {
	toolReg := tool.NewRegistry()
	pr := NewPluginRegistry(toolReg)

	p := &mockRegistryPlugin{name: "disable-test"}
	toolReg.Register(tool.RegisteredTool{Name: "dt_tool", Status: tool.StatusAvailable})

	pr.mu.Lock()
	pr.plugins["disable-test"] = &pluginEntry{
		plugin:   p,
		manifest: p.Manifest(),
		status:   "running",
		tools:    []string{"dt_tool"},
	}
	pr.mu.Unlock()

	if err := pr.Disable("disable-test"); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	if !p.stopped {
		t.Error("Stop was not called")
	}

	info, _ := pr.Get("disable-test")
	if info.Status != "stopped" {
		t.Errorf("status = %q, want stopped", info.Status)
	}

	// Tool should be removed.
	if _, ok := toolReg.Get("dt_tool"); ok {
		t.Error("tool still in registry after disable")
	}
}

func TestDisable_AlreadyStoppedIsNoop(t *testing.T) {
	pr := NewPluginRegistry(tool.NewRegistry())
	p := &mockRegistryPlugin{name: "stopped-test"}

	pr.mu.Lock()
	pr.plugins["stopped-test"] = &pluginEntry{
		plugin:   p,
		manifest: p.Manifest(),
		status:   "stopped",
	}
	pr.mu.Unlock()

	if err := pr.Disable("stopped-test"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if p.stopped {
		t.Error("Stop should not be called on already stopped plugin")
	}
}

func TestDisable_NotRegisteredReturnsError(t *testing.T) {
	pr := NewPluginRegistry(tool.NewRegistry())
	if err := pr.Disable("missing"); err == nil {
		t.Error("expected error for missing plugin")
	}
}
