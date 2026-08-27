package cloudworkspace

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/archiveutil"
	"github.com/RapidAI/CodeClaw/corelib/fileutil"
)

var (
	ErrInvalidBlobKey = errors.New("invalid cloud workspace blob key")
	ErrBlobNotFound   = errors.New("cloud workspace object not found")
	ErrBlobCorrupt    = errors.New("cloud workspace object corrupt")
	ErrBlobTooLarge   = errors.New("cloud workspace object too large")
	ErrDiskFull       = errors.New("insufficient disk space for cloud workspace object")
)

const objectFileExt = ".enc"

// BlobStore is the Hub-side content-addressed encrypted object store.
// HTTP bodies (when added in PR5b) stay plaintext; Hub hashes then seals.
// Writes that mutate a workspace still require a lease; this type is the
// library only.
type BlobStore struct {
	Root           string
	KeyDir         string
	DB             *sql.DB
	MaxObjectBytes int64
}

// PutResult is the identity of a stored plaintext object.
type PutResult struct {
	SHA256    string
	SizeBytes int64
	Existed   bool
}

func (s *BlobStore) keyDir() string {
	if s != nil && strings.TrimSpace(s.KeyDir) != "" {
		return s.KeyDir
	}
	if s == nil {
		return ""
	}
	return s.Root
}

func (s *BlobStore) maxObjectBytes() int64 {
	if s != nil && s.MaxObjectBytes > 0 {
		return s.MaxObjectBytes
	}
	return defaultMaxWorkspaceBytes
}

func validPathSegment(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || s == "." || s == ".." {
		return false
	}
	if strings.ContainsAny(s, `/\`) || strings.ContainsRune(s, 0) {
		return false
	}
	return true
}

func validSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func (s *BlobStore) workspaceDir(tenantID, userID, workspaceID string) (string, error) {
	if s == nil || strings.TrimSpace(s.Root) == "" {
		return "", ErrUnavailable
	}
	if !validPathSegment(tenantID) || !validPathSegment(userID) || !validPathSegment(workspaceID) {
		return "", ErrInvalidBlobKey
	}
	return filepath.Join(s.Root, tenantID, userID, workspaceID), nil
}

// ObjectsDir is {root}/{tenant}/{user}/{workspace}/objects.
func (s *BlobStore) ObjectsDir(tenantID, userID, workspaceID string) (string, error) {
	base, err := s.workspaceDir(tenantID, userID, workspaceID)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "objects"), nil
}

// StagingDir is {root}/{tenant}/{user}/{workspace}/staging.
func (s *BlobStore) StagingDir(tenantID, userID, workspaceID string) (string, error) {
	base, err := s.workspaceDir(tenantID, userID, workspaceID)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "staging"), nil
}

// ObjectPath is the on-disk AES-GCM blob {sha256}.enc.
func (s *BlobStore) ObjectPath(tenantID, userID, workspaceID, sha256hex string) (string, error) {
	if !validSHA256Hex(sha256hex) {
		return "", ErrInvalidBlobKey
	}
	dir, err := s.ObjectsDir(tenantID, userID, workspaceID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sha256hex+objectFileExt), nil
}

// PrepareStaging creates the per-workspace staging directory (0700).
func (s *BlobStore) PrepareStaging(tenantID, userID, workspaceID string) (string, error) {
	dir, err := s.StagingDir(tenantID, userID, workspaceID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// RemoveStaging deletes the staging directory if it exists.
func (s *BlobStore) RemoveStaging(tenantID, userID, workspaceID string) error {
	dir, err := s.StagingDir(tenantID, userID, workspaceID)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// Put hashes plaintext, seals it with a per-workspace DEK, and writes {sha256}.enc.
func (s *BlobStore) Put(ctx context.Context, tenantID, userID, workspaceID string, plaintext []byte) (PutResult, error) {
	if int64(len(plaintext)) > s.maxObjectBytes() {
		return PutResult{}, ErrBlobTooLarge
	}
	sum := plaintextSHA256(plaintext)
	path, err := s.ObjectPath(tenantID, userID, workspaceID, sum)
	if err != nil {
		return PutResult{}, err
	}
	if _, err := os.Stat(path); err == nil {
		if err := s.recordObject(ctx, workspaceID, sum, int64(len(plaintext))); err != nil {
			return PutResult{}, err
		}
		return PutResult{SHA256: sum, SizeBytes: int64(len(plaintext)), Existed: true}, nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return PutResult{}, err
	}
	master, err := loadMasterKey(s.keyDir())
	if err != nil {
		return PutResult{}, err
	}
	dek := deriveDEK(master, tenantID, userID, workspaceID)
	sealed, err := seal(dek, objectAAD(tenantID, userID, workspaceID), plaintext)
	if err != nil {
		return PutResult{}, err
	}
	if avail, err := archiveutil.AvailableBytes(dir); err == nil && avail < int64(len(sealed))+4096 {
		return PutResult{}, ErrDiskFull
	}
	if err := fileutil.AtomicWriteFile(path, sealed, 0o600); err != nil {
		return PutResult{}, err
	}
	if err := s.recordObject(ctx, workspaceID, sum, int64(len(plaintext))); err != nil {
		return PutResult{}, err
	}
	return PutResult{SHA256: sum, SizeBytes: int64(len(plaintext))}, nil
}

// Get decrypts {sha256}.enc and checks the plaintext hash.
func (s *BlobStore) Get(_ context.Context, tenantID, userID, workspaceID, sha256hex string) ([]byte, error) {
	path, err := s.ObjectPath(tenantID, userID, workspaceID, sha256hex)
	if err != nil {
		return nil, err
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrBlobNotFound
		}
		return nil, err
	}
	master, err := loadMasterKey(s.keyDir())
	if err != nil {
		return nil, err
	}
	dek := deriveDEK(master, tenantID, userID, workspaceID)
	plain, err := open(dek, objectAAD(tenantID, userID, workspaceID), blob)
	if err != nil {
		return nil, err
	}
	if plaintextSHA256(plain) != sha256hex {
		return nil, ErrBlobCorrupt
	}
	return plain, nil
}

// Has reports whether the encrypted object file exists.
func (s *BlobStore) Has(_ context.Context, tenantID, userID, workspaceID, sha256hex string) (bool, error) {
	path, err := s.ObjectPath(tenantID, userID, workspaceID, sha256hex)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (s *BlobStore) recordObject(ctx context.Context, workspaceID, sha256hex string, sizeBytes int64) error {
	if s == nil || s.DB == nil {
		return nil
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	_, err := s.DB.ExecContext(ctx, `
		INSERT OR IGNORE INTO cloud_workspace_objects (workspace_id, sha256, size_bytes, ref_count, created_at)
		VALUES (?, ?, ?, 0, ?)`,
		workspaceID, sha256hex, sizeBytes, ts,
	)
	return err
}
