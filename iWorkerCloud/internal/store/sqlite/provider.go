package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Provider struct {
	Write *sql.DB
	Read  *sql.DB
}

// NewProvider creates a Provider with separate read/write connection pools
// optimized for concurrent access. Write is limited to 1 connection (SQLite
// single-writer constraint). Read scales with CPU count via WAL mode.
func NewProvider(dsn string) (*Provider, error) {
	if dsn != "" && dsn != ":memory:" {
		dir := filepath.Dir(dsn)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("mkdir %s: %w", dir, err)
			}
		}
	}

	// For in-memory databases, use shared cache so read/write see the same data.
	effectiveDSN := dsn
	if dsn == ":memory:" {
		effectiveDSN = "file::memory:?cache=shared"
	}

	writeDB, err := sql.Open("sqlite", effectiveDSN)
	if err != nil {
		return nil, fmt.Errorf("open write db: %w", err)
	}

	readDB, err := sql.Open("sqlite", effectiveDSN)
	if err != nil {
		_ = writeDB.Close()
		return nil, fmt.Errorf("open read db: %w", err)
	}

	// Write: single connection (SQLite serializes writes regardless)
	writeDB.SetMaxOpenConns(1)
	writeDB.SetMaxIdleConns(1)
	writeDB.SetConnMaxLifetime(30 * time.Minute)

	// Read: scale with CPU count, minimum 4 for concurrent HTTP handlers
	readConns := runtime.NumCPU()
	if readConns < 4 {
		readConns = 4
	}
	readDB.SetMaxOpenConns(readConns)
	readDB.SetMaxIdleConns(readConns)
	readDB.SetConnMaxLifetime(30 * time.Minute)

	for _, conn := range []*sql.DB{writeDB, readDB} {
		if err := applyPragmas(conn); err != nil {
			_ = readDB.Close()
			_ = writeDB.Close()
			return nil, err
		}
	}

	return &Provider{Write: writeDB, Read: readDB}, nil
}

func (p *Provider) Close() {
	if p == nil {
		return
	}
	if p.Write != nil {
		_ = p.Write.Close()
	}
	if p.Read != nil {
		_ = p.Read.Close()
	}
}

func applyPragmas(conn *sql.DB) error {
	stmts := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA foreign_keys = ON;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA temp_store = MEMORY;",
		"PRAGMA cache_size = -8000;",        // 8 MB page cache per connection
		"PRAGMA mmap_size = 268435456;",     // 256 MB memory-mapped I/O
		"PRAGMA wal_autocheckpoint = 1000;", // checkpoint every 1000 pages
	}
	for _, stmt := range stmts {
		if _, err := conn.Exec(stmt); err != nil {
			return fmt.Errorf("pragma %q: %w", stmt, err)
		}
	}
	return nil
}

func RunMigrations(db *sql.DB) error {
	if _, err := db.Exec(schema); err != nil {
		return err
	}
	for _, stmt := range centerIntegrationMigrations {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	return nil
}

const schema = `
CREATE TABLE IF NOT EXISTS admins (
	id TEXT PRIMARY KEY,
	username TEXT UNIQUE NOT NULL,
	password_hash TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS centers (
	id TEXT PRIMARY KEY,
	company_name TEXT NOT NULL,
	admin_email TEXT NOT NULL,
	admin_phone TEXT NOT NULL DEFAULT '',
	address TEXT NOT NULL DEFAULT '',
	legal_person TEXT NOT NULL DEFAULT '',
	base_url TEXT NOT NULL DEFAULT '',
	supports_multi_tenant INTEGER NOT NULL DEFAULT 0,
	tenant_count INTEGER NOT NULL DEFAULT 0,
	cloud_control_mode TEXT NOT NULL DEFAULT 'cloud_managed',
	last_sync_status TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'pending',
	secret_hash TEXT NOT NULL DEFAULT '',
	last_heartbeat TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT (datetime('now')),
	updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS licenses (
	id TEXT PRIMARY KEY,
	center_id TEXT NOT NULL,
	modules TEXT NOT NULL DEFAULT '[]',
	type TEXT NOT NULL DEFAULT 'trial',
	expires_at TEXT NOT NULL,
	is_long_term INTEGER NOT NULL DEFAULT 0,
	certificate TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT (datetime('now')),
	revoked_at TEXT,
	FOREIGN KEY (center_id) REFERENCES centers(id)
);


CREATE TABLE IF NOT EXISTS skill_market_skills (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	category TEXT NOT NULL DEFAULT 'general',
	version TEXT NOT NULL DEFAULT '1.0.0',
	tags TEXT NOT NULL DEFAULT '[]',
	risk_level TEXT NOT NULL DEFAULT 'low',
	status TEXT NOT NULL DEFAULT 'active',
	price INTEGER NOT NULL DEFAULT 0,
	author TEXT NOT NULL DEFAULT '',
	avg_rating REAL NOT NULL DEFAULT 0,
	download_count INTEGER NOT NULL DEFAULT 0,
	package_format TEXT NOT NULL DEFAULT '',
	package_content TEXT NOT NULL DEFAULT '',
	package_sha256 TEXT NOT NULL DEFAULT '',
	package_size INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL DEFAULT (datetime('now')),
	updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_skill_market_status ON skill_market_skills(status);
CREATE INDEX IF NOT EXISTS idx_skill_market_category ON skill_market_skills(category);
CREATE TABLE IF NOT EXISTS system_settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL DEFAULT ''
);
`

var centerIntegrationMigrations = []string{
	`ALTER TABLE centers ADD COLUMN base_url TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE centers ADD COLUMN supports_multi_tenant INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE centers ADD COLUMN tenant_count INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE centers ADD COLUMN cloud_control_mode TEXT NOT NULL DEFAULT 'cloud_managed'`,
	`ALTER TABLE centers ADD COLUMN last_sync_status TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE skill_market_skills ADD COLUMN price INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE skill_market_skills ADD COLUMN author TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE skill_market_skills ADD COLUMN avg_rating REAL NOT NULL DEFAULT 0`,
	`ALTER TABLE skill_market_skills ADD COLUMN download_count INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE skill_market_skills ADD COLUMN package_format TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE skill_market_skills ADD COLUMN package_content TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE skill_market_skills ADD COLUMN package_sha256 TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE skill_market_skills ADD COLUMN package_size INTEGER NOT NULL DEFAULT 0`,
}
