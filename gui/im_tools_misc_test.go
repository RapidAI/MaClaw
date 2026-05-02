package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindSkillDefinitionFileSupportsYAMLVariantsOnly(t *testing.T) {
	root := t.TempDir()

	ymlDir := filepath.Join(root, "yml-skill")
	if err := os.MkdirAll(ymlDir, 0755); err != nil {
		t.Fatalf("MkdirAll(ymlDir) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(ymlDir, "skill.yml"), []byte("name: yml-skill\n"), 0644); err != nil {
		t.Fatalf("WriteFile(skill.yml) error = %v", err)
	}
	path, format := findSkillDefinitionFile(ymlDir)
	if filepath.Base(path) != "skill.yml" || format != "yaml" {
		t.Fatalf("findSkillDefinitionFile(skill.yml) = (%q, %q), want skill.yml/yaml", path, format)
	}
	jsonDir := filepath.Join(root, "json-skill")
	if err := os.MkdirAll(jsonDir, 0755); err != nil {
		t.Fatalf("MkdirAll(jsonDir) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(jsonDir, "skill.json"), []byte(`{"name":"json-skill"}`), 0644); err != nil {
		t.Fatalf("WriteFile(skill.json) error = %v", err)
	}
	path, format = findSkillDefinitionFile(jsonDir)
	if path != "" || format != "" {
		t.Fatalf("findSkillDefinitionFile(skill.json) = (%q, %q), want none", path, format)
	}
}

func TestValidateSkillFileContentRejectsJSONFormat(t *testing.T) {
	data := []byte(`{"name":"compat","steps":[{"run":"echo hi"}]}`)
	if got := validateSkillFileContent(data, "json"); got == "" {
		t.Fatal("validateSkillFileContent(json) should reject retired JSON skill definitions")
	}
}
