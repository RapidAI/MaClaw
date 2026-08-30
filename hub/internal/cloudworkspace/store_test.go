package cloudworkspace

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

func insertTestMachine(t *testing.T, st *Store, id, userID, hostname string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := st.db.Exec(`INSERT INTO machines (id, tenant_id, user_id, name, platform, hostname, machine_token_hash, status, created_at, updated_at)
		VALUES (?, 't1', ?, ?, 'windows', ?, 'hash', 'online', ?, ?)`,
		id, userID, id, hostname, now, now,
	); err != nil {
		t.Fatalf("insert machine %s: %v", id, err)
	}
}

func acquireParams(workspaceID, machineID string) AcquireParams {
	return AcquireParams{TenantID: "t1", UserID: "u1", WorkspaceID: workspaceID, MachineID: machineID}
}

func newTestWorkspaceStore(t *testing.T) (*Store, *store.Store) {
	t.Helper()
	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN:               filepath.Join(t.TempDir(), "cws.db"),
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  4,
		MaxReadIdleConns:  2,
		MaxWriteOpenConns: 4,
		MaxWriteIdleConns: 2,
	})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	return NewStore(provider.Write), sqlite.NewStore(provider)
}

func TestStoreCreateQuotaAndNameNorm(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	first, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "Foo", Quota: 2, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != StatusActive || first.UsedBytes != 0 {
		t.Fatalf("created=%+v", first)
	}
	_, err = st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "foo", Quota: 2, TenantMaxTotalBytes: 1 << 30}, now)
	if err != ErrNameTaken {
		t.Fatalf("dup name err=%v", err)
	}
	_, err = st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "Bar", Quota: 1, TenantMaxTotalBytes: 1 << 30}, now)
	if err != ErrQuota {
		t.Fatalf("quota err=%v", err)
	}
	second, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "", Quota: 2, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	if second.Name != "工作区 1" {
		t.Fatalf("default name=%q", second.Name)
	}
}

func TestStoreSoftDeleteFreesQuotaAndRestore(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "A", Quota: 1, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SoftDelete(ctx, "t1", "u1", "m1", ws.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	other, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "B", Quota: 1, TenantMaxTotalBytes: 1 << 30}, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.Restore(ctx, "t1", "u1", ws.ID, 1, now.Add(3*time.Second))
	if err != ErrQuota {
		t.Fatalf("restore over quota err=%v", err)
	}
	if _, err := st.SoftDelete(ctx, "t1", "u1", "m1", other.ID, now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	restored, err := st.Restore(ctx, "t1", "u1", ws.ID, 1, now.Add(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if restored.Status != StatusActive || restored.DeletedAt != "" {
		t.Fatalf("restored=%+v", restored)
	}
}

func TestStoreRestoreWindowExpired(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "A", Quota: 1, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SoftDelete(ctx, "t1", "u1", "m1", ws.ID, now); err != nil {
		t.Fatal(err)
	}
	_, err = st.Restore(ctx, "t1", "u1", ws.ID, 1, now.Add(RestoreWindow))
	if err != ErrRestoreWindow {
		t.Fatalf("expired err=%v", err)
	}
}

func TestStoreTenantDiskIncludesDeleted(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "A", Quota: 5, TenantMaxTotalBytes: 10}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE cloud_workspaces SET used_bytes = 10 WHERE id = ?`, ws.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SoftDelete(ctx, "t1", "u1", "m1", ws.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	_, err = st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "B", Quota: 5, TenantMaxTotalBytes: 10}, now.Add(2*time.Second))
	if err != ErrTenantDisk {
		t.Fatalf("tenant disk err=%v", err)
	}
}

func TestStoreConcurrentCreateAtQuota(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	const n = 2
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := st.Create(ctx, CreateParams{
				TenantID:            "t1",
				UserID:              "u1",
				Name:                "ws-" + string(rune('a'+i)),
				Quota:               1,
				TenantMaxTotalBytes: 1 << 30,
			}, now)
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	var ok, quota int
	for err := range errs {
		switch err {
		case nil:
			ok++
		case ErrQuota:
			quota++
		default:
			t.Fatalf("unexpected err=%v", err)
		}
	}
	if ok != 1 || quota != 1 {
		t.Fatalf("ok=%d quota=%d", ok, quota)
	}
}

func TestStoreListOverQuotaUsersAndPreview(t *testing.T) {
	st, hub := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	user := &store.User{ID: "u1", TenantID: "t1", Email: "u1@x.com", SN: "SN-u1", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	if err := hub.Users.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"A", "B", "C"} {
		if _, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: name, Quota: 10, TenantMaxTotalBytes: 1 << 30}, now); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.ListOverQuotaUsers(ctx, "t1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SN != "SN-u1" || got[0].Used != 3 || got[0].Quota != 2 {
		t.Fatalf("over quota=%+v", got)
	}
	svc := &Service{Workspaces: st}
	preview := svc.BuildPreview(ctx, "t1", Settings{Mode: ModeOff, Quota: 2})
	if len(preview.OverQuotaUsers) != 1 || preview.OverQuotaUsers[0].SN != "SN-u1" {
		t.Fatalf("preview=%+v", preview)
	}
}

func TestEntitlementForDisabledStillListsOwnRows(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "A", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SoftDelete(ctx, "t1", "u1", "m1", ws.ID, now.Add(time.Second)); err != nil {
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
	if ent.Enabled {
		t.Fatal("mode off should disable")
	}
	if ent.Reason != ReasonNotGranted {
		t.Fatalf("reason=%q want %q", ent.Reason, ReasonNotGranted)
	}
	if ent.Quota != 5 || ent.Used != 0 || len(ent.Deleted) != 1 {
		t.Fatalf("ent=%+v", ent)
	}
	if ent.Deleted[0].PurgeAfter == "" {
		t.Fatal("purge_after required on deleted rows")
	}
}
