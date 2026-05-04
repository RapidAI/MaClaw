package adminauth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/db"
)

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	return NewHandler(provider.Write, provider.Read)
}

func TestAuthenticateWithContextInjectsSessionTenant(t *testing.T) {
	h := newTestHandler(t)
	token := h.sessions.create("admin-1", "admin", "tenant-session")
	req := httptest.NewRequest(http.MethodGet, "/admin/bootstrap/status", nil)
	req.Header.Set("X-Tenant-ID", "tenant-header")
	req.AddCookie(&http.Cookie{Name: "iwc_session", Value: token})

	withTenant, ok := h.AuthenticateWithContext(req)
	if !ok {
		t.Fatal("expected request to authenticate")
	}
	if got := tenant.TenantIDFromContext(withTenant.Context()); got != "tenant-session" {
		t.Fatalf("context tenant = %q, want tenant-session", got)
	}
	if got := tenant.RequestTenantID(withTenant); got != "tenant-session" {
		t.Fatalf("request tenant = %q, want tenant-session", got)
	}
}

func TestAuthenticateWithContextRejectsMissingSession(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/admin/bootstrap/status", nil)

	withTenant, ok := h.AuthenticateWithContext(req)
	if ok {
		t.Fatal("expected request without session to be unauthenticated")
	}
	if withTenant != req {
		t.Fatal("unauthenticated request should be returned unchanged")
	}
}
