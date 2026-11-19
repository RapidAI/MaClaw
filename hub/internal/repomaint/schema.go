package repomaint

import "database/sql"

// SchemaStmts creates the repo-maint tables. Each statement uses IF NOT EXISTS
// so EnsureSchema and the hub migration list can both run them.
var SchemaStmts = []string{
	`CREATE TABLE IF NOT EXISTS github_repo_maint_rules (
    rule_id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    machine_id TEXT NOT NULL,
    owner TEXT NOT NULL,
    repo TEXT NOT NULL,
    hook_token TEXT NOT NULL UNIQUE,
    hook_secret TEXT NOT NULL,
    github_hook_id INTEGER NOT NULL DEFAULT 0,
    enabled INTEGER NOT NULL DEFAULT 0,
    push_branch TEXT NOT NULL DEFAULT '',
    event_kinds TEXT NOT NULL DEFAULT '[]',
    updated_at TEXT NOT NULL
)`,
	`CREATE INDEX IF NOT EXISTS github_repo_maint_rules_machine
    ON github_repo_maint_rules(machine_id, enabled)`,
	`CREATE TABLE IF NOT EXISTS github_repo_maint_deliveries (
    id TEXT PRIMARY KEY,
    rule_id TEXT NOT NULL,
    machine_id TEXT NOT NULL,
    github_delivery TEXT NOT NULL UNIQUE,
    event_kind TEXT NOT NULL,
    object_id TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
)`,
	`CREATE INDEX IF NOT EXISTS github_repo_maint_deliveries_machine
    ON github_repo_maint_deliveries(machine_id, status, created_at)`,
}

// EnsureSchema creates the repo-maint tables on an already-open Hub database.
func EnsureSchema(db *sql.DB) error {
	if db == nil {
		return errNoDB
	}
	for _, stmt := range SchemaStmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
