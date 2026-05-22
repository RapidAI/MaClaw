package auth

import (
	"context"
	"errors"
	"testing"
)

func TestAdminServiceSetupAndLogin(t *testing.T) {
	deps := newTestStore(t)
	svc := NewAdminService(deps.store.Admins, deps.store.System, deps.store.AdminAudit)
	ctx := context.Background()

	initialized, err := svc.IsInitialized(ctx)
	if err != nil {
		t.Fatalf("IsInitialized before setup: %v", err)
	}
	if initialized {
		t.Fatal("expected hub admin to be uninitialized")
	}

	if err := svc.SetupInitialAdmin(ctx, "admin", "pass123456", "admin@example.com"); err != nil {
		t.Fatalf("SetupInitialAdmin: %v", err)
	}

	initialized, err = svc.IsInitialized(ctx)
	if err != nil {
		t.Fatalf("IsInitialized after setup: %v", err)
	}
	if !initialized {
		t.Fatal("expected hub admin to be initialized")
	}

	if err := svc.SetupInitialAdmin(ctx, "admin2", "pass123456", "admin2@example.com"); !errors.Is(err, ErrAdminAlreadyInitialized) {
		t.Fatalf("expected ErrAdminAlreadyInitialized, got %v", err)
	}

	token, admin, err := svc.Login(ctx, "admin", "pass123456")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if token == "" {
		t.Fatal("expected admin login token")
	}
	if admin == nil || admin.Email != "admin@example.com" {
		t.Fatalf("unexpected admin: %+v", admin)
	}

	if _, _, err := svc.Login(ctx, "admin", "wrong-password"); !errors.Is(err, ErrInvalidAdminCredentials) {
		t.Fatalf("expected ErrInvalidAdminCredentials, got %v", err)
	}
}

func TestAdminServiceSetupRequiresEmail(t *testing.T) {
	deps := newTestStore(t)
	svc := NewAdminService(deps.store.Admins, deps.store.System, deps.store.AdminAudit)
	ctx := context.Background()

	if err := svc.SetupInitialAdmin(ctx, "admin", "pass123456", ""); err == nil {
		t.Fatal("expected setup to require admin email")
	}
	if err := svc.SetupInitialAdmin(ctx, "admin", "pass123456", "not-an-email"); err == nil {
		t.Fatal("expected setup to reject invalid admin email")
	}
}

func TestAdminServiceSetupRejectsBlankUsernameOrPassword(t *testing.T) {
	deps := newTestStore(t)
	svc := NewAdminService(deps.store.Admins, deps.store.System, deps.store.AdminAudit)
	ctx := context.Background()

	if err := svc.SetupInitialAdmin(ctx, "   ", "pass123456", "admin@example.com"); err == nil {
		t.Fatal("expected setup to reject blank username")
	}
	if err := svc.SetupInitialAdmin(ctx, "admin", "   ", "admin@example.com"); err == nil {
		t.Fatal("expected setup to reject blank password")
	}
	initialized, err := svc.IsInitialized(ctx)
	if err != nil {
		t.Fatalf("IsInitialized: %v", err)
	}
	if initialized {
		t.Fatal("invalid setup attempts should not initialize admin")
	}
}

func TestAdminServiceTenantAdminRequiresValidEmail(t *testing.T) {
	deps := newTestStore(t)
	svc := NewAdminService(deps.store.Admins, deps.store.System, deps.store.AdminAudit)
	ctx := context.Background()

	if _, err := svc.CreateTenantAdmin(ctx, "tenant_a", "admin", "pass123456", "not-an-email", "Tenant Admin", "tenant_admin"); err == nil {
		t.Fatal("expected tenant admin creation to reject invalid email")
	}
}

func TestAdminServiceUpdateEmailRequiresValidEmail(t *testing.T) {
	deps := newTestStore(t)
	svc := NewAdminService(deps.store.Admins, deps.store.System, deps.store.AdminAudit)
	ctx := context.Background()

	if err := svc.SetupInitialAdmin(ctx, "admin", "pass123456", "admin@example.com"); err != nil {
		t.Fatalf("SetupInitialAdmin: %v", err)
	}
	if _, _, err := svc.UpdateEmail(ctx, "admin", "not-an-email"); err == nil {
		t.Fatal("expected update email to reject invalid email")
	}
}

