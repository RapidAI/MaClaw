package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/config"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	storesqlite "github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

func TestCenterSkillMarketAuthenticateHandlerValidatesViewerAndMachineOwnership(t *testing.T) {
	identity, centerSvc, st, token := newCenterSkillMarketAuthTestServices(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := st.Users.Create(ctx, &store.User{ID: "user-1", TenantID: store.DefaultTenantID, Email: "user@example.com", Status: "active", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := st.Machines.Create(ctx, &store.Machine{ID: "machine-1", TenantID: store.DefaultTenantID, UserID: "user-1", ClientID: "desktop-1", MachineTokenHash: "unused", Status: "offline", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	viewerToken, err := identity.IssueViewerTokenForUser(ctx, "user-1")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/center/skillmarket-authenticate", bytes.NewBufferString(`{"machine_id":"machine-1"}`))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("X-HubCenter-Verify", token)
	rec := httptest.NewRecorder()
	CenterSkillMarketAuthenticateHandler(centerSvc, identity)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCenterSkillMarketAuthenticateHandlerRejectsForeignMachine(t *testing.T) {
	identity, centerSvc, st, token := newCenterSkillMarketAuthTestServices(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := st.Users.Create(ctx, &store.User{ID: "user-1", TenantID: store.DefaultTenantID, Email: "user@example.com", Status: "active", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := st.Machines.Create(ctx, &store.Machine{ID: "machine-2", TenantID: store.DefaultTenantID, UserID: "user-2", ClientID: "desktop-2", MachineTokenHash: "unused", Status: "offline", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	viewerToken, err := identity.IssueViewerTokenForUser(ctx, "user-1")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/center/skillmarket-authenticate", bytes.NewBufferString(`{"machine_id":"machine-2"}`))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("X-HubCenter-Verify", token)
	rec := httptest.NewRecorder()
	CenterSkillMarketAuthenticateHandler(centerSvc, identity)(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s, want unauthorized", rec.Code, rec.Body.String())
	}
}

func newCenterSkillMarketAuthTestServices(t *testing.T) (*auth.IdentityService, *center.Service, *store.Store, string) {
	t.Helper()
	provider, err := storesqlite.NewProvider(storesqlite.Config{DSN: t.TempDir() + "/hub.db"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := storesqlite.RunMigrations(provider.Write); err != nil {
		t.Fatal(err)
	}
	st := storesqlite.NewStore(provider)
	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "")
	secret := "hub-secret"
	if err := st.System.Set(context.Background(), "center_registration", `{"registered":true,"hub_id":"hub-1","hub_secret":"`+secret+`"}`); err != nil {
		t.Fatal(err)
	}
	return identity, center.NewService(config.Default(), st.System), st, sha256HexForCenterSkillMarketTest(secret)
}

func sha256HexForCenterSkillMarketTest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
