package scheduler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

const scheduledTaskSchema = `
CREATE TABLE IF NOT EXISTS scheduled_tasks (
  id TEXT PRIMARY KEY,
  position INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL DEFAULT '',
  bot_profile_id TEXT NOT NULL DEFAULT '',
  instance_id TEXT NOT NULL DEFAULT '',
  owner_tenant_id TEXT NOT NULL DEFAULT '',
  owner_user_id TEXT NOT NULL DEFAULT '',
  desktop_bot_id TEXT NOT NULL DEFAULT '',
  desktop_bot_owner TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL DEFAULT '',
  hour INTEGER NOT NULL DEFAULT 0,
  minute INTEGER NOT NULL DEFAULT 0,
  day_of_week INTEGER NOT NULL DEFAULT -1,
  day_of_month INTEGER NOT NULL DEFAULT -1,
  interval_minutes INTEGER NOT NULL DEFAULT 0,
  start_date TEXT NOT NULL DEFAULT '',
  end_date TEXT NOT NULL DEFAULT '',
  task_type TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT '',
  last_run_at TEXT NOT NULL DEFAULT '',
  next_run_at TEXT NOT NULL DEFAULT '',
  run_count INTEGER NOT NULL DEFAULT 0,
  last_result TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT '',
  delivery_enabled INTEGER NOT NULL DEFAULT 0,
  delivery_channel TEXT NOT NULL DEFAULT '',
  delivery_bot_profile_id TEXT NOT NULL DEFAULT '',
  delivery_on TEXT NOT NULL DEFAULT '',
  delivery_prefix TEXT NOT NULL DEFAULT '',
  delivery_fail_on_error INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS scheduled_task_targets (
  task_id TEXT NOT NULL,
  position INTEGER NOT NULL,
  kind TEXT NOT NULL DEFAULT '',
  group_id TEXT NOT NULL DEFAULT '',
  group_name TEXT NOT NULL DEFAULT '',
  user_id TEXT NOT NULL DEFAULT '',
  mention_all INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (task_id, position)
);
CREATE TABLE IF NOT EXISTS scheduled_task_mentions (
  task_id TEXT NOT NULL,
  target_position INTEGER NOT NULL,
  position INTEGER NOT NULL,
  user_id TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (task_id, target_position, position)
);`

// EnsureScheduledTaskSchema creates the column store. A database that still
// keeps each task as task_json is rewritten in place. The pre-migration JSON
// file is not touched.
func EnsureScheduledTaskSchema(db *sql.DB) error {
	return ensureScheduledTaskSchema(db)
}

// LoadScheduledTaskRows reads tasks in saved order when orderByPosition is set.
func LoadScheduledTaskRows(db *sql.DB, orderByPosition bool) ([]ScheduledTask, error) {
	return loadScheduledTaskRows(db, orderByPosition)
}

// SaveScheduledTaskRows replaces the task tables.
func SaveScheduledTaskRows(db *sql.DB, tasks []ScheduledTask, orderByPosition bool) error {
	return saveScheduledTaskRows(db, tasks, orderByPosition)
}

// scheduledTaskSchemaReady remembers databases whose column store is already
// in place. A task fire loads and saves through the same *sql.DB, and the
// sqlite_master plus table_info probes are the slow part once the schema exists.
// A failed rebuild is not recorded, so the next open tries again. The map keeps
// closed test databases reachable; that leak stays inside the test process.
var (
	scheduledTaskSchemaMu    sync.Mutex
	scheduledTaskSchemaReady sync.Map
)

// ensureScheduledTaskSchema creates the column store.
func ensureScheduledTaskSchema(db *sql.DB) error {
	if _, ok := scheduledTaskSchemaReady.Load(db); ok {
		return nil
	}
	scheduledTaskSchemaMu.Lock()
	defer scheduledTaskSchemaMu.Unlock()
	if _, ok := scheduledTaskSchemaReady.Load(db); ok {
		return nil
	}
	if err := migrateScheduledTaskSchema(db); err != nil {
		return err
	}
	scheduledTaskSchemaReady.Store(db, struct{}{})
	return nil
}

