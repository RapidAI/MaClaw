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
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/a2a"
)

// --- File Relay Integration Tests (Task 17.8) ---

// TestIntegration_FileRelay_UploadDownloadRoundTrip tests uploading text/image/document
// files and downloading them back, verifying content matches.
func TestIntegration_FileRelay_UploadDownloadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	testCases := []struct {
		name     string
		filename string
		mimeType string
		content  []byte
	}{
		{"text file", "hello.txt", "text/plain", []byte("Hello, World! This is a test text file.")},
		{"image file", "test.png", "image/png", bytes.Repeat([]byte{0x89, 0x50, 0x4E, 0x47}, 100)},
		{"document file", "report.pdf", "application/pdf", bytes.Repeat([]byte{0x25, 0x50, 0x44, 0x46}, 200)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Upload
			meta, err := fr.Upload(tc.filename, tc.mimeType, "session-1", "user-1", int64(len(tc.content)), bytes.NewReader(tc.content))
			if err != nil {
				t.Fatalf("Upload failed: %v", err)
			}
			if meta.Filename != tc.filename {
				t.Errorf("filename mismatch: got %s", meta.Filename)
			}
			if meta.MimeType != tc.mimeType {
				t.Errorf("mime type mismatch: got %s", meta.MimeType)
			}
			if meta.Size != int64(len(tc.content)) {
				t.Errorf("size mismatch: got %d, want %d", meta.Size, len(tc.content))
			}

			// Download via HTTP handler
			req := httptest.NewRequest(http.MethodGet, "/api/ve/files/"+meta.ID+"?session_id=session-1&participant_id=user-1", nil)
			rec := httptest.NewRecorder()
			fr.HandleDownload(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("download returned %d: %s", rec.Code, rec.Body.String())
			}

			body, _ := io.ReadAll(rec.Body)
			if !bytes.Equal(body, tc.content) {
				t.Error("downloaded content does not match uploaded content")
			}
			if rec.Header().Get("Content-Type") != tc.mimeType {
				t.Errorf("content-type mismatch: got %s", rec.Header().Get("Content-Type"))
			}
		})
	}
}

