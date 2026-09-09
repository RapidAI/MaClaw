package cloudworkspace

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

func TestAdmitBandwidthAccumulatesAndRejectsOverUserLimit(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := st.AdmitBandwidth(ctx, "t1", "u1", 100, 0, 150, 0, now); err != nil {
		t.Fatalf("first admit: %v", err)
	}
	err := st.AdmitBandwidth(ctx, "t1", "u1", 60, 0, 150, 0, now)
	var limitErr *BandwidthLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("second admit err=%v, want BandwidthLimitError", err)
	}
	if !errors.Is(err, ErrBandwidthLimit) {
		t.Fatalf("errors.Is mismatch: %v", err)
	}
	if limitErr.Scope != "user" || limitErr.Direction != "up" || limitErr.Limit != 150 || limitErr.Used != 100 || limitErr.Delta != 60 {
		t.Fatalf("limit detail=%+v", limitErr)
	}
	if limitErr.RetryAfterSeconds < 1 || limitErr.RetryAfterSeconds > 3600 {
		t.Fatalf("retry_after=%d out of window range", limitErr.RetryAfterSeconds)
	}
	up, down, err := st.BandwidthWindowUsage(ctx, "t1", now)
	if err != nil {
		t.Fatal(err)
	}
	if up != 100 || down != 0 {
		t.Fatalf("window usage up/down=%d/%d want 100/0 (rejected admit must not charge)", up, down)
	}
}

func TestAdmitBandwidthTenantAggregateAcrossUsers(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := st.AdmitBandwidth(ctx, "t1", "u1", 100, 0, 0, 150, now); err != nil {
		t.Fatalf("u1 admit: %v", err)
	}
	err := st.AdmitBandwidth(ctx, "t1", "u2", 60, 0, 0, 150, now)
	var limitErr *BandwidthLimitError
	if !errors.As(err, &limitErr) || limitErr.Scope != "tenant" {
		t.Fatalf("u2 admit err=%v, want tenant BandwidthLimitError", err)
	}
	// A different tenant is a separate accounting scope.
	if err := st.AdmitBandwidth(ctx, "t2", "u9", 60, 0, 0, 150, now); err != nil {
		t.Fatalf("other tenant admit: %v", err)
	}
}

func TestAdmitBandwidthWindowRolloverResetsCounters(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(bandwidthWindow).Add(10 * time.Minute)

	if err := st.AdmitBandwidth(ctx, "t1", "u1", 140, 0, 150, 0, now); err != nil {
		t.Fatalf("first window admit: %v", err)
	}
	next := now.Add(bandwidthWindow)
	if err := st.AdmitBandwidth(ctx, "t1", "u1", 140, 0, 150, 0, next); err != nil {
		t.Fatalf("next window admit must reset counters: %v", err)
	}
	up, _, err := st.BandwidthWindowUsage(ctx, "t1", next)
	if err != nil {
		t.Fatal(err)
	}
	if up != 140 {
		t.Fatalf("next window usage=%d want 140", up)
	}
}

func TestAdmitBandwidthZeroLimitsDisableAccounting(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < 3; i++ {
		if err := st.AdmitBandwidth(ctx, "t1", "u1", 1<<30, 1<<30, 0, 0, now); err != nil {
			t.Fatalf("unlimited admit %d: %v", i, err)
		}
	}
	var rows int
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cloud_workspace_bandwidth_usage`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("unlimited path wrote %d accounting rows, want 0", rows)
	}
}

func TestAdmitBandwidthPrunesExpiredWindows(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	oldWindow := now.Add(-72 * time.Hour).Truncate(bandwidthWindow).Format(time.RFC3339)
	if _, err := st.db.Exec(`INSERT INTO cloud_workspace_bandwidth_usage (tenant_id, user_id, window_start, bytes_up, bytes_down) VALUES ('t1', 'u1', ?, 5, 5)`, oldWindow); err != nil {
		t.Fatal(err)
	}
	if err := st.AdmitBandwidth(ctx, "t1", "u1", 10, 0, 100, 0, now); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cloud_workspace_bandwidth_usage WHERE window_start = ?`, oldWindow).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expired window rows=%d, want pruned", count)
	}
}

func TestAdmitBandwidthConcurrentAdmissionsNeverExceedLimit(t *testing.T) {
	// Production runs one write connection (see withImmediate); mirror that so
	// BEGIN IMMEDIATE transactions queue on the pool instead of colliding.
	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN:               filepath.Join(t.TempDir(), "cws.db"),
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  4,
		MaxReadIdleConns:  2,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	st := NewStore(provider.Write)
	ctx := context.Background()
	now := time.Now().UTC()

	const limit = 100
	const workers = 16
	var wg sync.WaitGroup
	var mu sync.Mutex
	succeeded := 0
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := st.AdmitBandwidth(ctx, "t1", "u1", 10, 0, limit, 0, now); err == nil {
				mu.Lock()
				succeeded++
				mu.Unlock()
			} else if !errors.Is(err, ErrBandwidthLimit) {
				t.Errorf("unexpected admit error: %v", err)
			}
		}()
	}
	wg.Wait()
	if succeeded != limit/10 {
		t.Fatalf("succeeded=%d want exactly %d serialized admissions", succeeded, limit/10)
	}
	up, _, err := st.BandwidthWindowUsage(ctx, "t1", now)
	if err != nil {
		t.Fatal(err)
	}
	if up != limit {
		t.Fatalf("charged=%d want exactly %d", up, limit)
	}
}

