package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestMigrationConfigFallsBackToHubCenterURL(t *testing.T) {
	hub := httptest.NewServer(http.NotFoundHandler())
	defer hub.Close()

	var center *httptest.Server
	center = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/quality":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"routable":      true,
				"quality_score": 1000,
				"features": map[string]bool{
					"can_resolve": true,
				},
			})
		case "/api/client/hubcenters":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"urls": []string{center.URL},
			})
		case "/api/entry/resolve":
			var req map[string]string
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode resolve body: %v", err)
			}
			if req["email"] != "user@example.com" {
				t.Fatalf("resolve email = %q, want user@example.com", req["email"])
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"email":          "user@example.com",
				"mode":           "direct",
				"default_hub_id": "hub-1",
				"hubs": []map[string]string{
					{
						"hub_id":   "hub-1",
						"name":     "Hub 1",
						"base_url": hub.URL,
						"status":   "online",
					},
				},
			})
		default:
			t.Fatalf("unexpected hub center path: %s", r.URL.Path)
		}
	}))
	defer center.Close()

	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, corelib.AppConfig{
		RemoteHubCenterURL: center.URL,
		RemoteEmail:        "user@example.com",
		RemoteMachineID:    "machine-1",
		RemoteMachineName:  "Machine 1",
		RemoteViewerToken:  "viewer-token",
		RemoteTenantID:     tenant.ID,
	}); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}

	cfg, err := (&HTTPServer{svc: svc}).migrationConfig(context.Background(), principal)
	if err != nil {
		t.Fatalf("migrationConfig: %v", err)
	}
	if cfg.HubURL != hub.URL {
		t.Fatalf("HubURL = %q, want resolved Hub URL %q", cfg.HubURL, hub.URL)
	}
	if cfg.MachineID != "machine-1" || cfg.ViewerToken != "viewer-token" {
		t.Fatalf("unexpected migration config: %#v", cfg)
	}
}

func TestRunMigrationImportRequiresPasswordForReadyExport(t *testing.T) {
	var aborted int32
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/migration/imports/export-1/claim":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"export":{"export_id":"export-1","status":"ready","chunk_count":1,"chunk_size":8,"encrypted_size":8,"encrypted_sha256":"abc","plain_sha256":"def"}}`))
		case "/api/v1/migration/imports/export-1/abort":
			atomic.AddInt32(&aborted, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Fatalf("unexpected hub path: %s", r.URL.Path)
		}
	}))
	defer hub.Close()

	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	srv := &HTTPServer{svc: svc}
	_, err = srv.runMigrationImport(context.Background(), agentservice.Principal{TenantID: "tenant-1", UserID: "user-1"}, migrationClientConfig{
		HubURL:      hub.URL,
		ViewerToken: "viewer-token",
		MachineID:   "machine-1",
	}, "export-1", "", func(float64, string) {})
	if err == nil || !strings.Contains(err.Error(), "migration password is required") {
		t.Fatalf("expected password required error, got %v", err)
	}
	if atomic.LoadInt32(&aborted) != 1 {
		t.Fatalf("expected claimed export to be aborted once, got %d", aborted)
	}
}

func TestHandleMigrationImportRejectsMissingPasswordBeforeHubAccess(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/migration/import", strings.NewReader(`{"export_id":"export-1"}`))
	rec := httptest.NewRecorder()

	(&HTTPServer{}).handleMigrationImport(rec, req, agentservice.Principal{TenantID: "tenant-1", UserID: "user-1"})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !strings.Contains(body["error"], "migration password is required") {
		t.Fatalf("error = %q", body["error"])
	}
}

func TestRunMigrationImportCleanupRetryDoesNotRequirePassword(t *testing.T) {
	var completed int32
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/migration/imports/export-1/claim":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"export":{"export_id":"export-1","status":"imported","claimed_by_machine_id":"machine-1"}}`))
		case "/api/v1/migration/imports/export-1/complete":
			atomic.AddInt32(&completed, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Fatalf("unexpected hub path: %s", r.URL.Path)
		}
	}))
	defer hub.Close()

	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	result, err := (&HTTPServer{svc: svc}).runMigrationImport(context.Background(), agentservice.Principal{TenantID: "tenant-1", UserID: "user-1"}, migrationClientConfig{
		HubURL:      hub.URL,
		ViewerToken: "viewer-token",
		MachineID:   "machine-1",
	}, "export-1", "", func(float64, string) {})
	if err != nil {
		t.Fatalf("runMigrationImport cleanup retry: %v", err)
	}
	if result["cleanup_retried"] != true {
		t.Fatalf("cleanup_retried = %#v, want true; result=%#v", result["cleanup_retried"], result)
	}
	if atomic.LoadInt32(&completed) != 1 {
		t.Fatalf("expected complete to be called once, got %d", completed)
	}
}
