package tenant

import (
	"net/http/httptest"
	"testing"
)

func TestRequestTenantIDPriority(t *testing.T) {
	req := httptest.NewRequest("GET", "/resource?tenant_id=query-tenant", nil)
	req.Header.Set("X-Tenant-ID", "header-tenant")
	req = req.WithContext(WithTenantID(req.Context(), "context-tenant"))
	if got := RequestTenantID(req); got != "context-tenant" {
		t.Fatalf("context priority = %q, want context-tenant", got)
	}

	req = httptest.NewRequest("GET", "/resource?tenant_id=query-tenant", nil)
	req.Header.Set("X-Tenant-ID", "header-tenant")
	if got := RequestTenantID(req); got != "query-tenant" {
		t.Fatalf("query fallback = %q, want query-tenant", got)
	}

	req = httptest.NewRequest("GET", "/resource", nil)
	req.Header.Set("X-Tenant-ID", "header-tenant")
	if got := RequestTenantID(req); got != "header-tenant" {
		t.Fatalf("header fallback = %q, want header-tenant", got)
	}

	req = httptest.NewRequest("GET", "/resource", nil)
	req = req.WithContext(WithTenantID(req.Context(), "context-tenant"))
	if got := RequestTenantID(req); got != "context-tenant" {
		t.Fatalf("context fallback = %q, want context-tenant", got)
	}

	if got := RequestTenantID(nil); got != "default" {
		t.Fatalf("nil request = %q, want default", got)
	}
}
