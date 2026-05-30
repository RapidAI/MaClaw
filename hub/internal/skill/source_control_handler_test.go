package skill

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestSourceControlRoutesUseTenantRuntimeUserID(t *testing.T) {
	svc := NewSourceControlService(newMockSystem())
	mux := http.NewServeMux()
	RegisterRoutes(mux, svc, func(h http.HandlerFunc) http.HandlerFunc { return h })

	body := bytes.NewBufferString(`{"enabled":true,"allowed_sources":["local"]}`)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/skill-sources/tenants/tenant-a/users/runtime-user-1", body)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status = %d body=%s", rr.Code, rr.Body.String())
	}

	got := svc.ResolveForUser(req.Context(), "runtime-user-1", "tenant-a")
	if !reflect.DeepEqual(got, []string{"local"}) {
		t.Fatalf("ResolveForUser = %#v, want local", got)
	}
	if fallback := svc.ResolveForUser(req.Context(), "employee@example.com", "tenant-a"); fallback != nil {
		t.Fatalf("email fallback policy = %#v, want nil", fallback)
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/admin/skill-sources/tenants/tenant-a/users/runtime-user-1/resolve", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("resolve status = %d body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		TenantID       string   `json:"tenant_id"`
		UserID         string   `json:"user_id"`
		AllowedSources []string `json:"allowed_sources"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.TenantID != "tenant-a" || payload.UserID != "runtime-user-1" || !reflect.DeepEqual(payload.AllowedSources, []string{"local"}) {
		t.Fatalf("resolve payload = %#v", payload)
	}
}

func TestSourceControlRoutesRejectTenantMismatchForTenantAdmin(t *testing.T) {
	svc := NewSourceControlService(newMockSystem())
	mux := http.NewServeMux()
	RegisterRoutes(mux, svc, func(h http.HandlerFunc) http.HandlerFunc { return h }, func(*http.Request) (string, bool) {
		return "tenant-a", false
	})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/skill-sources/tenants/tenant-b/users/runtime-user-1", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}
