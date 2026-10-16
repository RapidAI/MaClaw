package cloudworkspace

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strconv"
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
	// SnapshotRetentionCount bounds immutable manifest roots per workspace. The
	// newest roots remain recoverable; older roots no longer pin objects forever.
	SnapshotRetentionCount = 20

	manifestDirName = "manifest"
)

// SweepResult is the hourly GC accounting snapshot.
type SweepResult struct {
	PurgedWorkspaces     int
	PurgedObjects        int
	ReconciledOrphanObjs int
	RemovedOrphanDirs    int
	PurgedParts          int
	PurgedSnapshots      int
	RecalcWorkspaces     int
	ReconciledStaging    int
	ReconciledProvisions int
}

// StagingReconcileResult describes repairs between durable reservations and
// on-disk objects/{sha}.part/{index} files.
type StagingReconcileResult struct {
	AdoptedFiles   int
	ResizedRows    int
	RemovedRows    int
	SkippedInvalid int
}

const stagingMissingGrace = 5 * time.Minute

type stagingChunkKey struct {
	sha   string
	index int
}

type unreferencedObject struct {
	WorkspaceID string
	SHA256      string
	TenantID    string
	UserID      string
}

func (s *Service) recordFailure(ctx context.Context, tenantID, entityID, eventCode, message string, details map[string]any) {
	if eventCode == EventGCFailed {
		metricGCFailures.Add(1)
	}
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

// StartHourlyGC runs Sweep immediately then once per hour. Repeat calls are no-ops.
func (s *Service) StartHourlyGC() {
	if s == nil || s.Workspaces == nil {
		return
	}
	s.gcMu.Lock()
	defer s.gcMu.Unlock()
	if s.gcStop != nil {
		return
	}
	stop := make(chan struct{})
	s.gcStop = stop
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
		for {
			select {
			case <-ticker.C:
				run()
			case <-stop:
				return
			}
		}
	}()
}

// StopHourlyGC stops the background sweeper started by StartHourlyGC.
func (s *Service) StopHourlyGC() {
	if s == nil {
		return
	}
	s.gcMu.Lock()
	stop := s.gcStop
	s.gcStop = nil
	s.gcMu.Unlock()
	if stop != nil {
		close(stop)
	}
}

