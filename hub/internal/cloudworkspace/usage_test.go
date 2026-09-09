package cloudworkspace

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func insertUsageObject(t *testing.T, st *Store, workspaceID, sha string, size int64, state string, now time.Time) {
	t.Helper()
	if _, err := st.db.Exec(`INSERT INTO cloud_workspace_objects (
		workspace_id, sha256, size_bytes, plain_size_bytes, stored_size_bytes,
		compression, compression_level, encryption_version, ref_count, created_at, object_state
	) VALUES (?, ?, ?, ?, ?, 'none', 0, 'aes-gcm-v1', 0, ?, ?)`,
		workspaceID, sha, size, size, size/2, now.UTC().Format(time.RFC3339), state); err != nil {
		t.Fatal(err)
	}
}

func TestRetainedUsageClassifiesUniqueObjectsAndStaging(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "usage", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	currentSHA := "1111111111111111111111111111111111111111111111111111111111111111"
	snapshotSHA := "2222222222222222222222222222222222222222222222222222222222222222"
	orphanSHA := "3333333333333333333333333333333333333333333333333333333333333333"
	wholeStagingSHA := "4444444444444444444444444444444444444444444444444444444444444444"
	chunkSHA := "5555555555555555555555555555555555555555555555555555555555555555"
	quarantineSHA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	insertUsageObject(t, st, ws.ID, currentSHA, 10, "ready", now)
	insertUsageObject(t, st, ws.ID, snapshotSHA, 20, "ready", now)
	insertUsageObject(t, st, ws.ID, orphanSHA, 30, "deleting", now)
	insertUsageObject(t, st, ws.ID, quarantineSHA, 4, "quarantine", now)
	insertUsageObject(t, st, ws.ID, wholeStagingSHA, 40, "staging", now)
	if _, err := st.db.Exec(`INSERT INTO cloud_workspace_manifest_entries (workspace_id, path, sha256, size_bytes) VALUES
		(?, 'a.txt', ?, 10), (?, 'copy.txt', ?, 10)`, ws.ID, currentSHA, ws.ID, currentSHA); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`INSERT INTO cloud_workspace_snapshots (snapshot_id, workspace_id, manifest_revision, manifest_hash, created_by_session, created_at, retention_class)
		VALUES ('snap-1', ?, 'rev-1', 'hash-1', 'session-1', ?, 'standard')`, ws.ID, now.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`INSERT INTO cloud_workspace_snapshot_entries (snapshot_id, path, sha256, size_bytes) VALUES ('snap-1', 'old.txt', ?, 20)`, snapshotSHA); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`INSERT INTO cloud_workspace_staging_chunks (workspace_id, sha256, chunk_index, size_bytes, updated_at) VALUES
		(?, ?, 0, 5, ?), (?, ?, 1, 6, ?)`, ws.ID, chunkSHA, now.Format(time.RFC3339), ws.ID, chunkSHA, now.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	usage, err := st.UsageForWorkspace(ctx, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if usage.LogicalBytes != 20 || usage.CurrentObjectBytes != 10 {
		t.Fatalf("logical/current=%d/%d want 20/10: %+v", usage.LogicalBytes, usage.CurrentObjectBytes, usage)
	}
	if usage.SnapshotRetainedBytes != 20 || usage.UnreferencedRetainedBytes != 34 {
		t.Fatalf("snapshot/unreferenced=%d/%d want 20/34: %+v", usage.SnapshotRetainedBytes, usage.UnreferencedRetainedBytes, usage)
	}
	if usage.StagingBytes != 51 || usage.RetainedBytes != 115 {
		t.Fatalf("staging/retained=%d/%d want 51/115: %+v", usage.StagingBytes, usage.RetainedBytes, usage)
	}
	tenantUsage, err := st.TenantRetainedUsage(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if tenantUsage != usage {
		t.Fatalf("tenant usage=%+v want %+v", tenantUsage, usage)
	}
}

func TestPrepareObjectPutChargesSnapshotAndSoftDeletedRetainedBytes(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insertTestMachine(t, st, "m1", "u1", "host")
	first, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "first", Quota: 5, TenantMaxTotalBytes: 100}, now)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := st.Acquire(ctx, acquireParams(first.ID, "m1"), now)
	if err != nil {
		t.Fatal(err)
	}
	snapshotSHA := "6666666666666666666666666666666666666666666666666666666666666666"
	insertUsageObject(t, st, first.ID, snapshotSHA, 80, "ready", now)
	if _, err := st.db.Exec(`INSERT INTO cloud_workspace_snapshots (snapshot_id, workspace_id, manifest_revision, manifest_hash, created_by_session, created_at, retention_class)
		VALUES ('snap-retained', ?, 'rev-old', 'hash-old', 'session-1', ?, 'standard')`, first.ID, now.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`INSERT INTO cloud_workspace_snapshot_entries (snapshot_id, path, sha256, size_bytes) VALUES ('snap-retained', 'old.bin', ?, 80)`, snapshotSHA); err != nil {
		t.Fatal(err)
	}
	newSHA := "7777777777777777777777777777777777777777777777777777777777777777"
	if _, err := st.PrepareObjectPutWithSession(ctx, "t1", "u1", first.ID, "m1", "", lease.FencingToken, newSHA, 21, 100, 1000, now); err != ErrWorkspaceSize {
		t.Fatalf("workspace admission err=%v want %v", err, ErrWorkspaceSize)
	}
	if _, err := st.SoftDeleteWithSessionAndToken(ctx, "t1", "u1", "m1", "", lease.FencingToken, first.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	second, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "second", Quota: 5, TenantMaxTotalBytes: 1000}, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	secondLease, err := st.Acquire(ctx, acquireParams(second.ID, "m1"), now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PrepareObjectPutWithSession(ctx, "t1", "u1", second.ID, "m1", "", secondLease.FencingToken, newSHA, 21, 1000, 100, now.Add(2*time.Second)); err != ErrTenantDisk {
		t.Fatalf("tenant admission err=%v want %v", err, ErrTenantDisk)
	}
}

