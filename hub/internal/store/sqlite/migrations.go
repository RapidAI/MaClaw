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

		`CREATE TABLE IF NOT EXISTS failure_event_logs (
			id TEXT PRIMARY KEY,
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

		`CREATE TABLE IF NOT EXISTS voiceprints (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			email TEXT NOT NULL,
			label TEXT NOT NULL DEFAULT '',
			embedding BLOB NOT NULL,
			created_at TEXT NOT NULL
		);`,

		`CREATE INDEX IF NOT EXISTS idx_voiceprints_user_id ON voiceprints(user_id);`,

		`CREATE TABLE IF NOT EXISTS content_audit_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			user_id TEXT NOT NULL,
			platform TEXT NOT NULL,
			content_type TEXT NOT NULL,
			summary TEXT NOT NULL,
			return_code INTEGER NOT NULL,
			duration_ms INTEGER NOT NULL,
			message TEXT,
			content_hash TEXT
		);`,

		`CREATE INDEX IF NOT EXISTS idx_content_audit_logs_timestamp ON content_audit_logs(timestamp);`,
		`CREATE INDEX IF NOT EXISTS idx_content_audit_logs_user_id ON content_audit_logs(user_id);`,
		`CREATE INDEX IF NOT EXISTS idx_content_audit_logs_return_code ON content_audit_logs(return_code);`,

		`CREATE TABLE IF NOT EXISTS email_invites (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'viewer',
			status TEXT NOT NULL DEFAULT 'pending',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS llm_prompt_cache (
			cache_key TEXT PRIMARY KEY,
			provider_id TEXT NOT NULL,
			model TEXT NOT NULL,
			kind TEXT NOT NULL DEFAULT 'metadata',
			input_hash TEXT NOT NULL,
			payload BLOB,
			payload_bytes INTEGER NOT NULL DEFAULT 0,
			cached_input_tokens INTEGER NOT NULL DEFAULT 0,
			cache_write_tokens INTEGER NOT NULL DEFAULT 0,
			hit_count INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			accessed_at TEXT NOT NULL,
			expires_at TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_llm_prompt_cache_expires_at ON llm_prompt_cache(expires_at);`,
		`CREATE INDEX IF NOT EXISTS idx_llm_prompt_cache_accessed_at ON llm_prompt_cache(accessed_at);`,
		`CREATE INDEX IF NOT EXISTS idx_llm_prompt_cache_provider_model ON llm_prompt_cache(provider_id, model);`,

		`CREATE TABLE IF NOT EXISTS understanding_sessions (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			intent_json TEXT NOT NULL DEFAULT '{}',
			rounds_json TEXT NOT NULL DEFAULT '[]',
			state TEXT NOT NULL DEFAULT 'active',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_understanding_sessions_user_state ON understanding_sessions(user_id, state);`,

		`CREATE TABLE IF NOT EXISTS workflow_states (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			type TEXT NOT NULL,
			template_type TEXT NOT NULL,
			intent_json TEXT NOT NULL DEFAULT '{}',
			current_phase TEXT NOT NULL DEFAULT '',
			phase_outputs_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_states_user ON workflow_states(user_id);`,

		`CREATE TABLE IF NOT EXISTS a2a_group_profiles (
			tenant_id TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			display_name TEXT NOT NULL DEFAULT '',
			discoverable INTEGER NOT NULL DEFAULT 0,
			available INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL,
			profile_json TEXT NOT NULL,
			PRIMARY KEY (tenant_id, agent_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_a2a_group_profiles_active ON a2a_group_profiles(tenant_id, discoverable, available, updated_at DESC);`,

		`CREATE TABLE IF NOT EXISTS a2a_group_sessions (
			tenant_id TEXT NOT NULL,
			session_id TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT '',
			topic TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			session_json TEXT NOT NULL,
			PRIMARY KEY (tenant_id, session_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_a2a_group_sessions_updated ON a2a_group_sessions(tenant_id, updated_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_a2a_group_sessions_status ON a2a_group_sessions(tenant_id, status, updated_at DESC);`,

		`CREATE TABLE IF NOT EXISTS a2a_group_invites (
			tenant_id TEXT NOT NULL,
			invite_id TEXT NOT NULL,
			session_id TEXT NOT NULL,
			to_id TEXT NOT NULL DEFAULT '',
			from_id TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			created_at TEXT NOT NULL,
			responded_at TEXT NOT NULL DEFAULT '',
			invite_json TEXT NOT NULL,
			PRIMARY KEY (tenant_id, invite_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_a2a_group_invites_to_status ON a2a_group_invites(tenant_id, to_id, status, created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_a2a_group_invites_session ON a2a_group_invites(tenant_id, session_id);`,

		`CREATE TABLE IF NOT EXISTS capabilities (
			id TEXT PRIMARY KEY,
			capability_type TEXT NOT NULL,
			publisher TEXT NOT NULL,
			capability_id TEXT NOT NULL,
			display_name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL,
			managed_by TEXT NOT NULL,
			status TEXT NOT NULL,
			relation_to_origin TEXT NOT NULL DEFAULT '',
			global_key TEXT NOT NULL,
			current_version_key TEXT NOT NULL DEFAULT '',
			origin_key TEXT NOT NULL DEFAULT '',
			origin_json TEXT NOT NULL DEFAULT '{}',
			provenance_json TEXT NOT NULL DEFAULT '{}',
			metadata_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_capabilities_global_key ON capabilities(global_key);`,
		`CREATE INDEX IF NOT EXISTS idx_capabilities_type_status ON capabilities(capability_type, status);`,
		`CREATE INDEX IF NOT EXISTS idx_capabilities_origin_key ON capabilities(origin_key);`,

		`CREATE TABLE IF NOT EXISTS capability_versions (
			id TEXT PRIMARY KEY,
			capability_ref TEXT NOT NULL,
			version TEXT NOT NULL,
			version_key TEXT NOT NULL,
			package_url TEXT NOT NULL DEFAULT '',
			package_checksum TEXT NOT NULL DEFAULT '',
			package_signature TEXT NOT NULL DEFAULT '',
			manifest_json TEXT NOT NULL DEFAULT '{}',
			type_config_json TEXT NOT NULL DEFAULT '{}',
			permissions_json TEXT NOT NULL DEFAULT '{}',
			pricing_json TEXT NOT NULL DEFAULT '{}',
			license_json TEXT NOT NULL DEFAULT '{}',
			compatibility_json TEXT NOT NULL DEFAULT '{}',
			status TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_capability_versions_key ON capability_versions(version_key);`,
		`CREATE INDEX IF NOT EXISTS idx_capability_versions_capability_ref ON capability_versions(capability_ref);`,

		`CREATE TABLE IF NOT EXISTS capability_acquisition_requests (
			id TEXT PRIMARY KEY,
			requester_user_id TEXT NOT NULL,
			capability_type TEXT NOT NULL,
			source TEXT NOT NULL,
			source_capability_key TEXT NOT NULL,
			source_version_key TEXT NOT NULL DEFAULT '',
			request_kind TEXT NOT NULL,
			status TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT '',
			price_json TEXT NOT NULL DEFAULT '{}',
			license_json TEXT NOT NULL DEFAULT '{}',
			hub_customer_id TEXT NOT NULL DEFAULT '',
			approval_json TEXT NOT NULL DEFAULT '{}',
			purchase_json TEXT NOT NULL DEFAULT '{}',
			result_capability_id TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_acquisition_status ON capability_acquisition_requests(status);`,

		`CREATE TABLE IF NOT EXISTS capability_licenses (
			id TEXT PRIMARY KEY,
			capability_ref TEXT NOT NULL,
			source TEXT NOT NULL,
			source_license_id TEXT NOT NULL DEFAULT '',
			license_type TEXT NOT NULL,
			scope TEXT NOT NULL,
			seats_total INTEGER NOT NULL DEFAULT 0,
			seats_used INTEGER NOT NULL DEFAULT 0,
			usage_quota INTEGER NOT NULL DEFAULT 0,
			expires_at TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			raw_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS managed_capability_deployments (
			id TEXT PRIMARY KEY,
			capability_ref TEXT NOT NULL,
			capability_version_key TEXT NOT NULL DEFAULT '',
			scope_json TEXT NOT NULL DEFAULT '{}',
			deployment_policy TEXT NOT NULL DEFAULT 'required',
			reinstall_if_removed INTEGER NOT NULL DEFAULT 1,
			retry_interval_minutes INTEGER NOT NULL DEFAULT 60,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_by TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_managed_capability_deployments_enabled ON managed_capability_deployments(enabled);`,

		`CREATE TABLE IF NOT EXISTS recommended_capabilities (
			id TEXT PRIMARY KEY,
			capability_ref TEXT NOT NULL,
			capability_version_key TEXT NOT NULL DEFAULT '',
			scope_json TEXT NOT NULL DEFAULT '{}',
			recommendation_reason TEXT NOT NULL DEFAULT '',
			allow_user_dismiss INTEGER NOT NULL DEFAULT 1,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_by TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_recommended_capabilities_enabled ON recommended_capabilities(enabled);`,

		`CREATE TABLE IF NOT EXISTS user_capability_inventory (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL DEFAULT '',
			user_email TEXT NOT NULL DEFAULT '',
			capability_ref TEXT NOT NULL,
			capability_version_key TEXT NOT NULL DEFAULT '',
			capability_type TEXT NOT NULL DEFAULT '',
			install_status TEXT NOT NULL DEFAULT 'installed',
			installed INTEGER NOT NULL DEFAULT 1,
			metadata_json TEXT NOT NULL DEFAULT '{}',
			last_seen_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_user_capability_inventory_user_cap ON user_capability_inventory(user_email, capability_ref);`,
		`CREATE INDEX IF NOT EXISTS idx_user_capability_inventory_seen ON user_capability_inventory(user_email, last_seen_at DESC);`,

		`CREATE TABLE IF NOT EXISTS mcp_secret_requirements (
			id TEXT PRIMARY KEY,
			capability_ref TEXT NOT NULL,
			version_key TEXT NOT NULL,
			name TEXT NOT NULL,
			label TEXT NOT NULL DEFAULT '',
			scope TEXT NOT NULL,
			storage_policy TEXT NOT NULL,
			required INTEGER NOT NULL DEFAULT 1,
			help_url TEXT NOT NULL DEFAULT '',
			metadata_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`DELETE FROM mcp_secret_requirements WHERE rowid NOT IN (SELECT MIN(rowid) FROM mcp_secret_requirements GROUP BY capability_ref, version_key, name);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_mcp_secret_requirement_key ON mcp_secret_requirements(capability_ref, version_key, name);`,

		`CREATE TABLE IF NOT EXISTS mcp_secret_bindings (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			mcp_server_id TEXT NOT NULL,
			requirement_name TEXT NOT NULL,
			storage TEXT NOT NULL,
			hub_secret_ref TEXT NOT NULL DEFAULT '',
			local_secret_ref TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			last_verified_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_mcp_secret_binding ON mcp_secret_bindings(user_id, mcp_server_id, requirement_name);`,

		`CREATE TABLE IF NOT EXISTS mcp_hub_secrets (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			mcp_server_id TEXT NOT NULL,
			requirement_name TEXT NOT NULL,
			secret_value TEXT NOT NULL,
			secret_digest TEXT NOT NULL DEFAULT '',
			metadata_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_mcp_hub_secret_key ON mcp_hub_secrets(user_id, mcp_server_id, requirement_name);`,
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
	alterStmts = append(alterStmts, `ALTER TABLE invitation_codes ADD COLUMN validity_days INTEGER NOT NULL DEFAULT 0`)
	alterStmts = append(alterStmts, `ALTER TABLE user_enrollments ADD COLUMN mobile TEXT NOT NULL DEFAULT ''`)
	alterStmts = append(alterStmts, `ALTER TABLE invitation_codes ADD COLUMN exported INTEGER NOT NULL DEFAULT 0`)
	alterStmts = append(alterStmts, `ALTER TABLE users ADD COLUMN smart_route INTEGER NOT NULL DEFAULT 0`)
	alterStmts = append(alterStmts, `ALTER TABLE invitation_codes ADD COLUMN vip INTEGER NOT NULL DEFAULT 0`)

	for _, stmt := range alterStmts {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
			return fmt.Errorf("run alter migration: %w", err)
		}
	}

	return nil
}
