package cloudworkspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

func TestCompleteObjectExistingObjectReturnsDurableSize(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insertTestMachine(t, st, "m1", "u1", "HOST-M1")
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "complete-existing", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := st.Acquire(ctx, acquireParams(ws.ID, "m1"), now)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	bs := &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), DB: st.db}
	plain := []byte("already finalized content")
	put, err := bs.Put(ctx, "t1", "u1", ws.ID, plain)
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{Workspaces: st, Blobs: bs, Now: func() time.Time { return now }}
	principal := auth.MachinePrincipal{TenantID: "t1", UserID: "u1", MachineID: "m1", FencingToken: lease.FencingToken}
	got, err := svc.CompleteObject(ctx, principal, ws.ID, put.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Existed || got.SizeBytes != int64(len(plain)) {
		t.Fatalf("complete result=%+v, want existed=true size=%d", got, len(plain))
	}
}

func TestShareWriterChunkUploadUsesOwnerBlobDir(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insertTestMachine(t, st, "m1", "u1", "HOST-M1")
	insertTestMachine(t, st, "m2", "u2", "HOST-M2")
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "共享写入", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	share, err := st.UpsertWorkspaceShare(ctx, "t1", "u1", ws.ID, SharePermissionWrite, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AcceptWorkspaceShare(ctx, "t1", "u2", share.Token, "", now); err != nil {
		t.Fatal(err)
	}
	lease, err := st.Acquire(ctx, AcquireParams{TenantID: "t1", UserID: "u2", WorkspaceID: ws.ID, MachineID: "m2"}, now)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	bs := &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), DB: st.db}
	svc := &Service{Workspaces: st, Blobs: bs, Now: func() time.Time { return now }}
	plain := []byte("shared writer chunk")
	sum := plaintextSHA256(plain)
	writer := auth.MachinePrincipal{TenantID: "t1", UserID: "u2", MachineID: "m2", FencingToken: lease.FencingToken}
	if err := svc.PutObjectChunk(ctx, writer, ws.ID, sum, 0, plain); err != nil {
		t.Fatal(err)
	}
	got, err := svc.CompleteObject(ctx, writer, ws.ID, sum)
	if err != nil || got.SHA256 != sum {
		t.Fatalf("complete=%+v err=%v", got, err)
	}
	owner := auth.MachinePrincipal{TenantID: "t1", UserID: "u1", MachineID: "m1"}
	body, err := svc.GetObject(ctx, owner, ws.ID, sum)
	if err != nil || string(body) != string(plain) {
		t.Fatalf("owner get=%q err=%v", body, err)
	}
	ownerPath, err := bs.ObjectPath("t1", "u1", ws.ID, sum)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ownerPath); err != nil {
		t.Fatalf("owner blob missing: %v", err)
	}
	recipientPath, err := bs.ObjectPath("t1", "u2", ws.ID, sum)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(recipientPath); !os.IsNotExist(err) {
		t.Fatalf("chunk landed in recipient blob dir err=%v", err)
	}
}
