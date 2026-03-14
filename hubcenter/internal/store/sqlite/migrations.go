package sqlite

import (
	"database/sql"
	"fmt"
)

func RunMigrations(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS admin_users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			email TEXT NOT NULL UNIQUE,
			status TEXT NOT NULL DEFAULT 'active',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS system_settings (
			key TEXT PRIMARY KEY,
			value_json TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS hub_instances (
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
			status TEXT NOT NULL DEFAULT 'offline',
			is_disabled INTEGER NOT NULL DEFAULT 0,
			disabled_reason TEXT NOT NULL DEFAULT '',
			capabilities_json TEXT NOT NULL DEFAULT '{}',
			hub_secret_hash TEXT NOT NULL,
			last_seen_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS hub_user_links (
			id TEXT PRIMARY KEY,
			hub_id TEXT NOT NULL,
			email TEXT NOT NULL,
			is_default INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS blocked_emails (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL UNIQUE,
			reason TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS blocked_ips (
			id TEXT PRIMARY KEY,
			ip TEXT NOT NULL UNIQUE,
			reason TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS admin_audit_logs (
			id TEXT PRIMARY KEY,
			admin_user_id TEXT NOT NULL,
			action TEXT NOT NULL,
			payload_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL
		);`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("run migration: %w", err)
		}
	}
	if err := ensureHubInstallationIDColumn(db); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_hub_instances_installation_id ON hub_instances(installation_id) WHERE installation_id <> ''`); err != nil {
		return fmt.Errorf("create hub installation index: %w", err)
	}
	if err := ensureInvitationCodeRequiredColumn(db); err != nil {
		return err
	}
	return nil
}

func ensureHubInstallationIDColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(hub_instances)`)
	if err != nil {
		return fmt.Errorf("inspect hub_instances columns: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			return fmt.Errorf("scan hub_instances column: %w", err)
		}
		if name == "installation_id" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate hub_instances columns: %w", err)
	}

	if _, err := db.Exec(`ALTER TABLE hub_instances ADD COLUMN installation_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("add hub installation_id column: %w", err)
	}
	return nil
}

func ensureInvitationCodeRequiredColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(hub_instances)`)
	if err != nil {
		return fmt.Errorf("inspect hub_instances columns: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			return fmt.Errorf("scan hub_instances column: %w", err)
		}
		if name == "invitation_code_required" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate hub_instances columns: %w", err)
	}

	if _, err := db.Exec(`ALTER TABLE hub_instances ADD COLUMN invitation_code_required BOOLEAN NOT NULL DEFAULT 0`); err != nil {
		return fmt.Errorf("add hub invitation_code_required column: %w", err)
	}
	return nil
}
