package app

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/config"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

type tenantAdminRouteReconciliationSyncer struct {
	mu    sync.Mutex
	calls []tenantAdminRouteReconciliationCall
}

type appTestSettings struct {
	values map[string]string
}

func (s *appTestSettings) Set(_ context.Context, key, value string) error {
	s.values[key] = value
	return nil
}

func (s *appTestSettings) Get(_ context.Context, key string) (string, error) {
	return s.values[key], nil
}

type tenantAdminRouteReconciliationCall struct {
	email    string
	tenantID string
}

func (s *tenantAdminRouteReconciliationSyncer) SyncTenantAdminRoute(_ context.Context, email, tenantID string, _ ...string) error {
	s.mu.Lock()
	s.calls = append(s.calls, tenantAdminRouteReconciliationCall{email: email, tenantID: tenantID})
	s.mu.Unlock()
	return nil
}

func TestReconcileTenantAdminRoutesSyncsOnlyActiveTenantAdmins(t *testing.T) {
	provider, err := sqlite.NewProvider(sqlite.Config{DSN: filepath.Join(t.TempDir(), "hub.db"), WAL: true, BusyTimeoutMS: 5000, MaxReadOpenConns: 2, MaxReadIdleConns: 1, MaxWriteOpenConns: 1, MaxWriteIdleConns: 1})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	st := sqlite.NewStore(provider)
	admins := auth.NewAdminService(st.Admins, st.System, st.AdminAudit)
	now := time.Now().UTC()

	for _, tenant := range []*store.Tenant{
		{ID: "tenant_active", Slug: "active", Name: "Active", Status: "active", CreatedAt: now, UpdatedAt: now},
		{ID: "tenant_inactive", Slug: "inactive", Name: "Inactive", Status: "inactive", CreatedAt: now, UpdatedAt: now},
	} {
		if err := st.Tenants.Create(context.Background(), tenant); err != nil {
			t.Fatalf("create tenant %s: %v", tenant.ID, err)
		}
	}
	if _, err := admins.CreateTenantAdmin(context.Background(), "tenant_active", "active-admin", "StrongPassword123!", "Active@Example.com", "", "tenant_owner"); err != nil {
		t.Fatalf("create active tenant admin: %v", err)
	}
	if _, err := admins.CreateTenantAdmin(context.Background(), "tenant_inactive", "inactive-admin", "StrongPassword123!", "inactive@example.com", "", "tenant_owner"); err != nil {
		t.Fatalf("create inactive tenant admin: %v", err)
	}
	syncer := &tenantAdminRouteReconciliationSyncer{}

	synced, failed, err := reconcileTenantAdminRoutes(context.Background(), admins, syncer)
	if err != nil {
		t.Fatalf("reconcile tenant admin routes: %v", err)
	}
	if synced != 1 || failed != 0 {
		t.Fatalf("reconcile counts synced=%d failed=%d", synced, failed)
	}
	if len(syncer.calls) != 1 || syncer.calls[0].tenantID != "tenant_active" || syncer.calls[0].email != "active@example.com" {
		t.Fatalf("route sync calls=%#v", syncer.calls)
	}
}

func TestTenantAdminRouteSyncReadyRequiresRegisteredCenterIdentity(t *testing.T) {
	settings := &appTestSettings{values: map[string]string{}}
	cfg := config.Default()
	centerSvc := center.NewService(cfg, settings)
	if tenantAdminRouteSyncReady(context.Background(), centerSvc) {
		t.Fatal("unregistered center should not be route-sync ready")
	}
	settings.values["center_registration"] = `{"registered":true,"hub_id":"hub_test","hub_secret":"secret"}`
	if !tenantAdminRouteSyncReady(context.Background(), centerSvc) {
		t.Fatal("registered center should be route-sync ready")
	}
	settings.values["center_registration"] = `{"pending_confirmation":true,"hub_id":"hub_test","hub_secret":"secret"}`
	if tenantAdminRouteSyncReady(context.Background(), centerSvc) {
		t.Fatal("pending-confirmation center should not be route-sync ready")
	}
	settings.values["center_registration"] = `{"registered":true,"disabled":true,"hub_id":"hub_test","hub_secret":"secret"}`
	if tenantAdminRouteSyncReady(context.Background(), centerSvc) {
		t.Fatal("disabled center should not be route-sync ready")
	}
}
