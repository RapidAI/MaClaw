package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSavePastedFileSanitizesNameAndWritesData(t *testing.T) {
	app := &App{}
	encoded := base64.StdEncoding.EncodeToString([]byte("hello pasted file"))
	path, err := app.SavePastedFile(encoded, `..\bad:name?.txt`, "text/plain")
	if err != nil {
		t.Fatalf("SavePastedFile returned error: %v", err)
	}
	defer os.Remove(path)

	if !filepath.IsAbs(path) {
		t.Fatalf("path is not absolute: %q", path)
	}
	base := filepath.Base(path)
	if strings.ContainsAny(base, `<>:"/\|?*`) {
		t.Fatalf("filename was not sanitized: %q", base)
	}
	if !strings.HasSuffix(base, ".txt") {
		t.Fatalf("filename should retain extension, got %q", base)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}
	if string(data) != "hello pasted file" {
		t.Fatalf("saved data = %q", string(data))
	}
}

func TestSavePastedFileUsesMimeFallbackExtension(t *testing.T) {
	app := &App{}
	encoded := base64.StdEncoding.EncodeToString([]byte("pdf-ish"))
	path, err := app.SavePastedFile(encoded, "clipboard", "application/pdf")
	if err != nil {
		t.Fatalf("SavePastedFile returned error: %v", err)
	}
	defer os.Remove(path)

	if !strings.HasSuffix(filepath.Base(path), ".pdf") {
		t.Fatalf("expected .pdf fallback extension, got %q", filepath.Base(path))
	}
}

func TestSanitizePastedFileNameHandlesWindowsPathsOnEveryPlatform(t *testing.T) {
	got := sanitizePastedFileName(`C:\Users\me\Desktop\report?.pdf`)
	if got != "report_.pdf" {
		t.Fatalf("sanitizePastedFileName() = %q, want report_.pdf", got)
	}
}
