package workflow

import "time"

// persistence.go provides documentation and helper utilities for the
// PersistenceStore interface defined in types.go.
//
// The PersistenceStore interface abstracts workflow state persistence,
// allowing the engine to save/restore workflow states and understanding
// sessions across application restarts.
//
// Implementations:
//   - SQLiteStore (sqlite_store.go): Production implementation using SQLite.
//   - NullStore (this file): No-op fallback when SQLite is unavailable.
//
// Data lifecycle:
//   - Active workflows and understanding sessions are persisted on every
//     state change (phase advance, input handling, etc.).
//   - Completed/cancelled workflows older than 7 days are cleaned up
//     via CleanupExpired.
//   - Understanding sessions inactive for 30+ minutes are cleaned up
//     by IntentUnderstandingManager.CleanupExpired.

// NullStore is a no-op PersistenceStore used when SQLite is unavailable.
// All write operations succeed silently; all read operations return nil/empty.
type NullStore struct{}

var _ PersistenceStore = (*NullStore)(nil)

func (NullStore) SaveUnderstandingSession(_ *UnderstandingSession) error            { return nil }
func (NullStore) LoadUnderstandingSession(_ string) (*UnderstandingSession, error)  { return nil, nil }
func (NullStore) DeleteUnderstandingSession(_ string) error                         { return nil }
func (NullStore) SaveWorkflowState(_ *WorkflowState) error                         { return nil }
func (NullStore) LoadWorkflowState(_ string) (*WorkflowState, error)               { return nil, nil }
func (NullStore) DeleteWorkflowState(_ string) error                               { return nil }
func (NullStore) ListActiveWorkflows() ([]*WorkflowState, error)                   { return nil, nil }
func (NullStore) CleanupExpired(_ time.Duration) error                             { return nil }
