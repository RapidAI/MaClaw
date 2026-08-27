package cloudworkspace

import (
	"context"
	"os"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/corelib/archiveutil"
	"github.com/RapidAI/CodeClaw/corelib/fileutil"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

const (
	sidecarDirName = "sidecars"

	SidecarSession    = "session.json"
	SidecarTask       = "task.json"
	SidecarWorkbench  = "coding_workbench.json"
	SidecarCheckpoint = "coding_exec_checkpoint.json"
)

// MaxSidecarBytes is the plaintext cap for one named sidecar (not the file tree).
const MaxSidecarBytes int64 = MaxObjectBytes

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

// PutSidecar seals a named blob (not content-addressed) with the workspace DEK.
func (s *BlobStore) PutSidecar(_ context.Context, tenantID, userID, workspaceID, name string, plaintext []byte) error {
	if int64(len(plaintext)) > MaxSidecarBytes {
		return ErrBlobTooLarge
	}
	path, err := s.SidecarPath(tenantID, userID, workspaceID, name)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	master, err := loadMasterKey(s.keyDir())
	if err != nil {
		return err
	}
	dek := deriveDEK(master, tenantID, userID, workspaceID)
	sealed, err := seal(dek, sidecarAAD(tenantID, userID, workspaceID, name), plaintext)
	if err != nil {
		return err
	}
	if avail, err := archiveutil.AvailableBytes(dir); err == nil && avail < int64(len(sealed))+4096 {
		return ErrDiskFull
	}
	return fileutil.AtomicWriteFile(path, sealed, 0o600)
}

// GetSidecar decrypts a named sidecar. Missing files are ErrBlobNotFound.
func (s *BlobStore) GetSidecar(_ context.Context, tenantID, userID, workspaceID, name string) ([]byte, error) {
	path, err := s.SidecarPath(tenantID, userID, workspaceID, name)
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
	if int64(len(blob)) > s.maxCiphertextBytes() {
		return nil, ErrBlobTooLarge
	}
	master, err := loadMasterKey(s.keyDir())
	if err != nil {
		return nil, err
	}
	dek := deriveDEK(master, tenantID, userID, workspaceID)
	plain, err := open(dek, sidecarAAD(tenantID, userID, workspaceID, name), blob)
	if err != nil {
		return nil, err
	}
	return plain, nil
}

// PutSidecar admits a named sidecar. Grant is enforced at HTTP; owner+lease here.
func (s *Service) PutSidecar(ctx context.Context, principal auth.MachinePrincipal, workspaceID, name string, plaintext []byte) error {
	blobs, err := s.blobs()
	if err != nil {
		return err
	}
	name, err = ValidateSidecarName(name)
	if err != nil {
		return err
	}
	if int64(len(plaintext)) > MaxSidecarBytes {
		return ErrBlobTooLarge
	}
	if _, err := s.Workspaces.RequireLease(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, s.now()); err != nil {
		return err
	}
	dir, err := blobs.SidecarsDir(principal.TenantID, principal.UserID, workspaceID)
	if err != nil {
		return err
	}
	if err := s.checkVolume(dir, int64(len(plaintext))); err != nil {
		return err
	}
	return blobs.PutSidecar(ctx, principal.TenantID, principal.UserID, workspaceID, name, plaintext)
}

// GetSidecar returns sidecar plaintext. Owner+lease required.
func (s *Service) GetSidecar(ctx context.Context, principal auth.MachinePrincipal, workspaceID, name string) ([]byte, error) {
	blobs, err := s.blobs()
	if err != nil {
		return nil, err
	}
	name, err = ValidateSidecarName(name)
	if err != nil {
		return nil, err
	}
	if _, err := s.Workspaces.RequireLease(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, s.now()); err != nil {
		return nil, err
	}
	return blobs.GetSidecar(ctx, principal.TenantID, principal.UserID, workspaceID, name)
}
