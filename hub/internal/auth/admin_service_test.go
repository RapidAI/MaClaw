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
