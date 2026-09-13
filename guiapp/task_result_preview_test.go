package guiapp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPreviewTaskResultFilePPTX(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deck.pptx")
	if err := os.WriteFile(path, []byte("pk"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := (*App)(nil).PreviewTaskResultFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "pptx" || got.Language != "pptx" || got.FileName != "deck.pptx" {
		t.Fatalf("pptx preview = %+v", got)
	}
	if got.Path == "" || !strings.Contains(got.Path, "deck.pptx") {
		t.Fatalf("path = %q", got.Path)
	}
}

func TestPreviewTaskResultFileText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(path, []byte("# hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := (*App)(nil).PreviewTaskResultFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "text" || got.Language != "markdown" || got.Content != "# hello\n" {
		t.Fatalf("text preview = %+v", got)
	}
}

func TestPreviewTaskResultFilePDFToken(t *testing.T) {
	resetTaskResultPreviewLeasesForTest()
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.pdf")
	raw := []byte("%PDF-1.1\n1 0 obj<</Type/Catalog>>endobj\ntrailer<>\n%%EOF\n")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := (*App)(nil).PreviewTaskResultFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "pdf" {
		t.Fatalf("kind = %q", got.Kind)
	}
	if got.DataURL != "" {
		t.Fatalf("did not expect inline data url")
	}
	if !strings.HasPrefix(got.PreviewURL, taskResultPreviewHTTPPath+"?t=") {
		t.Fatalf("preview url = %q", got.PreviewURL)
	}

	req := httptest.NewRequest(http.MethodGet, got.PreviewURL, nil)
	rec := httptest.NewRecorder()
	handleTaskResultPreviewHTTP(nil, rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "pdf") {
		t.Fatalf("content-type = %q", rec.Header().Get("Content-Type"))
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != string(raw) {
		t.Fatalf("served pdf mismatch")
	}
}

func TestPreviewTaskResultFilePDFRejectsNonPDFHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fake.pdf")
	if err := os.WriteFile(path, []byte("not a pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (*App)(nil).PreviewTaskResultFile(path); err == nil || !strings.Contains(err.Error(), "PDF") {
		t.Fatalf("header error = %v", err)
	}
}

func TestTaskResultPreviewHTTPUnknownToken(t *testing.T) {
	resetTaskResultPreviewLeasesForTest()
	req := httptest.NewRequest(http.MethodGet, taskResultPreviewHTTPPath+"?t=deadbeef", nil)
	rec := httptest.NewRecorder()
	handleTaskResultPreviewHTTP(nil, rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestTaskResultPreviewHTTPExpiredToken(t *testing.T) {
	resetTaskResultPreviewLeasesForTest()
	token := issueTaskResultPreviewToken(filepath.Join(t.TempDir(), "gone.pdf"))
	taskResultPreviewMu.Lock()
	lease := taskResultPreviewLeases[token]
	lease.expires = time.Now().Add(-time.Second)
	taskResultPreviewLeases[token] = lease
	taskResultPreviewMu.Unlock()

	req := httptest.NewRequest(http.MethodGet, taskResultPreviewHTTPPath+"?t="+token, nil)
	rec := httptest.NewRecorder()
	handleTaskResultPreviewHTTP(nil, rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestPreviewTaskResultFileRejectsMissingAndDirectory(t *testing.T) {
	dir := t.TempDir()
	if _, err := (*App)(nil).PreviewTaskResultFile(filepath.Join(dir, "missing.txt")); err == nil || !strings.Contains(err.Error(), "文件不存在") {
		t.Fatalf("missing-file error = %v", err)
	}
	if _, err := (*App)(nil).PreviewTaskResultFile(dir); err == nil || !strings.Contains(err.Error(), "不是文件") {
		t.Fatalf("directory error = %v", err)
	}
}

func TestPreviewTaskResultFileRejectsBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob.bin")
	if err := os.WriteFile(path, []byte{0x00, 0x01, 0xff}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (*App)(nil).PreviewTaskResultFile(path); err == nil {
		t.Fatal("expected binary rejection")
	}
}

func TestIsTaskResultOfficeExt(t *testing.T) {
	if !isTaskResultOfficeExt(".docx") || !isTaskResultOfficeExt(".xlsx") || isTaskResultOfficeExt(".pptx") {
		t.Fatal("office ext classification")
	}
}
