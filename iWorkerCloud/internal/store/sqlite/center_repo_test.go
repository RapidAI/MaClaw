package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store"
)

func TestCenterRepoPersistsIWorkerReadinessAcrossHeartbeatAndIntegration(t *testing.T) {
	provider, err := NewProvider(":memory:")
	if err != nil {
		t.Fatalf("NewProvider() error: %v", err)
	}
	defer provider.Close()
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error: %v", err)
	}
	repo := NewStore(provider).Centers
	ctx := context.Background()

	createdAt := time.Now().UTC().Truncate(time.Second)
	center := &store.Center{
		ID:                  "ctr_1",
		CompanyName:         "Acme",
		AdminEmail:          "admin@example.com",
		BaseURL:             "https://center.example",
		SupportsMultiTenant: true,
		TenantCount:         1,
		CloudControlMode:    "cloud_managed",
		LastSyncStatus:      "registered",
		Status:              "active",
		SecretHash:          "hash",
		CreatedAt:           createdAt,
		UpdatedAt:           createdAt,
	}
	if err := repo.Create(ctx, center); err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	center.LastSyncStatus = "heartbeat_ok"
	center.IWorkerReady = true
	center.IWorkerReadinessStatus = "ready"
	center.IWorkerTenantCount = 2
	center.IWorkerRoleCount = 3
	center.IWorkerColleagueCount = 4
	center.IWorkerLocalAccountCount = 5
	center.IWorkerReadinessJSON = `{"ready":true,"status":"ready","tenant_count":2}`
	if err := repo.UpdateHeartbeat(ctx, center); err != nil {
		t.Fatalf("UpdateHeartbeat() error: %v", err)
	}

	got, err := repo.GetByID(ctx, "ctr_1")
	if err != nil {
		t.Fatalf("GetByID() after heartbeat error: %v", err)
	}
	if !got.IWorkerReady || got.IWorkerReadinessStatus != "ready" || got.IWorkerColleagueCount != 4 || got.IWorkerReadinessJSON == "" {
		t.Fatalf("readiness after heartbeat = %+v", got)
	}
	if got.LastHeartbeat.IsZero() || got.LastSyncStatus != "heartbeat_ok" {
		t.Fatalf("heartbeat fields = %+v", got)
	}

	got.BaseURL = "https://new-center.example"
	got.TenantCount = 3
	got.LastSyncStatus = "configured"
	if err := repo.UpdateIntegration(ctx, got); err != nil {
		t.Fatalf("UpdateIntegration() error: %v", err)
	}
	updated, err := repo.GetByID(ctx, "ctr_1")
	if err != nil {
		t.Fatalf("GetByID() after integration error: %v", err)
	}
	if updated.BaseURL != "https://new-center.example" || updated.TenantCount != 3 || updated.LastSyncStatus != "configured" {
		t.Fatalf("integration fields = %+v", updated)
	}
	if !updated.IWorkerReady || updated.IWorkerReadinessStatus != "ready" || updated.IWorkerRoleCount != 3 || updated.IWorkerReadinessJSON == "" {
		t.Fatalf("readiness should survive integration update: %+v", updated)
	}
}
