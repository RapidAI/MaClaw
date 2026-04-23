package main

import (
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"encoding/json"
	"testing"

)

func TestMarshalTemplate(t *testing.T) {
	tpl := remote.SessionTemplate{
		Name:        "my-template",
		Tool:        "claude",
		ProjectPath: "/home/user/project",
		ModelConfig: "claude-sonnet-4-20250514",
		YoloMode:    true,
		EnvVars:     map[string]string{"FOO": "bar"},
		CreatedAt:   "2025-01-01T00:00:00Z",
	}

	data, err := MarshalTemplate(tpl)
	if err != nil {
		t.Fatalf("MarshalTemplate: unexpected error: %v", err)
	}

	// Verify it's valid JSON
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("MarshalTemplate produced invalid JSON: %v", err)
	}

	if raw["name"] != "my-template" {
		t.Errorf("expected name=my-template, got %v", raw["name"])
	}
	if raw["tool"] != "claude" {
		t.Errorf("expected tool=claude, got %v", raw["tool"])
	}
}

func TestUnmarshalTemplate_Valid(t *testing.T) {
	input := `{"name":"test","tool":"codex","project_path":"/tmp","model_config":"gpt-4","yolo_mode":false}`

	tpl, err := UnmarshalTemplate([]byte(input))
	if err != nil {
		t.Fatalf("UnmarshalTemplate: unexpected error: %v", err)
	}
	if tpl.Name != "test" {
		t.Errorf("expected Name=test, got %s", tpl.Name)
	}
	if tpl.Tool != "codex" {
		t.Errorf("expected Tool=codex, got %s", tpl.Tool)
	}
	if tpl.ProjectPath != "/tmp" {
		t.Errorf("expected ProjectPath=/tmp, got %s", tpl.ProjectPath)
	}
}

func TestUnmarshalTemplate_MissingName(t *testing.T) {
	input := `{"tool":"claude"}`

	_, err := UnmarshalTemplate([]byte(input))
	if err == nil {
		t.Fatal("expected error for missing name, got nil")
	}
	if err.Error() != "name is required" {
		t.Errorf("expected 'name is required', got %q", err.Error())
	}
}

func TestUnmarshalTemplate_MissingTool(t *testing.T) {
	input := `{"name":"test"}`

	_, err := UnmarshalTemplate([]byte(input))
	if err == nil {
		t.Fatal("expected error for missing tool, got nil")
	}
	if err.Error() != "tool is required" {
		t.Errorf("expected 'tool is required', got %q", err.Error())
	}
}

func TestUnmarshalTemplate_InvalidJSON(t *testing.T) {
	_, err := UnmarshalTemplate([]byte(`{not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestMarshalUnmarshal_RoundTrip(t *testing.T) {
	original := remote.SessionTemplate{
		Name:        "roundtrip",
		Tool:        "gemini",
		ProjectPath: "/home/dev/app",
		ModelConfig: "gemini-pro",
		YoloMode:    true,
		EnvVars:     map[string]string{"KEY": "val"},
		CreatedAt:   "2025-06-01T12:00:00Z",
	}

	data, err := MarshalTemplate(original)
	if err != nil {
		t.Fatalf("MarshalTemplate: %v", err)
	}

	restored, err := UnmarshalTemplate(data)
	if err != nil {
		t.Fatalf("UnmarshalTemplate: %v", err)
	}

	if original.Name != restored.Name ||
		original.Tool != restored.Tool ||
		original.ProjectPath != restored.ProjectPath ||
		original.ModelConfig != restored.ModelConfig ||
		original.YoloMode != restored.YoloMode ||
		original.CreatedAt != restored.CreatedAt {
		t.Errorf("round-trip mismatch:\n  original: %+v\n  restored: %+v", original, restored)
	}

	if len(original.EnvVars) != len(restored.EnvVars) {
		t.Fatalf("EnvVars length mismatch: %d vs %d", len(original.EnvVars), len(restored.EnvVars))
	}
	for k, v := range original.EnvVars {
		if restored.EnvVars[k] != v {
			t.Errorf("EnvVars[%s]: expected %q, got %q", k, v, restored.EnvVars[k])
		}
	}
}
