package cloudworkspace

import (
	"context"
	"os"

	"github.com/RapidAI/CodeClaw/corelib/archiveutil"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

func (s *Service) blobs() (*BlobStore, error) {
	if s == nil || s.Workspaces == nil || s.Blobs == nil {
		return nil, ErrUnavailable
	}
	return s.Blobs, nil
}

func (s *Service) checkVolume(objectsDir string, requestSize int64) error {
	if err := os.MkdirAll(objectsDir, 0o700); err != nil {
		return err
	}
	avail, err := archiveutil.AvailableBytes(objectsDir)
	if err != nil {
		return err
	}
	need := requestSize
	if need < VolumeReserveBytes {
		need = VolumeReserveBytes
	}
	if avail < need {
		return ErrVolumeFull
	}
	return nil
}

func (s *Service) syncLimits(ctx context.Context, principal auth.MachinePrincipal) (maxWS, tenantMax int64) {
	settings := s.LoadTenantSettings(ctx, principal.TenantID)
	return settings.MaxWorkspaceBytes, settings.TenantMaxTotalBytes
}

// GetManifest returns the current tree. Requires grant (caller), owner, and lease.
func (s *Service) GetManifest(ctx context.Context, principal auth.MachinePrincipal, workspaceID string) (*Manifest, error) {
	if s == nil || s.Workspaces == nil {
		return nil, ErrUnavailable
	}
	return s.Workspaces.GetManifest(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, s.now())
}

// PutManifest fully replaces the tree in one transaction.
func (s *Service) PutManifest(ctx context.Context, principal auth.MachinePrincipal, workspaceID, ifMatch string, entries []ManifestEntry) (*Manifest, error) {
	if s == nil || s.Workspaces == nil {
		return nil, ErrUnavailable
	}
	normalized, err := normalizeEntries(entries)
	if err != nil {
		return nil, err
	}
	blobs, err := s.blobs()
	if err != nil {
		return nil, err
	}
	for _, e := range normalized {
		has, err := blobs.Has(ctx, principal.TenantID, principal.UserID, workspaceID, e.SHA256)
		if err != nil {
			return nil, err
		}
		if !has {
			return nil, ErrObjectMissing
		}
	}
	return s.Workspaces.ReplaceManifest(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, ifMatch, normalized, s.now())
}

// GetObject decrypts and returns plaintext. Requires a valid lease.
func (s *Service) GetObject(ctx context.Context, principal auth.MachinePrincipal, workspaceID, sha256hex string) ([]byte, error) {
	blobs, err := s.blobs()
	if err != nil {
		return nil, err
	}
	if !ValidSHA256Hex(sha256hex) {
		return nil, ErrInvalidBlobKey
	}
	if _, err := s.Workspaces.RequireLease(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, s.now()); err != nil {
		return nil, err
	}
	return blobs.Get(ctx, principal.TenantID, principal.UserID, workspaceID, sha256hex)
}

// PutObject admits, hashes, and seals a whole-object plaintext body.
func (s *Service) PutObject(ctx context.Context, principal auth.MachinePrincipal, workspaceID, sha256hex string, plaintext []byte) (PutResult, error) {
	blobs, err := s.blobs()
	if err != nil {
		return PutResult{}, err
	}
	if !ValidSHA256Hex(sha256hex) {
		return PutResult{}, ErrInvalidBlobKey
	}
	if int64(len(plaintext)) > MaxObjectBytes {
		return PutResult{}, ErrBlobTooLarge
	}
	if plaintextSHA256(plaintext) != sha256hex {
		return PutResult{}, ErrBlobHashMismatch
	}
	has, err := blobs.Has(ctx, principal.TenantID, principal.UserID, workspaceID, sha256hex)
	if err != nil {
		return PutResult{}, err
	}
	if has {
		if _, err := s.Workspaces.RequireLease(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, s.now()); err != nil {
			return PutResult{}, err
		}
		if err := blobs.recordObject(ctx, workspaceID, sha256hex, int64(len(plaintext))); err != nil {
			return PutResult{}, err
		}
		return PutResult{SHA256: sha256hex, SizeBytes: int64(len(plaintext)), Existed: true}, nil
	}
	maxWS, tenantMax := s.syncLimits(ctx, principal)
	objectsDir, err := blobs.ObjectsDir(principal.TenantID, principal.UserID, workspaceID)
	if err != nil {
		return PutResult{}, err
	}
	if err := s.checkVolume(objectsDir, int64(len(plaintext))); err != nil {
		return PutResult{}, err
	}
	if _, err := s.Workspaces.PrepareObjectPut(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, sha256hex, int64(len(plaintext)), maxWS, tenantMax, s.now()); err != nil {
		return PutResult{}, err
	}
	return blobs.PutExpected(ctx, principal.TenantID, principal.UserID, workspaceID, sha256hex, plaintext)
}

// PutObjectChunk stages one plaintext slice. used_bytes is not updated.
func (s *Service) PutObjectChunk(ctx context.Context, principal auth.MachinePrincipal, workspaceID, sha256hex string, index int, data []byte) error {
	blobs, err := s.blobs()
	if err != nil {
		return err
	}
	if !ValidSHA256Hex(sha256hex) {
		return ErrInvalidBlobKey
	}
	if _, err := s.Workspaces.RequireLease(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, s.now()); err != nil {
		return err
	}
	objectsDir, err := blobs.ObjectsDir(principal.TenantID, principal.UserID, workspaceID)
	if err != nil {
		return err
	}
	if err := s.checkVolume(objectsDir, int64(len(data))); err != nil {
		return err
	}
	return blobs.PutChunk(ctx, principal.TenantID, principal.UserID, workspaceID, sha256hex, index, data)
}

// CompleteObject concatenates staged chunks, verifies the digest, and seals once.
func (s *Service) CompleteObject(ctx context.Context, principal auth.MachinePrincipal, workspaceID, sha256hex string) (PutResult, error) {
	blobs, err := s.blobs()
	if err != nil {
		return PutResult{}, err
	}
	if !ValidSHA256Hex(sha256hex) {
		return PutResult{}, ErrInvalidBlobKey
	}
	if _, err := s.Workspaces.RequireLease(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, s.now()); err != nil {
		return PutResult{}, err
	}
	has, err := blobs.Has(ctx, principal.TenantID, principal.UserID, workspaceID, sha256hex)
	if err != nil {
		return PutResult{}, err
	}
	if has {
		_ = blobs.RemovePart(principal.TenantID, principal.UserID, workspaceID, sha256hex)
		return PutResult{SHA256: sha256hex, Existed: true}, nil
	}
	plain, err := blobs.AssembleChunks(principal.TenantID, principal.UserID, workspaceID, sha256hex)
	if err != nil {
		return PutResult{}, err
	}
	maxWS, tenantMax := s.syncLimits(ctx, principal)
	objectsDir, err := blobs.ObjectsDir(principal.TenantID, principal.UserID, workspaceID)
	if err != nil {
		return PutResult{}, err
	}
	if err := s.checkVolume(objectsDir, int64(len(plain))); err != nil {
		return PutResult{}, err
	}
	if _, err := s.Workspaces.PrepareObjectPut(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, sha256hex, int64(len(plain)), maxWS, tenantMax, s.now()); err != nil {
		return PutResult{}, err
	}
	got, err := blobs.PutExpected(ctx, principal.TenantID, principal.UserID, workspaceID, sha256hex, plain)
	if err != nil {
		return PutResult{}, err
	}
	_ = blobs.RemovePart(principal.TenantID, principal.UserID, workspaceID, sha256hex)
	return got, nil
}
