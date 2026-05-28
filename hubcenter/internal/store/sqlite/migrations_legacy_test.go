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

func TestRunMigrationsDedupesHubRegistrationEndpoints(t *testing.T) {
	provider, err := NewProvider(Config{
		DSN:               filepath.Join(t.TempDir(), "hubcenter-dedupe-endpoints.db"),
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
		installation_id TEXT NOT NULL DEFAULT '',
		owner_email TEXT NOT NULL,
		name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		base_url TEXT NOT NULL,
		host TEXT NOT NULL DEFAULT '',
		port INTEGER NOT NULL DEFAULT 0,
		visibility TEXT NOT NULL,
		enrollment_mode TEXT NOT NULL,
		corporate_email_domain TEXT NOT NULL DEFAULT '',
		accept_public_signup INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'offline',
		is_disabled INTEGER NOT NULL DEFAULT 0,
		disabled_reason TEXT NOT NULL DEFAULT '',
		capabilities_json TEXT NOT NULL DEFAULT '{}',
		hub_secret_hash TEXT NOT NULL,
		last_seen_at TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`); err != nil {
		t.Fatalf("create hub_instances: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE hub_user_links (
		id TEXT PRIMARY KEY,
		hub_id TEXT NOT NULL,
		tenant_id TEXT NOT NULL DEFAULT '',
		email TEXT NOT NULL,
		is_default INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`); err != nil {
		t.Fatalf("create hub_user_links: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE hub_domain_routes (
		id TEXT PRIMARY KEY,
		hub_id TEXT NOT NULL,
		tenant_id TEXT NOT NULL DEFAULT '',
		domain TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		priority INTEGER NOT NULL DEFAULT 100,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`); err != nil {
		t.Fatalf("create hub_domain_routes: %v", err)
	}

	older := time.Date(2026, 5, 26, 16, 21, 41, 0, time.UTC).Format(time.RFC3339)
	newer := time.Date(2026, 5, 26, 16, 33, 2, 0, time.UTC).Format(time.RFC3339)
	insertHub := func(id, baseURL, host string, port int, updatedAt string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO hub_instances (
			id, installation_id, owner_email, name, description, base_url, host, port, visibility,
			enrollment_mode, corporate_email_domain, accept_public_signup, status, is_disabled,
			disabled_reason, capabilities_json, hub_secret_hash, last_seen_at, created_at, updated_at
		) VALUES (?, '', 'owner@example.com', ?, '', ?, ?, ?, 'private', 'open', '', 0, 'pending_confirmation', 0, '', '{}', 'secret', NULL, ?, ?)`, id, id, baseURL, host, port, updatedAt, updatedAt); err != nil {
			t.Fatalf("insert hub %s: %v", id, err)
		}
	}
	insertHub("hub_old", "HTTP://41.10.1.7:9399/", "41.10.1.7", 9399, older)
	insertHub("hub_new", "http://41.10.1.7:9399", "41.10.1.7", 9399, newer)
	if _, err := db.Exec(`INSERT INTO hub_user_links (id, hub_id, email, is_default, created_at, updated_at) VALUES ('link_old', 'hub_old', 'owner@example.com', 1, ?, ?)`, older, older); err != nil {
		t.Fatalf("insert old link: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO hub_domain_routes (id, hub_id, domain, enabled, priority, created_at, updated_at) VALUES ('route_old', 'hub_old', 'example.com', 1, 100, ?, ?)`, older, older); err != nil {
		t.Fatalf("insert old route: %v", err)
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	var hubCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM hub_instances WHERE host = '41.10.1.7' AND port = 9399`).Scan(&hubCount); err != nil {
		t.Fatalf("count hubs: %v", err)
	}
	if hubCount != 1 {
		t.Fatalf("expected duplicate endpoint rows to merge, count=%d", hubCount)
	}
	var linkHubID, routeHubID, baseURL string
	if err := db.QueryRow(`SELECT hub_id FROM hub_user_links WHERE id = 'link_old'`).Scan(&linkHubID); err != nil {
		t.Fatalf("query link: %v", err)
	}
	if err := db.QueryRow(`SELECT hub_id FROM hub_domain_routes WHERE id = 'route_old'`).Scan(&routeHubID); err != nil {
		t.Fatalf("query route: %v", err)
	}
	if err := db.QueryRow(`SELECT base_url FROM hub_instances WHERE id = 'hub_new'`).Scan(&baseURL); err != nil {
		t.Fatalf("query canonical hub: %v", err)
	}
	if linkHubID != "hub_new" || routeHubID != "hub_new" || baseURL != "http://41.10.1.7:9399" {
		t.Fatalf("expected link/route/base_url normalized onto hub_new, link=%q route=%q base_url=%q", linkHubID, routeHubID, baseURL)
	}
	if _, err := db.Exec(`INSERT INTO hub_instances (
		id, installation_id, owner_email, name, description, base_url, host, port, visibility,
		enrollment_mode, corporate_email_domain, accept_public_signup, status, is_disabled,
		disabled_reason, capabilities_json, hub_secret_hash, last_seen_at, created_at, updated_at
	) VALUES ('hub_dup_again', '', 'owner@example.com', 'dup', '', 'http://41.10.1.7:9399', '41.10.1.7', 9399, 'private', 'open', '', 0, 'pending_confirmation', 0, '', '{}', 'secret', NULL, ?, ?)`, newer, newer); err == nil {
		t.Fatalf("expected unique endpoint index to reject new duplicate hub")
	}
}

func TestRunMigrationsDropsOldEndpointIndexesBeforeNormalizing(t *testing.T) {
	provider, err := NewProvider(Config{DSN: filepath.Join(t.TempDir(), "hubcenter-old-endpoint-index.db"), WAL: true, BusyTimeoutMS: 5000, MaxReadOpenConns: 4, MaxReadIdleConns: 2, MaxWriteOpenConns: 1, MaxWriteIdleConns: 1})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer provider.Close()
	db := provider.Write
	if _, err := db.Exec(`CREATE TABLE hub_instances (
		id TEXT PRIMARY KEY,
		installation_id TEXT NOT NULL DEFAULT '',
		owner_email TEXT NOT NULL,
		name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		base_url TEXT NOT NULL,
		host TEXT NOT NULL DEFAULT '',
		port INTEGER NOT NULL DEFAULT 0,
		visibility TEXT NOT NULL,
		enrollment_mode TEXT NOT NULL,
		corporate_email_domain TEXT NOT NULL DEFAULT '',
		accept_public_signup INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'offline',
		is_disabled INTEGER NOT NULL DEFAULT 0,
		disabled_reason TEXT NOT NULL DEFAULT '',
		capabilities_json TEXT NOT NULL DEFAULT '{}',
		hub_secret_hash TEXT NOT NULL,
		last_seen_at TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`); err != nil {
		t.Fatalf("create hub_instances: %v", err)
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX idx_hub_instances_endpoint ON hub_instances(host, port) WHERE host <> '' AND port > 0`); err != nil {
		t.Fatalf("create old endpoint index: %v", err)
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX idx_hub_instances_base_url ON hub_instances(base_url) WHERE base_url <> ''`); err != nil {
		t.Fatalf("create old base_url index: %v", err)
	}
	now := time.Date(2026, 5, 26, 16, 33, 2, 0, time.UTC).Format(time.RFC3339)
	for _, row := range []struct{ id, baseURL, host string }{
		{"hub_upper", "HTTP://Hub.Example.COM:9399/", "Hub.Example.COM"},
		{"hub_lower", "http://hub.example.com:9399", "hub.example.com"},
	} {
		if _, err := db.Exec(`INSERT INTO hub_instances (
			id, installation_id, owner_email, name, description, base_url, host, port, visibility,
			enrollment_mode, corporate_email_domain, accept_public_signup, status, is_disabled,
			disabled_reason, capabilities_json, hub_secret_hash, last_seen_at, created_at, updated_at
		) VALUES (?, '', 'owner@example.com', ?, '', ?, ?, 9399, 'private', 'open', '', 0, 'pending_confirmation', 0, '', '{}', 'secret', NULL, ?, ?)`, row.id, row.id, row.baseURL, row.host, now, now); err != nil {
			t.Fatalf("insert %s: %v", row.id, err)
		}
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM hub_instances WHERE host = 'hub.example.com' AND port = 9399`).Scan(&count); err != nil {
		t.Fatalf("count normalized hubs: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected old-index case variants to merge after migration, count=%d", count)
	}
}

func TestRunMigrationsDedupesHubInstallationIDsBeforeUniqueIndex(t *testing.T) {
	provider, err := NewProvider(Config{DSN: filepath.Join(t.TempDir(), "hubcenter-dedupe-installation.db"), WAL: true, BusyTimeoutMS: 5000, MaxReadOpenConns: 4, MaxReadIdleConns: 2, MaxWriteOpenConns: 1, MaxWriteIdleConns: 1})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	defer provider.Close()
	db := provider.Write
	if _, err := db.Exec(`CREATE TABLE hub_instances (
		id TEXT PRIMARY KEY,
		installation_id TEXT NOT NULL DEFAULT '',
		owner_email TEXT NOT NULL,
		name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		base_url TEXT NOT NULL,
		host TEXT NOT NULL DEFAULT '',
		port INTEGER NOT NULL DEFAULT 0,
		visibility TEXT NOT NULL,
		enrollment_mode TEXT NOT NULL,
		corporate_email_domain TEXT NOT NULL DEFAULT '',
		accept_public_signup INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'offline',
		is_disabled INTEGER NOT NULL DEFAULT 0,
		disabled_reason TEXT NOT NULL DEFAULT '',
		capabilities_json TEXT NOT NULL DEFAULT '{}',
		hub_secret_hash TEXT NOT NULL,
		last_seen_at TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`); err != nil {
		t.Fatalf("create hub_instances: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE hub_user_links (
		id TEXT PRIMARY KEY,
		hub_id TEXT NOT NULL,
		tenant_id TEXT NOT NULL DEFAULT '',
		email TEXT NOT NULL,
		is_default INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`); err != nil {
		t.Fatalf("create hub_user_links: %v", err)
	}
	old := time.Date(2026, 5, 26, 16, 21, 41, 0, time.UTC).Format(time.RFC3339)
	newer := time.Date(2026, 5, 26, 16, 33, 2, 0, time.UTC).Format(time.RFC3339)
	for _, row := range []struct{ id, installationID, updatedAt string }{
		{"hub_inst_old", " inst-1 ", old},
		{"hub_inst_new", "inst-1", newer},
	} {
		if _, err := db.Exec(`INSERT INTO hub_instances (
			id, installation_id, owner_email, name, description, base_url, host, port, visibility,
			enrollment_mode, corporate_email_domain, accept_public_signup, status, is_disabled,
			disabled_reason, capabilities_json, hub_secret_hash, last_seen_at, created_at, updated_at
		) VALUES (?, ?, 'owner@example.com', ?, '', ?, ?, 9399, 'private', 'open', '', 0, 'pending_confirmation', 0, '', '{}', 'secret', NULL, ?, ?)`, row.id, row.installationID, row.id, "http://"+row.id+".example.com", row.id+".example.com", row.updatedAt, row.updatedAt); err != nil {
			t.Fatalf("insert %s: %v", row.id, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO hub_user_links (id, hub_id, email, is_default, created_at, updated_at) VALUES ('link_inst_old', 'hub_inst_old', 'owner@example.com', 1, ?, ?)`, old, old); err != nil {
		t.Fatalf("insert old link: %v", err)
	}

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	var hubCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM hub_instances WHERE installation_id = 'inst-1'`).Scan(&hubCount); err != nil {
		t.Fatalf("count hubs: %v", err)
	}
	if hubCount != 1 {
		t.Fatalf("expected duplicate installation rows to merge, count=%d", hubCount)
	}
	var linkHubID string
	if err := db.QueryRow(`SELECT hub_id FROM hub_user_links WHERE id = 'link_inst_old'`).Scan(&linkHubID); err != nil {
		t.Fatalf("query link: %v", err)
	}
	if linkHubID != "hub_inst_new" {
		t.Fatalf("expected link to move to newest installation hub, got %q", linkHubID)
	}
	if _, err := db.Exec(`INSERT INTO hub_instances (
		id, installation_id, owner_email, name, description, base_url, host, port, visibility,
		enrollment_mode, corporate_email_domain, accept_public_signup, status, is_disabled,
		disabled_reason, capabilities_json, hub_secret_hash, last_seen_at, created_at, updated_at
	) VALUES ('hub_inst_dup_again', 'inst-1', 'owner@example.com', 'dup', '', 'http://dup.example.com', 'dup.example.com', 9399, 'private', 'open', '', 0, 'pending_confirmation', 0, '', '{}', 'secret', NULL, ?, ?)`, newer, newer); err == nil {
		t.Fatalf("expected unique installation index to reject new duplicate hub")
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