func TestAdminServiceResetAdminCredentials(t *testing.T) {
	deps := newTestStore(t)
	svc := NewAdminService(deps.store.Admins, deps.store.System, deps.store.AdminAudit)
	ctx := context.Background()

	if err := svc.SetupInitialAdmin(ctx, "admin", "pass123456", "admin@example.com"); err != nil {
		t.Fatalf("SetupInitialAdmin: %v", err)
	}

	if err := svc.ResetAdminCredentials(ctx, "owner", "reset123456"); err != nil {
		t.Fatalf("ResetAdminCredentials: %v", err)
	}

	count, err := deps.store.Admins.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	if _, _, err := svc.Login(ctx, "admin", "pass123456"); !errors.Is(err, ErrInvalidAdminCredentials) {
		t.Fatalf("expected old credentials to fail, got %v", err)
	}

	_, admin, err := svc.Login(ctx, "owner", "reset123456")
	if err != nil {
		t.Fatalf("Login with reset credentials: %v", err)
	}
	if admin == nil || admin.Email != "owner@local.admin" {
		t.Fatalf("unexpected reset admin: %+v", admin)
	}
}

func TestAdminServiceResetAdminCredentialsRejectsBlankInput(t *testing.T) {
	deps := newTestStore(t)
	svc := NewAdminService(deps.store.Admins, deps.store.System, deps.store.AdminAudit)
	ctx := context.Background()

	if err := svc.SetupInitialAdmin(ctx, "admin", "pass123456", "admin@example.com"); err != nil {
		t.Fatalf("SetupInitialAdmin: %v", err)
	}
	if err := svc.ResetAdminCredentials(ctx, "   ", "reset123456"); err == nil {
		t.Fatal("expected reset to reject blank username")
	}
	if err := svc.ResetAdminCredentials(ctx, "owner", "   "); err == nil {
		t.Fatal("expected reset to reject blank password")
	}
	if _, _, err := svc.Login(ctx, "admin", "pass123456"); err != nil {
		t.Fatalf("blank reset attempts should not delete existing admin: %v", err)
	}
}

func TestAdminServiceTokenSurvivesServiceRestart(t *testing.T) {
	deps := newTestStore(t)
	ctx := context.Background()

	first := NewAdminService(deps.store.Admins, deps.store.System, deps.store.AdminAudit)
	if err := first.SetupInitialAdmin(ctx, "admin", "pass123456", "admin@example.com"); err != nil {
		t.Fatalf("SetupInitialAdmin: %v", err)
	}

	token, _, err := first.Login(ctx, "admin", "pass123456")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	second := NewAdminService(deps.store.Admins, deps.store.System, deps.store.AdminAudit)
	admin, err := second.Authenticate(ctx, token)
	if err != nil {
		t.Fatalf("Authenticate after restart: %v", err)
	}
	if admin == nil || admin.Username != "admin" {
		t.Fatalf("unexpected admin after restart: %+v", admin)
	}
}

func TestAdminServiceChangePassword(t *testing.T) {
	deps := newTestStore(t)
	svc := NewAdminService(deps.store.Admins, deps.store.System, deps.store.AdminAudit)
	ctx := context.Background()

	if err := svc.SetupInitialAdmin(ctx, "admin", "pass123456", "admin@example.com"); err != nil {
		t.Fatalf("SetupInitialAdmin: %v", err)
	}
	oldToken, _, err := svc.Login(ctx, "admin", "pass123456")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	newToken, admin, err := svc.ChangePassword(ctx, "admin", "pass123456", "newpass123456")
	if err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if newToken == "" || admin == nil {
		t.Fatalf("expected updated token and admin, got token=%q admin=%+v", newToken, admin)
	}
	if _, err := svc.Authenticate(ctx, oldToken); !errors.Is(err, ErrInvalidAdminCredentials) {
		t.Fatalf("expected old token to be invalidated, got %v", err)
	}
	if _, _, err := svc.Login(ctx, "admin", "pass123456"); !errors.Is(err, ErrInvalidAdminCredentials) {
		t.Fatalf("expected old password to fail, got %v", err)
	}
	if _, _, err := svc.Login(ctx, "admin", "newpass123456"); err != nil {
		t.Fatalf("expected new password to work, got %v", err)
	}
}

