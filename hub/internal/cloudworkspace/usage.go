package cloudworkspace

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// RetainedUsage is the live quota view derived from the manifest, immutable
// object rows, snapshot roots, and staging reservations. Retained quota uses
// each workspace-local object's unique plaintext size. This keeps admission
// deterministic before encryption/compression and prevents codec or key
// rotation changes from changing a user's quota bill.
type RetainedUsage struct {
	LogicalBytes              int64 `json:"logical_bytes"`
	RetainedBytes             int64 `json:"retained_bytes"`
	CurrentObjectBytes        int64 `json:"current_object_bytes"`
	SnapshotRetainedBytes     int64 `json:"snapshot_retained_bytes"`
	StagingBytes              int64 `json:"staging_bytes"`
	UnreferencedRetainedBytes int64 `json:"unreferenced_retained_bytes"`
}

// retainedUsageForScope uses one immutable scope CTE so all components are
// calculated against the same SQLite snapshot. scopeSQL is an internal
// constant at every call site; user input is always passed as a query arg.
func retainedUsageForScope(ctx context.Context, q queryer, scopeSQL string, args ...any) (RetainedUsage, error) {
	var usage RetainedUsage
	query := `
		WITH scope(workspace_id) AS (` + scopeSQL + `)
		SELECT
			COALESCE((
				SELECT SUM(e.size_bytes)
				FROM cloud_workspace_manifest_entries e
				JOIN scope s ON s.workspace_id = e.workspace_id
			), 0),
			COALESCE((
				SELECT SUM(CASE
					WHEN COALESCE(o.plain_size_bytes, 0) > 0 THEN o.plain_size_bytes
					WHEN COALESCE(o.size_bytes, 0) > 0 THEN o.size_bytes
					ELSE 0 END)
				FROM cloud_workspace_objects o
				JOIN scope s ON s.workspace_id = o.workspace_id
				WHERE LOWER(COALESCE(NULLIF(TRIM(o.object_state), ''), 'ready')) != 'staging'
			), 0),
			COALESCE((
				SELECT SUM(CASE
					WHEN COALESCE(o.plain_size_bytes, 0) > 0 THEN o.plain_size_bytes
					WHEN COALESCE(o.size_bytes, 0) > 0 THEN o.size_bytes
					ELSE 0 END)
				FROM cloud_workspace_objects o
				JOIN scope s ON s.workspace_id = o.workspace_id
				WHERE LOWER(COALESCE(NULLIF(TRIM(o.object_state), ''), 'ready')) IN ('ready', 'deleting')
				  AND EXISTS (
					SELECT 1 FROM cloud_workspace_manifest_entries me
					WHERE me.workspace_id = o.workspace_id AND me.sha256 = o.sha256
				  )
			), 0),
			COALESCE((
				SELECT SUM(CASE
					WHEN COALESCE(o.plain_size_bytes, 0) > 0 THEN o.plain_size_bytes
					WHEN COALESCE(o.size_bytes, 0) > 0 THEN o.size_bytes
					ELSE 0 END)
				FROM cloud_workspace_objects o
				JOIN scope s ON s.workspace_id = o.workspace_id
				WHERE LOWER(COALESCE(NULLIF(TRIM(o.object_state), ''), 'ready')) IN ('ready', 'deleting')
				  AND NOT EXISTS (
					SELECT 1 FROM cloud_workspace_manifest_entries me
					WHERE me.workspace_id = o.workspace_id AND me.sha256 = o.sha256
				  )
				  AND EXISTS (
					SELECT 1
					FROM cloud_workspace_snapshot_entries se
					JOIN cloud_workspace_snapshots ss ON ss.snapshot_id = se.snapshot_id
					WHERE ss.workspace_id = o.workspace_id AND se.sha256 = o.sha256
				  )
			), 0),
			COALESCE((
				SELECT SUM(CASE
					WHEN COALESCE(o.plain_size_bytes, 0) > 0 THEN o.plain_size_bytes
					WHEN COALESCE(o.size_bytes, 0) > 0 THEN o.size_bytes
					ELSE 0 END)
				FROM cloud_workspace_objects o
				JOIN scope s ON s.workspace_id = o.workspace_id
				WHERE LOWER(COALESCE(NULLIF(TRIM(o.object_state), ''), 'ready')) != 'staging'
				  AND NOT EXISTS (
					SELECT 1 FROM cloud_workspace_manifest_entries me
					WHERE me.workspace_id = o.workspace_id AND me.sha256 = o.sha256
				  )
				  AND NOT EXISTS (
					SELECT 1
					FROM cloud_workspace_snapshot_entries se
					JOIN cloud_workspace_snapshots ss ON ss.snapshot_id = se.snapshot_id
					WHERE ss.workspace_id = o.workspace_id AND se.sha256 = o.sha256
				  )
			), 0),
			COALESCE((
				SELECT SUM(CASE
					WHEN COALESCE(o.plain_size_bytes, 0) > 0 THEN o.plain_size_bytes
					WHEN COALESCE(o.size_bytes, 0) > 0 THEN o.size_bytes
					ELSE 0 END)
				FROM cloud_workspace_objects o
				JOIN scope s ON s.workspace_id = o.workspace_id
				WHERE LOWER(COALESCE(NULLIF(TRIM(o.object_state), ''), 'ready')) = 'staging'
			), 0) +
			COALESCE((
				SELECT SUM(c.size_bytes)
				FROM cloud_workspace_staging_chunks c
				JOIN scope s ON s.workspace_id = c.workspace_id
			), 0)`
	var objectBytes int64
	if err := q.QueryRowContext(ctx, query, args...).Scan(
		&usage.LogicalBytes,
		&objectBytes,
		&usage.CurrentObjectBytes,
		&usage.SnapshotRetainedBytes,
		&usage.UnreferencedRetainedBytes,
		&usage.StagingBytes,
	); err != nil {
		return RetainedUsage{}, err
	}
	usage.RetainedBytes = objectBytes + usage.StagingBytes
	return usage, nil
}

