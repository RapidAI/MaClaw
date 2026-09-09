package cloudworkspace

import (
	"context"
	"errors"
	"os"

	"github.com/RapidAI/CodeClaw/corelib/archiveutil"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

func (s *Service) requireActiveWorkspace(ctx context.Context, principal auth.MachinePrincipal, workspaceID string) error {
	ws, err := s.Workspaces.GetOwned(ctx, principal.TenantID, principal.UserID, workspaceID)
	if err != nil {
		return err
	}
	if ws == nil || ws.Status != StatusActive {
		return ErrNotFound
	}
	return nil
}

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

// GetManifest returns the current tree. Reads require grant and ownership, but
// intentionally not the writer lease so read-only devices can coexist.
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
	return s.Workspaces.ReplaceManifestWithSession(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, principal.ClientInstanceID, principal.FencingToken, ifMatch, normalized, s.now())
}

// RestoreSnapshot promotes a retained manifest snapshot to a new revision.
func (s *Service) RestoreSnapshot(ctx context.Context, principal auth.MachinePrincipal, workspaceID, snapshotID, ifMatch string) (*Manifest, error) {
	if s == nil || s.Workspaces == nil {
		return nil, ErrUnavailable
	}
	return s.Workspaces.RestoreSnapshotWithSession(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, principal.ClientInstanceID, principal.FencingToken, snapshotID, ifMatch, s.now())
}

func (s *Service) ApplyManifestDelta(ctx context.Context, principal auth.MachinePrincipal, workspaceID string, delta ManifestDelta) (*Manifest, error) {
	if s == nil || s.Workspaces == nil {
		return nil, ErrUnavailable
	}
	return s.Workspaces.ApplyManifestDeltaWithSession(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, principal.ClientInstanceID, principal.FencingToken, delta, s.now())
}

