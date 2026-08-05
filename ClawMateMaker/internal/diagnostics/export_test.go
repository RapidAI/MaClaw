package diagnostics

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportUsesAllowListAndRedactsAgain(t *testing.T) {
	root, out := t.TempDir(), t.TempDir()
	dir := filepath.Join(root, "job-1")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "serial.log"), []byte("token=secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "private.txt"), []byte("must not export"), 0600); err != nil {
		t.Fatal(err)
	}
	bundle, err := ExportJob(root, "job-1", out)
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(bundle.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	if len(z.File) != 1 || z.File[0].Name != "serial.log" {
		t.Fatalf("unexpected files: %#v", z.File)
	}
	r, err := z.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 64)
	n, _ := r.Read(data)
	_ = r.Close()
	if strings.Contains(string(data[:n]), "secret") {
		t.Fatal("secret was exported")
	}
}
