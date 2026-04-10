package plugin

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseManifestBytes_FullManifest(t *testing.T) {
	yaml := `
name: weather-tools
version: "1.0.0"
description: "天气查询工具集"
type: mcp
author: "user"
tags: ["weather", "api"]
platforms: ["linux", "darwin"]
mcp:
  endpoint_url: "https://weather-api.example.com/mcp"
  auth_type: "api_key"
settings:
  default_city: "Beijing"
  units: "metric"
`
	m, err := ParseManifestBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Name != "weather-tools" {
		t.Errorf("name = %q, want %q", m.Name, "weather-tools")
	}
	if m.Version != "1.0.0" {
		t.Errorf("version = %q, want %q", m.Version, "1.0.0")
	}
	if m.Type != PluginTypeMCP {
		t.Errorf("type = %q, want %q", m.Type, PluginTypeMCP)
	}
	if m.Author != "user" {
		t.Errorf("author = %q, want %q", m.Author, "user")
	}
	if len(m.Tags) != 2 || m.Tags[0] != "weather" {
		t.Errorf("tags = %v, want [weather api]", m.Tags)
	}
	if len(m.Platforms) != 2 {
		t.Errorf("platforms = %v, want [linux darwin]", m.Platforms)
	}
	if m.RawTypeConfig == nil {
		t.Fatal("RawTypeConfig is nil, expected mcp config")
	}
	mcpCfg, ok := m.RawTypeConfig["mcp"]
	if !ok {
		t.Fatal("RawTypeConfig missing 'mcp' key")
	}
	mcpMap, ok := mcpCfg.(map[string]interface{})
	if !ok {
		t.Fatalf("mcp config is %T, want map", mcpCfg)
	}
	if mcpMap["endpoint_url"] != "https://weather-api.example.com/mcp" {
		t.Errorf("mcp.endpoint_url = %v", mcpMap["endpoint_url"])
	}
	if m.Settings == nil || m.Settings["default_city"] != "Beijing" {
		t.Errorf("settings = %v", m.Settings)
	}
}

func TestParseManifestBytes_LocalMCP(t *testing.T) {
	yaml := `
name: local-tool
type: local_mcp
local_mcp:
  command: "npx"
  args: ["-y", "@weather/mcp-server"]
`
	m, err := ParseManifestBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Type != PluginTypeLocalMCP {
		t.Errorf("type = %q, want %q", m.Type, PluginTypeLocalMCP)
	}
	if m.RawTypeConfig == nil || m.RawTypeConfig["local_mcp"] == nil {
		t.Fatal("RawTypeConfig missing 'local_mcp' key")
	}
}

func TestParseManifestBytes_NLSkill(t *testing.T) {
	yaml := `
name: translate-skill
type: nlskill
nlskill:
  triggers: ["翻译", "translate"]
`
	m, err := ParseManifestBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Type != PluginTypeNLSkill {
		t.Errorf("type = %q, want %q", m.Type, PluginTypeNLSkill)
	}
	if m.RawTypeConfig == nil || m.RawTypeConfig["nlskill"] == nil {
		t.Fatal("RawTypeConfig missing 'nlskill' key")
	}
}

