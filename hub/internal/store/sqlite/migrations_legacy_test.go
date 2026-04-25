package sqlite

import (
	"path/filepath"
	"testing"
	"time"
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
