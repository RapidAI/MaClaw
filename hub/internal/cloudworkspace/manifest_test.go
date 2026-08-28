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
