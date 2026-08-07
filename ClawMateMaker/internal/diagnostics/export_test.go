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
	const jobID = "job-0123456789abcdef"
	dir := filepath.Join(root, jobID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "serial.log"), []byte("token=secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "private.txt"), []byte("must not export"), 0600); err != nil {
		t.Fatal(err)
	}
	bundle, err := ExportJob(root, jobID, out)
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

func TestExportRejectsNonRegularAllowListedFile(t *testing.T) {
	root, out := t.TempDir(), t.TempDir()
	const jobID = "job-0123456789abcdef"
	dir := filepath.Join(root, jobID)
	if err := os.MkdirAll(filepath.Join(dir, "serial.log"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportJob(root, jobID, out); err == nil {
		t.Fatal("directory named as an allow-listed log was exported")
	}
}

func TestExportRejectsUnsafeJobID(t *testing.T) {
	if _, err := ExportJob(t.TempDir(), "../job-0123456789abcdef", t.TempDir()); err == nil {
		t.Fatal("unsafe job ID was accepted")
	}
}
