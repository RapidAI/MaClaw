package cloudworkspace

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
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

const (
	objectFileExt = ".enc"
	objectPartExt = ".part"
	// MaxObjectBytes is the single-file plaintext cap (workspace quota is separate).
	MaxObjectBytes int64 = 64 << 20
	// MaxChunkBytes is the per-chunk plaintext cap for staged uploads.
	MaxChunkBytes int64 = 8 << 20
	// VolumeReserveBytes is the free-space floor before admitting a write.
	VolumeReserveBytes    int64 = 1 << 30
	maxChunkCount               = 16
	defaultMaxObjectBytes       = MaxObjectBytes
	// AES-GCM disk blob is nonce (12) || ciphertext || tag (16).
	gcmBlobOverhead int64 = 12 + 16
)

// BlobStore is the Hub-side content-addressed encrypted object store.
// HTTP bodies stay plaintext; Hub hashes then seals.
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
	return defaultMaxObjectBytes
}

func (s *BlobStore) maxCiphertextBytes() int64 {
	return s.maxObjectBytes() + gcmBlobOverhead
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

// ValidSHA256Hex reports whether s is a lowercase 64-char hex digest.
func ValidSHA256Hex(s string) bool {
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

func validSHA256Hex(s string) bool { return ValidSHA256Hex(s) }

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

// RemoveWorkspace deletes objects, staging, sidecars, and the manifest dir.
func (s *BlobStore) RemoveWorkspace(tenantID, userID, workspaceID string) error {
	base, err := s.workspaceDir(tenantID, userID, workspaceID)
	if err != nil {
		return err
	}
	_ = os.RemoveAll(filepath.Join(base, sidecarDirName))
	_ = os.RemoveAll(filepath.Join(base, manifestDirName))
	objects, err := s.ObjectsDir(tenantID, userID, workspaceID)
	if err != nil {
		return err
	}
	_ = os.RemoveAll(objects)
	_ = os.RemoveAll(filepath.Join(base, "staging"))
	return os.RemoveAll(base)
}

// RemoveObjectFile deletes {sha256}.enc if present.
func (s *BlobStore) RemoveObjectFile(tenantID, userID, workspaceID, sha256hex string) error {
	path, err := s.ObjectPath(tenantID, userID, workspaceID, sha256hex)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func latestModTime(path string) (time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	latest := info.ModTime()
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return latest, nil
		}
		return time.Time{}, err
	}
	for _, e := range entries {
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if fi.ModTime().After(latest) {
			latest = fi.ModTime()
		}
	}
	return latest, nil
}

func staleDir(path string, now time.Time, maxAge time.Duration) bool {
	mt, err := latestModTime(path)
	if err != nil {
		return false
	}
	return !mt.After(now.Add(-maxAge))
}

// RemoveStaleParts deletes incomplete objects/{sha256}.part and staging dirs older than maxAge.
func (s *BlobStore) RemoveStaleParts(now time.Time, maxAge time.Duration) (int, error) {
	if s == nil || strings.TrimSpace(s.Root) == "" {
		return 0, nil
	}
	now = now.UTC()
	if maxAge <= 0 {
		maxAge = StagingGrace
	}
	removed := 0
	err := filepath.WalkDir(s.Root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		switch {
		case name == "staging" || strings.HasSuffix(name, objectPartExt):
			if !staleDir(path, now, maxAge) {
				return filepath.SkipDir
			}
			if err := os.RemoveAll(path); err != nil {
				return err
			}
			removed++
			return filepath.SkipDir
		default:
			return nil
		}
	})
	return removed, err
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
	if _, err := s.statObject(path); err != nil {
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
	if _, err := s.statObject(path); err != nil {
		if errors.Is(err, ErrBlobNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *BlobStore) statObject(path string) (os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrBlobNotFound
		}
		return nil, err
	}
	if info.Size() > s.maxCiphertextBytes() {
		return nil, ErrBlobTooLarge
	}
	return info, nil
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

// PartDir is {objects}/{sha256}.part for plaintext chunk staging.
func (s *BlobStore) PartDir(tenantID, userID, workspaceID, sha256hex string) (string, error) {
	if !validSHA256Hex(sha256hex) {
		return "", ErrInvalidBlobKey
	}
	dir, err := s.ObjectsDir(tenantID, userID, workspaceID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sha256hex+objectPartExt), nil
}

// PutExpected seals plaintext only when sha256(body) equals expectedSHA.
func (s *BlobStore) PutExpected(ctx context.Context, tenantID, userID, workspaceID, expectedSHA string, plaintext []byte) (PutResult, error) {
	if !validSHA256Hex(expectedSHA) {
		return PutResult{}, ErrInvalidBlobKey
	}
	if plaintextSHA256(plaintext) != expectedSHA {
		return PutResult{}, ErrBlobHashMismatch
	}
	return s.Put(ctx, tenantID, userID, workspaceID, plaintext)
}

// PutChunk writes one plaintext slice to objects/{sha256}.part/{index}.
func (s *BlobStore) PutChunk(_ context.Context, tenantID, userID, workspaceID, sha256hex string, index int, data []byte) error {
	if !validSHA256Hex(sha256hex) {
		return ErrInvalidBlobKey
	}
	if index < 0 || index >= maxChunkCount {
		return ErrInvalidChunkIndex
	}
	if len(data) == 0 {
		return ErrInvalidChunkIndex
	}
	if int64(len(data)) > MaxChunkBytes {
		return ErrBlobTooLarge
	}
	if int64(len(data)) > s.maxObjectBytes() {
		return ErrBlobTooLarge
	}
	dir, err := s.PartDir(tenantID, userID, workspaceID, sha256hex)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if avail, err := archiveutil.AvailableBytes(dir); err == nil && avail < int64(len(data))+4096 {
		return ErrDiskFull
	}
	return fileutil.AtomicWriteFile(filepath.Join(dir, strconv.Itoa(index)), data, 0o600)
}

// RemovePart deletes the chunk staging directory if it exists.
func (s *BlobStore) RemovePart(tenantID, userID, workspaceID, sha256hex string) error {
	dir, err := s.PartDir(tenantID, userID, workspaceID, sha256hex)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// AssembleChunks concatenates contiguous staged slices and verifies the digest.
// On hash mismatch the staging directory is deleted.
func (s *BlobStore) AssembleChunks(tenantID, userID, workspaceID, sha256hex string) ([]byte, error) {
	if !validSHA256Hex(sha256hex) {
		return nil, ErrInvalidBlobKey
	}
	dir, err := s.PartDir(tenantID, userID, workspaceID, sha256hex)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrIncompleteChunks
		}
		return nil, err
	}
	present := map[int]struct{}{}
	maxIdx := -1
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n, convErr := strconv.Atoi(e.Name())
		if convErr != nil || n < 0 || strconv.Itoa(n) != e.Name() {
			continue
		}
		present[n] = struct{}{}
		if n > maxIdx {
			maxIdx = n
		}
	}
	if maxIdx < 0 {
		return nil, ErrIncompleteChunks
	}
	var total int64
	parts := make([][]byte, maxIdx+1)
	for i := 0; i <= maxIdx; i++ {
		if _, ok := present[i]; !ok {
			return nil, ErrIncompleteChunks
		}
		raw, readErr := os.ReadFile(filepath.Join(dir, strconv.Itoa(i)))
		if readErr != nil {
			return nil, readErr
		}
		if int64(len(raw)) > MaxChunkBytes {
			return nil, ErrBlobTooLarge
		}
		total += int64(len(raw))
		if total > s.maxObjectBytes() {
			return nil, ErrBlobTooLarge
		}
		parts[i] = raw
	}
	out := make([]byte, 0, total)
	for _, p := range parts {
		out = append(out, p...)
	}
	if plaintextSHA256(out) != sha256hex {
		_ = os.RemoveAll(dir)
		return nil, ErrBlobHashMismatch
	}
	return out, nil
}
