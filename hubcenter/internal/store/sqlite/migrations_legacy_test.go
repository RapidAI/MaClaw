package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestRunMigrationsBackfillsLegacyHubDomainRoutesAndPublicSignup(t *testing.T) {
	provider, err := NewProvider(Config{
		DSN:               filepath.Join(t.TempDir(), "hubcenter-legacy-migration.db"),
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
	if _, err := db.Exec(`CREATE TABLE hub_instances (
		id TEXT PRIMARY KEY,
		owner_email TEXT NOT NULL,
		name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		base_url TEXT NOT NULL,
		host TEXT NOT NULL DEFAULT '',
		port INTEGER NOT NULL DEFAULT 0,
		visibility TEXT NOT NULL,
		enrollment_mode TEXT NOT NULL,
		corporate_email_domain TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'offline',
		is_disabled INTEGER NOT NULL DEFAULT 0,
		disabled_reason TEXT NOT NULL DEFAULT '',
		capabilities_json TEXT NOT NULL DEFAULT '{}',
		hub_secret_hash TEXT NOT NULL,
		last_seen_at TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}

	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
	legacyRows := []struct {
		id, ownerEmail, name, baseURL, visibility, enrollmentMode, corporateDomain string
	}{
		{"hub_corp", "owner@rapidai.tech", "Corp Hub", "https://corp.example.com", "shared", "approval", "@RapidAI.Tech"},
		{"hub_shared", "owner@example.com", "Shared Hub", "https://shared.example.com", "shared", "open", ""},
		{"hub_private", "owner@example.com", "Private Hub", "https://private.example.com", "private", "manual", ""},
	}
	for _, row := range legacyRows {
		if _, err := db.Exec(`
			INSERT INTO hub_instances (
				id, owner_email, name, description, base_url, host, port, visibility,
				enrollment_mode, corporate_email_domain, status, is_disabled, disabled_reason,
				capabilities_json, hub_secret_hash, last_seen_at, created_at, updated_at
			) VALUES (?, ?, ?, '', ?, '', 0, ?, ?, ?, 'online', 0, '', '{}', 'secret', NULL, ?, ?)
		`, row.id, row.ownerEmail, row.name, row.baseURL, row.visibility, row.enrollmentMode, row.corporateDomain, now, now); err != nil {
			t.Fatalf("insert legacy row %s: %v", row.id, err)
		}
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	assertAcceptPublicSignup := func(hubID string, want bool) {
		t.Helper()
		var got int
		if err := db.QueryRow(`SELECT accept_public_signup FROM hub_instances WHERE id = ?`, hubID).Scan(&got); err != nil {
			t.Fatalf("query accept_public_signup for %s: %v", hubID, err)
		}
		if (got != 0) != want {
			t.Fatalf("accept_public_signup for %s = %v, want %v", hubID, got != 0, want)
		}
	}
	assertAcceptPublicSignup("hub_corp", false)
	assertAcceptPublicSignup("hub_shared", true)
	assertAcceptPublicSignup("hub_private", false)

	var installationID string
	if err := db.QueryRow(`SELECT installation_id FROM hub_instances WHERE id = 'hub_corp'`).Scan(&installationID); err != nil {
		t.Fatalf("query installation_id: %v", err)
	}
	if installationID != "" {
		t.Fatalf("expected legacy installation_id to default empty, got %q", installationID)
	}

	st := NewStore(provider)
	routes, err := st.HubDomainRoutes.ListAll(context.Background())
	if err != nil {
		t.Fatalf("list routes: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("expected 1 backfilled route, got %d: %+v", len(routes), routes)
	}
	if routes[0].HubID != "hub_corp" || routes[0].Domain != "rapidai.tech" || !routes[0].Enabled {
		t.Fatalf("unexpected backfilled route: %+v", routes[0])
	}
}

func TestRunMigrationsAddsFailureEventLogsToLegacyHubCenterDB(t *testing.T) {
	provider, err := NewProvider(Config{
		DSN:               filepath.Join(t.TempDir(), "hubcenter-legacy-failure-logs.db"),
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
	if _, err := db.Exec(`CREATE TABLE system_settings (
		key TEXT PRIMARY KEY,
		value_json TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`); err != nil {
		t.Fatalf("create legacy system_settings: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE hub_instances (
		id TEXT PRIMARY KEY,
		owner_email TEXT NOT NULL,
		name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		base_url TEXT NOT NULL,
		host TEXT NOT NULL DEFAULT '',
		port INTEGER NOT NULL DEFAULT 0,
		visibility TEXT NOT NULL,
		enrollment_mode TEXT NOT NULL,
		corporate_email_domain TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'offline',
		is_disabled INTEGER NOT NULL DEFAULT 0,
		disabled_reason TEXT NOT NULL DEFAULT '',
		capabilities_json TEXT NOT NULL DEFAULT '{}',
		hub_secret_hash TEXT NOT NULL,
		last_seen_at TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`); err != nil {
		t.Fatalf("create legacy hub_instances: %v", err)
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	now := time.Date(2026, 4, 25, 12, 15, 0, 0, time.UTC).Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO failure_event_logs (id, category, event_code, message, entity_id, email, client_ip, details_json, created_at) VALUES ('legacy_hc_log_1', 'routing', 'legacy_upgrade_check', 'ok', 'hub_1', 'owner@rapidai.tech', '10.0.0.5', '{}', ?)`, now); err != nil {
		t.Fatalf("insert failure log after migration: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM failure_event_logs`).Scan(&count); err != nil {
		t.Fatalf("count failure logs: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected failure_event_logs table to accept inserts, count=%d", count)
	}
}
