package httpapi

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEdgeBodyTenantKeepsTheTail(t *testing.T) {
	payload := append([]byte(`{"tenant_id":"tenant-a","blob":"`), bytes.Repeat([]byte("x"), 1<<20)...)
	payload = append(payload, []byte(`"}`)...)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/desktops/upload?tenant_id=tenant-a", bytes.NewReader(payload))
	req.ContentLength = int64(len(payload))
	rec := httptest.NewRecorder()
	if !edgeBodyTenantAllowed(rec, req, "tenant-a") {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	got, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("body len %d want %d", len(got), len(payload))
	}
}

func TestEdgeBodyTenantRejectsOtherTenant(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/desktops/upload?tenant_id=other", http.NoBody)
	rec := httptest.NewRecorder()
	if edgeBodyTenantAllowed(rec, req, "tenant-a") {
		t.Fatal("query tenant was accepted")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d", rec.Code)
	}

	body := strings.NewReader(`{"tenant_id":"other"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/desktops/session", body)
	req.ContentLength = int64(body.Len())
	rec = httptest.NewRecorder()
	if edgeBodyTenantAllowed(rec, req, "tenant-a") {
		t.Fatal("json tenant was accepted")
	}
}

func TestEdgeBodyTenantRequiresItsOwnTenant(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/desktops/session", strings.NewReader(`{}`))
	req.ContentLength = 2
	rec := httptest.NewRecorder()
	if edgeBodyTenantAllowed(rec, req, "tenant-a") {
		t.Fatal("empty tenant was accepted")
	}

	buried := append([]byte(`{"blob":"`), bytes.Repeat([]byte("x"), 1<<20)...)
	buried = append(buried, []byte(`","tenant_id":"tenant-a"}`)...)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/desktops/session?tenant_id=tenant-a", bytes.NewReader(buried))
	req.ContentLength = int64(len(buried))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	if edgeBodyTenantAllowed(rec, req, "tenant-a") {
		t.Fatal("tenant past the inspected prefix was accepted")
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/desktops/upload?tenant_id=tenant-a", bytes.NewReader([]byte("file-bytes")))
	req.ContentLength = 10
	req.Header.Set("Content-Type", "application/octet-stream")
	rec = httptest.NewRecorder()
	if !edgeBodyTenantAllowed(rec, req, "tenant-a") {
		t.Fatalf("upload status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestDesktopBotLimitPatchRequiresTheField(t *testing.T) {
	if _, err := desktopBotLimitPatch([]byte(`{}`)); err == nil {
		t.Fatal("missing field cleared the limit")
	}
	limit, err := desktopBotLimitPatch([]byte(`{"desktop_bot_limit":null}`))
	if err != nil || limit != nil {
		t.Fatalf("null limit = %v %v", limit, err)
	}
	limit, err = desktopBotLimitPatch([]byte(`{"desktop_bot_limit":2}`))
	if err != nil || limit == nil || *limit != 2 {
		t.Fatalf("limit = %v %v", limit, err)
	}
	if _, err = desktopBotLimitPatch([]byte(`{"desktop_bot_limit":1.5}`)); err == nil || err.Error() != "desktop_bot_limit must be an integer" {
		t.Fatalf("fraction = %v", err)
	}
	if _, err = desktopBotLimitPatch([]byte(`{"desktop_bot_limit":-1}`)); err == nil || err.Error() != "desktop_bot_limit must be zero or greater" {
		t.Fatalf("negative = %v", err)
	}
}
