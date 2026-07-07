package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareSkillForUpload_GeneratesManifestWhenPortable(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("id: test-pub.my-skill\nversion: \"1.0.0\"\nname: My Skill\nsteps:\n  - action: bash\n    params:\n      command: echo hi\n"), 0644)
	os.MkdirAll(filepath.Join(dir, "scripts"), 0755)
	os.WriteFile(filepath.Join(dir, "scripts", "run.py"), []byte("print('hello')"), 0644)

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload: %v", err)
	}
	if !result.Portable() {
		t.Fatalf("expected portable, got blocking_paths=%d missing=%d", len(result.BlockingPaths), len(result.MissingFiles))
	}

	// Manifest should have been written
	manifest, err := ReadPackageManifest(dir)
	if err != nil {
		t.Fatalf("ReadPackageManifest: %v", err)
	}
	if manifest == nil {
		t.Fatal("expected manifest to be generated")
	}
	if manifest.SkillID != "test-pub.my-skill" {
		t.Errorf("manifest.SkillID = %q, want test-pub.my-skill", manifest.SkillID)
	}
	if manifest.Version != "1.0.0" {
		t.Errorf("manifest.Version = %q, want 1.0.0", manifest.Version)
	}
	if _, ok := manifest.Files["skill.yaml"]; !ok {
		t.Error("manifest should include skill.yaml")
	}
	if _, ok := manifest.Files["scripts/run.py"]; !ok {
		t.Error("manifest should include scripts/run.py")
	}
}

func TestPrepareSkillForUpload_ValidatesSkillIDFormat(t *testing.T) {
	dir := t.TempDir()
	// Invalid id format (uppercase)
	os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("id: INVALID\nname: Test\nsteps:\n  - action: bash\n    params:\n      command: echo hi\n"), 0644)

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload: %v", err)
	}
	// Should have a warning about invalid id format
	found := false
	for _, w := range result.Warnings {
		if contains(w, "skill_id") || contains(w, "格式无效") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about invalid skill_id format, got warnings: %v", result.Warnings)
	}
}

func TestPrepareSkillForUpload_SkillIDPropagatedToResult(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("id: alice-1234.pdf-tool\nname: PDF Tool\nsteps:\n  - action: bash\n    params:\n      command: echo hi\n"), 0644)

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload: %v", err)
	}
	if result.SkillID != "alice-1234.pdf-tool" {
		t.Errorf("result.SkillID = %q, want alice-1234.pdf-tool", result.SkillID)
	}
}

func TestPrepareSkillForUpload_NoManifestWhenNotPortable(t *testing.T) {
	dir := t.TempDir()
	// Reference a file outside the skill dir (non-portable)
	os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: Bad Skill\nsteps:\n  - action: bash\n    params:\n      command: python /opt/secret/run.py\n"), 0644)

	result, err := PrepareSkillForUpload(dir)
	if err != nil {
		t.Fatalf("PrepareSkillForUpload: %v", err)
	}
	if result.Portable() {
		t.Fatal("expected not portable")
	}

	// Manifest should NOT have been written (only generated when portable)
	manifest, _ := ReadPackageManifest(dir)
	if manifest != nil {
		t.Error("manifest should not be generated for non-portable skills")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
