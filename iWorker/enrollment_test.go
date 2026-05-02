package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscoverCenterEnrollmentFetchesTenantScopedOptions(t *testing.T) {
	seenTenantHeaders := make(chan string, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/tenants":
			_ = json.NewEncoder(w).Encode(map[string]any{"tenants": []map[string]string{{"id": "tenant-a", "company_name": "Acme"}}})
		case "/diworker-auth/methods":
			_ = json.NewEncoder(w).Encode(map[string]any{"methods": []map[string]any{
				{"method": "local", "label": "Local account", "enabled": true, "implemented": true, "status": "ready"},
				{"method": "ldap", "label": "LDAP", "enabled": true, "implemented": true, "status": "available"},
				{"method": "oidc", "label": "OIDC / OAuth SSO", "enabled": false, "implemented": false, "status": "reserved"},
			}})
		case "/client/roles":
			seenTenantHeaders <- r.Header.Get("X-Tenant-ID")
			_ = json.NewEncoder(w).Encode(map[string]any{"roles": []map[string]any{{"id": "role-ops", "name": "Ops", "code": "ops"}}})
		case "/client/colleagues":
			seenTenantHeaders <- r.Header.Get("X-Tenant-ID")
			_ = json.NewEncoder(w).Encode(map[string]any{"colleagues": []map[string]any{{"id": "worker-ops", "name": "Ops iWorker", "role_id": "role-ops", "role_name": "Ops", "role_code": "ops"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	got, err := discoverCenterEnrollment(srv.URL+"/", "", 3)
	if err != nil {
		t.Fatalf("discoverCenterEnrollment returned error: %v", err)
	}
	if got.BaseURL != srv.URL || got.SelectedTenantID != "tenant-a" {
		t.Fatalf("discovery identity = %+v", got)
	}
	if len(got.Tenants) != 1 || got.Tenants[0].ID != "tenant-a" {
		t.Fatalf("tenants = %+v", got.Tenants)
	}
	if len(got.Roles) != 1 || got.Roles[0].Code != "ops" {
		t.Fatalf("roles = %+v", got.Roles)
	}
	if len(got.Colleagues) != 1 || got.Colleagues[0].ID != "worker-ops" {
		t.Fatalf("colleagues = %+v", got.Colleagues)
	}
	if len(got.AuthMethods) != 3 || got.AuthMethods[0].Method != "local" || got.AuthMethods[2].Method != "oidc" || got.AuthMethods[2].Implemented {
		t.Fatalf("auth methods = %+v", got.AuthMethods)
	}
	for i := 0; i < 2; i++ {
		if header := <-seenTenantHeaders; header != "tenant-a" {
			t.Fatalf("tenant header = %q, want tenant-a", header)
		}
	}
}

func TestApplyCenterEnrollmentWritesRunnableSettings(t *testing.T) {
	home := setTestHome(t)
	var gotTenant, gotMethod, gotUsername, gotPassword, gotWorker string
	seenHeartbeat := make(chan CenterAgentInstanceHeartbeatRequest, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/diworker-auth/enrollment/verify":
			gotTenant = r.Header.Get("X-Tenant-ID")
			body, _ := io.ReadAll(r.Body)
			var req map[string]string
			_ = json.Unmarshal(body, &req)
			gotMethod = req["method"]
			gotUsername = req["username"]
			gotPassword = req["password"]
			gotWorker = req["worker_id"]
			_ = json.NewEncoder(w).Encode(map[string]any{"verified": true, "authenticated": true, "worker_id": gotWorker})
		case "/runtime/iworker/instances/heartbeat":
			var req CenterAgentInstanceHeartbeatRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode heartbeat: %v", err)
			}
			seenHeartbeat <- req
			_ = json.NewEncoder(w).Encode(CenterAgentInstanceHeartbeatResult{Instance: CenterAgentInstance{TenantID: r.Header.Get("X-Tenant-ID"), WorkerID: req.WorkerID, InstanceID: req.InstanceID, Role: req.Role, Status: req.Status, WorkStatus: req.WorkStatus}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	app := NewApp()
	settings, err := app.ApplyCenterEnrollment(ApplyCenterEnrollmentRequest{
		BaseURL:         srv.URL + "/",
		TenantID:        "tenant-a",
		DepartmentID:    "ops",
		WorkerID:        "worker-ops",
		RoleName:        "Ops iWorker",
		RoleDescription: "Handles operating tasks",
		TimeoutSec:      20,
		AuthMethod:      "local",
		AuthUsername:    "alice",
		AuthPassword:    "secret",
	})
	if err != nil {
		t.Fatalf("ApplyCenterEnrollment returned error: %v", err)
	}
	if !settings.Center.Enabled || settings.Center.BaseURL != srv.URL {
		t.Fatalf("center settings = %+v", settings.Center)
	}
	if settings.Center.Host == "" || settings.Center.Port == 0 {
		t.Fatalf("host/port = %s/%d", settings.Center.Host, settings.Center.Port)
	}
	if settings.Center.TenantID != "tenant-a" || settings.Center.DepartmentID != "ops" || settings.Center.WorkerID != "worker-ops" {
		t.Fatalf("binding = %+v", settings.Center)
	}
	if settings.RoleProfile.Name != "Ops iWorker" || settings.RoleProfile.Description != "Handles operating tasks" {
		t.Fatalf("role profile = %+v", settings.RoleProfile)
	}
	if gotTenant != "tenant-a" || gotMethod != "local" || gotUsername != "alice" || gotPassword != "secret" || gotWorker != "worker-ops" {
		t.Fatalf("verify request tenant=%q method=%q username=%q password=%q worker=%q", gotTenant, gotMethod, gotUsername, gotPassword, gotWorker)
	}
	select {
	case heartbeat := <-seenHeartbeat:
		if heartbeat.WorkerID != "worker-ops" || heartbeat.WorkStatus == nil {
			t.Fatalf("initial heartbeat = %+v", heartbeat)
		}
	default:
		t.Fatal("ApplyCenterEnrollment should send an initial agent runtime heartbeat")
	}

	loaded, err := app.LoadDiWorkerSettings()
	if err != nil {
		t.Fatalf("LoadDiWorkerSettings returned error: %v", err)
	}
	if loaded.Center.WorkerID != "worker-ops" || loaded.Center.TenantID != "tenant-a" {
		t.Fatalf("persisted settings = %+v", loaded.Center)
	}
	if home == "" {
		t.Fatal("test home should be set")
	}
}

func TestApplyCenterEnrollmentRequiresWorkerID(t *testing.T) {
	setTestHome(t)
	app := NewApp()
	if _, err := app.ApplyCenterEnrollment(ApplyCenterEnrollmentRequest{BaseURL: "http://127.0.0.1:9377"}); err == nil {
		t.Fatal("ApplyCenterEnrollment should require worker_id")
	}
}

func TestApplyCenterEnrollmentRejectsFailedIdentityVerification(t *testing.T) {
	setTestHome(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"verified": false, "authenticated": true, "error": "account is not allowed to bind this iWorker"})
	}))
	defer srv.Close()

	app := NewApp()
	_, err := app.ApplyCenterEnrollment(ApplyCenterEnrollmentRequest{
		BaseURL:      srv.URL,
		TenantID:     "tenant-a",
		WorkerID:     "worker-finance",
		AuthMethod:   "local",
		AuthUsername: "alice",
		AuthPassword: "secret",
	})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("ApplyCenterEnrollment error = %v, want not allowed", err)
	}
}
