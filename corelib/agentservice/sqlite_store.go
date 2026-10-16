package agentservice

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStore is the durable control-plane repository used when a host wants
// database-level atomicity for message/run/outbox lifecycle changes. The
// public Store interface intentionally remains unchanged; this repository
// adds the optional lifecycle and RunEventStore capabilities on top.
//
// The state document is kept as one versioned SQLite row rather than split
// across several independently committed files. That gives admission and
// terminal transitions one ACID boundary today while preserving the exact
// query semantics (including credential migration and cascade deletes) of
// MemoryStore. The format is deliberately private so it can be replaced by
// normalized tables in a later migration without changing callers.
type SQLiteStore struct {
	mu     sync.Mutex
	db     *sql.DB
	path   string
	inner  *MemoryStore
	closed bool
}

const sqliteStoreStateVersion = 1

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	if err := secureMkdirAll(filepath.Dir(path)); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &SQLiteStore{db: db, path: path, inner: NewMemoryStore()}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS agentservice_state (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  schema_version INTEGER NOT NULL,
  state_json BLOB NOT NULL,
  updated_at TEXT NOT NULL
);`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.ensureInitialState(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) ensureInitialState() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var payload []byte
	var version int
	err := s.db.QueryRow(`SELECT schema_version, state_json FROM agentservice_state WHERE id=1`).Scan(&version, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		// MaClawSrv historically persisted the control plane as state/store.json.
		// Import that document exactly once when the new SQLite backend is first
		// opened so switching backends does not silently hide existing tenants,
		// sessions or runs. The legacy file is retained as a user-visible backup.
		legacyPath := filepath.Join(filepath.Dir(s.path), "store.json")
		if legacy, readErr := os.ReadFile(legacyPath); readErr == nil && len(legacy) > 0 {
			if applyErr := s.applyState(legacy); applyErr != nil {
				return applyErr
			}
			// Re-encode through MemoryStore's credential normalizer instead of
			// copying a legacy document that may still contain plaintext api_key.
			payload, err = json.Marshal(s.inner.snapshot())
		} else {
			payload, err = json.Marshal(s.inner.snapshot())
		}
		if err != nil {
			return err
		}
		_, err = s.db.Exec(`INSERT INTO agentservice_state(id, schema_version, state_json, updated_at) VALUES(1,?,?,?)`, sqliteStoreStateVersion, payload, time.Now().UTC().Format(time.RFC3339Nano))
		return err
	}
	if err != nil {
		return err
	}
	if version != sqliteStoreStateVersion {
		return fmt.Errorf("unsupported agentservice SQLite state schema version %d", version)
	}
	return s.applyState(payload)
}

func (s *SQLiteStore) applyState(payload []byte) error {
	var state storeState
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &state); err != nil {
			return err
		}
	}
	s.inner = newMemoryStoreFromState(state)
	return nil
}

func (s *SQLiteStore) ensureOpenLocked() error {
	if s == nil || s.db == nil || s.inner == nil || s.closed {
		return ErrServiceClosed
	}
	return nil
}

// refreshLocked reloads the committed state before a read/write. This keeps
// separate SQLiteStore handles coherent and makes cross-process idempotency
// replays observe the winner of a concurrent transaction.
func (s *SQLiteStore) refreshLocked() error {
	if err := s.ensureOpenLocked(); err != nil {
		return err
	}
	var payload []byte
	var version int
	if err := s.db.QueryRow(`SELECT schema_version, state_json FROM agentservice_state WHERE id=1`).Scan(&version, &payload); err != nil {
		return err
	}
	if version != sqliteStoreStateVersion {
		return fmt.Errorf("unsupported agentservice SQLite state schema version %d", version)
	}
	return s.applyState(payload)
}

func (s *SQLiteStore) read(fn func(*MemoryStore) error) error {
	if s == nil {
		return ErrServiceClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshLocked(); err != nil {
		return err
	}
	return fn(s.inner)
}

func (s *SQLiteStore) write(fn func(*MemoryStore) error) error {
	if s == nil {
		return ErrServiceClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureOpenLocked(); err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		err := s.writeOnceLocked(fn)
		if !isSQLiteBusy(err) || attempt >= 6 {
			return err
		}
		// A deferred SQLite transaction can observe a stale read snapshot when
		// another process wins the writer lock. Retrying the complete operation
		// reloads the latest state and preserves idempotency semantics.
		time.Sleep(time.Duration(10*(1<<attempt)) * time.Millisecond)
	}
}

func (s *SQLiteStore) writeOnceLocked(fn func(*MemoryStore) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var payload []byte
	var version int
	if err := tx.QueryRow(`SELECT schema_version, state_json FROM agentservice_state WHERE id=1`).Scan(&version, &payload); err != nil {
		return err
	}
	if version != sqliteStoreStateVersion {
		return fmt.Errorf("unsupported agentservice SQLite state schema version %d", version)
	}
	working := NewMemoryStore()
	if len(payload) > 0 {
		var state storeState
		if err := json.Unmarshal(payload, &state); err != nil {
			return err
		}
		working = newMemoryStoreFromState(state)
	}
	if err := fn(working); err != nil {
		return err
	}
	next, err := json.Marshal(working.snapshot())
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE agentservice_state SET schema_version=?, state_json=?, updated_at=? WHERE id=1`, sqliteStoreStateVersion, next, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.inner = working
	return nil
}

