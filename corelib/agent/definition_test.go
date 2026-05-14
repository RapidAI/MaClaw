package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAgentRegistry_BuiltinsLoaded(t *testing.T) {
	r := NewAgentRegistry()
	names := r.Names()
	if len(names) < 4 {
		t.Errorf("expected at least 4 builtins, got %d: %v", len(names), names)
	}
	// Check specific builtins exist
	for _, name := range []string{"coding_workflow", "code_reviewer", "researcher", "help"} {
		if r.Get(name) == nil {
			t.Errorf("missing builtin: %s", name)
		}
	}
}

func TestAgentRegistry_GetReturnsNilForUnknown(t *testing.T) {
	r := NewAgentRegistry()
	if r.Get("nonexistent") != nil {
		t.Error("should return nil for unknown agent")
	}
}

func TestAgentRegistry_LoadFromDir(t *testing.T) {
	dir := t.TempDir()

	// Write a YAML definition
	yaml := `name: test_agent
description: "A test agent"
system_prompt: "You are a test agent."
tools:
  - read_file
  - bash
max_rounds: 15
model: fast
sandbox: readonly
tags:
  - test
`
	if err := os.WriteFile(filepath.Join(dir, "test_agent.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	r := NewAgentRegistry(dir)
	r.Load()

	def := r.Get("test_agent")
	if def == nil {
		t.Fatal("test_agent not loaded")
	}
	if def.Description != "A test agent" {
		t.Errorf("wrong description: %s", def.Description)
	}
	if len(def.Tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(def.Tools))
	}
	if def.MaxRounds != 15 {
		t.Errorf("expected max_rounds=15, got %d", def.MaxRounds)
	}
	if def.ModelTask != "fast" {
		t.Errorf("expected model=fast, got %s", def.ModelTask)
	}
	if def.Sandbox != "readonly" {
		t.Errorf("expected sandbox=readonly, got %s", def.Sandbox)
	}
}

func TestAgentRegistry_ProjectOverridesUser(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	// User-level definition
	userYAML := `name: my_agent
description: "User version"
system_prompt: "User prompt"
`
	os.WriteFile(filepath.Join(userDir, "my_agent.yaml"), []byte(userYAML), 0644)

	// Project-level definition (same name, different content)
	projectYAML := `name: my_agent
description: "Project version"
system_prompt: "Project prompt"
`
	os.WriteFile(filepath.Join(projectDir, "my_agent.yaml"), []byte(projectYAML), 0644)

	// Project dir comes after user dir → overrides
	r := NewAgentRegistry(userDir, projectDir)
	r.Load()

	def := r.Get("my_agent")
	if def == nil {
		t.Fatal("my_agent not loaded")
	}
	if def.Description != "Project version" {
		t.Errorf("project should override user, got: %s", def.Description)
	}
}

func TestAgentRegistry_Search(t *testing.T) {
	r := NewAgentRegistry()
	results := r.Search("code")
	found := false
	for _, d := range results {
		if d.Name == "code_reviewer" {
			found = true
			break
		}
	}
	if !found {
		t.Error("search for 'code' should find code_reviewer")
	}
}

func TestAgentRegistry_SearchByTag(t *testing.T) {
	r := NewAgentRegistry()
	results := r.Search("security")
	found := false
	for _, d := range results {
		if d.Name == "code_reviewer" {
			found = true
			break
		}
	}
	if !found {
		t.Error("search for 'security' tag should find code_reviewer")
	}
}

func TestAgentDefinition_EffectiveMaxRounds(t *testing.T) {
	d := AgentDefinition{MaxRounds: 0}
	if d.EffectiveMaxRounds() != 50 {
		t.Errorf("default should be 50, got %d", d.EffectiveMaxRounds())
	}
	d.MaxRounds = 20
	if d.EffectiveMaxRounds() != 20 {
		t.Errorf("should return configured value, got %d", d.EffectiveMaxRounds())
	}
}

func TestAgentDefinition_IsReadOnly(t *testing.T) {
	d := AgentDefinition{Sandbox: "readonly"}
	if !d.IsReadOnly() {
		t.Error("readonly sandbox should be read-only")
	}
	d.Sandbox = "full"
	if d.IsReadOnly() {
		t.Error("full sandbox should not be read-only")
	}
}

func TestAgentRegistry_Register(t *testing.T) {
	r := NewAgentRegistry()
	r.Register(&AgentDefinition{
		Name:        "custom",
		Description: "Custom agent",
	})
	if r.Get("custom") == nil {
		t.Error("registered agent should be retrievable")
	}
}

func TestAgentRegistry_LoadNonexistentDir(t *testing.T) {
	r := NewAgentRegistry("/nonexistent/path/that/does/not/exist")
	err := r.Load()
	if err != nil {
		t.Errorf("nonexistent dir should not error: %v", err)
	}
}

func TestAgentRegistry_FilenameAsName(t *testing.T) {
	dir := t.TempDir()
	// YAML without name field — should use filename
	yaml := `description: "No name field"
system_prompt: "Hello"
`
	os.WriteFile(filepath.Join(dir, "auto_named.yaml"), []byte(yaml), 0644)

	r := NewAgentRegistry(dir)
	r.Load()

	def := r.Get("auto_named")
	if def == nil {
		t.Fatal("should use filename as name when name field is empty")
	}
}
