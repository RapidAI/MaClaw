package cloudworkspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/diagnostics"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func newGCService(t *testing.T) (*Service, *Store, *store.Store) {
	t.Helper()
	st, hub := newTestWorkspaceStore(t)
	root := t.TempDir()
	t.Setenv(masterKeyEnv, "")
	svc := &Service{
		System:     hub.System,
		Workspaces: st,
		Blobs:      &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), DB: st.db},
		Failures:   diagnostics.NewFailureEventRecorder(hub.FailureLogs),
	}
	return svc, st, hub
}

func seedLeasedWorkspace(t *testing.T, st *Store, now time.Time) *Workspace {
	t.Helper()
	insertTestMachine(t, st, "m1", "u1", "HOST-M1")
	ws, err := st.Create(context.Background(), CreateParams{TenantID: "t1", UserID: "u1", Name: "A", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Acquire(context.Background(), acquireParams(ws.ID, "m1"), now); err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestSweepPurgesExpiredDeletedWorkspace(t *testing.T) {
	svc, st, _ := newGCService(t)
	ctx := context.Background()
	now := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	ws := seedLeasedWorkspace(t, st, now)
	plain := []byte("purge-me")
	put, err := svc.Blobs.Put(ctx, "t1", "u1", ws.ID, plain)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReplaceManifest(ctx, "t1", "u1", ws.ID, "m1", "", []ManifestEntry{
		{Path: "a.txt", SHA256: put.SHA256, Size: put.SizeBytes},
	}, now); err != nil {
		t.Fatal(err)
	}
	base, err := svc.Blobs.workspaceDir("t1", "u1", ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, sidecarDirName), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, sidecarDirName, "meta.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, manifestDirName), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SoftDelete(ctx, "t1", "u1", "m1", ws.ID, now); err != nil {
		t.Fatal(err)
	}
	deletedAt := now.Add(-RestoreWindow - time.Hour).Format(time.RFC3339)
	if _, err := st.db.ExecContext(ctx, `UPDATE cloud_workspaces SET deleted_at = ? WHERE id = ?`, deletedAt, ws.ID); err != nil {
		t.Fatal(err)
	}
	used, err := st.TenantUsedBytes(ctx, "t1")
	if err != nil || used != put.SizeBytes {
		t.Fatalf("soft-deleted still counts used=%d err=%v", used, err)
	}

	got, err := svc.Sweep(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.PurgedWorkspaces != 1 {
		t.Fatalf("purged workspaces=%d want 1", got.PurgedWorkspaces)
	}
	if _, err := st.GetOwned(ctx, "t1", "u1", ws.ID); err != ErrNotFound {
		t.Fatalf("row still present err=%v", err)
	}
	if _, err := os.Stat(base); !os.IsNotExist(err) {
		t.Fatalf("workspace dir still present err=%v", err)
	}
	used, err = st.TenantUsedBytes(ctx, "t1")
	if err != nil || used != 0 {
		t.Fatalf("after gc used=%d err=%v", used, err)
	}
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM cloud_workspace_objects WHERE workspace_id = ?`, ws.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("object rows=%d", n)
	}
}

func TestSweepKeepsDeletedInsideRestoreWindow(t *testing.T) {
	svc, st, _ := newGCService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ws := seedLeasedWorkspace(t, st, now)
	if _, err := st.SoftDelete(ctx, "t1", "u1", "m1", ws.ID, now); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Sweep(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.PurgedWorkspaces != 0 {
		t.Fatalf("purged=%d", got.PurgedWorkspaces)
	}
	if _, err := st.GetOwned(ctx, "t1", "u1", ws.ID); err != nil {
		t.Fatalf("kept workspace err=%v", err)
	}
}

func TestSweepDeletesUnreferencedEncAfterGrace(t *testing.T) {
	svc, st, _ := newGCService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ws := seedLeasedWorkspace(t, st, now)
	put, err := svc.Blobs.Put(ctx, "t1", "u1", ws.ID, []byte("unref"))
	if err != nil {
		t.Fatal(err)
	}
	old := now.Add(-2 * time.Hour).Format(time.RFC3339)
	if _, err := st.db.ExecContext(ctx, `UPDATE cloud_workspace_objects SET created_at = ? WHERE workspace_id = ? AND sha256 = ?`, old, ws.ID, put.SHA256); err != nil {
		t.Fatal(err)
	}
	path, err := svc.Blobs.ObjectPath("t1", "u1", ws.ID, put.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Sweep(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.PurgedObjects != 1 {
		t.Fatalf("purged objects=%d", got.PurgedObjects)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("enc still present err=%v", err)
	}
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ?`, ws.ID, put.SHA256).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("object row remains n=%d", n)
	}
}

func TestDeleteUnreferencedRowSkipsReferencedBlob(t *testing.T) {
	svc, st, _ := newGCService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ws := seedLeasedWorkspace(t, st, now)
	put, err := svc.Blobs.Put(ctx, "t1", "u1", ws.ID, []byte("live"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE cloud_workspace_objects SET ref_count = 1, created_at = ? WHERE sha256 = ?`, now.Add(-2*time.Hour).Format(time.RFC3339), put.SHA256); err != nil {
		t.Fatal(err)
	}
	deleted, err := st.DeleteUnreferencedRow(ctx, unreferencedObject{WorkspaceID: ws.ID, SHA256: put.SHA256, TenantID: "t1", UserID: "u1"})
	if err != nil || deleted {
		t.Fatalf("deleted=%v err=%v", deleted, err)
	}
	path, err := svc.Blobs.ObjectPath("t1", "u1", ws.ID, put.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("referenced enc removed: %v", err)
	}
}

func TestSweepKeepsFreshUnreferencedAndDeletedWorkspaceObjects(t *testing.T) {
	svc, st, _ := newGCService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ws := seedLeasedWorkspace(t, st, now)
	fresh, err := svc.Blobs.Put(ctx, "t1", "u1", ws.ID, []byte("fresh"))
	if err != nil {
		t.Fatal(err)
	}
	held, err := svc.Blobs.Put(ctx, "t1", "u1", ws.ID, []byte("held-deleted"))
	if err != nil {
		t.Fatal(err)
	}
	old := now.Add(-3 * time.Hour).Format(time.RFC3339)
	if _, err := st.db.ExecContext(ctx, `UPDATE cloud_workspace_objects SET created_at = ? WHERE sha256 = ?`, old, held.SHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SoftDelete(ctx, "t1", "u1", "m1", ws.ID, now); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Sweep(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.PurgedObjects != 0 {
		t.Fatalf("purged objects=%d want 0", got.PurgedObjects)
	}
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM cloud_workspace_objects WHERE workspace_id = ?`, ws.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("rows=%d want 2 (fresh + deleted-workspace)", n)
	}
	path, err := svc.Blobs.ObjectPath("t1", "u1", ws.ID, fresh.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fresh enc missing: %v", err)
	}
}

func TestSweepDeletesStalePartAndStaging(t *testing.T) {
	svc, st, _ := newGCService(t)
	now := time.Now().UTC()
	ws := seedLeasedWorkspace(t, st, now)
	stalePart, err := svc.Blobs.PartDir("t1", "u1", ws.ID, plaintextSHA256([]byte("stale")))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stalePart, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stalePart, "0"), []byte("chunk"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-2 * time.Hour)
	if err := os.Chtimes(filepath.Join(stalePart, "0"), old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(stalePart, old, old); err != nil {
		t.Fatal(err)
	}
	staging, err := svc.Blobs.PrepareStaging("t1", "u1", ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(staging, old, old); err != nil {
		t.Fatal(err)
	}
	freshPart, err := svc.Blobs.PartDir("t1", "u1", ws.ID, plaintextSHA256([]byte("fresh")))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(freshPart, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(freshPart, "0"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Sweep(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if got.PurgedParts < 2 {
		t.Fatalf("purged parts=%d want >=2", got.PurgedParts)
	}
	if _, err := os.Stat(stalePart); !os.IsNotExist(err) {
		t.Fatalf("stale part still present err=%v", err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatalf("staging still present err=%v", err)
	}
	if _, err := os.Stat(freshPart); err != nil {
		t.Fatalf("fresh part removed: %v", err)
	}
}

func TestSweepReconcilesOrphanObjectFiles(t *testing.T) {
	svc, st, _ := newGCService(t)
	now := time.Now().UTC()
	ws := seedLeasedWorkspace(t, st, now)
	orphanSHA := plaintextSHA256([]byte("orphan-object"))
	orphanPath, err := svc.Blobs.ObjectPath("t1", "u1", ws.ID, orphanSHA)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(orphanPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphanPath, []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-2 * time.Hour)
	if err := os.Chtimes(orphanPath, old, old); err != nil {
		t.Fatal(err)
	}
	// A recent orphan is retained for the grace window in case an uploader is
	// between the file fsync and its metadata transaction.
	recentSHA := plaintextSHA256([]byte("recent-orphan"))
	recentPath, err := svc.Blobs.ObjectPath("t1", "u1", ws.ID, recentSHA)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recentPath, []byte("recent"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Sweep(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReconciledOrphanObjs != 1 {
		t.Fatalf("reconciled orphan objects=%d want 1", got.ReconciledOrphanObjs)
	}
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Fatalf("orphan object still present err=%v", err)
	}
	if _, err := os.Stat(recentPath); err != nil {
		t.Fatalf("recent orphan removed: %v", err)
	}
}

func TestSweepRecalcUsageFromManifest(t *testing.T) {
	svc, st, _ := newGCService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ws := seedLeasedWorkspace(t, st, now)
	put, err := svc.Blobs.Put(ctx, "t1", "u1", ws.ID, []byte("abcde"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReplaceManifest(ctx, "t1", "u1", ws.ID, "m1", "", []ManifestEntry{
		{Path: "a.txt", SHA256: put.SHA256, Size: put.SizeBytes},
		{Path: "b.txt", SHA256: put.SHA256, Size: put.SizeBytes},
	}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE cloud_workspaces SET used_bytes = 1, file_count = 0 WHERE id = ?`, ws.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Sweep(ctx, now); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetOwned(ctx, "t1", "u1", ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UsedBytes != put.SizeBytes*2 || got.FileCount != 2 {
		t.Fatalf("recalc=%+v want used=%d files=2", got, put.SizeBytes*2)
	}
}

func TestSweepRecordsGCFailed(t *testing.T) {
	svc, st, hub := newGCService(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	ws := seedLeasedWorkspace(t, st, now)
	put, err := svc.Blobs.Put(ctx, "t1", "u1", ws.ID, []byte("keep-until-retry"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReplaceManifest(ctx, "t1", "u1", ws.ID, "m1", "", []ManifestEntry{
		{Path: "a.txt", SHA256: put.SHA256, Size: put.SizeBytes},
	}, now); err != nil {
		t.Fatal(err)
	}
	base, err := svc.Blobs.workspaceDir("t1", "u1", ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SoftDelete(ctx, "t1", "u1", "m1", ws.ID, now); err != nil {
		t.Fatal(err)
	}
	deletedAt := now.Add(-RestoreWindow - time.Minute).Format(time.RFC3339)
	if _, err := st.db.ExecContext(ctx, `UPDATE cloud_workspaces SET deleted_at = ? WHERE id = ?`, deletedAt, ws.ID); err != nil {
		t.Fatal(err)
	}
	root := svc.Blobs.Root
	svc.Blobs.Root = ""
	got, err := svc.Sweep(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.PurgedWorkspaces != 0 {
		t.Fatalf("purged=%d want 0 on blob failure", got.PurgedWorkspaces)
	}
	if _, err := st.GetOwned(ctx, "t1", "u1", ws.ID); err != nil {
		t.Fatalf("row dropped after blob failure: %v", err)
	}
	if _, err := os.Stat(base); err != nil {
		t.Fatalf("workspace dir dropped after blob failure: %v", err)
	}
	used, err := st.TenantUsedBytes(ctx, "t1")
	if err != nil || used != put.SizeBytes {
		t.Fatalf("quota dropped after blob failure used=%d err=%v", used, err)
	}
	items, _, err := hub.FailureLogs.List(ctx, store.FailureEventLogFilter{Category: FailureCategory, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item != nil && item.EventCode == EventGCFailed {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected gc_failed log, got %+v", items)
	}

	svc.Blobs.Root = root
	got, err = svc.Sweep(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.PurgedWorkspaces != 1 {
		t.Fatalf("retry purged=%d want 1", got.PurgedWorkspaces)
	}
	if _, err := st.GetOwned(ctx, "t1", "u1", ws.ID); err != ErrNotFound {
		t.Fatalf("row still present after retry err=%v", err)
	}
	if _, err := os.Stat(base); !os.IsNotExist(err) {
		t.Fatalf("workspace dir still present after retry err=%v", err)
	}
}

func TestRecordSyncFailed(t *testing.T) {
	svc, _, hub := newGCService(t)
	ctx := context.Background()
	svc.RecordSyncFailed(ctx, "t1", "cws_x", "push failed")
	items, _, err := hub.FailureLogs.List(ctx, store.FailureEventLogFilter{Category: FailureCategory, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item != nil && item.EventCode == EventSyncFailed && item.EntityID == "cws_x" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected sync_failed log, got %+v", items)
	}
}

func TestReconcileStagingReservationsAdoptsResizesAndRemoves(t *testing.T) {
	svc, st, _ := newGCService(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	ws := seedLeasedWorkspace(t, st, now)
	sha := strings.Repeat("a", 64)
	partDir, err := svc.Blobs.PartDir("t1", "u1", ws.ID, sha)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(partDir, 0o700); err != nil {
		t.Fatal(err)
	}
	part := filepath.Join(partDir, "0")
	if err := os.WriteFile(part, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(part, now, now); err != nil {
		t.Fatal(err)
	}
	got, err := svc.reconcileStaging(ctx, now)
	if err != nil || got.AdoptedFiles != 1 {
		t.Fatalf("adopt result=%+v err=%v", got, err)
	}
	var size int64
	if err := st.db.QueryRowContext(ctx, `SELECT size_bytes FROM cloud_workspace_staging_chunks WHERE workspace_id = ? AND sha256 = ? AND chunk_index = 0`, ws.ID, sha).Scan(&size); err != nil || size != 3 {
		t.Fatalf("adopted reservation size=%d err=%v", size, err)
	}
	if err := os.WriteFile(part, []byte("abcd"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = svc.reconcileStaging(ctx, now.Add(time.Second))
	if err != nil || got.ResizedRows != 1 {
		t.Fatalf("resize result=%+v err=%v", got, err)
	}
	if err := st.db.QueryRowContext(ctx, `SELECT size_bytes FROM cloud_workspace_staging_chunks WHERE workspace_id = ? AND sha256 = ? AND chunk_index = 0`, ws.ID, sha).Scan(&size); err != nil || size != 4 {
		t.Fatalf("resized reservation size=%d err=%v", size, err)
	}
	if err := os.Remove(part); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-stagingMissingGrace - time.Second).Format(time.RFC3339)
	if _, err := st.db.ExecContext(ctx, `UPDATE cloud_workspace_staging_chunks SET updated_at = ? WHERE workspace_id = ? AND sha256 = ?`, old, ws.ID, sha); err != nil {
		t.Fatal(err)
	}
	got, err = svc.reconcileStaging(ctx, now.Add(2*time.Second))
	if err != nil || got.RemovedRows != 1 {
		t.Fatalf("remove result=%+v err=%v", got, err)
	}
}

func TestCollectMetricsTenantsAndVolume(t *testing.T) {
	svc, st, _ := newGCService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := svc.SaveTenantSettings(ctx, "t1", Settings{Mode: ModeAllUsers, Quota: 5}); err != nil {
		t.Fatal(err)
	}
	ws := seedLeasedWorkspace(t, st, now)
	if _, err := st.db.ExecContext(ctx, `UPDATE cloud_workspaces SET used_bytes = 42 WHERE id = ?`, ws.ID); err != nil {
		t.Fatal(err)
	}
	got := svc.CollectMetrics(ctx)
	if got.TenantsEnabled != 1 {
		t.Fatalf("tenants_enabled=%d want 1", got.TenantsEnabled)
	}
	if got.OpenLeases != 1 {
		t.Fatalf("open_leases=%d want 1", got.OpenLeases)
	}
	if got.UsedBytes != 42 {
		t.Fatalf("used_bytes=%d want 42", got.UsedBytes)
	}
	if got.VolumeFreeBytes <= 0 {
		t.Fatalf("volume_free_bytes=%d", got.VolumeFreeBytes)
	}
}

func TestStartHourlyGCIdempotentAndStop(t *testing.T) {
	svc, _, _ := newGCService(t)
	svc.StartHourlyGC()
	svc.StartHourlyGC()
	if svc.gcStop == nil {
		t.Fatal("gcStop should be set after start")
	}
	first := svc.gcStop
	svc.StartHourlyGC()
	if svc.gcStop != first {
		t.Fatal("second start must not replace the stopper")
	}
	svc.StopHourlyGC()
	if svc.gcStop != nil {
		t.Fatal("gcStop should be cleared after stop")
	}
	svc.StopHourlyGC()
}

func TestSweepRemovesOrphanWorkspaceDirs(t *testing.T) {
	svc, st, _ := newGCService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	live := seedLeasedWorkspace(t, st, now)

	// Live workspace directory: must survive the sweep.
	liveDir := filepath.Join(svc.Blobs.Root, "t1", "u1", live.ID, "objects")
	if err := os.MkdirAll(liveDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Orphan directory: a purge crashed between filesystem cleanup and the
	// metadata commit. Backdate it past the grace window.
	orphanDir := filepath.Join(svc.Blobs.Root, "t1", "u1", "cws_orphan", "objects")
	if err := os.MkdirAll(orphanDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphanDir, strings.Repeat("a", 64)+".enc"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(svc.Blobs.Root, "t1", "u1", "cws_orphan"), old, old); err != nil {
		t.Fatal(err)
	}
	// The key directory must never be mistaken for a tenant.
	if err := os.MkdirAll(svc.Blobs.KeyDir, 0o700); err != nil {
		t.Fatal(err)
	}

	result, err := svc.Sweep(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.RemovedOrphanDirs != 1 {
		t.Fatalf("removed_orphan_dirs=%d want 1 (result=%+v)", result.RemovedOrphanDirs, result)
	}
	if _, err := os.Stat(filepath.Join(svc.Blobs.Root, "t1", "u1", "cws_orphan")); !os.IsNotExist(err) {
		t.Fatalf("orphan dir still exists: %v", err)
	}
	if _, err := os.Stat(liveDir); err != nil {
		t.Fatalf("live workspace dir removed: %v", err)
	}
	if _, err := os.Stat(svc.Blobs.KeyDir); err != nil {
		t.Fatalf("key dir removed: %v", err)
	}
}

func TestSweepKeepsFreshOrphanDirWithinGrace(t *testing.T) {
	svc, _, _ := newGCService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	orphanDir := filepath.Join(svc.Blobs.Root, "t1", "u1", "cws_fresh")
	if err := os.MkdirAll(orphanDir, 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Sweep(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.RemovedOrphanDirs != 0 {
		t.Fatalf("removed_orphan_dirs=%d want 0 within grace", result.RemovedOrphanDirs)
	}
	if _, err := os.Stat(orphanDir); err != nil {
		t.Fatalf("fresh orphan dir removed during grace: %v", err)
	}
}
