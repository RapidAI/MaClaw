package guiapp

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

const taskPlanTablesSQLiteSchema = `
CREATE TABLE IF NOT EXISTS task_plans (
    id TEXT PRIMARY KEY,
    signature TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    runtime_status TEXT NOT NULL DEFAULT '',
    created_at_unix_nano INTEGER NOT NULL DEFAULT 0,
    updated_at_unix_nano INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS task_plan_subtasks (
    plan_id TEXT NOT NULL,
    position INTEGER NOT NULL,
    id TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    tool TEXT NOT NULL DEFAULT '',
    session_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    runtime_status TEXT NOT NULL DEFAULT '',
    result TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(plan_id, position),
    FOREIGN KEY(plan_id) REFERENCES task_plans(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_task_plan_subtasks_session ON task_plan_subtasks(session_id);
CREATE TABLE IF NOT EXISTS task_plan_dependencies (
    plan_id TEXT NOT NULL,
    subtask_position INTEGER NOT NULL,
    position INTEGER NOT NULL,
    depends_on_id TEXT NOT NULL,
    PRIMARY KEY(plan_id, subtask_position, position),
    FOREIGN KEY(plan_id) REFERENCES task_plans(id) ON DELETE CASCADE
);
`

func configureTaskPlanSchema(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS task_plans_meta (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`); err != nil {
		return err
	}
	columns, err := taskPlanTableColumns(db)
	if err != nil {
		return err
	}
	if columns["payload_json"] {
		if err := migrateTaskPlanPayloadRows(db); err != nil {
			return err
		}
	}
	if _, err := db.Exec(taskPlanTablesSQLiteSchema); err != nil {
		return err
	}
	return ensureTaskPlanSignatures(db)
}

func taskPlanTableColumns(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(`PRAGMA table_info(task_plans)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

func migrateTaskPlanPayloadRows(db *sql.DB) error {
	rows, err := db.Query(`SELECT id, payload_json, updated_at FROM task_plans`)
	if err != nil {
		return fmt.Errorf("task_orchestrator: read payload rows: %w", err)
	}
	type payloadPlan struct {
		plan      TaskPlan
		updatedAt int64
	}
	plans := make([]payloadPlan, 0)
	for rows.Next() {
		var id, payload string
		var updatedAt int64
		if err := rows.Scan(&id, &payload, &updatedAt); err != nil {
			_ = rows.Close()
			return err
		}
		var plan TaskPlan
		if err := json.Unmarshal([]byte(payload), &plan); err != nil {
			_ = rows.Close()
			return fmt.Errorf("task_orchestrator: decode payload plan %s: %w", id, err)
		}
		if plan.ID == "" {
			plan.ID = id
		}
		plans = append(plans, payloadPlan{plan: plan, updatedAt: updatedAt})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`ALTER TABLE task_plans RENAME TO task_plans_payload_legacy`); err != nil {
		return err
	}
	if _, err := tx.Exec(taskPlanTablesSQLiteSchema); err != nil {
		return err
	}
	for _, item := range plans {
		if err := writeTaskPlanTx(tx, &item.plan, item.updatedAt); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DROP TABLE task_plans_payload_legacy`); err != nil {
		return err
	}
	return tx.Commit()
}

func taskPlanSignature(plan *TaskPlan) (string, error) {
	raw, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

func ensureTaskPlanSignatures(db *sql.DB) error {
	var hasSignature int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('task_plans') WHERE name='signature'`).Scan(&hasSignature); err != nil {
		return err
	}
	if hasSignature > 0 {
		return nil
	}
	plans, err := loadTaskPlanRows(db)
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`ALTER TABLE task_plans ADD COLUMN signature TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	for id, plan := range plans {
		signature, err := taskPlanSignature(plan)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE task_plans SET signature=? WHERE id=?`, signature, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func writeTaskPlanTx(tx *sql.Tx, plan *TaskPlan, updatedAt int64) error {
	if plan == nil || plan.ID == "" {
		return fmt.Errorf("task_orchestrator: plan id is required")
	}
	signature, err := taskPlanSignature(plan)
	if err != nil {
		return err
	}
	var createdAt int64
	if !plan.CreatedAt.IsZero() {
		createdAt = plan.CreatedAt.UnixNano()
	}
	if _, err := tx.Exec(`INSERT INTO task_plans(id, signature, description, status, runtime_status, created_at_unix_nano, updated_at_unix_nano)
		VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET signature=excluded.signature, description=excluded.description,
		status=excluded.status, runtime_status=excluded.runtime_status,
		created_at_unix_nano=excluded.created_at_unix_nano, updated_at_unix_nano=excluded.updated_at_unix_nano`,
		plan.ID, signature, plan.Description, plan.Status, plan.RuntimeStatus, createdAt, updatedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM task_plan_dependencies WHERE plan_id=?`, plan.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM task_plan_subtasks WHERE plan_id=?`, plan.ID); err != nil {
		return err
	}
	for position, subtask := range plan.SubTasks {
		if _, err := tx.Exec(`INSERT INTO task_plan_subtasks(
			plan_id, position, id, description, tool, session_id, status, runtime_status, result)
			VALUES(?,?,?,?,?,?,?,?,?)`, plan.ID, position, subtask.ID, subtask.Description, subtask.Tool,
			subtask.SessionID, subtask.Status, subtask.RuntimeStatus, subtask.Result); err != nil {
			return err
		}
		for dependencyPosition, dependsOn := range subtask.DependsOn {
			if _, err := tx.Exec(`INSERT INTO task_plan_dependencies(plan_id, subtask_position, position, depends_on_id)
				VALUES(?,?,?,?)`, plan.ID, position, dependencyPosition, dependsOn); err != nil {
				return err
			}
		}
	}
	return nil
}

func loadTaskPlanRows(db *sql.DB) (map[string]*TaskPlan, error) {
	plans := map[string]*TaskPlan{}
	rows, err := db.Query(`SELECT id, description, status, runtime_status, created_at_unix_nano FROM task_plans ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("task_orchestrator: query plans: %w", err)
	}
	for rows.Next() {
		var plan TaskPlan
		var createdAt int64
		if err := rows.Scan(&plan.ID, &plan.Description, &plan.Status, &plan.RuntimeStatus, &createdAt); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if createdAt != 0 {
			plan.CreatedAt = time.Unix(0, createdAt)
		}
		plans[plan.ID] = &plan
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	subtaskRows, err := db.Query(`SELECT plan_id, position, id, description, tool, session_id, status, runtime_status, result
		FROM task_plan_subtasks ORDER BY plan_id, position`)
	if err != nil {
		return nil, fmt.Errorf("task_orchestrator: query subtasks: %w", err)
	}
	type subtaskKey struct {
		planID   string
		position int
	}
	positions := map[subtaskKey]int{}
	for subtaskRows.Next() {
		var planID string
		var position int
		var subtask PlanSubTask
		if err := subtaskRows.Scan(&planID, &position, &subtask.ID, &subtask.Description, &subtask.Tool,
			&subtask.SessionID, &subtask.Status, &subtask.RuntimeStatus, &subtask.Result); err != nil {
			_ = subtaskRows.Close()
			return nil, err
		}
		if plan := plans[planID]; plan != nil {
			plan.SubTasks = append(plan.SubTasks, subtask)
			positions[subtaskKey{planID: planID, position: position}] = len(plan.SubTasks) - 1
		}
	}
	if err := subtaskRows.Err(); err != nil {
		_ = subtaskRows.Close()
		return nil, err
	}
	_ = subtaskRows.Close()
	dependencyRows, err := db.Query(`SELECT plan_id, subtask_position, depends_on_id
		FROM task_plan_dependencies ORDER BY plan_id, subtask_position, position`)
	if err != nil {
		return nil, fmt.Errorf("task_orchestrator: query dependencies: %w", err)
	}
	defer dependencyRows.Close()
	for dependencyRows.Next() {
		var planID, dependsOn string
		var subtaskPosition int
		if err := dependencyRows.Scan(&planID, &subtaskPosition, &dependsOn); err != nil {
			return nil, err
		}
		plan := plans[planID]
		index, ok := positions[subtaskKey{planID: planID, position: subtaskPosition}]
		if plan != nil && ok && index < len(plan.SubTasks) {
			plan.SubTasks[index].DependsOn = append(plan.SubTasks[index].DependsOn, dependsOn)
		}
	}
	if err := dependencyRows.Err(); err != nil {
		return nil, err
	}
	return plans, nil
}
