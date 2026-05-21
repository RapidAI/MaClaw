package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

func TestTenantMigrateUsersCommandDryRunDoesNotCommit(t *testing.T) {
	dbPath, configPath, mappingPath := seedTenantMigrateUsersCommandDB(t)

	if err := runTenantMigrateUsers([]string{"--config", configPath, "--mapping", mappingPath, "--json"}); err != nil {
		t.Fatalf("run tenant migrate-users dry-run: %v", err)
	}

	provider := openTenantMigrateUsersCommandProvider(t, dbPath)
	var tenantID string
	if err := provider.Write.QueryRowContext(context.Background(), `SELECT tenant_id FROM users WHERE id = 'user-1'`).Scan(&tenantID); err != nil {
		t.Fatalf("query user tenant: %v", err)
	}
	if tenantID != store.DefaultTenantID {
		t.Fatalf("dry-run committed user tenant %q", tenantID)
	}
}

func TestTenantMigrateUsersCommandApplyMovesUser(t *testing.T) {
	dbPath, configPath, mappingPath := seedTenantMigrateUsersCommandDB(t)

	if err := runTenantMigrateUsers([]string{"--config", configPath, "--mapping", mappingPath, "--apply", "--dry-run=false", "--json"}); err != nil {
		t.Fatalf("run tenant migrate-users apply: %v", err)
	}

	provider := openTenantMigrateUsersCommandProvider(t, dbPath)
	for _, table := range []string{"users", "machines"} {
		var count int
		if err := provider.Write.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM `+table+` WHERE tenant_id = 'tenant_a'`).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("%s tenant_a count = %d, want 1", table, count)
		}
	}
}

func seedTenantMigrateUsersCommandDB(t *testing.T) (dbPath, configPath, mappingPath string) {
	t.Helper()
	dir := t.TempDir()
	dbPath = filepath.Join(dir, "hub.db")
	provider := openTenantMigrateUsersCommandProvider(t, dbPath)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO tenants (id, slug, name, status, settings_json, created_by_admin_id, created_at, updated_at) VALUES ('tenant_a', 'tenant-a', 'Tenant A', 'active', '{}', 'test', ?, ?)`, now, now); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO users (id, tenant_id, email, sn, status, enrollment_status, smart_route, created_at, updated_at) VALUES ('user-1', ?, 'alice@example.com', 'sn-1', 'active', 'approved', 0, ?, ?)`, store.DefaultTenantID, now, now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO machines (id, tenant_id, user_id, name, platform, machine_token_hash, status, created_at, updated_at) VALUES ('machine-1', ?, 'user-1', 'pc', 'windows', 'hash', 'offline', ?, ?)`, store.DefaultTenantID, now, now); err != nil {
		t.Fatalf("insert machine: %v", err)
	}
	_ = provider.Close()

	configPath = filepath.Join(dir, "hub.yaml")
	configYAML := "database:\n  dsn: " + filepath.ToSlash(dbPath) + "\n  wal: false\n  busy_timeout_ms: 5000\n  max_read_open_conns: 2\n  max_read_idle_conns: 1\n  max_write_open_conns: 1\n  max_write_idle_conns: 1\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	mappingPath = filepath.Join(dir, "users.csv")
	if err := os.WriteFile(mappingPath, []byte("email,tenant_id\nalice@example.com,tenant_a\n"), 0o644); err != nil {
		t.Fatalf("write mapping: %v", err)
	}
	return dbPath, configPath, mappingPath
}

func openTenantMigrateUsersCommandProvider(t *testing.T, dbPath string) *sqlite.Provider {
	t.Helper()
	provider, err := sqlite.NewProvider(sqlite.Config{DSN: dbPath, BusyTimeoutMS: 5000, MaxReadOpenConns: 2, MaxReadIdleConns: 1, MaxWriteOpenConns: 1, MaxWriteIdleConns: 1})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	return provider
}
