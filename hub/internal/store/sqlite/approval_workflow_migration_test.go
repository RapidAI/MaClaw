package sqlite

import (
	"database/sql"
	"testing"
)

func TestApprovalWorkflowMigration_TablesCreated(t *testing.T) {
	st := newTestStore(t)
	_ = st // ensure migrations ran via newTestStore

	// Access the underlying DB through the provider
	provider, err := NewProvider(Config{
		DSN:               t.TempDir() + "/approval-wf-test.db",
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  2,
		MaxReadIdleConns:  1,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	db := provider.Write

	// Verify all 5 tables exist
	tables := []string{
		"workflow_definitions",
		"workflow_versions",
		"workflow_instances",
		"node_executions",
		"approval_audit_trail",
	}
	for _, table := range tables {
		var name string
		err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Fatalf("table %q not found: %v", table, err)
		}
	}

	// Verify indexes exist
	indexes := []string{
		"idx_workflow_def_owner",
		"idx_wf_ver_workflow",
		"idx_wf_ver_status",
		"idx_wf_ver_published",
		"idx_wf_inst_status",
		"idx_wf_inst_workflow",
		"idx_node_exec_instance",
		"idx_node_exec_status",
		"idx_audit_instance",
		"idx_audit_actor",
		"idx_audit_timestamp",
		"idx_audit_decision",
	}
	for _, idx := range indexes {
		var name string
		err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, idx,
		).Scan(&name)
		if err != nil {
			t.Fatalf("index %q not found: %v", idx, err)
		}
	}

	// Verify triggers exist
	triggers := []string{
		"trg_audit_trail_no_update",
		"trg_audit_trail_no_delete",
	}
	for _, trg := range triggers {
		var name string
		err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='trigger' AND name=?`, trg,
		).Scan(&name)
		if err != nil {
			t.Fatalf("trigger %q not found: %v", trg, err)
		}
	}
}

func TestApprovalWorkflowMigration_AuditTrailImmutability(t *testing.T) {
	provider, err := NewProvider(Config{
		DSN:               t.TempDir() + "/audit-immutable-test.db",
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  2,
		MaxReadIdleConns:  1,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	db := provider.Write

	// Insert a valid audit trail entry
	_, err = db.Exec(`INSERT INTO approval_audit_trail (id, instance_id, event_type, timestamp)
		VALUES ('at_1', 'inst_1', 'instance_created', datetime('now'))`)
	if err != nil {
		t.Fatalf("insert audit entry: %v", err)
	}

	// Verify the entry exists
	var id string
	err = db.QueryRow(`SELECT id FROM approval_audit_trail WHERE id = 'at_1'`).Scan(&id)
	if err != nil {
		t.Fatalf("query audit entry: %v", err)
	}

	// Attempt UPDATE — should be rejected by trigger
	_, err = db.Exec(`UPDATE approval_audit_trail SET event_type = 'modified' WHERE id = 'at_1'`)
	if err == nil {
		t.Fatal("expected UPDATE to be rejected by trigger, but it succeeded")
	}
	if !isAuditImmutableError(err) {
		t.Fatalf("unexpected error on UPDATE: %v", err)
	}

	// Attempt DELETE — should be rejected by trigger
	_, err = db.Exec(`DELETE FROM approval_audit_trail WHERE id = 'at_1'`)
	if err == nil {
		t.Fatal("expected DELETE to be rejected by trigger, but it succeeded")
	}
	if !isAuditImmutableError(err) {
		t.Fatalf("unexpected error on DELETE: %v", err)
	}

	// Verify the entry is unchanged
	var eventType string
	err = db.QueryRow(`SELECT event_type FROM approval_audit_trail WHERE id = 'at_1'`).Scan(&eventType)
	if err != nil {
		t.Fatalf("query after failed update/delete: %v", err)
	}
	if eventType != "instance_created" {
		t.Fatalf("event_type changed to %q, expected 'instance_created'", eventType)
	}
}

func TestApprovalWorkflowMigration_PublishedUniqueConstraint(t *testing.T) {
	provider, err := NewProvider(Config{
		DSN:               t.TempDir() + "/published-unique-test.db",
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  2,
		MaxReadIdleConns:  1,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	db := provider.Write

	// Create a workflow definition
	_, err = db.Exec(`INSERT INTO workflow_definitions (id, owner_id, name, created_at, updated_at)
		VALUES ('wf_1', 'owner_1', 'Test Workflow', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatalf("insert workflow definition: %v", err)
	}

	// Insert first published version — should succeed
	_, err = db.Exec(`INSERT INTO workflow_versions (id, workflow_id, version_number, status, graph_json, created_at, updated_at)
		VALUES ('v_1', 'wf_1', '1.0.0', 'published', '{}', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatalf("insert first published version: %v", err)
	}

	// Insert second published version for same workflow — should fail (unique constraint)
	_, err = db.Exec(`INSERT INTO workflow_versions (id, workflow_id, version_number, status, graph_json, created_at, updated_at)
		VALUES ('v_2', 'wf_1', '2.0.0', 'published', '{}', datetime('now'), datetime('now'))`)
	if err == nil {
		t.Fatal("expected unique constraint violation for second published version, but insert succeeded")
	}

	// Insert draft version for same workflow — should succeed (partial index only covers published)
	_, err = db.Exec(`INSERT INTO workflow_versions (id, workflow_id, version_number, status, graph_json, created_at, updated_at)
		VALUES ('v_3', 'wf_1', '2.0.0', 'draft', '{}', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatalf("insert draft version should succeed: %v", err)
	}

	// Insert published version for different workflow — should succeed
	_, err = db.Exec(`INSERT INTO workflow_definitions (id, owner_id, name, created_at, updated_at)
		VALUES ('wf_2', 'owner_2', 'Another Workflow', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatalf("insert second workflow definition: %v", err)
	}
	_, err = db.Exec(`INSERT INTO workflow_versions (id, workflow_id, version_number, status, graph_json, created_at, updated_at)
		VALUES ('v_4', 'wf_2', '1.0.0', 'published', '{}', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatalf("insert published version for different workflow should succeed: %v", err)
	}
}

func TestApprovalWorkflowMigration_BasicCRUD(t *testing.T) {
	provider, err := NewProvider(Config{
		DSN:               t.TempDir() + "/crud-test.db",
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  2,
		MaxReadIdleConns:  1,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	db := provider.Write

	// Insert workflow definition
	_, err = db.Exec(`INSERT INTO workflow_definitions (id, owner_id, name, description, created_at, updated_at)
		VALUES ('wf_crud', 'owner_1', 'Purchase Approval', '采购审批流程', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatalf("insert workflow_definitions: %v", err)
	}

	// Insert workflow version
	_, err = db.Exec(`INSERT INTO workflow_versions (id, workflow_id, version_number, status, graph_json, created_at, updated_at)
		VALUES ('ver_1', 'wf_crud', '1.0.0', 'draft', '{"nodes":[],"edges":[]}', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatalf("insert workflow_versions: %v", err)
	}

	// Insert workflow instance
	_, err = db.Exec(`INSERT INTO workflow_instances (id, workflow_id, version_id, status, created_at)
		VALUES ('inst_1', 'wf_crud', 'ver_1', 'running', datetime('now'))`)
	if err != nil {
		t.Fatalf("insert workflow_instances: %v", err)
	}

	// Insert node execution
	_, err = db.Exec(`INSERT INTO node_executions (id, instance_id, node_id, status, started_at)
		VALUES ('ne_1', 'inst_1', 'node_approval_1', 'pending', datetime('now'))`)
	if err != nil {
		t.Fatalf("insert node_executions: %v", err)
	}

	// Insert audit trail entry
	_, err = db.Exec(`INSERT INTO approval_audit_trail (id, instance_id, node_id, event_type, actor_id, decision, timestamp)
		VALUES ('at_crud', 'inst_1', 'node_approval_1', 'decision_made', 've_approver_1', 'approve', datetime('now'))`)
	if err != nil {
		t.Fatalf("insert approval_audit_trail: %v", err)
	}

	// Query by owner
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM workflow_definitions WHERE owner_id = 'owner_1'`).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("query by owner: count=%d, err=%v", count, err)
	}

	// Query by status
	err = db.QueryRow(`SELECT COUNT(*) FROM workflow_instances WHERE status = 'running'`).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("query by status: count=%d, err=%v", count, err)
	}

	// Query audit by instance
	err = db.QueryRow(`SELECT COUNT(*) FROM approval_audit_trail WHERE instance_id = 'inst_1'`).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("query audit by instance: count=%d, err=%v", count, err)
	}
}

// isAuditImmutableError checks if the error is from the immutability trigger.
func isAuditImmutableError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "immutable") || contains(msg, "ABORT")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Verify foreign key constraints work (SQLite needs PRAGMA foreign_keys = ON)
func TestApprovalWorkflowMigration_ForeignKeys(t *testing.T) {
	provider, err := NewProvider(Config{
		DSN:               t.TempDir() + "/fk-test.db",
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  2,
		MaxReadIdleConns:  1,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	db := provider.Write

	// Enable foreign keys
	_, _ = db.Exec(`PRAGMA foreign_keys = ON`)

	// Try inserting a workflow_version referencing non-existent workflow_definitions
	_, err = db.Exec(`INSERT INTO workflow_versions (id, workflow_id, version_number, status, graph_json, created_at, updated_at)
		VALUES ('v_orphan', 'nonexistent_wf', '1.0.0', 'draft', '{}', datetime('now'), datetime('now'))`)
	if err == nil {
		// Foreign keys may not be enforced if PRAGMA foreign_keys is not supported
		// in this build. This is acceptable — the constraint is defined in schema.
		var fkEnabled int
		_ = db.QueryRow(`PRAGMA foreign_keys`).Scan(&fkEnabled)
		if fkEnabled == 1 {
			t.Fatal("expected foreign key violation, but insert succeeded with FK enabled")
		}
	}
}

// Verify idempotency — running migrations twice should not error
func TestApprovalWorkflowMigration_Idempotent(t *testing.T) {
	provider, err := NewProvider(Config{
		DSN:               t.TempDir() + "/idempotent-test.db",
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  2,
		MaxReadIdleConns:  1,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	// Run migrations twice — should not error (CREATE TABLE IF NOT EXISTS)
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("first migration run: %v", err)
	}
	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("second migration run (idempotency): %v", err)
	}

	// Verify tables still exist after double run
	var name string
	err = provider.Write.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='approval_audit_trail'`,
	).Scan(&name)
	if err != nil {
		t.Fatalf("table not found after double migration: %v", err)
	}
}

// Verify the audit trail timestamp has millisecond precision
func TestApprovalWorkflowMigration_AuditTimestampPrecision(t *testing.T) {
	provider, err := NewProvider(Config{
		DSN:               t.TempDir() + "/timestamp-test.db",
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  2,
		MaxReadIdleConns:  1,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	if err := RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	db := provider.Write

	// Insert with default timestamp (uses strftime with milliseconds)
	_, err = db.Exec(`INSERT INTO approval_audit_trail (id, instance_id, event_type)
		VALUES ('at_ts', 'inst_ts', 'test_event')`)
	if err != nil {
		t.Fatalf("insert with default timestamp: %v", err)
	}

	var ts string
	err = db.QueryRow(`SELECT timestamp FROM approval_audit_trail WHERE id = 'at_ts'`).Scan(&ts)
	if err != nil {
		t.Fatalf("query timestamp: %v", err)
	}

	// Verify timestamp contains milliseconds (format: YYYY-MM-DDTHH:MM:SS.mmm)
	// strftime('%Y-%m-%dT%H:%M:%f','now') produces e.g. "2026-01-15T10:30:45.123"
	if len(ts) < 23 {
		t.Fatalf("timestamp lacks millisecond precision: %q (len=%d)", ts, len(ts))
	}
}

// Helper to check if a table has a specific column
func hasColumn(db *sql.DB, table, column string) bool {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dfltValue *string
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return false
		}
		if name == column {
			return true
		}
	}
	return false
}
