package pyenv

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractTarGzUsesBuiltinExtractor(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "fixture.tar.gz")
	if err := writeTarGzFixture(archivePath, "python/bin/python3", "ok"); err != nil {
		t.Fatal(err)
	}

	destDir := t.TempDir()
	t.Setenv("PATH", "")
	if err := extractTarGz(archivePath, destDir); err != nil {
		t.Fatalf("extractTarGz() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(destDir, "python", "bin", "python3"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "ok" {
		t.Fatalf("extracted content = %q, want ok", string(data))
	}
}

func TestExtractZipUsesBuiltinExtractor(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "fixture.zip")
	if err := writeZipFixture(archivePath, "uv/uv.exe", "ok"); err != nil {
		t.Fatal(err)
	}

	destDir := t.TempDir()
	t.Setenv("PATH", "")
	if err := extractZip(archivePath, destDir); err != nil {
		t.Fatalf("extractZip() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(destDir, "uv", "uv.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "ok" {
		t.Fatalf("extracted content = %q, want ok", string(data))
	}
}

func writeTarGzFixture(path, name, content string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(content))}); err != nil {
		return err
	}
	_, err = tw.Write([]byte(content))
	return err
}

func writeZipFixture(path, name, content string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write([]byte(content))
	return err
}
