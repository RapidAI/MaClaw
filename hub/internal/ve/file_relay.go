package ve

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// File size limits by category.
const (
	MaxTextFileSize     = 500 * 1024       // 500KB
	MaxImageFileSize    = 10 * 1024 * 1024 // 10MB
	MaxDocumentFileSize = 20 * 1024 * 1024 // 20MB
	FileTTL             = 24 * time.Hour
	CleanupInterval     = 1 * time.Hour
)

// Allowed MIME type prefixes/values.
var allowedMIMETypes = map[string]bool{
	"application/pdf": true,
}

var allowedMIMEPrefixes = []string{
	"text/",
	"image/png",
	"image/jpeg",
	"image/gif",
	"image/webp",
	"application/vnd.openxmlformats",
}

// fileCategory classifies a MIME type into text/image/document for size validation.
func fileCategory(mimeType string) string {
	if strings.HasPrefix(mimeType, "text/") {
		return "text"
	}
	if strings.HasPrefix(mimeType, "image/") {
		return "image"
	}
	return "document"
}

// isAllowedMIME checks if the given MIME type is permitted.
func isAllowedMIME(mimeType string) bool {
	if allowedMIMETypes[mimeType] {
		return true
	}
	for _, prefix := range allowedMIMEPrefixes {
		if strings.HasPrefix(mimeType, prefix) {
			return true
		}
	}
	return false
}

// maxSizeForCategory returns the maximum allowed file size for a given category.
func maxSizeForCategory(category string) int64 {
	switch category {
	case "text":
		return MaxTextFileSize
	case "image":
		return MaxImageFileSize
	default:
		return MaxDocumentFileSize
	}
}

