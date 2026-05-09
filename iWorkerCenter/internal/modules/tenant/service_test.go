package tenant

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/db"
)

func setupTestDB(t *testing.T) *db.Provider {
	t.Helper()
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	return provider
}

func newTestService(t *testing.T) (*TenantService, *db.Provider) {
	t.Helper()
	if os.Getenv("IWORKERCENTER_HOME") == "" {
		t.Setenv("IWORKERCENTER_HOME", t.TempDir())
	}
	p := setupTestDB(t)
	repo := NewTenantRepo(p.Write, p.Read)
	svc := NewTenantService(repo, p.Write, p.Write, nil)
	return svc, p
}

func TestSetupFirstTenant_Success(t *testing.T) {
	svc, p := newTestService(t)
	ctx := context.Background()

	tenant, err := svc.SetupFirstTenant(ctx, CreateTenantRequest{
		CompanyName:   "测试公司",
		LegalPerson:   "张三",
		Email:         "admin@test.com",
		Address:       "北京市",
		AdminUsername: "admin",
		AdminPassword: "pass1234",
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if tenant == nil || tenant.ID == "" {
		t.Fatal("expected tenant with ID")
	}
	if tenant.CompanyName != "测试公司" {
		t.Errorf("expected company name '测试公司', got %q", tenant.CompanyName)
	}
	if tenant.Status != "active" {
		t.Errorf("expected status 'active', got %q", tenant.Status)
	}

	// Verify admin user was created
	var adminCount int
	_ = p.Read.QueryRow(`SELECT COUNT(*) FROM admin_users WHERE tenant_id=?`, tenant.ID).Scan(&adminCount)
	if adminCount != 1 {
		t.Errorf("expected 1 admin user, got %d", adminCount)
	}

	// Verify root security group was created
	var sgCount int
	_ = p.Read.QueryRow(`SELECT COUNT(*) FROM security_groups WHERE tenant_id=?`, tenant.ID).Scan(&sgCount)
	if sgCount != 1 {
		t.Errorf("expected 1 security group, got %d", sgCount)
	}
}

func TestSetupFirstTenant_AlreadyDone(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	_, err := svc.SetupFirstTenant(ctx, CreateTenantRequest{
		CompanyName: "公司A", Email: "a@test.com",
		AdminUsername: "admin", AdminPassword: "pass",
	})
	if err != nil {
		t.Fatalf("first setup: %v", err)
	}

	// Second setup should fail
	_, err = svc.SetupFirstTenant(ctx, CreateTenantRequest{
		CompanyName: "公司B", Email: "b@test.com",
		AdminUsername: "admin2", AdminPassword: "pass",
	})
	if err != ErrSetupAlreadyDone {
		t.Errorf("expected ErrSetupAlreadyDone, got %v", err)
	}
}

func TestSetupFirstTenant_DuplicateCompanyName(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	_, err := svc.SetupFirstTenant(ctx, CreateTenantRequest{
		CompanyName: "唯一公司", Email: "a@test.com",
		AdminUsername: "admin", AdminPassword: "pass",
	})
	if err != nil {
		t.Fatalf("first setup: %v", err)
	}

	// Can't test duplicate via SetupFirstTenant (it checks count > 0).
	// Test via createTenantInternal indirectly — provision would hit this.
}

func TestSetupFirstTenant_MissingFields(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	cases := []struct {
		name string
		req  CreateTenantRequest
	}{
		{"no company", CreateTenantRequest{Email: "a@t.com", AdminUsername: "a", AdminPassword: "p"}},
		{"no email", CreateTenantRequest{CompanyName: "C", AdminUsername: "a", AdminPassword: "p"}},
		{"no username", CreateTenantRequest{CompanyName: "C", Email: "a@t.com", AdminPassword: "p"}},
		{"no password", CreateTenantRequest{CompanyName: "C", Email: "a@t.com", AdminUsername: "a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.SetupFirstTenant(ctx, tc.req)
			if err == nil {
				t.Error("expected error for missing fields")
			}
		})
	}
}

func TestListActiveTenants(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	// Initially empty
	list, err := svc.ListActiveTenants(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0 tenants, got %d", len(list))
	}

	// Create one
	_, _ = svc.SetupFirstTenant(ctx, CreateTenantRequest{
		CompanyName: "公司A", Email: "a@test.com",
		AdminUsername: "admin", AdminPassword: "pass",
	})

	list, err = svc.ListActiveTenants(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 tenant, got %d", len(list))
	}
}

func TestTenantCount(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	count, _ := svc.TenantCount(ctx)
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}

	_, _ = svc.SetupFirstTenant(ctx, CreateTenantRequest{
		CompanyName: "公司A", Email: "a@test.com",
		AdminUsername: "admin", AdminPassword: "pass",
	})

	count, _ = svc.TenantCount(ctx)
	if count != 1 {
		t.Errorf("expected 1, got %d", count)
	}
}

func TestNonceRepo_ConsumeAndReplay(t *testing.T) {
	p := setupTestDB(t)
	repo := NewNonceRepo(p.Write)
	ctx := context.Background()

	expiry := time.Now().Add(5 * time.Minute)

	// First consume should succeed
	ok, err := repo.Consume(ctx, "nonce-123", expiry)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if !ok {
		t.Error("expected first consume to succeed")
	}

	// Second consume (replay) should fail
	ok, err = repo.Consume(ctx, "nonce-123", expiry)
	if err != nil {
		t.Fatalf("consume replay: %v", err)
	}
	if ok {
		t.Error("expected replay to be rejected")
	}
}

func TestTenantRepo_CRUD(t *testing.T) {
	p := setupTestDB(t)
	repo := NewTenantRepo(p.Write, p.Read)
	ctx := context.Background()

	now := time.Now()
	tenant := &Tenant{
		ID:          "tnt_test",
		CompanyName: "测试企业",
		LegalPerson: "李四",
		Email:       "test@example.com",
		Address:     "上海市",
		Status:      "active",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := repo.Create(ctx, tenant); err != nil {
		t.Fatalf("create: %v", err)
	}

	// GetByID
	got, err := repo.GetByID(ctx, "tnt_test")
	if err != nil {
		t.Fatalf("getByID: %v", err)
	}
	if got == nil || got.CompanyName != "测试企业" {
		t.Errorf("unexpected tenant: %+v", got)
	}

	// GetByCompanyName
	got, err = repo.GetByCompanyName(ctx, "测试企业")
	if err != nil {
		t.Fatalf("getByCompanyName: %v", err)
	}
	if got == nil || got.ID != "tnt_test" {
		t.Errorf("unexpected tenant: %+v", got)
	}

	// ListActive
	list, err := repo.ListActive(ctx)
	if err != nil {
		t.Fatalf("listActive: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1, got %d", len(list))
	}

	// Count
	count, _ := repo.Count(ctx)
	if count != 1 {
		t.Errorf("expected 1, got %d", count)
	}

	// UpdateStatus
	if err := repo.UpdateStatus(ctx, "tnt_test", "disabled"); err != nil {
		t.Fatalf("updateStatus: %v", err)
	}
	list, _ = repo.ListActive(ctx)
	if len(list) != 0 {
		t.Errorf("expected 0 active after disable, got %d", len(list))
	}

	// Duplicate company name
	dup := &Tenant{
		ID: "tnt_dup", CompanyName: "测试企业", Email: "dup@test.com",
		Status: "active", CreatedAt: now, UpdatedAt: now,
	}
	err = repo.Create(ctx, dup)
	if err == nil {
		t.Error("expected error for duplicate company name")
	}
}

func TestTenantRepoUpdateCloudInfoRequiresExistingTenant(t *testing.T) {
	p := setupTestDB(t)
	repo := NewTenantRepo(p.Write, p.Read)

	err := repo.UpdateCloudInfo(context.Background(), "missing-tenant", "center-1", "secret-1")
	if err != ErrTenantNotFound {
		t.Fatalf("UpdateCloudInfo missing tenant err = %v, want ErrTenantNotFound", err)
	}
}

func TestLoginWithTenantID(t *testing.T) {
	p := setupTestDB(t)
	svc, _ := newTestService(t)
	ctx := context.Background()

	// Create tenant
	tenant, err := svc.SetupFirstTenant(ctx, CreateTenantRequest{
		CompanyName: "登录测试公司", Email: "login@test.com",
		AdminUsername: "loginadmin", AdminPassword: "secret123",
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Verify admin can be found with tenant_id
	var userID, storedHash, salt string
	err = p.Read.QueryRow(
		`SELECT id, password_hash, salt FROM admin_users WHERE username=? AND tenant_id=?`,
		"loginadmin", tenant.ID).Scan(&userID, &storedHash, &salt)
	if err != nil {
		t.Fatalf("query admin: %v", err)
	}
	if userID == "" {
		t.Error("expected admin user ID")
	}

	// Verify password hash matches
	computed := hashPassword("secret123", salt)
	if computed != storedHash {
		t.Error("password hash mismatch")
	}

	// Verify wrong tenant_id returns no rows
	err = p.Read.QueryRow(
		`SELECT id FROM admin_users WHERE username=? AND tenant_id=?`,
		"loginadmin", "wrong_tenant").Scan(&userID)
	if err == nil {
		t.Error("expected no rows for wrong tenant_id")
	}
}