// Sweep purges expired soft-deletes, unreferenced blobs, stale staging, then recalcs usage.
func (s *Service) Sweep(ctx context.Context, now time.Time) (SweepResult, error) {
	var out SweepResult
	if s == nil || s.Workspaces == nil {
		return out, ErrUnavailable
	}
	now = now.UTC()
	var n int
	var err error
	// A service without a BlobStore is still useful for metadata-only health
	// checks. Preserve the previous behavior (per-item cleanup failures are
	// recorded and retried later) instead of failing the entire sweep before it
	// can recalculate usage.
	if s.Blobs != nil {
		n, err = s.reconcileOrphanWorkspaces(ctx, now)
		if err != nil {
			return out, err
		}
		out.RemovedOrphanDirs = n
	}
	n, err = s.purgeExpiredDeleted(ctx, now)
	if err != nil {
		return out, err
	}
	out.PurgedWorkspaces += n
	if n, err := s.Workspaces.ReconcileStaleWorkspaceTaskProvisions(ctx, now, ProvisioningGrace); err != nil {
		return out, err
	} else {
		out.ReconciledProvisions = n
	}
	n, err = s.Workspaces.PurgeOldSnapshots(ctx, SnapshotRetentionCount)
	if err != nil {
		return out, err
	}
	out.PurgedSnapshots = n
	n, err = s.purgeUnreferenced(ctx, now)
	if err != nil {
		return out, err
	}
	out.PurgedObjects = n
	reconciled, err := s.reconcileStaging(ctx, now)
	if err != nil {
		return out, err
	}
	out.ReconciledStaging = reconciled.AdoptedFiles + reconciled.ResizedRows + reconciled.RemovedRows
	if n, err := s.reconcileOrphanObjects(ctx, now); err != nil {
		return out, err
	} else {
		out.ReconciledOrphanObjs = n
	}
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

// reconcileOrphanObjects removes old encrypted object files that have no
// corresponding metadata row. A crash can leave a fully written .enc file
// immediately before recordObjectMeta commits; such a file is intentionally
// invisible to BlobStore.Get/Has and must eventually be reclaimed so it cannot
// accumulate outside the quota ledger. Recent files are left alone for one
// grace window to let an in-flight uploader finish its metadata transaction.
func (s *Service) reconcileOrphanObjects(ctx context.Context, now time.Time) (int, error) {
	if s == nil || s.Workspaces == nil || s.Blobs == nil || s.Workspaces.db == nil {
		return 0, nil
	}
	rows, err := s.Workspaces.db.QueryContext(ctx, `SELECT id, tenant_id, user_id FROM cloud_workspaces`)
	if err != nil {
		return 0, err
	}
	type workspaceRef struct{ id, tenant, user string }
	workspaces := make([]workspaceRef, 0)
	for rows.Next() {
		var ref workspaceRef
		if err := rows.Scan(&ref.id, &ref.tenant, &ref.user); err != nil {
			rows.Close()
			return 0, err
		}
		workspaces = append(workspaces, ref)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	removed := 0
	for _, ref := range workspaces {
		dir, err := s.Blobs.ObjectsDir(ref.tenant, ref.user, ref.id)
		if err != nil {
			continue
		}
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return removed, err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), objectFileExt) {
				continue
			}
			sha := strings.TrimSuffix(entry.Name(), objectFileExt)
			if !ValidSHA256Hex(sha) {
				// Object names are content-addressed; an old malformed file in
				// this directory cannot be referenced by a manifest.
				if info, statErr := entry.Info(); statErr == nil && !now.After(info.ModTime().UTC().Add(stagingMissingGrace)) {
					continue
				}
				if removeErr := os.Remove(filepath.Join(dir, entry.Name())); removeErr == nil || os.IsNotExist(removeErr) {
					removed++
				}
				continue
			}
			info, statErr := entry.Info()
			if statErr != nil || !now.After(info.ModTime().UTC().Add(stagingMissingGrace)) {
				continue
			}
			var exists int
			qErr := s.Workspaces.db.QueryRowContext(ctx, `SELECT 1 FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ? LIMIT 1`, ref.id, sha).Scan(&exists)
			if qErr == nil {
				continue
			}
			if !errors.Is(qErr, sql.ErrNoRows) {
				return removed, qErr
			}
			if removeErr := os.Remove(filepath.Join(dir, entry.Name())); removeErr == nil || os.IsNotExist(removeErr) {
				removed++
			}
		}
	}
	return removed, nil
}