// FileMetadata stores metadata for an uploaded file.
type FileMetadata struct {
	ID         string    `json:"id"`
	Filename   string    `json:"filename"`
	MimeType   string    `json:"mime_type"`
	Size       int64     `json:"size"`
	SessionID  string    `json:"session_id"`
	UploaderID string    `json:"uploader_id"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// SessionParticipantValidator verifies that a participant belongs to a session.
// If no validator is set (nil), the FileRelay falls back to session_id match only.
type SessionParticipantValidator interface {
	IsSessionParticipant(sessionID, participantID string) bool
}

// FileRelay manages temporary file storage for VE conversations.
type FileRelay struct {
	dataDir   string
	mu        sync.RWMutex
	files     map[string]*FileMetadata // key: file_id
	validator SessionParticipantValidator

	cancel context.CancelFunc
	done   chan struct{}
}

// NewFileRelay creates a new FileRelay instance.
func NewFileRelay(dataDir string) *FileRelay {
	_ = os.MkdirAll(dataDir, 0o755)
	return &FileRelay{
		dataDir: dataDir,
		files:   make(map[string]*FileMetadata),
		done:    make(chan struct{}),
	}
}

// SetParticipantValidator sets the validator used to verify participant membership
// in sessions. If nil, the FileRelay falls back to session_id match only.
func (fr *FileRelay) SetParticipantValidator(v SessionParticipantValidator) {
	fr.validator = v
}

// Start begins the TTL cleanup goroutine.
// Calling Start() on an already-started FileRelay is a no-op.
func (fr *FileRelay) Start(ctx context.Context) {
	fr.mu.Lock()
	if fr.cancel != nil {
		fr.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	fr.cancel = cancel
	fr.done = done
	fr.mu.Unlock()

	go fr.cleanupLoop(ctx, done)
}

// Stop terminates the cleanup goroutine.
func (fr *FileRelay) Stop() {
	fr.mu.Lock()
	cancel := fr.cancel
	done := fr.done
	if cancel == nil {
		fr.mu.Unlock()
		return
	}
	fr.cancel = nil
	fr.mu.Unlock()

	cancel()
	<-done
}

// cleanupLoop periodically removes expired files.
func (fr *FileRelay) cleanupLoop(ctx context.Context, done chan struct{}) {
	defer close(done)
	defer func() {
		fr.mu.Lock()
		if fr.done == done {
			fr.cancel = nil
		}
		fr.mu.Unlock()
	}()
	ticker := time.NewTicker(CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fr.cleanExpired()
		}
	}
}

// cleanExpired removes files that have exceeded their TTL.
func (fr *FileRelay) cleanExpired() {
	now := time.Now()

	// Phase 1: Collect expired file IDs under read lock.
	fr.mu.RLock()
	var expired []string
	for id, meta := range fr.files {
		if now.After(meta.ExpiresAt) {
			expired = append(expired, id)
		}
	}
	fr.mu.RUnlock()

	if len(expired) == 0 {
		return
	}

	// Phase 2: Remove expired files under write lock.
	fr.mu.Lock()
	defer fr.mu.Unlock()

	for _, id := range expired {
		meta, ok := fr.files[id]
		if !ok {
			continue
		}
		// Re-check expiry in case it was refreshed between phases.
		if !now.After(meta.ExpiresAt) {
			continue
		}
		// Remove file from disk.
		ext := filepath.Ext(meta.Filename)
		diskPath := filepath.Join(fr.dataDir, id+ext)
		_ = os.Remove(diskPath)
		delete(fr.files, id)
	}
}

// Upload stores a file and returns its metadata.
func (fr *FileRelay) Upload(filename, mimeType, sessionID, uploaderID string, size int64, reader io.Reader) (*FileMetadata, error) {
	// Validate MIME type.
	if !isAllowedMIME(mimeType) {
		return nil, fmt.Errorf("unsupported file type: %s", mimeType)
	}

	// Validate file size.
	category := fileCategory(mimeType)
	maxSize := maxSizeForCategory(category)
	if size > maxSize {
		return nil, fmt.Errorf("file size %d exceeds limit %d for %s files", size, maxSize, category)
	}

	id := uuid.NewString()
	ext := filepath.Ext(filename)
	diskName := id + ext

	dst, err := os.Create(filepath.Join(fr.dataDir, diskName))
	if err != nil {
		return nil, fmt.Errorf("create file: %w", err)
	}
	defer dst.Close()

	// Use a LimitReader to enforce size limit during copy.
	limited := io.LimitReader(reader, maxSize+1)
	written, err := io.Copy(dst, limited)
	if err != nil {
		_ = os.Remove(filepath.Join(fr.dataDir, diskName))
		return nil, fmt.Errorf("write file: %w", err)
	}
	if written > maxSize {
		_ = os.Remove(filepath.Join(fr.dataDir, diskName))
		return nil, fmt.Errorf("file size exceeds limit %d for %s files", maxSize, category)
	}

	now := time.Now()
	meta := &FileMetadata{
		ID:         id,
		Filename:   filename,
		MimeType:   mimeType,
		Size:       written,
		SessionID:  sessionID,
		UploaderID: uploaderID,
		CreatedAt:  now,
		ExpiresAt:  now.Add(FileTTL),
	}

	fr.mu.Lock()
	fr.files[id] = meta
	fr.mu.Unlock()

	return meta, nil
}

// GetFile retrieves file metadata by ID. Returns nil if not found or expired.
func (fr *FileRelay) GetFile(fileID string) *FileMetadata {
	fr.mu.RLock()
	defer fr.mu.RUnlock()

	meta, ok := fr.files[fileID]
	if !ok {
		return nil
	}
	if time.Now().After(meta.ExpiresAt) {
		return nil
	}
	return meta
}

// FilePath returns the disk path for a given file.
func (fr *FileRelay) FilePath(meta *FileMetadata) string {
	ext := filepath.Ext(meta.Filename)
	return filepath.Join(fr.dataDir, meta.ID+ext)
}

// --- HTTP Handlers ---

// HandleUpload handles POST /api/ve/files/upload.
func (fr *FileRelay) HandleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeVEError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}

	// Parse multipart form with a generous memory limit.
	// The actual file size is validated per-category.
	if err := r.ParseMultipartForm(MaxDocumentFileSize + 1024*1024); err != nil {
		writeVEError(w, http.StatusBadRequest, "invalid_request", "failed to parse multipart form: "+err.Error())
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeVEError(w, http.StatusBadRequest, "missing_file", "file field is required")
		return
	}
	defer file.Close()

	// Determine MIME type.
	mimeType := detectMIMEType(header)

	// Extract session_id and participant_id from form or headers.
	sessionID := r.FormValue("session_id")
	if sessionID == "" {
		sessionID = r.Header.Get("X-Session-ID")
	}
	participantID := r.FormValue("participant_id")
	if participantID == "" {
		participantID = r.Header.Get("X-Participant-ID")
	}

	meta, err := fr.Upload(header.Filename, mimeType, sessionID, participantID, header.Size, file)
	if err != nil {
		// Determine appropriate status code.
		status := http.StatusBadRequest
		code := "upload_failed"
		if strings.Contains(err.Error(), "unsupported file type") {
			code = "unsupported_type"
		} else if strings.Contains(err.Error(), "exceeds limit") {
			code = "size_exceeded"
			status = http.StatusRequestEntityTooLarge
		}
		writeVEError(w, status, code, err.Error())
		return
	}

	// Return success response.
	resp := map[string]any{
		"ok":       true,
		"file_id":  meta.ID,
		"file_url": fmt.Sprintf("/api/ve/files/%s", meta.ID),
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// HandleDownload handles GET /api/ve/files/{id}.
func (fr *FileRelay) HandleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeVEError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required")
		return
	}

	// Extract file ID from path.
	fileID := extractFileID(r.URL.Path)
	if fileID == "" {
		writeVEError(w, http.StatusBadRequest, "invalid_request", "file ID is required")
		return
	}

	// Validate authorization.
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		sessionID = r.Header.Get("X-Session-ID")
	}
	participantID := r.URL.Query().Get("participant_id")
	if participantID == "" {
		participantID = r.Header.Get("X-Participant-ID")
	}

	meta := fr.GetFile(fileID)
	if meta == nil {
		writeVEError(w, http.StatusNotFound, "not_found", "file not found or expired")
		return
	}

	// Authorization for group chat attachment broadcast (Requirement 11.11, Task 17.7):
	//
	// In group chats, all participants share the same session_id. When participant A
	// uploads a file, the GroupDiscussionMessage containing the file_url is broadcast
	// to all participants (B, C, D...). Each participant needs to download the file
	// using their own participant_id but the shared session_id.
	//
	// Authorization paths (any one grants access):
	// 1. Session match: requester's session_id == file's session_id (covers all
	//    participants in the same group session).
	// 2. Participant validator confirms membership (defense-in-depth for group sessions).
	// 3. Uploader match: requester is the original uploader (owner access fallback).
	authorized := false

	// Path 1: Direct session_id match: covers 1:1 and group chats where all
	// participants share the same session_id.
	if meta.SessionID != "" && sessionID != "" && meta.SessionID == sessionID {
		authorized = true
	}

	// Path 2: Participant validator confirms the requester belongs to the file's
	// session. This is the primary authorization path for group chat participants
	// accessing files uploaded by other participants in the same group.
	if !authorized && fr.validator != nil && meta.SessionID != "" && participantID != "" {
		if fr.validator.IsSessionParticipant(meta.SessionID, participantID) {
			authorized = true
		}
	}

	// Path 3: Uploader/owner access: the participant who uploaded the file can
	// always access it regardless of session_id.
	if !authorized && participantID != "" && meta.UploaderID != "" && participantID == meta.UploaderID {
		authorized = true
	}

	if !authorized {
		writeVEError(w, http.StatusForbidden, "access_denied", "not authorized to access this file")
		return
	}

	// Serve the file.
	diskPath := fr.FilePath(meta)
	f, err := os.Open(diskPath)
	if err != nil {
		writeVEError(w, http.StatusNotFound, "not_found", "file not found on disk")
		return
	}
	defer f.Close()

	// Sanitize filename for Content-Disposition header to prevent header injection.
	safeFilename := sanitizeFilename(meta.Filename)
	w.Header().Set("Content-Type", meta.MimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, safeFilename))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

// RegisterRoutes registers the file relay HTTP routes on the given mux.
func (fr *FileRelay) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/ve/files/upload", fr.HandleUpload)
	mux.HandleFunc("GET /api/ve/files/{id}", fr.HandleDownload)
}

// --- Helpers ---

// detectMIMEType determines the MIME type from the multipart file header.
func detectMIMEType(header *multipart.FileHeader) string {
	// First try Content-Type from the multipart header.
	ct := header.Header.Get("Content-Type")
	if ct != "" && ct != "application/octet-stream" {
		mediaType, _, _ := mime.ParseMediaType(ct)
		if mediaType != "" {
			return mediaType
		}
	}
	// Fallback: infer from file extension.
	ext := strings.ToLower(filepath.Ext(header.Filename))
	switch ext {
	case ".txt", ".log", ".csv":
		return "text/plain"
	case ".md":
		return "text/markdown"
	case ".json":
		return "application/json"
	case ".xml":
		return "text/xml"
	case ".yaml", ".yml":
		return "text/yaml"
	case ".go", ".py", ".js", ".ts", ".html", ".css":
		return "text/plain"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".pdf":
		return "application/pdf"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	default:
		return "application/octet-stream"
	}
}

// extractFileID extracts the file ID from a URL path like /api/ve/files/{id}.
func extractFileID(path string) string {
	const prefix = "/api/ve/files/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	id := strings.TrimPrefix(path, prefix)
	// Remove any trailing slashes or query params that might leak in.
	if idx := strings.IndexByte(id, '/'); idx >= 0 {
		id = id[:idx]
	}
	return id
}

// sanitizeFilename removes characters that could cause header injection or path traversal.
func sanitizeFilename(name string) string {
	// Remove path separators and control characters.
	var sb strings.Builder
	for _, r := range name {
		if r == '/' || r == '\\' || r == '"' || r < 32 || r == 127 {
			continue
		}
		sb.WriteRune(r)
	}
	result := sb.String()
	if result == "" {
		return "download"
	}
	return result
}

// writeVEError writes a JSON error response (local helper matching hub pattern).
func writeVEError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      false,
		"code":    code,
		"message": message,
	})
}