// TestIntegration_FileRelay_UnsupportedTypeRejection tests that unsupported file types
// are rejected at upload time.
func TestIntegration_FileRelay_UnsupportedTypeRejection(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	unsupportedTypes := []struct {
		filename string
		mimeType string
	}{
		{"malware.exe", "application/x-executable"},
		{"video.mp4", "video/mp4"},
		{"archive.zip", "application/zip"},
		{"binary.bin", "application/octet-stream"},
	}

	for _, tc := range unsupportedTypes {
		t.Run(tc.filename, func(t *testing.T) {
			_, err := fr.Upload(tc.filename, tc.mimeType, "s1", "u1", 100, strings.NewReader("data"))
			if err == nil {
				t.Fatalf("expected rejection for %s (%s)", tc.filename, tc.mimeType)
			}
			if !strings.Contains(err.Error(), "unsupported file type") {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}

	// Verify via HTTP handler
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, _ := writer.CreateFormFile("file", "hack.exe")
	_, _ = part.Write([]byte("malicious content"))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/ve/files/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	fr.HandleUpload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unsupported type, got %d", rec.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["code"] != "unsupported_type" {
		t.Errorf("expected code=unsupported_type, got %v", resp["code"])
	}
}

// TestIntegration_FileRelay_OversizedFileRejection tests that files exceeding size
// limits are rejected.
func TestIntegration_FileRelay_OversizedFileRejection(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	testCases := []struct {
		name     string
		filename string
		mimeType string
		size     int64
	}{
		{"text over 500KB", "big.txt", "text/plain", MaxTextFileSize + 1},
		{"image over 10MB", "big.png", "image/png", MaxImageFileSize + 1},
		{"document over 20MB", "big.pdf", "application/pdf", MaxDocumentFileSize + 1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := fr.Upload(tc.filename, tc.mimeType, "s1", "u1", tc.size, strings.NewReader("x"))
			if err == nil {
				t.Fatal("expected size rejection")
			}
			if !strings.Contains(err.Error(), "exceeds limit") {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestIntegration_FileRelay_WrongSessionForbidden tests that downloading with a
// wrong session_id returns 403.
func TestIntegration_FileRelay_WrongSessionForbidden(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	content := []byte("secret data")
	meta, _ := fr.Upload("secret.txt", "text/plain", "session-owner", "user-owner", int64(len(content)), bytes.NewReader(content))

	// Try to download with wrong session_id and wrong participant_id
	req := httptest.NewRequest(http.MethodGet, "/api/ve/files/"+meta.ID+"?session_id=session-attacker&participant_id=user-attacker", nil)
	rec := httptest.NewRecorder()
	fr.HandleDownload(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for wrong session, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestIntegration_FileRelay_GroupSessionAccess tests that all participants in the same
// group session can access files uploaded by any participant (Requirement 11.11).
func TestIntegration_FileRelay_GroupSessionAccess(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	// Participant A uploads a file in group session
	content := []byte("shared group file content")
	meta, _ := fr.Upload("shared.txt", "text/plain", "group-session-1", "participant-A", int64(len(content)), bytes.NewReader(content))

	// Participant B (same session) should be able to download
	req := httptest.NewRequest(http.MethodGet, "/api/ve/files/"+meta.ID+"?session_id=group-session-1&participant_id=participant-B", nil)
	rec := httptest.NewRecorder()
	fr.HandleDownload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("participant B should access group file, got %d: %s", rec.Code, rec.Body.String())
	}

	body, _ := io.ReadAll(rec.Body)
	if !bytes.Equal(body, content) {
		t.Error("content mismatch for group participant download")
	}
}

// TestIntegration_FileRelay_TTLExpiry tests that files are removed after TTL expires.
func TestIntegration_FileRelay_TTLExpiry(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	content := []byte("temporary file")
	meta, _ := fr.Upload("temp.txt", "text/plain", "s1", "u1", int64(len(content)), bytes.NewReader(content))

	// Verify file exists
	if fr.GetFile(meta.ID) == nil {
		t.Fatal("file should exist before expiry")
	}

	// Manually expire the file
	fr.mu.Lock()
	fr.files[meta.ID].ExpiresAt = time.Now().Add(-1 * time.Hour)
	fr.mu.Unlock()

	// GetFile should return nil for expired file
	if fr.GetFile(meta.ID) != nil {
		t.Fatal("expired file should not be returned by GetFile")
	}

	// Run cleanup
	fr.cleanExpired()

	// File should be removed from memory
	fr.mu.RLock()
	_, exists := fr.files[meta.ID]
	fr.mu.RUnlock()
	if exists {
		t.Fatal("expired file should be removed from memory after cleanup")
	}

	// File should be removed from disk
	diskPath := fr.FilePath(meta)
	if _, err := os.Stat(diskPath); !os.IsNotExist(err) {
		t.Fatal("expired file should be removed from disk after cleanup")
	}
}

// TestIntegration_FileRelay_CleanupGoroutine tests that the cleanup goroutine
// starts and stops correctly.
func TestIntegration_FileRelay_CleanupGoroutine(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	ctx := context.Background()
	fr.Start(ctx)

	// Give it a moment to start
	time.Sleep(20 * time.Millisecond)

	// Stop should not hang
	fr.Stop()
}

// --- A2A Attachment Serialization Tests (Task 17.8) ---

// TestIntegration_A2A_AllAttachmentTypes_JSONRoundTrip tests that a GroupDiscussionMessage
// with all three attachment types survives JSON serialization/deserialization.
func TestIntegration_A2A_AllAttachmentTypes_JSONRoundTrip(t *testing.T) {
	msg := a2a.GroupDiscussionMessage{
		ID:        "msg-full-attach",
		SessionID: "session-full",
		FromID:    "user-sender",
		Kind:      a2a.MessageStatement,
		Content:   "Here are all types of attachments",
		TextAttachments: []a2a.TextAttachment{
			{Content: "SGVsbG8gV29ybGQ=", Filename: "hello.txt", MimeType: "text/plain"},
			{Content: "cHJpbnQoImhpIik=", Filename: "script.py", MimeType: "text/x-python"},
		},
		ImageAttachments: []a2a.ImageAttachment{
			{FileURL: "http://hub/files/img-001", Filename: "photo.png", MimeType: "image/png", Width: 1920, Height: 1080},
			{FileURL: "http://hub/files/img-002", Filename: "thumb.jpg", MimeType: "image/jpeg", Width: 200, Height: 200},
		},
		FileAttachments: []a2a.FileAttachment{
			{FileURL: "http://hub/files/doc-001", Filename: "report.pdf", MimeType: "application/pdf", SizeBytes: 5242880},
			{FileURL: "http://hub/files/doc-002", Filename: "data.docx", MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", SizeBytes: 1048576},
		},
		CreatedAt: time.Date(2026, 5, 15, 10, 30, 0, 0, time.UTC),
	}

	// Serialize
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Deserialize
	var decoded a2a.GroupDiscussionMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Verify all fields
	if decoded.ID != msg.ID || decoded.SessionID != msg.SessionID || decoded.FromID != msg.FromID {
		t.Fatal("basic fields mismatch")
	}
	if decoded.Kind != msg.Kind || decoded.Content != msg.Content {
		t.Fatal("kind/content mismatch")
	}

	// Text attachments
	if len(decoded.TextAttachments) != 2 {
		t.Fatalf("expected 2 text attachments, got %d", len(decoded.TextAttachments))
	}
	if decoded.TextAttachments[0].Filename != "hello.txt" || decoded.TextAttachments[1].Filename != "script.py" {
		t.Fatal("text attachment filenames mismatch")
	}

	// Image attachments
	if len(decoded.ImageAttachments) != 2 {
		t.Fatalf("expected 2 image attachments, got %d", len(decoded.ImageAttachments))
	}
	if decoded.ImageAttachments[0].Width != 1920 || decoded.ImageAttachments[1].Height != 200 {
		t.Fatal("image attachment dimensions mismatch")
	}

	// File attachments
	if len(decoded.FileAttachments) != 2 {
		t.Fatalf("expected 2 file attachments, got %d", len(decoded.FileAttachments))
	}
	if decoded.FileAttachments[0].SizeBytes != 5242880 || decoded.FileAttachments[1].SizeBytes != 1048576 {
		t.Fatal("file attachment sizes mismatch")
	}
}

// TestIntegration_A2A_EmptyAttachments_Omitted tests that empty attachment arrays
// are omitted from JSON output (omitempty behavior).
func TestIntegration_A2A_EmptyAttachments_Omitted(t *testing.T) {
	msg := a2a.GroupDiscussionMessage{
		ID:        "msg-no-attach",
		SessionID: "session-1",
		FromID:    "user-1",
		Kind:      a2a.MessageStatement,
		Content:   "No attachments here",
		CreatedAt: time.Now(),
	}

	data, _ := json.Marshal(msg)
	s := string(data)

	if strings.Contains(s, "text_attachments") {
		t.Fatal("empty text_attachments should be omitted")
	}
	if strings.Contains(s, "image_attachments") {
		t.Fatal("empty image_attachments should be omitted")
	}
	if strings.Contains(s, "file_attachments") {
		t.Fatal("empty file_attachments should be omitted")
	}
}

// TestIntegration_A2A_StreamChunkWithAttachments tests that stream_chunk messages
// can carry attachment fields (for VE responses that include generated files).
func TestIntegration_A2A_StreamChunkWithAttachments(t *testing.T) {
	msg := a2a.GroupDiscussionMessage{
		ID:        "msg-stream-attach",
		SessionID: "session-stream",
		FromID:    "ve-1",
		Kind:      a2a.MessageStreamEnd,
		Content:   "Here's the generated diagram",
		ImageAttachments: []a2a.ImageAttachment{
			{FileURL: "http://hub/files/gen-img-1", Filename: "diagram.png", MimeType: "image/png", Width: 800, Height: 600},
		},
		CreatedAt: time.Now(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal stream_end with attachment failed: %v", err)
	}

	var decoded a2a.GroupDiscussionMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Kind != a2a.MessageStreamEnd {
		t.Fatalf("expected kind=stream_end, got %s", decoded.Kind)
	}
	if len(decoded.ImageAttachments) != 1 {
		t.Fatalf("expected 1 image attachment, got %d", len(decoded.ImageAttachments))
	}
	if decoded.ImageAttachments[0].Filename != "diagram.png" {
		t.Fatal("image attachment filename mismatch")
	}
}

// TestIntegration_FileRelay_HTTPUploadDownload_FullCycle tests the complete HTTP
// upload → download cycle through the handler layer.
func TestIntegration_FileRelay_HTTPUploadDownload_FullCycle(t *testing.T) {
	dir := t.TempDir()
	fr := NewFileRelay(dir)

	// Upload via HTTP
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("session_id", "http-session-1")
	_ = writer.WriteField("participant_id", "http-user-1")
	part, _ := writer.CreateFormFile("file", "document.md")
	content := []byte("# Integration Test\n\nThis is a test document for the file relay.")
	_, _ = part.Write(content)
	writer.Close()

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/ve/files/upload", &buf)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRec := httptest.NewRecorder()
	fr.HandleUpload(uploadRec, uploadReq)

	if uploadRec.Code != http.StatusOK {
		t.Fatalf("upload returned %d: %s", uploadRec.Code, uploadRec.Body.String())
	}

	var uploadResp map[string]any
	_ = json.Unmarshal(uploadRec.Body.Bytes(), &uploadResp)
	if uploadResp["ok"] != true {
		t.Fatal("upload response ok should be true")
	}
	fileURL, _ := uploadResp["file_url"].(string)
	if !strings.HasPrefix(fileURL, "/api/ve/files/") {
		t.Fatalf("unexpected file_url: %s", fileURL)
	}

	// Download via HTTP using the returned URL
	downloadReq := httptest.NewRequest(http.MethodGet, fileURL+"?session_id=http-session-1&participant_id=http-user-1", nil)
	downloadRec := httptest.NewRecorder()
	fr.HandleDownload(downloadRec, downloadReq)

	if downloadRec.Code != http.StatusOK {
		t.Fatalf("download returned %d: %s", downloadRec.Code, downloadRec.Body.String())
	}

	downloadedContent, _ := io.ReadAll(downloadRec.Body)
	if !bytes.Equal(downloadedContent, content) {
		t.Error("downloaded content does not match uploaded content")
	}
}