func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "database is locked") || strings.Contains(text, "sqlite_busy") || strings.Contains(text, "database table is locked")
}

// Store methods. Reads are wrapped as well as writes so a second process's
// committed changes are visible without requiring a service restart.
func (s *SQLiteStore) SaveTenant(v Tenant) error {
	return s.write(func(m *MemoryStore) error { return m.SaveTenant(v) })
}
func (s *SQLiteStore) GetTenant(id string) (Tenant, error) {
	var out Tenant
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.GetTenant(id); return e })
	return out, err
}
func (s *SQLiteStore) ListTenants() ([]Tenant, error) {
	var out []Tenant
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.ListTenants(); return e })
	return out, err
}
func (s *SQLiteStore) DeleteTenant(id string) error {
	return s.write(func(m *MemoryStore) error { return m.DeleteTenant(id) })
}
func (s *SQLiteStore) SaveUser(v User) error {
	return s.write(func(m *MemoryStore) error { return m.SaveUser(v) })
}
func (s *SQLiteStore) GetUser(t, u string) (User, error) {
	var out User
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.GetUser(t, u); return e })
	return out, err
}
func (s *SQLiteStore) ListUsers(t string) ([]User, error) {
	var out []User
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.ListUsers(t); return e })
	return out, err
}
func (s *SQLiteStore) DeleteUser(t, u string) error {
	return s.write(func(m *MemoryStore) error { return m.DeleteUser(t, u) })
}
func (s *SQLiteStore) SaveCredential(v Credential) error {
	return s.write(func(m *MemoryStore) error { return m.SaveCredential(v) })
}
func (s *SQLiteStore) GetCredential(t, u, id string) (Credential, error) {
	var out Credential
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.GetCredential(t, u, id); return e })
	return out, err
}
func (s *SQLiteStore) ListCredentials(t, u string) ([]Credential, error) {
	var out []Credential
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.ListCredentials(t, u); return e })
	return out, err
}
func (s *SQLiteStore) GetCredentialByAPIKey(key string) (Credential, error) {
	var out Credential
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.GetCredentialByAPIKey(key); return e })
	return out, err
}
func (s *SQLiteStore) SaveUserConfig(v UserConfig) error {
	return s.write(func(m *MemoryStore) error { return m.SaveUserConfig(v) })
}
func (s *SQLiteStore) GetUserConfig(t, u string) (UserConfig, error) {
	var out UserConfig
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.GetUserConfig(t, u); return e })
	return out, err
}
func (s *SQLiteStore) SaveInstance(v Instance) error {
	return s.write(func(m *MemoryStore) error { return m.SaveInstance(v) })
}
func (s *SQLiteStore) GetInstance(t, u, id string) (Instance, error) {
	var out Instance
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.GetInstance(t, u, id); return e })
	return out, err
}
func (s *SQLiteStore) ListInstances(t, u string) ([]Instance, error) {
	var out []Instance
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.ListInstances(t, u); return e })
	return out, err
}
func (s *SQLiteStore) DeleteInstance(t, u, id string) error {
	return s.write(func(m *MemoryStore) error { return m.DeleteInstance(t, u, id) })
}
func (s *SQLiteStore) SaveSession(v Session) error {
	return s.write(func(m *MemoryStore) error { return m.SaveSession(v) })
}
func (s *SQLiteStore) GetSession(t, u, i, id string) (Session, error) {
	var out Session
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.GetSession(t, u, i, id); return e })
	return out, err
}
func (s *SQLiteStore) ListSessions(t, u, i string) ([]Session, error) {
	var out []Session
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.ListSessions(t, u, i); return e })
	return out, err
}
func (s *SQLiteStore) DeleteSession(t, u, i, id string) error {
	return s.write(func(m *MemoryStore) error { return m.DeleteSession(t, u, i, id) })
}
func (s *SQLiteStore) SaveMessage(v Message) error {
	return s.write(func(m *MemoryStore) error { return m.SaveMessage(v) })
}
func (s *SQLiteStore) ListMessages(id string) ([]Message, error) {
	var out []Message
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.ListMessages(id); return e })
	return out, err
}
func (s *SQLiteStore) DeleteMessages(id string, ids []string) (int, error) {
	var n int
	err := s.write(func(m *MemoryStore) error { var e error; n, e = m.DeleteMessages(id, ids); return e })
	return n, err
}
func (s *SQLiteStore) SaveRun(v Run) error {
	return s.write(func(m *MemoryStore) error { return m.SaveRun(v) })
}
func (s *SQLiteStore) GetRun(t, u, i, id string) (Run, error) {
	var out Run
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.GetRun(t, u, i, id); return e })
	return out, err
}
func (s *SQLiteStore) ListRuns(t, u, i string) ([]Run, error) {
	var out []Run
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.ListRuns(t, u, i); return e })
	return out, err
}
func (s *SQLiteStore) GetRunByUserMessageID(t, u, i, id string) (Run, error) {
	var out Run
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.GetRunByUserMessageID(t, u, i, id); return e })
	return out, err
}
func (s *SQLiteStore) SaveAuditEvent(v AuditEvent) error {
	return s.write(func(m *MemoryStore) error { return m.SaveAuditEvent(v) })
}
func (s *SQLiteStore) ListAuditEvents(t, u string) ([]AuditEvent, error) {
	var out []AuditEvent
	err := s.read(func(m *MemoryStore) error { var e error; out, e = m.ListAuditEvents(t, u); return e })
	return out, err
}
func (s *SQLiteStore) DeleteAuditEvents(t, u string) (int, error) {
	var n int
	err := s.write(func(m *MemoryStore) error { var e error; n, e = m.DeleteAuditEvents(t, u); return e })
	return n, err
}

