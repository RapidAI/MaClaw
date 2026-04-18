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
}
