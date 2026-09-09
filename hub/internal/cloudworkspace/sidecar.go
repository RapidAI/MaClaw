package cloudworkspace

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/archiveutil"
	"github.com/RapidAI/CodeClaw/corelib/fileutil"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

var sidecarCodecMagic = [4]byte{'M', 'C', 'S', '1'}
var sidecarWriteMu sync.Mutex

func encodeSidecar(plain []byte) ([]byte, error) {
	if int64(len(plain)) > MaxSidecarBytes {
		return nil, ErrBlobTooLarge
	}
	stored, compression, _ := compressObject(plain)
	out := make([]byte, 13+len(stored))
	copy(out[:4], sidecarCodecMagic[:])
	if compression == "zstd" {
		out[4] = 1
	}
	binary.BigEndian.PutUint64(out[5:13], uint64(len(plain)))
	copy(out[13:], stored)
	return out, nil
}

func decodeSidecar(data []byte) ([]byte, error) {
	if len(data) < 13 || string(data[:4]) != string(sidecarCodecMagic[:]) {
		return data, nil
	}
	if data[4] != 0 && data[4] != 1 {
		return nil, ErrBlobCorrupt
	}
	plainSize := int64(binary.BigEndian.Uint64(data[5:13]))
	if plainSize < 0 || plainSize > MaxSidecarBytes {
		return nil, ErrBlobTooLarge
	}
	compression := "none"
	if data[4] == 1 {
		compression = "zstd"
	}
	if compression == "none" && int64(len(data[13:])) != plainSize {
		return nil, ErrBlobCorrupt
	}
	return decompressObject(data[13:], compression, plainSize)
}

const (
	sidecarDirName = "sidecars"

	SidecarSession    = "session.json"
	SidecarTask       = "task.json"
	SidecarWorkbench  = "coding_workbench.json"
	SidecarCheckpoint = "coding_exec_checkpoint.json"
)

// MaxSidecarBytes is the plaintext cap for one named sidecar (not the file tree).
const MaxSidecarBytes int64 = MaxObjectBytes

// TaskSidecar is the Hub task.json payload used to restore the GUI task list.
type TaskSidecar struct {
	WorkspaceID    string `json:"workspace_id,omitempty"`
	CloudTaskID    string `json:"cloud_task_id,omitempty"`
	BindingVersion int64  `json:"binding_version,omitempty"`
	Name           string `json:"name"`
	Mode           string `json:"mode"`
	Tag            string `json:"tag"`
}

// Sidecar is a named task-continuity blob together with its CAS revision.
// Revisions are SHA-256 digests of the plaintext of the single canonical file,
// which keeps reads stateless.
type Sidecar struct {
	Data     []byte
	Revision string
}

func ParseTaskSidecar(data []byte) TaskSidecar {
	var task TaskSidecar
	if len(data) == 0 {
		return task
	}
	_ = json.Unmarshal(data, &task)
	task.Name = strings.TrimSpace(task.Name)
	task.Mode = strings.TrimSpace(task.Mode)
	task.Tag = strings.TrimSpace(task.Tag)
	task.WorkspaceID = strings.TrimSpace(task.WorkspaceID)
	task.CloudTaskID = strings.TrimSpace(task.CloudTaskID)
	return task
}

func (s *Service) taskSidecarFor(ctx context.Context, tenantID, userID, workspaceID string) TaskSidecar {
	blobs, err := s.blobs()
	if err != nil {
		return TaskSidecar{}
	}
	data, err := blobs.GetSidecar(ctx, tenantID, userID, workspaceID, SidecarTask)
	if err == nil && len(data) > 0 {
		return ParseTaskSidecar(data)
	}
	if binding, bindErr := s.Workspaces.GetTaskBinding(ctx, tenantID, userID, workspaceID); bindErr == nil && binding != nil {
		return TaskSidecar{WorkspaceID: workspaceID, CloudTaskID: binding.CloudTaskID, BindingVersion: binding.Version, Name: binding.Name, Mode: binding.Mode, Tag: binding.Tag}
	}
	return TaskSidecar{}
}

