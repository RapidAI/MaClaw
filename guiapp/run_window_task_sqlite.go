package guiapp

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

type runWindowStore struct {
	db *sql.DB
}

func runWindowDBPath(jsonPath string) string {
	ext := filepath.Ext(jsonPath)
	if strings.EqualFold(ext, ".json") {
		return strings.TrimSuffix(jsonPath, ext) + ".db"
	}
	return jsonPath + ".db"
}

func openRunWindowStore(jsonPath string) (*runWindowStore, error) {
	dbPath := runWindowDBPath(jsonPath)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if testing.Testing() {
		db.SetMaxIdleConns(0)
	} else {
		db.SetMaxIdleConns(1)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS run_window_tasks (
    id TEXT PRIMARY KEY,
    signature TEXT NOT NULL DEFAULT '',
    params_signature TEXT NOT NULL DEFAULT '',
    position INTEGER NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    prompt TEXT NOT NULL DEFAULT '',
    initial_message TEXT NOT NULL DEFAULT '',
    task_type TEXT NOT NULL DEFAULT '',
    expert_id TEXT NOT NULL DEFAULT '',
    expert_name TEXT NOT NULL DEFAULT '',
    workflow_template_id TEXT NOT NULL DEFAULT '',
    workspace_kind TEXT NOT NULL DEFAULT '',
    local_path TEXT NOT NULL DEFAULT '',
    cloud_workspace_id TEXT NOT NULL DEFAULT '',
    cloud_name TEXT NOT NULL DEFAULT '',
    remote_host TEXT NOT NULL DEFAULT '',
    remote_port INTEGER NOT NULL DEFAULT 0,
    remote_user TEXT NOT NULL DEFAULT '',
    remote_work_dir TEXT NOT NULL DEFAULT '',
    latex_template_id TEXT NOT NULL DEFAULT '',
    latex_template_name TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    start_at_unix_nano INTEGER NOT NULL DEFAULT 0,
    end_at_unix_nano INTEGER,
    project_path TEXT NOT NULL DEFAULT '',
    session_user_id TEXT NOT NULL DEFAULT '',
    claimed_at_unix_nano INTEGER,
    started_at_unix_nano INTEGER,
    created_at_unix_nano INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_run_window_tasks_status_start ON run_window_tasks(status, start_at_unix_nano);
CREATE TABLE IF NOT EXISTS run_window_workflow_params (
    task_id TEXT NOT NULL,
    key TEXT NOT NULL,
    value TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(task_id, key),
    FOREIGN KEY(task_id) REFERENCES run_window_tasks(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS run_window_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureRunWindowTaskSignatures(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := importLegacyRunWindowTasks(db, jsonPath); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &runWindowStore{db: db}, nil
}

func runWindowTaskSignature(task RunWindowTask) (string, error) {
	raw, err := json.Marshal(task)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

func runWindowParamsSignature(params map[string]string) (string, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

func ensureRunWindowTaskSignatures(db *sql.DB) error {
	var hasSignature, hasParamsSignature int
	if err := db.QueryRow(`SELECT
		COUNT(CASE WHEN name='signature' THEN 1 END),
		COUNT(CASE WHEN name='params_signature' THEN 1 END)
		FROM pragma_table_info('run_window_tasks')`).Scan(&hasSignature, &hasParamsSignature); err != nil {
		return err
	}
	if hasSignature > 0 && hasParamsSignature > 0 {
		return nil
	}
	tasks, err := loadRunWindowTasks(db)
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if hasSignature == 0 {
		if _, err := tx.Exec(`ALTER TABLE run_window_tasks ADD COLUMN signature TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if hasParamsSignature == 0 {
		if _, err := tx.Exec(`ALTER TABLE run_window_tasks ADD COLUMN params_signature TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	for _, task := range tasks {
		signature, err := runWindowTaskSignature(task)
		if err != nil {
			return err
		}
		paramsSignature, err := runWindowParamsSignature(task.Draft.WorkflowParams)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE run_window_tasks SET signature=?, params_signature=? WHERE id=?`, signature, paramsSignature, task.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func importLegacyRunWindowTasks(db *sql.DB, jsonPath string) error {
	var imported int
	if err := db.QueryRow(`SELECT COUNT(*) FROM run_window_meta WHERE key='legacy_json_imported'`).Scan(&imported); err != nil {
		return err
	}
	if imported > 0 {
		return nil
	}
	tasks := []RunWindowTask{}
	data, err := os.ReadFile(jsonPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &tasks); err != nil {
			return fmt.Errorf("读取运行时段任务失败: %w", err)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := writeRunWindowTasks(tx, tasks); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO run_window_meta(key, value) VALUES('legacy_json_imported', '1')`); err != nil {
		return err
	}
	return tx.Commit()
}

func loadRunWindowTasks(db *sql.DB) ([]RunWindowTask, error) {
	rows, err := db.Query(`SELECT id, name, prompt, initial_message, task_type, expert_id, expert_name,
		workflow_template_id, workspace_kind, local_path, cloud_workspace_id, cloud_name, remote_host,
		remote_port, remote_user, remote_work_dir, latex_template_id, latex_template_name, status,
		start_at_unix_nano, end_at_unix_nano, project_path, session_user_id, claimed_at_unix_nano,
		started_at_unix_nano, created_at_unix_nano, last_error
		FROM run_window_tasks ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	tasks := make([]RunWindowTask, 0)
	for rows.Next() {
		var task RunWindowTask
		var endAt, claimedAt, startedAt sql.NullInt64
		var startAt, createdAt int64
		if err := rows.Scan(&task.ID, &task.Name, &task.Prompt, &task.InitialMessage,
			&task.Draft.TaskType, &task.Draft.ExpertID, &task.Draft.ExpertName, &task.Draft.WorkflowTemplateID,
			&task.Draft.WorkspaceKind, &task.Draft.LocalPath, &task.Draft.CloudWorkspaceID, &task.Draft.CloudName,
			&task.Draft.RemoteHost, &task.Draft.RemotePort, &task.Draft.RemoteUser, &task.Draft.RemoteWorkDir,
			&task.Draft.LatexTemplateID, &task.Draft.LatexTemplateName, &task.Status, &startAt, &endAt,
			&task.ProjectPath, &task.SessionUserID, &claimedAt, &startedAt, &createdAt, &task.LastError); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if startAt != 0 {
			task.StartAt = time.Unix(0, startAt)
		}
		if createdAt != 0 {
			task.CreatedAt = time.Unix(0, createdAt)
		}
		task.EndAt = runWindowTimePointer(endAt)
		task.ClaimedAt = runWindowTimePointer(claimedAt)
		task.StartedAt = runWindowTimePointer(startedAt)
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	paramRows, err := db.Query(`SELECT task_id, key, value FROM run_window_workflow_params ORDER BY task_id, key`)
	if err != nil {
		return nil, err
	}
	defer paramRows.Close()
	params := map[string]map[string]string{}
	for paramRows.Next() {
		var taskID, key, value string
		if err := paramRows.Scan(&taskID, &key, &value); err != nil {
			return nil, err
		}
		if params[taskID] == nil {
			params[taskID] = map[string]string{}
		}
		params[taskID][key] = value
	}
	if err := paramRows.Err(); err != nil {
		return nil, err
	}
	for index := range tasks {
		tasks[index].Draft.WorkflowParams = params[tasks[index].ID]
	}
	return tasks, nil
}

func runWindowTimePointer(value sql.NullInt64) *time.Time {
	if !value.Valid || value.Int64 == 0 {
		return nil
	}
	timestamp := time.Unix(0, value.Int64)
	return &timestamp
}

func saveRunWindowTasks(db *sql.DB, tasks []RunWindowTask) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := writeRunWindowTasks(tx, tasks); err != nil {
		return err
	}
	return tx.Commit()
}

type runWindowStoredSignatures struct {
	task     string
	params   string
	position int
}

func writeRunWindowTasks(tx *sql.Tx, tasks []RunWindowTask) error {
	existingSignatures := map[string]runWindowStoredSignatures{}
	signatureRows, err := tx.Query(`SELECT id, signature, params_signature, position FROM run_window_tasks`)
	if err != nil {
		return err
	}
	for signatureRows.Next() {
		var id string
		var signatures runWindowStoredSignatures
		if err := signatureRows.Scan(&id, &signatures.task, &signatures.params, &signatures.position); err != nil {
			_ = signatureRows.Close()
			return err
		}
		existingSignatures[id] = signatures
	}
	if err := signatureRows.Err(); err != nil {
		_ = signatureRows.Close()
		return err
	}
	_ = signatureRows.Close()
	kept := make(map[string]struct{}, len(tasks))
	for position, task := range tasks {
		if task.ID == "" {
			return fmt.Errorf("运行时段任务 ID 为空")
		}
		signature, err := runWindowTaskSignature(task)
		if err != nil {
			return err
		}
		kept[task.ID] = struct{}{}
		storedSignatures := existingSignatures[task.ID]
		if storedSignatures.task == signature && storedSignatures.position == position {
			continue
		}
		paramsSignature, err := runWindowParamsSignature(task.Draft.WorkflowParams)
		if err != nil {
			return err
		}
		if err := writeRunWindowTask(tx, task, position, signature, paramsSignature, storedSignatures.params != paramsSignature); err != nil {
			return err
		}
	}
	rows, err := tx.Query(`SELECT id FROM run_window_tasks`)
	if err != nil {
		return err
	}
	stale := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		if _, ok := kept[id]; !ok {
			stale = append(stale, id)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	for _, id := range stale {
		if _, err := tx.Exec(`DELETE FROM run_window_tasks WHERE id=?`, id); err != nil {
			return err
		}
	}
	return nil
}

func writeRunWindowTask(tx *sql.Tx, task RunWindowTask, position int, signature, paramsSignature string, rewriteParams bool) error {
	var startAt, createdAt int64
	if !task.StartAt.IsZero() {
		startAt = task.StartAt.UnixNano()
	}
	if !task.CreatedAt.IsZero() {
		createdAt = task.CreatedAt.UnixNano()
	}
	if _, err := tx.Exec(`INSERT INTO run_window_tasks(
		id, signature, params_signature, position, name, prompt, initial_message, task_type, expert_id, expert_name, workflow_template_id,
		workspace_kind, local_path, cloud_workspace_id, cloud_name, remote_host, remote_port, remote_user,
		remote_work_dir, latex_template_id, latex_template_name, status, start_at_unix_nano, end_at_unix_nano,
		project_path, session_user_id, claimed_at_unix_nano, started_at_unix_nano, created_at_unix_nano, last_error)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET signature=excluded.signature, params_signature=excluded.params_signature, position=excluded.position, name=excluded.name, prompt=excluded.prompt,
		initial_message=excluded.initial_message, task_type=excluded.task_type, expert_id=excluded.expert_id,
		expert_name=excluded.expert_name, workflow_template_id=excluded.workflow_template_id,
		workspace_kind=excluded.workspace_kind, local_path=excluded.local_path,
		cloud_workspace_id=excluded.cloud_workspace_id, cloud_name=excluded.cloud_name, remote_host=excluded.remote_host,
		remote_port=excluded.remote_port, remote_user=excluded.remote_user, remote_work_dir=excluded.remote_work_dir,
		latex_template_id=excluded.latex_template_id, latex_template_name=excluded.latex_template_name,
		status=excluded.status, start_at_unix_nano=excluded.start_at_unix_nano, end_at_unix_nano=excluded.end_at_unix_nano,
		project_path=excluded.project_path, session_user_id=excluded.session_user_id,
		claimed_at_unix_nano=excluded.claimed_at_unix_nano, started_at_unix_nano=excluded.started_at_unix_nano,
		created_at_unix_nano=excluded.created_at_unix_nano, last_error=excluded.last_error`,
		task.ID, signature, paramsSignature, position, task.Name, task.Prompt, task.InitialMessage, task.Draft.TaskType, task.Draft.ExpertID,
		task.Draft.ExpertName, task.Draft.WorkflowTemplateID, task.Draft.WorkspaceKind, task.Draft.LocalPath,
		task.Draft.CloudWorkspaceID, task.Draft.CloudName, task.Draft.RemoteHost, task.Draft.RemotePort,
		task.Draft.RemoteUser, task.Draft.RemoteWorkDir, task.Draft.LatexTemplateID, task.Draft.LatexTemplateName,
		task.Status, startAt, nullableRunWindowTime(task.EndAt), task.ProjectPath, task.SessionUserID,
		nullableRunWindowTime(task.ClaimedAt), nullableRunWindowTime(task.StartedAt), createdAt, task.LastError); err != nil {
		return err
	}
	if !rewriteParams {
		return nil
	}
	if _, err := tx.Exec(`DELETE FROM run_window_workflow_params WHERE task_id=?`, task.ID); err != nil {
		return err
	}
	for key, value := range task.Draft.WorkflowParams {
		if _, err := tx.Exec(`INSERT INTO run_window_workflow_params(task_id, key, value) VALUES(?,?,?)`, task.ID, key, value); err != nil {
			return err
		}
	}
	return nil
}

func nullableRunWindowTime(value *time.Time) interface{} {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.UnixNano()
}
