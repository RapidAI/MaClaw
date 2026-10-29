package scheduler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// sqliteTaskStore keeps scheduled tasks in a database next to the pre-migration
// JSON file. One connection stays open for the process: a task fire saves
// while holding the scheduler lock, and reopening the file there is the slow
// part. Tests drop the idle connection so a temp directory can be removed.
// Stop does not close it; an in-flight fire may still be writing.
type sqliteTaskStore struct {
	mu   sync.Mutex
	path string
	db   *sql.DB
}

func scheduleDBPath(jsonPath string) string {
	ext := filepath.Ext(jsonPath)
	if strings.EqualFold(ext, ".json") {
		return strings.TrimSuffix(jsonPath, ext) + ".db"
	}
	return jsonPath + ".db"
}

// OpenSQLiteTaskStore stores scheduled tasks at path.
func OpenSQLiteTaskStore(path string) (*sqliteTaskStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("scheduler: task database path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("scheduler: resolve task database: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, fmt.Errorf("scheduler: mkdir: %w", err)
	}
	store := &sqliteTaskStore{path: abs}
	runtime.SetFinalizer(store, func(s *sqliteTaskStore) { s.close() })
	return store, nil
}

// conn returns the shared database. The caller holds s.mu.
func (s *sqliteTaskStore) conn() (*sql.DB, error) {
	if s.db != nil {
		return s.db, nil
	}
	db, err := sql.Open("sqlite", s.path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, fmt.Errorf("scheduler: open task database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if testing.Testing() {
		db.SetMaxIdleConns(0)
	} else {
		db.SetMaxIdleConns(1)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS scheduled_tasks (
		id TEXT PRIMARY KEY,
		position INTEGER NOT NULL,
		task_json BLOB NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("scheduler: task database schema: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schedule_meta (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("scheduler: task database schema: %w", err)
	}
	s.db = db
	return db, nil
}

func (s *sqliteTaskStore) close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		_ = s.db.Close()
		s.db = nil
	}
}

func (s *sqliteTaskStore) legacyImportDone() (bool, error) {
	if s == nil {
		return false, fmt.Errorf("scheduler: task store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.conn()
	if err != nil {
		return false, err
	}
	var value string
	err = db.QueryRow(`SELECT value FROM schedule_meta WHERE key = 'legacy_json_imported'`).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("scheduler: read import marker: %w", err)
	}
	return value == "1", nil
}

func (s *sqliteTaskStore) markLegacyImportDone() error {
	if s == nil {
		return fmt.Errorf("scheduler: task store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.conn()
	if err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO schedule_meta(key, value) VALUES('legacy_json_imported', '1') ON CONFLICT(key) DO NOTHING`); err != nil {
		return fmt.Errorf("scheduler: mark import: %w", err)
	}
	return nil
}

// LoadScheduledTasks reads task rows in their saved order.
func (s *sqliteTaskStore) LoadScheduledTasks() ([]ScheduledTask, error) {
	if s == nil {
		return nil, fmt.Errorf("scheduler: task store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.conn()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT task_json FROM scheduled_tasks ORDER BY position ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScheduledTask{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var task ScheduledTask
		if err := json.Unmarshal(raw, &task); err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, rows.Err()
}

// SaveScheduledTasks replaces the task rows.
func (s *sqliteTaskStore) SaveScheduledTasks(tasks []ScheduledTask) error {
	if s == nil {
		return fmt.Errorf("scheduler: task store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	encoded := make([][]byte, len(tasks))
	for i, task := range tasks {
		raw, err := json.Marshal(task)
		if err != nil {
			return err
		}
		encoded[i] = raw
	}
	db, err := s.conn()
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM scheduled_tasks`); err != nil {
		_ = tx.Rollback()
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := insertScheduledTasks(tx, tasks, encoded, now); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func insertScheduledTasks(tx *sql.Tx, tasks []ScheduledTask, encoded [][]byte, now string) error {
	const maxRows = 200
	for start := 0; start < len(tasks); start += maxRows {
		end := start + maxRows
		if end > len(tasks) {
			end = len(tasks)
		}
		var query strings.Builder
		query.WriteString(`INSERT INTO scheduled_tasks(id, position, task_json, updated_at) VALUES `)
		args := make([]any, 0, (end-start)*4)
		for i := start; i < end; i++ {
			if i > start {
				query.WriteByte(',')
			}
			query.WriteString(`(?,?,?,?)`)
			key := tasks[i].ID
			if key == "" {
				key = fmt.Sprintf("~%d", i)
			}
			args = append(args, key, i, encoded[i], now)
		}
		if _, err := tx.Exec(query.String(), args...); err != nil {
			return err
		}
	}
	return nil
}
