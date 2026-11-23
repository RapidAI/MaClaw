package desktopd

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisteredDesktopUploadKeepsTheWholeBody(t *testing.T) {
	link := &HubLink{Key: "primary-token-value-0123456789"}
	link.creds.TenantID = "tenant-a"
	var uploaded int
	svc := &Service{
		HubLink:       link,
		ToolWorkspace: true,
		Run: func(context.Context, ...string) (string, error) {
			return "true", nil
		},
		RunCommand: func(_ context.Context, in io.Reader, _ ...string) (string, int, error) {
			if in != nil {
				raw, err := io.ReadAll(in)
				if err != nil {
					return "", 0, err
				}
				uploaded = len(raw)
			}
			return "", 0, nil
		},
	}
	handler := Handler(svc, "primary-token-value-0123456789", t.TempDir(), nil, nil)
	payload := bytes.Repeat([]byte("x"), 2<<20)
	req := httptest.NewRequest(http.MethodPost, "/v1/desktops/upload?tenant_id=tenant-a&user_id=alice&path=~%2F%E7%B4%A0%E6%9D%90%2Fa.bin", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer primary-token-value-0123456789")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || uploaded != len(payload) {
		t.Fatalf("status=%d uploaded=%d body=%s", rec.Code, uploaded, rec.Body.String())
	}
}

func TestRegisteredDesktopRejectsAnotherTenant(t *testing.T) {
	dir := t.TempDir()
	const token = "primary-token-value-0123456789"
	link := &HubLink{StatePath: filepath.Join(dir, "hub_link.json"), Key: token, Self: "127.0.0.1:9"}
	handler := Handler(&Service{HubLink: link}, token, dir, nil, nil)

	status := func(method, target, body string) int {
		t.Helper()
		var reader *strings.Reader
		if body != "" {
			reader = strings.NewReader(body)
		}
		var req *http.Request
		if reader == nil {
			req = httptest.NewRequest(method, target, nil)
		} else {
			req = httptest.NewRequest(method, target, reader)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}
	if status(http.MethodGet, "/v1/health?tenant_id=tenant-b", "") != http.StatusOK {
		t.Fatal("an unregistered desktop refused another tenant")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"node_id":"node123","node_secret":"secsecsecsecsecsecsecsecsecsec12","tenant_id":"tenant-a"}`))
	}))
	defer srv.Close()
	link.Client = srv.Client()
	if _, err := link.EnrollWith(t.Context(), HubLinkEnroll{HubURL: srv.URL, Code: "CODE"}); err != nil {
		t.Fatal(err)
	}
	if link.BoundTenant() != "tenant-a" {
		t.Fatalf("bound tenant %q", link.BoundTenant())
	}
	if status(http.MethodGet, "/v1/health", "") != http.StatusOK {
		t.Fatal("a health check with no tenant was refused")
	}
	if status(http.MethodGet, "/v1/health?tenant_id=tenant-a", "") != http.StatusOK {
		t.Fatal("the bound tenant was refused")
	}
	if status(http.MethodGet, "/v1/health?tenant_id=tenant-b", "") != http.StatusForbidden {
		t.Fatal("another tenant's query was accepted")
	}
	if status(http.MethodPost, "/v1/health", `{"tenant_id":"tenant-b","user_id":"u"}`) != http.StatusForbidden {
		t.Fatal("another tenant's body was accepted")
	}
	if status(http.MethodPost, "/v1/health", `{"tenant_id":"tenant-a","user_id":"u"}`) == http.StatusForbidden {
		t.Fatal("the bound tenant's body was refused")
	}
	if err := link.Leave(); err != nil {
		t.Fatal(err)
	}
	if status(http.MethodGet, "/v1/health?tenant_id=tenant-b", "") != http.StatusOK {
		t.Fatal("leaving the hub kept the tenant lock")
	}
}
