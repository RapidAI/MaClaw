package sqlite

import (
	"database/sql"
	"fmt"
	"strings"
)

func isIgnorableMigrationError(err error) bool {
	if err == nil {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate column name") ||
		strings.Contains(msg, "already exists")
}

func isDeferredLegacyColumnMigrationError(stmt string, err error) bool {
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "no such column") {
		return false
	}
	stmt = strings.TrimSpace(strings.ToLower(stmt))
	return strings.HasPrefix(stmt, "create index") ||
		strings.HasPrefix(stmt, "create unique index") ||
		strings.HasPrefix(stmt, "delete from mcp_secret_requirements")
}

func RunMigrations(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tenants (
			id TEXT PRIMARY KEY,
			slug TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			primary_domain TEXT NOT NULL DEFAULT '',
			settings_json TEXT NOT NULL DEFAULT '{}',
			created_by_admin_id TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			deleted_at TEXT
		);`,
		`INSERT OR IGNORE INTO tenants (id, slug, name, status, settings_json, created_by_admin_id, created_at, updated_at)
		 VALUES ('tenant_default', 'default', 'Default Tenant', 'active', '{}', 'migration', datetime('now'), datetime('now'));`,
		`CREATE TABLE IF NOT EXISTS tenant_settings (
			tenant_id TEXT NOT NULL,
			key TEXT NOT NULL,
			value_json TEXT NOT NULL,
			updated_by_admin_id TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			PRIMARY KEY (tenant_id, key)
		);`,
		`CREATE TABLE IF NOT EXISTS tenant_digital_employee_authorizations (
			tenant_id TEXT PRIMARY KEY,
			enabled INTEGER NOT NULL DEFAULT 0,
			quota INTEGER NOT NULL DEFAULT 0,
			used INTEGER NOT NULL DEFAULT 0,
			valid_from TEXT,
			valid_until TEXT,
			status TEXT NOT NULL DEFAULT 'inactive',
			source TEXT NOT NULL DEFAULT 'manual',
			metadata_json TEXT NOT NULL DEFAULT '{}',
			updated_by_admin_id TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			created_at TEXT NOT NULL
		);`,
		`INSERT OR IGNORE INTO tenant_digital_employee_authorizations (tenant_id, enabled, quota, used, status, source, metadata_json, updated_by_admin_id, updated_at, created_at)
		 VALUES ('tenant_default', 0, 0, 0, 'inactive', 'migration', '{}', 'migration', datetime('now'), datetime('now'));`,
		`CREATE TABLE IF NOT EXISTS admin_users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			email TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			scope TEXT NOT NULL DEFAULT 'global',
			role TEXT NOT NULL DEFAULT 'global_owner',
			tenant_id TEXT NOT NULL DEFAULT '',
			display_name TEXT NOT NULL DEFAULT ''
		);`,

		`CREATE TABLE IF NOT EXISTS system_settings (
			key TEXT PRIMARY KEY,
			value_json TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			email TEXT NOT NULL,
			sn TEXT NOT NULL UNIQUE,
			status TEXT NOT NULL DEFAULT 'active',
			enrollment_status TEXT NOT NULL DEFAULT 'approved',
			smart_route INTEGER NOT NULL DEFAULT 0,
			email_verified INTEGER NOT NULL DEFAULT 0,
			email_verified_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(tenant_id, email)
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
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			email TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(tenant_id, email)
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_email_blocklist_tenant_email ON email_blocklist(tenant_id, email);`,

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

		`CREATE TABLE IF NOT EXISTS session_token_usage_snapshots (
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			session_id TEXT NOT NULL,
			user_id TEXT NOT NULL DEFAULT '',
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			cached_input_tokens INTEGER NOT NULL DEFAULT 0,
			cache_write_tokens INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (tenant_id, session_id)
		);`,
		`CREATE TABLE IF NOT EXISTS user_usage_daily (
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			user_email TEXT NOT NULL,
			day TEXT NOT NULL,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			cached_input_tokens INTEGER NOT NULL DEFAULT 0,
			cache_write_tokens INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (tenant_id, user_email, day)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_user_usage_daily_tenant_day ON user_usage_daily(tenant_id, day);`,

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
			tenant_id TEXT NOT NULL DEFAULT '',
			admin_user_id TEXT NOT NULL,
			action TEXT NOT NULL,
			payload_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS failure_event_logs (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			category TEXT NOT NULL,
			event_code TEXT NOT NULL,
			message TEXT NOT NULL,
			entity_id TEXT NOT NULL DEFAULT '',
			email TEXT NOT NULL DEFAULT '',
			client_ip TEXT NOT NULL DEFAULT '',
			details_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_admin_audit_logs_tenant_created_at ON admin_audit_logs(tenant_id, created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_failure_event_logs_created_at ON failure_event_logs(created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_failure_event_logs_category_created_at ON failure_event_logs(category, created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_failure_event_logs_tenant_created_at ON failure_event_logs(tenant_id, created_at DESC);`,

		`CREATE TABLE IF NOT EXISTS knowledge_shares (
				knowledge_id TEXT PRIMARY KEY,
				tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
				owner_user_id TEXT NOT NULL DEFAULT '',
				owner_user_email TEXT NOT NULL DEFAULT '',
				title TEXT NOT NULL DEFAULT '',
				description TEXT NOT NULL DEFAULT '',
				visibility_scope TEXT NOT NULL DEFAULT 'private',
				visibility_users_json TEXT NOT NULL DEFAULT '[]',
				source_summary_json TEXT NOT NULL DEFAULT '{}',
				share_url TEXT NOT NULL DEFAULT '',
				hub_id TEXT NOT NULL DEFAULT '',
				storage_ref TEXT NOT NULL DEFAULT '',
				status TEXT NOT NULL DEFAULT 'active',
				view_count INTEGER NOT NULL DEFAULT 0,
				import_count INTEGER NOT NULL DEFAULT 0,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				published_at TEXT NOT NULL,
				forced_deleted_by TEXT NOT NULL DEFAULT '',
				forced_deleted_reason TEXT NOT NULL DEFAULT '',
				forced_deleted_at TEXT
			);`,
		`CREATE INDEX IF NOT EXISTS idx_knowledge_shares_tenant_published ON knowledge_shares(tenant_id, published_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_knowledge_shares_owner_published ON knowledge_shares(owner_user_email, published_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_knowledge_shares_expires_at ON knowledge_shares(expires_at);`,
		`CREATE INDEX IF NOT EXISTS idx_knowledge_shares_view_count ON knowledge_shares(view_count DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_knowledge_shares_import_count ON knowledge_shares(import_count DESC);`,

		`CREATE TABLE IF NOT EXISTS invitation_codes (
				id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			code TEXT NOT NULL UNIQUE,
			status TEXT NOT NULL DEFAULT 'unused',
			used_by_email TEXT NOT NULL DEFAULT '',
			used_at DATETIME,
			validity_days INTEGER NOT NULL DEFAULT 0,
			exported INTEGER NOT NULL DEFAULT 0,
			vip INTEGER NOT NULL DEFAULT 0,
			llm_service_group_id TEXT NOT NULL DEFAULT '',
			llm_grant_duration_days INTEGER NOT NULL DEFAULT 0,
			llm_grant_credits REAL NOT NULL DEFAULT 0,
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

		`CREATE TABLE IF NOT EXISTS content_audit_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
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
		`CREATE INDEX IF NOT EXISTS idx_content_audit_logs_tenant_timestamp ON content_audit_logs(tenant_id, timestamp);`,
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

		`CREATE TABLE IF NOT EXISTS user_data_migration_exports (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			user_id TEXT NOT NULL,
			source_machine_id TEXT NOT NULL,
			source_machine_name TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			compressed_size INTEGER NOT NULL DEFAULT 0,
			encrypted_size INTEGER NOT NULL DEFAULT 0,
			encrypted_sha256 TEXT NOT NULL DEFAULT '',
			plain_sha256 TEXT NOT NULL DEFAULT '',
			chunk_size INTEGER NOT NULL DEFAULT 0,
			chunk_count INTEGER NOT NULL DEFAULT 0,
			manifest_json TEXT NOT NULL DEFAULT '{}',
			claimed_by_machine_id TEXT NOT NULL DEFAULT '',
			claimed_at TEXT,
			claim_expires_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			imported_at TEXT,
			deleted_at TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_user_data_migration_exports_owner ON user_data_migration_exports(tenant_id, user_id, status, updated_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_user_data_migration_exports_source ON user_data_migration_exports(tenant_id, user_id, source_machine_id);`,

		`CREATE TABLE IF NOT EXISTS user_data_migration_chunks (
			export_id TEXT NOT NULL,
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			user_id TEXT NOT NULL,
			chunk_index INTEGER NOT NULL,
			size INTEGER NOT NULL DEFAULT 0,
			sha256 TEXT NOT NULL DEFAULT '',
			uploaded_at TEXT NOT NULL,
			PRIMARY KEY (export_id, chunk_index)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_user_data_migration_chunks_owner ON user_data_migration_chunks(tenant_id, user_id, export_id);`,

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
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
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
		`CREATE INDEX IF NOT EXISTS idx_workflow_states_tenant_user ON workflow_states(tenant_id, user_id);`,

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
		`CREATE INDEX IF NOT EXISTS idx_a2a_group_invites_from_status ON a2a_group_invites(tenant_id, from_id, status, created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_a2a_group_invites_session ON a2a_group_invites(tenant_id, session_id);`,

		`CREATE TABLE IF NOT EXISTS capabilities (
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
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
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_capabilities_global_key ON capabilities(tenant_id, global_key);`,
		`CREATE INDEX IF NOT EXISTS idx_capabilities_type_status ON capabilities(tenant_id, capability_type, status);`,
		`CREATE INDEX IF NOT EXISTS idx_capabilities_origin_key ON capabilities(tenant_id, origin_key);`,

		`CREATE TABLE IF NOT EXISTS capability_versions (
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
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
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_capability_versions_key ON capability_versions(tenant_id, version_key);`,
		`CREATE INDEX IF NOT EXISTS idx_capability_versions_capability_ref ON capability_versions(tenant_id, capability_ref);`,

		`CREATE TABLE IF NOT EXISTS capability_acquisition_requests (
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
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
		`CREATE INDEX IF NOT EXISTS idx_acquisition_status ON capability_acquisition_requests(tenant_id, status);`,

		`CREATE TABLE IF NOT EXISTS capability_licenses (
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
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
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
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
		`CREATE INDEX IF NOT EXISTS idx_managed_capability_deployments_enabled ON managed_capability_deployments(tenant_id, enabled);`,

		`CREATE TABLE IF NOT EXISTS recommended_capabilities (
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
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
		`CREATE INDEX IF NOT EXISTS idx_recommended_capabilities_enabled ON recommended_capabilities(tenant_id, enabled);`,

		`CREATE TABLE IF NOT EXISTS user_capability_inventory (
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
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
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_user_capability_inventory_user_cap ON user_capability_inventory(tenant_id, user_email, capability_ref);`,
		`CREATE INDEX IF NOT EXISTS idx_user_capability_inventory_seen ON user_capability_inventory(tenant_id, user_email, last_seen_at DESC);`,

		`CREATE TABLE IF NOT EXISTS mcp_secret_requirements (
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
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
		`DELETE FROM mcp_secret_requirements WHERE rowid NOT IN (SELECT MIN(rowid) FROM mcp_secret_requirements GROUP BY tenant_id, capability_ref, version_key, name);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_mcp_secret_requirement_key ON mcp_secret_requirements(tenant_id, capability_ref, version_key, name);`,

		`CREATE TABLE IF NOT EXISTS mcp_secret_bindings (
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
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
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_mcp_secret_binding ON mcp_secret_bindings(tenant_id, user_id, mcp_server_id, requirement_name);`,

		`CREATE TABLE IF NOT EXISTS mcp_hub_secrets (
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
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
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_mcp_hub_secret_key ON mcp_hub_secrets(tenant_id, user_id, mcp_server_id, requirement_name);`,

		`CREATE TABLE IF NOT EXISTS security_groups (
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			parent_id TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_security_groups_parent ON security_groups(parent_id);`,
		`CREATE INDEX IF NOT EXISTS idx_security_groups_tenant_parent ON security_groups(tenant_id, parent_id);`,
		`CREATE TABLE IF NOT EXISTS security_group_members (
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			email TEXT NOT NULL,
			group_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY (tenant_id, email)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_sgm_group ON security_group_members(group_id);`,
		`CREATE INDEX IF NOT EXISTS idx_sgm_tenant_group ON security_group_members(tenant_id, group_id);`,
		`CREATE TABLE IF NOT EXISTS security_policies (
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			group_id TEXT NOT NULL,
			policy_json TEXT NOT NULL DEFAULT '{}',
			updated_at TEXT NOT NULL,
			PRIMARY KEY (tenant_id, group_id)
		);`,

		// ---------------------------------------------------------------
		// Approval Workflow Tables (VE Approval Workflow feature)
		// ---------------------------------------------------------------

		// Workflow definitions (owned by users)
		`CREATE TABLE IF NOT EXISTS workflow_definitions (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			owner_id TEXT NOT NULL,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now')) ,
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_def_owner ON workflow_definitions(owner_id);`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_def_tenant_owner ON workflow_definitions(tenant_id, owner_id);`,

		// Workflow versions (revisions of a definition)
		`CREATE TABLE IF NOT EXISTS workflow_versions (
			id TEXT PRIMARY KEY,
			workflow_id TEXT NOT NULL REFERENCES workflow_definitions(id),
			version_number TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'draft',
			graph_json TEXT NOT NULL DEFAULT '{}',
			submitted_at TEXT,
			published_at TEXT,
			rejection_reason TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		);`,
		`CREATE INDEX IF NOT EXISTS idx_wf_ver_workflow ON workflow_versions(workflow_id);`,
		`CREATE INDEX IF NOT EXISTS idx_wf_ver_status ON workflow_versions(status);`,
		// Enforce at most one published version per workflow.
		// SQLite supports partial indexes with WHERE clause.
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_wf_ver_published ON workflow_versions(workflow_id) WHERE status = 'published';`,

		// Workflow instances (running executions)
		`CREATE TABLE IF NOT EXISTS workflow_instances (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			workflow_id TEXT NOT NULL,
			version_id TEXT NOT NULL REFERENCES workflow_versions(id),
			status TEXT NOT NULL DEFAULT 'running',
			current_node_id TEXT NOT NULL DEFAULT '',
			instance_data TEXT NOT NULL DEFAULT '{}',
			trigger_data TEXT NOT NULL DEFAULT '',
			row_version INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			completed_at TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_wf_inst_status ON workflow_instances(status);`,
		`CREATE INDEX IF NOT EXISTS idx_wf_inst_workflow ON workflow_instances(workflow_id);`,
		`CREATE INDEX IF NOT EXISTS idx_wf_inst_tenant_status ON workflow_instances(tenant_id, status);`,

		// Node executions within an instance
		`CREATE TABLE IF NOT EXISTS node_executions (
			id TEXT PRIMARY KEY,
			instance_id TEXT NOT NULL REFERENCES workflow_instances(id),
			node_id TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			started_at TEXT NOT NULL DEFAULT (datetime('now')),
			completed_at TEXT,
			result_json TEXT,
			fail_reason TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE INDEX IF NOT EXISTS idx_node_exec_instance ON node_executions(instance_id);`,
		`CREATE INDEX IF NOT EXISTS idx_node_exec_status ON node_executions(status);`,

		// Audit trail (append-only, immutable)
		`CREATE TABLE IF NOT EXISTS approval_audit_trail (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			instance_id TEXT NOT NULL,
			node_id TEXT NOT NULL DEFAULT '',
			event_type TEXT NOT NULL,
			actor_id TEXT NOT NULL DEFAULT '',
			decision TEXT NOT NULL DEFAULT '',
			matched_rule TEXT NOT NULL DEFAULT '',
			rationale TEXT NOT NULL DEFAULT '',
			details_json TEXT NOT NULL DEFAULT '{}',
			timestamp TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now'))
		);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_instance ON approval_audit_trail(instance_id);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_actor ON approval_audit_trail(actor_id);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_tenant_instance ON approval_audit_trail(tenant_id, instance_id);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON approval_audit_trail(timestamp);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_decision ON approval_audit_trail(decision);`,

		// Trigger to prevent UPDATE on approval_audit_trail (immutability enforcement)
		`CREATE TRIGGER IF NOT EXISTS trg_audit_trail_no_update
		 BEFORE UPDATE ON approval_audit_trail
		 BEGIN
		   SELECT RAISE(ABORT, 'approval_audit_trail is immutable: UPDATE not allowed');
		 END;`,

		// Trigger to prevent DELETE on approval_audit_trail (immutability enforcement)
		`CREATE TRIGGER IF NOT EXISTS trg_audit_trail_no_delete
		 BEFORE DELETE ON approval_audit_trail
		 BEGIN
		   SELECT RAISE(ABORT, 'approval_audit_trail is immutable: DELETE not allowed');
		 END;`,

		// ---------------------------------------------------------------
		// Confirmation tracking table (post-completion executor/notifier confirmations)
		`CREATE TABLE IF NOT EXISTS confirmations (
			id                      TEXT PRIMARY KEY,
			tenant_id               TEXT NOT NULL DEFAULT 'tenant_default',
			instance_id             TEXT NOT NULL REFERENCES workflow_instances(id),
			recipient_id            TEXT NOT NULL,
			type                    TEXT NOT NULL,
			status                  TEXT NOT NULL DEFAULT 'pending',
			notes                   TEXT DEFAULT '',
			timeout_hours           INTEGER NOT NULL DEFAULT 48,
			max_reminders           INTEGER NOT NULL DEFAULT 3,
			reminders_sent          INTEGER NOT NULL DEFAULT 0,
			reminder_interval_hours INTEGER NOT NULL DEFAULT 24,
			last_reminder_at        TEXT,
			confirmed_at            TEXT,
			auto_closed_at          TEXT,
			auto_close_reason       TEXT DEFAULT '',
			created_at              TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f','now'))
		);`,
		`CREATE INDEX IF NOT EXISTS idx_confirm_instance ON confirmations(instance_id);`,
		`CREATE INDEX IF NOT EXISTS idx_confirm_tenant_pending ON confirmations(tenant_id, status, recipient_id);`,
		`CREATE INDEX IF NOT EXISTS idx_confirm_recipient ON confirmations(recipient_id);`,
		`CREATE INDEX IF NOT EXISTS idx_confirm_status ON confirmations(status);`,
		`CREATE INDEX IF NOT EXISTS idx_confirm_pending ON confirmations(status, recipient_id) WHERE status = 'pending';`,
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil && !isDeferredLegacyColumnMigrationError(stmt, err) {
			return fmt.Errorf("run migration: %w", err)
		}
	}

	alterStmts := []string{
		`ALTER TABLE admin_users ADD COLUMN scope TEXT NOT NULL DEFAULT 'global'`,
		`ALTER TABLE admin_users ADD COLUMN role TEXT NOT NULL DEFAULT 'global_owner'`,
		`ALTER TABLE admin_users ADD COLUMN tenant_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE admin_users ADD COLUMN display_name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE user_enrollments ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE email_blocklist ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE invitation_codes ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE email_invites ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE machines ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE viewer_tokens ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE login_tokens ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE sessions ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE admin_audit_logs ADD COLUMN tenant_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE failure_event_logs ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE content_audit_logs ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE capabilities ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE capability_versions ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE capability_acquisition_requests ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE capability_licenses ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE managed_capability_deployments ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE recommended_capabilities ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE user_capability_inventory ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE mcp_secret_requirements ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE mcp_secret_bindings ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE mcp_hub_secrets ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE security_groups ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE security_group_members ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE security_policies ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
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
	alterStmts = append(alterStmts, `CREATE INDEX IF NOT EXISTS idx_admin_audit_logs_tenant_created_at ON admin_audit_logs(tenant_id, created_at DESC)`)
	alterStmts = append(alterStmts, `CREATE INDEX IF NOT EXISTS idx_failure_event_logs_tenant_created_at ON failure_event_logs(tenant_id, created_at DESC)`)
	alterStmts = append(alterStmts, `CREATE INDEX IF NOT EXISTS idx_content_audit_logs_tenant_timestamp ON content_audit_logs(tenant_id, timestamp)`)
	alterStmts = append(alterStmts, `ALTER TABLE users ADD COLUMN smart_route INTEGER NOT NULL DEFAULT 0`)
	alterStmts = append(alterStmts, `ALTER TABLE invitation_codes ADD COLUMN vip INTEGER NOT NULL DEFAULT 0`)
	alterStmts = append(alterStmts, `ALTER TABLE invitation_codes ADD COLUMN llm_service_group_id TEXT NOT NULL DEFAULT ''`)
	alterStmts = append(alterStmts, `ALTER TABLE invitation_codes ADD COLUMN llm_grant_duration_days INTEGER NOT NULL DEFAULT 0`)
	alterStmts = append(alterStmts, `ALTER TABLE invitation_codes ADD COLUMN llm_grant_credits REAL NOT NULL DEFAULT 0`)
	alterStmts = append(alterStmts, `ALTER TABLE node_executions ADD COLUMN node_type TEXT NOT NULL DEFAULT ''`)
	alterStmts = append(alterStmts, `ALTER TABLE workflow_definitions ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`)
	alterStmts = append(alterStmts, `ALTER TABLE workflow_instances ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`)
	alterStmts = append(alterStmts, `ALTER TABLE workflow_instances ADD COLUMN row_version INTEGER NOT NULL DEFAULT 0`)
	alterStmts = append(alterStmts, `ALTER TABLE approval_audit_trail ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`)
	alterStmts = append(alterStmts, `ALTER TABLE confirmations ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`)
	alterStmts = append(alterStmts, `CREATE INDEX IF NOT EXISTS idx_workflow_def_tenant_owner ON workflow_definitions(tenant_id, owner_id)`)
	alterStmts = append(alterStmts, `CREATE INDEX IF NOT EXISTS idx_wf_inst_tenant_status ON workflow_instances(tenant_id, status)`)
	alterStmts = append(alterStmts, `CREATE INDEX IF NOT EXISTS idx_audit_tenant_instance ON approval_audit_trail(tenant_id, instance_id)`)
	alterStmts = append(alterStmts, `CREATE INDEX IF NOT EXISTS idx_confirm_tenant_pending ON confirmations(tenant_id, status, recipient_id)`)
	alterStmts = append(alterStmts, `ALTER TABLE understanding_sessions ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`)
	alterStmts = append(alterStmts, `ALTER TABLE workflow_states ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`)
	alterStmts = append(alterStmts, `CREATE INDEX IF NOT EXISTS idx_understanding_sessions_tenant_user_state ON understanding_sessions(tenant_id, user_id, state)`)
	alterStmts = append(alterStmts, `CREATE INDEX IF NOT EXISTS idx_workflow_states_tenant_user ON workflow_states(tenant_id, user_id)`)
	alterStmts = append(alterStmts, `ALTER TABLE users ADD COLUMN email_verified INTEGER NOT NULL DEFAULT 0`)
	alterStmts = append(alterStmts, `ALTER TABLE users ADD COLUMN email_verified_at TEXT NOT NULL DEFAULT ''`)
	alterStmts = append(alterStmts, `ALTER TABLE knowledge_shares ADD COLUMN expires_at TEXT`)

	// machine_heartbeat_log: stores timestamped heartbeats for accurate
	// usage-duration calculation. SummarizeUserDurations merges consecutive
	// heartbeats (gap < 5 min) into online intervals.
	alterStmts = append(alterStmts, `CREATE TABLE IF NOT EXISTS machine_heartbeat_log (
		tenant_id TEXT NOT NULL,
		machine_id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		heartbeat_at TEXT NOT NULL,
		UNIQUE(tenant_id, machine_id, heartbeat_at)
	)`)
	alterStmts = append(alterStmts, `CREATE INDEX IF NOT EXISTS idx_heartbeat_log_tenant_user_at ON machine_heartbeat_log(tenant_id, user_id, heartbeat_at)`)
	alterStmts = append(alterStmts, `CREATE INDEX IF NOT EXISTS idx_heartbeat_log_cleanup ON machine_heartbeat_log(heartbeat_at)`)

	for _, stmt := range alterStmts {
		if _, err := db.Exec(stmt); err != nil && !isIgnorableMigrationError(err) {
			return fmt.Errorf("run alter migration: %w", err)
		}
	}

	if err := migrateTenantScopedAdminUsersTable(db); err != nil {
		return err
	}
	if err := migrateTenantScopedAdminUserIndexes(db); err != nil {
		return err
	}
	if err := migrateTenantScopedUserTable(db); err != nil {
		return err
	}
	if err := migrateTenantScopedEmailBlocklistTable(db); err != nil {
		return err
	}
	if err := migrateTenantScopedCapabilityIndexes(db); err != nil {
		return err
	}
	if err := migrateTenantScopedEmailBlocklistIndexes(db); err != nil {
		return err
	}
	if err := migrateTenantScopedUserIndexes(db); err != nil {
		return err
	}

	// Performance indexes for high-frequency query patterns.
	perfIndexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_sessions_machine_status ON sessions(machine_id, status);`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_user_status ON sessions(tenant_id, user_id, status);`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_updated_at ON sessions(updated_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_machines_user_tenant ON machines(tenant_id, user_id, status);`,
		`CREATE INDEX IF NOT EXISTS idx_viewer_tokens_user_id ON viewer_tokens(user_id);`,
		`CREATE INDEX IF NOT EXISTS idx_login_tokens_expires_at ON login_tokens(expires_at);`,
	}
	for _, stmt := range perfIndexes {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("perf index: %w", err)
		}
	}

	return nil
}

func migrateTenantScopedAdminUsersTable(db *sql.DB) error {
	usernameUnique, err := tableSQLContains(db, "admin_users", "username text not null unique")
	if err != nil {
		return err
	}
	emailUnique, err := tableSQLContains(db, "admin_users", "email text not null unique")
	if err != nil {
		return err
	}
	if !usernameUnique && !emailUnique {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin admin users tenant rebuild: %w", err)
	}
	defer tx.Rollback()

	stmts := []string{
		`CREATE TABLE admin_users_new (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			email TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			scope TEXT NOT NULL DEFAULT 'global',
			role TEXT NOT NULL DEFAULT 'global_owner',
			tenant_id TEXT NOT NULL DEFAULT '',
			display_name TEXT NOT NULL DEFAULT ''
		);`,
		`INSERT INTO admin_users_new (id, username, password_hash, email, status, created_at, updated_at, scope, role, tenant_id, display_name)
		 SELECT id, username, password_hash, email, status, created_at, updated_at,
		        COALESCE(NULLIF(scope, ''), 'global'),
		        COALESCE(NULLIF(role, ''), CASE WHEN COALESCE(NULLIF(scope, ''), 'global') = 'tenant' THEN 'tenant_owner' ELSE 'global_owner' END),
		        CASE WHEN COALESCE(NULLIF(scope, ''), 'global') = 'tenant' THEN COALESCE(NULLIF(tenant_id, ''), 'tenant_default') ELSE '' END,
		        COALESCE(display_name, '')
		 FROM admin_users;`,
		`DROP TABLE admin_users;`,
		`ALTER TABLE admin_users_new RENAME TO admin_users;`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("rebuild admin users tenant table: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit admin users tenant rebuild: %w", err)
	}
	return nil
}

func migrateTenantScopedAdminUserIndexes(db *sql.DB) error {
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_admin_users_scope_tenant_username ON admin_users(scope, tenant_id, username)`); err != nil {
		return fmt.Errorf("create admin users scoped username index: %w", err)
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_admin_users_scope_tenant_email ON admin_users(scope, tenant_id, email)`); err != nil {
		return fmt.Errorf("create admin users scoped email index: %w", err)
	}
	return nil
}

func migrateTenantScopedUserTable(db *sql.DB) error {
	needs, err := tableSQLContains(db, "users", "email text not null unique")
	if err != nil || !needs {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin users tenant rebuild: %w", err)
	}
	defer tx.Rollback()

	stmts := []string{
		`CREATE TABLE users_new (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			email TEXT NOT NULL,
			sn TEXT NOT NULL UNIQUE,
			status TEXT NOT NULL DEFAULT 'active',
			enrollment_status TEXT NOT NULL DEFAULT 'approved',
			smart_route INTEGER NOT NULL DEFAULT 0,
			email_verified INTEGER NOT NULL DEFAULT 0,
			email_verified_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(tenant_id, email)
		);`,
		`INSERT INTO users_new (id, tenant_id, email, sn, status, enrollment_status, smart_route, created_at, updated_at)
		 SELECT id, COALESCE(NULLIF(tenant_id, ''), 'tenant_default'), email, sn, status, enrollment_status, smart_route, created_at, updated_at
		 FROM users;`,
		`DROP TABLE users;`,
		`ALTER TABLE users_new RENAME TO users;`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("rebuild users tenant table: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit users tenant rebuild: %w", err)
	}
	return nil
}

func migrateTenantScopedUserIndexes(db *sql.DB) error {
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_tenant_email ON users(tenant_id, email)`); err != nil {
		return fmt.Errorf("create users tenant email index: %w", err)
	}
	return nil
}

func migrateTenantScopedEmailBlocklistTable(db *sql.DB) error {
	needs, err := tableSQLContains(db, "email_blocklist", "email text not null unique")
	if err != nil || !needs {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin email blocklist tenant rebuild: %w", err)
	}
	defer tx.Rollback()

	stmts := []string{
		`CREATE TABLE email_blocklist_new (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			email TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(tenant_id, email)
		);`,
		`INSERT INTO email_blocklist_new (id, tenant_id, email, reason, created_at, updated_at)
		 SELECT id, COALESCE(NULLIF(tenant_id, ''), 'tenant_default'), email, reason, created_at, updated_at
		 FROM email_blocklist;`,
		`DROP TABLE email_blocklist;`,
		`ALTER TABLE email_blocklist_new RENAME TO email_blocklist;`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("rebuild email blocklist tenant table: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit email blocklist tenant rebuild: %w", err)
	}
	return nil
}

func migrateTenantScopedEmailBlocklistIndexes(db *sql.DB) error {
	if _, err := db.Exec(`DROP INDEX IF EXISTS idx_email_blocklist_tenant_email`); err != nil {
		return fmt.Errorf("drop email blocklist tenant index: %w", err)
	}
	if _, err := db.Exec(`DELETE FROM email_blocklist WHERE rowid NOT IN (SELECT MIN(rowid) FROM email_blocklist GROUP BY tenant_id, email)`); err != nil {
		return fmt.Errorf("dedupe email blocklist: %w", err)
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_email_blocklist_tenant_email ON email_blocklist(tenant_id, email)`); err != nil {
		return fmt.Errorf("create email blocklist tenant index: %w", err)
	}
	return nil
}

