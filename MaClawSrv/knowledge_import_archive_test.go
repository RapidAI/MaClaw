package main

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractKnowledgeZipArchive(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "docs.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(file)
	w, err := zw.Create("nested/readme.md")
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if _, err := w.Write([]byte("hello knowledge")); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("file close: %v", err)
	}

	extractDir, paths, err := extractKnowledgeArchive(context.Background(), archivePath, "docs.zip", dir, 1024*1024)
	if err != nil {
		t.Fatalf("extractKnowledgeArchive: %v", err)
	}
	defer os.RemoveAll(extractDir)
	if len(paths) != 1 || filepath.Base(paths[0]) != "readme.md" {
		t.Fatalf("paths = %#v", paths)
	}
	if _, err := os.Stat(paths[0]); err != nil {
		t.Fatalf("extracted file missing: %v", err)
	}
}

func TestExtractKnowledgeZipRejectsUnsafePath(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "unsafe.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(file)
	if _, err := zw.Create("../escape.md"); err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("file close: %v", err)
	}

	if _, _, err := extractKnowledgeArchive(context.Background(), archivePath, "unsafe.zip", dir, 1024*1024); err == nil {
		t.Fatalf("expected unsafe archive path error")
	}
}
