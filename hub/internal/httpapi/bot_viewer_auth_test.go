package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
)

// botViewerStub is the MaClawSrv a bot is provisioned against. Creating a bot
// provisions an instance there, so the bot-list tests need one to stand in.
func botViewerStub(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/me":
			_, _ = w.Write([]byte(`{"id":"svc","tenant_id":"maclaw-tenant"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/tenants/maclaw-tenant/users":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"user_member"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/credentials"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"api_key":"key-member","api_secret":"sec-member"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-member","principal":{"user_id":"user_member"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/instances":
			_, _ = io.WriteString(w, `{"id":"inst_alice"}`)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			_, _ = w.Write([]byte(`{"id":"inst_alice"}`))
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// grantedBotService returns a service with one bot owned by alice, provisioned
// against a stub MaClawSrv.
func grantedBotService(t *testing.T, srv *httptest.Server) *botmgmt.Service {
	t.Helper()
	svc := botmgmt.NewService(&botSettingsMem{})
	svc.HTTP = srv.Client()
	ctx := context.Background()
	if _, err := svc.SaveConnection(ctx, "tenant-a", srv.URL, "secret-token", true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveAdminSecret(ctx, "tenant-a", "admin-secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateGrant(ctx, "tenant-a", botmgmt.Grant{Scope: botmgmt.ScopeUser, TargetID: "alice"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateBotForUser(ctx, "tenant-a", "alice", "Ada", "research"); err != nil {
		t.Fatal(err)
	}
	return svc
}

// The mobile and web clients only hold a viewer token. They must reach their
// own bots with it, so a viewer-only request has to authenticate and resolve to
// the same tenant and user a machine token would.
func TestBotListAcceptsViewerToken(t *testing.T) {
	svc := grantedBotService(t, botViewerStub(t))
	authn := fakeVEMachineAuth{
		token: "viewer-token",
		viewer: &auth.ViewerPrincipal{
			TenantID: "tenant-a",
			UserID:   "alice",
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/bots", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()
	ListBotsHandler(svc, authn).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"Ada"`) {
		t.Fatalf("viewer token did not reach the owner bots: %s", rec.Body.String())
	}
}

// A viewer token must not widen access: it resolves to its own user only, so
// another user's bots stay invisible.
func TestBotListViewerTokenIsScopedToItsOwnUser(t *testing.T) {
	svc := grantedBotService(t, botViewerStub(t))
	authn := fakeVEMachineAuth{
		token: "viewer-token",
		viewer: &auth.ViewerPrincipal{
			TenantID: "tenant-a",
			UserID:   "mallory",
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/bots", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()
	ListBotsHandler(svc, authn).ServeHTTP(rec, req)

	if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), `"name":"Ada"`) {
		t.Fatalf("another user's bot leaked to a viewer token: %s", rec.Body.String())
	}
}

// An unrecognized bearer token must stay unauthorized on both paths.
func TestBotListRejectsUnknownToken(t *testing.T) {
	svc := grantedBotService(t, botViewerStub(t))
	authn := fakeVEMachineAuth{
		token:      "viewer-token",
		principals: map[string]*auth.MachinePrincipal{},
		viewer:     &auth.ViewerPrincipal{TenantID: "tenant-a", UserID: "alice"},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/bots", nil)
	req.Header.Set("Authorization", "Bearer someone-elses-token")
	rec := httptest.NewRecorder()
	ListBotsHandler(svc, authn).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// A machine request keeps working after the viewer fallback was added: the
// machine ID is still what resolves it, and the owner is unchanged.
func TestBotListStillAcceptsMachineCredential(t *testing.T) {
	svc := grantedBotService(t, botViewerStub(t))
	authn := fakeVEMachineAuth{token: "machine-token", principals: map[string]*auth.MachinePrincipal{
		"machine-1": {TenantID: "tenant-a", UserID: "alice", MachineID: "machine-1"},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/bots", nil)
	req.Header.Set("Authorization", "Bearer machine-token")
	req.Header.Set("X-Machine-ID", "machine-1")
	rec := httptest.NewRecorder()
	ListBotsHandler(svc, authn).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"Ada"`) {
		t.Fatalf("machine auth regressed: status=%d body=%s", rec.Code, rec.Body.String())
	}
}