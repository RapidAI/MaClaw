package db

import (
	"database/sql"
	"fmt"
	"log"
)

// migrations is an ordered list of DDL statements. Each entry is applied once.
// Append new migrations at the end; never reorder or remove existing entries.
var migrations = []string{
	// 1: roles — independent role definitions
	`CREATE TABLE IF NOT EXISTS roles (
		id                 TEXT PRIMARY KEY,
		name               TEXT NOT NULL UNIQUE,
		code               TEXT NOT NULL UNIQUE,
		description        TEXT NOT NULL DEFAULT '',
		default_strengths  TEXT NOT NULL DEFAULT '[]',
		applicable_tasks   TEXT NOT NULL DEFAULT '[]',
		status             TEXT NOT NULL DEFAULT 'active',
		sort_order         INTEGER NOT NULL DEFAULT 0,
		created_at         TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at         TEXT NOT NULL DEFAULT (datetime('now'))
	);`,

	// 2: colleagues — digital workers, with role_id FK to roles
	`CREATE TABLE IF NOT EXISTS colleagues (
		id          TEXT PRIMARY KEY,
		name        TEXT NOT NULL,
		avatar      TEXT NOT NULL DEFAULT '',
		role_id     TEXT NOT NULL DEFAULT '',
		description TEXT NOT NULL DEFAULT '',
		strengths   TEXT NOT NULL DEFAULT '[]',
		tasks       TEXT NOT NULL DEFAULT '[]',
		status      TEXT NOT NULL DEFAULT 'active',
		created_at  TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at  TEXT NOT NULL DEFAULT (datetime('now')),
		FOREIGN KEY (role_id) REFERENCES roles(id)
	);`,

	// 3: role_assignment_log — audit trail for role changes
	`CREATE TABLE IF NOT EXISTS role_assignment_log (
		id            TEXT PRIMARY KEY,
		colleague_id  TEXT NOT NULL,
		old_role_id   TEXT NOT NULL DEFAULT '',
		new_role_id   TEXT NOT NULL,
		reason        TEXT NOT NULL DEFAULT '',
		assigned_at   TEXT NOT NULL DEFAULT (datetime('now')),
		FOREIGN KEY (colleague_id) REFERENCES colleagues(id),
		FOREIGN KEY (new_role_id) REFERENCES roles(id)
	);`,

	// 4: shared_memories — enterprise/role/team knowledge for context injection
	`CREATE TABLE IF NOT EXISTS shared_memories (
		id          TEXT PRIMARY KEY,
		title       TEXT NOT NULL,
		content     TEXT NOT NULL DEFAULT '',
		level       TEXT NOT NULL DEFAULT 'enterprise',
		scope       TEXT NOT NULL DEFAULT 'all',
		tags        TEXT NOT NULL DEFAULT '[]',
		version     INTEGER NOT NULL DEFAULT 1,
		status      TEXT NOT NULL DEFAULT 'active',
		created_at  TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
	);`,

	// 5: capability_packages — skills / "会做的事" that can be bound to colleagues
	`CREATE TABLE IF NOT EXISTS capability_packages (
		id          TEXT PRIMARY KEY,
		name        TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		category    TEXT NOT NULL DEFAULT 'general',
		version     TEXT NOT NULL DEFAULT '1.0.0',
		source      TEXT NOT NULL DEFAULT 'local',
		risk_level  TEXT NOT NULL DEFAULT 'low',
		status      TEXT NOT NULL DEFAULT 'active',
		created_at  TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
	);`,

	// 6: colleague_capability_bindings — many-to-many between colleagues and capabilities
	`CREATE TABLE IF NOT EXISTS colleague_capability_bindings (
		id             TEXT PRIMARY KEY,
		colleague_id   TEXT NOT NULL,
		capability_id  TEXT NOT NULL,
		bound_at       TEXT NOT NULL DEFAULT (datetime('now')),
		FOREIGN KEY (colleague_id) REFERENCES colleagues(id),
		FOREIGN KEY (capability_id) REFERENCES capability_packages(id),
		UNIQUE(colleague_id, capability_id)
	);`,

	// 7: collaboration_tasks — point-to-point task delegation between colleagues
	`CREATE TABLE IF NOT EXISTS collaboration_tasks (
		id               TEXT PRIMARY KEY,
		title            TEXT NOT NULL,
		description      TEXT NOT NULL DEFAULT '',
		from_colleague_id TEXT NOT NULL,
		to_colleague_id  TEXT NOT NULL,
		to_role_code     TEXT NOT NULL DEFAULT '',
		status           TEXT NOT NULL DEFAULT 'pending',
		priority         INTEGER NOT NULL DEFAULT 0,
		result           TEXT NOT NULL DEFAULT '',
		workflow_step_instance_id TEXT NOT NULL DEFAULT '',
		created_at       TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at       TEXT NOT NULL DEFAULT (datetime('now')),
		FOREIGN KEY (from_colleague_id) REFERENCES colleagues(id),
		FOREIGN KEY (to_colleague_id) REFERENCES colleagues(id)
	);`,

	// 8: collaboration_task_events — audit trail for collaboration state changes
	`CREATE TABLE IF NOT EXISTS collaboration_task_events (
		id        TEXT PRIMARY KEY,
		task_id   TEXT NOT NULL,
		event     TEXT NOT NULL,
		actor_id  TEXT NOT NULL DEFAULT '',
		note      TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		FOREIGN KEY (task_id) REFERENCES collaboration_tasks(id)
	);`,

	// 9: workflow_definitions — reusable workflow templates
	`CREATE TABLE IF NOT EXISTS workflow_definitions (
		id          TEXT PRIMARY KEY,
		name        TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		trigger_type TEXT NOT NULL DEFAULT 'manual',
		status      TEXT NOT NULL DEFAULT 'draft',
		created_at  TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
	);`,

	// 10: workflow_step_definitions — ordered steps within a workflow template
	`CREATE TABLE IF NOT EXISTS workflow_step_definitions (
		id                TEXT PRIMARY KEY,
		workflow_id       TEXT NOT NULL,
		step_code         TEXT NOT NULL,
		step_name         TEXT NOT NULL,
		step_type         TEXT NOT NULL DEFAULT 'processing',
		assignee_mode     TEXT NOT NULL DEFAULT 'by_role',
		assignee_role_code TEXT NOT NULL DEFAULT '',
		assignee_colleague_id TEXT NOT NULL DEFAULT '',
		timeout_minutes   INTEGER NOT NULL DEFAULT 0,
		reject_rule       TEXT NOT NULL DEFAULT 'end_process',
		sort_order        INTEGER NOT NULL DEFAULT 0,
		FOREIGN KEY (workflow_id) REFERENCES workflow_definitions(id)
	);`,

	// 11: workflow_instances — running instances of a workflow
	`CREATE TABLE IF NOT EXISTS workflow_instances (
		id                TEXT PRIMARY KEY,
		definition_id     TEXT NOT NULL,
		title             TEXT NOT NULL DEFAULT '',
		initiator_id      TEXT NOT NULL DEFAULT '',
		current_step_id   TEXT NOT NULL DEFAULT '',
		status            TEXT NOT NULL DEFAULT 'running',
		input_data        TEXT NOT NULL DEFAULT '',
		created_at        TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at        TEXT NOT NULL DEFAULT (datetime('now')),
		FOREIGN KEY (definition_id) REFERENCES workflow_definitions(id)
	);`,

	// 12: workflow_step_instances — individual step execution records
	`CREATE TABLE IF NOT EXISTS workflow_step_instances (
		id                   TEXT PRIMARY KEY,
		instance_id          TEXT NOT NULL,
		step_definition_id   TEXT NOT NULL,
		assignee_colleague_id TEXT NOT NULL DEFAULT '',
		collaboration_task_id TEXT NOT NULL DEFAULT '',
		status               TEXT NOT NULL DEFAULT 'pending',
		result               TEXT NOT NULL DEFAULT '',
		sort_order           INTEGER NOT NULL DEFAULT 0,
		created_at           TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at           TEXT NOT NULL DEFAULT (datetime('now')),
		FOREIGN KEY (instance_id) REFERENCES workflow_instances(id),
		FOREIGN KEY (step_definition_id) REFERENCES workflow_step_definitions(id)
	);`,

	// 13: workflow_instance_events — audit trail for workflow state changes
	`CREATE TABLE IF NOT EXISTS workflow_instance_events (
		id          TEXT PRIMARY KEY,
		instance_id TEXT NOT NULL,
		step_id     TEXT NOT NULL DEFAULT '',
		event       TEXT NOT NULL,
		actor_id    TEXT NOT NULL DEFAULT '',
		note        TEXT NOT NULL DEFAULT '',
		created_at  TEXT NOT NULL DEFAULT (datetime('now')),
		FOREIGN KEY (instance_id) REFERENCES workflow_instances(id)
	);`,

	// 14: indexes for query performance
	`CREATE INDEX IF NOT EXISTS idx_colleagues_role_id ON colleagues(role_id);
	 CREATE INDEX IF NOT EXISTS idx_colleagues_status ON colleagues(status);
	 CREATE INDEX IF NOT EXISTS idx_shared_memories_level ON shared_memories(level);
	 CREATE INDEX IF NOT EXISTS idx_shared_memories_scope ON shared_memories(scope);
	 CREATE INDEX IF NOT EXISTS idx_shared_memories_status ON shared_memories(status);
	 CREATE INDEX IF NOT EXISTS idx_collab_tasks_to ON collaboration_tasks(to_colleague_id);
	 CREATE INDEX IF NOT EXISTS idx_collab_tasks_from ON collaboration_tasks(from_colleague_id);
	 CREATE INDEX IF NOT EXISTS idx_collab_tasks_status ON collaboration_tasks(status);
	 CREATE INDEX IF NOT EXISTS idx_collab_events_task ON collaboration_task_events(task_id);
	 CREATE INDEX IF NOT EXISTS idx_wf_step_defs_workflow ON workflow_step_definitions(workflow_id);
	 CREATE INDEX IF NOT EXISTS idx_wf_instances_def ON workflow_instances(definition_id);
	 CREATE INDEX IF NOT EXISTS idx_wf_instances_status ON workflow_instances(status);
	 CREATE INDEX IF NOT EXISTS idx_wf_step_inst_instance ON workflow_step_instances(instance_id);
	 CREATE INDEX IF NOT EXISTS idx_wf_events_instance ON workflow_instance_events(instance_id);
	 CREATE INDEX IF NOT EXISTS idx_role_assign_log_colleague ON role_assignment_log(colleague_id);
	 CREATE INDEX IF NOT EXISTS idx_cap_bindings_colleague ON colleague_capability_bindings(colleague_id);
	 CREATE INDEX IF NOT EXISTS idx_cap_bindings_capability ON colleague_capability_bindings(capability_id);`,

	// 15: proxy_audit_log — records every LLM proxy request for audit
	`CREATE TABLE IF NOT EXISTS proxy_audit_log (
		id           TEXT PRIMARY KEY,
		request_id   TEXT NOT NULL DEFAULT '',
		provider_id  TEXT NOT NULL DEFAULT '',
		model        TEXT NOT NULL DEFAULT '',
		work_type    TEXT NOT NULL DEFAULT '',
		cost_tier    TEXT NOT NULL DEFAULT '',
		status       TEXT NOT NULL DEFAULT 'ok',
		latency_ms   INTEGER NOT NULL DEFAULT 0,
		input_tokens INTEGER NOT NULL DEFAULT 0,
		summary      TEXT NOT NULL DEFAULT '',
		error_msg    TEXT NOT NULL DEFAULT '',
		created_at   TEXT NOT NULL DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_proxy_audit_created ON proxy_audit_log(created_at);
	CREATE INDEX IF NOT EXISTS idx_proxy_audit_provider ON proxy_audit_log(provider_id);`,

	// 16: security_policies — configurable security rules
	`CREATE TABLE IF NOT EXISTS security_policies (
		id          TEXT PRIMARY KEY,
		name        TEXT NOT NULL,
		policy_type TEXT NOT NULL DEFAULT 'keyword_block',
		description TEXT NOT NULL DEFAULT '',
		rules       TEXT NOT NULL DEFAULT '{}',
		scope       TEXT NOT NULL DEFAULT 'all',
		priority    INTEGER NOT NULL DEFAULT 0,
		status      TEXT NOT NULL DEFAULT 'active',
		created_at  TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_security_policies_status ON security_policies(status);
	CREATE INDEX IF NOT EXISTS idx_security_policies_type ON security_policies(policy_type);`,

	// 17: security_policy_hit_records — audit trail for policy triggers
	`CREATE TABLE IF NOT EXISTS security_policy_hit_records (
		id          TEXT PRIMARY KEY,
		policy_id   TEXT NOT NULL,
		policy_name TEXT NOT NULL DEFAULT '',
		actor_id    TEXT NOT NULL DEFAULT '',
		action      TEXT NOT NULL DEFAULT 'blocked',
		detail      TEXT NOT NULL DEFAULT '',
		created_at  TEXT NOT NULL DEFAULT (datetime('now')),
		FOREIGN KEY (policy_id) REFERENCES security_policies(id)
	);
	CREATE INDEX IF NOT EXISTS idx_security_hits_policy ON security_policy_hit_records(policy_id);
	CREATE INDEX IF NOT EXISTS idx_security_hits_created ON security_policy_hit_records(created_at);`,

	// 18: config_bundles — configuration packages for delivery to DiWorker clients
	`CREATE TABLE IF NOT EXISTS config_bundles (
		id           TEXT PRIMARY KEY,
		version      INTEGER NOT NULL DEFAULT 1,
		content_type TEXT NOT NULL DEFAULT 'full',
		payload      TEXT NOT NULL DEFAULT '{}',
		status       TEXT NOT NULL DEFAULT 'draft',
		note         TEXT NOT NULL DEFAULT '',
		created_at   TEXT NOT NULL DEFAULT (datetime('now')),
		published_at TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS idx_config_bundles_status ON config_bundles(status);`,

	// 19: model_endpoints — DB-managed model provider endpoints
	`CREATE TABLE IF NOT EXISTS model_endpoints (
		id         TEXT PRIMARY KEY,
		name       TEXT NOT NULL,
		protocol   TEXT NOT NULL DEFAULT 'openai',
		base_url   TEXT NOT NULL DEFAULT '',
		api_key    TEXT NOT NULL DEFAULT '',
		model      TEXT NOT NULL DEFAULT '',
		cost_tier  TEXT NOT NULL DEFAULT 'medium',
		priority   INTEGER NOT NULL DEFAULT 0,
		features   TEXT NOT NULL DEFAULT '[]',
		status     TEXT NOT NULL DEFAULT 'active',
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_model_endpoints_status ON model_endpoints(status);
	CREATE INDEX IF NOT EXISTS idx_model_endpoints_tier ON model_endpoints(cost_tier);`,

	// 20: model_routing_policies — DB-managed routing rules
	`CREATE TABLE IF NOT EXISTS model_routing_policies (
		id            TEXT PRIMARY KEY,
		name          TEXT NOT NULL,
		description   TEXT NOT NULL DEFAULT '',
		work_type     TEXT NOT NULL DEFAULT '*',
		role_code     TEXT NOT NULL DEFAULT '*',
		endpoint_id   TEXT NOT NULL DEFAULT '',
		fallback_mode TEXT NOT NULL DEFAULT 'next_priority',
		priority      INTEGER NOT NULL DEFAULT 0,
		status        TEXT NOT NULL DEFAULT 'active',
		created_at    TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_model_routing_status ON model_routing_policies(status);`,

	// 21: admin_users — admin accounts for iWorkerCenter login
	`CREATE TABLE IF NOT EXISTS admin_users (
		id            TEXT PRIMARY KEY,
		username      TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		salt          TEXT NOT NULL DEFAULT '',
		email         TEXT NOT NULL DEFAULT '',
		created_at    TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
	);`,

	// 22: security_groups — hierarchical user groups for centralized security management
	`CREATE TABLE IF NOT EXISTS security_groups (
		id         TEXT PRIMARY KEY,
		name       TEXT NOT NULL,
		parent_id  TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_security_groups_parent ON security_groups(parent_id);`,

	// 23: security_group_members — user-to-group assignment (single group per user)
	`CREATE TABLE IF NOT EXISTS security_group_members (
		email      TEXT PRIMARY KEY,
		group_id   TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_sgm_group ON security_group_members(group_id);`,

	// 24: security_group_policies — sparse policy overrides per group
	`CREATE TABLE IF NOT EXISTS security_group_policies (
		group_id    TEXT PRIMARY KEY,
		policy_json TEXT NOT NULL DEFAULT '{}',
		updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
	);`,

	// 25: diworker_accounts — local authentication accounts for DiWorker
	`CREATE TABLE IF NOT EXISTS diworker_accounts (
		id            TEXT PRIMARY KEY,
		username      TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		salt          TEXT NOT NULL DEFAULT '',
		identifier    TEXT NOT NULL DEFAULT '',
		expires_at    TEXT,
		disabled      INTEGER NOT NULL DEFAULT 0,
		created_at    TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_diworker_accounts_username ON diworker_accounts(username);`,

	// 26: system_settings — generic key-value store for module configs (LDAP, etc.)
	`CREATE TABLE IF NOT EXISTS system_settings (
		key        TEXT PRIMARY KEY,
		value_json TEXT NOT NULL DEFAULT '{}',
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	);`,

	// 27: tenants — multi-tenancy: one row per company
	`CREATE TABLE IF NOT EXISTS tenants (
		id               TEXT PRIMARY KEY,
		company_name     TEXT NOT NULL UNIQUE,
		legal_person     TEXT NOT NULL DEFAULT '',
		email            TEXT NOT NULL,
		address          TEXT NOT NULL DEFAULT '',
		status           TEXT NOT NULL DEFAULT 'active',
		cloud_center_id  TEXT NOT NULL DEFAULT '',
		cloud_secret     TEXT NOT NULL DEFAULT '',
		created_at       TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at       TEXT NOT NULL DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_tenants_status ON tenants(status);`,

	// 28: add tenant_id column to all existing business tables
	`ALTER TABLE roles ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE colleagues ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE role_assignment_log ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE shared_memories ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE capability_packages ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE colleague_capability_bindings ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE collaboration_tasks ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE collaboration_task_events ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE workflow_definitions ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE workflow_step_definitions ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE workflow_instances ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE workflow_step_instances ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE workflow_instance_events ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE proxy_audit_log ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE security_policies ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE security_policy_hit_records ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE config_bundles ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE model_endpoints ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE model_routing_policies ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE admin_users ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE security_groups ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE security_group_members ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE security_group_policies ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE diworker_accounts ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';`,

	// 29: indexes on tenant_id for query performance
	`CREATE INDEX IF NOT EXISTS idx_roles_tenant ON roles(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_colleagues_tenant ON colleagues(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_shared_memories_tenant ON shared_memories(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_capability_packages_tenant ON capability_packages(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_collab_tasks_tenant ON collaboration_tasks(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_workflow_defs_tenant ON workflow_definitions(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_workflow_instances_tenant ON workflow_instances(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_security_policies_tenant ON security_policies(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_model_endpoints_tenant ON model_endpoints(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_admin_users_tenant ON admin_users(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_security_groups_tenant ON security_groups(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_diworker_accounts_tenant ON diworker_accounts(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_config_bundles_tenant ON config_bundles(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_proxy_audit_tenant ON proxy_audit_log(tenant_id);`,

	// 30: provision_nonces — replay protection for cloud provision requests
	`CREATE TABLE IF NOT EXISTS provision_nonces (
		nonce      TEXT PRIMARY KEY,
		expires_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_nonces_expires ON provision_nonces(expires_at);`,

	// 31: a2a_sessions - persisted iWorker-to-iWorker deliberation sessions
	`CREATE TABLE IF NOT EXISTS a2a_sessions (
		tenant_id          TEXT NOT NULL,
		id                 TEXT NOT NULL,
		org_unit_id        TEXT NOT NULL DEFAULT '',
		topic              TEXT NOT NULL,
		status             TEXT NOT NULL DEFAULT 'open',
		decision_policy    TEXT NOT NULL DEFAULT 'majority',
		participant_count  INTEGER NOT NULL DEFAULT 0,
		message_count      INTEGER NOT NULL DEFAULT 0,
		proposal_count     INTEGER NOT NULL DEFAULT 0,
		review_count       INTEGER NOT NULL DEFAULT 0,
		payload_json       TEXT NOT NULL DEFAULT '{}',
		created_at         TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at         TEXT NOT NULL DEFAULT (datetime('now')),
		PRIMARY KEY (tenant_id, id)
	);
	CREATE INDEX IF NOT EXISTS idx_a2a_sessions_tenant_status ON a2a_sessions(tenant_id, status);
	CREATE INDEX IF NOT EXISTS idx_a2a_sessions_tenant_org_unit ON a2a_sessions(tenant_id, org_unit_id);
	CREATE INDEX IF NOT EXISTS idx_a2a_sessions_tenant_updated ON a2a_sessions(tenant_id, updated_at);`,
}

// Migrate applies all pending migrations inside a transaction.
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS _migrations (
		seq     INTEGER PRIMARY KEY,
		applied TEXT NOT NULL DEFAULT (datetime('now'))
	);`); err != nil {
		return fmt.Errorf("create _migrations table: %w", err)
	}

	var applied int
	if err := db.QueryRow("SELECT COALESCE(MAX(seq), 0) FROM _migrations").Scan(&applied); err != nil {
		return fmt.Errorf("read migration seq: %w", err)
	}

	pending := migrations[applied:]
	if len(pending) == 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin migration tx: %w", err)
	}

	for i, ddl := range pending {
		seq := applied + i + 1
		if _, err := tx.Exec(ddl); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d: %w", seq, err)
		}
		if _, err := tx.Exec("INSERT INTO _migrations (seq) VALUES (?)", seq); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", seq, err)
		}
		log.Printf("[iWorkerCenter] applied migration %d", seq)
	}

	return tx.Commit()
}
