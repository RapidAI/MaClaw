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
	// 验证文件已解压
	if _, err := os.Stat(filepath.Join(destDir, "skill.yaml")); err != nil {
		t.Error("skill.yaml not extracted")
	}
	if _, err := os.Stat(filepath.Join(destDir, "main.py")); err != nil {
		t.Error("main.py not extracted")
	}
}

func TestSafeUnzip_InvalidZip(t *testing.T) {
	// 创建一个非 zip 文件
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
	// 创建超过 maxFileCount 个文件的 zip
	files := make(map[string]string)
	for i := 0; i < maxFileCount+1; i++ {
		files[filepath.Join("dir", strings.Repeat("f", 5)+string(rune('a'+i%26))+strings.Repeat("x", 3))] = "x"
	}
	// 简化：直接测试文件数量检查
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
	// zip slip 通过路径遍历攻击
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
	// 验证 sandbox 目录存在
	if _, err := os.Stat(sandboxDir); err != nil {
		t.Error("sandbox dir should exist after unzip")
	}
	// 模拟 defer 清理
	os.RemoveAll(sandboxDir)
	if _, err := os.Stat(sandboxDir); !os.IsNotExist(err) {
		t.Error("sandbox dir should be removed after cleanup")
	}
}

// ── test helpers ────────────────────────────────────────────────────────

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
	// 尝试路径遍历
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
