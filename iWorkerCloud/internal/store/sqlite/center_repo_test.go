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
	if _, err := provider.Write.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS compute_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatalf("create compute settings table: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO compute_settings (key, value) VALUES (?, ?), (?, ?)`, "compute_permission_"+center.ID, "allowed", "force_sync_"+center.ID, "true"); err != nil {
		t.Fatalf("insert compute settings: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS token_usage_records (id TEXT PRIMARY KEY, center_id TEXT NOT NULL, created_at TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatalf("create token usage table: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO token_usage_records (id, center_id, created_at) VALUES (?, ?, ?)`, "usage-delete", center.ID, now.Format(time.RFC3339)); err != nil {
		t.Fatalf("insert token usage: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS cost_summaries (id TEXT PRIMARY KEY, center_id TEXT NOT NULL, total_cost INTEGER NOT NULL DEFAULT 0)`); err != nil {
		t.Fatalf("create cost summary table: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO cost_summaries (id, center_id, total_cost) VALUES (?, ?, ?)`, "cost-delete", center.ID, 99); err != nil {
		t.Fatalf("insert cost summary: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO skill_market_skills (id, name, description, source_center_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, "skill-delete", "Delete Skill", "published by center", center.ID, now.Format(time.RFC3339), now.Format(time.RFC3339)); err != nil {
		t.Fatalf("insert sourced skill: %v", err)
	}
	if err := repo.Delete(ctx, center.ID); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	var centerCount, licenseCount, assignmentCount, computeSettingCount, tokenUsageCount, costSummaryCount int
	_ = provider.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM centers WHERE id=?`, center.ID).Scan(&centerCount)
	_ = provider.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM licenses WHERE center_id=?`, center.ID).Scan(&licenseCount)
	_ = provider.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM center_provider_assignments WHERE center_id=?`, center.ID).Scan(&assignmentCount)
	_ = provider.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM compute_settings WHERE key IN (?, ?)`, "compute_permission_"+center.ID, "force_sync_"+center.ID).Scan(&computeSettingCount)
	_ = provider.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM token_usage_records WHERE center_id=?`, center.ID).Scan(&tokenUsageCount)
	_ = provider.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM cost_summaries WHERE center_id=?`, center.ID).Scan(&costSummaryCount)
	if centerCount != 0 || licenseCount != 0 || assignmentCount != 0 || computeSettingCount != 0 || tokenUsageCount != 0 || costSummaryCount != 0 {
		t.Fatalf("dependent rows left: centers=%d licenses=%d assignments=%d compute_settings=%d token_usage=%d cost_summaries=%d", centerCount, licenseCount, assignmentCount, computeSettingCount, tokenUsageCount, costSummaryCount)
	}
	var sourceCenterID string
	if err := provider.Read.QueryRowContext(ctx, `SELECT source_center_id FROM skill_market_skills WHERE id=?`, "skill-delete").Scan(&sourceCenterID); err != nil {
		t.Fatalf("query sourced skill: %v", err)
	}
	if sourceCenterID != "" {
		t.Fatalf("sourced skill still references deleted center: %q", sourceCenterID)
	}
}

func TestCenterRepoUpdateRegistrationPersistsRepairedManagementSecret(t *testing.T) {
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
	center := &store.Center{
		ID:          "ctr_secret_repair",
		MachineID:   "machine-1",
		CompanyID:   "company-1",
		CompanyName: "Legacy",
		AdminEmail:  "old@example.com",
		Status:      "pending",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := repo.Create(ctx, center); err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	center.CompanyName = "Repaired"
	center.AdminEmail = "admin@example.com"
	center.SecretHash = "hash-repaired"
	center.ManagementSecret = "secret-repaired"
	if err := repo.UpdateRegistration(ctx, center); err != nil {
		t.Fatalf("UpdateRegistration() error: %v", err)
	}
	got, err := repo.GetByRegistrationKey(ctx, "machine-1", "company-1")
	if err != nil {
		t.Fatalf("GetByRegistrationKey() error: %v", err)
	}
	if got.CompanyName != "Repaired" || got.SecretHash != "hash-repaired" || got.ManagementSecret != "secret-repaired" {
		t.Fatalf("registration update did not persist repaired secret: %+v", got)
	}
}

func TestSkillRepoSearchActiveOnlyReturnsPackagedSkills(t *testing.T) {
	provider, err := NewProvider(":memory:")
	if err != nil {
		t.Fatalf("NewProvider() error: %v", err)
	}
	defer provider.Close()
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations() error: %v", err)
	}
	ctx := context.Background()
	repo := NewStore(provider).Skills
	now := time.Now().UTC()
	create := func(id, status, packageContent, packageSHA string) {
		t.Helper()
		if err := repo.Create(ctx, &store.Skill{
			ID:             id,
			Name:           id,
			Description:    id,
			Category:       "ops",
			Version:        "1.0.0",
			Status:         status,
			PackageFormat:  "skill.md",
			PackageContent: packageContent,
			PackageSHA256:  packageSHA,
			PackageSize:    int64(len(packageContent)),
			CreatedAt:      now,
			UpdatedAt:      now,
		}); err != nil {
			t.Fatalf("Create(%s) error: %v", id, err)
		}
	}
	create("packaged-active", "active", "IyBQYWNrYWdlZAo=", "sha")
	create("metadata-active", "active", "", "")
	create("draft-packaged", "draft", "IyBEcmFmdAo=", "sha")

	items, err := repo.SearchActive(ctx, "")
	if err != nil {
		t.Fatalf("SearchActive() error: %v", err)
	}
	if len(items) != 1 || items[0].ID != "packaged-active" {
		t.Fatalf("SearchActive() = %+v, want only packaged active skill", items)
	}

	items, err = repo.SearchActive(ctx, "metadata")
	if err != nil {
		t.Fatalf("SearchActive(metadata) error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("SearchActive(metadata) = %+v, want no unpackaged skill", items)
	}
}
