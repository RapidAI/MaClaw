package skill

import (
	"path/filepath"
	"testing"
)

func TestResolveDiskSkillSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		declared  string
		skillDir  string
		skillName string
		want      string
	}{
		{name: "explicit learned", declared: "learned", skillDir: "/skills/foo", skillName: "foo", want: "learned"},
		{name: "explicit crafted case", declared: "CRAFTED", skillDir: "/skills/foo", skillName: "foo", want: "crafted"},
		{name: "crafted subdir", declared: "", skillDir: filepath.Join("/home", ".maclaw", "data", "skills", "crafted", "my-tool"), skillName: "my-tool", want: "crafted"},
		{name: "craft prefix legacy", declared: "", skillDir: filepath.Join("/skills", "craft-task-2c94a115"), skillName: "craft_task_2c94a115", want: "learned"},
		{name: "craft kebab dir name", declared: "", skillDir: filepath.Join("/skills", "craft-task-2c94a115"), skillName: "craft-task-2c94a115", want: "learned"},
		{name: "normal file", declared: "", skillDir: filepath.Join("/skills", "contract-review"), skillName: "contract-review", want: "file"},
		{name: "explicit file still craft prefix", declared: "file", skillDir: "/skills/x", skillName: "craft_task_abc", want: "learned"},
		{name: "non-file declared preserved", declared: "hub", skillDir: "/skills/craft_x", skillName: "craft_x", want: "hub"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveDiskSkillSource(tt.declared, tt.skillDir, tt.skillName)
			if got != tt.want {
				t.Fatalf("ResolveDiskSkillSource(%q, %q, %q) = %q, want %q",
					tt.declared, tt.skillDir, tt.skillName, got, tt.want)
			}
		})
	}
}

func TestSkillYAMLDeclaredSourceFromFieldAndExtra(t *testing.T) {
	t.Parallel()

	sf := &SkillYAMLFile{Source: "learned"}
	if got := skillYAMLDeclaredSource(sf); got != "learned" {
		t.Fatalf("field source = %q, want learned", got)
	}

	sf2 := &SkillYAMLFile{Extra: map[string]any{"source": "crafted"}}
	if got := skillYAMLDeclaredSource(sf2); got != "crafted" {
		t.Fatalf("extra source = %q, want crafted", got)
	}
}

func TestParseSkillYAMLFilePreservesSource(t *testing.T) {
	t.Parallel()

	data := []byte(`name: craft_task_demo
description: demo
source: learned
status: active
triggers:
  - demo
steps:
  - action: bash
    params:
      command: echo hi
`)
	sf, err := ParseSkillYAMLFile(data)
	if err != nil {
		t.Fatalf("ParseSkillYAMLFile: %v", err)
	}
	if sf.Source != "learned" {
		t.Fatalf("Source = %q, want learned", sf.Source)
	}
}
