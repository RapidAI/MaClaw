package ve

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileRelay_Upload_Success(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	content := []byte("hello world")
	meta, err := fr.Upload("test.txt", "text/plain", "session-1", "user-1", int64(len(content)), bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}
	if meta.ID == "" {
		t.Fatal("expected non-empty file ID")
	}
	if meta.Filename != "test.txt" {
		t.Errorf("expected filename test.txt, got %s", meta.Filename)
	}
	if meta.MimeType != "text/plain" {
		t.Errorf("expected mime text/plain, got %s", meta.MimeType)
	}
	if meta.Size != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), meta.Size)
	}
	if meta.SessionID != "session-1" {
		t.Errorf("expected session-1, got %s", meta.SessionID)
	}

	// Verify file on disk.
	diskPath := fr.FilePath(meta)
	data, err := os.ReadFile(diskPath)
	if err != nil {
		t.Fatalf("failed to read file from disk: %v", err)
	}
	if !bytes.Equal(data, content) {
		t.Error("file content mismatch")
	}
}

func TestFileRelay_Upload_UnsupportedMIME(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	_, err := fr.Upload("test.exe", "application/x-executable", "s1", "u1", 100, strings.NewReader("data"))
	if err == nil {
		t.Fatal("expected error for unsupported MIME type")
	}
	if !strings.Contains(err.Error(), "unsupported file type") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFileRelay_Upload_TextSizeExceeded(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	// Create content larger than 500KB.
	bigContent := make([]byte, MaxTextFileSize+1)
	_, err := fr.Upload("big.txt", "text/plain", "s1", "u1", int64(len(bigContent)), bytes.NewReader(bigContent))
	if err == nil {
		t.Fatal("expected error for oversized text file")
	}
	if !strings.Contains(err.Error(), "exceeds limit") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFileRelay_Upload_ImageSizeExceeded(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	// Declared size exceeds 10MB.
	_, err := fr.Upload("big.png", "image/png", "s1", "u1", MaxImageFileSize+1, strings.NewReader("x"))
	if err == nil {
		t.Fatal("expected error for oversized image file")
	}
	if !strings.Contains(err.Error(), "exceeds limit") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFileRelay_Upload_DocumentSizeExceeded(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	_, err := fr.Upload("big.pdf", "application/pdf", "s1", "u1", MaxDocumentFileSize+1, strings.NewReader("x"))
	if err == nil {
		t.Fatal("expected error for oversized document file")
	}
	if !strings.Contains(err.Error(), "exceeds limit") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFileRelay_MetadataPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)
	content := []byte("persisted file")
	meta, err := fr.Upload("persisted.txt", "text/plain", "sess-1", "user-1", int64(len(content)), bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	restarted := NewFileRelay(dir)
	loaded := restarted.GetFile(meta.ID)
	if loaded == nil {
		t.Fatal("expected uploaded metadata to load after restart")
	}
	if loaded.Filename != "persisted.txt" || loaded.SessionID != "sess-1" || loaded.UploaderID != "user-1" {
		t.Fatalf("loaded metadata = %+v", loaded)
	}
	data, err := os.ReadFile(restarted.FilePath(loaded))
	if err != nil {
		t.Fatalf("read persisted file: %v", err)
	}
	if !bytes.Equal(data, content) {
		t.Fatalf("persisted content = %q", string(data))
	}
}

func TestFileRelay_LoadMetadataSkipsMissingDiskFile(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)
	meta, err := fr.Upload("missing.txt", "text/plain", "sess-1", "user-1", 4, strings.NewReader("data"))
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}
	if err := os.Remove(fr.FilePath(meta)); err != nil {
		t.Fatalf("remove uploaded file: %v", err)
	}

	restarted := NewFileRelay(dir)
	if got := restarted.GetFile(meta.ID); got != nil {
		t.Fatalf("expected missing disk file metadata to be ignored, got %+v", got)
	}
}
func TestFileRelay_GetFile_NotFound(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	meta := fr.GetFile("nonexistent-id")
	if meta != nil {
		t.Fatal("expected nil for nonexistent file")
	}
}

func TestFileRelay_GetFile_Expired(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	content := []byte("test")
	meta, _ := fr.Upload("test.txt", "text/plain", "s1", "u1", int64(len(content)), bytes.NewReader(content))

	// Manually expire the file.
	fr.mu.Lock()
	fr.files[meta.ID].ExpiresAt = time.Now().Add(-1 * time.Hour)
	fr.mu.Unlock()

	result := fr.GetFile(meta.ID)
	if result != nil {
		t.Fatal("expected nil for expired file")
	}
}

func TestFileRelay_CleanExpired(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	content := []byte("test data")
	meta, _ := fr.Upload("test.txt", "text/plain", "s1", "u1", int64(len(content)), bytes.NewReader(content))

	// Verify file exists on disk.
	diskPath := fr.FilePath(meta)
	if _, err := os.Stat(diskPath); err != nil {
		t.Fatalf("file should exist on disk: %v", err)
	}

	// Expire the file.
	fr.mu.Lock()
	fr.files[meta.ID].ExpiresAt = time.Now().Add(-1 * time.Hour)
	fr.mu.Unlock()

	// Run cleanup.
	fr.cleanExpired()

	// Verify file is removed from memory and disk.
	if fr.GetFile(meta.ID) != nil {
		t.Error("file should be removed from memory after cleanup")
	}
	if _, err := os.Stat(diskPath); !os.IsNotExist(err) {
		t.Error("file should be removed from disk after cleanup")
	}
}

func TestFileRelay_StartStop(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	ctx := context.Background()
	fr.Start(ctx)

	// Give it a moment to start.
	time.Sleep(10 * time.Millisecond)

	// Stop should not hang.
	fr.Stop()
}

func TestFileRelay_StopBeforeStartDoesNotHang(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	done := make(chan struct{})
	go func() {
		fr.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Stop before Start should not hang")
	}
}

func TestFileRelay_StartIsIdempotentAndRestartable(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	ctx, cancel := context.WithCancel(context.Background())
	fr.Start(ctx)
	fr.Start(ctx)
	cancel()
	time.Sleep(20 * time.Millisecond)

	fr.Start(context.Background())
	fr.Stop()
}
func TestFileRelay_HandleUpload_HTTP(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	// Build multipart request.
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("session_id", "sess-123")
	_ = writer.WriteField("participant_id", "user-456")
	part, _ := writer.CreateFormFile("file", "hello.txt")
	_, _ = part.Write([]byte("hello world"))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/ve/files/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	fr.HandleUpload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["ok"] != true {
		t.Errorf("expected ok=true, got %v", resp["ok"])
	}
	if resp["file_id"] == nil || resp["file_id"] == "" {
		t.Error("expected non-empty file_id")
	}
	fileURL, _ := resp["file_url"].(string)
	if !strings.HasPrefix(fileURL, "/api/ve/files/") {
		t.Errorf("unexpected file_url: %s", fileURL)
	}
}

func TestFileRelay_HandleDownload_HTTP(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	// Upload a file first.
	content := []byte("download me")
	meta, _ := fr.Upload("dl.txt", "text/plain", "sess-1", "user-1", int64(len(content)), bytes.NewReader(content))

	// Download it.
	req := httptest.NewRequest(http.MethodGet, "/api/ve/files/"+meta.ID+"?session_id=sess-1&participant_id=user-1", nil)
	rec := httptest.NewRecorder()

	fr.HandleDownload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "text/plain" {
		t.Errorf("unexpected content-type: %s", rec.Header().Get("Content-Type"))
	}
	body, _ := io.ReadAll(rec.Body)
	if !bytes.Equal(body, content) {
		t.Error("downloaded content mismatch")
	}
}

func TestFileRelay_HandleDownload_NotFound(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	req := httptest.NewRequest(http.MethodGet, "/api/ve/files/nonexistent", nil)
	rec := httptest.NewRecorder()

	fr.HandleDownload(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestFileRelay_HandleDownload_SessionMismatch(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	content := []byte("secret")
	meta, _ := fr.Upload("secret.txt", "text/plain", "sess-owner", "user-1", int64(len(content)), bytes.NewReader(content))

	// Try to download with wrong session.
	req := httptest.NewRequest(http.MethodGet, "/api/ve/files/"+meta.ID+"?session_id=sess-attacker&participant_id=user-2", nil)
	rec := httptest.NewRecorder()

	fr.HandleDownload(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

type staticParticipantValidator map[string]map[string]bool

func (v staticParticipantValidator) IsSessionParticipant(sessionID, participantID string) bool {
	participants := v[sessionID]
	return participants != nil && participants[participantID]
}

func TestFileRelay_HandleUpload_RequiresValidatedParticipantWhenValidatorIsSet(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)
	fr.SetParticipantValidator(staticParticipantValidator{
		"sess-1": {"user-1": true},
	})

	buildRequest := func(participantID string) *http.Request {
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		_ = writer.WriteField("session_id", "sess-1")
		_ = writer.WriteField("participant_id", participantID)
		part, _ := writer.CreateFormFile("file", "hello.txt")
		_, _ = part.Write([]byte("hello world"))
		writer.Close()
		req := httptest.NewRequest(http.MethodPost, "/api/ve/files/upload", &buf)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		return req
	}

	rec := httptest.NewRecorder()
	fr.HandleUpload(rec, buildRequest("user-attacker"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-participant upload, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	fr.HandleUpload(rec, buildRequest("user-1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for validated participant upload, got %d: %s", rec.Code, rec.Body.String())
	}
}
func TestFileRelay_HandleDownload_ValidatorOverridesSessionMatch(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)
	fr.SetParticipantValidator(staticParticipantValidator{
		"sess-owner": {"user-1": true, "user-2": true},
	})

	content := []byte("shared secret")
	meta, _ := fr.Upload("secret.txt", "text/plain", "sess-owner", "user-1", int64(len(content)), bytes.NewReader(content))

	// A caller with the right session_id but not in the participant set must not be
	// able to download when the validator is available.
	req := httptest.NewRequest(http.MethodGet, "/api/ve/files/"+meta.ID+"?session_id=sess-owner&participant_id=user-attacker", nil)
	rec := httptest.NewRecorder()
	fr.HandleDownload(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-participant, got %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/ve/files/"+meta.ID+"?session_id=sess-owner&participant_id=user-2", nil)
	rec = httptest.NewRecorder()
	fr.HandleDownload(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for validated participant, got %d: %s", rec.Code, rec.Body.String())
	}
}
func TestFileRelay_HandleUpload_UnsupportedType(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, _ := writer.CreateFormFile("file", "malware.exe")
	_, _ = part.Write([]byte("bad stuff"))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/ve/files/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	fr.HandleUpload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["code"] != "unsupported_type" {
		t.Errorf("expected code unsupported_type, got %v", resp["code"])
	}
}

func TestIsAllowedMIME(t *testing.T) {
	tests := []struct {
		mime   string
		expect bool
	}{
		{"text/plain", true},
		{"text/markdown", true},
		{"application/json", true},
		{"text/html", true},
		{"image/png", true},
		{"image/jpeg", true},
		{"image/gif", true},
		{"image/webp", true},
		{"image/bmp", true},
		{"application/pdf", true},
		{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", true},
		{"application/x-executable", false},
		{"application/octet-stream", false},
		{"video/mp4", false},
	}
	for _, tt := range tests {
		got := isAllowedMIME(tt.mime)
		if got != tt.expect {
			t.Errorf("isAllowedMIME(%q) = %v, want %v", tt.mime, got, tt.expect)
		}
	}
}

func TestDetectMIMEType(t *testing.T) {
	tests := []struct {
		filename string
		expect   string
	}{
		{"test.txt", "text/plain"},
		{"test.md", "text/markdown"},
		{"test.json", "application/json"},
		{"test.png", "image/png"},
		{"test.jpg", "image/jpeg"},
		{"test.gif", "image/gif"},
		{"test.webp", "image/webp"},
		{"test.pdf", "application/pdf"},
		{"test.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{"test.go", "text/plain"},
		{"test.py", "text/plain"},
		{"test.unknown", "application/octet-stream"},
	}
	for _, tt := range tests {
		header := &multipart.FileHeader{
			Filename: tt.filename,
			Header:   make(map[string][]string),
		}
		got := detectMIMEType(header)
		if got != tt.expect {
			t.Errorf("detectMIMEType(%q) = %q, want %q", tt.filename, got, tt.expect)
		}
	}
}

func TestExtractFileID(t *testing.T) {
	tests := []struct {
		path   string
		expect string
	}{
		{"/api/ve/files/abc-123", "abc-123"},
		{"/api/ve/files/abc-123/extra", "abc-123"},
		{"/api/ve/files/", ""},
		{"/other/path", ""},
	}
	for _, tt := range tests {
		got := extractFileID(tt.path)
		if got != tt.expect {
			t.Errorf("extractFileID(%q) = %q, want %q", tt.path, got, tt.expect)
		}
	}
}

func TestFileCategory(t *testing.T) {
	tests := []struct {
		mime   string
		expect string
	}{
		{"text/plain", "text"},
		{"text/markdown", "text"},
		{"image/png", "image"},
		{"image/jpeg", "image"},
		{"application/pdf", "document"},
		{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "document"},
	}
	for _, tt := range tests {
		got := fileCategory(tt.mime)
		if got != tt.expect {
			t.Errorf("fileCategory(%q) = %q, want %q", tt.mime, got, tt.expect)
		}
	}
}

func TestFileRelay_AllowedImageTypes(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	// Small PNG content (1x1 pixel).
	pngData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

	for _, mimeType := range []string{"image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp"} {
		meta, err := fr.Upload("test"+filepath.Ext(".png"), mimeType, "s1", "u1", int64(len(pngData)), bytes.NewReader(pngData))
		if err != nil {
			t.Errorf("Upload with mime %s failed: %v", mimeType, err)
			continue
		}
		if meta.MimeType != mimeType {
			t.Errorf("expected mime %s, got %s", mimeType, meta.MimeType)
		}
	}
}
