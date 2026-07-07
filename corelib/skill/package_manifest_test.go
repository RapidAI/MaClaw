package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateAndVerifyPackageManifest(t *testing.T) {
	dir := t.TempDir()

	// Create some files
	os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("id: test.skill\nname: test\n"), 0644)
	os.MkdirAll(filepath.Join(dir, "scripts"), 0755)
	os.WriteFile(filepath.Join(dir, "scripts", "run.py"), []byte("print('hello')"), 0644)

	// Generate manifest
	manifest, err := GeneratePackageManifest(dir, "test.skill", "1.0.0")
	if err != nil {
		t.Fatalf("GeneratePackageManifest: %v", err)
	}
	if manifest.SkillID != "test.skill" {
		t.Errorf("SkillID = %q, want test.skill", manifest.SkillID)
	}
	if manifest.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", manifest.Version)
	}
	if len(manifest.Files) != 2 {
		t.Errorf("Files count = %d, want 2", len(manifest.Files))
	}
	if _, ok := manifest.Files["skill.yaml"]; !ok {
		t.Error("manifest missing skill.yaml")
	}
	if _, ok := manifest.Files["scripts/run.py"]; !ok {
		t.Error("manifest missing scripts/run.py")
	}

	// Write manifest
	if err := WritePackageManifest(dir, manifest); err != nil {
		t.Fatalf("WritePackageManifest: %v", err)
	}

	// Verify (should pass)
	if err := VerifyPackageIntegrity(dir, manifest); err != nil {
		t.Fatalf("VerifyPackageIntegrity should pass: %v", err)
	}

	// Tamper with a file
	os.WriteFile(filepath.Join(dir, "scripts", "run.py"), []byte("print('tampered')"), 0644)

	// Verify (should fail)
	if err := VerifyPackageIntegrity(dir, manifest); err == nil {
		t.Fatal("VerifyPackageIntegrity should fail after tampering")
	}

	// Delete a file
	os.Remove(filepath.Join(dir, "scripts", "run.py"))

	// Verify (should fail with MISSING)
	err = VerifyPackageIntegrity(dir, manifest)
	if err == nil {
		t.Fatal("VerifyPackageIntegrity should fail after deletion")
	}
}

func TestReadPackageManifest_NotExists(t *testing.T) {
	dir := t.TempDir()
	manifest, err := ReadPackageManifest(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifest != nil {
		t.Fatal("expected nil manifest for dir without manifest file")
	}
}

func TestGeneratePackageManifest_SkipsRuntimeFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: test\n"), 0644)
	os.WriteFile(filepath.Join(dir, "upload_status.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(dir, "quality_status.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(dir, "skill.yaml.bak"), []byte("old"), 0644)
	os.MkdirAll(filepath.Join(dir, "__pycache__"), 0755)
	os.WriteFile(filepath.Join(dir, "__pycache__", "cache.pyc"), []byte("bytecode"), 0644)

	manifest, err := GeneratePackageManifest(dir, "test.skill", "1.0.0")
	if err != nil {
		t.Fatalf("GeneratePackageManifest: %v", err)
	}

	// Only skill.yaml should be in the manifest
	if len(manifest.Files) != 1 {
		t.Errorf("Files count = %d, want 1 (only skill.yaml). Got: %v", len(manifest.Files), manifest.Files)
	}
	if _, ok := manifest.Files["skill.yaml"]; !ok {
		t.Error("manifest should contain skill.yaml")
	}
}

func TestVerifyPackageIntegrity_NilManifest(t *testing.T) {
	// Nil manifest = legacy skill, verification passes
	if err := VerifyPackageIntegrity("/tmp", nil); err != nil {
		t.Errorf("nil manifest should pass: %v", err)
	}
}