// reconcileStaging makes the reservation table and on-disk chunk directories
// converge after crashes, manual cleanup, or a Hub process restart. It is
// idempotent and safe to run on every GC sweep.
func (s *Service) reconcileStaging(ctx context.Context, now time.Time) (StagingReconcileResult, error) {
	var result StagingReconcileResult
	if s == nil || s.Workspaces == nil || s.Blobs == nil || s.Workspaces.db == nil {
		return result, nil
	}
	rows, err := s.Workspaces.db.QueryContext(ctx, `SELECT id, tenant_id, user_id FROM cloud_workspaces`)
	if err != nil {
		return result, err
	}
	type workspaceRef struct{ id, tenant, user string }
	workspaces := make([]workspaceRef, 0)
	for rows.Next() {
		var ref workspaceRef
		if err := rows.Scan(&ref.id, &ref.tenant, &ref.user); err != nil {
			rows.Close()
			return result, err
		}
		workspaces = append(workspaces, ref)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()
	for _, ref := range workspaces {
		adopted, resized, removed, invalid, err := s.reconcileWorkspaceStaging(ctx, ref.id, ref.tenant, ref.user, now)
		if err != nil {
			return result, err
		}
		result.AdoptedFiles += adopted
		result.ResizedRows += resized
		result.RemovedRows += removed
		result.SkippedInvalid += invalid
	}
	return result, nil
}

func (s *Service) reconcileWorkspaceStaging(ctx context.Context, workspaceID, tenantID, userID string, now time.Time) (adopted, resized, removed, invalid int, err error) {
	objectsDir, dirErr := s.Blobs.ObjectsDir(tenantID, userID, workspaceID)
	if dirErr != nil {
		return 0, 0, 0, 0, nil // malformed legacy ownership rows are ignored
	}
	physical := map[stagingChunkKey]int64{}
	physicalUpdated := map[stagingChunkKey]string{}
	entries, readErr := os.ReadDir(objectsDir)
	if readErr != nil && !os.IsNotExist(readErr) {
		return 0, 0, 0, 0, readErr
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasSuffix(entry.Name(), objectPartExt) {
			continue
		}
		sha := strings.TrimSuffix(entry.Name(), objectPartExt)
		if !ValidSHA256Hex(sha) {
			invalid++
			continue
		}
		partDir := filepath.Join(objectsDir, entry.Name())
		parts, partErr := os.ReadDir(partDir)
		if partErr != nil {
			if os.IsNotExist(partErr) {
				continue
			}
			return adopted, resized, removed, invalid, partErr
		}
		for _, part := range parts {
			if part.IsDir() {
				continue
			}
			index, convErr := strconv.Atoi(part.Name())
			if convErr != nil || index < 0 || index >= maxChunkCount || strconv.Itoa(index) != part.Name() {
				invalid++
				continue
			}
			info, statErr := part.Info()
			if statErr != nil {
				continue
			}
			size := info.Size()
			if size <= 0 || size > MaxChunkBytes {
				invalid++
				continue
			}
			key := stagingChunkKey{sha: sha, index: index}
			physical[key] = size
			physicalUpdated[key] = info.ModTime().UTC().Format(time.RFC3339)
		}
	}

	err = s.Workspaces.withImmediate(ctx, func(q queryer) error {
		rows, queryErr := q.QueryContext(ctx, `SELECT sha256, chunk_index, size_bytes, updated_at FROM cloud_workspace_staging_chunks WHERE workspace_id = ?`, workspaceID)
		if queryErr != nil {
			return queryErr
		}
		dbRows := map[stagingChunkKey]struct {
			size    int64
			updated string
		}{}
		for rows.Next() {
			var sha, updated string
			var index, size int64
			if scanErr := rows.Scan(&sha, &index, &size, &updated); scanErr != nil {
				rows.Close()
				return scanErr
			}
			key := stagingChunkKey{sha: sha, index: int(index)}
			dbRows[key] = struct {
				size    int64
				updated string
			}{size: size, updated: updated}
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			rows.Close()
			return rowsErr
		}
		rows.Close()

		cutoff := now.Add(-stagingMissingGrace)
		for key, dbRow := range dbRows {
			size, exists := physical[key]
			if !exists {
				updatedAt, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(dbRow.updated))
				if parseErr == nil && updatedAt.After(cutoff) {
					continue
				}
				if _, execErr := q.ExecContext(ctx, `DELETE FROM cloud_workspace_staging_chunks WHERE workspace_id = ? AND sha256 = ? AND chunk_index = ?`, workspaceID, key.sha, key.index); execErr != nil {
					return execErr
				}
				removed++
				continue
			}
			if dbRow.size != size {
				if _, execErr := q.ExecContext(ctx, `UPDATE cloud_workspace_staging_chunks SET size_bytes = ?, updated_at = ? WHERE workspace_id = ? AND sha256 = ? AND chunk_index = ?`, size, now.UTC().Format(time.RFC3339), workspaceID, key.sha, key.index); execErr != nil {
					return execErr
				}
				resized++
			}
		}

		knownHashes := map[string]struct{}{}
		for key := range dbRows {
			knownHashes[key.sha] = struct{}{}
		}
		for key, size := range physical {
			if _, exists := dbRows[key]; exists {
				continue
			}
			if len(knownHashes) >= MaxStagingHashes {
				continue
			}
			updated := physicalUpdated[key]
			if updatedAt, parseErr := time.Parse(time.RFC3339, updated); parseErr == nil && !updatedAt.After(now.Add(-StagingGrace)) {
				continue
			}
			if _, execErr := q.ExecContext(ctx, `INSERT OR IGNORE INTO cloud_workspace_staging_chunks (workspace_id, sha256, chunk_index, size_bytes, updated_at) VALUES (?, ?, ?, ?, ?)`, workspaceID, key.sha, key.index, size, now.UTC().Format(time.RFC3339)); execErr != nil {
				return execErr
			}
			knownHashes[key.sha] = struct{}{}
			adopted++
		}
		return nil
	})
	return adopted, resized, removed, invalid, err
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
	if s.Blobs == nil {
		return ErrUnavailable
	}
	ok, err := s.Workspaces.IsExpiredDeleted(ctx, ws.ID, now)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	// Files first so a blob failure leaves the row for the next hourly retry. A
	// crash between unlink and the metadata commit is reclaimed by the sweep's
	// orphan-directory reconciliation.
	if err := s.Blobs.RemoveWorkspace(ws.TenantID, ws.UserID, ws.ID); err != nil {
		return err
	}
	return s.Workspaces.HardDeleteExpired(ctx, ws.ID, now)
}

