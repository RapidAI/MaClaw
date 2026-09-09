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
	"unicode/utf8"

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
	// v2 disk blob is envelope magic/key id (40) + AES-GCM nonce/tag (28).
	gcmBlobOverhead int64 = int64(ciphertextEnvelopeHeaderSize + 12 + 16)
)

// BlobStore is the Hub-side content-addressed encrypted object store.
// HTTP bodies stay plaintext; Hub hashes then seals.
// Writes that mutate a workspace still require a lease; this type is the
// library only.
type BlobStore struct {
	Root           string
	KeyDir         string
	Keys           KeyProvider
	DB             *sql.DB
	MaxObjectBytes int64
}

type objectFinalizeGuard struct {
	TenantID         string
	UserID           string
	MachineID        string
	ClientInstanceID string
	FencingToken     int64
	Now              time.Time
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
	if s == "" || strings.TrimSpace(s) != s || s == "." || s == ".." || !utf8.ValidString(s) || len(s) > 128 {
		return false
	}
	if strings.ContainsAny(s, `/\:`) || strings.ContainsRune(s, 0) {
		return false
	}
	for _, r := range s {
		if r < 0x20 {
			return false
		}
	}
	// Keep Hub paths portable across Windows/macOS/Linux. In particular,
	// Windows treats trailing dots/spaces and device names specially, and a
	// colon can denote an alternate data stream (rejected above).
	if strings.HasSuffix(s, ".") || strings.HasSuffix(s, " ") {
		return false
	}
	base := strings.TrimRight(s, " .")
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
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
	// Keep cleanup error-aware and re-runnable. Previously intermediate
	// RemoveAll errors were discarded, allowing the database row to be purged
	// while encrypted objects or sidecars remained on disk.
	paths := []string{
		filepath.Join(base, sidecarDirName),
		filepath.Join(base, manifestDirName),
		filepath.Join(base, "objects"),
		filepath.Join(base, "staging"),
	}
	var firstErr error
	for _, path := range paths {
		if removeErr := os.RemoveAll(path); removeErr != nil && !os.IsNotExist(removeErr) && firstErr == nil {
			firstErr = removeErr
		}
	}
	if firstErr != nil {
		return firstErr
	}
	if removeErr := os.Remove(base); removeErr != nil && !os.IsNotExist(removeErr) {
		return removeErr
	}
	return nil
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
	return s.put(ctx, tenantID, userID, workspaceID, plaintext, nil)
}

func (s *BlobStore) put(ctx context.Context, tenantID, userID, workspaceID string, plaintext []byte, guard *objectFinalizeGuard) (PutResult, error) {
	if int64(len(plaintext)) > s.maxObjectBytes() {
		return PutResult{}, ErrBlobTooLarge
	}
	sum := plaintextSHA256(plaintext)
	path, err := s.ObjectPath(tenantID, userID, workspaceID, sum)
	if err != nil {
		return PutResult{}, err
	}
	if s.DB != nil {
		var state string
		queryErr := s.DB.QueryRowContext(ctx, `SELECT COALESCE(object_state, 'ready') FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ?`, workspaceID, sum).Scan(&state)
		if queryErr != nil && !errors.Is(queryErr, sql.ErrNoRows) {
			return PutResult{}, queryErr
		}
		if strings.EqualFold(strings.TrimSpace(state), "deleting") {
			return PutResult{}, ErrObjectDeleting
		}
	}
	if _, err := os.Stat(path); err == nil {
		ready := s.DB == nil
		if s.DB != nil {
			var state string
			if queryErr := s.DB.QueryRowContext(ctx, `SELECT COALESCE(object_state, 'ready') FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ?`, workspaceID, sum).Scan(&state); queryErr == nil {
				ready = strings.EqualFold(strings.TrimSpace(state), "ready")
			} else if !errors.Is(queryErr, sql.ErrNoRows) {
				return PutResult{}, queryErr
			}
		}
		if ready {
			result := PutResult{SHA256: sum, SizeBytes: int64(len(plaintext)), Existed: true}
			if err := s.finalizeReadyObject(ctx, workspaceID, sum, int64(len(plaintext)), result, guard); err != nil {
				return PutResult{}, err
			}
			return result, nil
		}
		// The file is an orphan/provisional write whose codec metadata never
		// committed. Rewrite it from the supplied plaintext so compression and
		// encryption metadata can be finalized deterministically.
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return PutResult{}, err
	}
	stored, compression, level := compressObject(plaintext)
	sealed, encryptionVersion, err := sealWorkspace(ctx, s.keyProvider(), tenantID, userID, workspaceID, objectAAD(tenantID, userID, workspaceID), stored)
	if err != nil {
		return PutResult{}, err
	}
	if avail, err := archiveutil.AvailableBytes(dir); err == nil && avail < int64(len(sealed))+4096 {
		return PutResult{}, ErrDiskFull
	}
	if err := fileutil.AtomicWriteFile(path, sealed, 0o600); err != nil {
		return PutResult{}, err
	}
	result := PutResult{SHA256: sum, SizeBytes: int64(len(plaintext))}
	if err := s.finalizeObjectMeta(ctx, workspaceID, sum, int64(len(plaintext)), int64(len(sealed)), compression, level, encryptionVersion, result, guard); err != nil {
		return PutResult{}, err
	}
	return result, nil
}

