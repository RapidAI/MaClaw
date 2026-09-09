package skill

import (
	"os"
	"path/filepath"
	"strings"
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

func TestVerifyPackageIntegrityRejectsTraversalAndSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "run.sh"), []byte("echo ok\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../outside.sh", "/absolute.sh"} {
		manifest := &PackageManifest{Files: map[string]string{name: strings.Repeat("0", 64)}}
		if err := VerifyPackageIntegrity(root, manifest); err == nil {
			t.Fatalf("VerifyPackageIntegrity(%q) succeeded, want invalid-path error", name)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "outside.sh"), []byte("secret\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside.sh"), filepath.Join(root, "link.sh")); err == nil {
		manifest := &PackageManifest{Files: map[string]string{
			"run.sh":  strings.Repeat("0", 64),
			"link.sh": strings.Repeat("0", 64),
		}}
		if verifyErr := VerifyPackageIntegrity(root, manifest); verifyErr == nil || !strings.Contains(strings.ToLower(verifyErr.Error()), "symlink") {
			t.Fatalf("symlink manifest error = %v, want symlink rejection", verifyErr)
		}
	}
}

func TestVerifyPackageIntegrityRejectsNormalizedDuplicatePath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "run.sh"), []byte("echo ok\n"), 0644); err != nil {
		t.Fatal(err)
	}
	manifest := &PackageManifest{Files: map[string]string{
		"run.sh":           strings.Repeat("0", 64),
		"nested/../run.sh": strings.Repeat("0", 64),
	}}
	if err := VerifyPackageIntegrity(root, manifest); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate path verification error = %v", err)
	}
}

func TestVerifyPackageIntegrityRejectsEmptyManifest(t *testing.T) {
	if err := VerifyPackageIntegrity(t.TempDir(), &PackageManifest{Files: map[string]string{}}); err == nil {
		t.Fatal("empty manifest unexpectedly accepted")
	}
}

func TestVerifyPackageIntegrityRejectsUnexpectedFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "run.sh"), []byte("echo ok\n"), 0644); err != nil {
		t.Fatal(err)
	}
	manifest, err := GeneratePackageManifest(root, "demo.skill", "1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "payload.bin"), []byte("unexpected"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPackageIntegrity(root, manifest); err == nil || !strings.Contains(err.Error(), "unexpected file") {
		t.Fatalf("unexpected file verification error = %v", err)
	}
}

func TestVerifyPackageIntegrityAcceptsGeneratedManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "run.sh"), []byte("echo ok\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".cache"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".cache", "state"), []byte("runtime"), 0644); err != nil {
		t.Fatal(err)
	}
	manifest, err := GeneratePackageManifest(root, "demo.skill", "1")
	if err != nil {
		t.Fatal(err)
	}
	if err := WritePackageManifest(root, manifest); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPackageIntegrity(root, manifest); err != nil {
		t.Fatalf("generated manifest verification failed: %v", err)
	}
}
