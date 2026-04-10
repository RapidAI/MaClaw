package plugin

import (
	"context"
	"testing"
)

// Task 8.5: 为 Adapter 层编写单元测试

func TestMCPPluginAdapter_Lifecycle(t *testing.T) {
	m := PluginManifest{Name: "mcp-test", Type: PluginTypeMCP, Version: "1.0.0"}
	a := NewMCPPluginAdapter(m)

	// Manifest
	if a.Manifest().Name != "mcp-test" {
		t.Errorf("Name = %q", a.Manifest().Name)
	}

	// Init
	if err := a.Init(PluginConfig{DataDir: "/tmp"}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Before Start: unhealthy, no tools
	if a.Health().Status != "unhealthy" {
		t.Errorf("pre-start health = %q", a.Health().Status)
	}
	if len(a.Tools()) != 0 {
		t.Errorf("pre-start tools = %d", len(a.Tools()))
	}

	// Start
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if a.Health().Status != "healthy" {
		t.Errorf("post-start health = %q", a.Health().Status)
	}

	// Stop
	if err := a.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if a.Health().Status != "unhealthy" {
		t.Errorf("post-stop health = %q", a.Health().Status)
	}
}

func TestLocalMCPPluginAdapter_Lifecycle(t *testing.T) {
	m := PluginManifest{Name: "local-test", Type: PluginTypeLocalMCP}
	a := NewLocalMCPPluginAdapter(m)

	if a.Manifest().Name != "local-test" {
		t.Errorf("Name = %q", a.Manifest().Name)
	}

	a.Init(PluginConfig{})
	a.Start(context.Background())

	if a.Health().Status != "healthy" {
		t.Errorf("health = %q", a.Health().Status)
	}

	a.Stop(context.Background())
	if a.Health().Status != "unhealthy" {
		t.Errorf("post-stop health = %q", a.Health().Status)
	}
	if len(a.Tools()) != 0 {
		t.Errorf("post-stop tools = %d", len(a.Tools()))
	}
}

func TestNLSkillPluginAdapter_CreatesToolOnStart(t *testing.T) {
	m := PluginManifest{
		Name:        "skill-test",
		Type:        PluginTypeNLSkill,
		Description: "A test skill",
	}
	a := NewNLSkillPluginAdapter(m)

	a.Init(PluginConfig{})

	// Before Start: no tools
	if len(a.Tools()) != 0 {
		t.Errorf("pre-start tools = %d", len(a.Tools()))
	}

	a.Start(context.Background())

	tools := a.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "skill-test" {
		t.Errorf("tool name = %q", tools[0].Name)
	}
	if tools[0].Description != "A test skill" {
		t.Errorf("tool desc = %q", tools[0].Description)
	}

	// Handler should work
	result, err := tools[0].Handler(nil)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result != "nlskill execution placeholder" {
		t.Errorf("handler result = %q", result)
	}

	// Always healthy
	if a.Health().Status != "healthy" {
		t.Errorf("health = %q", a.Health().Status)
	}

	// Stop is no-op
	if err := a.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestCreateAdapter_MCP(t *testing.T) {
	p := CreateAdapter(PluginManifest{Name: "a", Type: PluginTypeMCP})
	if p == nil {
		t.Fatal("expected non-nil for MCP")
	}
	if _, ok := p.(*MCPPluginAdapter); !ok {
		t.Errorf("expected *MCPPluginAdapter, got %T", p)
	}
}

func TestCreateAdapter_LocalMCP(t *testing.T) {
	p := CreateAdapter(PluginManifest{Name: "b", Type: PluginTypeLocalMCP})
	if p == nil {
		t.Fatal("expected non-nil for LocalMCP")
	}
	if _, ok := p.(*LocalMCPPluginAdapter); !ok {
		t.Errorf("expected *LocalMCPPluginAdapter, got %T", p)
	}
}

func TestCreateAdapter_NLSkill(t *testing.T) {
	p := CreateAdapter(PluginManifest{Name: "c", Type: PluginTypeNLSkill})
	if p == nil {
		t.Fatal("expected non-nil for NLSkill")
	}
	if _, ok := p.(*NLSkillPluginAdapter); !ok {
		t.Errorf("expected *NLSkillPluginAdapter, got %T", p)
	}
}

func TestCreateAdapter_Native(t *testing.T) {
	p := CreateAdapter(PluginManifest{Name: "d", Type: PluginTypeNative})
	if p != nil {
		t.Error("expected nil for Native (registered via EntryPointProvider)")
	}
}

func TestCreateAdapter_Unknown(t *testing.T) {
	p := CreateAdapter(PluginManifest{Name: "e", Type: "unknown"})
	if p != nil {
		t.Error("expected nil for unknown type")
	}
}