func workspaceRetainedUsage(ctx context.Context, q queryer, workspaceID string) (RetainedUsage, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	var exists int
	if err := q.QueryRowContext(ctx, `SELECT 1 FROM cloud_workspaces WHERE id = ?`, workspaceID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RetainedUsage{}, ErrNotFound
		}
		return RetainedUsage{}, err
	}
	return retainedUsageForScope(ctx, q, `SELECT id FROM cloud_workspaces WHERE id = ?`, workspaceID)
}

func tenantRetainedUsage(ctx context.Context, q queryer, tenantID string) (RetainedUsage, error) {
	return retainedUsageForScope(ctx, q,
		`SELECT id FROM cloud_workspaces WHERE tenant_id = ?`, store.NormalizeTenantID(tenantID))
}

func allRetainedUsage(ctx context.Context, q queryer) (RetainedUsage, error) {
	return retainedUsageForScope(ctx, q, `SELECT id FROM cloud_workspaces`)
}

// UsageForWorkspace returns the authoritative live usage view. It deliberately
// has no cached retained_bytes column for GC to drift from.
func (s *Store) UsageForWorkspace(ctx context.Context, workspaceID string) (RetainedUsage, error) {
	if s == nil || s.db == nil {
		return RetainedUsage{}, ErrUnavailable
	}
	return workspaceRetainedUsage(ctx, s.db, workspaceID)
}

// TenantRetainedUsage includes active, provisioning, and soft-deleted
// workspaces. Deleted bytes remain charged until the recoverable data is
// physically purged and its metadata transaction commits.
func (s *Store) TenantRetainedUsage(ctx context.Context, tenantID string) (RetainedUsage, error) {
	if s == nil || s.db == nil {
		return RetainedUsage{}, ErrUnavailable
	}
	return tenantRetainedUsage(ctx, s.db, tenantID)
}

func (s *Store) TotalRetainedUsage(ctx context.Context) (RetainedUsage, error) {
	if s == nil || s.db == nil {
		return RetainedUsage{}, ErrUnavailable
	}
	return allRetainedUsage(ctx, s.db)
}

// admitRetainedDelta is called only inside BEGIN IMMEDIATE write
// transactions. A zero/negative delta must remain admissible so an over-quota
// workspace can retry or shrink an existing reservation and finish cleanup.
func admitRetainedDelta(ctx context.Context, q queryer, tenantID, workspaceID string, delta, maxWorkspaceBytes, tenantMaxTotalBytes int64) error {
	if delta <= 0 {
		return nil
	}
	workspaceUsage, err := workspaceRetainedUsage(ctx, q, workspaceID)
	if err != nil {
		return err
	}
	if maxWorkspaceBytes > 0 && workspaceUsage.RetainedBytes+delta > maxWorkspaceBytes {
		return ErrWorkspaceSize
	}
	tenantUsage, err := tenantRetainedUsage(ctx, q, tenantID)
	if err != nil {
		return err
	}
	if tenantMaxTotalBytes > 0 && tenantUsage.RetainedBytes+delta > tenantMaxTotalBytes {
		return ErrTenantDisk
	}
	return nil
}
