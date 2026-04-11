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
