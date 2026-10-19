package agentservice

import (
	"encoding/json"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// ScheduledTaskStore is the service database this process already opened.
// A file-backed service has no row store and keeps scheduled_tasks.json.
func (s *Service) ScheduledTaskStore() scheduler.TaskStore {
	if s == nil {
		return nil
	}
	db, ok := s.store.(*SQLiteStore)
	if !ok || db == nil {
		return nil
	}
	return db
}

func (s *SQLiteStore) ensureScheduleTableLocked() error {
	if err := s.ensureOpenLocked(); err != nil {
		return err
	}
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS scheduled_tasks (
  id TEXT PRIMARY KEY,
  task_json BLOB NOT NULL,
  updated_at TEXT NOT NULL
)`)
	return err
}

// LoadScheduledTasks reads the schedule rows from this service database.
func (s *SQLiteStore) LoadScheduledTasks() ([]scheduler.ScheduledTask, error) {
	if s == nil {
		return nil, ErrServiceClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureScheduleTableLocked(); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT task_json FROM scheduled_tasks ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []scheduler.ScheduledTask{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var task scheduler.ScheduledTask
		if err := json.Unmarshal(raw, &task); err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, rows.Err()
}

// SaveScheduledTasks replaces the schedule rows in this service database.
func (s *SQLiteStore) SaveScheduledTasks(tasks []scheduler.ScheduledTask) error {
	if s == nil {
		return ErrServiceClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureScheduleTableLocked(); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM scheduled_tasks`); err != nil {
		_ = tx.Rollback()
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, task := range tasks {
		if task.ID == "" {
			continue
		}
		raw, err := json.Marshal(task)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err := tx.Exec(`INSERT INTO scheduled_tasks(id, task_json, updated_at) VALUES(?,?,?)`, task.ID, raw, now); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
