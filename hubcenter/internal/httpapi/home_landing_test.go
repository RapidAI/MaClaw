package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRouterServesHomeLandingPage guards the public landing page served at
// the exact site root "/". The page must render for anonymous visitors, deep
// paths must keep their real 404 (the landing handler must never mask other
// routes), and non-GET/HEAD methods must be rejected.
func TestRouterServesHomeLandingPage(t *testing.T) {
	svc := newHubCenterHTTPTestServices(t)

	rec := httptest.NewRecorder()
	svc.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / => %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Hub Center") || !strings.Contains(body, "MaClaw") {
		t.Fatalf("landing page missing expected brand content")
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("landing content-type = %q, want text/html", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-cache") {
		t.Fatalf("landing cache-control = %q, want no-cache directive", cc)
	}

	rec = httptest.NewRecorder()
	svc.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/definitely-not-a-route", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /definitely-not-a-route => %d, want 404 (landing must not mask other routes)", rec.Code)
	}

	rec = httptest.NewRecorder()
	svc.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}")))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST / => %d, want 405", rec.Code)
	}
}