// GetObject decrypts and returns plaintext. Reads are lease-free to allow
// concurrent multi-machine mounts; ownership and active status are still
// enforced by the store.
func (s *Service) GetObject(ctx context.Context, principal auth.MachinePrincipal, workspaceID, sha256hex string) ([]byte, error) {
	blobs, err := s.blobs()
	if err != nil {
		return nil, err
	}
	if !ValidSHA256Hex(sha256hex) {
		return nil, ErrInvalidBlobKey
	}
	if err := s.requireActiveWorkspace(ctx, principal, workspaceID); err != nil {
		return nil, err
	}
	// Pre-admit the download against the hourly window using the committed
	// plaintext size so an over-quota read never reads or decrypts the blob.
	// With no quota configured (the default), skip the extra metadata lookup
	// entirely and let Get serve its own not-found error.
	if blobs.DB != nil && s.bandwidthLimited(ctx, principal.TenantID) {
		size, err := blobs.ObjectPlainSize(ctx, workspaceID, sha256hex)
		if err != nil {
			return nil, err
		}
		if err := s.admitBandwidth(ctx, principal, 0, size); err != nil {
			return nil, err
		}
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
	// All object finalization is writer-only in v1-sequential. Check the lease
	// before the idempotent fast path as well, otherwise a stale/deleted writer
	// could still mutate object metadata.
	if _, err := s.Workspaces.RequireLeaseWithSessionAndToken(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, principal.ClientInstanceID, principal.FencingToken, s.now()); err != nil {
		return PutResult{}, err
	}
	// Charge the hourly bandwidth window before touching disk. The bytes have
	// already crossed the wire; failed uploads are not refunded so retry loops
	// cannot amplify transfer beyond the tenant-configured quota.
	if err := s.admitBandwidth(ctx, principal, int64(len(plaintext)), 0); err != nil {
		return PutResult{}, err
	}
	// A whole-object PUT supersedes any partial chunk upload for the same hash;
	// release its aggregate reservation before admitting the final object.
	if err := blobs.RemovePart(principal.TenantID, principal.UserID, workspaceID, sha256hex); err != nil {
		return PutResult{}, err
	}
	has, err := blobs.Has(ctx, principal.TenantID, principal.UserID, workspaceID, sha256hex)
	if err != nil {
		return PutResult{}, err
	}
	if has {
		result := PutResult{SHA256: sha256hex, SizeBytes: int64(len(plaintext)), Existed: true}
		guard := &objectFinalizeGuard{TenantID: principal.TenantID, UserID: principal.UserID, MachineID: principal.MachineID, ClientInstanceID: principal.ClientInstanceID, FencingToken: principal.FencingToken, Now: s.now()}
		if err := blobs.finalizeReadyObject(ctx, workspaceID, sha256hex, int64(len(plaintext)), result, guard); err != nil {
			return PutResult{}, err
		}
		return result, nil
	}
	maxWS, tenantMax := s.syncLimits(ctx, principal)
	objectsDir, err := blobs.ObjectsDir(principal.TenantID, principal.UserID, workspaceID)
	if err != nil {
		return PutResult{}, err
	}
	if err := s.checkVolume(objectsDir, int64(len(plaintext))); err != nil {
		return PutResult{}, err
	}
	if _, err := s.Workspaces.PrepareObjectPutWithSession(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, principal.ClientInstanceID, principal.FencingToken, sha256hex, int64(len(plaintext)), maxWS, tenantMax, s.now()); err != nil {
		return PutResult{}, err
	}
	guard := &objectFinalizeGuard{TenantID: principal.TenantID, UserID: principal.UserID, MachineID: principal.MachineID, ClientInstanceID: principal.ClientInstanceID, FencingToken: principal.FencingToken, Now: s.now()}
	return blobs.PutExpectedWithGuard(ctx, principal.TenantID, principal.UserID, workspaceID, sha256hex, plaintext, guard)
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
	if _, err := s.Workspaces.RequireLeaseWithSessionAndToken(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, principal.ClientInstanceID, principal.FencingToken, s.now()); err != nil {
		return err
	}
	// Chunks are charged per slice because each slice crosses the wire; the
	// ready-object idempotent no-op below still consumed upload bandwidth.
	if err := s.admitBandwidth(ctx, principal, int64(len(data)), 0); err != nil {
		return err
	}
	// A ready content-addressed object makes every chunk for the same digest an
	// idempotent no-op. Reclaim any abandoned part directory so ready bytes and
	// duplicate staging bytes cannot remain charged at the same time.
	if has, err := blobs.Has(ctx, principal.TenantID, principal.UserID, workspaceID, sha256hex); err != nil {
		return err
	} else if has {
		if err := blobs.RemovePart(principal.TenantID, principal.UserID, workspaceID, sha256hex); err != nil {
			return err
		}
		return s.Workspaces.FinalizeStagingChunkWithSession(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, principal.ClientInstanceID, principal.FencingToken, sha256hex, index, int64(len(data)), s.now())
	}
	objectsDir, err := blobs.ObjectsDir(principal.TenantID, principal.UserID, workspaceID)
	if err != nil {
		return err
	}
	if err := s.checkVolume(objectsDir, int64(len(data))); err != nil {
		return err
	}
	maxWS, tenantMax := s.syncLimits(ctx, principal)
	if err := s.Workspaces.ReserveStagingChunk(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, principal.ClientInstanceID, principal.FencingToken, sha256hex, index, int64(len(data)), maxWS, tenantMax, s.now()); err != nil {
		return err
	}
	if err := blobs.PutChunk(ctx, principal.TenantID, principal.UserID, workspaceID, sha256hex, index, data); err != nil {
		_ = s.Workspaces.ReleaseStagingChunk(ctx, workspaceID, sha256hex, index)
		return err
	}
	return s.Workspaces.FinalizeStagingChunkWithSession(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, principal.ClientInstanceID, principal.FencingToken, sha256hex, index, int64(len(data)), s.now())
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
	if _, err := s.Workspaces.RequireLeaseWithSessionAndToken(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, principal.ClientInstanceID, principal.FencingToken, s.now()); err != nil {
		return PutResult{}, err
	}
	has, err := blobs.Has(ctx, principal.TenantID, principal.UserID, workspaceID, sha256hex)
	if err != nil {
		return PutResult{}, err
	}
	if has {
		if err := blobs.RemovePart(principal.TenantID, principal.UserID, workspaceID, sha256hex); err != nil {
			return PutResult{}, err
		}
		// Complete is also an idempotent read-after-commit operation.  Return the
		// durable plaintext size instead of zero when the object was finalized by
		// an earlier request; clients use this value to reconcile progress and
		// quota accounting after a lost response.
		result := PutResult{SHA256: sha256hex, Existed: true}
		if blobs.DB != nil {
			if err := blobs.DB.QueryRowContext(ctx, `
				SELECT CASE WHEN COALESCE(plain_size_bytes, 0) > 0 THEN plain_size_bytes ELSE size_bytes END
				  FROM cloud_workspace_objects
				 WHERE workspace_id = ? AND sha256 = ? AND COALESCE(object_state, 'ready') = 'ready'`,
				workspaceID, sha256hex).Scan(&result.SizeBytes); err != nil {
				return PutResult{}, err
			}
		}
		guard := &objectFinalizeGuard{TenantID: principal.TenantID, UserID: principal.UserID, MachineID: principal.MachineID, ClientInstanceID: principal.ClientInstanceID, FencingToken: principal.FencingToken, Now: s.now()}
		if err := blobs.finalizeReadyObject(ctx, workspaceID, sha256hex, 0, result, guard); err != nil {
			return PutResult{}, err
		}
		return result, nil
	}
	plain, err := blobs.AssembleChunks(principal.TenantID, principal.UserID, workspaceID, sha256hex)
	if err != nil {
		if errors.Is(err, ErrBlobHashMismatch) {
			_ = s.Workspaces.ReleaseStagingChunks(ctx, workspaceID, sha256hex)
		}
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
	// Chunk bytes are already reserved in cloud_workspace_staging_chunks. Do
	// not charge the assembled object a second time during finalization.
	if _, err := s.Workspaces.PrepareObjectPutWithSession(ctx, principal.TenantID, principal.UserID, workspaceID, principal.MachineID, principal.ClientInstanceID, principal.FencingToken, sha256hex, 0, maxWS, tenantMax, s.now()); err != nil {
		return PutResult{}, err
	}
	guard := &objectFinalizeGuard{TenantID: principal.TenantID, UserID: principal.UserID, MachineID: principal.MachineID, ClientInstanceID: principal.ClientInstanceID, FencingToken: principal.FencingToken, Now: s.now()}
	got, err := blobs.PutExpectedWithGuard(ctx, principal.TenantID, principal.UserID, workspaceID, sha256hex, plain, guard)
	if err != nil {
		return PutResult{}, err
	}
	if err := blobs.RemovePart(principal.TenantID, principal.UserID, workspaceID, sha256hex); err != nil {
		return PutResult{}, err
	}
	if err := s.Workspaces.ReleaseStagingChunks(ctx, workspaceID, sha256hex); err != nil {
		return PutResult{}, err
	}
	return got, nil
}