func TestReservationRetryUsesDeltaAndCanShrinkWhileOverQuota(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insertTestMachine(t, st, "m1", "u1", "host")
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "retry", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := st.Acquire(ctx, acquireParams(ws.ID, "m1"), now)
	if err != nil {
		t.Fatal(err)
	}
	sha := "8888888888888888888888888888888888888888888888888888888888888888"
	if _, err := st.PrepareObjectPutWithSession(ctx, "t1", "u1", ws.ID, "m1", "", lease.FencingToken, sha, 90, 100, 1000, now); err != nil {
		t.Fatal(err)
	}
	// Retrying the same durable reservation must not add another 90 bytes.
	if _, err := st.PrepareObjectPutWithSession(ctx, "t1", "u1", ws.ID, "m1", "", lease.FencingToken, sha, 90, 100, 1000, now.Add(time.Second)); err != nil {
		t.Fatalf("same-size retry: %v", err)
	}
	// A smaller retry is cleanup and remains legal even if the configured cap
	// has since been reduced below current retained usage.
	if _, err := st.PrepareObjectPutWithSession(ctx, "t1", "u1", ws.ID, "m1", "", lease.FencingToken, sha, 70, 50, 50, now.Add(2*time.Second)); err != nil {
		t.Fatalf("shrinking retry: %v", err)
	}
	usage, err := st.UsageForWorkspace(ctx, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if usage.StagingBytes != 70 || usage.RetainedBytes != 70 {
		t.Fatalf("usage after shrink=%+v", usage)
	}
}

func TestPrepareObjectPutDoesNotResurrectDeletingObject(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insertTestMachine(t, st, "m1", "u1", "host")
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "deleting", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := st.Acquire(ctx, acquireParams(ws.ID, "m1"), now)
	if err != nil {
		t.Fatal(err)
	}
	sha := "9999999999999999999999999999999999999999999999999999999999999999"
	insertUsageObject(t, st, ws.ID, sha, 5, "deleting", now)
	if _, err := st.PrepareObjectPutWithSession(ctx, "t1", "u1", ws.ID, "m1", "", lease.FencingToken, sha, 5, 100, 1000, now); err != ErrObjectDeleting {
		t.Fatalf("prepare deleting object err=%v, want %v", err, ErrObjectDeleting)
	}
	var state string
	if err := st.db.QueryRowContext(ctx, `SELECT object_state FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ?`, ws.ID, sha).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "deleting" {
		t.Fatalf("object state=%q, want deleting", state)
	}
}

func TestEntitlementReportsLogicalAndRetainedUsageWithoutChangingUsedBytes(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "entitlement", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	sha := "9999999999999999999999999999999999999999999999999999999999999999"
	insertUsageObject(t, st, ws.ID, sha, 12, "ready", now)
	if _, err := st.db.Exec(`INSERT INTO cloud_workspace_manifest_entries (workspace_id, path, sha256, size_bytes) VALUES (?, 'a.txt', ?, 12)`, ws.ID, sha); err != nil {
		t.Fatal(err)
	}
	// used_bytes is a compatibility cache. The new fields are calculated from
	// the manifest/object roots and therefore remain correct during repair.
	if _, err := st.db.Exec(`UPDATE cloud_workspaces SET used_bytes = 7 WHERE id = ?`, ws.ID); err != nil {
		t.Fatal(err)
	}
	svc := &Service{
		System:     memorySettings{},
		Users:      &fakeUsers{byID: map[string]*store.User{"u1": testUser()}},
		Workspaces: st,
	}
	ent, err := svc.EntitlementFor(ctx, auth.MachinePrincipal{TenantID: "t1", UserID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ent.Workspaces) != 1 {
		t.Fatalf("workspaces=%+v", ent.Workspaces)
	}
	item := ent.Workspaces[0]
	if item.UsedBytes != 7 || item.LogicalBytes != 12 || item.RetainedBytes != 12 {
		t.Fatalf("workspace usage=%+v", item)
	}
	if ent.LogicalBytes != 12 || ent.RetainedBytes != 12 || ent.TenantMaxTotalBytes == 0 {
		t.Fatalf("entitlement totals=%+v", ent)
	}
}
