package guiapp

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOCRTranscriptAlreadyCovers(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "scan.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "out.txt")
	var body strings.Builder
	for i := 1; i <= 8; i++ {
		if i > 1 {
			body.WriteString("\n\n")
		}
		fmt.Fprintf(&body, "## Page %d\n甲", i)
	}
	if err := os.WriteFile(outPath, []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	receipt, ok := ocrTranscriptAlreadyCovers(pdfPath, outPath, 1)
	if !ok || !strings.Contains(receipt, "start_page=9") || !strings.Contains(receipt, "不要重复识别") {
		t.Fatalf("start 1 must skip, ok=%v receipt=%q", ok, receipt)
	}
	if _, ok := ocrTranscriptAlreadyCovers(pdfPath, outPath, 9); ok {
		t.Fatal("start 9 is past the transcript and must be recognized")
	}
	gapPath := filepath.Join(dir, "gap.txt")
	if err := os.WriteFile(gapPath, []byte("## Page 1\n甲\n\n## Page 8\n乙\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gapReceipt, gapOK := ocrTranscriptAlreadyCovers(pdfPath, gapPath, 1)
	if !gapOK || !strings.Contains(gapReceipt, "start_page=2") || !strings.Contains(gapReceipt, "第 2 页") || strings.Contains(gapReceipt, "第 8 页") {
		t.Fatalf("a gap must name the missing page, ok=%v receipt=%q", gapOK, gapReceipt)
	}
	brokenPath := filepath.Join(dir, "broken.txt")
	if err := os.WriteFile(brokenPath, []byte("## Page 1\n[page extract error]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := ocrTranscriptAlreadyCovers(pdfPath, brokenPath, 1); ok {
		t.Fatal("an extract-error page must be readable again")
	}
	if _, ok := ocrTranscriptAlreadyCovers(pdfPath, filepath.Join(dir, "fresh.txt"), 1); ok {
		t.Fatal("a new output path must start over")
	}
	again, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != body.String() {
		t.Fatalf("skip must not rewrite the transcript: %q", again)
	}
}

func TestOCROutputPathRejected(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "scan.pdf")
	if got := ocrOutputPathRejected(pdfPath, filepath.Join(dir, "scan.txt")); got != "" {
		t.Fatalf("a txt beside the pdf must be accepted, got %q", got)
	}
	if got := ocrOutputPathRejected(pdfPath, pdfPath); got == "" {
		t.Fatal("output must not replace the source pdf")
	}
	if got := ocrOutputPathRejected(pdfPath, filepath.Join(dir, "文字版.pdf")); got == "" || !strings.Contains(got, ".txt") {
		t.Fatalf("pdf output = %q", got)
	}
}

func TestOCRRecognizePastEndKeepsGap(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(outPath, []byte("## Page 1\n甲\n\n## Page 8\n乙\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ocrRecognizePastEnd(outPath, 232)
	if !strings.Contains(got, "start_page=2") || !strings.Contains(got, "第 2 页") || strings.Contains(got, "第 8 页") || strings.Contains(got, "已到文件末尾") {
		t.Fatalf("a gap past the end must name the missing page: %q", got)
	}
	var body strings.Builder
	for i := 1; i <= 3; i++ {
		if i > 1 {
			body.WriteString("\n\n")
		}
		fmt.Fprintf(&body, "## Page %d\n甲", i)
	}
	if err := os.WriteFile(outPath, []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got = ocrRecognizePastEnd(outPath, 3)
	if !strings.Contains(got, "已到文件末尾") || strings.Contains(got, "start_page=") {
		t.Fatalf("a finished transcript is the end: %q", got)
	}
}

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
