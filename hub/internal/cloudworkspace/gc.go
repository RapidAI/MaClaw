package cloudworkspace

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/diagnostics"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	FailureCategory = "cloud_workspace"
	EventGCFailed   = "gc_failed"
	EventSyncFailed = "sync_failed"

	// UnreferencedGrace keeps newly uploaded objects until a manifest can reference them.
	UnreferencedGrace = time.Hour
	// StagingGrace drops incomplete objects/{sha256}.part and staging dirs.
	StagingGrace = time.Hour

	sidecarDirName  = "sidecars"
	manifestDirName = "manifest"
)

// SweepResult is the hourly GC accounting snapshot.
type SweepResult struct {
	PurgedWorkspaces int
	PurgedObjects    int
	PurgedParts      int
	RecalcWorkspaces int
}

type unreferencedObject struct {
	WorkspaceID string
	SHA256      string
	TenantID    string
	UserID      string
}

func (s *Service) recordFailure(ctx context.Context, tenantID, entityID, eventCode, message string, details map[string]any) {
	if s == nil || s.Failures == nil {
		return
	}
	s.Failures.Record(ctx, diagnostics.FailureEventInput{
		TenantID:  tenantID,
		Category:  FailureCategory,
		EventCode: eventCode,
		Message:   message,
		EntityID:  entityID,
		Details:   details,
	})
}

// RecordSyncFailed writes failure_event_logs event_code=sync_failed.
func (s *Service) RecordSyncFailed(ctx context.Context, tenantID, workspaceID, message string) {
	s.recordFailure(ctx, tenantID, workspaceID, EventSyncFailed, message, nil)
}

// StartHourlyGC runs Sweep immediately then once per hour.
func (s *Service) StartHourlyGC() {
	if s == nil || s.Workspaces == nil {
		return
	}
	go func() {
		run := func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			if _, err := s.Sweep(ctx, s.now()); err != nil {
				log.Printf("[cloud-workspace] gc failed err=%v", err)
				s.recordFailure(ctx, "", "", EventGCFailed, err.Error(), nil)
			}
		}
		run()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			run()
		}
	}()
}

// Sweep purges expired soft-deletes, unreferenced blobs, stale staging, then recalcs usage.
func (s *Service) Sweep(ctx context.Context, now time.Time) (SweepResult, error) {
	var out SweepResult
	if s == nil || s.Workspaces == nil {
		return out, ErrUnavailable
	}
	now = now.UTC()
	n, err := s.purgeExpiredDeleted(ctx, now)
	if err != nil {
		return out, err
	}
	out.PurgedWorkspaces = n
	n, err = s.purgeUnreferenced(ctx, now)
	if err != nil {
		return out, err
	}
	out.PurgedObjects = n
	n, err = s.purgeStaleParts(now)
	if err != nil {
		return out, err
	}
	out.PurgedParts = n
	n, err = s.Workspaces.RecalcUsage(ctx)
	if err != nil {
		s.recordFailure(ctx, "", "", EventGCFailed, err.Error(), map[string]any{"step": "recalc_usage"})
		return out, err
	}
	out.RecalcWorkspaces = n
	return out, nil
}

func (s *Service) purgeExpiredDeleted(ctx context.Context, now time.Time) (int, error) {
	cutoff := now.Add(-RestoreWindow)
	rows, err := s.Workspaces.ListDeletedBefore(ctx, cutoff)
	if err != nil {
		s.recordFailure(ctx, "", "", EventGCFailed, err.Error(), map[string]any{"step": "list_deleted"})
		return 0, err
	}
	purged := 0
	for _, ws := range rows {
		if ws == nil {
			continue
		}
		if err := s.purgeOneDeleted(ctx, ws, now); err != nil {
			log.Printf("[cloud-workspace] gc_failed workspace_id=%s tenant_id=%s err=%v", ws.ID, ws.TenantID, err)
			s.recordFailure(ctx, ws.TenantID, ws.ID, EventGCFailed, err.Error(), map[string]any{
				"step": "purge_deleted", "user_id": ws.UserID,
			})
			continue
		}
		purged++
		log.Printf("[cloud-workspace] gc workspace_id=%s tenant_id=%s user_id=%s purged used_bytes=%d file_count=%d",
			ws.ID, ws.TenantID, ws.UserID, ws.UsedBytes, ws.FileCount)
	}
	return purged, nil
}

