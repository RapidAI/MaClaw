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
		`CREATE TABLE IF NOT EXISTS gossip_posts (
			id TEXT PRIMARY KEY,
			machine_id TEXT NOT NULL,
			user_email TEXT NOT NULL DEFAULT '',
			nickname TEXT NOT NULL,
			content TEXT NOT NULL,
			category TEXT NOT NULL DEFAULT 'owner',
			score INTEGER NOT NULL DEFAULT 0,
			votes INTEGER NOT NULL DEFAULT 0,
			locked INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS gossip_comments (
			id TEXT PRIMARY KEY,
			post_id TEXT NOT NULL,
			machine_id TEXT NOT NULL,
			user_email TEXT NOT NULL DEFAULT '',
			nickname TEXT NOT NULL,
			content TEXT NOT NULL,
			rating INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_gossip_comments_post_id ON gossip_comments(post_id);`,
		// 鈹€鈹€ News (announcements) 鈹€鈹€
		`CREATE TABLE IF NOT EXISTS news_articles (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			content TEXT NOT NULL DEFAULT '',
			category TEXT NOT NULL DEFAULT 'notice',
			pinned INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
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
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_gossip_comments_unique_rating ON gossip_comments(post_id, machine_id) WHERE rating > 0`); err != nil {
		return fmt.Errorf("create gossip rating unique index: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS ha_sync_ops (
		seq INTEGER PRIMARY KEY AUTOINCREMENT,
		op_id TEXT NOT NULL UNIQUE,
		source_node_id TEXT NOT NULL,
		entity_type TEXT NOT NULL,
		entity_id TEXT NOT NULL,
		op_type TEXT NOT NULL,
		entity_version INTEGER NOT NULL,
		occurred_at TEXT NOT NULL,
		payload_json TEXT NOT NULL,
		payload_hash TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create ha_sync_ops: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_ha_sync_ops_seq ON ha_sync_ops(seq)`); err != nil {
		return fmt.Errorf("create ha_sync_ops seq index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_ha_sync_ops_entity ON ha_sync_ops(entity_type, entity_id)`); err != nil {
		return fmt.Errorf("create ha_sync_ops entity index: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS ha_applied_ops (
		op_id TEXT PRIMARY KEY,
		source_node_id TEXT NOT NULL,
		entity_type TEXT NOT NULL,
		entity_id TEXT NOT NULL,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create ha_applied_ops: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS ha_peer_cursors (
		peer_node_id TEXT PRIMARY KEY,
		last_pulled_seq INTEGER NOT NULL DEFAULT 0,
		last_pulled_at TEXT,
		last_success_at TEXT,
		last_error TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		return fmt.Errorf("create ha_peer_cursors: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS ha_entity_versions (
		entity_type TEXT NOT NULL,
		entity_id TEXT NOT NULL,
		version INTEGER NOT NULL,
		updated_at TEXT NOT NULL,
		updated_by_node_id TEXT NOT NULL,
		PRIMARY KEY(entity_type, entity_id)
	)`); err != nil {
		return fmt.Errorf("create ha_entity_versions: %w", err)
	}
	if err := ensureGossipFlaggedColumn(db); err != nil {
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

func ensureGossipFlaggedColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(gossip_posts)`)
	if err != nil {
		return fmt.Errorf("inspect gossip_posts columns: %w", err)
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
			return fmt.Errorf("scan gossip_posts column: %w", err)
		}
		if name == "flagged" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate gossip_posts columns: %w", err)
	}

	if _, err := db.Exec(`ALTER TABLE gossip_posts ADD COLUMN flagged INTEGER NOT NULL DEFAULT 0`); err != nil {
		return fmt.Errorf("add gossip flagged column: %w", err)
	}
	return nil
}
