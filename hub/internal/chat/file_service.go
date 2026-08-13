package chat

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/google/uuid"
)

// FileService handles file uploads and downloads for chat.
type FileService struct {
	store   *Store
	dataDir string // directory to store uploaded files
}

// NewFileService creates a FileService.
func NewFileService(store *Store, dataDir string) *FileService {
	_ = os.MkdirAll(dataDir, 0o755)
	return &FileService{store: store, dataDir: dataDir}
}

// DataDir returns the directory that contains chat upload files.
func (s *FileService) DataDir() string {
	if s == nil {
		return ""
	}
	return s.dataDir
}

// Upload stores a file and returns its metadata.
func (s *FileService) Upload(uploaderID, channelID, filename, mimeType string, size int64, reader io.Reader) (*FileRecord, error) {
	tenantID := store.DefaultTenantID
	if ch, err := s.store.GetChannel(channelID); err == nil && ch != nil {
		tenantID = store.NormalizeTenantID(ch.TenantID)
	}
	id := uuid.NewString()
	ext := filepath.Ext(filename)
	diskName := id + ext

	dst, err := os.Create(filepath.Join(s.dataDir, diskName))
	if err != nil {
		return nil, fmt.Errorf("create file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, reader); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	rec := &FileRecord{
		ID:         id,
		TenantID:   tenantID,
		UploaderID: uploaderID,
		ChannelID:  channelID,
		Filename:   filename,
		MimeType:   mimeType,
		Size:       size,
		CreatedAt:  time.Now(),
	}

	_, err = s.store.db.Exec(
		`INSERT INTO chat_files (id, tenant_id, uploader_id, channel_id, filename, mime_type, size, has_thumb, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		rec.ID, rec.TenantID, rec.UploaderID, rec.ChannelID, rec.Filename, rec.MimeType, rec.Size, rec.CreatedAt.UnixMilli(),
	)
	if err != nil {
		return nil, fmt.Errorf("insert file record: %w", err)
	}

	return rec, nil
}

// FilePath returns the disk path for a file ID.
func (s *FileService) FilePath(fileID, ext string) string {
	return filepath.Join(s.dataDir, fileID+ext)
}

// GetFileRecord retrieves file metadata by ID.
func (s *FileService) GetFileRecord(fileID string) (*FileRecord, error) {
	var rec FileRecord
	var createdAt int64
	err := s.store.db.QueryRow(
		`SELECT id, tenant_id, uploader_id, channel_id, filename, mime_type, size, has_thumb, created_at
		 FROM chat_files WHERE id = ?`, fileID,
	).Scan(&rec.ID, &rec.TenantID, &rec.UploaderID, &rec.ChannelID, &rec.Filename, &rec.MimeType, &rec.Size, &rec.HasThumb, &createdAt)
	if err != nil {
		return nil, err
	}
	rec.CreatedAt = time.UnixMilli(createdAt)
	return &rec, nil
}
