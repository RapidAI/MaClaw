package sqlite

import (
	"database/sql"
	"fmt"
	"strings"
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

		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL UNIQUE,
			sn TEXT NOT NULL UNIQUE,
			status TEXT NOT NULL DEFAULT 'active',
			enrollment_status TEXT NOT NULL DEFAULT 'approved',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS user_enrollments (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL,
			status TEXT NOT NULL,
			note TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS email_blocklist (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL UNIQUE,
			reason TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS email_invites (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'viewer',
			status TEXT NOT NULL DEFAULT 'active',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS machines (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			name TEXT NOT NULL,
			platform TEXT NOT NULL,
			machine_token_hash TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'offline',
			last_seen_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			machine_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			tool TEXT NOT NULL,
			title TEXT NOT NULL,
			project_path TEXT NOT NULL,
			status TEXT NOT NULL,
			summary_json TEXT NOT NULL DEFAULT '{}',
			preview_text TEXT NOT NULL DEFAULT '',
			output_seq INTEGER NOT NULL DEFAULT 0,
			host_online INTEGER NOT NULL DEFAULT 1,
			started_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			ended_at TEXT,
			exit_code INTEGER
		);`,

		`CREATE TABLE IF NOT EXISTS viewer_tokens (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			token_hash TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			revoked_at TEXT
		);`,

		`CREATE TABLE IF NOT EXISTS login_tokens (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			purpose TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			consumed_at TEXT,
			created_at TEXT NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS audit_logs (
			id TEXT PRIMARY KEY,
			user_id TEXT,
			machine_id TEXT,
			session_id TEXT,
			event_type TEXT NOT NULL,
			payload_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS admin_audit_logs (
			id TEXT PRIMARY KEY,
			admin_user_id TEXT NOT NULL,
			action TEXT NOT NULL,
			payload_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS invitation_codes (
			id TEXT PRIMARY KEY,
			code TEXT NOT NULL UNIQUE,
			status TEXT NOT NULL DEFAULT 'unused',
			used_by_email TEXT NOT NULL DEFAULT '',
			used_at DATETIME,
			created_at DATETIME NOT NULL
		);`,

		`CREATE INDEX IF NOT EXISTS idx_invitation_codes_code ON invitation_codes(code);`,

		`CREATE INDEX IF NOT EXISTS idx_invitation_codes_status ON invitation_codes(status);`,
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("run migration: %w", err)
		}
	}

	alterStmts := []string{
		`ALTER TABLE machines ADD COLUMN hostname TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE machines ADD COLUMN arch TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE machines ADD COLUMN app_version TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE machines ADD COLUMN heartbeat_sec INTEGER NOT NULL DEFAULT 10`,
		`ALTER TABLE machines ADD COLUMN client_id TEXT NOT NULL DEFAULT ''`,
	}
	alterStmts = append(alterStmts, `ALTER TABLE machines ADD COLUMN alias TEXT NOT NULL DEFAULT ''`)
	alterStmts = append(alterStmts, `ALTER TABLE login_tokens ADD COLUMN poll_token_hash TEXT NOT NULL DEFAULT ''`)

	for _, stmt := range alterStmts {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
			return fmt.Errorf("run alter migration: %w", err)
		}
	}

	return nil
}
