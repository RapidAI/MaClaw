package cloudworkspace

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

func TestTaskBindingIsUniqueAndVersioned(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insertTestMachine(t, st, "m1", "u1", "host")
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "binding", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	first, err := st.UpsertTaskBinding(ctx, "t1", "u1", ws.ID, "", "dev-1", "Task", "coding", "cloud_workspace:"+ws.ID, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.CloudTaskID == "" || first.Version != 1 {
		t.Fatalf("first=%+v", first)
	}
	if _, err := st.UpsertTaskBinding(ctx, "t1", "u1", ws.ID, "other", "dev-2", "Task", "coding", "", 0, now); err != ErrRevisionConflict {
		t.Fatalf("different cloud task id err=%v", err)
	}
	lease, err := st.Acquire(ctx, acquireParams(ws.ID, "m1"), now)
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{Workspaces: st, Blobs: &BlobStore{Root: t.TempDir(), KeyDir: t.TempDir(), DB: st.db}}
	principal := auth.MachinePrincipal{TenantID: "t1", UserID: "u1", MachineID: "m1", FencingToken: lease.FencingToken}
	if _, err := svc.PutSidecarWithRevision(ctx, principal, ws.ID, SidecarTask, "", []byte(`{"name":"Task","mode":"coding","tag":"cloud_workspace:`+ws.ID+`"}`)); err != nil {
		t.Fatal(err)
	}
	binding, err := st.GetTaskBinding(ctx, "t1", "u1", ws.ID)
	if err != nil || binding == nil {
		t.Fatalf("binding=%+v err=%v", binding, err)
	}
	if binding.Version != 2 {
		t.Fatalf("sidecar commit must advance binding atomically, binding=%+v", binding)
	}

	// A sidecar carrying a stale binding version must fail the same transaction
	// that would update its bytes. The existing sidecar and binding therefore
	// remain unchanged (no half-committed task identity).
	raw, err := svc.Blobs.GetSidecar(ctx, "t1", "u1", ws.ID, SidecarTask)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PutSidecarWithRevision(ctx, principal, ws.ID, SidecarTask, sidecarRevision(raw), []byte(`{"name":"new","mode":"coding","tag":"cloud_workspace:`+ws.ID+`","binding_version":1}`)); err != ErrRevisionConflict {
		t.Fatalf("stale binding version err=%v", err)
	}
	after, err := st.GetTaskBinding(ctx, "t1", "u1", ws.ID)
	if err != nil || after.Version != binding.Version || after.Name != binding.Name {
		t.Fatalf("binding changed after failed sidecar commit: before=%+v after=%+v err=%v", binding, after, err)
	}

	if _, err := st.SoftDelete(ctx, "t1", "u1", "m1", ws.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetTaskBinding(ctx, "t1", "u1", ws.ID); err != ErrNotFound {
		t.Fatalf("deleted workspace binding should be hidden, err=%v", err)
	}
}
