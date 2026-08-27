package cloudworkspace

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestStoreConcurrentAcquireOnlyOneGranted(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insertTestMachine(t, st, "m1", "u1", "HOST-M1")
	insertTestMachine(t, st, "m2", "u1", "HOST-M2")
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "A", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	const n = 2
	results := make(chan error, n)
	var wg sync.WaitGroup
	for _, mid := range []string{"m1", "m2"} {
		wg.Add(1)
		go func(mid string) {
			defer wg.Done()
			_, err := st.Acquire(ctx, acquireParams(ws.ID, mid), now)
			results <- err
		}(mid)
	}
	wg.Wait()
	close(results)
	var ok, inUse int
	for err := range results {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrInUse):
			inUse++
		default:
			t.Fatalf("unexpected err=%v", err)
		}
	}
	if ok != 1 || inUse != 1 {
		t.Fatalf("ok=%d inUse=%d", ok, inUse)
	}
}

func TestStoreExpiredLeaseStolenByOtherMachine(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	insertTestMachine(t, st, "m1", "u1", "HOST-M1")
	insertTestMachine(t, st, "m2", "u1", "HOST-M2")
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "A", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	first, err := st.Acquire(ctx, acquireParams(ws.ID, "m1"), now)
	if err != nil {
		t.Fatal(err)
	}
	if first.Acquired != AcquiredGranted {
		t.Fatalf("first=%+v", first)
	}
	tooSoon, err := st.Acquire(ctx, acquireParams(ws.ID, "m2"), now.Add(LeaseTTL-time.Second))
	if !errors.Is(err, ErrInUse) || tooSoon != nil {
		t.Fatalf("unexpired steal err=%v out=%+v", err, tooSoon)
	}
	stolen, err := st.Acquire(ctx, acquireParams(ws.ID, "m2"), now.Add(LeaseTTL))
	if err != nil {
		t.Fatal(err)
	}
	if stolen.Acquired != AcquiredGranted || stolen.LeaseID == first.LeaseID {
		t.Fatalf("stolen=%+v first=%+v", stolen, first)
	}
	var stolenBy, releasedAt string
	if err := st.db.QueryRow(`SELECT stolen_by, released_at FROM cloud_workspace_leases WHERE id = ?`, first.LeaseID).Scan(&stolenBy, &releasedAt); err != nil {
		t.Fatal(err)
	}
	if stolenBy != "m2" || releasedAt == "" {
		t.Fatalf("old row stolen_by=%q released_at=%q", stolenBy, releasedAt)
	}
}

func TestStoreSameMachineAcquireRenews(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	insertTestMachine(t, st, "m1", "u1", "HOST-M1")
	insertTestMachine(t, st, "m2", "u1", "HOST-M2")
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "A", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	first, err := st.Acquire(ctx, acquireParams(ws.ID, "m1"), now)
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := st.Acquire(ctx, acquireParams(ws.ID, "m1"), now.Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if renewed.Acquired != AcquiredRenewed || renewed.LeaseID != first.LeaseID {
		t.Fatalf("renewed=%+v first=%+v", renewed, first)
	}
	wantExpiry := now.Add(30*time.Second + LeaseTTL).Format(time.RFC3339)
	if renewed.ExpiresAt != wantExpiry {
		t.Fatalf("expires_at=%q want %q", renewed.ExpiresAt, wantExpiry)
	}
	blocked, err := st.Acquire(ctx, acquireParams(ws.ID, "m2"), now.Add(30*time.Second))
	if !errors.Is(err, ErrInUse) || blocked != nil {
		t.Fatalf("other machine err=%v out=%+v", err, blocked)
	}
}

func TestStoreHeartbeat409AfterSteal(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	insertTestMachine(t, st, "m1", "u1", "HOST-M1")
	insertTestMachine(t, st, "m2", "u1", "HOST-M2")
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "A", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	first, err := st.Acquire(ctx, acquireParams(ws.ID, "m1"), now)
	if err != nil {
		t.Fatal(err)
	}
	forced, err := st.Acquire(ctx, AcquireParams{TenantID: "t1", UserID: "u1", WorkspaceID: ws.ID, MachineID: "m2", Force: true}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if forced.Acquired != AcquiredGranted || forced.LeaseID == first.LeaseID {
		t.Fatalf("forced=%+v first=%+v", forced, first)
	}
	_, err = st.Heartbeat(ctx, "t1", "u1", ws.ID, first.LeaseID, "m1", now.Add(2*time.Second))
	if !errors.Is(err, ErrInUse) {
		t.Fatalf("old heartbeat err=%v", err)
	}
	hb, err := st.Heartbeat(ctx, "t1", "u1", ws.ID, forced.LeaseID, "m2", now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if hb.LeaseID != forced.LeaseID {
		t.Fatalf("new heartbeat=%+v", hb)
	}
}

func TestStoreSoftDeleteBlockedByOtherUnexpiredLease(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insertTestMachine(t, st, "m1", "u1", "HOST-M1")
	insertTestMachine(t, st, "m2", "u1", "HOST-M2")
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "A", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Acquire(ctx, acquireParams(ws.ID, "m2"), now); err != nil {
		t.Fatal(err)
	}
	_, err = st.SoftDelete(ctx, "t1", "u1", "m1", ws.ID, now)
	if !errors.Is(err, ErrInUse) {
		t.Fatalf("delete while other holds err=%v", err)
	}
	if _, err := st.SoftDelete(ctx, "t1", "u1", "m2", ws.ID, now); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetOwned(ctx, "t1", "u1", ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusDeleted {
		t.Fatalf("status=%q", got.Status)
	}
	leases, err := st.ListActiveLeases(ctx, "t1", "u1")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := leases[ws.ID]; ok {
		t.Fatal("self lease should be released on delete")
	}
}

func TestEntitlementLeaseIsSelf(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	insertTestMachine(t, st, "m1", "u1", "HOST-M1")
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "A", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Acquire(ctx, acquireParams(ws.ID, "m1"), now); err != nil {
		t.Fatal(err)
	}
	svc := &Service{
		System:     memorySettings{"tenant:t1:cloud_workspace": `{"mode":"all_users","quota":5}`},
		Users:      &fakeUsers{byID: map[string]*store.User{"u1": testUser()}},
		Workspaces: st,
		Now:        func() time.Time { return now },
	}
	ent, err := svc.EntitlementFor(ctx, auth.MachinePrincipal{TenantID: "t1", UserID: "u1", MachineID: "m1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ent.Workspaces) != 1 || ent.Workspaces[0].Lease == nil {
		t.Fatalf("ent=%+v", ent)
	}
	lease := ent.Workspaces[0].Lease
	if !lease.Held || !lease.IsSelf || lease.MachineID != "m1" || lease.MachineName != "HOST-M1" {
		t.Fatalf("lease=%+v", lease)
	}
}
