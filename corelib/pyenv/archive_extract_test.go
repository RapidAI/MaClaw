package pyenv

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestExtractTarGzKeepsPythonExecutable(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "python.tar.gz")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	body := []byte("#!/bin/sh\n")
	if err := tw.WriteHeader(&tar.Header{Name: "python/bin/python3.12", Mode: 0o775, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	destDir := t.TempDir()
	if err := extractTarGz(archivePath, destDir); err != nil {
		t.Fatalf("extractTarGz() error = %v", err)
	}
	binPath := filepath.Join(destDir, "python", "bin", "python3.12")
	data, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(body) {
		t.Fatalf("extracted content = %q", data)
	}
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("python binary mode = %o, want 0755", info.Mode().Perm())
	}
}

func TestCompactProbeErrorCollapsesAndTruncates(t *testing.T) {
	if got := compactProbeError("  permission \n denied  "); got != "permission denied" {
		t.Fatalf("compact = %q", got)
	}
	got := compactProbeError(strings.Repeat("x", 200))
	if want := 183; len([]rune(got)) != want || !strings.HasSuffix(got, "...") {
		t.Fatalf("truncated = %q (len %d), want %d runes", got, len([]rune(got)), want)
	}
}

func TestInspectPythonReportsMissingBinary(t *testing.T) {
	_, err := inspectPython(filepath.Join(t.TempDir(), "missing-python"))
	if err == nil || err.Error() == "" {
		t.Fatalf("inspectPython() error = %v", err)
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
