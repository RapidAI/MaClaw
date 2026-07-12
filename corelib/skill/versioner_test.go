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

func TestVersioner_RestoreVersion_WithPreBackup(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "skill.yaml")
	if err := os.WriteFile(yamlPath, []byte("name: original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := &Versioner{}
	if _, err := v.BackupCurrent(dir); err != nil { // creates v1 = original
		t.Fatal(err)
	}
	// Mutate current
	if err := os.WriteFile(yamlPath, []byte("name: broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	written, pre, err := v.RestoreVersion(dir, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if written != yamlPath {
		t.Fatalf("written=%s", written)
	}
	if pre < 2 {
		// pre-restore backup of "broken" should be v2
		t.Fatalf("preBackupVer=%d want >=2", pre)
	}
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "name: original\n" {
		t.Fatalf("restored content=%q", data)
	}
	// broken saved as pre-backup
	if _, err := os.Stat(filepath.Join(dir, "skill.yaml.v2")); err != nil {
		t.Fatalf("pre-restore backup missing: %v", err)
	}
}

func TestVersioner_RestoreLatest(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "skill.yaml")
	_ = os.WriteFile(yamlPath, []byte("v0\n"), 0o644)
	v := &Versioner{}
	_, _ = v.BackupCurrent(dir) // v1
	_ = os.WriteFile(yamlPath, []byte("v1-content\n"), 0o644)
	_, _ = v.BackupCurrent(dir) // v2 from current
	_ = os.WriteFile(yamlPath, []byte("latest-bad\n"), 0o644)

	path, restored, pre, err := v.RestoreLatest(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if restored != 2 || path != yamlPath {
		t.Fatalf("restored=%d path=%s pre=%d", restored, path, pre)
	}
	data, _ := os.ReadFile(yamlPath)
	if string(data) != "v1-content\n" {
		// v2 backup was taken from "v1-content"
		t.Fatalf("content=%q", data)
	}
}

func TestVersioner_ListVersions(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("x\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "skill.yaml.v2"), []byte("2"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "skill.yaml.v1"), []byte("1"), 0o644)
	v := &Versioner{}
	got := v.ListVersions(dir)
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("got=%v", got)
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

func TestVersioner_BackupCurrentIgnoresJSONDefinition(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "skill.json"), []byte(`{"name":"json-skill"}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	v := &Versioner{}
	if _, err := v.BackupCurrent(dir); err == nil {
		t.Fatal("BackupCurrent should reject retired skill.json definitions")
	}
}
