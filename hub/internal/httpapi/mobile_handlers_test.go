package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMobileBootstrapHandlerRequiresViewerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/bootstrap", nil)
	rec := httptest.NewRecorder()

	MobileBootstrapHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rec.Body.String(), "UNAUTHORIZED") {
		t.Fatalf("body = %s, want UNAUTHORIZED", rec.Body.String())
	}
}

func TestMobileDigitalEmployeesHandlerRequiresViewerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/digital-employees", nil)
	rec := httptest.NewRecorder()

	MobileDigitalEmployeesHandler(nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rec.Body.String(), "UNAUTHORIZED") {
		t.Fatalf("body = %s, want UNAUTHORIZED", rec.Body.String())
	}
}
