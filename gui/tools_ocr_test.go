package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOCRToolImageBase64RequiresInput(t *testing.T) {
	if _, err := ocrToolImageBase64(map[string]interface{}{}); err == nil {
		t.Fatal("expected error when neither image_path nor image_base64 is given")
	}
	if _, err := ocrToolImageBase64(map[string]interface{}{"image_path": "  ", "image_base64": ""}); err == nil {
		t.Fatal("expected error for blank inputs")
	}
}

func TestOCRToolImageBase64Passthrough(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte("fake-png"))
	got, err := ocrToolImageBase64(map[string]interface{}{"image_base64": b64})
	if err != nil {
		t.Fatal(err)
	}
	if got != b64 {
		t.Fatalf("expected passthrough, got %q", got)
	}
}

func TestOCRToolImageBase64OversizedBase64(t *testing.T) {
	huge := strings.Repeat("A", ocrToolMaxImageBytes*2+1)
	if _, err := ocrToolImageBase64(map[string]interface{}{"image_base64": huge}); err == nil {
		t.Fatal("expected error for oversized image_base64")
	} else if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOCRToolImageBase64FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "img.png")
	content := []byte("fake-png-bytes")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ocrToolImageBase64(map[string]interface{}{"image_path": path})
	if err != nil {
		t.Fatal(err)
	}
	if got != base64.StdEncoding.EncodeToString(content) {
		t.Fatal("file content not base64-encoded correctly")
	}
}

func TestOCRToolImageBase64MissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.png")
	if _, err := ocrToolImageBase64(map[string]interface{}{"image_path": path}); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestOCRToolImageBase64OversizedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	// Sparse file just over the cap — no real 25 MiB write needed.
	if err := f.Truncate(ocrToolMaxImageBytes + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	if _, err := ocrToolImageBase64(map[string]interface{}{"image_path": path}); err == nil {
		t.Fatal("expected error for oversized image file")
	} else if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteOCRResultFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "result.md")
	written, err := writeOCRResultFile(path, "OCR 全文")
	if err != nil {
		t.Fatalf("writeOCRResultFile: %v", err)
	}
	data, err := os.ReadFile(written)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(data) != "OCR 全文" {
		t.Fatalf("content = %q", data)
	}
	if _, err := os.Stat(written + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file left behind")
	}
}

func TestWriteOCRResultFileOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.txt")
	if _, err := writeOCRResultFile(path, "v1"); err != nil {
		t.Fatal(err)
	}
	if _, err := writeOCRResultFile(path, "v2"); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "v2" {
		t.Fatalf("content = %q, want v2", data)
	}
}