func sidecarAAD(tenantID, userID, workspaceID, name string) []byte {
	return []byte(tenantID + "|" + userID + "|" + workspaceID + "|sidecar|" + name)
}

// ValidateSidecarName allowlists the four session-continuity blobs.
func ValidateSidecarName(name string) (string, error) {
	switch name {
	case SidecarSession, SidecarTask, SidecarWorkbench, SidecarCheckpoint:
		return name, nil
	default:
		return "", ErrInvalidSidecarName
	}
}

func (s *BlobStore) SidecarsDir(tenantID, userID, workspaceID string) (string, error) {
	base, err := s.workspaceDir(tenantID, userID, workspaceID)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, sidecarDirName), nil
}

func (s *BlobStore) SidecarPath(tenantID, userID, workspaceID, name string) (string, error) {
	name, err := ValidateSidecarName(name)
	if err != nil {
		return "", err
	}
	dir, err := s.SidecarsDir(tenantID, userID, workspaceID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+objectFileExt), nil
}

// stageSidecarFile seals the payload and writes it to an fsync'd temporary
// sibling of the canonical path. The caller renames it into place after the
// metadata transaction commits, so a failed commit never destroys the
// previously committed canonical content.
func (s *BlobStore) stageSidecarFile(ctx context.Context, tenantID, userID, workspaceID, name, canonicalPath string, plaintext []byte) (string, error) {
	if s == nil {
		return "", ErrUnavailable
	}
	if _, err := ValidateSidecarName(name); err != nil {
		return "", err
	}
	dir := filepath.Dir(canonicalPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	encoded, err := encodeSidecar(plaintext)
	if err != nil {
		return "", err
	}
	sealed, _, err := sealWorkspace(ctx, s.keyProvider(), tenantID, userID, workspaceID, sidecarAAD(tenantID, userID, workspaceID, name), encoded)
	if err != nil {
		return "", err
	}
	if avail, err := archiveutil.AvailableBytes(dir); err == nil && avail < int64(len(sealed))+4096 {
		return "", ErrDiskFull
	}
	tmp, err := os.CreateTemp(dir, ".sidecar-stage-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(sealed); err != nil {
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	ok = true
	return tmpPath, nil
}

func sidecarRevision(plaintext []byte) string {
	sum := sha256.Sum256(plaintext)
	return hex.EncodeToString(sum[:])
}

// GetSidecar decrypts a named sidecar. Missing files are ErrBlobNotFound.
func (s *BlobStore) GetSidecar(ctx context.Context, tenantID, userID, workspaceID, name string) ([]byte, error) {
	if s == nil {
		return nil, ErrUnavailable
	}
	path, err := s.SidecarPath(tenantID, userID, workspaceID, name)
	if err != nil {
		return nil, err
	}
	// The canonical file is the only copy; its plaintext hash is the revision.
	blob, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// Deployments written by the pre-11.31 CAS design may have their latest
		// committed content only in the immutable revision file named by the DB
		// row (the canonical file was a best-effort compat copy). Fall back to
		// that committed pointer instead of reporting the sidecar as lost.
		blob, err = s.readLegacySidecarRevision(ctx, workspaceID, name, path)
	}
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrBlobNotFound
		}
		return nil, err
	}
	// Sidecars carry a small codec header in addition to the plaintext cap.
	if int64(len(blob)) > s.maxCiphertextBytes()+13 {
		return nil, ErrBlobTooLarge
	}
	plain, _, err := openWorkspace(ctx, s.keyProvider(), tenantID, userID, workspaceID, sidecarAAD(tenantID, userID, workspaceID, name), blob)
	if err != nil {
		return nil, err
	}
	return decodeSidecar(plain)
}