func tableSQLContains(db *sql.DB, tableName, needle string) (bool, error) {
	var tableSQL string
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, tableName).Scan(&tableSQL)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("read table schema %s: %w", tableName, err)
	}
	normalized := strings.Join(strings.Fields(strings.ToLower(tableSQL)), " ")
	return strings.Contains(normalized, needle), nil
}

func migrateTenantScopedCapabilityIndexes(db *sql.DB) error {
	dropStmts := []string{
		`DROP INDEX IF EXISTS idx_capabilities_global_key`,
		`DROP INDEX IF EXISTS idx_capabilities_type_status`,
		`DROP INDEX IF EXISTS idx_capabilities_origin_key`,
		`DROP INDEX IF EXISTS idx_capability_versions_key`,
		`DROP INDEX IF EXISTS idx_capability_versions_capability_ref`,
		`DROP INDEX IF EXISTS idx_acquisition_status`,
		`DROP INDEX IF EXISTS idx_managed_capability_deployments_enabled`,
		`DROP INDEX IF EXISTS idx_recommended_capabilities_enabled`,
		`DROP INDEX IF EXISTS idx_user_capability_inventory_user_cap`,
		`DROP INDEX IF EXISTS idx_user_capability_inventory_seen`,
		`DROP INDEX IF EXISTS idx_mcp_secret_requirement_key`,
		`DROP INDEX IF EXISTS idx_mcp_secret_binding`,
		`DROP INDEX IF EXISTS idx_mcp_hub_secret_key`,
	}
	for _, stmt := range dropStmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("drop legacy capability index: %w", err)
		}
	}

	dedupeStmts := []string{
		`DELETE FROM capabilities WHERE rowid NOT IN (SELECT MIN(rowid) FROM capabilities GROUP BY tenant_id, global_key)`,
		`DELETE FROM capability_versions WHERE rowid NOT IN (SELECT MIN(rowid) FROM capability_versions GROUP BY tenant_id, version_key)`,
		`DELETE FROM user_capability_inventory WHERE rowid NOT IN (SELECT MIN(rowid) FROM user_capability_inventory GROUP BY tenant_id, user_email, capability_ref)`,
		`DELETE FROM mcp_secret_requirements WHERE rowid NOT IN (SELECT MIN(rowid) FROM mcp_secret_requirements GROUP BY tenant_id, capability_ref, version_key, name)`,
		`DELETE FROM mcp_secret_bindings WHERE rowid NOT IN (SELECT MIN(rowid) FROM mcp_secret_bindings GROUP BY tenant_id, user_id, mcp_server_id, requirement_name)`,
		`DELETE FROM mcp_hub_secrets WHERE rowid NOT IN (SELECT MIN(rowid) FROM mcp_hub_secrets GROUP BY tenant_id, user_id, mcp_server_id, requirement_name)`,
	}
	for _, stmt := range dedupeStmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("dedupe tenant capability data: %w", err)
		}
	}

	createStmts := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_capabilities_global_key ON capabilities(tenant_id, global_key)`,
		`CREATE INDEX IF NOT EXISTS idx_capabilities_type_status ON capabilities(tenant_id, capability_type, status)`,
		`CREATE INDEX IF NOT EXISTS idx_capabilities_origin_key ON capabilities(tenant_id, origin_key)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_capability_versions_key ON capability_versions(tenant_id, version_key)`,
		`CREATE INDEX IF NOT EXISTS idx_capability_versions_capability_ref ON capability_versions(tenant_id, capability_ref)`,
		`CREATE INDEX IF NOT EXISTS idx_acquisition_status ON capability_acquisition_requests(tenant_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_managed_capability_deployments_enabled ON managed_capability_deployments(tenant_id, enabled)`,
		`CREATE INDEX IF NOT EXISTS idx_recommended_capabilities_enabled ON recommended_capabilities(tenant_id, enabled)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_user_capability_inventory_user_cap ON user_capability_inventory(tenant_id, user_email, capability_ref)`,
		`CREATE INDEX IF NOT EXISTS idx_user_capability_inventory_seen ON user_capability_inventory(tenant_id, user_email, last_seen_at DESC)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_mcp_secret_requirement_key ON mcp_secret_requirements(tenant_id, capability_ref, version_key, name)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_mcp_secret_binding ON mcp_secret_bindings(tenant_id, user_id, mcp_server_id, requirement_name)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_mcp_hub_secret_key ON mcp_hub_secrets(tenant_id, user_id, mcp_server_id, requirement_name)`,
	}
	for _, stmt := range createStmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("create tenant capability index: %w", err)
		}
	}
	return nil
}