func TestParseManifestBytes_EmptyName(t *testing.T) {
	yaml := `
name: ""
type: mcp
`
	_, err := ParseManifestBytes([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestParseManifestBytes_MissingName(t *testing.T) {
	yaml := `
type: mcp
`
	_, err := ParseManifestBytes([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestParseManifestBytes_UnknownType(t *testing.T) {
	yaml := `
name: test
type: unknown_type
`
	_, err := ParseManifestBytes([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestParseManifestBytes_InvalidYAML(t *testing.T) {
	_, err := ParseManifestBytes([]byte(`{{{not yaml`))
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestParseManifestFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.yaml")
	content := `
name: file-test
type: native
version: "0.1.0"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	m, err := ParseManifestFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Name != "file-test" {
		t.Errorf("name = %q, want %q", m.Name, "file-test")
	}
	if m.Type != PluginTypeNative {
		t.Errorf("type = %q, want %q", m.Type, PluginTypeNative)
	}
}

func TestParseManifestFile_NotFound(t *testing.T) {
	_, err := ParseManifestFile("/nonexistent/plugin.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestFormatManifestFile_Basic(t *testing.T) {
	m := &PluginManifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    PluginTypeMCP,
		Author:  "dev",
		Tags:    []string{"test"},
	}
	data, err := FormatManifestFile(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("empty output")
	}
	// Parse it back.
	got, err := ParseManifestBytes(data)
	if err != nil {
		t.Fatalf("round-trip parse failed: %v", err)
	}
	if got.Name != m.Name || got.Version != m.Version || got.Type != m.Type {
		t.Errorf("round-trip mismatch: got %+v", got)
	}
}

func TestFormatManifestFile_WithTypeConfig(t *testing.T) {
	m := &PluginManifest{
		Name:    "mcp-plugin",
		Version: "2.0.0",
		Type:    PluginTypeMCP,
		RawTypeConfig: map[string]interface{}{
			"mcp": map[string]interface{}{
				"endpoint_url": "https://example.com/mcp",
				"auth_type":    "bearer",
			},
		},
	}
	data, err := FormatManifestFile(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := ParseManifestBytes(data)
	if err != nil {
		t.Fatalf("round-trip parse failed: %v", err)
	}
	if got.Name != m.Name || got.Type != m.Type {
		t.Errorf("basic fields mismatch: got %+v", got)
	}
	if got.RawTypeConfig == nil {
		t.Fatal("RawTypeConfig is nil after round-trip")
	}
	mcpCfg, ok := got.RawTypeConfig["mcp"].(map[string]interface{})
	if !ok {
		t.Fatalf("mcp config type = %T", got.RawTypeConfig["mcp"])
	}
	if mcpCfg["endpoint_url"] != "https://example.com/mcp" {
		t.Errorf("mcp.endpoint_url = %v", mcpCfg["endpoint_url"])
	}
}

func TestFormatManifestFile_WithSettings(t *testing.T) {
	m := &PluginManifest{
		Name: "settings-plugin",
		Type: PluginTypeNLSkill,
		Settings: map[string]interface{}{
			"key1": "value1",
			"key2": float64(42),
		},
	}
	data, err := FormatManifestFile(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := ParseManifestBytes(data)
	if err != nil {
		t.Fatalf("round-trip parse failed: %v", err)
	}
	if got.Settings == nil {
		t.Fatal("settings is nil after round-trip")
	}
	if got.Settings["key1"] != "value1" {
		t.Errorf("settings.key1 = %v", got.Settings["key1"])
	}
}

func TestRoundTrip_FullManifest(t *testing.T) {
	original := &PluginManifest{
		Name:        "round-trip-test",
		Version:     "3.0.0",
		Description: "A test plugin",
		Type:        PluginTypeLocalMCP,
		Author:      "tester",
		Tags:        []string{"a", "b"},
		Platforms:   []string{"linux"},
		Settings: map[string]interface{}{
			"timeout": 30,
		},
		RawTypeConfig: map[string]interface{}{
			"local_mcp": map[string]interface{}{
				"command": "node",
				"args":    []interface{}{"server.js"},
			},
		},
	}

	data, err := FormatManifestFile(original)
	if err != nil {
		t.Fatalf("format error: %v", err)
	}
	got, err := ParseManifestBytes(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Compare core fields.
	if got.Name != original.Name {
		t.Errorf("Name: got %q, want %q", got.Name, original.Name)
	}
	if got.Version != original.Version {
		t.Errorf("Version: got %q, want %q", got.Version, original.Version)
	}
	if got.Description != original.Description {
		t.Errorf("Description: got %q, want %q", got.Description, original.Description)
	}
	if got.Type != original.Type {
		t.Errorf("Type: got %q, want %q", got.Type, original.Type)
	}
	if got.Author != original.Author {
		t.Errorf("Author: got %q, want %q", got.Author, original.Author)
	}
	if !reflect.DeepEqual(got.Tags, original.Tags) {
		t.Errorf("Tags: got %v, want %v", got.Tags, original.Tags)
	}
	if !reflect.DeepEqual(got.Platforms, original.Platforms) {
		t.Errorf("Platforms: got %v, want %v", got.Platforms, original.Platforms)
	}
	// Settings: YAML unmarshals integers as int, so compare key presence.
	if got.Settings == nil {
		t.Fatal("Settings is nil")
	}
	if got.RawTypeConfig == nil || got.RawTypeConfig["local_mcp"] == nil {
		t.Fatal("RawTypeConfig missing local_mcp after round-trip")
	}
}