// readLegacySidecarRevision resolves the committed DB pointer to a pre-11.31
// immutable revision file. Revision is a validated content hash and name is
// allowlisted, so the derived path cannot escape the sidecar directory.
func (s *BlobStore) readLegacySidecarRevision(ctx context.Context, workspaceID, name, canonicalPath string) ([]byte, error) {
	if s.DB == nil {
		return nil, os.ErrNotExist
	}
	var revision string
	err := s.DB.QueryRowContext(ctx, `SELECT revision FROM cloud_workspace_sidecars WHERE workspace_id = ? AND name = ?`, workspaceID, name).Scan(&revision)
	if err != nil || !ValidSHA256Hex(strings.TrimSpace(revision)) {
		return nil, os.ErrNotExist
	}
	legacy := filepath.Join(filepath.Dir(canonicalPath), name+"."+strings.TrimSpace(revision)+objectFileExt)
	blob, err := os.ReadFile(legacy)
	if err != nil {
		return nil, err
	}
	return blob, nil
}

func (s *BlobStore) GetSidecarWithRevision(ctx context.Context, tenantID, userID, workspaceID, name string) (*Sidecar, error) {
	plain, err := s.GetSidecar(ctx, tenantID, userID, workspaceID, name)
	if err != nil {
		return nil, err
	}
	return &Sidecar{Data: plain, Revision: sidecarRevision(plain)}, nil
}

// PutSidecar admits a named sidecar. Grant is enforced at HTTP; owner+lease here.
func (s *Service) PutSidecar(ctx context.Context, principal auth.MachinePrincipal, workspaceID, name string, plaintext []byte) error {
	_, err := s.PutSidecarWithRevision(ctx, principal, workspaceID, name, "", plaintext)
	return err
}