func TestAdmitBandwidthRequiresUserAndStore(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := st.AdmitBandwidth(ctx, "t1", "", 10, 0, 100, 0, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty user err=%v, want ErrNotFound", err)
	}
	var nilStore *Store
	if err := nilStore.AdmitBandwidth(ctx, "t1", "u1", 10, 0, 100, 0, now); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil store err=%v, want ErrUnavailable", err)
	}
}

func TestServiceAdmitBandwidthShortCircuitsWhenUnconfigured(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	svc := &Service{System: memorySettings{}, Workspaces: st}
	principal := auth.MachinePrincipal{TenantID: "t1", UserID: "u1", MachineID: "m1"}
	// Default settings carry zero limits: the hot path must not touch the DB.
	if err := svc.admitBandwidth(context.Background(), principal, 1<<30, 1<<30); err != nil {
		t.Fatalf("default settings admit: %v", err)
	}
	var rows int
	if err := st.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM cloud_workspace_bandwidth_usage`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("default settings wrote %d accounting rows", rows)
	}
}

func TestServiceAdmitBandwidthUsesTenantSettings(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	settings := memorySettings{}
	svc := &Service{System: settings, Workspaces: st}
	if _, err := svc.SaveTenantSettings(context.Background(), "t1", Settings{
		Mode:                      ModeAllUsers,
		Quota:                     5,
		BandwidthUserBytesPerHour: 100,
	}); err != nil {
		t.Fatal(err)
	}
	principal := auth.MachinePrincipal{TenantID: "t1", UserID: "u1", MachineID: "m1"}
	if err := svc.admitBandwidth(context.Background(), principal, 90, 0); err != nil {
		t.Fatalf("under limit: %v", err)
	}
	err := svc.admitBandwidth(context.Background(), principal, 20, 0)
	if !errors.Is(err, ErrBandwidthLimit) {
		t.Fatalf("over limit err=%v, want ErrBandwidthLimit", err)
	}
}

func TestClampBandwidthLimitKeepsZeroAsUnlimited(t *testing.T) {
	if got := clampBandwidthLimit(0); got != 0 {
		t.Fatalf("zero clamp=%d want 0 (unlimited)", got)
	}
	if got := clampBandwidthLimit(-5); got != 0 {
		t.Fatalf("negative clamp=%d want 0", got)
	}
	if got := clampBandwidthLimit(maxBandwidthBytesPerHour + 1); got != maxBandwidthBytesPerHour {
		t.Fatalf("upper clamp=%d want %d", got, maxBandwidthBytesPerHour)
	}
	// Settings round-trip must preserve the disabled default instead of
	// clamping it to a non-zero floor like the byte-size fields.
	filled := fillSettingsDefaults(Settings{Mode: ModeAllUsers, Quota: 5})
	if filled.BandwidthUserBytesPerHour != 0 || filled.BandwidthTenantBytesPerHour != 0 {
		t.Fatalf("defaults bandwidth=%d/%d want 0/0", filled.BandwidthUserBytesPerHour, filled.BandwidthTenantBytesPerHour)
	}
	prepared, err := prepareForWrite(Settings{Mode: ModeAllUsers, Quota: 5, BandwidthUserBytesPerHour: 100, BandwidthTenantBytesPerHour: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.BandwidthUserBytesPerHour != 100 || prepared.BandwidthTenantBytesPerHour != 1000 {
		t.Fatalf("prepared bandwidth=%d/%d", prepared.BandwidthUserBytesPerHour, prepared.BandwidthTenantBytesPerHour)
	}
}

func TestBandwidthWindowUsageTenantScopedAndGlobal(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := st.AdmitBandwidth(ctx, "t1", "u0", 10, 5, 1<<40, 0, now); err != nil {
		t.Fatal(err)
	}
	if err := st.AdmitBandwidth(ctx, "t1", "u1", 10, 5, 1<<40, 0, now); err != nil {
		t.Fatal(err)
	}
	if err := st.AdmitBandwidth(ctx, "t2", "u2", 10, 5, 1<<40, 0, now); err != nil {
		t.Fatal(err)
	}
	up, down, err := st.BandwidthWindowUsage(ctx, "t1", now)
	if err != nil {
		t.Fatal(err)
	}
	if up != 20 || down != 10 {
		t.Fatalf("tenant window=%d/%d want 20/10", up, down)
	}
	up, down, err = st.BandwidthWindowUsage(ctx, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if up != 30 || down != 15 {
		t.Fatalf("global window=%d/%d want 30/15", up, down)
	}
}
