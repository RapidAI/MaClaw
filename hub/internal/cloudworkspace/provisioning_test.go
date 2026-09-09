package cloudworkspace

import (
	"context"
	"testing"
	"time"
)

func TestWorkspaceTaskProvisionCommitAndCompensate(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	op, err := st.BeginWorkspaceTaskProvision(ctx, WorkspaceTaskProvisionParams{
		TenantID: "t1", UserID: "u1", Name: "编排任务", Quota: 2, TenantMaxTotalBytes: 1 << 30,
		DeviceTaskID: "local-1", Mode: "coding_dev", Tag: "cloud_workspace",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if op.State != ProvisionStateProvisioning || op.WorkspaceID == "" || op.CloudTaskID == "" {
		t.Fatalf("op=%+v", op)
	}
	ws, err := st.GetOwned(ctx, "t1", "u1", op.WorkspaceID)
	if err != nil || ws.Status != StatusActive {
		t.Fatalf("workspace=%+v err=%v", ws, err)
	}
	binding, err := st.GetTaskBinding(ctx, "t1", "u1", op.WorkspaceID)
	if err != nil || binding == nil {
		t.Fatalf("provisioning binding=%+v err=%v", binding, err)
	}
	if _, err := st.CompleteWorkspaceTaskProvision(ctx, "t1", "u1", op.OperationID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	ws, err = st.GetOwned(ctx, "t1", "u1", op.WorkspaceID)
	if err != nil || ws.Status != StatusActive {
		t.Fatalf("completed workspace=%+v err=%v", ws, err)
	}
	binding, err = st.GetTaskBinding(ctx, "t1", "u1", op.WorkspaceID)
	if err != nil || binding.CloudTaskID != op.CloudTaskID {
		t.Fatalf("binding=%+v err=%v", binding, err)
	}

	op2, err := st.BeginWorkspaceTaskProvision(ctx, WorkspaceTaskProvisionParams{
		TenantID: "t1", UserID: "u1", Name: "失败任务", Quota: 3, TenantMaxTotalBytes: 1 << 30,
	}, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	aborted, err := st.AbortWorkspaceTaskProvision(ctx, "t1", "u1", op2.OperationID, "local task write failed", now.Add(3*time.Second))
	if err != nil || aborted.State != ProvisionStateFailed || aborted.LastError == "" {
		t.Fatalf("aborted=%+v err=%v", aborted, err)
	}
	ws, err = st.GetOwned(ctx, "t1", "u1", op2.WorkspaceID)
	if err != nil || ws.Status != StatusDeleted {
		t.Fatalf("aborted workspace=%+v err=%v", ws, err)
	}
	if _, err := st.GetTaskBinding(ctx, "t1", "u1", op2.WorkspaceID); err != ErrNotFound {
		t.Fatalf("aborted binding should be removed, err=%v", err)
	}
	// Repeating compensation is idempotent and returns the durable terminal row.
	replay, err := st.AbortWorkspaceTaskProvision(ctx, "t1", "u1", op2.OperationID, "different reason", now.Add(4*time.Second))
	if err != nil || replay.LastError != aborted.LastError {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
}

func TestWorkspaceTaskProvisionKeyReplaysDurableOperation(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	params := WorkspaceTaskProvisionParams{
		TenantID: "t1", UserID: "u1", Name: "幂等编排", DeviceTaskID: "local-idem", Mode: "coding_dev",
		IdempotencyKey: "idem-provision", IdempotencyPayloadHash: "payload-a", Quota: 5,
	}
	first, err := st.BeginWorkspaceTaskProvision(ctx, params, now)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := st.BeginWorkspaceTaskProvision(ctx, params, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if replay.OperationID != first.OperationID || replay.WorkspaceID != first.WorkspaceID {
		t.Fatalf("replay=%+v first=%+v", replay, first)
	}
	params.IdempotencyPayloadHash = "payload-b"
	if _, err := st.BeginWorkspaceTaskProvision(ctx, params, now.Add(2*time.Minute)); err != ErrIdempotencyKeyReused {
		t.Fatalf("payload conflict err=%v", err)
	}
}

func TestReconcileStaleWorkspaceTaskProvisionSkipsLiveLease(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	op, err := st.BeginWorkspaceTaskProvision(ctx, WorkspaceTaskProvisionParams{TenantID: "t1", UserID: "u1", Name: "超时任务", Quota: 5}, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Acquire(ctx, AcquireParams{TenantID: "t1", UserID: "u1", WorkspaceID: op.WorkspaceID, MachineID: "m1", ClientInstanceID: "cwi-1"}, now); err != nil {
		t.Fatal(err)
	}
	if n, err := st.ReconcileStaleWorkspaceTaskProvisions(ctx, now, time.Minute); err != nil || n != 0 {
		t.Fatalf("live lease reconcile n=%d err=%v", n, err)
	}
	// Once the lease has expired, the next sweep can safely compensate it.
	if n, err := st.ReconcileStaleWorkspaceTaskProvisions(ctx, now.Add(2*time.Minute), time.Minute); err != nil || n != 1 {
		t.Fatalf("expired lease reconcile n=%d err=%v", n, err)
	}
	got, err := st.GetWorkspaceTaskProvision(ctx, "t1", "u1", op.OperationID)
	if err != nil || got.State != ProvisionStateFailed {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestWorkspaceTaskProvisionTransitionFencedToOtherClient(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	op, err := st.BeginWorkspaceTaskProvision(ctx, WorkspaceTaskProvisionParams{TenantID: "t1", UserID: "u1", Name: "跨进程", Quota: 5, ClientInstanceID: "cwi-owner"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CompleteWorkspaceTaskProvisionWithSession(ctx, "t1", "u1", op.OperationID, "m2", "cwi-other", 0, now.Add(time.Second)); err != ErrFenced {
		t.Fatalf("cross-client complete err=%v", err)
	}
	if _, err := st.AbortWorkspaceTaskProvisionWithSession(ctx, "t1", "u1", op.OperationID, "wrong client", "m2", "cwi-other", 0, now.Add(time.Second)); err != ErrFenced {
		t.Fatalf("cross-client abort err=%v", err)
	}
}

func TestWorkspaceTaskProvisionCompleteRequiresCurrentFencingToken(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	op, err := st.BeginWorkspaceTaskProvision(ctx, WorkspaceTaskProvisionParams{
		TenantID: "t1", UserID: "u1", Name: "提交屏障", Quota: 5, ClientInstanceID: "cwi-owner",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := st.Acquire(ctx, AcquireParams{
		TenantID: "t1", UserID: "u1", WorkspaceID: op.WorkspaceID, MachineID: "m1", ClientInstanceID: "cwi-owner",
	}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CompleteWorkspaceTaskProvisionWithSession(ctx, "t1", "u1", op.OperationID, "m1", "cwi-owner", 0, now.Add(2*time.Second)); err != ErrFenced {
		t.Fatalf("missing fencing token err=%v", err)
	}
	if _, err := st.CompleteWorkspaceTaskProvisionWithSession(ctx, "t1", "u1", op.OperationID, "m1", "cwi-owner", lease.FencingToken, now.Add(2*time.Second)); err != nil {
		t.Fatalf("current fencing token err=%v", err)
	}
}
