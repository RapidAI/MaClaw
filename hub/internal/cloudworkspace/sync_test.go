package cloudworkspace

import (
	"context"
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
