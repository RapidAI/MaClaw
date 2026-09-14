package guiapp

import (
	"fmt"
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

func TestPreviewTaskResultFilePDFReusesLiveToken(t *testing.T) {
	resetTaskResultPreviewLeasesForTest()
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.1\n%%EOF\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := (*App)(nil).PreviewTaskResultFile(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := (*App)(nil).PreviewTaskResultFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if first.PreviewURL == "" || first.PreviewURL != second.PreviewURL {
		t.Fatalf("token reuse failed: %q vs %q", first.PreviewURL, second.PreviewURL)
	}
}

func TestPreviewTaskResultFilePDFAcceptsPrefixedHeader(t *testing.T) {
	resetTaskResultPreviewLeasesForTest()
	dir := t.TempDir()
	path := filepath.Join(dir, "prefixed.pdf")
	if err := os.WriteFile(path, []byte("junk\n%PDF-1.4\n%%EOF\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := (*App)(nil).PreviewTaskResultFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "pdf" {
		t.Fatalf("kind = %q", got.Kind)
	}
}

func TestTaskResultPreviewHTTPHead(t *testing.T) {
	resetTaskResultPreviewLeasesForTest()
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.1\n%%EOF\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := (*App)(nil).PreviewTaskResultFile(path)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodHead, got.PreviewURL, nil)
	rec := httptest.NewRecorder()
	handleTaskResultPreviewHTTP(nil, rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD should not write a body")
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

func TestRecordAudioAssetMiddlewareRoutesPreviewHEAD(t *testing.T) {
	resetTaskResultPreviewLeasesForTest()
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.1\n%%EOF\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := (*App)(nil).PreviewTaskResultFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fellThrough := false
	handler := recordAudioAssetMiddleware(nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		fellThrough = true
	}))
	req := httptest.NewRequest(http.MethodHead, got.PreviewURL, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if fellThrough {
		t.Fatal("HEAD preview request fell through to the asset server")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
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

func TestTaskResultPreviewHTTPRefreshTTL(t *testing.T) {
	resetTaskResultPreviewLeasesForTest()
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.1\n%%EOF\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := (*App)(nil).PreviewTaskResultFile(path)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimPrefix(got.PreviewURL, taskResultPreviewHTTPPath+"?t=")
	taskResultPreviewMu.Lock()
	lease := taskResultPreviewLeases[token]
	lease.expires = time.Now().Add(time.Second)
	taskResultPreviewLeases[token] = lease
	taskResultPreviewMu.Unlock()

	if _, ok := lookupTaskResultPreviewPath(token); !ok {
		t.Fatal("expected live token")
	}
	taskResultPreviewMu.Lock()
	refreshed := taskResultPreviewLeases[token]
	taskResultPreviewMu.Unlock()
	if time.Until(refreshed.expires) < 9*time.Minute {
		t.Fatalf("ttl was not refreshed: remaining %v", time.Until(refreshed.expires))
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

func TestExportTaskResultFileCopiesToChosenPath(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "report.docx")
	if err := os.WriteFile(src, []byte("hello-doc"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "copy.docx")
	orig := taskResultSaveDialog
	t.Cleanup(func() { taskResultSaveDialog = orig })
	taskResultSaveDialog = func(_ *App, _, _ string) (string, error) {
		return dest, nil
	}

	got, err := (*App)(nil).ExportTaskResultFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if got != dest && !strings.EqualFold(got, dest) {
		t.Fatalf("dest = %q, want %q", got, dest)
	}
	raw, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "hello-doc" {
		t.Fatalf("copied %q", raw)
	}
}

func TestExportTaskResultFileCancelReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(src, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := taskResultSaveDialog
	t.Cleanup(func() { taskResultSaveDialog = orig })
	taskResultSaveDialog = func(_ *App, _, _ string) (string, error) {
		return "", nil
	}
	got, err := (*App)(nil).ExportTaskResultFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("cancelled export returned %q", got)
	}
}

func TestExportTaskResultFileAppendsExtension(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "deck.pptx")
	if err := os.WriteFile(src, []byte("pk"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "exported")
	orig := taskResultSaveDialog
	t.Cleanup(func() { taskResultSaveDialog = orig })
	taskResultSaveDialog = func(_ *App, _, defaultFilename string) (string, error) {
		if defaultFilename != "deck.pptx" {
			t.Fatalf("default filename = %q", defaultFilename)
		}
		return dest, nil
	}
	got, err := (*App)(nil).ExportTaskResultFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.ToLower(got), ".pptx") {
		t.Fatalf("expected .pptx suffix, got %q", got)
	}
	raw, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "pk" {
		t.Fatalf("copied %q", raw)
	}
}

func TestExportTaskResultFileRejectsMissingAndDirectory(t *testing.T) {
	dir := t.TempDir()
	if _, err := (*App)(nil).ExportTaskResultFile(filepath.Join(dir, "missing.txt")); err == nil || !strings.Contains(err.Error(), "文件不存在") {
		t.Fatalf("missing-file error = %v", err)
	}
	if _, err := (*App)(nil).ExportTaskResultFile(dir); err == nil || !strings.Contains(err.Error(), "不是文件") {
		t.Fatalf("directory error = %v", err)
	}
}

func TestExportTaskResultFileOverwritesExistingDest(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "report.docx")
	dest := filepath.Join(dir, "copy.docx")
	if err := os.WriteFile(src, []byte("new-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := taskResultSaveDialog
	t.Cleanup(func() { taskResultSaveDialog = orig })
	taskResultSaveDialog = func(_ *App, _, _ string) (string, error) {
		return dest, nil
	}
	got, err := (*App)(nil).ExportTaskResultFile(src)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "new-bytes" {
		t.Fatalf("overwrite copied %q", raw)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".maclaw-export-") {
			t.Fatalf("leftover temp file %s", entry.Name())
		}
	}
}

func TestExportTaskResultFileCopiesWhenDialogReturnsPathAndError(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "notes.md")
	dest := filepath.Join(dir, "copy.md")
	if err := os.WriteFile(src, []byte("keep-me"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := taskResultSaveDialog
	t.Cleanup(func() { taskResultSaveDialog = orig })
	taskResultSaveDialog = func(_ *App, _, _ string) (string, error) {
		return dest, fmt.Errorf("dialog warning")
	}
	got, err := (*App)(nil).ExportTaskResultFile(src)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "keep-me" {
		t.Fatalf("copied %q", raw)
	}
}

func TestExportTaskResultFileDialogErrorWithoutPathIsCancel(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(src, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := taskResultSaveDialog
	t.Cleanup(func() { taskResultSaveDialog = orig })
	taskResultSaveDialog = func(_ *App, _, _ string) (string, error) {
		return "", fmt.Errorf("dialog failed")
	}
	got, err := (*App)(nil).ExportTaskResultFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("cancelled export returned %q", got)
	}
}

func TestExportTaskResultFileSamePathIsNoop(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "same.md")
	if err := os.WriteFile(src, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := taskResultSaveDialog
	t.Cleanup(func() { taskResultSaveDialog = orig })
	taskResultSaveDialog = func(_ *App, _, _ string) (string, error) {
		return src, nil
	}
	got, err := (*App)(nil).ExportTaskResultFile(src)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "keep" {
		t.Fatalf("same-path copy clobbered file: %q", raw)
	}
}
