package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscoverCenterEnrollmentFetchesTenantScopedOptions(t *testing.T) {
	seenTenantHeaders := make(chan string, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/tenants":
			_ = json.NewEncoder(w).Encode(map[string]any{"tenants": []map[string]string{{"id": "tenant-a", "company_name": "Acme"}}})
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
	for i := 0; i < 2; i++ {
		if header := <-seenTenantHeaders; header != "tenant-a" {
			t.Fatalf("tenant header = %q, want tenant-a", header)
		}
	}
}

func TestApplyCenterEnrollmentWritesRunnableSettings(t *testing.T) {
	home := setTestHome(t)
	app := NewApp()
	settings, err := app.ApplyCenterEnrollment(ApplyCenterEnrollmentRequest{
		BaseURL:         "http://127.0.0.1:9377/",
		TenantID:        "tenant-a",
		DepartmentID:    "ops",
		WorkerID:        "worker-ops",
		RoleName:        "Ops iWorker",
		RoleDescription: "Handles operating tasks",
		TimeoutSec:      20,
	})
	if err != nil {
		t.Fatalf("ApplyCenterEnrollment returned error: %v", err)
	}
	if !settings.Center.Enabled || settings.Center.BaseURL != "http://127.0.0.1:9377" {
		t.Fatalf("center settings = %+v", settings.Center)
	}
	if settings.Center.Host != "127.0.0.1" || settings.Center.Port != 9377 {
		t.Fatalf("host/port = %s/%d", settings.Center.Host, settings.Center.Port)
	}
	if settings.Center.TenantID != "tenant-a" || settings.Center.DepartmentID != "ops" || settings.Center.WorkerID != "worker-ops" {
		t.Fatalf("binding = %+v", settings.Center)
	}
	if settings.RoleProfile.Name != "Ops iWorker" || settings.RoleProfile.Description != "Handles operating tasks" {
		t.Fatalf("role profile = %+v", settings.RoleProfile)
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
