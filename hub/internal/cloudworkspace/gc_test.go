package cloudworkspace

import (
	"context"
	"os"
	"path/filepath"
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
