package digitalasset

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func writeTestZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExtractZipSafely_OK(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "docs.zip")
	writeTestZip(t, zipPath, map[string]string{
		"a/readme.md": "# hello",
		"b/note.txt":  "world",
	})
	dest := filepath.Join(dir, "out")
	n, bytes, err := ExtractZipSafely(zipPath, dest, 100, 1<<20, 1<<20, []string{".exe"})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if n != 2 || bytes <= 0 {
		t.Fatalf("n=%d bytes=%d", n, bytes)
	}
	if _, err := os.Stat(filepath.Join(dest, "a", "readme.md")); err != nil {
		t.Fatalf("missing extracted file: %v", err)
	}
}

func TestExtractZipSafely_ZipSlip(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "evil.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("../escape.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("x"))
	_ = zw.Close()
	_ = f.Close()

	dest := filepath.Join(dir, "out")
	_, _, err = ExtractZipSafely(zipPath, dest, 100, 1<<20, 1<<20, nil)
	if err == nil {
		t.Fatal("expected zip slip error")
	}
}
