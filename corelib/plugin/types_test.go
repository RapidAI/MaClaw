package plugin

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

// Task 1.3: 为核心类型编写单元测试

func TestPluginType_Constants(t *testing.T) {
	if PluginTypeMCP != "mcp" {
		t.Errorf("PluginTypeMCP = %q, want %q", PluginTypeMCP, "mcp")
	}
	if PluginTypeLocalMCP != "local_mcp" {
		t.Errorf("PluginTypeLocalMCP = %q, want %q", PluginTypeLocalMCP, "local_mcp")
	}
	if PluginTypeNLSkill != "nlskill" {
		t.Errorf("PluginTypeNLSkill = %q, want %q", PluginTypeNLSkill, "nlskill")
	}
	if PluginTypeNative != "native" {
		t.Errorf("PluginTypeNative = %q, want %q", PluginTypeNative, "native")
	}
}

func TestPluginScope_Constants(t *testing.T) {
	if ScopeUser != "user" {
		t.Errorf("ScopeUser = %q, want %q", ScopeUser, "user")
	}
	if ScopeProject != "project" {
		t.Errorf("ScopeProject = %q, want %q", ScopeProject, "project")
	}
	if ScopePackage != "package" {
		t.Errorf("ScopePackage = %q, want %q", ScopePackage, "package")
	}
	if ScopeBuiltin != "builtin" {
		t.Errorf("ScopeBuiltin = %q, want %q", ScopeBuiltin, "builtin")
	}
}

func TestPluginManifest_JSONSerialization(t *testing.T) {
	m := PluginManifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    PluginTypeMCP,
		Scope:   ScopeUser,
		Author:  "dev",
		Tags:    []string{"a", "b"},
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var got PluginManifest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got.Name != m.Name || got.Version != m.Version || got.Type != m.Type || got.Scope != m.Scope {
		t.Errorf("JSON round-trip mismatch: got %+v", got)
	}
}

func TestPluginManifest_YAMLSerialization(t *testing.T) {
	m := PluginManifest{
		Name:    "yaml-test",
		Version: "2.0.0",
		Type:    PluginTypeNLSkill,
		Author:  "tester",
	}
	data, err := yaml.Marshal(m)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	var got PluginManifest
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if got.Name != m.Name || got.Version != m.Version || got.Type != m.Type {
		t.Errorf("YAML round-trip mismatch: got %+v", got)
	}
}

func TestHealthStatus_JSONOmitEmpty(t *testing.T) {
	hs := HealthStatus{Status: "healthy"}
	data, _ := json.Marshal(hs)
	s := string(data)
	if s != `{"status":"healthy"}` {
		t.Errorf("expected message omitted, got %s", s)
	}
}

func TestPluginInfo_JSONSerialization(t *testing.T) {
	info := PluginInfo{
		Name:      "info-test",
		Type:      PluginTypeMCP,
		Status:    "running",
		ToolCount: 3,
		HookCount: 1,
		Health:    HealthStatus{Status: "healthy"},
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var got PluginInfo
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got.ToolCount != 3 || got.HookCount != 1 || got.Health.Status != "healthy" {
		t.Errorf("round-trip mismatch: got %+v", got)
	}
}
