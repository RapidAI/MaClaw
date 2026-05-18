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
			corporate_email_domain TEXT NOT NULL DEFAULT '',
			accept_public_signup INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'offline',
			is_disabled INTEGER NOT NULL DEFAULT 0,
			disabled_reason TEXT NOT NULL DEFAULT '',
			capabilities_json TEXT NOT NULL DEFAULT '{}',
			hub_secret_hash TEXT NOT NULL,
			digital_employee_quota INTEGER NOT NULL DEFAULT 0,
			digital_employee_authorization_enabled INTEGER NOT NULL DEFAULT 0,
			digital_employee_authorization_expires_at TEXT,
			last_seen_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS hub_user_links (
			id TEXT PRIMARY KEY,
			hub_id TEXT NOT NULL,
			tenant_id TEXT NOT NULL DEFAULT '',
			email TEXT NOT NULL,
			is_default INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS hub_domain_routes (
			id TEXT PRIMARY KEY,
			hub_id TEXT NOT NULL,
			tenant_id TEXT NOT NULL DEFAULT '',
			domain TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			priority INTEGER NOT NULL DEFAULT 100,
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
		`CREATE TABLE IF NOT EXISTS failure_event_logs (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT '',
			category TEXT NOT NULL,
			event_code TEXT NOT NULL,
			message TEXT NOT NULL,
			entity_id TEXT NOT NULL DEFAULT '',
			email TEXT NOT NULL DEFAULT '',
			client_ip TEXT NOT NULL DEFAULT '',
			details_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_failure_event_logs_created_at ON failure_event_logs(created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_failure_event_logs_category_created_at ON failure_event_logs(category, created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_failure_event_logs_tenant_created_at ON failure_event_logs(tenant_id, created_at DESC);`,
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
		// 闁冲厜鍋撻柍鍏夊亾 News (announcements) 闁冲厜鍋撻柍鍏夊亾
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
	if err := ensureCorporateEmailDomainColumn(db); err != nil {
		return err
	}
	if err := ensureDigitalEmployeeAuthorizationColumns(db); err != nil {
		return err
	}
	if err := ensureAcceptPublicSignupColumn(db); err != nil {
		return err
	}
	if err := ensureHubDomainRoutesTable(db); err != nil {
		return err
	}
	if err := ensureHubDomainRoutesTenantIDColumn(db); err != nil {
		return err
	}
	if err := backfillPrimaryHubRoutes(db); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_gossip_comments_unique_rating ON gossip_comments(post_id, machine_id) WHERE rating > 0`); err != nil {
		return fmt.Errorf("create gossip rating unique index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_hub_user_links_email_default ON hub_user_links(email, is_default, updated_at DESC)`); err != nil {
		return fmt.Errorf("create hub_user_links email/default index: %w", err)
	}
	if err := ensureHubUserLinksTenantIDColumn(db); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_hub_user_links_email_tenant_default ON hub_user_links(email, tenant_id, is_default, updated_at DESC)`); err != nil {
		return fmt.Errorf("create hub_user_links email/tenant/default index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_hub_user_links_hub_id ON hub_user_links(hub_id)`); err != nil {
		return fmt.Errorf("create hub_user_links hub_id index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_hub_instances_status_disabled ON hub_instances(status, is_disabled)`); err != nil {
		return fmt.Errorf("create hub_instances status/disabled index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_hub_instances_public_signup ON hub_instances(accept_public_signup, status, is_disabled)`); err != nil {
		return fmt.Errorf("create hub_instances public signup index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_hub_domain_routes_domain_enabled_priority ON hub_domain_routes(domain, enabled, priority, updated_at DESC)`); err != nil {
		return fmt.Errorf("create hub_domain_routes domain index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_hub_domain_routes_tenant_domain ON hub_domain_routes(tenant_id, domain, enabled, priority, updated_at DESC)`); err != nil {
		return fmt.Errorf("create hub_domain_routes tenant domain index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_hub_domain_routes_hub_id ON hub_domain_routes(hub_id)`); err != nil {
		return fmt.Errorf("create hub_domain_routes hub_id index: %w", err)
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
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS ha_heartbeat_sync_state (
		hub_id TEXT PRIMARY KEY,
		last_synced_seen_at TEXT
	)`); err != nil {
		return fmt.Errorf("create ha_heartbeat_sync_state: %w", err)
	}
	if err := ensureGossipFlaggedColumn(db); err != nil {
		return err
	}
	if err := ensureFailureLogsTenantIDColumn(db); err != nil {
		return err
	}
	return nil
}

func ensureFailureLogsTenantIDColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(failure_event_logs)`)
	if err != nil {
		return fmt.Errorf("inspect failure_event_logs columns: %w", err)
	}
	defer rows.Close()

	hasTenantID := false
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
			return fmt.Errorf("scan failure_event_logs column: %w", err)
		}
		if name == "tenant_id" {
			hasTenantID = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate failure_event_logs columns: %w", err)
	}
	if !hasTenantID {
		if _, err := db.Exec(`ALTER TABLE failure_event_logs ADD COLUMN tenant_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add failure_event_logs tenant_id: %w", err)
		}
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_failure_event_logs_tenant_created_at ON failure_event_logs(tenant_id, created_at DESC)`); err != nil {
		return fmt.Errorf("create failure_event_logs tenant index: %w", err)
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

func ensureHubUserLinksTenantIDColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(hub_user_links)`)
	if err != nil {
		return fmt.Errorf("inspect hub_user_links columns: %w", err)
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
			return fmt.Errorf("scan hub_user_links column: %w", err)
		}
		if name == "tenant_id" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate hub_user_links columns: %w", err)
	}
	if _, err := db.Exec(`ALTER TABLE hub_user_links ADD COLUMN tenant_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("add hub_user_links tenant_id: %w", err)
	}
	return nil
}

func ensureHubDomainRoutesTenantIDColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(hub_domain_routes)`)
	if err != nil {
		return fmt.Errorf("inspect hub_domain_routes columns: %w", err)
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
			return fmt.Errorf("scan hub_domain_routes column: %w", err)
		}
		if name == "tenant_id" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate hub_domain_routes columns: %w", err)
	}
	if _, err := db.Exec(`ALTER TABLE hub_domain_routes ADD COLUMN tenant_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("add hub_domain_routes tenant_id: %w", err)
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

func ensureCorporateEmailDomainColumn(db *sql.DB) error {
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
		if name == "corporate_email_domain" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate hub_instances columns: %w", err)
	}

	if _, err := db.Exec(`ALTER TABLE hub_instances ADD COLUMN corporate_email_domain TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("add hub corporate_email_domain column: %w", err)
	}
	return nil
}

func ensureAcceptPublicSignupColumn(db *sql.DB) error {
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
		if name == "accept_public_signup" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate hub_instances columns: %w", err)
	}

	if _, err := db.Exec(`ALTER TABLE hub_instances ADD COLUMN accept_public_signup INTEGER NOT NULL DEFAULT 0`); err != nil {
		return fmt.Errorf("add hub accept_public_signup column: %w", err)
	}
	if _, err := db.Exec(`
		UPDATE hub_instances
		SET accept_public_signup = CASE
			WHEN TRIM(corporate_email_domain) = '' AND LOWER(TRIM(visibility)) IN ('shared', 'public') THEN 1
			ELSE 0
		END
	`); err != nil {
		return fmt.Errorf("backfill hub accept_public_signup column: %w", err)
	}
	return nil
}

func ensureHubDomainRoutesTable(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS hub_domain_routes (
		id TEXT PRIMARY KEY,
		hub_id TEXT NOT NULL,
		tenant_id TEXT NOT NULL DEFAULT '',
		domain TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		priority INTEGER NOT NULL DEFAULT 100,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create hub_domain_routes: %w", err)
	}
	return nil
}

func backfillPrimaryHubRoutes(db *sql.DB) error {
	if _, err := db.Exec(`
		INSERT INTO hub_domain_routes (id, hub_id, tenant_id, domain, enabled, priority, created_at, updated_at)
		SELECT 'hdr_primary_' || id, id, '', LOWER(TRIM(TRIM(corporate_email_domain, '@'), '.')), 1, 100, created_at, updated_at
		FROM hub_instances
		WHERE TRIM(corporate_email_domain) <> ''
		  AND NOT EXISTS (
			SELECT 1 FROM hub_domain_routes r WHERE r.id = 'hdr_primary_' || hub_instances.id
		  )
	`); err != nil {
		return fmt.Errorf("backfill hub_domain_routes: %w", err)
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

func ensureDigitalEmployeeAuthorizationColumns(db *sql.DB) error {
	columns := map[string]string{
		"digital_employee_quota":                    "ALTER TABLE hub_instances ADD COLUMN digital_employee_quota INTEGER NOT NULL DEFAULT 0",
		"digital_employee_authorization_enabled":    "ALTER TABLE hub_instances ADD COLUMN digital_employee_authorization_enabled INTEGER NOT NULL DEFAULT 0",
		"digital_employee_authorization_expires_at": "ALTER TABLE hub_instances ADD COLUMN digital_employee_authorization_expires_at TEXT",
	}
	for name, stmt := range columns {
		ok, err := hubInstanceColumnExists(db, name)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("add hub_instances.%s column: %w", name, err)
		}
	}
	return nil
}

func hubInstanceColumnExists(db *sql.DB, target string) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(hub_instances)`)
	if err != nil {
		return false, fmt.Errorf("inspect hub_instances columns: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultVal sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			return false, fmt.Errorf("scan hub_instances column: %w", err)
		}
		if name == target {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate hub_instances columns: %w", err)
	}
	return false, nil
}
