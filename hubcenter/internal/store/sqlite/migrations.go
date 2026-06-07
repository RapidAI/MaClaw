package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
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
		`CREATE TABLE IF NOT EXISTS hub_instances (
			id TEXT PRIMARY KEY,
			installation_id TEXT NOT NULL DEFAULT '',
			hub_origin TEXT NOT NULL DEFAULT 'self_hosted',
			default_signup_scope TEXT NOT NULL DEFAULT 'domain_restricted',
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
			registration_policy_json TEXT NOT NULL DEFAULT '{}',
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
		// News (announcements)
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
	if _, err := db.Exec(`DROP INDEX IF EXISTS idx_hub_instances_installation_id`); err != nil {
		return fmt.Errorf("drop old hub installation index: %w", err)
	}
	if _, err := db.Exec(`DROP INDEX IF EXISTS idx_hub_instances_endpoint`); err != nil {
		return fmt.Errorf("drop old hub endpoint index: %w", err)
	}
	if _, err := db.Exec(`DROP INDEX IF EXISTS idx_hub_instances_base_url`); err != nil {
		return fmt.Errorf("drop old hub base url index: %w", err)
	}
	if err := dedupeHubRegistrationInstallations(db); err != nil {
		return err
	}
	if err := dedupeHubRegistrationEndpoints(db); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_hub_instances_installation_id ON hub_instances(installation_id) WHERE installation_id <> ''`); err != nil {
		return fmt.Errorf("create hub installation index: %w", err)
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_hub_instances_endpoint ON hub_instances(host, port) WHERE host <> '' AND port > 0`); err != nil {
		return fmt.Errorf("create hub endpoint index: %w", err)
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_hub_instances_base_url ON hub_instances(base_url) WHERE base_url <> ''`); err != nil {
		return fmt.Errorf("create hub base url index: %w", err)
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
	if err := ensureHubRegistrationPolicyColumns(db); err != nil {
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
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_hub_user_links_hub_tenant_email ON hub_user_links(hub_id, tenant_id, email)`); err != nil {
		return fmt.Errorf("create hub_user_links hub/tenant/email index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_hub_instances_status_disabled ON hub_instances(status, is_disabled)`); err != nil {
		return fmt.Errorf("create hub_instances status/disabled index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_hub_instances_public_signup ON hub_instances(accept_public_signup, status, is_disabled)`); err != nil {
		return fmt.Errorf("create hub_instances public signup index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_hub_instances_inventory_refresh ON hub_instances(base_url, hub_secret_hash, updated_at DESC)`); err != nil {
		return fmt.Errorf("create hub_instances inventory refresh index: %w", err)
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
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_ha_sync_ops_occurred_at ON ha_sync_ops(occurred_at, seq)`); err != nil {
		return fmt.Errorf("create ha_sync_ops occurred_at index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_ha_sync_ops_entity_seq ON ha_sync_ops(entity_type, entity_id, seq DESC)`); err != nil {
		return fmt.Errorf("create ha_sync_ops entity seq index: %w", err)
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
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_ha_applied_ops_applied_at ON ha_applied_ops(applied_at)`); err != nil {
		return fmt.Errorf("create ha_applied_ops applied_at index: %w", err)
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
	if err := ensureInvitationCodeRoutesTable(db); err != nil {
		return err
	}
	return nil
}

func ensureHubRegistrationPolicyColumns(db *sql.DB) error {
	columns := map[string]string{
		"hub_origin":               "ALTER TABLE hub_instances ADD COLUMN hub_origin TEXT NOT NULL DEFAULT 'self_hosted'",
		"default_signup_scope":     "ALTER TABLE hub_instances ADD COLUMN default_signup_scope TEXT NOT NULL DEFAULT 'domain_restricted'",
		"registration_policy_json": "ALTER TABLE hub_instances ADD COLUMN registration_policy_json TEXT NOT NULL DEFAULT '{}'",
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
	_, err := db.Exec(`
		UPDATE hub_instances
		SET default_signup_scope = CASE
			WHEN accept_public_signup = 1 THEN 'public'
			WHEN invitation_code_required = 1 THEN 'invite_only'
			ELSE 'domain_restricted'
		END
		WHERE default_signup_scope = '' OR default_signup_scope IS NULL
	`)
	if err != nil {
		return fmt.Errorf("backfill hub registration policy columns: %w", err)
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

func dedupeHubRegistrationInstallations(db *sql.DB) error {
	if _, err := db.Exec(`UPDATE hub_instances SET installation_id = TRIM(installation_id)`); err != nil {
		return fmt.Errorf("normalize hub installation ids: %w", err)
	}
	for {
		merged, err := mergeDuplicateHubInstallationGroup(db)
		if err != nil {
			return err
		}
		if !merged {
			return nil
		}
	}
}

func mergeDuplicateHubInstallationGroup(db *sql.DB) (bool, error) {
	var installationID string
	err := db.QueryRow(`
		SELECT installation_id
		FROM hub_instances
		WHERE installation_id <> ''
		GROUP BY installation_id
		HAVING COUNT(*) > 1
		LIMIT 1
	`).Scan(&installationID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("find duplicate hub installation ids: %w", err)
	}

	rows, err := db.Query(`
		SELECT id
		FROM hub_instances
		WHERE installation_id = ?
		ORDER BY updated_at DESC, created_at DESC, id DESC
	`, installationID)
	if err != nil {
		return false, fmt.Errorf("list duplicate hub installation ids: %w", err)
	}
	defer rows.Close()

	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return false, fmt.Errorf("scan duplicate hub installation id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate duplicate hub installation ids: %w", err)
	}
	if len(ids) < 2 {
		return false, nil
	}
	if err := mergeDuplicateHubIDs(db, ids[0], ids[1:]); err != nil {
		return false, err
	}
	return true, nil
}

func dedupeHubRegistrationEndpoints(db *sql.DB) error {
	if _, err := db.Exec(`
		UPDATE hub_instances
		SET host = LOWER(TRIM(host)), base_url = RTRIM(TRIM(base_url), '/')
	`); err != nil {
		return fmt.Errorf("normalize hub registration endpoints: %w", err)
	}
	if err := normalizeHubBaseURLsForEndpointDedupe(db); err != nil {
		return err
	}
	for {
		merged, err := mergeDuplicateHubEndpointGroup(db, "host_port")
		if err != nil {
			return err
		}
		mergedBaseURL, err := mergeDuplicateHubEndpointGroup(db, "base_url")
		if err != nil {
			return err
		}
		if !merged && !mergedBaseURL {
			return nil
		}
	}
}

func normalizeHubBaseURLsForEndpointDedupe(db *sql.DB) error {
	rows, err := db.Query(`SELECT id, base_url FROM hub_instances WHERE base_url <> ''`)
	if err != nil {
		return fmt.Errorf("list hub base urls for normalization: %w", err)
	}
	defer rows.Close()

	type update struct{ id, baseURL string }
	updates := []update{}
	for rows.Next() {
		var id, baseURL string
		if err := rows.Scan(&id, &baseURL); err != nil {
			return fmt.Errorf("scan hub base url for normalization: %w", err)
		}
		normalized := normalizeMigrationHubBaseURL(baseURL)
		if normalized != baseURL {
			updates = append(updates, update{id: id, baseURL: normalized})
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate hub base urls for normalization: %w", err)
	}
	for _, item := range updates {
		if _, err := db.Exec(`UPDATE hub_instances SET base_url = ? WHERE id = ?`, item.baseURL, item.id); err != nil {
			return fmt.Errorf("normalize hub base url %s: %w", item.id, err)
		}
	}
	return nil
}

func normalizeMigrationHubBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return ""
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return baseURL
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String()
}

func mergeDuplicateHubEndpointGroup(db *sql.DB, mode string) (bool, error) {
	var rows *sql.Rows
	var err error
	switch mode {
	case "host_port":
		rows, err = db.Query(`
			SELECT host, port
			FROM hub_instances
			WHERE host <> '' AND port > 0
			GROUP BY host, port
			HAVING COUNT(*) > 1
			LIMIT 1
		`)
	case "base_url":
		rows, err = db.Query(`
			SELECT base_url, 0
			FROM hub_instances
			WHERE base_url <> ''
			GROUP BY base_url
			HAVING COUNT(*) > 1
			LIMIT 1
		`)
	default:
		return false, fmt.Errorf("unknown hub endpoint dedupe mode %q", mode)
	}
	if err != nil {
		return false, fmt.Errorf("find duplicate hub endpoints: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("iterate duplicate hub endpoints: %w", err)
		}
		return false, nil
	}
	var key string
	var port int
	if err := rows.Scan(&key, &port); err != nil {
		return false, fmt.Errorf("scan duplicate hub endpoint: %w", err)
	}
	if err := rows.Close(); err != nil {
		return false, fmt.Errorf("close duplicate hub endpoint rows: %w", err)
	}

	ids, err := duplicateHubIDs(db, mode, key, port)
	if err != nil {
		return false, err
	}
	if len(ids) < 2 {
		return false, nil
	}
	if err := mergeDuplicateHubIDs(db, ids[0], ids[1:]); err != nil {
		return false, err
	}
	return true, nil
}

func duplicateHubIDs(db *sql.DB, mode, key string, port int) ([]string, error) {
	var rows *sql.Rows
	var err error
	switch mode {
	case "host_port":
		rows, err = db.Query(`
			SELECT id
			FROM hub_instances
			WHERE host = ? AND port = ?
			ORDER BY updated_at DESC, created_at DESC, id DESC
		`, key, port)
	case "base_url":
		rows, err = db.Query(`
			SELECT id
			FROM hub_instances
			WHERE base_url = ?
			ORDER BY updated_at DESC, created_at DESC, id DESC
		`, key)
	default:
		return nil, fmt.Errorf("unknown hub endpoint dedupe mode %q", mode)
	}
	if err != nil {
		return nil, fmt.Errorf("list duplicate hub ids: %w", err)
	}
	defer rows.Close()

	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan duplicate hub id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate duplicate hub ids: %w", err)
	}
	return ids, nil
}

func mergeDuplicateHubIDs(db *sql.DB, canonicalID string, duplicateIDs []string) error {
	if canonicalID == "" || len(duplicateIDs) == 0 {
		return nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(duplicateIDs)), ",")
	args := make([]any, 0, len(duplicateIDs)+1)
	args = append(args, canonicalID)
	for _, id := range duplicateIDs {
		args = append(args, id)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin duplicate hub merge: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.Exec(`UPDATE hub_user_links SET hub_id = ? WHERE hub_id IN (`+placeholders+`)`, args...); err != nil {
		return fmt.Errorf("merge duplicate hub user links: %w", err)
	}
	if _, err := tx.Exec(`UPDATE hub_domain_routes SET hub_id = ? WHERE hub_id IN (`+placeholders+`)`, args...); err != nil {
		return fmt.Errorf("merge duplicate hub domain routes: %w", err)
	}
	deleteArgs := make([]any, 0, len(duplicateIDs))
	for _, id := range duplicateIDs {
		deleteArgs = append(deleteArgs, id)
	}
	if _, err := tx.Exec(`DELETE FROM hub_instances WHERE id IN (`+placeholders+`)`, deleteArgs...); err != nil {
		return fmt.Errorf("delete duplicate hub rows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit duplicate hub merge: %w", err)
	}
	committed = true
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

func ensureInvitationCodeRoutesTable(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS invitation_code_routes (
		code TEXT NOT NULL,
		hub_id TEXT NOT NULL,
		tenant_id TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		PRIMARY KEY(code)
	)`); err != nil {
		return fmt.Errorf("create invitation_code_routes table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_invitation_code_routes_hub_id ON invitation_code_routes(hub_id)`); err != nil {
		return fmt.Errorf("create invitation_code_routes hub_id index: %w", err)
	}
	return nil
}
