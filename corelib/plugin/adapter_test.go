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
	m := PluginManifest{
		Name: "local-test",
		Type: PluginTypeLocalMCP,
		RawTypeConfig: map[string]interface{}{
			"local_mcp": map[string]interface{}{
				"command": "echo",
				"args":    []interface{}{"hello"},
			},
		},
	}
	a := NewLocalMCPPluginAdapter(m)

	if a.Manifest().Name != "local-test" {
		t.Errorf("Name = %q", a.Manifest().Name)
	}

	// Init should parse config successfully.
	if err := a.Init(PluginConfig{}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Init without config section should fail.
	a2 := NewLocalMCPPluginAdapter(PluginManifest{Name: "no-config", Type: PluginTypeLocalMCP})
	if err := a2.Init(PluginConfig{}); err == nil {
		t.Error("expected error for missing local_mcp config")
	}

	// Init without command should fail.
	a3 := NewLocalMCPPluginAdapter(PluginManifest{
		Name: "no-cmd",
		Type: PluginTypeLocalMCP,
		RawTypeConfig: map[string]interface{}{
			"local_mcp": map[string]interface{}{},
		},
	})
	if err := a3.Init(PluginConfig{}); err == nil {
		t.Error("expected error for missing command")
	}

	// Health before start should be unhealthy.
	if a.Health().Status != "unhealthy" {
		t.Errorf("pre-start health = %q", a.Health().Status)
	}
	if len(a.Tools()) != 0 {
		t.Errorf("pre-start tools = %d", len(a.Tools()))
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

func TestScriptPluginAdapter_Lifecycle(t *testing.T) {
	m := PluginManifest{
		Name:        "script-test",
		Type:        PluginTypeScript,
		Description: "A test script",
		Tags:        []string{"test"},
		RawTypeConfig: map[string]interface{}{
			"script": map[string]interface{}{
				"command":     "echo",
				"args":        []interface{}{"hello"},
				"timeout":     5,
				"tool_name":   "my_echo",
				"description": "Echo tool",
				"input_schema": map[string]interface{}{
					"msg": map[string]interface{}{
						"type":        "string",
						"description": "message",
					},
				},
				"required": []interface{}{"msg"},
			},
		},
	}
	a := NewScriptPluginAdapter(m)

	if a.Manifest().Name != "script-test" {
		t.Errorf("Name = %q", a.Manifest().Name)
	}

	if err := a.Init(PluginConfig{}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Verify parsed config.
	if a.command != "echo" {
		t.Errorf("command = %q", a.command)
	}
	if a.toolName != "my_echo" {
		t.Errorf("toolName = %q", a.toolName)
	}
	if a.timeoutSec != 5 {
		t.Errorf("timeoutSec = %d", a.timeoutSec)
	}

	// Start creates tool definitions.
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	tools := a.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "my_echo" {
		t.Errorf("tool name = %q", tools[0].Name)
	}
	if tools[0].Description != "Echo tool" {
		t.Errorf("tool desc = %q", tools[0].Description)
	}
	if len(tools[0].Required) != 1 || tools[0].Required[0] != "msg" {
		t.Errorf("required = %v", tools[0].Required)
	}

	// Health should be healthy.
	if a.Health().Status != "healthy" {
		t.Errorf("health = %q", a.Health().Status)
	}

	// Stop clears tools.
	if err := a.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if len(a.Tools()) != 0 {
		t.Errorf("post-stop tools = %d", len(a.Tools()))
	}
}

func TestScriptPluginAdapter_InitErrors(t *testing.T) {
	// Missing script config section.
	a1 := NewScriptPluginAdapter(PluginManifest{Name: "no-config", Type: PluginTypeScript})
	if err := a1.Init(PluginConfig{}); err == nil {
		t.Error("expected error for missing script config")
	}

	// Missing command.
	a2 := NewScriptPluginAdapter(PluginManifest{
		Name: "no-cmd",
		Type: PluginTypeScript,
		RawTypeConfig: map[string]interface{}{
			"script": map[string]interface{}{},
		},
	})
	if err := a2.Init(PluginConfig{}); err == nil {
		t.Error("expected error for missing command")
	}
}

func TestScriptPluginAdapter_DefaultToolName(t *testing.T) {
	m := PluginManifest{
		Name:        "my-script",
		Type:        PluginTypeScript,
		Description: "desc",
		RawTypeConfig: map[string]interface{}{
			"script": map[string]interface{}{
				"command": "echo",
			},
		},
	}
	a := NewScriptPluginAdapter(m)
	a.Init(PluginConfig{})
	a.Start(context.Background())

	tools := a.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	// Should use manifest name as default tool name.
	if tools[0].Name != "my-script" {
		t.Errorf("tool name = %q, want my-script", tools[0].Name)
	}
}

func TestCreateAdapter_Script(t *testing.T) {
	p := CreateAdapter(PluginManifest{Name: "s", Type: PluginTypeScript})
	if p == nil {
		t.Fatal("expected non-nil for Script")
	}
	if _, ok := p.(*ScriptPluginAdapter); !ok {
		t.Errorf("expected *ScriptPluginAdapter, got %T", p)
	}
}
