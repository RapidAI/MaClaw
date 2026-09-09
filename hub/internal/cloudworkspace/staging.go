package cloudworkspace

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	// A writer may stage several files, but an abandoned client cannot create an
	// unbounded number of hash directories. The byte budget is additionally
	// checked against workspace and tenant quotas.
	MaxStagingHashes = 256
)

// ReserveStagingChunk records the exact size of one chunk before it is written
// to disk. Replacing a chunk adjusts the aggregate by the size delta, so retry
// uploads are not double charged.
func (s *Store) ReserveStagingChunk(ctx context.Context, tenantID, userID, workspaceID, machineID, clientInstanceID string, fencingToken int64, sha256hex string, index int, size, maxWorkspaceBytes, tenantMaxTotalBytes int64, now time.Time) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	if !ValidSHA256Hex(sha256hex) || index < 0 || index >= maxChunkCount || size <= 0 || size > MaxChunkBytes {
		return ErrInvalidInput
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID, workspaceID = strings.TrimSpace(userID), strings.TrimSpace(workspaceID)
	return s.withImmediate(ctx, func(q queryer) error {
		if _, err := requireActiveOwned(ctx, q, tenantID, userID, workspaceID); err != nil {
			return err
		}
		if err := assertLeaseHeldForSession(ctx, q, workspaceID, machineID, clientInstanceID, fencingToken, now); err != nil {
			return err
		}
		var oldSize int64
		oldErr := q.QueryRowContext(ctx, `SELECT size_bytes FROM cloud_workspace_staging_chunks WHERE workspace_id = ? AND sha256 = ? AND chunk_index = ?`, workspaceID, sha256hex, index).Scan(&oldSize)
		if oldErr != nil && oldErr != sql.ErrNoRows {
			return oldErr
		}
		delta := size - oldSize
		if err := admitRetainedDelta(ctx, q, tenantID, workspaceID, delta, maxWorkspaceBytes, tenantMaxTotalBytes); err != nil {
			return err
		}
		if oldErr == sql.ErrNoRows {
			var hashExists int
			hashErr := q.QueryRowContext(ctx, `SELECT 1 FROM cloud_workspace_staging_chunks WHERE workspace_id = ? AND sha256 = ? LIMIT 1`, workspaceID, sha256hex).Scan(&hashExists)
			if hashErr != nil && hashErr != sql.ErrNoRows {
				return hashErr
			}
			if hashErr == nil {
				_, err := q.ExecContext(ctx, `INSERT INTO cloud_workspace_staging_chunks (workspace_id, sha256, chunk_index, size_bytes, updated_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT(workspace_id, sha256, chunk_index) DO UPDATE SET size_bytes = excluded.size_bytes, updated_at = excluded.updated_at`, workspaceID, sha256hex, index, size, now.UTC().Format(time.RFC3339))
				return err
			}
			var hashes int
			if err := q.QueryRowContext(ctx, `SELECT COUNT(DISTINCT sha256) FROM cloud_workspace_staging_chunks WHERE workspace_id = ?`, workspaceID).Scan(&hashes); err != nil {
				return err
			}
			if hashes >= MaxStagingHashes {
				return ErrWorkspaceSize
			}
		}
		_, err := q.ExecContext(ctx, `INSERT INTO cloud_workspace_staging_chunks (workspace_id, sha256, chunk_index, size_bytes, updated_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT(workspace_id, sha256, chunk_index) DO UPDATE SET size_bytes = excluded.size_bytes, updated_at = excluded.updated_at`, workspaceID, sha256hex, index, size, now.UTC().Format(time.RFC3339))
		return err
	})
}

func (s *Store) ReleaseStagingChunks(ctx context.Context, workspaceID, sha256hex string) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM cloud_workspace_staging_chunks WHERE workspace_id = ? AND sha256 = ?`, strings.TrimSpace(workspaceID), strings.TrimSpace(sha256hex))
	return err
}

// FinalizeStagingChunkWithSession is the durable acceptance point for one
// chunk. The part file is written first; this transaction revalidates the
// writer epoch and commits the idempotency response. A crash before COMMIT may
// leave a provisional part/reservation, but a retry can safely overwrite it.
func (s *Store) FinalizeStagingChunkWithSession(ctx context.Context, tenantID, userID, workspaceID, machineID, clientInstanceID string, fencingToken int64, sha256hex string, index int, size int64, now time.Time) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID, workspaceID, sha256hex = strings.TrimSpace(userID), strings.TrimSpace(workspaceID), strings.TrimSpace(sha256hex)
	return s.withImmediate(ctx, func(q queryer) error {
		if _, err := requireActiveOwned(ctx, q, tenantID, userID, workspaceID); err != nil {
			return err
		}
		if err := assertLeaseHeldForSession(ctx, q, workspaceID, machineID, clientInstanceID, fencingToken, now); err != nil {
			return err
		}
		var ready int
		readyErr := q.QueryRowContext(ctx, `SELECT 1 FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ? AND COALESCE(object_state, 'ready') = 'ready'`, workspaceID, sha256hex).Scan(&ready)
		if readyErr != nil && readyErr != sql.ErrNoRows {
			return readyErr
		}
		if readyErr == nil {
			if _, err := q.ExecContext(ctx, `DELETE FROM cloud_workspace_staging_chunks WHERE workspace_id = ? AND sha256 = ?`, workspaceID, sha256hex); err != nil {
				return err
			}
		} else {
			var reserved int64
			if err := q.QueryRowContext(ctx, `SELECT size_bytes FROM cloud_workspace_staging_chunks WHERE workspace_id = ? AND sha256 = ? AND chunk_index = ?`, workspaceID, sha256hex, index).Scan(&reserved); err != nil {
				if err == sql.ErrNoRows {
					return ErrIncompleteChunks
				}
				return err
			}
			if reserved != size {
				return ErrContentLength
			}
			if _, err := q.ExecContext(ctx, `UPDATE cloud_workspace_staging_chunks SET updated_at = ? WHERE workspace_id = ? AND sha256 = ? AND chunk_index = ?`, now.UTC().Format(time.RFC3339), workspaceID, sha256hex, index); err != nil {
				return err
			}
		}
		return stageAtomicIdempotency(ctx, q, nil)
	})
}

func (s *Store) ReleaseStagingChunk(ctx context.Context, workspaceID, sha256hex string, index int) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM cloud_workspace_staging_chunks WHERE workspace_id = ? AND sha256 = ? AND chunk_index = ?`, strings.TrimSpace(workspaceID), strings.TrimSpace(sha256hex), index)
	return err
}

func (s *Store) PurgeStaleStagingChunks(ctx context.Context, cutoff time.Time) (int64, error) {
	if s == nil || s.db == nil {
		return 0, ErrUnavailable
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM cloud_workspace_staging_chunks WHERE updated_at < ?`, cutoff.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
