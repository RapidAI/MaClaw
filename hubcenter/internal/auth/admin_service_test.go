package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()

	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN:               t.TempDir() + `\hubcenter-test.db`,
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

	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	return sqlite.NewStore(provider)
}

func TestAdminServiceSetupLoginAndReset(t *testing.T) {
	st := newTestStore(t)
	svc := NewAdminService(st.Admins, st.System, st.AdminAudit)
	ctx := context.Background()

	if err := svc.SetupInitialAdmin(ctx, "admin", "pass123456", "admin@example.com"); err != nil {
		t.Fatalf("SetupInitialAdmin: %v", err)
	}

	if _, admin, err := svc.Login(ctx, "admin", "pass123456"); err != nil {
		t.Fatalf("Login: %v", err)
	} else if admin == nil || admin.Email != "admin@example.com" {
		t.Fatalf("unexpected admin: %+v", admin)
	}

	if err := svc.ResetAdminCredentials(ctx, "owner", "reset123456"); err != nil {
		t.Fatalf("ResetAdminCredentials: %v", err)
	}

	if _, _, err := svc.Login(ctx, "admin", "pass123456"); !errors.Is(err, ErrInvalidAdminCredentials) {
		t.Fatalf("expected old credentials to fail, got %v", err)
	}

	if _, admin, err := svc.Login(ctx, "owner", "reset123456"); err != nil {
		t.Fatalf("login with reset credentials: %v", err)
	} else if admin == nil || admin.Email != "owner@local.admin" {
		t.Fatalf("unexpected reset admin: %+v", admin)
	}
}

func TestAdminServiceSetupRequiresEmail(t *testing.T) {
	st := newTestStore(t)
	svc := NewAdminService(st.Admins, st.System, st.AdminAudit)
	ctx := context.Background()

	if err := svc.SetupInitialAdmin(ctx, "admin", "pass123456", ""); err == nil {
		t.Fatal("expected setup to require admin email")
	}
}

func TestAdminServiceTokenSurvivesServiceRestart(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	first := NewAdminService(st.Admins, st.System, st.AdminAudit)
	if err := first.SetupInitialAdmin(ctx, "admin", "pass123456", "admin@example.com"); err != nil {
		t.Fatalf("SetupInitialAdmin: %v", err)
	}

	token, _, err := first.Login(ctx, "admin", "pass123456")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	second := NewAdminService(st.Admins, st.System, st.AdminAudit)
	admin, err := second.Authenticate(ctx, token)
	if err != nil {
		t.Fatalf("Authenticate after restart: %v", err)
	}
	if admin == nil || admin.Username != "admin" {
		t.Fatalf("unexpected admin after restart: %+v", admin)
	}
}

func TestAdminServiceChangePassword(t *testing.T) {
	st := newTestStore(t)
	svc := NewAdminService(st.Admins, st.System, st.AdminAudit)
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