func (s *SQLiteStore) SaveRunAdmission(message Message, run Run) error {
	return s.SaveRunAdmissionWithEvents(message, run, nil)
}

func (s *SQLiteStore) SaveRunAdmissionWithEvents(message Message, run Run, events []RunEvent) error {
	return s.write(func(m *MemoryStore) error {
		if err := m.SaveMessage(message); err != nil {
			return err
		}
		if err := m.SaveRun(run); err != nil {
			return err
		}
		for _, event := range events {
			if _, err := m.Append(event); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *SQLiteStore) SaveRunCompletion(message Message, run Run) error {
	return s.SaveRunCompletionWithEvents(message, run, nil)
}

func (s *SQLiteStore) SaveRunCompletionWithEvents(message Message, run Run, events []RunEvent) error {
	return s.write(func(m *MemoryStore) error {
		if err := m.SaveMessage(message); err != nil {
			return err
		}
		if err := m.SaveRun(run); err != nil {
			return err
		}
		for _, event := range events {
			if _, err := m.Append(event); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *SQLiteStore) SaveRunTerminalWithEvents(run Run, events []RunEvent) error {
	return s.write(func(m *MemoryStore) error {
		if err := m.SaveRun(run); err != nil {
			return err
		}
		for _, event := range events {
			if _, err := m.Append(event); err != nil {
				return err
			}
		}
		return nil
	})
}

// CommitRunLifecycle is the single transaction seam custom hosts can mirror
// when they provide a SQL-backed Store. The generic contract is validated
// before entering SQLite, then the phase-specific operation runs inside the
// existing write transaction.
func (s *SQLiteStore) CommitRunLifecycle(mutation RunLifecycleMutation) error {
	if err := validateRunLifecycleMutation(mutation); err != nil {
		return err
	}
	return s.write(func(m *MemoryStore) error {
		switch mutation.Phase {
		case RunLifecyclePhaseAdmission:
			if err := m.SaveMessage(*mutation.Message); err != nil {
				return err
			}
			if err := m.SaveRun(mutation.Run); err != nil {
				return err
			}
		case RunLifecyclePhaseCompletion:
			if err := m.SaveMessage(*mutation.Message); err != nil {
				return err
			}
			if err := m.SaveRun(mutation.Run); err != nil {
				return err
			}
		case RunLifecyclePhaseTerminal:
			if err := m.SaveRun(mutation.Run); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported run lifecycle phase %q", mutation.Phase)
		}
		for _, event := range mutation.Events {
			if _, err := m.Append(event); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *SQLiteStore) Append(event RunEvent) (RunEvent, error) {
	var out RunEvent
	err := s.write(func(m *MemoryStore) error { var err error; out, err = m.Append(event); return err })
	return out, err
}

func (s *SQLiteStore) SequenceForID(t, u, r, id string) (uint64, bool, error) {
	var seq uint64
	var found bool
	err := s.read(func(m *MemoryStore) error { var err error; seq, found, err = m.SequenceForID(t, u, r, id); return err })
	return seq, found, err
}

func (s *SQLiteStore) ListAfter(t, u, r string, after uint64, limit int) ([]RunEvent, error) {
	var out []RunEvent
	err := s.read(func(m *MemoryStore) error { var err error; out, err = m.ListAfter(t, u, r, after, limit); return err })
	return out, err
}

func (s *SQLiteStore) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

var (
	_ Store                        = (*SQLiteStore)(nil)
	_ RunEventStore                = (*SQLiteStore)(nil)
	_ RunAdmissionEventStore       = (*SQLiteStore)(nil)
	_ RunCompletionEventStore      = (*SQLiteStore)(nil)
	_ RunTerminalEventStore        = (*SQLiteStore)(nil)
	_ RunLifecycleTransactionStore = (*SQLiteStore)(nil)
)