func (s *Service) purgeOneDeleted(ctx context.Context, ws *Workspace, now time.Time) error {
	if err := s.Workspaces.HardDeleteExpired(ctx, ws.ID, now); err != nil {
		return err
	}
	if s.Blobs == nil {
		return ErrUnavailable
	}
	return s.Blobs.RemoveWorkspace(ws.TenantID, ws.UserID, ws.ID)
}

func (s *Service) purgeUnreferenced(ctx context.Context, now time.Time) (int, error) {
	cutoff := now.Add(-UnreferencedGrace)
	objs, err := s.Workspaces.ListUnreferenced(ctx, cutoff)
	if err != nil {
		s.recordFailure(ctx, "", "", EventGCFailed, err.Error(), map[string]any{"step": "list_unreferenced"})
		return 0, err
	}
	purged := 0
	for _, obj := range objs {
		if err := s.Workspaces.DeleteUnreferenced(ctx, obj, cutoff, func() error {
			if s.Blobs == nil {
				return ErrUnavailable
			}
			return s.Blobs.RemoveObjectFile(obj.TenantID, obj.UserID, obj.WorkspaceID, obj.SHA256)
		}); err != nil {
			log.Printf("[cloud-workspace] gc_failed workspace_id=%s sha256=%s err=%v", obj.WorkspaceID, obj.SHA256, err)
			s.recordFailure(ctx, obj.TenantID, obj.WorkspaceID, EventGCFailed, err.Error(), map[string]any{
				"step": "purge_unreferenced", "sha256": obj.SHA256,
			})
			continue
		}
		purged++
		log.Printf("[cloud-workspace] gc unreferenced workspace_id=%s sha256=%s", obj.WorkspaceID, obj.SHA256)
	}
	return purged, nil
}

func (s *Service) purgeStaleParts(now time.Time) (int, error) {
	if s.Blobs == nil {
		return 0, nil
	}
	n, err := s.Blobs.RemoveStaleParts(now, StagingGrace)
	if err != nil {
		s.recordFailure(context.Background(), "", "", EventGCFailed, err.Error(), map[string]any{"step": "purge_staging"})
		return n, err
	}
	return n, nil
}

func (s *Store) ListDeletedBefore(ctx context.Context, cutoff time.Time) ([]*Workspace, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+workspaceCols+` FROM cloud_workspaces WHERE status = ? AND deleted_at IS NOT NULL AND deleted_at < ?`,
		StatusDeleted, cutoff.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Workspace{}
	for rows.Next() {
		ws, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ws)
	}
	return out, rows.Err()
}