func (s *BlobStore) refreshObjectMetadata(ctx context.Context, workspaceID, sha string, plainSize int64) error {
	return s.finalizeReadyObject(ctx, workspaceID, sha, plainSize, PutResult{SHA256: sha, SizeBytes: plainSize, Existed: true}, nil)
}

// Get decrypts {sha256}.enc and checks the plaintext hash.
// ObjectPlainSize returns the committed plaintext size used by download
// admission. It applies the same ready-state gate as Get so a staging or
// deleting row is never pre-charged; missing rows report ErrBlobNotFound.
func (s *BlobStore) ObjectPlainSize(ctx context.Context, workspaceID, sha256hex string) (int64, error) {
	if s == nil || s.DB == nil {
		return 0, ErrUnavailable
	}
	var size int64
	var state string
	err := s.DB.QueryRowContext(ctx, `
		SELECT CASE WHEN COALESCE(plain_size_bytes, 0) > 0 THEN plain_size_bytes ELSE size_bytes END,
		       COALESCE(object_state, 'ready')
		  FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ?`, workspaceID, sha256hex).Scan(&size, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrBlobNotFound
	}
	if err != nil {
		return 0, err
	}
	if state != "" && !strings.EqualFold(strings.TrimSpace(state), "ready") {
		return 0, ErrBlobNotFound
	}
	return size, nil
}

func (s *BlobStore) Get(ctx context.Context, tenantID, userID, workspaceID, sha256hex string) ([]byte, error) {
	if s == nil {
		return nil, ErrUnavailable
	}
	path, err := s.ObjectPath(tenantID, userID, workspaceID, sha256hex)
	if err != nil {
		return nil, err
	}
	// The encrypted file is not itself the source of truth.  Require a
	// committed, ready metadata row before exposing bytes; otherwise an orphan
	// left by a crash between file fsync and metadata finalize could be read by
	// a client even though it can never be referenced by a manifest.
	if s.DB != nil {
		var state string
		err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(object_state, 'ready') FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ?`, workspaceID, sha256hex).Scan(&state)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrBlobNotFound
		}
		if err != nil {
			return nil, err
		}
		if state != "" && !strings.EqualFold(strings.TrimSpace(state), "ready") {
			return nil, ErrBlobNotFound
		}
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
	stored, _, err := openWorkspace(ctx, s.keyProvider(), tenantID, userID, workspaceID, objectAAD(tenantID, userID, workspaceID), blob)
	if err != nil {
		return nil, err
	}
	compression, plainSize := s.objectCompression(ctx, workspaceID, sha256hex, int64(len(stored)))
	plain, err := decompressObject(stored, compression, plainSize)
	if err != nil {
		return nil, err
	}
	if plaintextSHA256(plain) != sha256hex {
		return nil, ErrBlobCorrupt
	}
	return plain, nil
}

// Has reports whether the encrypted object file exists.
func (s *BlobStore) Has(ctx context.Context, tenantID, userID, workspaceID, sha256hex string) (bool, error) {
	if s == nil {
		return false, ErrUnavailable
	}
	path, err := s.ObjectPath(tenantID, userID, workspaceID, sha256hex)
	if err != nil {
		return false, err
	}
	if s.DB != nil {
		var state string
		err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(object_state, 'ready') FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ?`, workspaceID, sha256hex).Scan(&state)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if state != "" && !strings.EqualFold(strings.TrimSpace(state), "ready") {
			return false, nil
		}
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
	return s.finalizeReadyObject(ctx, workspaceID, sha256hex, sizeBytes, PutResult{SHA256: sha256hex, SizeBytes: sizeBytes, Existed: true}, nil)
}

func (s *BlobStore) recordObjectMeta(ctx context.Context, workspaceID, sha256hex string, plainSize, storedSize int64, compression string, level int) error {
	return s.finalizeObjectMeta(ctx, workspaceID, sha256hex, plainSize, storedSize, compression, level, "aes-gcm-v1", PutResult{SHA256: sha256hex, SizeBytes: plainSize}, nil)
}

