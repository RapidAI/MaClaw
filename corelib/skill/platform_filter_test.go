package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMapGOOSToPlatform(t *testing.T) {
	tests := []struct {
		goos     string
		expected string
	}{
		{"darwin", "macos"},
		{"windows", "windows"},
		{"linux", "linux"},
		{"freebsd", "freebsd"}, // unknown OS passes through
	}
	for _, tt := range tests {
		got := mapGOOSToPlatform(tt.goos)
		if got != tt.expected {
			t.Errorf("mapGOOSToPlatform(%q) = %q, want %q", tt.goos, got, tt.expected)
		}
	}
}

func TestIsSkillCompatibleWithPlatform(t *testing.T) {
	tests := []struct {
		name       string
		platforms  []string
		platform   string
		compatible bool
	}{
		{"empty platforms = universal", nil, "windows", true},
		{"empty slice = universal", []string{}, "macos", true},
		{"matching platform", []string{"windows", "linux"}, "windows", true},
		{"non-matching platform", []string{"macos"}, "windows", false},
		{"case insensitive match", []string{"Windows"}, "windows", true},
		{"single platform match", []string{"linux"}, "linux", true},
		{"all platforms listed", []string{"windows", "macos", "linux"}, "macos", true},
		{"explicit universal platform", []string{"universal"}, "windows", true},
		{"all alias platform", []string{"all"}, "linux", true},
		{"cross-platform alias", []string{"cross-platform"}, "macos", true},
		{"darwin alias matches macos", []string{"darwin"}, "macos", true},
		{"blank entries behave as unrestricted", []string{"", " "}, "windows", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSkillCompatibleWithPlatform(tt.platforms, tt.platform)
			if got != tt.compatible {
				t.Errorf("isSkillCompatibleWithPlatform(%v, %q) = %v, want %v",
					tt.platforms, tt.platform, got, tt.compatible)
			}
		})
	}
}

func TestScanSkillDir_PlatformFiltering(t *testing.T) {
	// Create a temp directory with two skill subdirectories:
	// one compatible with current platform, one not.
	root := t.TempDir()

	// Skill compatible with current platform
	compatDir := filepath.Join(root, "compat-skill")
	os.MkdirAll(compatDir, 0755)
	os.WriteFile(filepath.Join(compatDir, "skill.yaml"), []byte(
		"name: compat-skill\ndescription: compatible\nplatforms:\n  - "+currentPlatform+"\nsteps:\n  - action: bash\n    params:\n      command: echo hi\n",
	), 0644)

	// Skill incompatible with current platform
	incompatDir := filepath.Join(root, "incompat-skill")
	os.MkdirAll(incompatDir, 0755)
	// Use a platform that is definitely not the current one
	otherPlatform := "macos"
	if currentPlatform == "macos" {
		otherPlatform = "linux"
	}
	os.WriteFile(filepath.Join(incompatDir, "skill.yaml"), []byte(
		"name: incompat-skill\ndescription: incompatible\nplatforms:\n  - "+otherPlatform+"\nsteps:\n  - action: bash\n    params:\n      command: echo hi\n",
	), 0644)

	// Skill with no platform restriction (universal)
	universalDir := filepath.Join(root, "universal-skill")
	os.MkdirAll(universalDir, 0755)
	os.WriteFile(filepath.Join(universalDir, "skill.yaml"), []byte(
		"name: universal-skill\ndescription: universal\nsteps:\n  - action: bash\n    params:\n      command: echo hi\n",
	), 0644)

	// Skill with an explicit universal platform tag, as used by community skills.
	universalTaggedDir := filepath.Join(root, "universal-tagged-skill")
	os.MkdirAll(universalTaggedDir, 0755)
	os.WriteFile(filepath.Join(universalTaggedDir, "skill.yaml"), []byte(
		"name: universal-tagged-skill\ndescription: universal tag\nplatforms:\n  - universal\nsteps:\n  - action: bash\n    params:\n      command: echo hi\n",
	), 0644)

	results := ScanSkillDir(root)

	// Should include compat-skill and universal-skill, but not incompat-skill
	names := make(map[string]bool)
	for _, s := range results {
		names[s.Name] = true
	}

	if !names["compat-skill"] {
		t.Error("expected compat-skill to be included")
	}
	if names["incompat-skill"] {
		t.Error("expected incompat-skill to be excluded")
	}
	if !names["universal-skill"] {
		t.Error("expected universal-skill to be included")
	}
	if !names["universal-tagged-skill"] {
		t.Error("expected universal-tagged-skill to be included")
	}
}

func TestScanSkillDirIgnoresJSONDefinition(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "json-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.json"), []byte(`{"name":"json-skill"}`), 0644); err != nil {
		t.Fatalf("WriteFile(skill.json) error: %v", err)
	}
	results := ScanSkillDir(root)
	if len(results) != 0 {
		t.Fatalf("skill.json should not be scanned as a skill definition: %+v", results)
	}
}

func TestScanSkillDirRecognizesStandaloneMaclawAppPackage(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "pdf-translator-app")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "maclaw.app.json"), []byte(`{
  "schema":"maclaw.app.v1",
  "privateMarker":"x_maclaw_apps",
  "app":{"id":"pdf-translator","name":"PDF Translator","description":"Translate PDFs"}
}`), 0o644); err != nil {
		t.Fatalf("WriteFile(maclaw.app.json) error = %v", err)
	}

	skills := ScanSkillDir(root)
	if len(skills) != 1 {
		t.Fatalf("ScanSkillDir() returned %d skills, want 1: %#v", len(skills), skills)
	}
	got := skills[0]
	if got.Name != "pdf-translator-app" || got.Type != "instruction" || len(got.Steps) != 0 {
		t.Fatalf("scanned app-only package = %#v, want instruction-only container", got)
	}
	if got.Description != "Translate PDFs" || len(got.Triggers) != 2 {
		t.Fatalf("app metadata not projected into scan entry: %#v", got)
	}
}

func TestScanSkillDirKeepsExecutableMaclawAppWrapperExecutable(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "pdf-translator")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: pdf-translator\nsteps:\n  - action: bash\n    params:\n      command: echo translate\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "maclaw.app.json"), []byte(`{
  "schema":"maclaw.app.v1",
  "privateMarker":"x_maclaw_apps",
  "app":{"id":"pdf-translator","name":"PDF Translator"}
}`), 0o644); err != nil {
		t.Fatalf("WriteFile(maclaw.app.json) error = %v", err)
	}

	skills := ScanSkillDir(root)
	if len(skills) != 1 {
		t.Fatalf("ScanSkillDir() returned %d skills, want 1: %#v", len(skills), skills)
	}
	if skills[0].Type == "instruction" || len(skills[0].Steps) != 1 {
		t.Fatalf("executable app wrapper was degraded to app-only container: %#v", skills[0])
	}
}
