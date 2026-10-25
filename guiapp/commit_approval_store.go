package guiapp

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	_ "modernc.org/sqlite"
)

type sqliteCommitApprovalStore struct {
	mu     sync.Mutex
	db     *sql.DB
	dbPath string
}

var commitApprovalStores sync.Map

// commitApprovalDBPath is this app's own data directory. A nil app, and a
// bare app that has not loaded a data directory, return false so tests do
// not create a file in the user profile.
func commitApprovalDBPath(app *App) (string, bool) {
	if app == nil {
		return "", false
	}
	if base, ok := app.effectiveBaseDir.Load().(string); ok {
		base = strings.TrimSpace(base)
		if base != "" {
			return filepath.Join(base, "commit-approvals.db"), true
		}
	}
	if home := strings.TrimSpace(app.testHomeDir); home != "" {
		return filepath.Join(home, ".maclaw", "commit-approvals.db"), true
	}
	return "", false
}

func commitApprovalStoreForApp(app *App) (*sqliteCommitApprovalStore, error) {
	dbPath, ok := commitApprovalDBPath(app)
	if !ok {
		return nil, nil
	}
	if existing, ok := commitApprovalStores.Load(dbPath); ok {
		return existing.(*sqliteCommitApprovalStore), nil
	}
	store, err := newSQLiteCommitApprovalStore(dbPath)
	if err != nil {
		return nil, err
	}
	actual, loaded := commitApprovalStores.LoadOrStore(dbPath, store)
	stored := actual.(*sqliteCommitApprovalStore)
	if loaded && stored != store {
		_ = store.db.Close()
	}
	return stored, nil
}

func newSQLiteCommitApprovalStore(dbPath string) (*sqliteCommitApprovalStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS commit_decisions (
		task_key TEXT NOT NULL,
		fingerprint TEXT NOT NULL,
		decision TEXT NOT NULL,
		updated_at DATETIME NOT NULL,
		PRIMARY KEY (task_key, fingerprint)
	)`)
	if err == nil {
		_, err = db.Exec(`CREATE TABLE IF NOT EXISTS commit_pending_asks (
			task_key TEXT PRIMARY KEY,
			fingerprint TEXT NOT NULL,
			nonce TEXT NOT NULL,
			created_at DATETIME NOT NULL
		)`)
	}
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &sqliteCommitApprovalStore{db: db, dbPath: dbPath}, nil
}

func (s *sqliteCommitApprovalStore) ensureLocked() error {
	if s.db != nil {
		return nil
	}
	store, err := newSQLiteCommitApprovalStore(s.dbPath)
	if err != nil {
		return err
	}
	s.db = store.db
	return nil
}

func (s *sqliteCommitApprovalStore) closeLocked() {
	if s.db != nil {
		_ = s.db.Close()
		s.db = nil
	}
}

func (s *sqliteCommitApprovalStore) lookup(taskKey, fingerprint string) (agent.CommitDecision, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.closeLocked()
	if err := s.ensureLocked(); err != nil {
		return "", false, err
	}
	var decision string
	err := s.db.QueryRow(`SELECT decision FROM commit_decisions WHERE task_key=? AND fingerprint=?`, taskKey, fingerprint).Scan(&decision)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return agent.CommitDecision(decision), decision != "", nil
}

func (s *sqliteCommitApprovalStore) storeDecision(taskKey, fingerprint string, decision agent.CommitDecision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.closeLocked()
	if err := s.ensureLocked(); err != nil {
		return err
	}
	var err error
	if decision == "" {
		_, err = s.db.Exec(`DELETE FROM commit_decisions WHERE task_key=? AND fingerprint=?`, taskKey, fingerprint)
	} else {
		_, err = s.db.Exec(`INSERT INTO commit_decisions(task_key,fingerprint,decision,updated_at) VALUES(?,?,?,?)
			ON CONFLICT(task_key,fingerprint) DO UPDATE SET decision=excluded.decision,updated_at=excluded.updated_at`,
			taskKey, fingerprint, string(decision), time.Now().UTC())
	}
	return err
}

func (s *sqliteCommitApprovalStore) deniedTools(taskKey string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.closeLocked()
	if err := s.ensureLocked(); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT fingerprint FROM commit_decisions WHERE task_key=? AND decision=?`, taskKey, string(agent.CommitDeny))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var names []string
	for rows.Next() {
		var fingerprint string
		if err := rows.Scan(&fingerprint); err != nil {
			return nil, err
		}
		name, _, _ := strings.Cut(fingerprint, "\n")
		name = strings.TrimSpace(name)
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}

