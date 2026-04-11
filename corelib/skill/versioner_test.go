package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVersioner_BackupCurrent(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "skill.yaml")
	if err := os.WriteFile(yamlPath, []byte("name: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := &Versioner{}
	ver, err := v.BackupCurrent(dir)
	if err != nil {
		t.Fatalf("BackupCurrent failed: %v", err)
	}
	if ver != 1 {
		t.Errorf("expected version 1, got %d", ver)
	}

	// Verify backup file exists.
	backupPath := filepath.Join(dir, "skill.yaml.v1")
	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("backup file not found: %v", err)
	}
	if string(data) != "name: test\n" {
		t.Errorf("backup content mismatch: %q", string(data))
	}

	// Second backup should be v2.
	ver2, err := v.BackupCurrent(dir)
	if err != nil {
		t.Fatalf("second BackupCurrent failed: %v", err)
	}
	if ver2 != 2 {
		t.Errorf("expected version 2, got %d", ver2)
	}
}

func TestVersioner_BackupCurrent_NoSkillYaml(t *testing.T) {
	dir := t.TempDir()
	v := &Versioner{}
	_, err := v.BackupCurrent(dir)
	if err == nil {
		t.Fatal("expected error when skill.yaml doesn't exist")
	}
}

func TestVersioner_LatestVersion_NoVersions(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: test\n"), 0o644)

	v := &Versioner{}
	if got := v.LatestVersion(dir); got != 0 {
		t.Errorf("expected 0 for no versions, got %d", got)
	}
}

func TestVersioner_LatestVersion_WithVersions(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: test\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "skill.yaml.v1"), []byte("v1"), 0o644)
	os.WriteFile(filepath.Join(dir, "skill.yaml.v3"), []byte("v3"), 0o644)
	os.WriteFile(filepath.Join(dir, "skill.yaml.v2"), []byte("v2"), 0o644)

	v := &Versioner{}
	if got := v.LatestVersion(dir); got != 3 {
		t.Errorf("expected 3, got %d", got)
	}
}

func TestVersioner_CleanOldVersions(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("current"), 0o644)
	for i := 1; i <= 7; i++ {
		os.WriteFile(filepath.Join(dir, "skill.yaml.v"+string(rune('0'+i))), []byte("old"), 0o644)
	}

	v := &Versioner{}
	if err := v.CleanOldVersions(dir, 3); err != nil {
		t.Fatalf("CleanOldVersions failed: %v", err)
	}

	// Should keep only the latest 3 versions.
	remaining := v.listVersionFiles(dir)
	if len(remaining) != 3 {
		t.Errorf("expected 3 remaining versions, got %d", len(remaining))
	}
	// Latest should be v7.
	if remaining[len(remaining)-1].version != 7 {
		t.Errorf("latest version should be 7, got %d", remaining[len(remaining)-1].version)
	}
}

func TestVersioner_CleanOldVersions_UnderLimit(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill.yaml.v1"), []byte("v1"), 0o644)
	os.WriteFile(filepath.Join(dir, "skill.yaml.v2"), []byte("v2"), 0o644)

	v := &Versioner{}
	if err := v.CleanOldVersions(dir, 5); err != nil {
		t.Fatalf("CleanOldVersions failed: %v", err)
	}

	remaining := v.listVersionFiles(dir)
	if len(remaining) != 2 {
		t.Errorf("expected 2 remaining (under limit), got %d", len(remaining))
	}
}
