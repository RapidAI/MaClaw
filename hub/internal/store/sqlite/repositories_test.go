package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
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

func TestUserRepositoryIdentityCannotMoveToAnotherUser(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for _, user := range []*store.User{
		{ID: "user_email", TenantID: "tenant_a", Email: "buyer@example.com", SN: "SN-user-email", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "user_other", TenantID: "tenant_a", Email: "other@example.com", SN: "SN-user-other", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
	} {
		if err := st.Users.Create(ctx, user); err != nil {
			t.Fatalf("create user %s: %v", user.ID, err)
		}
	}
	if err := st.Users.UpsertIdentity(ctx, &store.UserIdentity{TenantID: "tenant_a", UserID: "user_email", Type: "phone", Value: "19900001111", Verified: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("bind phone: %v", err)
	}
	if err := st.Users.UpsertIdentity(ctx, &store.UserIdentity{TenantID: "tenant_a", UserID: "user_other", Type: "phone", Value: "19900001111", Verified: true, CreatedAt: now, UpdatedAt: now}); err == nil {
		t.Fatal("expected identity conflict when binding same phone to another user")
	}
	got, err := st.Users.GetByTenantIdentity(ctx, "tenant_a", "phone", "19900001111")
	if err != nil {
		t.Fatalf("lookup phone identity: %v", err)
	}
	if got == nil || got.Email != "buyer@example.com" {
		t.Fatalf("phone identity moved unexpectedly: %#v", got)
	}
}

func TestUserRepositoryCreateDoesNotLeaveUserWhenAccountIdentityConflicts(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	caseUser := &store.User{ID: "user_case", TenantID: "tenant_a", Email: "CaseUser@Example.com", SN: "SN-user-case", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	if err := st.Users.Create(ctx, caseUser); err != nil {
		t.Fatalf("create case user: %v", err)
	}
	if caseUser.Email != "caseuser@example.com" {
		t.Fatalf("created user struct email = %q, want normalized lowercase", caseUser.Email)
	}
	gotCase, err := st.Users.GetByID(ctx, "user_case")
	if err != nil {
		t.Fatalf("lookup case user: %v", err)
	}
	if gotCase == nil || gotCase.Email != "caseuser@example.com" {
		t.Fatalf("case user email = %#v, want normalized lowercase", gotCase)
	}
	err = st.Users.Create(ctx, &store.User{ID: "user_duplicate_case", TenantID: "tenant_a", Email: "caseuser@example.com", SN: "SN-duplicate-case", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now})
	if err == nil {
		t.Fatal("expected case-insensitive primary account conflict")
	}
	duplicateCase, err := st.Users.GetByID(ctx, "user_duplicate_case")
	if err != nil {
		t.Fatalf("lookup duplicate case user: %v", err)
	}
	if duplicateCase != nil {
		t.Fatalf("case-insensitive conflict left duplicate user row: %#v", duplicateCase)
	}

	primaryUser := &store.User{ID: "user_primary", TenantID: "tenant_a", Email: "primary@example.com", SN: "SN-user-primary", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	if err := st.Users.Create(ctx, primaryUser); err != nil {
		t.Fatalf("create primary user: %v", err)
	}
	err = st.Users.Create(ctx, &store.User{ID: "user_duplicate_primary", TenantID: "tenant_a", Email: "primary@example.com", SN: "SN-duplicate-primary", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now})
	if err == nil {
		t.Fatal("expected primary account conflict creating duplicate email user")
	}
	if !strings.Contains(err.Error(), "already belongs to another user") {
		t.Fatalf("unexpected primary conflict error: %v", err)
	}
	duplicatePrimary, err := st.Users.GetByID(ctx, "user_duplicate_primary")
	if err != nil {
		t.Fatalf("lookup duplicate primary user: %v", err)
	}
	if duplicatePrimary != nil {
		t.Fatalf("conflicting primary Create left duplicate user row: %#v", duplicatePrimary)
	}

	phoneUser := &store.User{ID: "user_phone", TenantID: "tenant_a", Email: "phone:19900001111", SN: "SN-user-phone", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	if err := st.Users.Create(ctx, phoneUser); err != nil {
		t.Fatalf("create phone user: %v", err)
	}
	if err := st.Users.UpsertIdentity(ctx, &store.UserIdentity{TenantID: "tenant_a", UserID: phoneUser.ID, Type: "email", Value: "buyer@example.com", Verified: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("bind email identity: %v", err)
	}

	err = st.Users.Create(ctx, &store.User{ID: "user_duplicate_email", TenantID: "tenant_a", Email: "buyer@example.com", SN: "SN-duplicate-email", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now})
	if err == nil {
		t.Fatal("expected identity conflict creating duplicate email user")
	}
	if !strings.Contains(err.Error(), "already belongs to another user") {
		t.Fatalf("unexpected conflict error: %v", err)
	}
	duplicate, err := st.Users.GetByID(ctx, "user_duplicate_email")
	if err != nil {
		t.Fatalf("lookup duplicate user: %v", err)
	}
	if duplicate != nil {
		t.Fatalf("conflicting Create left duplicate user row: %#v", duplicate)
	}
	got, err := st.Users.GetByTenantIdentity(ctx, "tenant_a", "email", "buyer@example.com")
	if err != nil {
		t.Fatalf("lookup email identity: %v", err)
	}
	if got == nil || got.ID != phoneUser.ID {
		t.Fatalf("email identity owner = %#v, want %s", got, phoneUser.ID)
	}
}

func TestUserRepositoryUpsertIdentityClaimsOrphanIdentity(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	user := &store.User{ID: "user_email", TenantID: "tenant_a", Email: "buyer@example.com", SN: "SN-user-email", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	repo, ok := st.Users.(*userRepo)
	if !ok {
		t.Fatal("expected sqlite user repo")
	}
	if _, err := repo.db.ExecContext(ctx,
		`INSERT INTO user_identities (id, tenant_id, user_id, type, value, verified, verified_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"orphan_phone", "tenant_a", "missing_user", "phone", "19900001111", 1, now.Format(time.RFC3339), now.Format(time.RFC3339), now.Format(time.RFC3339),
	); err != nil {
		t.Fatalf("seed orphan identity: %v", err)
	}
	if got, err := st.Users.GetByTenantIdentity(ctx, "tenant_a", "phone", "19900001111"); err != nil || got != nil {
		t.Fatalf("orphan identity should not resolve before claim, got=%#v err=%v", got, err)
	}

	if err := st.Users.UpsertIdentity(ctx, &store.UserIdentity{TenantID: "tenant_a", UserID: user.ID, Type: "phone", Value: "19900001111", Verified: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("claim orphan phone: %v", err)
	}
	got, err := st.Users.GetByTenantIdentity(ctx, "tenant_a", "phone", "19900001111")
	if err != nil {
		t.Fatalf("lookup claimed phone: %v", err)
	}
	if got == nil || got.ID != user.ID {
		t.Fatalf("claimed phone owner = %#v, want %s", got, user.ID)
	}
}

func TestUserRepositoryCreateClaimsOrphanAccountIdentity(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	repo, ok := st.Users.(*userRepo)
	if !ok {
		t.Fatal("expected sqlite user repo")
	}
	if _, err := repo.db.ExecContext(ctx,
		`INSERT INTO user_identities (id, tenant_id, user_id, type, value, verified, verified_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"orphan_email", "tenant_a", "missing_user", "email", "orphan@example.com", 1, now.Format(time.RFC3339), now.Format(time.RFC3339), now.Format(time.RFC3339),
	); err != nil {
		t.Fatalf("seed orphan email identity: %v", err)
	}

	user := &store.User{ID: "user_orphan_email", TenantID: "tenant_a", Email: "orphan@example.com", SN: "SN-orphan-email", Status: "active", EnrollmentStatus: "approved", EmailVerified: true, CreatedAt: now, UpdatedAt: now}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("create user should claim orphan email identity: %v", err)
	}
	got, err := st.Users.GetByTenantIdentity(ctx, "tenant_a", "email", "orphan@example.com")
	if err != nil {
		t.Fatalf("lookup claimed email identity: %v", err)
	}
	if got == nil || got.ID != user.ID {
		t.Fatalf("claimed email owner = %#v, want %s", got, user.ID)
	}
}

func TestUserRepositoryListIdentitiesByUsers(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for _, user := range []*store.User{
		{ID: "user_email", TenantID: "tenant_a", Email: "buyer@example.com", SN: "SN-user-email", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "user_phone", TenantID: "tenant_a", Email: "phone:19900001111", SN: "SN-user-phone", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
	} {
		if err := st.Users.Create(ctx, user); err != nil {
			t.Fatalf("create user %s: %v", user.ID, err)
		}
	}
	for _, identity := range []*store.UserIdentity{
		{TenantID: "tenant_a", UserID: "user_email", Type: "email", Value: "buyer@example.com", Verified: true, CreatedAt: now, UpdatedAt: now},
		{TenantID: "tenant_a", UserID: "user_email", Type: "phone", Value: "18800001111", Verified: true, CreatedAt: now, UpdatedAt: now},
		{TenantID: "tenant_a", UserID: "user_phone", Type: "phone", Value: "19900001111", Verified: true, CreatedAt: now, UpdatedAt: now},
	} {
		if err := st.Users.UpsertIdentity(ctx, identity); err != nil {
			t.Fatalf("upsert identity %#v: %v", identity, err)
		}
	}
	batchRepo, ok := st.Users.(interface {
		ListIdentitiesByUsers(context.Context, string, []string) (map[string][]*store.UserIdentity, error)
	})
	if !ok {
		t.Fatal("user repository does not support batch identity listing")
	}
	got, err := batchRepo.ListIdentitiesByUsers(ctx, "tenant_a", []string{"user_email", "user_phone", "missing_user", "user_email"})
	if err != nil {
		t.Fatalf("list identities by users: %v", err)
	}
	if len(got["user_email"]) != 2 || len(got["user_phone"]) != 1 {
		t.Fatalf("unexpected identities by user: %#v", got)
	}
	if _, ok := got["missing_user"]; !ok || len(got["missing_user"]) != 0 {
		t.Fatalf("expected missing user entry, got %#v", got["missing_user"])
	}
}

func TestUserRepositoryReassignIdentityReplacesTargetIdentity(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for _, user := range []*store.User{
		{ID: "user_email", TenantID: "tenant_a", Email: "buyer@example.com", SN: "SN-user-email", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "user_phone", TenantID: "tenant_a", Email: "phone:19900001111", SN: "SN-user-phone", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
	} {
		if err := st.Users.Create(ctx, user); err != nil {
			t.Fatalf("create user %s: %v", user.ID, err)
		}
	}
	if err := st.Users.UpsertIdentity(ctx, &store.UserIdentity{TenantID: "tenant_a", UserID: "user_email", Type: "phone", Value: "18800001111", Verified: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("bind old phone: %v", err)
	}
	reassigner, ok := st.Users.(interface {
		ReassignIdentity(context.Context, string, string, string, string, bool, time.Time) error
	})
	if !ok {
		t.Fatal("user repository does not support identity reassignment")
	}
	if err := reassigner.ReassignIdentity(ctx, "tenant_a", "phone", "19900001111", "user_email", true, now); err != nil {
		t.Fatalf("reassign identity: %v", err)
	}

	got, err := st.Users.GetByTenantIdentity(ctx, "tenant_a", "phone", "19900001111")
	if err != nil {
		t.Fatalf("lookup new phone identity: %v", err)
	}
	if got == nil || got.ID != "user_email" {
		t.Fatalf("new phone identity owner = %#v, want user_email", got)
	}
	old, err := st.Users.GetByTenantIdentity(ctx, "tenant_a", "phone", "18800001111")
	if err != nil {
		t.Fatalf("lookup old phone identity: %v", err)
	}
	if old != nil {
		t.Fatalf("old phone identity still exists: %#v", old)
	}
	identities, err := st.Users.ListIdentitiesByUser(ctx, "tenant_a", "user_email")
	if err != nil {
		t.Fatalf("list identities: %v", err)
	}
	phoneCount := 0
	for _, identity := range identities {
		if identity.Type == "phone" {
			phoneCount++
			if identity.ID != "user_email_phone" || identity.Value != "19900001111" {
				t.Fatalf("unexpected phone identity after reassign: %#v", identity)
			}
		}
	}
	if phoneCount != 1 {
		t.Fatalf("phone identity count = %d, want 1", phoneCount)
	}
}

func TestInvitationCodeMarkUsedOnlyConsumesUnusedCode(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	item := &store.InvitationCode{
		ID:        "ic_atomic",
		TenantID:  store.DefaultTenantID,
		Code:      "ATOMIC1234",
		Status:    "unused",
		CreatedAt: now,
	}
	if err := st.InvitationCodes.Create(ctx, item); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := st.InvitationCodes.MarkUsed(ctx, item.ID, "first@example.com", now); err != nil {
		t.Fatalf("first MarkUsed: %v", err)
	}
	if err := st.InvitationCodes.MarkUsed(ctx, item.ID, "second@example.com", now.Add(time.Minute)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("second MarkUsed error = %v, want sql.ErrNoRows", err)
	}
	got, err := st.InvitationCodes.GetByID(ctx, item.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil || got.UsedByEmail != "first@example.com" {
		t.Fatalf("used email was overwritten: %#v", got)
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
	repo := st.Tenants.(*tenantRepo)
	deletedAt := time.Now().UTC().Format(time.RFC3339)
	if err := execWrite(ctx, repo.batch, repo.db, `UPDATE tenants SET status = 'deleted', deleted_at = ?, updated_at = ? WHERE id = ?`, deletedAt, deletedAt, store.DefaultTenantID); err != nil {
		t.Fatalf("mark default tenant deleted: %v", err)
	}
	defaultTenant, err = st.Tenants.EnsureDefault(ctx)
	if err != nil {
		t.Fatalf("repair default tenant: %v", err)
	}
	if defaultTenant == nil || defaultTenant.DeletedAt != nil || defaultTenant.Status != "active" {
		t.Fatalf("default tenant not repaired: %#v", defaultTenant)
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
	if err := repo.UpdateSettings(ctx, tenant.ID, "Acme Renamed", "team.acme.com", `{"email_domains":["team.acme.com"],"allow_user_registration":false}`); err != nil {
		t.Fatalf("update tenant settings: %v", err)
	}
	got, err = st.Tenants.GetByID(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("get updated tenant: %v", err)
	}
	if got == nil || got.Name != "Acme Renamed" || got.PrimaryDomain != "team.acme.com" || !strings.Contains(got.SettingsJSON, `"allow_user_registration":false`) {
		t.Fatalf("unexpected updated tenant: %#v", got)
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
func TestSessionRepositoryUserTokenUsageSnapshotDeltasAndReset(t *testing.T) {
	st := newTestStore(t)
	ctx := store.WithTenant(context.Background(), "tenant_acme")
	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	user := &store.User{ID: "u_rank", TenantID: "tenant_acme", Email: "rank@example.com", SN: "SN-RANK", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	usage := store.UserTokenUsage{InputTokens: 100, OutputTokens: 20, CachedInputTokens: 30, CacheWriteTokens: 5}
	if err := st.Sessions.RecordUserTokenUsageSnapshot(ctx, user.TenantID, "gui:machine-1", user.ID, usage, now); err != nil {
		t.Fatalf("record first snapshot: %v", err)
	}
	if err := st.Sessions.RecordUserTokenUsageSnapshot(ctx, user.TenantID, "gui:machine-1", user.ID, usage, now.Add(time.Minute)); err != nil {
		t.Fatalf("record duplicate snapshot: %v", err)
	}
	mixedUsage := store.UserTokenUsage{InputTokens: 180, OutputTokens: 10, CachedInputTokens: 30, CacheWriteTokens: 5}
	if err := st.Sessions.RecordUserTokenUsageSnapshot(ctx, user.TenantID, "gui:machine-1", user.ID, mixedUsage, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("record mixed snapshot: %v", err)
	}
	resetUsage := store.UserTokenUsage{InputTokens: 10, OutputTokens: 2, CachedInputTokens: 1, CacheWriteTokens: 1}
	if err := st.Sessions.RecordUserTokenUsageSnapshot(ctx, user.TenantID, "gui:machine-1", user.ID, resetUsage, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("record reset snapshot: %v", err)
	}

	rows, err := st.Sessions.SummarizeUserTokenUsage(ctx, user.TenantID, now, now.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("summarize token usage: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1: %#v", len(rows), rows)
	}
	got := rows[0]
	if got.UserEmail != "rank@example.com" {
		t.Fatalf("email = %q", got.UserEmail)
	}
	if got.Usage.InputTokens != 190 || got.Usage.OutputTokens != 22 || got.Usage.CachedInputTokens != 31 || got.Usage.CacheWriteTokens != 6 {
		t.Fatalf("usage = %#v", got.Usage)
	}
	if got.Usage.TotalTokens() != 212 {
		t.Fatalf("total tokens = %d, want input + output only", got.Usage.TotalTokens())
	}
}

func TestSessionRepositorySummarizeUserTokenUsageMergesLegacyBoundEmailAndPhone(t *testing.T) {
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
	st := NewStore(provider)
	ctx := store.WithTenant(context.Background(), "tenant_acme")
	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	user := &store.User{ID: "u_bound_usage", TenantID: "tenant_acme", Email: "phone:17090134628", SN: "SN-BOUND-USAGE", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := st.Users.UpsertIdentity(ctx, &store.UserIdentity{
		ID:        user.ID + "_email",
		TenantID:  user.TenantID,
		UserID:    user.ID,
		Type:      "email",
		Value:     "ztest@163.com",
		Verified:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert email identity: %v", err)
	}
	for _, row := range []struct {
		account string
		input   int64
		output  int64
	}{
		{account: "phone:17090134628", input: 20, output: 5},
		{account: "ztest@163.com", input: 100, output: 10},
	} {
		if _, err := provider.Write.ExecContext(ctx, `
			INSERT INTO user_usage_daily (tenant_id, user_email, day, input_tokens, output_tokens, cached_input_tokens, cache_write_tokens, updated_at)
			VALUES (?, ?, ?, ?, ?, 0, 0, ?)`,
			user.TenantID, row.account, now.Format("2006-01-02"), row.input, row.output, now.Format(time.RFC3339)); err != nil {
			t.Fatalf("insert legacy usage row %s: %v", row.account, err)
		}
	}

	rows, err := st.Sessions.SummarizeUserTokenUsage(ctx, user.TenantID, now, now.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("summarize token usage: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1: %#v", len(rows), rows)
	}
	got := rows[0]
	if got.UserID != user.ID || got.UserEmail != "ztest@163.com" {
		t.Fatalf("identity = user_id:%q email:%q, want bound user/email", got.UserID, got.UserEmail)
	}
	if got.Usage.InputTokens != 120 || got.Usage.OutputTokens != 15 || got.Usage.TotalTokens() != 135 {
		t.Fatalf("usage = %#v, want merged phone+email totals", got.Usage)
	}
}

func TestRunMigrationsBackfillsUserUsageUserID(t *testing.T) {
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
	if _, err := provider.Write.Exec(`CREATE TABLE users (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
		email TEXT NOT NULL,
		sn TEXT NOT NULL UNIQUE,
		status TEXT NOT NULL DEFAULT 'active',
		enrollment_status TEXT NOT NULL DEFAULT 'approved',
		smart_route INTEGER NOT NULL DEFAULT 0,
		email_verified INTEGER NOT NULL DEFAULT 0,
		email_verified_at TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE(tenant_id, email)
	)`); err != nil {
		t.Fatalf("create legacy users: %v", err)
	}
	if _, err := provider.Write.Exec(`CREATE TABLE user_usage_daily (
		tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
		user_email TEXT NOT NULL,
		day TEXT NOT NULL,
		input_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		cached_input_tokens INTEGER NOT NULL DEFAULT 0,
		cache_write_tokens INTEGER NOT NULL DEFAULT 0,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (tenant_id, user_email, day)
	)`); err != nil {
		t.Fatalf("create legacy usage: %v", err)
	}
	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	if _, err := provider.Write.Exec(`INSERT INTO users (id, tenant_id, email, sn, status, enrollment_status, created_at, updated_at)
		VALUES ('u_backfill_usage', 'tenant_acme', 'phone:17090134628', 'SN-BACKFILL-USAGE', 'active', 'approved', ?, ?)`, now.Format(time.RFC3339), now.Format(time.RFC3339)); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := provider.Write.Exec(`INSERT INTO user_usage_daily (tenant_id, user_email, day, input_tokens, output_tokens, updated_at)
		VALUES ('tenant_acme', 'phone:17090134628', '2026-06-24', 20, 5, ?)`, now.Format(time.RFC3339)); err != nil {
		t.Fatalf("insert usage: %v", err)
	}

	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	var userID string
	if err := provider.Read.QueryRow(`SELECT user_id FROM user_usage_daily WHERE tenant_id = 'tenant_acme' AND user_email = 'phone:17090134628'`).Scan(&userID); err != nil {
		t.Fatalf("query backfilled user_id: %v", err)
	}
	if userID != "u_backfill_usage" {
		t.Fatalf("user_id = %q, want u_backfill_usage", userID)
	}
}

func TestSessionRepositorySummarizeUserDurationsMergesOverlapsAndRequiresEmail(t *testing.T) {
	st := newTestStore(t)
	ctx := store.WithTenant(context.Background(), "tenant_acme")
	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	user := &store.User{ID: "u_duration", TenantID: "tenant_acme", Email: "duration@example.com", SN: "SN-DURATION", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	alpha := &store.User{ID: "u_alpha_duration", TenantID: "tenant_acme", Email: "alpha@example.com", SN: "SN-DURATION-ALPHA", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	if err := st.Users.Create(ctx, alpha); err != nil {
		t.Fatalf("create alpha user: %v", err)
	}

	// Record heartbeats for user "u_duration": two sessions with overlap
	// Session A: 10:00 - 10:30 (heartbeats every minute)
	// Session B: 10:15 - 11:00 (heartbeats every minute, overlaps with A)
	// Merged: 10:00 - 11:00 = 60 minutes
	for i := 0; i < 30; i++ {
		_ = st.Sessions.RecordHeartbeat(ctx, user.TenantID, "m_a", user.ID, now.Add(time.Duration(i)*time.Minute))
	}
	for i := 15; i < 60; i++ {
		_ = st.Sessions.RecordHeartbeat(ctx, user.TenantID, "m_b", user.ID, now.Add(time.Duration(i)*time.Minute))
	}
	// Session C: 12:00 - 12:10 (gap from previous, separate interval)
	for i := 0; i < 10; i++ {
		_ = st.Sessions.RecordHeartbeat(ctx, user.TenantID, "m_c", user.ID, now.Add(2*time.Hour+time.Duration(i)*time.Minute))
	}

	// Record heartbeats for alpha: 10:00 - 10:10
	for i := 0; i < 10; i++ {
		_ = st.Sessions.RecordHeartbeat(ctx, alpha.TenantID, "m_alpha", alpha.ID, now.Add(time.Duration(i)*time.Minute))
	}

	// User with email-like ID (direct@example.com) — 10:00 - 10:05
	for i := 0; i < 5; i++ {
		_ = st.Sessions.RecordHeartbeat(ctx, user.TenantID, "m_direct", "direct@example.com", now.Add(time.Duration(i)*time.Minute))
	}

	// User with no email (u_missing_email) — should be excluded
	for i := 0; i < 20; i++ {
		_ = st.Sessions.RecordHeartbeat(ctx, user.TenantID, "m_uid", "u_missing_email", now.Add(time.Duration(i)*time.Minute))
	}

	rows, err := st.Sessions.SummarizeUserDurations(ctx, user.TenantID, now, now.Add(24*time.Hour), now.Add(3*time.Hour))
	if err != nil {
		t.Fatalf("summarize user durations: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3: %#v", len(rows), rows)
	}
	// Sorted by email: alpha < direct < duration
	if rows[0].UserEmail != "alpha@example.com" {
		t.Fatalf("first row = %#v, want alpha@example.com", rows[0])
	}
	if rows[1].UserEmail != "direct@example.com" {
		t.Fatalf("second row = %#v, want direct@example.com", rows[1])
	}
	if rows[2].UserEmail != "duration@example.com" {
		t.Fatalf("third row = %#v, want duration@example.com", rows[2])
	}
	// duration@example.com: merged 60m + 10m = 70m (intervals overlap-merged)
	// The exact value depends on heartbeat merge logic (gap < 5min merges)
	if rows[2].DurationSeconds < int64(60*time.Minute/time.Second) {
		t.Fatalf("duration@example.com duration = %d seconds, want >= 3600", rows[2].DurationSeconds)
	}
}

func TestSessionRepositorySummarizeUserDurationsIncludesLegacyBlankUpdatedAt(t *testing.T) {
	st := newTestStore(t)
	ctx := store.WithTenant(context.Background(), "tenant_acme")
	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	user := &store.User{ID: "u_legacy_duration", TenantID: "tenant_acme", Email: "legacy@example.com", SN: "SN-LEGACY-DURATION", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Duration is now purely heartbeat-based. Record heartbeats for 45 minutes.
	for i := 0; i < 45; i++ {
		_ = st.Sessions.RecordHeartbeat(ctx, user.TenantID, "m_legacy", user.ID, now.Add(time.Duration(i)*time.Minute))
	}

	rows, err := st.Sessions.SummarizeUserDurations(ctx, user.TenantID, now, now.Add(24*time.Hour), now.Add(45*time.Minute))
	if err != nil {
		t.Fatalf("summarize user durations: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1: %#v", len(rows), rows)
	}
	if rows[0].UserEmail != "legacy@example.com" || rows[0].DurationSeconds != int64(44*time.Minute/time.Second) {
		t.Fatalf("row = %#v, want legacy@example.com with 44m (45 heartbeats at 1min intervals = 44min span)", rows[0])
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

func TestUsersTenantEmailLookupAndDeleteAreCaseInsensitive(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	user := &store.User{ID: "u_mixed_case", TenantID: "tenant_case", Email: "Mixed.User@Example.com", SN: "SN-CASE", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	got, err := st.Users.GetByTenantEmail(ctx, "tenant_case", "mixed.user@example.com")
	if err != nil {
		t.Fatalf("get mixed-case user: %v", err)
	}
	if got == nil || got.ID != user.ID {
		t.Fatalf("got user = %#v, want %s", got, user.ID)
	}

	if err := st.Users.DeleteByTenantEmail(ctx, "tenant_case", "MIXED.USER@example.com"); err != nil {
		t.Fatalf("delete mixed-case user: %v", err)
	}
	got, err = st.Users.GetByTenantEmail(ctx, "tenant_case", "mixed.user@example.com")
	if err != nil || got != nil {
		t.Fatalf("deleted user = %#v err=%v, want nil", got, err)
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