// reconcileOrphanWorkspaces removes on-disk workspace directories whose
// metadata row is already gone. The workspace row is always created before any
// blob directory exists, so a directory without a row is crash residue from a
// purge that died between filesystem cleanup and the final SQLite commit. A
// modtime grace window keeps freshly created directories out of the sweep.
func (s *Service) reconcileOrphanWorkspaces(ctx context.Context, now time.Time) (int, error) {
	if s == nil || s.Workspaces == nil || s.Blobs == nil || s.Workspaces.db == nil {
		return 0, nil
	}
	root := strings.TrimSpace(s.Blobs.Root)
	if root == "" {
		return 0, nil
	}
	keyDir := strings.TrimSpace(s.Blobs.KeyDir)
	if keyDir != "" {
		if abs, absErr := filepath.Abs(keyDir); absErr == nil {
			keyDir = abs
		}
	}
	tenantEntries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, tenantEntry := range tenantEntries {
		if !tenantEntry.IsDir() || !validPathSegment(tenantEntry.Name()) {
			continue
		}
		tenantDir := filepath.Join(root, tenantEntry.Name())
		if keyDir != "" {
			if abs, absErr := filepath.Abs(tenantDir); absErr == nil && abs == keyDir {
				continue
			}
		}
		userEntries, readErr := os.ReadDir(tenantDir)
		if readErr != nil {
			continue
		}
		for _, userEntry := range userEntries {
			if !userEntry.IsDir() || !validPathSegment(userEntry.Name()) {
				continue
			}
			wsEntries, wsErr := os.ReadDir(filepath.Join(tenantDir, userEntry.Name()))
			if wsErr != nil {
				continue
			}
			for _, wsEntry := range wsEntries {
				// Only directories shaped like a workspace ID are candidates;
				// KeyDir may point at the blob root in production, so anything
				// else (key backups, manual folders) must never be walked into.
				if !wsEntry.IsDir() || !strings.HasPrefix(wsEntry.Name(), idPrefix) || !validPathSegment(wsEntry.Name()) {
					continue
				}
				info, infoErr := wsEntry.Info()
				if infoErr != nil || !now.After(info.ModTime().UTC().Add(stagingMissingGrace)) {
					continue
				}
				var exists int
				qErr := s.Workspaces.db.QueryRowContext(ctx, `SELECT 1 FROM cloud_workspaces WHERE id = ?`, wsEntry.Name()).Scan(&exists)
				if qErr == nil {
					continue
				}
				if !errors.Is(qErr, sql.ErrNoRows) {
					return removed, qErr
				}
				if removeErr := s.Blobs.RemoveWorkspace(tenantEntry.Name(), userEntry.Name(), wsEntry.Name()); removeErr == nil || os.IsNotExist(removeErr) {
					removed++
				} else {
					s.recordFailure(ctx, tenantEntry.Name(), wsEntry.Name(), EventGCFailed, removeErr.Error(), map[string]any{"step": "orphan_workspace_dir"})
				}
			}
		}
	}
	return removed, nil
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
		ok, err := s.purgeOneUnreferenced(ctx, obj)
		if err != nil {
			log.Printf("[cloud-workspace] gc_failed workspace_id=%s sha256=%s err=%v", obj.WorkspaceID, obj.SHA256, err)
			s.recordFailure(ctx, obj.TenantID, obj.WorkspaceID, EventGCFailed, err.Error(), map[string]any{
				"step": "purge_unreferenced", "sha256": obj.SHA256,
			})
			continue
		}
		if !ok {
			continue
		}
		purged++
		log.Printf("[cloud-workspace] gc unreferenced workspace_id=%s sha256=%s", obj.WorkspaceID, obj.SHA256)
	}
	return purged, nil
}

