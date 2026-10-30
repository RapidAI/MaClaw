package scheduler

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

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
	if err := ensureScheduledTaskSchema(db); err != nil {
		_ = db.Close()
		return nil, err
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

// LegacyTaskImportDone reports whether scheduled_tasks.json was already copied.
func (s *sqliteTaskStore) LegacyTaskImportDone() (bool, error) {
	if s == nil {
		return false, fmt.Errorf("scheduler: task store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.conn()
	if err != nil {
		return false, err
	}
	return TaskLegacyImportDone(db)
}

// MarkLegacyTaskImportDone records that the pre-migration file has been read.
func (s *sqliteTaskStore) MarkLegacyTaskImportDone() error {
	if s == nil {
		return fmt.Errorf("scheduler: task store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.conn()
	if err != nil {
		return err
	}
	return MarkTaskLegacyImportDone(db)
}

// TaskLegacyImportDone reads the shared schedule_meta marker.
func TaskLegacyImportDone(db *sql.DB) (bool, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM schedule_meta WHERE key = 'legacy_json_imported'`).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("scheduler: read import marker: %w", err)
	}
	return value == "1", nil
}

// MarkTaskLegacyImportDone writes the shared schedule_meta marker.
func MarkTaskLegacyImportDone(db *sql.DB) error {
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
	return loadScheduledTaskRows(db, true)
}

// SaveScheduledTasks replaces the task rows.
func (s *sqliteTaskStore) SaveScheduledTasks(tasks []ScheduledTask) error {
	if s == nil {
		return fmt.Errorf("scheduler: task store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.conn()
	if err != nil {
		return err
	}
	return saveScheduledTaskRows(db, tasks, true)
}
