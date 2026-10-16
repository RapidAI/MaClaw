package cloudworkspace

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestReplaceManifestUpdatesUsageAndRefCount(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insertTestMachine(t, st, "m1", "u1", "HOST-M1")
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "A", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Acquire(ctx, acquireParams(ws.ID, "m1"), now); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	t.Setenv(masterKeyEnv, "")
	bs := &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), DB: st.db}
	a := []byte("alpha")
	b := []byte("beta-file")
	putA, err := bs.Put(ctx, "t1", "u1", ws.ID, a)
	if err != nil {
		t.Fatal(err)
	}
	putB, err := bs.Put(ctx, "t1", "u1", ws.ID, b)
	if err != nil {
		t.Fatal(err)
	}

	first, err := st.ReplaceManifest(ctx, "t1", "u1", ws.ID, "m1", "", []ManifestEntry{
		{Path: "a.txt", SHA256: putA.SHA256, Size: putA.SizeBytes},
		{Path: "dir/b.txt", SHA256: putB.SHA256, Size: putB.SizeBytes},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision == "" || len(first.Entries) != 2 {
		t.Fatalf("first=%+v", first)
	}
	got, err := st.GetOwned(ctx, "t1", "u1", ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UsedBytes != putA.SizeBytes+putB.SizeBytes || got.FileCount != 2 || got.ManifestRevision != first.Revision {
		t.Fatalf("usage=%+v", got)
	}
	var refA, refB int
	if err := st.db.QueryRow(`SELECT ref_count FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ?`, ws.ID, putA.SHA256).Scan(&refA); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRow(`SELECT ref_count FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ?`, ws.ID, putB.SHA256).Scan(&refB); err != nil {
		t.Fatal(err)
	}
	if refA != 1 || refB != 1 {
		t.Fatalf("ref A=%d B=%d", refA, refB)
	}

	if _, err := st.ReplaceManifest(ctx, "t1", "u1", ws.ID, "m1", "stale", first.Entries, now); err != ErrRevisionConflict {
		t.Fatalf("stale err=%v", err)
	}

	same, err := st.ReplaceManifest(ctx, "t1", "u1", ws.ID, "m1", first.Revision, first.Entries, now)
	if err != nil {
		t.Fatal(err)
	}
	if same.Revision != first.Revision {
		t.Fatalf("unchanged tree must keep revision, got %q want %q", same.Revision, first.Revision)
	}

	second, err := st.ReplaceManifest(ctx, "t1", "u1", ws.ID, "m1", first.Revision, []ManifestEntry{
		{Path: "a.txt", SHA256: putA.SHA256, Size: putA.SizeBytes},
		{Path: "a2.txt", SHA256: putA.SHA256, Size: putA.SizeBytes},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	got, err = st.GetOwned(ctx, "t1", "u1", ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UsedBytes != putA.SizeBytes*2 || got.FileCount != 2 {
		t.Fatalf("after replace usage=%+v", got)
	}
	if err := st.db.QueryRow(`SELECT ref_count FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ?`, ws.ID, putA.SHA256).Scan(&refA); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRow(`SELECT ref_count FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ?`, ws.ID, putB.SHA256).Scan(&refB); err != nil {
		t.Fatal(err)
	}
	if refA != 2 || refB != 0 {
		t.Fatalf("ref after A=%d B=%d", refA, refB)
	}
	if second.Revision == first.Revision {
		t.Fatal("revision should change")
	}
}

func TestManifestRejectsOldSessionAfterTakeover(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "Fence", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	first, err := st.Acquire(ctx, AcquireParams{TenantID: "t1", UserID: "u1", WorkspaceID: ws.ID, MachineID: "m1", ClientInstanceID: "cwi-1"}, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.Acquire(ctx, AcquireParams{TenantID: "t1", UserID: "u1", WorkspaceID: ws.ID, MachineID: "m2", ClientInstanceID: "cwi-2", Force: true}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if second.FencingToken <= first.FencingToken {
		t.Fatalf("tokens first=%d second=%d", first.FencingToken, second.FencingToken)
	}
	_, err = st.ReplaceManifestWithSession(ctx, "t1", "u1", ws.ID, "m1", "cwi-1", first.FencingToken, "", nil, now.Add(2*time.Second))
	if err != ErrFenced {
		t.Fatalf("stale manifest err=%v", err)
	}
}

func TestPrepareObjectPutDoesNotChangeUsedBytes(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insertTestMachine(t, st, "m1", "u1", "HOST-M1")
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "A", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Acquire(ctx, acquireParams(ws.ID, "m1"), now); err != nil {
		t.Fatal(err)
	}
	sum := plaintextSHA256([]byte("hello"))
	existed, err := st.PrepareObjectPut(ctx, "t1", "u1", ws.ID, "m1", sum, 5, 1<<20, 1<<30, now)
	if err != nil || existed {
		t.Fatalf("prepare existed=%v err=%v", existed, err)
	}
	got, err := st.GetOwned(ctx, "t1", "u1", ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UsedBytes != 0 || got.FileCount != 0 {
		t.Fatalf("used_bytes updated on object admit: %+v", got)
	}
}

func TestManifestDeltaAndSnapshotRestore(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insertTestMachine(t, st, "m1", "u1", "HOST-M1")
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "delta", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Acquire(ctx, acquireParams(ws.ID, "m1"), now); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	bs := &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), DB: st.db}
	firstObj, err := bs.Put(ctx, "t1", "u1", ws.ID, []byte("first"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := st.ReplaceManifest(ctx, "t1", "u1", ws.ID, "m1", "", []ManifestEntry{{Path: "a.txt", SHA256: firstObj.SHA256, Size: firstObj.SizeBytes}}, now)
	if err != nil {
		t.Fatal(err)
	}
	var snapshotID string
	if err := st.db.QueryRow(`SELECT snapshot_id FROM cloud_workspace_snapshots WHERE workspace_id = ? ORDER BY created_at DESC LIMIT 1`, ws.ID).Scan(&snapshotID); err != nil {
		t.Fatal(err)
	}
	secondObj, err := bs.Put(ctx, "t1", "u1", ws.ID, []byte("second"))
	if err != nil {
		t.Fatal(err)
	}
	patched, err := st.ApplyManifestDeltaWithSession(ctx, "t1", "u1", ws.ID, "m1", "", 0, ManifestDelta{IfMatchRevision: first.Revision, Puts: []ManifestEntry{{Path: "b.txt", SHA256: secondObj.SHA256, Size: secondObj.SizeBytes}}, Deletes: []string{"a.txt"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(patched.Entries) != 1 || patched.Entries[0].Path != "b.txt" {
		t.Fatalf("patched=%+v", patched)
	}
	restored, err := st.RestoreSnapshotWithSession(ctx, "t1", "u1", ws.ID, "m1", "", 0, snapshotID, patched.Revision, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.Entries) != 1 || restored.Entries[0].Path != "a.txt" || restored.Revision == patched.Revision {
		t.Fatalf("restored=%+v", restored)
	}
}

func TestIdempotencyLedgerReplayAndPayloadConflict(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	rec, err := st.BeginIdempotency(ctx, "t1", "u1", "ws1", "cwi_1", "k1", "hash-a", now)
	if err != nil || rec != nil {
		t.Fatalf("reserve rec=%+v err=%v", rec, err)
	}
	if err := st.FinishIdempotency(ctx, "t1", "u1", "ws1", "k1", "hash-a", 200, []byte(`{"ok":true}`), now); err != nil {
		t.Fatal(err)
	}
	replay, err := st.BeginIdempotency(ctx, "t1", "u1", "ws1", "cwi_1", "k1", "hash-a", now)
	if err != nil || replay == nil || !replay.Completed || string(replay.Response) != `{"ok":true}` {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	if _, err := st.BeginIdempotency(ctx, "t1", "u1", "ws1", "cwi_1", "k1", "hash-b", now); err != ErrIdempotencyKeyReused {
		t.Fatalf("payload conflict err=%v", err)
	}
}

func TestIdempotencyLedgerDoesNotReclaimStalePending(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	created := time.Now().UTC().Add(-10 * time.Minute)
	if _, err := st.db.ExecContext(ctx, `INSERT INTO cloud_workspace_idempotency (tenant_id, user_id, workspace_id, client_instance_id, idempotency_key, payload_hash, status, status_code, response_json, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, 'pending', 0, NULL, ?, ?)`, "t1", "u1", "ws1", "cwi-old", "stale-key", "hash-a", created.Format(time.RFC3339), created.Add(24*time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginIdempotency(ctx, "t1", "u1", "ws1", "cwi-new", "stale-key", "hash-a", created.Add(10*time.Minute)); err != ErrIdempotencyInProgress {
		t.Fatalf("stale pending err=%v, want ErrIdempotencyInProgress", err)
	}
	var owner string
	if err := st.db.QueryRowContext(ctx, `SELECT client_instance_id FROM cloud_workspace_idempotency WHERE tenant_id = ? AND user_id = ? AND workspace_id = ? AND idempotency_key = ?`, "t1", "u1", "ws1", "stale-key").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if owner != "cwi-old" {
		t.Fatalf("pending owner changed to %q", owner)
	}
}

func TestIdempotencyLedgerPurgesOnlyCommittedRows(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	old := now.Add(-48 * time.Hour)
	for _, row := range []struct {
		key, status string
	}{
		{key: "pending-old", status: "pending"},
		{key: "committed-old", status: "committed"},
	} {
		if _, err := st.db.ExecContext(ctx, `INSERT INTO cloud_workspace_idempotency (tenant_id, user_id, workspace_id, client_instance_id, idempotency_key, payload_hash, status, status_code, response_json, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, 200, ?, ?, ?)`, "t1", "u1", "ws1", "cwi", row.key, "hash", row.status, []byte(`{"ok":true}`), old.Format(time.RFC3339), old.Add(time.Hour).Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.PurgeExpiredIdempotency(ctx, now); err != nil {
		t.Fatal(err)
	}
	var pending, committed int
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cloud_workspace_idempotency WHERE idempotency_key = 'pending-old'`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cloud_workspace_idempotency WHERE idempotency_key = 'committed-old'`).Scan(&committed); err != nil {
		t.Fatal(err)
	}
	if pending != 1 || committed != 0 {
		t.Fatalf("purge counts pending=%d committed=%d", pending, committed)
	}
}

func TestPrepareObjectPutEnforcesWorkspaceAndTenant(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insertTestMachine(t, st, "m1", "u1", "HOST-M1")
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "A", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Acquire(ctx, acquireParams(ws.ID, "m1"), now); err != nil {
		t.Fatal(err)
	}
	sum := plaintextSHA256([]byte("hello"))
	if _, err := st.PrepareObjectPut(ctx, "t1", "u1", ws.ID, "m1", sum, 100, 50, 1<<30, now); err != ErrWorkspaceSize {
		t.Fatalf("size err=%v", err)
	}
	if _, err := st.PrepareObjectPut(ctx, "t1", "u1", ws.ID, "m1", sum, 100, 1<<20, 50, now); err != ErrTenantDisk {
		t.Fatalf("tenant err=%v", err)
	}
	if _, err := st.PrepareObjectPut(ctx, "t1", "u1", ws.ID, "m1", sum, 100, 1<<20, 1<<30, now); err != nil {
		t.Fatal(err)
	}
}

func TestRequireLease(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insertTestMachine(t, st, "m1", "u1", "HOST-M1")
	insertTestMachine(t, st, "m2", "u1", "HOST-M2")
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "A", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.RequireLease(ctx, "t1", "u1", ws.ID, "m1", now); err != ErrLeaseRequired {
		t.Fatalf("no lease err=%v", err)
	}
	if _, err := st.Acquire(ctx, acquireParams(ws.ID, "m1"), now); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RequireLease(ctx, "t1", "u1", ws.ID, "m1", now); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RequireLease(ctx, "t1", "u1", ws.ID, "m2", now); err != ErrLeaseRequired {
		t.Fatalf("other machine err=%v", err)
	}
}
