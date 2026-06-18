package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
)

func TestRunMigrationsPreservesLegacyUsersWhenAddingFailureLogs(t *testing.T) {
	provider, err := NewProvider(Config{
		DSN:               filepath.Join(t.TempDir(), "hub-legacy-migration.db"),
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  4,
		MaxReadIdleConns:  2,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer provider.Close()

	db := provider.Write
	legacySchema := []string{
		`CREATE TABLE users (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL UNIQUE,
			sn TEXT NOT NULL UNIQUE,
			status TEXT NOT NULL DEFAULT 'active',
			enrollment_status TEXT NOT NULL DEFAULT 'approved',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE system_settings (
			key TEXT PRIMARY KEY,
			value_json TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
	}
	for _, stmt := range legacySchema {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("create legacy schema: %v", err)
		}
	}

	now := time.Date(2026, 4, 25, 12, 30, 0, 0, time.UTC).Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO users (id, email, sn, status, enrollment_status, created_at, updated_at) VALUES ('u_1', 'legacy@example.com', 'SN001', 'active', 'approved', ?, ?)`, now, now); err != nil {
		t.Fatalf("insert legacy user: %v", err)
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE email = 'legacy@example.com'`).Scan(&count); err != nil {
		t.Fatalf("count legacy users: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected legacy user to be preserved, got count=%d", count)
	}

	if _, err := db.Exec(`INSERT INTO failure_event_logs (id, category, event_code, message, entity_id, email, client_ip, details_json, created_at) VALUES ('log_1', 'registration', 'legacy_upgrade_check', 'ok', '', 'legacy@example.com', '', '{}', ?)`, now); err != nil {
		t.Fatalf("insert failure log after migration: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM failure_event_logs`).Scan(&count); err != nil {
		t.Fatalf("count failure logs: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected failure_event_logs table to accept inserts, count=%d", count)
	}
}

func TestRunMigrationsUpgradesLegacyFailureEventLogsWithoutTenantID(t *testing.T) {
	provider, err := NewProvider(Config{
		DSN:               filepath.Join(t.TempDir(), "hub-legacy-failure-logs.db"),
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  4,
		MaxReadIdleConns:  2,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer provider.Close()

	db := provider.Write
	now := time.Date(2026, 5, 21, 14, 1, 44, 0, time.UTC).Format(time.RFC3339)
	legacySchema := []string{
		`CREATE TABLE failure_event_logs (
			id TEXT PRIMARY KEY,
			category TEXT NOT NULL,
			event_code TEXT NOT NULL,
			message TEXT NOT NULL,
			entity_id TEXT NOT NULL DEFAULT '',
			email TEXT NOT NULL DEFAULT '',
			client_ip TEXT NOT NULL DEFAULT '',
			details_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL
		);`,
		`INSERT INTO failure_event_logs (id, category, event_code, message, entity_id, email, client_ip, details_json, created_at)
		 VALUES ('log_legacy', 'registration', 'legacy', 'old row', '', 'legacy@example.com', '', '{}', '` + now + `');`,
	}
	for _, stmt := range legacySchema {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("create legacy failure_event_logs schema: %v", err)
		}
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	var tenantID string
	if err := db.QueryRow(`SELECT tenant_id FROM failure_event_logs WHERE id = 'log_legacy'`).Scan(&tenantID); err != nil {
		t.Fatalf("query migrated tenant_id: %v", err)
	}
	if tenantID != "tenant_default" {
		t.Fatalf("tenant_id = %q, want tenant_default", tenantID)
	}

	if _, err := db.Exec(`INSERT INTO failure_event_logs (id, tenant_id, category, event_code, message, entity_id, email, client_ip, details_json, created_at) VALUES ('log_new', 'tenant_default', 'registration', 'new', 'ok', '', 'new@example.com', '', '{}', ?)`, now); err != nil {
		t.Fatalf("insert failure log after migration: %v", err)
	}
}

func TestRunMigrationsUpgradesLegacyWorkflowDefinitionsWithoutTenantID(t *testing.T) {
	provider, err := NewProvider(Config{
		DSN:               filepath.Join(t.TempDir(), "hub-legacy-workflow-definitions.db"),
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  4,
		MaxReadIdleConns:  2,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer provider.Close()

	db := provider.Write
	now := time.Date(2026, 6, 18, 9, 45, 0, 0, time.UTC).Format(time.RFC3339)
	legacySchema := []string{
		`CREATE TABLE workflow_definitions (
			id TEXT PRIMARY KEY,
			owner_id TEXT NOT NULL,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE workflow_versions (
			id TEXT PRIMARY KEY,
			workflow_id TEXT NOT NULL REFERENCES workflow_definitions(id),
			version_number TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'draft',
			graph_json TEXT NOT NULL DEFAULT '{}',
			submitted_at TEXT,
			published_at TEXT,
			rejection_reason TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`INSERT INTO workflow_definitions (id, owner_id, name, description, created_at, updated_at)
		 VALUES ('wf_legacy', 'owner_1', 'Legacy approval', '', '` + now + `', '` + now + `');`,
		`INSERT INTO workflow_versions (id, workflow_id, version_number, status, graph_json, submitted_at, created_at, updated_at)
		 VALUES ('ver_legacy', 'wf_legacy', '1.0.0', 'pending_review', '{}', '` + now + `', '` + now + `', '` + now + `');`,
	}
	for _, stmt := range legacySchema {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("create legacy workflow schema: %v", err)
		}
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	var tenantID string
	if err := db.QueryRow(`SELECT tenant_id FROM workflow_definitions WHERE id = 'wf_legacy'`).Scan(&tenantID); err != nil {
		t.Fatalf("query migrated workflow tenant_id: %v", err)
	}
	if tenantID != "tenant_default" {
		t.Fatalf("tenant_id = %q, want tenant_default", tenantID)
	}

	reviews, total, err := NewWorkflowStore(db).ListPendingReviews(context.Background(), 1, 50)
	if err != nil {
		t.Fatalf("ListPendingReviews after legacy migration: %v", err)
	}
	if total != 1 || len(reviews) != 1 || reviews[0].ID != "ver_legacy" || reviews[0].Status != workflow.VersionPendingReview {
		t.Fatalf("pending reviews = total %d items %+v, want ver_legacy", total, reviews)
	}
}
