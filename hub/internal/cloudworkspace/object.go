package cloudworkspace

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func sumUnreferenced(ctx context.Context, q queryer, workspaceID string) (int64, error) {
	var n sql.NullInt64
	err := q.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(size_bytes), 0) FROM cloud_workspace_objects
		WHERE workspace_id = ? AND ref_count = 0`, workspaceID).Scan(&n)
	if err != nil {
		return 0, err
	}
	if n.Valid {
		return n.Int64, nil
	}
	return 0, nil
}

func sumTenantUnreferenced(ctx context.Context, q queryer, tenantID string) (int64, error) {
	var n sql.NullInt64
	err := q.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(o.size_bytes), 0)
		FROM cloud_workspace_objects o
		JOIN cloud_workspaces w ON w.id = o.workspace_id
		WHERE w.tenant_id = ? AND o.ref_count = 0`, tenantID).Scan(&n)
	if err != nil {
		return 0, err
	}
	if n.Valid {
		return n.Int64, nil
	}
	return 0, nil
}

func objectRowExists(ctx context.Context, q queryer, workspaceID, sha256hex string) (bool, error) {
	var one int
	err := q.QueryRowContext(ctx, `
		SELECT 1 FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ?`,
		workspaceID, sha256hex).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func admitObjectBytes(ctx context.Context, q queryer, tenantID, workspaceID string, requestSize, maxWorkspaceBytes, tenantMaxTotalBytes int64) error {
	if requestSize < 0 {
		return ErrBlobTooLarge
	}
	ws, err := scanWorkspace(q.QueryRowContext(ctx,
		`SELECT `+workspaceCols+` FROM cloud_workspaces WHERE id = ? AND tenant_id = ?`,
		workspaceID, tenantID,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	unref, err := sumUnreferenced(ctx, q, workspaceID)
	if err != nil {
		return err
	}
	if maxWorkspaceBytes > 0 && ws.UsedBytes+unref+requestSize > maxWorkspaceBytes {
		return ErrWorkspaceSize
	}
	tenantUsed, err := tenantUsedBytes(ctx, q, tenantID)
	if err != nil {
		return err
	}
	tenantUnref, err := sumTenantUnreferenced(ctx, q, tenantID)
	if err != nil {
		return err
	}
	if tenantMaxTotalBytes > 0 && tenantUsed+tenantUnref+requestSize > tenantMaxTotalBytes {
		return ErrTenantDisk
	}
	return nil
}

// PrepareObjectPut checks lease + quota and reserves a ref_count=0 object row.
// existed is true when the digest is already recorded (idempotent PUT).
func (s *Store) PrepareObjectPut(ctx context.Context, tenantID, userID, workspaceID, machineID, sha256hex string, requestSize, maxWorkspaceBytes, tenantMaxTotalBytes int64, now time.Time) (existed bool, err error) {
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
		// An empty machine id is used by the v2 multi-writer object path. Object
		// admission is safe without an exclusive lease because objects are
		// immutable, content addressed, and the operation commit is serialized
		// separately in ApplyOperation. Legacy callers still pass machineID and
		// retain the lease check.
		if machineID != "" {
			if err := assertLeaseHeld(ctx, q, workspaceID, machineID, now); err != nil {
				return err
			}
		}
		ok, err := objectRowExists(ctx, q, workspaceID, sha256hex)
		if err != nil {
			return err
		}
		if ok {
			existed = true
			return nil
		}
		if err := admitObjectBytes(ctx, q, tenantID, workspaceID, requestSize, maxWorkspaceBytes, tenantMaxTotalBytes); err != nil {
			return err
		}
		_, err = q.ExecContext(ctx, `
			INSERT OR IGNORE INTO cloud_workspace_objects (workspace_id, sha256, size_bytes, ref_count, created_at)
			VALUES (?, ?, ?, 0, ?)`, workspaceID, sha256hex, requestSize, ts)
		return err
	})
	return existed, err
}

// RequireLease confirms this machine holds an unexpired exclusive lease.
func (s *Store) RequireLease(ctx context.Context, tenantID, userID, workspaceID, machineID string, now time.Time) (*Workspace, error) {
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
	if err := assertLeaseHeld(ctx, s.db, workspaceID, machineID, now); err != nil {
		return nil, err
	}
	return ws, nil
}
