package cloudworkspace

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func objectReservation(ctx context.Context, q queryer, workspaceID, sha256hex string) (state string, size int64, found bool, err error) {
	err = q.QueryRowContext(ctx, `
		SELECT LOWER(COALESCE(NULLIF(TRIM(object_state), ''), 'ready')),
			CASE
				WHEN COALESCE(plain_size_bytes, 0) > 0 THEN plain_size_bytes
				WHEN COALESCE(size_bytes, 0) > 0 THEN size_bytes
				ELSE 0 END
		FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ?`,
		workspaceID, sha256hex).Scan(&state, &size)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, err
	}
	return state, size, true, nil
}

// PrepareObjectPut checks lease + quota and reserves a ref_count=0 object row.
// existed is true when the digest is already recorded (idempotent PUT).
func (s *Store) PrepareObjectPut(ctx context.Context, tenantID, userID, workspaceID, machineID, sha256hex string, requestSize, maxWorkspaceBytes, tenantMaxTotalBytes int64, now time.Time) (existed bool, err error) {
	return s.PrepareObjectPutWithSession(ctx, tenantID, userID, workspaceID, machineID, "", 0, sha256hex, requestSize, maxWorkspaceBytes, tenantMaxTotalBytes, now)
}

func (s *Store) PrepareObjectPutWithSession(ctx context.Context, tenantID, userID, workspaceID, machineID, clientInstanceID string, fencingToken int64, sha256hex string, requestSize, maxWorkspaceBytes, tenantMaxTotalBytes int64, now time.Time) (existed bool, err error) {
	if s == nil || s.db == nil {
		return false, ErrUnavailable
	}
	if !ValidSHA256Hex(sha256hex) {
		return false, ErrInvalidBlobKey
	}
	if requestSize < 0 || requestSize > MaxObjectBytes {
		return false, ErrBlobTooLarge
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	workspaceID = strings.TrimSpace(workspaceID)
	machineID = strings.TrimSpace(machineID)
	ts := now.UTC().Format(time.RFC3339)
	err = s.withImmediate(ctx, func(q queryer) error {
		if _, err := requireActiveOwned(ctx, q, tenantID, userID, workspaceID); err != nil {
			return err
		}
		// The v1 service always supplies machine/session identity. Keep the empty
		// machine compatibility branch for offline migration tooling only.
		if machineID != "" {
			if err := assertLeaseHeldForSession(ctx, q, workspaceID, machineID, clientInstanceID, fencingToken, now); err != nil {
				return err
			}
		}
		state, reserved, found, err := objectReservation(ctx, q, workspaceID, sha256hex)
		if err != nil {
			return err
		}
		if found && state == "ready" {
			existed = true
			return nil
		}
		if found && state == "deleting" {
			// GC has already unlinked (or is about to unlink) this object. Do not
			// resurrect the row as staging: a writer could otherwise race the
			// physical unlink and end up with ready metadata pointing at no file.
			return ErrObjectDeleting
		}
		delta := requestSize
		if found {
			delta -= reserved
		}
		if err := admitRetainedDelta(ctx, q, tenantID, workspaceID, delta, maxWorkspaceBytes, tenantMaxTotalBytes); err != nil {
			return err
		}
		_, err = q.ExecContext(ctx, `
			INSERT INTO cloud_workspace_objects (
				workspace_id, sha256, size_bytes, plain_size_bytes, stored_size_bytes,
				compression, compression_level, encryption_version, ref_count, created_at, object_state
			) VALUES (?, ?, ?, ?, 0, 'none', 0, 'aes-gcm-v1', 0, ?, 'staging')
			ON CONFLICT(workspace_id, sha256) DO UPDATE SET
				size_bytes = excluded.size_bytes,
				plain_size_bytes = excluded.plain_size_bytes,
				stored_size_bytes = 0,
				compression = 'none',
				compression_level = 0,
				encryption_version = 'aes-gcm-v1',
				created_at = excluded.created_at,
				object_state = 'staging'
			WHERE LOWER(COALESCE(NULLIF(TRIM(cloud_workspace_objects.object_state), ''), 'ready')) != 'ready'`,
			workspaceID, sha256hex, requestSize, requestSize, ts)
		return err
	})
	return existed, err
}

// RequireLease confirms this machine holds an unexpired exclusive lease.
func (s *Store) RequireLease(ctx context.Context, tenantID, userID, workspaceID, machineID string, now time.Time) (*Workspace, error) {
	return s.RequireLeaseWithSession(ctx, tenantID, userID, workspaceID, machineID, "", now)
}

func (s *Store) RequireLeaseWithSession(ctx context.Context, tenantID, userID, workspaceID, machineID, clientInstanceID string, now time.Time) (*Workspace, error) {
	return s.RequireLeaseWithSessionAndToken(ctx, tenantID, userID, workspaceID, machineID, clientInstanceID, 0, now)
}

// RequireLeaseWithSessionAndToken is the write-path lease guard. The token is
// optional for legacy in-process callers, but when supplied it fences a stale
// writer even if it presents the same machine/session identity.
func (s *Store) RequireLeaseWithSessionAndToken(ctx context.Context, tenantID, userID, workspaceID, machineID, clientInstanceID string, fencingToken int64, now time.Time) (*Workspace, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	workspaceID = strings.TrimSpace(workspaceID)
	machineID = strings.TrimSpace(machineID)
	ws, err := requireActiveOwned(ctx, s.db, tenantID, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	if err := assertLeaseHeldForSession(ctx, s.db, workspaceID, machineID, clientInstanceID, fencingToken, now); err != nil {
		return nil, err
	}
	return ws, nil
}

// PurgeStaleObjectReservations removes durable staging reservations left by a
// crashed or abandoned chunk upload. It is intentionally separate from
// ready-object GC so a partially uploaded object can never become manifest
// visible merely because a reservation expired.
func (s *Store) PurgeStaleObjectReservations(ctx context.Context, cutoff time.Time) (int64, error) {
	if s == nil || s.db == nil {
		return 0, ErrUnavailable
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM cloud_workspace_objects WHERE COALESCE(object_state, 'ready') = 'staging' AND created_at < ?`, cutoff.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
