package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()

	provider, err := NewProvider(Config{
		DSN:               filepath.Join(t.TempDir(), "hub-test.db"),
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  4,
		MaxReadIdleConns:  2,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	t.Cleanup(func() {
		_ = provider.Close()
	})

	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	return NewStore(provider)
}

func TestAdminUserRepositoryRoundTrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	admin := &store.AdminUser{
		ID:           "adm_1",
		Username:     "admin",
		PasswordHash: "hash",
		Email:        "admin@example.com",
		Scope:        "global",
		Role:         "global_owner",
		Status:       "active",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := st.Admins.Create(ctx, admin); err != nil {
		t.Fatalf("create admin: %v", err)
	}

	got, err := st.Admins.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}
	if got == nil || got.Email != admin.Email || got.Scope != "global" || got.Role != "global_owner" {
		t.Fatalf("unexpected admin: %#v", got)
	}

	count, err := st.Admins.Count(ctx)
	if err != nil {
		t.Fatalf("count admins: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	if err := st.Admins.DeleteAll(ctx); err != nil {
		t.Fatalf("delete all admins: %v", err)
	}

	count, err = st.Admins.Count(ctx)
	if err != nil {
		t.Fatalf("count admins after delete: %v", err)
	}
	if count != 0 {
		t.Fatalf("count after delete = %d, want 0", count)
	}
}

func TestAdminUserRepositoryScopesUsernameAndEmail(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	admins := []*store.AdminUser{
		{ID: "adm_global", Username: "shared", PasswordHash: "hash", Email: "shared@example.com", Scope: "global", Role: "global_owner", Status: "active", CreatedAt: now, UpdatedAt: now},
		{ID: "adm_tenant_a", Username: "shared", PasswordHash: "hash-a", Email: "tenant@example.com", Scope: "tenant", Role: "tenant_admin", TenantID: "tenant_a", Status: "active", CreatedAt: now, UpdatedAt: now},
		{ID: "adm_tenant_b", Username: "shared", PasswordHash: "hash-b", Email: "tenant@example.com", Scope: "tenant", Role: "tenant_admin", TenantID: "tenant_b", Status: "active", CreatedAt: now, UpdatedAt: now},
	}
	for _, admin := range admins {
		if err := st.Admins.Create(ctx, admin); err != nil {
			t.Fatalf("create admin %s: %v", admin.ID, err)
		}
	}

	global, err := st.Admins.GetByUsername(ctx, "shared")
	if err != nil {
		t.Fatalf("get global admin: %v", err)
	}
	if global == nil || global.ID != "adm_global" {
		t.Fatalf("GetByUsername should resolve global admin, got %#v", global)
	}
	tenantA, err := st.Admins.GetByUsernameScoped(ctx, "shared", "tenant", "tenant_a")
	if err != nil {
		t.Fatalf("get tenant admin: %v", err)
	}
	if tenantA == nil || tenantA.ID != "adm_tenant_a" {
		t.Fatalf("unexpected tenant admin: %#v", tenantA)
	}
	if err := st.Admins.UpdateEmailScoped(ctx, "shared", "tenant", "tenant_a", "new-tenant@example.com", now.Add(time.Minute)); err != nil {
		t.Fatalf("update tenant email: %v", err)
	}
	tenantB, err := st.Admins.GetByUsernameScoped(ctx, "shared", "tenant", "tenant_b")
	if err != nil {
		t.Fatalf("get tenant b admin: %v", err)
	}
	if tenantB == nil || tenantB.Email != "tenant@example.com" {
		t.Fatalf("tenant_b email should be unchanged, got %#v", tenantB)
	}

	dup := &store.AdminUser{ID: "adm_tenant_a_dup", Username: "shared", PasswordHash: "hash", Email: "other@example.com", Scope: "tenant", Role: "tenant_admin", TenantID: "tenant_a", Status: "active", CreatedAt: now, UpdatedAt: now}
	if err := st.Admins.Create(ctx, dup); err == nil {
		t.Fatal("expected duplicate username in same tenant to fail")
	}
}

func TestAdminUsersLegacyUniqueSchemaMigratesToScopedIndexes(t *testing.T) {
	provider, err := NewProvider(Config{
		DSN:               filepath.Join(t.TempDir(), "hub-legacy-admin-users.db"),
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  4,
		MaxReadIdleConns:  2,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	if _, err := provider.Write.Exec(`CREATE TABLE admin_users (
		id TEXT PRIMARY KEY,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		email TEXT NOT NULL UNIQUE,
		status TEXT NOT NULL DEFAULT 'active',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`); err != nil {
		t.Fatalf("create legacy admin_users: %v", err)
	}
	if _, err := provider.Write.Exec(`INSERT INTO admin_users (id, username, password_hash, email, status, created_at, updated_at)
		VALUES ('adm_legacy', 'shared', 'hash', 'shared@example.com', 'active', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert legacy admin: %v", err)
	}
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	st := NewStore(provider)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	for _, admin := range []*store.AdminUser{
		{ID: "adm_tenant_a", Username: "shared", PasswordHash: "hash-a", Email: "tenant@example.com", Scope: "tenant", Role: "tenant_admin", TenantID: "tenant_a", Status: "active", CreatedAt: now, UpdatedAt: now},
		{ID: "adm_tenant_b", Username: "shared", PasswordHash: "hash-b", Email: "tenant@example.com", Scope: "tenant", Role: "tenant_admin", TenantID: "tenant_b", Status: "active", CreatedAt: now, UpdatedAt: now},
	} {
		if err := st.Admins.Create(ctx, admin); err != nil {
			t.Fatalf("create scoped admin %s after migration: %v", admin.ID, err)
		}
	}
	global, err := st.Admins.GetByUsername(ctx, "shared")
	if err != nil {
		t.Fatalf("get migrated global admin: %v", err)
	}
	if global == nil || global.ID != "adm_legacy" || global.Scope != "global" || global.TenantID != "" {
		t.Fatalf("unexpected migrated global admin: %#v", global)
	}
}

func TestTenantRepositoryEnsureDefaultAndRoundTrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	defaultTenant, err := st.Tenants.EnsureDefault(ctx)
	if err != nil {
		t.Fatalf("ensure default tenant: %v", err)
	}
	if defaultTenant == nil || defaultTenant.ID != store.DefaultTenantID || defaultTenant.Slug != "default" {
		t.Fatalf("unexpected default tenant: %#v", defaultTenant)
	}

	now := time.Now().UTC().Truncate(time.Second)
	tenant := &store.Tenant{
		ID:               "tenant_acme",
		Slug:             "acme",
		Name:             "Acme Corp",
		Status:           "active",
		PrimaryDomain:    "acme.com",
		SettingsJSON:     `{"work_mode":"approval"}`,
		CreatedByAdminID: "adm_1",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := st.Tenants.Create(ctx, tenant); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	got, err := st.Tenants.GetBySlug(ctx, "acme")
	if err != nil {
		t.Fatalf("get tenant by slug: %v", err)
	}
	if got == nil || got.ID != tenant.ID || got.PrimaryDomain != tenant.PrimaryDomain {
		t.Fatalf("unexpected tenant: %#v", got)
	}
}

func TestSystemSettingsRepositoryRoundTrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	if err := st.System.Set(ctx, "admin_initialized", `{"value":true}`); err != nil {
		t.Fatalf("set system setting: %v", err)
	}

	value, err := st.System.Get(ctx, "admin_initialized")
	if err != nil {
		t.Fatalf("get system setting: %v", err)
	}
	if value != `{"value":true}` {
		t.Fatalf("value = %q", value)
	}
}

func TestUserMachineAndSessionRepositoriesRoundTrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	user := &store.User{
		ID:               "u_1",
		TenantID:         "tenant_acme",
		Email:            "user@example.com",
		SN:               "SN-2026-000001",
		Status:           "active",
		EnrollmentStatus: "approved",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	gotUser, err := st.Users.GetByTenantEmail(ctx, user.TenantID, user.Email)
	if err != nil {
		t.Fatalf("get user by email: %v", err)
	}
	if gotUser == nil || gotUser.SN != user.SN || gotUser.TenantID != user.TenantID {
		t.Fatalf("unexpected user: %#v", gotUser)
	}

	machine := &store.Machine{
		ID:               "m_1",
		TenantID:         user.TenantID,
		UserID:           user.ID,
		Name:             "office-pc",
		Platform:         "windows",
		MachineTokenHash: "machine-hash",
		Status:           "offline",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := st.Machines.Create(ctx, machine); err != nil {
		t.Fatalf("create machine: %v", err)
	}
	if err := st.Machines.UpdateStatus(ctx, machine.ID, "online"); err != nil {
		t.Fatalf("update machine status: %v", err)
	}
	if err := st.Machines.UpdateHeartbeat(ctx, machine.ID, now.Add(time.Minute)); err != nil {
		t.Fatalf("update heartbeat: %v", err)
	}

	gotMachine, err := st.Machines.GetByID(ctx, machine.ID)
	if err != nil {
		t.Fatalf("get machine: %v", err)
	}
	if gotMachine == nil || gotMachine.Status != "online" || gotMachine.TenantID != user.TenantID {
		t.Fatalf("unexpected machine: %#v", gotMachine)
	}

	machineList, err := st.Machines.ListByUserID(ctx, user.ID)
	if err != nil {
		t.Fatalf("list machines: %v", err)
	}
	if len(machineList) != 1 {
		t.Fatalf("machine list len = %d, want 1", len(machineList))
	}

	session := &store.Session{
		ID:          "s_1",
		TenantID:    user.TenantID,
		MachineID:   machine.ID,
		UserID:      user.ID,
		Tool:        "claude",
		Title:       "payment-service",
		ProjectPath: "D:/workprj/payment-service",
		Status:      "starting",
		SummaryJSON: `{}`,
		PreviewText: "",
		OutputSeq:   0,
		HostOnline:  true,
		StartedAt:   now,
		UpdatedAt:   now,
	}
	if err := st.Sessions.Create(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := st.Sessions.UpdateSummary(ctx, session.ID, `{"status":"busy"}`, "busy", now.Add(time.Minute)); err != nil {
		t.Fatalf("update session summary: %v", err)
	}
	if err := st.Sessions.UpdatePreview(ctx, session.ID, "Running tests", 3, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("update session preview: %v", err)
	}
	exitCode := 0
	if err := st.Sessions.Close(ctx, session.ID, &exitCode, now.Add(3*time.Minute), "exited"); err != nil {
		t.Fatalf("close session: %v", err)
	}
}

func TestMachineTenantUserDeletesDoNotCrossTenants(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	for _, machine := range []*store.Machine{
		{ID: "m_a", TenantID: "tenant_a", UserID: "same-user", Name: "a", Platform: "windows", MachineTokenHash: "hash-a", Status: "offline", CreatedAt: now, UpdatedAt: now},
		{ID: "m_b", TenantID: "tenant_b", UserID: "same-user", Name: "b", Platform: "windows", MachineTokenHash: "hash-b", Status: "offline", CreatedAt: now, UpdatedAt: now},
	} {
		if err := st.Machines.Create(ctx, machine); err != nil {
			t.Fatalf("create machine %s: %v", machine.ID, err)
		}
	}

	deleted, err := st.Machines.ForceDeleteByTenantUserID(ctx, "tenant_a", "same-user")
	if err != nil {
		t.Fatalf("force delete tenant user: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	if got, err := st.Machines.GetByID(ctx, "m_b"); err != nil || got == nil || got.TenantID != "tenant_b" {
		t.Fatalf("tenant_b machine = %#v err=%v, want preserved", got, err)
	}
}
func TestUsersAllowSameEmailAcrossTenants(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	users := []*store.User{
		{ID: "u_tenant_a", TenantID: "tenant_a", Email: "shared@example.com", SN: "SN-SHARED-A", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "u_tenant_b", TenantID: "tenant_b", Email: "shared@example.com", SN: "SN-SHARED-B", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
	}
	for _, user := range users {
		if err := st.Users.Create(ctx, user); err != nil {
			t.Fatalf("create user %s: %v", user.ID, err)
		}
	}

	gotA, err := st.Users.GetByTenantEmail(ctx, "tenant_a", "shared@example.com")
	if err != nil {
		t.Fatalf("get tenant a user: %v", err)
	}
	gotB, err := st.Users.GetByTenantEmail(ctx, "tenant_b", "shared@example.com")
	if err != nil {
		t.Fatalf("get tenant b user: %v", err)
	}
	if gotA == nil || gotA.ID != "u_tenant_a" || gotB == nil || gotB.ID != "u_tenant_b" {
		t.Fatalf("unexpected tenant users: a=%#v b=%#v", gotA, gotB)
	}
}

func TestSessionRepositoryRuntimeUpdatesAreTenantScoped(t *testing.T) {
	st := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	ctxA := store.WithTenant(context.Background(), "tenant_a")
	ctxB := store.WithTenant(context.Background(), "tenant_b")
	sessionA := &store.Session{
		ID:          "shared-session",
		TenantID:    "tenant_a",
		MachineID:   "machine-a",
		UserID:      "user-a",
		Tool:        "claude",
		Status:      "running",
		SummaryJSON: `{}`,
		HostOnline:  true,
		StartedAt:   now,
		UpdatedAt:   now,
	}
	if err := st.Sessions.Create(ctxA, sessionA); err != nil {
		t.Fatalf("create tenant A session: %v", err)
	}

	if err := st.Sessions.UpdateSummary(ctxB, sessionA.ID, `{"status":"leaked"}`, "leaked", now.Add(time.Minute)); err != nil {
		t.Fatalf("cross tenant update summary: %v", err)
	}
	if err := st.Sessions.UpdatePreview(ctxB, sessionA.ID, "leaked preview", 9, now.Add(time.Minute)); err != nil {
		t.Fatalf("cross tenant update preview: %v", err)
	}
	if err := st.Sessions.UpdateHostOnline(ctxB, sessionA.ID, false, now.Add(time.Minute)); err != nil {
		t.Fatalf("cross tenant update host: %v", err)
	}
	exitCode := 1
	if err := st.Sessions.Close(ctxB, sessionA.ID, &exitCode, now.Add(time.Minute), "leaked"); err != nil {
		t.Fatalf("cross tenant close: %v", err)
	}

	row := st.Sessions.(*sessionRepo).db.QueryRowContext(context.Background(), `SELECT status, summary_json, preview_text, output_seq, host_online, ended_at FROM sessions WHERE id = ? AND tenant_id = ?`, sessionA.ID, "tenant_a")
	var status, summaryJSON, previewText string
	var outputSeq, hostOnline int
	var endedAt any
	if err := row.Scan(&status, &summaryJSON, &previewText, &outputSeq, &hostOnline, &endedAt); err != nil {
		t.Fatalf("scan tenant A session: %v", err)
	}
	if status != "running" || summaryJSON != `{}` || previewText != "" || outputSeq != 0 || hostOnline != 1 || endedAt != nil {
		t.Fatalf("tenant B mutated tenant A session: status=%q summary=%q preview=%q seq=%d host=%d ended=%v", status, summaryJSON, previewText, outputSeq, hostOnline, endedAt)
	}

	if err := st.Sessions.UpdateSummary(ctxA, sessionA.ID, `{"status":"busy"}`, "busy", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("tenant A update summary: %v", err)
	}
	row = st.Sessions.(*sessionRepo).db.QueryRowContext(context.Background(), `SELECT status, summary_json FROM sessions WHERE id = ? AND tenant_id = ?`, sessionA.ID, "tenant_a")
	if err := row.Scan(&status, &summaryJSON); err != nil {
		t.Fatalf("scan tenant A session after update: %v", err)
	}
	if status != "busy" || summaryJSON != `{"status":"busy"}` {
		t.Fatalf("tenant A update did not apply: status=%q summary=%q", status, summaryJSON)
	}
}

func TestEmailBlocklistAllowSameEmailAcrossTenants(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	items := []*store.EmailBlockItem{
		{ID: "blk_a", TenantID: "tenant_a", Email: "blocked@example.com", Reason: "a", CreatedAt: now, UpdatedAt: now},
		{ID: "blk_b", TenantID: "tenant_b", Email: "blocked@example.com", Reason: "b", CreatedAt: now, UpdatedAt: now},
	}
	for _, item := range items {
		if err := st.EmailBlocks.Create(ctx, item); err != nil {
			t.Fatalf("create block %s: %v", item.ID, err)
		}
	}

	gotA, err := st.EmailBlocks.GetByTenantEmail(ctx, "tenant_a", "blocked@example.com")
	if err != nil {
		t.Fatalf("get tenant a block: %v", err)
	}
	gotB, err := st.EmailBlocks.GetByTenantEmail(ctx, "tenant_b", "blocked@example.com")
	if err != nil {
		t.Fatalf("get tenant b block: %v", err)
	}
	if gotA == nil || gotA.Reason != "a" || gotB == nil || gotB.Reason != "b" {
		t.Fatalf("unexpected tenant blocks: a=%#v b=%#v", gotA, gotB)
	}
}

func TestViewerAndLoginTokenRepositoriesRoundTrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	user := &store.User{
		ID:               "u_2",
		TenantID:         "tenant_acme",
		Email:            "viewer@example.com",
		SN:               "SN-2026-000002",
		Status:           "active",
		EnrollmentStatus: "approved",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	viewerToken := &store.ViewerToken{
		ID:        "vt_1",
		TenantID:  user.TenantID,
		UserID:    user.ID,
		TokenHash: "viewer-hash",
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now,
	}
	if err := st.ViewerTokens.Create(ctx, viewerToken); err != nil {
		t.Fatalf("create viewer token: %v", err)
	}

	gotViewer, err := st.ViewerTokens.GetByTokenHash(ctx, viewerToken.TokenHash)
	if err != nil {
		t.Fatalf("get viewer token: %v", err)
	}
	if gotViewer == nil || gotViewer.UserID != user.ID || gotViewer.TenantID != user.TenantID {
		t.Fatalf("unexpected viewer token: %#v", gotViewer)
	}

	loginToken := &store.LoginToken{
		ID:        "lt_1",
		TenantID:  user.TenantID,
		Email:     user.Email,
		TokenHash: "login-hash",
		Purpose:   "login",
		ExpiresAt: now.Add(15 * time.Minute),
		CreatedAt: now,
	}
	if err := st.LoginTokens.Create(ctx, loginToken); err != nil {
		t.Fatalf("create login token: %v", err)
	}

	gotLogin, err := st.LoginTokens.GetByTokenHash(ctx, loginToken.TokenHash)
	if err != nil {
		t.Fatalf("get login token: %v", err)
	}
	if gotLogin == nil || gotLogin.Email != user.Email || gotLogin.TenantID != user.TenantID {
		t.Fatalf("unexpected login token: %#v", gotLogin)
	}

	consumedAt := now.Add(5 * time.Minute)
	if err := st.LoginTokens.Consume(ctx, loginToken.ID, consumedAt); err != nil {
		t.Fatalf("consume login token: %v", err)
	}

	consumedLogin, err := st.LoginTokens.GetByTokenHash(ctx, loginToken.TokenHash)
	if err != nil {
		t.Fatalf("reload login token: %v", err)
	}
	if consumedLogin == nil || consumedLogin.ConsumedAt == nil {
		t.Fatalf("login token was not marked consumed: %#v", consumedLogin)
	}
}

func TestAdminAuditRepositoryListFilters(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	logs := []*store.AdminAuditLog{
		{ID: "audit-1", TenantID: "tenant_a", AdminUserID: "adm-1", Action: "security.group.create", PayloadJSON: `{"group_id":"dept-a","name":"Dept A"}`, CreatedAt: time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)},
		{ID: "audit-2", TenantID: "tenant_a", AdminUserID: "adm-2", Action: "capability.recommendation.create", PayloadJSON: `{"capability_ref":"skill-a","scope":{"type":"global"}}`, CreatedAt: time.Date(2026, 5, 16, 8, 0, 0, 0, time.UTC)},
		{ID: "audit-3", TenantID: "tenant_b", AdminUserID: "adm-3", Action: "security.group.delete", PayloadJSON: `{"group_id":"dept-b"}`, CreatedAt: time.Date(2026, 5, 17, 8, 0, 0, 0, time.UTC)},
	}
	for _, log := range logs {
		if err := st.AdminAudit.Create(ctx, log); err != nil {
			t.Fatalf("create audit log %s: %v", log.ID, err)
		}
	}

	items, err := st.AdminAudit.List(ctx, store.AdminAuditLogFilter{Action: "security.group.create", Query: "dept-a", CreatedFrom: time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC), CreatedTo: time.Date(2026, 5, 15, 23, 59, 59, 0, time.UTC), Limit: 10})
	if err != nil {
		t.Fatalf("list audit logs: %v", err)
	}
	if len(items) != 1 || items[0].ID != "audit-1" {
		t.Fatalf("filtered items = %+v, want audit-1", items)
	}

	items, err = st.AdminAudit.List(ctx, store.AdminAuditLogFilter{Query: "dept", Limit: 2})
	if err != nil {
		t.Fatalf("list audit logs by query: %v", err)
	}
	if len(items) != 2 || items[0].ID != "audit-3" || items[1].ID != "audit-1" {
		t.Fatalf("query/order/limit items = %+v", items)
	}

	items, err = st.AdminAudit.List(ctx, store.AdminAuditLogFilter{TenantID: "tenant_a", TenantScoped: true, Query: "dept", Limit: 10})
	if err != nil {
		t.Fatalf("list tenant audit logs: %v", err)
	}
	if len(items) != 1 || items[0].ID != "audit-1" || items[0].TenantID != "tenant_a" {
		t.Fatalf("tenant filtered audit items = %+v, want tenant_a/audit-1", items)
	}
}
