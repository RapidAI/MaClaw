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
	if !edgeBodyTenantAllowed(rec, req, "tenant-a") {
		t.Fatalf("large body status %d %s", rec.Code, rec.Body.String())
	}
	got, err := io.ReadAll(req.Body)
	if err != nil || !bytes.Equal(got, buried) {
		t.Fatalf("large body was not put back: %d %v", len(got), err)
	}

	conflict := append([]byte(`{"tenant_id":"tenant-a","blob":"`), bytes.Repeat([]byte("x"), 1<<20)...)
	conflict = append(conflict, []byte(`","tenant_id":"other"}`)...)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/desktops/session?tenant_id=tenant-a", bytes.NewReader(conflict))
	req.ContentLength = int64(len(conflict))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	if edgeBodyTenantAllowed(rec, req, "tenant-a") {
		t.Fatal("a second tenant after a large field was accepted")
	}

	wrong := append([]byte(`{"blob":"`), bytes.Repeat([]byte("y"), 1<<20)...)
	wrong = append(wrong, []byte(`","tenant_id":"other"}`)...)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/desktops/notify", bytes.NewReader(wrong))
	req.ContentLength = int64(len(wrong))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	if edgeBodyTenantAllowed(rec, req, "tenant-a") {
		t.Fatal("another tenant after a large field was accepted")
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/desktops/upload?tenant_id=tenant-a", bytes.NewReader([]byte("file-bytes")))
	req.ContentLength = 10
	req.Header.Set("Content-Type", "application/octet-stream")
	rec = httptest.NewRecorder()
	if !edgeBodyTenantAllowed(rec, req, "tenant-a") {
		t.Fatalf("upload status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestEdgeBodyTenantAcceptsTheToolTenantField(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/desktop-services/tool", strings.NewReader(`{"tenant":"tenant-a","user":"alice","action":"file_read","args":{"path":"/home/desktop/a.txt"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(`{"tenant":"tenant-a","user":"alice","action":"file_read","args":{"path":"/home/desktop/a.txt"}}`))
	rec := httptest.NewRecorder()
	if !edgeBodyTenantAllowed(rec, req, "tenant-a") {
		t.Fatalf("tool tenant status %d body %s", rec.Code, rec.Body.String())
	}
	other := httptest.NewRequest(http.MethodPost, "/api/v1/desktop-services/tool", strings.NewReader(`{"tenant":"other","action":"bash"}`))
	other.Header.Set("Content-Type", "application/json")
	other.ContentLength = int64(len(`{"tenant":"other","action":"bash"}`))
	rec = httptest.NewRecorder()
	if edgeBodyTenantAllowed(rec, other, "tenant-a") {
		t.Fatal("another tenant's tool call was accepted")
	}
	both := httptest.NewRequest(http.MethodPost, "/api/v1/desktop-services/tool", strings.NewReader(`{"tenant_id":"tenant-a","tenant":"other"}`))
	both.Header.Set("Content-Type", "application/json")
	both.ContentLength = int64(len(`{"tenant_id":"tenant-a","tenant":"other"}`))
	rec = httptest.NewRecorder()
	if edgeBodyTenantAllowed(rec, both, "tenant-a") {
		t.Fatal("two tenant names were accepted")
	}
}

func TestJSONStringFieldSkipsALongerKey(t *testing.T) {
	raw := []byte(`{"tenant_id":"tenant-a","tenant":"other"}`)
	if jsonStringField(raw, "tenant_id") != "tenant-a" || jsonStringField(raw, "tenant") != "other" {
		t.Fatalf("tenant_id=%q tenant=%q", jsonStringField(raw, "tenant_id"), jsonStringField(raw, "tenant"))
	}
	head := append([]byte(`{"tenant_id":"tenant-a","tenant":"other","blob":"`), bytes.Repeat([]byte("x"), 64)...)
	if id, ok := edgeJSONTenant(head); ok {
		t.Fatalf("disagreeing truncated body = %q", id)
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
