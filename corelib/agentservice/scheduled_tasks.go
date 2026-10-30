package agentservice

import (
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
	return scheduler.EnsureScheduledTaskSchema(s.db)
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
	return scheduler.LoadScheduledTaskRows(s.db, false)
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
	return scheduler.SaveScheduledTaskRows(s.db, tasks, false)
}

// LegacyTaskImportDone reports whether scheduled_tasks.json was already copied
// into this service database.
func (s *SQLiteStore) LegacyTaskImportDone() (bool, error) {
	if s == nil {
		return false, ErrServiceClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureScheduleTableLocked(); err != nil {
		return false, err
	}
	return scheduler.TaskLegacyImportDone(s.db)
}

// MarkLegacyTaskImportDone records that the pre-migration file has been read.
func (s *SQLiteStore) MarkLegacyTaskImportDone() error {
	if s == nil {
		return ErrServiceClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureScheduleTableLocked(); err != nil {
		return err
	}
	return scheduler.MarkTaskLegacyImportDone(s.db)
}
