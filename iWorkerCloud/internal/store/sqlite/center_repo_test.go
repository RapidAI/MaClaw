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

func TestCenterRepoDeleteCleansDependentRows(t *testing.T) {
	provider, err := NewProvider(":memory:")
	if err != nil {
		t.Fatalf("NewProvider() error: %v", err)
	}
	defer provider.Close()
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error: %v", err)
	}
	ctx := context.Background()
	repo := NewStore(provider).Centers
	now := time.Now().UTC().Truncate(time.Second)
	center := &store.Center{ID: "ctr_delete", CompanyName: "Delete Me", AdminEmail: "admin@example.com", Status: "active", SecretHash: "hash", CreatedAt: now, UpdatedAt: now}
	if err := repo.Create(ctx, center); err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO licenses (id, center_id, modules, type, expires_at, is_long_term, certificate, created_at) VALUES (?, ?, '[]', 'manual', ?, 0, '', ?)`, "lic_delete", center.ID, now.AddDate(0, 1, 0).Format(time.RFC3339), now.Format(time.RFC3339)); err != nil {
		t.Fatalf("insert license: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `CREATE TABLE center_provider_assignments (center_id TEXT NOT NULL, provider_id TEXT NOT NULL, PRIMARY KEY(center_id, provider_id))`); err != nil {
		t.Fatalf("create assignment table: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO center_provider_assignments (center_id, provider_id) VALUES (?, ?)`, center.ID, "provider-1"); err != nil {
		t.Fatalf("insert assignment: %v", err)
	}
	if err := repo.Delete(ctx, center.ID); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	var centerCount, licenseCount, assignmentCount int
	_ = provider.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM centers WHERE id=?`, center.ID).Scan(&centerCount)
	_ = provider.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM licenses WHERE center_id=?`, center.ID).Scan(&licenseCount)
	_ = provider.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM center_provider_assignments WHERE center_id=?`, center.ID).Scan(&assignmentCount)
	if centerCount != 0 || licenseCount != 0 || assignmentCount != 0 {
		t.Fatalf("dependent rows left: centers=%d licenses=%d assignments=%d", centerCount, licenseCount, assignmentCount)
	}
}
