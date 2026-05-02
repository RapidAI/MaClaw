package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

// TestManageSkillHandler_AllCanonicalActionsHandled verifies that the TUI
// dispatcher has a handler for every action in the canonical ManageSkillActions
// list. If a new action is added to the single source of truth but not to the
// TUI switch, this test fails.
func TestManageSkillHandler_AllCanonicalActionsHandled(t *testing.T) {
	app := &TUIApp{
		appConfig: corelib.AppConfig{},
	}
	handler := newManageSkillHandler(app)

	for _, action := range skill.ManageSkillActionNames() {
		got := handler(map[string]interface{}{"action": action})
		if strings.Contains(got, "未知 manage_skill action") {
			t.Errorf("TUI dispatcher has no handler for canonical action %q", action)
		}
	}
}

func TestFindSkillDefFileYAMLOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "skill.json"), []byte(`{"name":"json"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	path, format := findSkillDefFile(dir)
	if path != "" || format != "" {
		t.Fatalf("findSkillDefFile should ignore skill.json, got (%q, %q)", path, format)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.yml"), []byte("name: yml-skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, format = findSkillDefFile(dir)
	if filepath.Base(path) != "skill.yml" || format != "yaml" {
		t.Fatalf("findSkillDefFile(skill.yml) = (%q, %q), want skill.yml/yaml", path, format)
	}
}

func TestValidateSkillContentUsesSkillParser(t *testing.T) {
	if got := validateSkillContent([]byte("name: valid-skill\nsteps:\n  - action: bash\n    params:\n      command: echo ok\n"), "yaml"); got != "" {
		t.Fatalf("valid skill yaml rejected: %s", got)
	}
	if got := validateSkillContent([]byte("name: [unterminated\n"), "yaml"); got == "" {
		t.Fatal("invalid YAML should be rejected")
	}
	if got := validateSkillContent([]byte(`{"name":"json"}`), "json"); got == "" {
		t.Fatal("json skill definitions should be rejected")
	}
}