func (s *BlobStore) finalizeReadyObject(ctx context.Context, workspaceID, sha256hex string, plainSize int64, result PutResult, guard *objectFinalizeGuard) error {
	if s == nil || s.DB == nil {
		return nil
	}
	workspaceStore := NewStore(s.DB)
	return workspaceStore.withImmediate(ctx, func(q queryer) error {
		if guard != nil {
			if _, err := requireActiveOwned(ctx, q, guard.TenantID, guard.UserID, workspaceID); err != nil {
				return err
			}
			if err := assertLeaseHeldForSession(ctx, q, workspaceID, guard.MachineID, guard.ClientInstanceID, guard.FencingToken, guard.Now); err != nil {
				return err
			}
		}
		res, err := q.ExecContext(ctx, `UPDATE cloud_workspace_objects SET size_bytes = CASE WHEN size_bytes = 0 THEN ? ELSE size_bytes END, plain_size_bytes = CASE WHEN plain_size_bytes = 0 THEN ? ELSE plain_size_bytes END WHERE workspace_id = ? AND sha256 = ? AND COALESCE(object_state, 'ready') = 'ready'`, plainSize, plainSize, workspaceID, sha256hex)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrBlobNotFound
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM cloud_workspace_staging_chunks WHERE workspace_id = ? AND sha256 = ?`, workspaceID, sha256hex); err != nil {
			return err
		}
		return stageAtomicIdempotency(ctx, q, result)
	})
}

func (s *BlobStore) finalizeObjectMeta(ctx context.Context, workspaceID, sha256hex string, plainSize, storedSize int64, compression string, level int, encryptionVersion string, result PutResult, guard *objectFinalizeGuard) error {
	if s == nil || s.DB == nil {
		return nil
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	// PrepareObjectPut creates a reservation row before bytes are sealed. Use an
	// upsert so the final codec metadata replaces the reservation defaults;
	// INSERT OR IGNORE would leave compression=zstd objects marked as plain.
	workspaceStore := NewStore(s.DB)
	return workspaceStore.withImmediate(ctx, func(q queryer) error {
		if guard != nil {
			if _, err := requireActiveOwned(ctx, q, guard.TenantID, guard.UserID, workspaceID); err != nil {
				return err
			}
			if err := assertLeaseHeldForSession(ctx, q, workspaceID, guard.MachineID, guard.ClientInstanceID, guard.FencingToken, guard.Now); err != nil {
				return err
			}
		}
		var state string
		if err := q.QueryRowContext(ctx, `SELECT COALESCE(object_state, 'ready') FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ?`, workspaceID, sha256hex).Scan(&state); err == nil {
			if strings.EqualFold(strings.TrimSpace(state), "deleting") {
				return ErrObjectDeleting
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := q.ExecContext(ctx, `
			INSERT INTO cloud_workspace_objects (workspace_id, sha256, size_bytes, plain_size_bytes, stored_size_bytes, compression, compression_level, encryption_version, ref_count, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?)
			ON CONFLICT(workspace_id, sha256) DO UPDATE SET
				size_bytes = excluded.size_bytes,
				plain_size_bytes = excluded.plain_size_bytes,
				stored_size_bytes = excluded.stored_size_bytes,
				compression = excluded.compression,
				compression_level = excluded.compression_level,
				encryption_version = excluded.encryption_version,
				object_state = 'ready'`,
			workspaceID, sha256hex, plainSize, plainSize, storedSize, compression, level, encryptionVersion, ts,
		); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM cloud_workspace_staging_chunks WHERE workspace_id = ? AND sha256 = ?`, workspaceID, sha256hex); err != nil {
			return err
		}
		return stageAtomicIdempotency(ctx, q, result)
	})
}

func (s *BlobStore) objectCompression(ctx context.Context, workspaceID, sha string, fallback int64) (string, int64) {
	if s == nil || s.DB == nil {
		return "none", fallback
	}
	var compression string
	var plainSize int64
	if err := s.DB.QueryRowContext(ctx, `SELECT compression, plain_size_bytes FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ?`, workspaceID, sha).Scan(&compression, &plainSize); err != nil {
		return "none", fallback
	}
	if plainSize <= 0 {
		plainSize = fallback
	}
	return compression, plainSize
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
	return s.PutExpectedWithGuard(ctx, tenantID, userID, workspaceID, expectedSHA, plaintext, nil)
}

func (s *BlobStore) PutExpectedWithGuard(ctx context.Context, tenantID, userID, workspaceID, expectedSHA string, plaintext []byte, guard *objectFinalizeGuard) (PutResult, error) {
	if !validSHA256Hex(expectedSHA) {
		return PutResult{}, ErrInvalidBlobKey
	}
	if plaintextSHA256(plaintext) != expectedSHA {
		return PutResult{}, ErrBlobHashMismatch
	}
	return s.put(ctx, tenantID, userID, workspaceID, plaintext, guard)
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
	// Bound the complete staging set, not just each chunk. Otherwise callers can
	// create many partial hashes and exhaust the volume before CompleteObject.
	var staged int64
	if parts, readErr := os.ReadDir(dir); readErr == nil {
		for _, part := range parts {
			if part.IsDir() {
				continue
			}
			raw, statErr := os.Stat(filepath.Join(dir, part.Name()))
			if statErr == nil {
				staged += raw.Size()
			}
		}
	}
	if old, statErr := os.Stat(filepath.Join(dir, strconv.Itoa(index))); statErr == nil {
		staged -= old.Size()
	}
	if staged+int64(len(data)) > s.maxObjectBytes() {
		return ErrBlobTooLarge
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
	err = os.RemoveAll(dir)
	if s.DB != nil {
		if _, dbErr := s.DB.Exec(`DELETE FROM cloud_workspace_staging_chunks WHERE workspace_id = ? AND sha256 = ?`, workspaceID, sha256hex); err == nil {
			err = dbErr
		}
	}
	return err
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