func (s *sqliteCommitApprovalStore) prepare(note commitAskNote) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.closeLocked()
	if err := s.ensureLocked(); err != nil {
		return err
	}
	_, err := s.db.Exec(`INSERT INTO commit_pending_asks(task_key,fingerprint,nonce,created_at) VALUES(?,?,?,?)
		ON CONFLICT(task_key) DO UPDATE SET fingerprint=excluded.fingerprint,nonce=excluded.nonce,created_at=excluded.created_at`,
		note.TaskKey, note.Fingerprint, note.Nonce, time.Now().UTC())
	return err
}

func (s *sqliteCommitApprovalStore) validate(note commitAskNote) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.closeLocked()
	if err := s.ensureLocked(); err != nil {
		return err
	}
	return s.validateLocked(note)
}

func (s *sqliteCommitApprovalStore) validateLocked(note commitAskNote) error {
	var fingerprint, nonce string
	err := s.db.QueryRow(`SELECT fingerprint,nonce FROM commit_pending_asks WHERE task_key=?`, note.TaskKey).Scan(&fingerprint, &nonce)
	if err == sql.ErrNoRows {
		return fmt.Errorf("commit approval is no longer pending")
	}
	if err != nil {
		return err
	}
	if fingerprint != note.Fingerprint || nonce != note.Nonce {
		return fmt.Errorf("commit approval ticket mismatch")
	}
	return nil
}

func (s *sqliteCommitApprovalStore) settle(note commitAskNote, decision agent.CommitDecision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.closeLocked()
	if err := s.ensureLocked(); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var fingerprint, nonce string
	err = tx.QueryRow(`SELECT fingerprint,nonce FROM commit_pending_asks WHERE task_key=?`, note.TaskKey).Scan(&fingerprint, &nonce)
	if err == sql.ErrNoRows {
		return fmt.Errorf("commit approval is no longer pending")
	}
	if err != nil {
		return err
	}
	if fingerprint != note.Fingerprint || nonce != note.Nonce {
		return fmt.Errorf("commit approval ticket mismatch")
	}
	if _, err = tx.Exec(`INSERT INTO commit_decisions(task_key,fingerprint,decision,updated_at) VALUES(?,?,?,?)
		ON CONFLICT(task_key,fingerprint) DO UPDATE SET decision=excluded.decision,updated_at=excluded.updated_at`,
		note.TaskKey, note.Fingerprint, string(decision), time.Now().UTC()); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM commit_pending_asks WHERE task_key=?`, note.TaskKey); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *sqliteCommitApprovalStore) latestPendingTaskKey(scope string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.closeLocked()
	if err := s.ensureLocked(); err != nil {
		return "", err
	}
	prefix := scope + "\n"
	var taskKey string
	err := s.db.QueryRow(`SELECT task_key FROM commit_pending_asks
		WHERE substr(task_key,1,?)=? ORDER BY created_at DESC LIMIT 1`, len(prefix), prefix).Scan(&taskKey)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return taskKey, err
}

func (s *sqliteCommitApprovalStore) forget(taskKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.closeLocked()
	if err := s.ensureLocked(); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.Exec(`DELETE FROM commit_decisions WHERE task_key=?`, taskKey); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM commit_pending_asks WHERE task_key=?`, taskKey); err != nil {
		return err
	}
	return tx.Commit()
}