// PutSidecarWithRevision performs an exclusive-lease protected CAS update.
// An empty ifMatch is accepted only when the sidecar does not exist yet.
func (s *Service) PutSidecarWithRevision(ctx context.Context, principal auth.MachinePrincipal, workspaceID, name, ifMatch string, plaintext []byte) (*Sidecar, error) {
	blobs, err := s.blobs()
	if err != nil {
		return nil, err
	}
	name, err = ValidateSidecarName(name)
	if err != nil {
		return nil, err
	}
	if int64(len(plaintext)) > MaxSidecarBytes {
		return nil, ErrBlobTooLarge
	}
	// v1-sequential has one writer for every sidecar, including session.json.
	// The old session-only multi-writer exception could resurrect stale history
	// and bypass the lease/fencing boundary during handoff.
	if _, err := s.Workspaces.RequireLeaseWithSessionAndToken(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, principal.ClientInstanceID, principal.FencingToken, s.now()); err != nil {
		return nil, err
	}
	if err := s.admitBandwidth(ctx, principal, int64(len(plaintext)), 0); err != nil {
		return nil, err
	}
	dir, err := blobs.SidecarsDir(principal.TenantID, principal.UserID, workspaceID)
	if err != nil {
		return nil, err
	}
	if err := s.checkVolume(dir, int64(len(plaintext))); err != nil {
		return nil, err
	}
	// task.json is also the transport for the unique server-side binding. Parse
	// and validate it now, but defer the binding mutation until the durable
	// sidecar transaction below. This prevents a stale/failed sidecar write from
	// advancing the task binding on its own.
	var taskBinding *TaskSidecar
	if name == SidecarTask {
		task := ParseTaskSidecar(plaintext)
		if task.WorkspaceID != "" && task.WorkspaceID != strings.TrimSpace(workspaceID) {
			return nil, ErrRevisionConflict
		}
		taskBinding = &task
	}
	newRevision := sidecarRevision(plaintext)
	path, err := blobs.SidecarPath(principal.TenantID, principal.UserID, workspaceID, name)
	if err != nil {
		return nil, err
	}
	// The lease already guarantees a single writer; the mutex only serializes
	// in-process CAS check + atomic write against local readers/writers.
	sidecarWriteMu.Lock()
	defer sidecarWriteMu.Unlock()
	// The canonical file content is the CAS baseline: its plaintext hash is the
	// current revision, and a missing file means the sidecar does not exist.
	current, currentErr := blobs.GetSidecar(ctx, principal.TenantID, principal.UserID, workspaceID, name)
	if currentErr != nil && !errors.Is(currentErr, ErrBlobNotFound) {
		return nil, currentErr
	}
	currentRev := ""
	if currentErr == nil {
		currentRev = sidecarRevision(current)
	}
	if strings.TrimSpace(ifMatch) != currentRev {
		return nil, ErrRevisionConflict
	}
	// Stage the sealed payload beside the canonical file, commit the DB
	// transaction, then atomically rename into place. A fenced or failed commit
	// discards only the temp file; the previously committed canonical content is
	// never deleted out from under readers. A crash between commit and rename
	// leaves the DB revision ahead of the file, which the next write's CAS (the
	// file hash is the baseline) heals on retry.
	staged, err := blobs.stageSidecarFile(ctx, principal.TenantID, principal.UserID, workspaceID, name, path, plaintext)
	if err != nil {
		return nil, err
	}
	out := &Sidecar{Data: append([]byte(nil), plaintext...), Revision: newRevision}
	now := s.now().UTC()
	err = s.Workspaces.withImmediate(ctx, func(q queryer) error {
		if _, err := requireActiveOwned(ctx, q, principal.TenantID, principal.UserID, workspaceID); err != nil {
			return err
		}
		if err := assertLeaseHeldForSession(ctx, q, workspaceID, principal.MachineID, principal.ClientInstanceID, principal.FencingToken, now); err != nil {
			return err
		}
		if taskBinding != nil {
			if _, err := upsertTaskBindingTx(ctx, q, principal.TenantID, principal.UserID, workspaceID, taskBinding.CloudTaskID, "", taskBinding.Name, taskBinding.Mode, taskBinding.Tag, taskBinding.BindingVersion, now.Format(time.RFC3339)); err != nil {
				return err
			}
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO cloud_workspace_sidecars (workspace_id, name, revision, updated_by_session, updated_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT(workspace_id, name) DO UPDATE SET revision = excluded.revision, updated_by_session = excluded.updated_by_session, updated_at = excluded.updated_at`, workspaceID, name, newRevision, principal.ClientInstanceID, now.Format(time.RFC3339)); err != nil {
			return err
		}
		return stageAtomicIdempotency(ctx, q, out)
	})
	if err != nil {
		_ = os.Remove(staged)
		return nil, err
	}
	if err := fileutil.RenameAtomicFile(staged, path); err != nil {
		// The revision is committed; the caller can retry the same If-Match CAS
		// against the previous canonical content. Keep the staged file out of the
		// directory so the legacy-fallback and orphan scans never see it.
		_ = os.Remove(staged)
		return nil, err
	}
	return out, nil
}

// GetSidecar returns sidecar plaintext for the owner. Reads do not take the
// exclusive write lease so a new machine can restore task.json into the list.
func (s *Service) GetSidecar(ctx context.Context, principal auth.MachinePrincipal, workspaceID, name string) ([]byte, error) {
	out, err := s.GetSidecarWithRevision(ctx, principal, workspaceID, name)
	if err != nil {
		return nil, err
	}
	return out.Data, nil
}

func (s *Service) GetSidecarWithRevision(ctx context.Context, principal auth.MachinePrincipal, workspaceID, name string) (*Sidecar, error) {
	blobs, err := s.blobs()
	if err != nil {
		return nil, err
	}
	name, err = ValidateSidecarName(name)
	if err != nil {
		return nil, err
	}
	if s.Workspaces == nil {
		return nil, ErrUnavailable
	}
	ws, err := s.Workspaces.GetOwned(ctx, principal.TenantID, principal.UserID, workspaceID)
	if err != nil {
		return nil, err
	}
	if ws == nil || ws.Status != StatusActive {
		return nil, ErrNotFound
	}
	out, err := blobs.GetSidecarWithRevision(ctx, principal.TenantID, principal.UserID, workspaceID, name)
	if err != nil {
		return nil, err
	}
	// Sidecar rows carry no durable size column, so downloads are charged after
	// the read against the actual plaintext length. An over-limit window still
	// fails closed: the response is withheld and the next window must admit it.
	if err := s.admitBandwidth(ctx, principal, 0, int64(len(out.Data))); err != nil {
		return nil, err
	}
	return out, nil
}