func (s *Service) purgeOneUnreferenced(ctx context.Context, obj unreferencedObject) (bool, error) {
	// Mark first, unlink second, and delete metadata last. A crash or
	// permission failure during unlink therefore leaves a durable `deleting`
	// row that the next sweep can retry instead of losing the only pointer to
	// an orphaned file.
	marked, err := s.Workspaces.MarkUnreferencedDeleting(ctx, obj)
	if err != nil {
		return false, err
	}
	if !marked {
		return false, nil
	}
	if s.Blobs == nil {
		return false, ErrUnavailable
	}
	if err := s.Blobs.RemoveObjectFile(obj.TenantID, obj.UserID, obj.WorkspaceID, obj.SHA256); err != nil {
		s.recordFailure(ctx, obj.TenantID, obj.WorkspaceID, EventGCFailed, err.Error(), map[string]any{
			"step": "unlink_unreferenced", "sha256": obj.SHA256,
		})
		log.Printf("[cloud-workspace] gc_failed unlink workspace_id=%s sha256=%s err=%v", obj.WorkspaceID, obj.SHA256, err)
		return false, err
	}
	if err := s.Workspaces.FinalizeDeletingObject(ctx, obj); err != nil {
		return false, err
	}
	return true, nil
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
	// Remove durable reservations whose staging directories were reclaimed (or
	// whose process crashed before creating one). Leaving these rows behind
	// would permanently charge quota and block a retry for the same digest.
	if s.Workspaces != nil {
		if _, dbErr := s.Workspaces.PurgeStaleObjectReservations(context.Background(), now.Add(-StagingGrace)); dbErr != nil {
			return n, dbErr
		}
		if _, dbErr := s.Workspaces.PurgeStaleStagingChunks(context.Background(), now.Add(-StagingGrace)); dbErr != nil {
			return n, dbErr
		}
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

func (s *Store) IsExpiredDeleted(ctx context.Context, id string, now time.Time) (bool, error) {
	if s == nil || s.db == nil {
		return false, ErrUnavailable
	}
	ws, err := scanWorkspace(s.db.QueryRowContext(ctx,
		`SELECT `+workspaceCols+` FROM cloud_workspaces WHERE id = ?`, strings.TrimSpace(id),
	))
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if ws.Status != StatusDeleted {
		return false, nil
	}
	deadline, ok := restoreDeadline(ws.DeletedAt)
	if !ok || now.UTC().Before(deadline) {
		return false, nil
	}
	return true, nil
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
		if _, err := q.ExecContext(ctx, `DELETE FROM cloud_workspace_staging_chunks WHERE workspace_id = ?`, id); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM cloud_workspace_task_bindings WHERE workspace_id = ?`, id); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM cloud_workspace_task_provisions WHERE workspace_id = ?`, id); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM cloud_workspace_snapshot_entries WHERE snapshot_id IN (SELECT snapshot_id FROM cloud_workspace_snapshots WHERE workspace_id = ?)`, id); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM cloud_workspace_snapshots WHERE workspace_id = ?`, id); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM cloud_workspace_sidecars WHERE workspace_id = ?`, id); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM cloud_workspace_idempotency WHERE workspace_id = ?`, id); err != nil {
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
		WHERE o.ref_count = 0 AND COALESCE(o.object_state, 'ready') IN ('ready', 'deleting') AND o.created_at < ? AND w.status != ?
		  AND NOT EXISTS (
			SELECT 1 FROM cloud_workspace_snapshot_entries se
			JOIN cloud_workspace_snapshots ss ON ss.snapshot_id = se.snapshot_id
			WHERE ss.workspace_id = o.workspace_id AND se.sha256 = o.sha256
		  )`,
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

func (s *Store) DeleteUnreferencedRow(ctx context.Context, obj unreferencedObject) (bool, error) {
	if s == nil || s.db == nil {
		return false, ErrUnavailable
	}
	deleted := false
	err := s.withImmediate(ctx, func(q queryer) error {
		var ref int
		var status string
		err := q.QueryRowContext(ctx, `
			SELECT o.ref_count, w.status FROM cloud_workspace_objects o
			JOIN cloud_workspaces w ON w.id = o.workspace_id
			WHERE o.workspace_id = ? AND o.sha256 = ?
			  AND COALESCE(o.object_state, 'ready') = 'ready'
			  AND NOT EXISTS (
				SELECT 1 FROM cloud_workspace_snapshot_entries se
				JOIN cloud_workspace_snapshots ss ON ss.snapshot_id = se.snapshot_id
				WHERE ss.workspace_id = o.workspace_id AND se.sha256 = o.sha256
			  )`,
			obj.WorkspaceID, obj.SHA256,
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
		res, err := q.ExecContext(ctx,
			`DELETE FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ? AND ref_count = 0`,
			obj.WorkspaceID, obj.SHA256,
		)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		deleted = n > 0
		return nil
	})
	return deleted, err
}

// MarkUnreferencedDeleting atomically fences an unreferenced ready object for
// deletion. The row remains present until FinalizeDeletingObject succeeds.
func (s *Store) MarkUnreferencedDeleting(ctx context.Context, obj unreferencedObject) (bool, error) {
	if s == nil || s.db == nil {
		return false, ErrUnavailable
	}
	marked := false
	err := s.withImmediate(ctx, func(q queryer) error {
		var ref int
		var status, objectState string
		err := q.QueryRowContext(ctx, `
			SELECT o.ref_count, w.status, COALESCE(o.object_state, 'ready')
			FROM cloud_workspace_objects o JOIN cloud_workspaces w ON w.id = o.workspace_id
			WHERE o.workspace_id = ? AND o.sha256 = ?
			  AND NOT EXISTS (
				SELECT 1 FROM cloud_workspace_manifest_entries me WHERE me.workspace_id = o.workspace_id AND me.sha256 = o.sha256
			  )
			  AND NOT EXISTS (
				SELECT 1 FROM cloud_workspace_snapshot_entries se
				JOIN cloud_workspace_snapshots ss ON ss.snapshot_id = se.snapshot_id
				WHERE ss.workspace_id = o.workspace_id AND se.sha256 = o.sha256
			  )`, obj.WorkspaceID, obj.SHA256).Scan(&ref, &status, &objectState)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if ref != 0 || status == StatusDeleted {
			return nil
		}
		if strings.EqualFold(strings.TrimSpace(objectState), "deleting") {
			marked = true
			return nil
		}
		if !strings.EqualFold(strings.TrimSpace(objectState), "ready") {
			return nil
		}
		res, err := q.ExecContext(ctx, `UPDATE cloud_workspace_objects SET object_state = 'deleting' WHERE workspace_id = ? AND sha256 = ? AND ref_count = 0 AND COALESCE(object_state, 'ready') = 'ready'`, obj.WorkspaceID, obj.SHA256)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		marked = n > 0
		return nil
	})
	return marked, err
}

// FinalizeDeletingObject drops metadata only after its physical file has been
// unlinked. It is safe to retry after a process crash.
func (s *Store) FinalizeDeletingObject(ctx context.Context, obj unreferencedObject) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ? AND ref_count = 0 AND COALESCE(object_state, 'ready') = 'deleting'`, obj.WorkspaceID, obj.SHA256)
	return err
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

