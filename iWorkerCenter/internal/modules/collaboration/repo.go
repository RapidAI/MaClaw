package collaboration

import (
	"database/sql"
	"fmt"
	"time"
)

// Repo provides persistence for collaboration tasks and events.
type Repo struct {
	write *sql.DB
	read  *sql.DB
}

// NewRepo creates a Repo.
func NewRepo(write, read *sql.DB) *Repo {
	return &Repo{write: write, read: read}
}

// InsertTask creates a new collaboration task.
func (r *Repo) InsertTask(tenantID string, t *Task) error {
	_, err := r.write.Exec(`INSERT INTO collaboration_tasks
		(tenant_id, id, title, description, from_colleague_id, to_colleague_id, to_role_code, status, priority, result, workflow_step_instance_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		tenantID, t.ID, t.Title, t.Description, t.FromColleagueID, t.ToColleagueID,
		t.ToRoleCode, t.Status, t.Priority, t.Result, t.WorkflowStepInstanceID,
		t.CreatedAt.Format(time.RFC3339), t.UpdatedAt.Format(time.RFC3339))
	return err
}

// InsertTaskTx creates a task inside an existing transaction.
func (r *Repo) InsertTaskTx(tenantID string, tx *sql.Tx, t *Task) error {
	_, err := tx.Exec(`INSERT INTO collaboration_tasks
		(tenant_id, id, title, description, from_colleague_id, to_colleague_id, to_role_code, status, priority, result, workflow_step_instance_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		tenantID, t.ID, t.Title, t.Description, t.FromColleagueID, t.ToColleagueID,
		t.ToRoleCode, t.Status, t.Priority, t.Result, t.WorkflowStepInstanceID,
		t.CreatedAt.Format(time.RFC3339), t.UpdatedAt.Format(time.RFC3339))
	return err
}

// UpdateStatus changes a task's status and result.
func (r *Repo) UpdateStatus(tenantID string, id, status, result string) error {
	res, err := r.write.Exec(`UPDATE collaboration_tasks SET status=?, result=?, updated_at=? WHERE id=? AND tenant_id=?`,
		status, result, time.Now().Format(time.RFC3339), id, tenantID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("task %s not found", id)
	}
	return nil
}

// UpdateStatusTx changes status inside a transaction.
func (r *Repo) UpdateStatusTx(tenantID string, tx *sql.Tx, id, status, result string) error {
	res, err := tx.Exec(`UPDATE collaboration_tasks SET status=?, result=?, updated_at=? WHERE id=? AND tenant_id=?`,
		status, result, time.Now().Format(time.RFC3339), id, tenantID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("task %s not found", id)
	}
	return nil
}

// GetByID returns a task by ID.
func (r *Repo) GetByID(tenantID string, id string) (*Task, error) {
	row := r.read.QueryRow(`SELECT id, title, description, from_colleague_id, to_colleague_id, to_role_code,
		status, priority, result, workflow_step_instance_id, created_at, updated_at
		FROM collaboration_tasks WHERE id=? AND tenant_id=?`, id, tenantID)
	return scanTask(row)
}

// ListByColleague returns tasks assigned to a colleague (inbox).
func (r *Repo) ListByColleague(tenantID string, colleagueID string) ([]*Task, error) {
	rows, err := r.read.Query(`SELECT id, title, description, from_colleague_id, to_colleague_id, to_role_code,
		status, priority, result, workflow_step_instance_id, created_at, updated_at
		FROM collaboration_tasks WHERE to_colleague_id=? AND tenant_id=? ORDER BY created_at DESC`, colleagueID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTasks(rows)
}

// ListAll returns all tasks ordered by creation time.
func (r *Repo) ListAll(tenantID string) ([]*Task, error) {
	rows, err := r.read.Query(`SELECT id, title, description, from_colleague_id, to_colleague_id, to_role_code,
		status, priority, result, workflow_step_instance_id, created_at, updated_at
		FROM collaboration_tasks WHERE tenant_id=? ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTasks(rows)
}

// InsertEvent records a state change event.
func (r *Repo) InsertEvent(tenantID string, e *TaskEvent) error {
	_, err := r.write.Exec(`INSERT INTO collaboration_task_events (tenant_id, id, task_id, event, actor_id, note, created_at)
		VALUES (?,?,?,?,?,?,?)`, tenantID, e.ID, e.TaskID, e.Event, e.ActorID, e.Note, e.CreatedAt.Format(time.RFC3339))
	return err
}

// InsertEventTx records an event inside a transaction.
func (r *Repo) InsertEventTx(tenantID string, tx *sql.Tx, e *TaskEvent) error {
	_, err := tx.Exec(`INSERT INTO collaboration_task_events (tenant_id, id, task_id, event, actor_id, note, created_at)
		VALUES (?,?,?,?,?,?,?)`, tenantID, e.ID, e.TaskID, e.Event, e.ActorID, e.Note, e.CreatedAt.Format(time.RFC3339))
	return err
}

// ListEvents returns events for a task.
func (r *Repo) ListEvents(tenantID string, taskID string) ([]*TaskEvent, error) {
	rows, err := r.read.Query(`SELECT id, task_id, event, actor_id, note, created_at
		FROM collaboration_task_events WHERE task_id=? AND tenant_id=? ORDER BY created_at`, taskID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*TaskEvent
	for rows.Next() {
		var e TaskEvent
		var at string
		if err := rows.Scan(&e.ID, &e.TaskID, &e.Event, &e.ActorID, &e.Note, &at); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, at)
		result = append(result, &e)
	}
	return result, rows.Err()
}

func scanTask(row *sql.Row) (*Task, error) {
	var t Task
	var ca, ua string
	if err := row.Scan(&t.ID, &t.Title, &t.Description, &t.FromColleagueID, &t.ToColleagueID,
		&t.ToRoleCode, &t.Status, &t.Priority, &t.Result, &t.WorkflowStepInstanceID, &ca, &ua); err != nil {
		return nil, err
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339, ca)
	t.UpdatedAt, _ = time.Parse(time.RFC3339, ua)
	return &t, nil
}

func scanTasks(rows *sql.Rows) ([]*Task, error) {
	var result []*Task
	for rows.Next() {
		var t Task
		var ca, ua string
		if err := rows.Scan(&t.ID, &t.Title, &t.Description, &t.FromColleagueID, &t.ToColleagueID,
			&t.ToRoleCode, &t.Status, &t.Priority, &t.Result, &t.WorkflowStepInstanceID, &ca, &ua); err != nil {
			return nil, err
		}
		t.CreatedAt, _ = time.Parse(time.RFC3339, ca)
		t.UpdatedAt, _ = time.Parse(time.RFC3339, ua)
		result = append(result, &t)
	}
	return result, rows.Err()
}
