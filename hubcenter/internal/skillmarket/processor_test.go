package skillmarket

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── Task 4.5: Processor 单元测试 ────────────────────────────────────────

func TestSafeUnzip_ValidZip(t *testing.T) {
	zipPath := createTestZip(t, map[string]string{
		"skill.yaml": "name: test\ndescription: hello\n",
		"main.py":    "print('hello')\n",
	})
	destDir := t.TempDir()
	if err := SafeUnzip(zipPath, destDir); err != nil {
		t.Fatalf("SafeUnzip failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "skill.yaml")); err != nil {
		t.Error("skill.yaml not extracted")
	}
	if _, err := os.Stat(filepath.Join(destDir, "main.py")); err != nil {
		t.Error("main.py not extracted")
	}
}

func TestSafeUnzip_InvalidZip(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "bad.zip")
	if err := os.WriteFile(tmpFile, []byte("not a zip file"), 0o644); err != nil {
		t.Fatal(err)
	}
	destDir := t.TempDir()
	if err := SafeUnzip(tmpFile, destDir); err == nil {
		t.Error("expected error for invalid zip")
	}
}

func TestSafeUnzip_TooManyFiles(t *testing.T) {
	files := make(map[string]string)
	for i := 0; i < maxFileCount+1; i++ {
		files[filepath.Join("dir", strings.Repeat("f", 5)+string(rune('a'+i%26))+strings.Repeat("x", 3))] = "x"
	}
	zipPath := createLargeFileCountZip(t, maxFileCount+1)
	destDir := t.TempDir()
	err := SafeUnzip(zipPath, destDir)
	if err == nil {
		t.Error("expected error for too many files")
	}
	if err != nil && !strings.Contains(err.Error(), "too many files") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSafeUnzip_ZipSlipPrevention(t *testing.T) {
	zipPath := createZipSlipZip(t)
	destDir := t.TempDir()
	err := SafeUnzip(zipPath, destDir)
	if err == nil {
		t.Error("expected error for zip slip attack")
	}
}

func TestSafeUnzip_SandboxCleanup(t *testing.T) {
	zipPath := createTestZip(t, map[string]string{
		"skill.yaml": "name: test\ndescription: hello\n",
	})
	sandboxDir := filepath.Join(t.TempDir(), "sandbox-test")
	if err := SafeUnzip(zipPath, sandboxDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sandboxDir); err != nil {
		t.Error("sandbox dir should exist after unzip")
	}
	os.RemoveAll(sandboxDir)
	if _, err := os.Stat(sandboxDir); !os.IsNotExist(err) {
		t.Error("sandbox dir should be removed after cleanup")
	}
}

func createTestZip(t *testing.T, files map[string]string) string {
	t.Helper()
	zipPath := filepath.Join(t.TempDir(), "test.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	for name, content := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return zipPath
}

func createLargeFileCountZip(t *testing.T, count int) string {
	t.Helper()
	zipPath := filepath.Join(t.TempDir(), "many_files.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	for i := 0; i < count; i++ {
		name := filepath.Join("files", strings.Replace(generateID(), "-", "", -1))
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write([]byte("x"))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return zipPath
}

func createZipSlipZip(t *testing.T) string {
	t.Helper()
	zipPath := filepath.Join(t.TempDir(), "slip.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	fw, err := w.Create("../../etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write([]byte("malicious"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return zipPath
}

func TestResolvePackageRoot_FlatLayout(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: test\n"), 0o644)
	got := resolvePackageRoot(dir)
	if got != dir {
		t.Errorf("expected %s, got %s", dir, got)
	}
}

func TestResolvePackageRoot_WrappedLayout(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "my-skill")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "skill.yaml"), []byte("name: test\n"), 0o644)
	got := resolvePackageRoot(dir)
	if got != sub {
		t.Errorf("expected %s, got %s", sub, got)
	}
}

func TestResolvePackageRoot_WrappedWithMacOSX(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "my-skill")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "skill.yaml"), []byte("name: test\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "__MACOSX"), 0o755)
	got := resolvePackageRoot(dir)
	if got != sub {
		t.Errorf("expected %s, got %s", sub, got)
	}
}

func TestResolvePackageRoot_MultipleDirs(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "a"), 0o755)
	os.MkdirAll(filepath.Join(dir, "b"), 0o755)
	got := resolvePackageRoot(dir)
	if got != dir {
		t.Errorf("expected fallback to %s, got %s", dir, got)
	}
}

func TestResolvePackageRoot_SubdirWithoutYaml(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "empty-skill"), 0o755)
	got := resolvePackageRoot(dir)
	if got != dir {
		t.Errorf("expected fallback to %s, got %s", dir, got)
	}
}

func TestResolvePackageRoot_WrappedWithLooseFiles(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "my-skill")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "skill.yaml"), []byte("name: test\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte{}, 0o644)
	got := resolvePackageRoot(dir)
	if got != sub {
		t.Errorf("expected %s, got %s", sub, got)
	}
}

func TestValidatePackage_RejectsLegacyPackage(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Legacy Skill\n\nUse it."), 0o644)
	os.WriteFile(filepath.Join(dir, "_meta.json"), []byte(`{"description":"legacy"}`), 0o644)

	result, err := ValidatePackage(dir)
	if err != nil {
		t.Fatalf("ValidatePackage error: %v", err)
	}
	if result.Valid {
		t.Fatalf("expected invalid result")
	}
	if len(result.Errors) == 0 || !strings.Contains(result.Errors[0].Message, "no longer supported") {
		t.Fatalf("unexpected errors: %#v", result.Errors)
	}
}

func TestValidatePackage_WrappedLayout(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "my-skill")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "skill.yaml"), []byte("name: wrapped\ndescription: test wrapped layout\n"), 0o644)

	result, err := ValidatePackage(dir)
	if err != nil {
		t.Fatalf("ValidatePackage error: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected valid, got errors: %v", result.Errors)
	}
	if result.Metadata.Name != "wrapped" {
		t.Errorf("expected name 'wrapped', got %q", result.Metadata.Name)
	}
	if result.PackageRoot != sub {
		t.Errorf("expected PackageRoot %s, got %s", sub, result.PackageRoot)
	}
}