// PurgeOldSnapshots keeps only the newest retain snapshots for each workspace
// and removes their entry rows in the same transaction. Snapshot deletion does
// not touch object rows; the subsequent unreferenced-object sweep decides when
// bytes are safe to reclaim.
func (s *Store) PurgeOldSnapshots(ctx context.Context, retain int) (int, error) {
	if s == nil || s.db == nil {
		return 0, ErrUnavailable
	}
	if retain < 1 {
		retain = 1
	}
	var purged int
	err := s.withImmediate(ctx, func(q queryer) error {
		rows, err := q.QueryContext(ctx, `SELECT DISTINCT workspace_id FROM cloud_workspace_snapshots`)
		if err != nil {
			return err
		}
		var workspaceIDs []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			workspaceIDs = append(workspaceIDs, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, workspaceID := range workspaceIDs {
			oldIDs := []string{}
			oldRows, err := q.QueryContext(ctx, `SELECT snapshot_id FROM cloud_workspace_snapshots WHERE workspace_id = ? ORDER BY created_at DESC, snapshot_id DESC LIMIT -1 OFFSET ?`, workspaceID, retain)
			if err != nil {
				return err
			}
			for oldRows.Next() {
				var id string
				if err := oldRows.Scan(&id); err != nil {
					oldRows.Close()
					return err
				}
				oldIDs = append(oldIDs, id)
			}
			if err := oldRows.Err(); err != nil {
				oldRows.Close()
				return err
			}
			oldRows.Close()
			for _, snapshotID := range oldIDs {
				if _, err := q.ExecContext(ctx, `DELETE FROM cloud_workspace_snapshot_entries WHERE snapshot_id = ?`, snapshotID); err != nil {
					return err
				}
				res, err := q.ExecContext(ctx, `DELETE FROM cloud_workspace_snapshots WHERE snapshot_id = ?`, snapshotID)
				if err != nil {
					return err
				}
				n, _ := res.RowsAffected()
				purged += int(n)
			}
		}
		return nil
	})
	return purged, err
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

func (s *Store) CountOpenLeasesForTenant(ctx context.Context, tenantID string, now time.Time) (int64, error) {
	if s == nil || s.db == nil {
		return 0, ErrUnavailable
	}
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM cloud_workspace_leases l
		  JOIN cloud_workspaces w ON w.id = l.workspace_id
		 WHERE l.released_at IS NULL AND l.expires_at > ? AND w.tenant_id = ?`,
		now.UTC().Format(time.RFC3339), store.NormalizeTenantID(tenantID)).Scan(&n)
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

func (s *Store) SumUsedBytesForTenant(ctx context.Context, tenantID string) (int64, error) {
	if s == nil || s.db == nil {
		return 0, ErrUnavailable
	}
	var n sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(used_bytes), 0) FROM cloud_workspaces WHERE tenant_id = ?`,
		store.NormalizeTenantID(tenantID)).Scan(&n)
	if err != nil {
		return 0, err
	}
	if n.Valid {
		return n.Int64, nil
	}
	return 0, nil
}

func (s *Store) CountAuditRows(ctx context.Context, tenantID string) (int64, error) {
	if s == nil || s.db == nil {
		return 0, ErrUnavailable
	}
	if strings.TrimSpace(tenantID) == "" {
		return 0, ErrInvalidAuditEvent
	}
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM cloud_workspace_audit_events WHERE tenant_id = ?`,
		store.NormalizeTenantID(tenantID)).Scan(&n)
	return n, err
}

func (s *Store) CountAuditRowsAll(ctx context.Context) (int64, error) {
	if s == nil || s.db == nil {
		return 0, ErrUnavailable
	}
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cloud_workspace_audit_events`).Scan(&n)
	return n, err
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