func TestAdminServiceChangePasswordRejectsBlankNewPassword(t *testing.T) {
	deps := newTestStore(t)
	svc := NewAdminService(deps.store.Admins, deps.store.System, deps.store.AdminAudit)
	ctx := context.Background()

	if err := svc.SetupInitialAdmin(ctx, "admin", "pass123456", "admin@example.com"); err != nil {
		t.Fatalf("SetupInitialAdmin: %v", err)
	}
	if _, _, err := svc.ChangePassword(ctx, "admin", "pass123456", "   "); err == nil {
		t.Fatal("expected blank new password to be rejected")
	}
	if _, _, err := svc.Login(ctx, "admin", "pass123456"); err != nil {
		t.Fatalf("blank password change should leave old password intact: %v", err)
	}
}

func TestTenantAdminsCanShareUsernameAcrossTenantScopes(t *testing.T) {
	deps := newTestStore(t)
	svc := NewAdminService(deps.store.Admins, deps.store.System, deps.store.AdminAudit)
	ctx := context.Background()

	if err := svc.SetupInitialAdmin(ctx, "shared", "globalpass123", "shared@example.com"); err != nil {
		t.Fatalf("SetupInitialAdmin: %v", err)
	}
	if _, err := svc.CreateTenantAdmin(ctx, "tenant_a", "shared", "tenantpass123", "tenant@example.com", "Tenant A", "tenant_admin"); err != nil {
		t.Fatalf("create tenant a admin: %v", err)
	}
	if _, err := svc.CreateTenantAdmin(ctx, "tenant_b", "shared", "tenantpass123", "tenant@example.com", "Tenant B", "tenant_admin"); err != nil {
		t.Fatalf("create tenant b admin with reused username/email: %v", err)
	}

	_, globalAdmin, err := svc.Login(ctx, "shared", "globalpass123")
	if err != nil {
		t.Fatalf("global login: %v", err)
	}
	if globalAdmin.Scope != "global" || globalAdmin.TenantID != "" {
		t.Fatalf("expected global admin, got %#v", globalAdmin)
	}
	if _, _, err := svc.LoginScoped(ctx, "shared", "tenantpass123", ""); !errors.Is(err, ErrInvalidAdminCredentials) {
		t.Fatalf("tenant admin login without tenant scope should fail, got %v", err)
	}
	_, tenantAdmin, err := svc.LoginScoped(ctx, "shared", "tenantpass123", "tenant_a")
	if err != nil {
		t.Fatalf("tenant login: %v", err)
	}
	if tenantAdmin.Scope != "tenant" || tenantAdmin.TenantID != "tenant_a" {
		t.Fatalf("expected tenant_a admin, got %#v", tenantAdmin)
	}

	if _, _, err := svc.ChangePasswordScoped(ctx, "shared", "tenantpass123", "tenantnew123", "tenant", "tenant_a"); err != nil {
		t.Fatalf("tenant change password: %v", err)
	}
	if _, _, err := svc.LoginScoped(ctx, "shared", "tenantpass123", "tenant_a"); !errors.Is(err, ErrInvalidAdminCredentials) {
		t.Fatalf("expected old tenant_a password to fail, got %v", err)
	}
	if _, _, err := svc.LoginScoped(ctx, "shared", "tenantnew123", "tenant_a"); err != nil {
		t.Fatalf("expected tenant_a new password to work: %v", err)
	}
	if _, _, err := svc.LoginScoped(ctx, "shared", "tenantpass123", "tenant_b"); err != nil {
		t.Fatalf("tenant_b password should be unchanged: %v", err)
	}
	if _, _, err := svc.Login(ctx, "shared", "globalpass123"); err != nil {
		t.Fatalf("global password should be unchanged: %v", err)
	}
}
