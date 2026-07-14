package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestWelcomeSyncStatusNotLoggedIn(t *testing.T) {
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      corelib.AppConfig{},
	}
	status, err := app.WelcomeSyncStatus(WelcomeSyncRequest{})
	if err != nil {
		t.Fatalf("status err: %v", err)
	}
	if status.LoggedIn {
		t.Fatalf("expected not logged in, got %+v", status)
	}
}

func TestWelcomeSyncPushPullRoundTrip(t *testing.T) {
	var stored []byte
	var storedRev string
	var gotTenant, gotEmail string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		gotTenant = r.Header.Get("X-Maclaw-Tenant-ID")
		gotEmail = r.Header.Get("X-Maclaw-User-Email")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/api/welcome/sync/status"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"has_document":   len(stored) > 0,
				"revision":       storedRev,
				"template_count": 1,
				"limit_bytes":    524288,
			})
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/api/welcome/sync"):
			body, _ := io.ReadAll(r.Body)
			var req map[string]any
			_ = json.Unmarshal(body, &req)
			payload, _ := json.Marshal(req["payload"])
			stored = payload
			storedRev = "rev-1"
			_ = json.NewEncoder(w).Encode(map[string]any{
				"has_document":   true,
				"revision":       storedRev,
				"template_count": 1,
				"updated_at":     "2026-07-14T00:00:00Z",
			})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/api/welcome/sync"):
			if len(stored) == 0 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"code":"WELCOME_SYNC_NOT_FOUND","message":"no cloud welcome document"}`))
				return
			}
			w.Header().Set("X-Welcome-Sync-Revision", storedRev)
			w.Header().Set("X-Welcome-Sync-Template-Count", "1")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(stored)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      srv.URL,
			RemoteViewerToken: "viewer-token",
			RemoteTenantID:    "tenant-a",
			RemoteEmail:       "user@example.com",
		},
	}
	payload := `{"version":2,"kind":"maclaw-welcome-custom-templates","templates":[{"title":"T","body":"body text here"}]}`
	pushed, err := app.WelcomeSyncPush(WelcomeSyncPushRequest{PayloadJSON: payload})
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if !pushed.HasDocument || pushed.Revision != "rev-1" {
		t.Fatalf("pushed = %+v", pushed)
	}
	if gotTenant != "tenant-a" || gotEmail != "user@example.com" {
		t.Fatalf("identity headers tenant=%q email=%q", gotTenant, gotEmail)
	}
	// Pull uses a single GET (no status preflight).
	pulled, err := app.WelcomeSyncPull(WelcomeSyncRequest{})
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if !strings.Contains(pulled.PayloadJSON, "maclaw-welcome-custom-templates") {
		t.Fatalf("pulled payload = %s", pulled.PayloadJSON)
	}
	if pulled.TemplateCount != 1 {
		t.Fatalf("template count = %d", pulled.TemplateCount)
	}
}

func TestWelcomeSyncPullEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/api/welcome/sync") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"WELCOME_SYNC_NOT_FOUND","message":"no cloud welcome document"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      srv.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	_, err := app.WelcomeSyncPull(WelcomeSyncRequest{})
	if err == nil || !strings.Contains(err.Error(), "no cloud welcome") {
		t.Fatalf("expected no cloud document error, got %v", err)
	}
}

func TestWelcomeSyncStatusNetworkSoftFail(t *testing.T) {
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			// Unreachable port — should soft-fail without hard error.
			RemoteHubURL:      "http://127.0.0.1:1",
			RemoteViewerToken: "viewer-token",
		},
	}
	status, err := app.WelcomeSyncStatus(WelcomeSyncRequest{})
	if err != nil {
		t.Fatalf("status should soft-fail, got err %v", err)
	}
	if status.LoggedIn {
		t.Fatalf("expected not logged in on network fail, got %+v", status)
	}
	if strings.TrimSpace(status.Error) == "" {
		t.Fatalf("expected error detail on network fail")
	}
}

func TestWelcomeSyncStatusUnsupportedHub(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      srv.URL,
			RemoteViewerToken: "viewer-token",
		},
	}
	status, err := app.WelcomeSyncStatus(WelcomeSyncRequest{})
	if err != nil {
		t.Fatalf("status err: %v", err)
	}
	if !status.LoggedIn || !strings.Contains(status.Error, "does not support") {
		t.Fatalf("expected unsupported hub status, got %+v", status)
	}
}