func migrateScheduledTaskSchema(db *sql.DB) error {
	exists, err := sqliteTableExists(db, "scheduled_tasks")
	if err != nil {
		return err
	}
	if !exists {
		if _, err := db.Exec(scheduledTaskSchema); err != nil {
			return fmt.Errorf("scheduler: task schema: %w", err)
		}
	} else {
		cols, err := sqliteTableColumns(db, "scheduled_tasks")
		if err != nil {
			return err
		}
		if cols["task_json"] {
			if err := rebuildScheduledTasksFromJSON(db, cols["position"]); err != nil {
				return err
			}
		}
		if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS scheduled_task_targets (
  task_id TEXT NOT NULL,
  position INTEGER NOT NULL,
  kind TEXT NOT NULL DEFAULT '',
  group_id TEXT NOT NULL DEFAULT '',
  group_name TEXT NOT NULL DEFAULT '',
  user_id TEXT NOT NULL DEFAULT '',
  mention_all INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (task_id, position)
);
CREATE TABLE IF NOT EXISTS scheduled_task_mentions (
  task_id TEXT NOT NULL,
  target_position INTEGER NOT NULL,
  position INTEGER NOT NULL,
  user_id TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (task_id, target_position, position)
);`); err != nil {
			return fmt.Errorf("scheduler: task target schema: %w", err)
		}
	}
	// schedule_meta holds the one-time JSON import marker. An empty task table
	// is not that marker: the pre-migration file stays on disk.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schedule_meta (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("scheduler: task import marker: %w", err)
	}
	return nil
}

func rebuildScheduledTasksFromJSON(db *sql.DB, hasPosition bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	query := `SELECT id, task_json, updated_at FROM scheduled_tasks ORDER BY id`
	if hasPosition {
		query = `SELECT id, task_json, updated_at, position FROM scheduled_tasks ORDER BY position, id`
	}
	rows, err := tx.Query(query)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	type legacyRow struct {
		id       string
		raw      []byte
		updated  string
		position int
	}
	var legacy []legacyRow
	for rows.Next() {
		var row legacyRow
		if hasPosition {
			err = rows.Scan(&row.id, &row.raw, &row.updated, &row.position)
		} else {
			err = rows.Scan(&row.id, &row.raw, &row.updated)
		}
		if err != nil {
			_ = rows.Close()
			_ = tx.Rollback()
			return err
		}
		legacy = append(legacy, row)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		_ = tx.Rollback()
		return err
	}
	_ = rows.Close()
	tasks := make([]ScheduledTask, 0, len(legacy))
	updated := make([]string, 0, len(legacy))
	for i, row := range legacy {
		var task ScheduledTask
		if err := json.Unmarshal(row.raw, &task); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("scheduler: decode stored task %s: %w", row.id, err)
		}
		if task.ID == "" {
			task.ID = row.id
		}
		if task.ID == "" {
			task.ID = fmt.Sprintf("~%d", i)
		}
		tasks = append(tasks, task)
		updated = append(updated, row.updated)
	}
	if _, err := tx.Exec(`ALTER TABLE scheduled_tasks RENAME TO scheduled_tasks_json_old`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(scheduledTaskSchema); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := insertScheduledTaskRows(tx, tasks, updated); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DROP TABLE scheduled_tasks_json_old`); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// loadScheduledTaskRows reads tasks in saved order when orderByPosition is set.
// Otherwise the service database keeps its historical id order.
func loadScheduledTaskRows(db *sql.DB, orderByPosition bool) ([]ScheduledTask, error) {
	if err := ensureScheduledTaskSchema(db); err != nil {
		return nil, err
	}
	order := `id`
	if orderByPosition {
		order = `position, id`
	}
	rows, err := db.Query(`SELECT id, position, bot_profile_id, instance_id, owner_tenant_id, owner_user_id,
		desktop_bot_id, desktop_bot_owner, name, action, hour, minute, day_of_week, day_of_month,
		interval_minutes, start_date, end_date, task_type, status, created_at, last_run_at, next_run_at,
		run_count, last_result, last_error, delivery_enabled, delivery_channel, delivery_bot_profile_id,
		delivery_on, delivery_prefix, delivery_fail_on_error
		FROM scheduled_tasks ORDER BY ` + order)
	if err != nil {
		return nil, err
	}
	out := []ScheduledTask{}
	for rows.Next() {
		task, err := scanScheduledTask(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, task)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	if err := attachScheduledTaskTargets(db, out); err != nil {
		return nil, err
	}
	return out, nil
}

func scanScheduledTask(rows *sql.Rows) (ScheduledTask, error) {
	var task ScheduledTask
	var position int
	var created, lastRun, nextRun string
	var enabled, failOnError int
	var channel, bot, on, prefix string
	err := rows.Scan(
		&task.ID, &position, &task.BotProfileID, &task.InstanceID, &task.OwnerTenantID, &task.OwnerUserID,
		&task.DesktopBotID, &task.DesktopBotOwner, &task.Name, &task.Action, &task.Hour, &task.Minute, &task.DayOfWeek, &task.DayOfMonth,
		&task.IntervalMinutes, &task.StartDate, &task.EndDate, &task.TaskType, &task.Status, &created, &lastRun, &nextRun,
		&task.RunCount, &task.LastResult, &task.LastError, &enabled, &channel, &bot, &on, &prefix, &failOnError,
	)
	if err != nil {
		return ScheduledTask{}, err
	}
	task.CreatedAt = parseTaskTime(created)
	task.LastRunAt = parseTaskTimePtr(lastRun)
	task.NextRunAt = parseTaskTimePtr(nextRun)
	if enabled != 0 || channel != "" || bot != "" || on != "" || prefix != "" || failOnError != 0 {
		task.Delivery = &TaskDelivery{
			Enabled:      enabled != 0,
			Channel:      channel,
			BotProfileID: bot,
			On:           on,
			Prefix:       prefix,
			FailOnError:  failOnError != 0,
		}
	}
	return task, nil
}

func attachScheduledTaskTargets(db *sql.DB, tasks []ScheduledTask) error {
	if len(tasks) == 0 {
		return nil
	}
	index := make(map[string]int, len(tasks))
	for i := range tasks {
		index[tasks[i].ID] = i
	}
	rows, err := db.Query(`SELECT task_id, position, kind, group_id, group_name, user_id, mention_all FROM scheduled_task_targets ORDER BY task_id, position`)
	if err != nil {
		return err
	}
	type pendingTarget struct {
		taskID   string
		position int
		target   DeliveryTarget
	}
	var targets []pendingTarget
	for rows.Next() {
		var item pendingTarget
		var mentionAll int
		if err := rows.Scan(&item.taskID, &item.position, &item.target.Kind, &item.target.GroupID, &item.target.GroupName, &item.target.UserID, &mentionAll); err != nil {
			_ = rows.Close()
			return err
		}
		item.target.MentionAll = mentionAll != 0
		targets = append(targets, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	mentions, err := db.Query(`SELECT task_id, target_position, user_id FROM scheduled_task_mentions ORDER BY task_id, target_position, position`)
	if err != nil {
		return err
	}
	defer mentions.Close()
	mentionByTarget := map[string][]string{}
	for mentions.Next() {
		var taskID, userID string
		var targetPos int
		if err := mentions.Scan(&taskID, &targetPos, &userID); err != nil {
			return err
		}
		key := fmt.Sprintf("%s\x00%d", taskID, targetPos)
		mentionByTarget[key] = append(mentionByTarget[key], userID)
	}
	if err := mentions.Err(); err != nil {
		return err
	}
	for _, item := range targets {
		i, ok := index[item.taskID]
		if !ok {
			continue
		}
		if tasks[i].Delivery == nil {
			tasks[i].Delivery = &TaskDelivery{}
		}
		item.target.MentionUserIDs = mentionByTarget[fmt.Sprintf("%s\x00%d", item.taskID, item.position)]
		tasks[i].Delivery.Targets = append(tasks[i].Delivery.Targets, item.target)
	}
	return nil
}

// saveScheduledTaskRows replaces the task tables. skipEmptyID matches the
// service database, which never stored a row without an id. orderByPosition
// stores the slice index so the desktop scheduler reloads in that order.
func saveScheduledTaskRows(db *sql.DB, tasks []ScheduledTask, orderByPosition bool) error {
	if err := ensureScheduledTaskSchema(db); err != nil {
		return err
	}
	stored := make([]ScheduledTask, 0, len(tasks))
	for i, task := range tasks {
		if strings.TrimSpace(task.ID) == "" {
			if !orderByPosition {
				continue
			}
			task.ID = fmt.Sprintf("~%d", i)
		}
		stored = append(stored, task)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM scheduled_task_mentions`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM scheduled_task_targets`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM scheduled_tasks`); err != nil {
		_ = tx.Rollback()
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	updated := make([]string, len(stored))
	for i := range updated {
		updated[i] = now
	}
	if err := insertScheduledTaskRows(tx, stored, updated); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// dedupeScheduledTasks keeps the first slot for an id and replaces it with the
// later record. A repeated id would abort the insert, and a failed rebuild
// would leave task_json in place so every later load failed the same way.
func dedupeScheduledTasks(tasks []ScheduledTask, updated []string) ([]ScheduledTask, []string) {
	pos := make(map[string]int, len(tasks))
	out := make([]ScheduledTask, 0, len(tasks))
	stamps := make([]string, 0, len(tasks))
	for i, task := range tasks {
		stamp := ""
		if i < len(updated) {
			stamp = updated[i]
		}
		if at, ok := pos[task.ID]; ok {
			out[at] = task
			stamps[at] = stamp
			continue
		}
		pos[task.ID] = len(out)
		out = append(out, task)
		stamps = append(stamps, stamp)
	}
	return out, stamps
}

func insertScheduledTaskRows(tx *sql.Tx, tasks []ScheduledTask, updated []string) error {
	tasks, updated = dedupeScheduledTasks(tasks, updated)
	taskStmt, err := tx.Prepare(`INSERT INTO scheduled_tasks(
			id, position, updated_at, bot_profile_id, instance_id, owner_tenant_id, owner_user_id,
			desktop_bot_id, desktop_bot_owner, name, action, hour, minute, day_of_week, day_of_month,
			interval_minutes, start_date, end_date, task_type, status, created_at, last_run_at, next_run_at,
			run_count, last_result, last_error, delivery_enabled, delivery_channel, delivery_bot_profile_id,
			delivery_on, delivery_prefix, delivery_fail_on_error
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer taskStmt.Close()
	targetStmt, err := tx.Prepare(`INSERT INTO scheduled_task_targets(task_id, position, kind, group_id, group_name, user_id, mention_all) VALUES(?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer targetStmt.Close()
	mentionStmt, err := tx.Prepare(`INSERT INTO scheduled_task_mentions(task_id, target_position, position, user_id) VALUES(?,?,?,?)`)
	if err != nil {
		return err
	}
	defer mentionStmt.Close()
	for i, task := range tasks {
		delivery := task.Delivery
		enabled, fail := 0, 0
		channel, bot, on, prefix := "", "", "", ""
		if delivery != nil {
			if delivery.Enabled {
				enabled = 1
			}
			if delivery.FailOnError {
				fail = 1
			}
			channel = delivery.Channel
			bot = delivery.BotProfileID
			on = delivery.On
			prefix = delivery.Prefix
		}
		stamp := ""
		if i < len(updated) {
			stamp = updated[i]
		}
		if _, err := taskStmt.Exec(
			task.ID, i, stamp, task.BotProfileID, task.InstanceID, task.OwnerTenantID, task.OwnerUserID,
			task.DesktopBotID, task.DesktopBotOwner, task.Name, task.Action, task.Hour, task.Minute, task.DayOfWeek, task.DayOfMonth,
			task.IntervalMinutes, task.StartDate, task.EndDate, task.TaskType, task.Status, formatTaskTime(task.CreatedAt), formatTaskTimePtr(task.LastRunAt), formatTaskTimePtr(task.NextRunAt),
			task.RunCount, task.LastResult, task.LastError, enabled, channel, bot, on, prefix, fail,
		); err != nil {
			return err
		}
		if delivery == nil {
			continue
		}
		for n, target := range delivery.Targets {
			mention := 0
			if target.MentionAll {
				mention = 1
			}
			if _, err := targetStmt.Exec(task.ID, n, target.Kind, target.GroupID, target.GroupName, target.UserID, mention); err != nil {
				return err
			}
			for m, userID := range target.MentionUserIDs {
				if _, err := mentionStmt.Exec(task.ID, n, m, userID); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func formatTaskTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func formatTaskTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return formatTaskTime(*t)
}

func parseTaskTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func parseTaskTimePtr(value string) *time.Time {
	parsed := parseTaskTime(value)
	if parsed.IsZero() {
		return nil
	}
	return &parsed
}

func sqliteTableExists(db *sql.DB, name string) (bool, error) {
	var found string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&found)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func sqliteTableColumns(db *sql.DB, name string) (map[string]bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + name + `)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var colName, colType string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &colName, &colType, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols[colName] = true
	}
	return cols, rows.Err()
}
