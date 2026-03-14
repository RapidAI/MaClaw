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
	if got == nil || got.Email != admin.Email {
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

	gotUser, err := st.Users.GetByEmail(ctx, user.Email)
	if err != nil {
		t.Fatalf("get user by email: %v", err)
	}
	if gotUser == nil || gotUser.SN != user.SN {
		t.Fatalf("unexpected user: %#v", gotUser)
	}

	machine := &store.Machine{
		ID:               "m_1",
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
	if gotMachine == nil || gotMachine.Status != "online" {
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

func TestViewerAndLoginTokenRepositoriesRoundTrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	user := &store.User{
		ID:               "u_2",
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
	if gotViewer == nil || gotViewer.UserID != user.ID {
		t.Fatalf("unexpected viewer token: %#v", gotViewer)
	}

	loginToken := &store.LoginToken{
		ID:        "lt_1",
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
	if gotLogin == nil || gotLogin.Email != user.Email {
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