func (s *Store) HardDeleteExpired(ctx context.Context, id string, now time.Time) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	id = strings.TrimSpace(id)
	now = now.UTC()
	deletedBefore := now.Add(-RestoreWindow).Format(time.RFC3339)
	return s.withImmediate(ctx, func(q queryer) error {
		ws, err := scanWorkspace(q.QueryRowContext(ctx,
			`SELECT `+workspaceCols+` FROM cloud_workspaces WHERE id = ?`, id,
		))
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if ws.Status != StatusDeleted {
			return nil
		}
		deadline, ok := restoreDeadline(ws.DeletedAt)
		if !ok || now.Before(deadline) {
			return nil
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM cloud_workspace_manifest_entries WHERE workspace_id = ?`, id); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM cloud_workspace_objects WHERE workspace_id = ?`, id); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM cloud_workspace_leases WHERE workspace_id = ?`, id); err != nil {
			return err
		}
		_, err = q.ExecContext(ctx,
			`DELETE FROM cloud_workspaces WHERE id = ? AND status = ? AND deleted_at IS NOT NULL AND deleted_at < ?`,
			id, StatusDeleted, deletedBefore,
		)
		return err
	})
}

func (s *Store) ListUnreferenced(ctx context.Context, cutoff time.Time) ([]unreferencedObject, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT o.workspace_id, o.sha256, w.tenant_id, w.user_id
		FROM cloud_workspace_objects o
		JOIN cloud_workspaces w ON w.id = o.workspace_id
		WHERE o.ref_count = 0 AND o.created_at < ? AND w.status != ?`,
		cutoff.UTC().Format(time.RFC3339), StatusDeleted,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []unreferencedObject{}
	for rows.Next() {
		var obj unreferencedObject
		if err := rows.Scan(&obj.WorkspaceID, &obj.SHA256, &obj.TenantID, &obj.UserID); err != nil {
			return nil, err
		}
		out = append(out, obj)
	}
	return out, rows.Err()
}

func (s *Store) DeleteUnreferenced(ctx context.Context, obj unreferencedObject, cutoff time.Time, removeFile func() error) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	ts := cutoff.UTC().Format(time.RFC3339)
	return s.withImmediate(ctx, func(q queryer) error {
		var ref int
		var status string
		err := q.QueryRowContext(ctx, `
			SELECT o.ref_count, w.status FROM cloud_workspace_objects o
			JOIN cloud_workspaces w ON w.id = o.workspace_id
			WHERE o.workspace_id = ? AND o.sha256 = ? AND o.created_at < ?`,
			obj.WorkspaceID, obj.SHA256, ts,
		).Scan(&ref, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if ref != 0 || status == StatusDeleted {
			return nil
		}
		if removeFile != nil {
			if err := removeFile(); err != nil {
				return err
			}
		}
		_, err = q.ExecContext(ctx,
			`DELETE FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ? AND ref_count = 0`,
			obj.WorkspaceID, obj.SHA256,
		)
		return err
	})
}

func (s *Store) RecalcUsage(ctx context.Context) (int, error) {
	if s == nil || s.db == nil {
		return 0, ErrUnavailable
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE cloud_workspaces SET
			used_bytes = (SELECT COALESCE(SUM(size_bytes), 0) FROM cloud_workspace_manifest_entries e WHERE e.workspace_id = cloud_workspaces.id),
			file_count = (SELECT COUNT(*) FROM cloud_workspace_manifest_entries e WHERE e.workspace_id = cloud_workspaces.id)`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *Store) CountOpenLeases(ctx context.Context, now time.Time) (int64, error) {
	if s == nil || s.db == nil {
		return 0, ErrUnavailable
	}
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM cloud_workspace_leases WHERE released_at IS NULL AND expires_at > ?`,
		now.UTC().Format(time.RFC3339),
	).Scan(&n)
	return n, err
}

func (s *Store) SumUsedBytes(ctx context.Context) (int64, error) {
	if s == nil || s.db == nil {
		return 0, ErrUnavailable
	}
	var n sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(used_bytes), 0) FROM cloud_workspaces`).Scan(&n)
	if err != nil {
		return 0, err
	}
	if n.Valid {
		return n.Int64, nil
	}
	return 0, nil
}

func (s *Store) ListSettingTenantIDs(ctx context.Context) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT key FROM system_settings WHERE key = ? OR key LIKE ?`,
		SettingsKey, "tenant:%:"+SettingsKey,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		if id, ok := tenantIDFromSettingsKey(key); ok {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}

func (s *Store) ListDistinctTenantIDs(ctx context.Context) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT tenant_id FROM cloud_workspaces`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		id = strings.TrimSpace(id)
		if id != "" {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}

func tenantIDFromSettingsKey(key string) (string, bool) {
	key = strings.TrimSpace(key)
	if key == SettingsKey {
		return store.DefaultTenantID, true
	}
	prefix := "tenant:"
	suffix := ":" + SettingsKey
	if strings.HasPrefix(key, prefix) && strings.HasSuffix(key, suffix) {
		id := strings.TrimSuffix(strings.TrimPrefix(key, prefix), suffix)
		if id != "" {
			return id, true
		}
	}
	return "", false
}
