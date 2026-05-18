package workflow

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStore implements PersistenceStore using a local SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

var _ PersistenceStore = (*SQLiteStore)(nil)

// NewSQLiteStore creates a new SQLiteStore at the given database path.
// It ensures the parent directory exists, opens the connection, applies
// pragmas (WAL, busy_timeout, synchronous), and creates tables/indexes.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	if err := ensureParentDir(dbPath); err != nil {
		return nil, fmt.Errorf("workflow sqlite: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("workflow sqlite open: %w", err)
	}

	if err := applyWorkflowPragmas(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	if err := createWorkflowTables(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &SQLiteStore{db: db}, nil
}

// Close closes the underlying database connection.
func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// ---------------------------------------------------------------------------
// Understanding session operations
// ---------------------------------------------------------------------------

func (s *SQLiteStore) SaveUnderstandingSession(session *UnderstandingSession) error {
	intentJSON, err := json.Marshal(session.Intent)
	if err != nil {
		return fmt.Errorf("marshal intent: %w", err)
	}
	roundsJSON, err := json.Marshal(session.Rounds)
	if err != nil {
		return fmt.Errorf("marshal rounds: %w", err)
	}

	const q = `INSERT OR REPLACE INTO understanding_sessions
		(id, user_id, intent_json, rounds_json, state, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`

	_, err = s.db.Exec(q,
		session.ID,
		session.UserID,
		string(intentJSON),
		string(roundsJSON),
		string(session.State),
		session.CreatedAt.UTC(),
		session.UpdatedAt.UTC(),
	)
	return err
}

func (s *SQLiteStore) LoadUnderstandingSession(userID string) (*UnderstandingSession, error) {
	const q = `SELECT id, user_id, intent_json, rounds_json, state, created_at, updated_at
		FROM understanding_sessions WHERE user_id = ?`

	row := s.db.QueryRow(q, userID)

	var (
		sess       UnderstandingSession
		intentJSON string
		roundsJSON string
		stateStr   string
	)
	err := row.Scan(
		&sess.ID,
		&sess.UserID,
		&intentJSON,
		&roundsJSON,
		&stateStr,
		&sess.CreatedAt,
		&sess.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(intentJSON), &sess.Intent); err != nil {
		return nil, fmt.Errorf("unmarshal intent: %w", err)
	}
	if err := json.Unmarshal([]byte(roundsJSON), &sess.Rounds); err != nil {
		return nil, fmt.Errorf("unmarshal rounds: %w", err)
	}
	sess.State = UnderstandingState(stateStr)
	return &sess, nil
}

func (s *SQLiteStore) DeleteUnderstandingSession(userID string) error {
	_, err := s.db.Exec(`DELETE FROM understanding_sessions WHERE user_id = ?`, userID)
	return err
}

// ---------------------------------------------------------------------------
// Workflow state operations
// ---------------------------------------------------------------------------

func (s *SQLiteStore) SaveWorkflowState(state *WorkflowState) error {
	intentJSON, err := json.Marshal(state.Intent)
	if err != nil {
		return fmt.Errorf("marshal intent: %w", err)
	}
	outputsJSON, err := json.Marshal(state.PhaseOutputs)
	if err != nil {
		return fmt.Errorf("marshal outputs: %w", err)
	}
	gatesJSON, err := json.Marshal(state.GateResults)
	if err != nil {
		return fmt.Errorf("marshal gates: %w", err)
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal workflow state: %w", err)
	}

	const q = `INSERT OR REPLACE INTO workflow_states
		(id, user_id, type, intent_json, current_phase, phase_index,
		 outputs_json, gates_json, status, created_at, updated_at, state_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = s.db.Exec(q,
		state.ID,
		state.UserID,
		string(state.Type),
		string(intentJSON),
		state.CurrentPhase,
		state.PhaseIndex,
		string(outputsJSON),
		string(gatesJSON),
		string(state.Status),
		state.CreatedAt.UTC(),
		state.UpdatedAt.UTC(),
		string(stateJSON),
	)
	return err
}

func (s *SQLiteStore) LoadWorkflowState(userID string) (*WorkflowState, error) {
	const q = `SELECT id, user_id, type, intent_json, current_phase, phase_index,
		outputs_json, gates_json, status, created_at, updated_at, state_json
		FROM workflow_states WHERE user_id = ?`

	row := s.db.QueryRow(q, userID)

	var (
		ws         WorkflowState
		intentJSON string
		outputJSON string
		gatesJSON  string
		typeStr    string
		statusStr  string
		stateJSON  sql.NullString
	)
	err := row.Scan(
		&ws.ID,
		&ws.UserID,
		&typeStr,
		&intentJSON,
		&ws.CurrentPhase,
		&ws.PhaseIndex,
		&outputJSON,
		&gatesJSON,
		&statusStr,
		&ws.CreatedAt,
		&ws.UpdatedAt,
		&stateJSON,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if stateJSON.Valid && strings.TrimSpace(stateJSON.String) != "" {
		var full WorkflowState
		if err := json.Unmarshal([]byte(stateJSON.String), &full); err != nil {
			return nil, fmt.Errorf("unmarshal workflow state: %w", err)
		}
		full.ID = ws.ID
		full.UserID = ws.UserID
		full.Type = WorkflowType(typeStr)
		full.CurrentPhase = ws.CurrentPhase
		full.PhaseIndex = ws.PhaseIndex
		full.Status = WorkflowStatus(statusStr)
		full.CreatedAt = ws.CreatedAt
		full.UpdatedAt = ws.UpdatedAt
		return &full, nil
	}

	ws.Type = WorkflowType(typeStr)
	ws.Status = WorkflowStatus(statusStr)

	if err := json.Unmarshal([]byte(intentJSON), &ws.Intent); err != nil {
		return nil, fmt.Errorf("unmarshal intent: %w", err)
	}
	if err := json.Unmarshal([]byte(outputJSON), &ws.PhaseOutputs); err != nil {
		return nil, fmt.Errorf("unmarshal outputs: %w", err)
	}
	if err := json.Unmarshal([]byte(gatesJSON), &ws.GateResults); err != nil {
		return nil, fmt.Errorf("unmarshal gates: %w", err)
	}
	return &ws, nil
}

func (s *SQLiteStore) DeleteWorkflowState(id string) error {
	_, err := s.db.Exec(`DELETE FROM workflow_states WHERE id = ?`, id)
	return err
}

func (s *SQLiteStore) ListActiveWorkflows() ([]*WorkflowState, error) {
	const q = `SELECT id, user_id, type, intent_json, current_phase, phase_index,
		outputs_json, gates_json, status, created_at, updated_at, state_json
		FROM workflow_states WHERE status = 'active'`

	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*WorkflowState
	for rows.Next() {
		var (
			ws         WorkflowState
			intentJSON string
			outputJSON string
			gatesJSON  string
			typeStr    string
			statusStr  string
			stateJSON  sql.NullString
		)
		if err := rows.Scan(
			&ws.ID,
			&ws.UserID,
			&typeStr,
			&intentJSON,
			&ws.CurrentPhase,
			&ws.PhaseIndex,
			&outputJSON,
			&gatesJSON,
			&statusStr,
			&ws.CreatedAt,
			&ws.UpdatedAt,
			&stateJSON,
		); err != nil {
			return nil, err
		}
		if stateJSON.Valid && strings.TrimSpace(stateJSON.String) != "" {
			var full WorkflowState
			if err := json.Unmarshal([]byte(stateJSON.String), &full); err != nil {
				return nil, fmt.Errorf("unmarshal workflow state: %w", err)
			}
			full.ID = ws.ID
			full.UserID = ws.UserID
			full.Type = WorkflowType(typeStr)
			full.CurrentPhase = ws.CurrentPhase
			full.PhaseIndex = ws.PhaseIndex
			full.Status = WorkflowStatus(statusStr)
			full.CreatedAt = ws.CreatedAt
			full.UpdatedAt = ws.UpdatedAt
			result = append(result, &full)
			continue
		}

		ws.Type = WorkflowType(typeStr)
		ws.Status = WorkflowStatus(statusStr)

		if err := json.Unmarshal([]byte(intentJSON), &ws.Intent); err != nil {
			return nil, fmt.Errorf("unmarshal intent: %w", err)
		}
		if err := json.Unmarshal([]byte(outputJSON), &ws.PhaseOutputs); err != nil {
			return nil, fmt.Errorf("unmarshal outputs: %w", err)
		}
		if err := json.Unmarshal([]byte(gatesJSON), &ws.GateResults); err != nil {
			return nil, fmt.Errorf("unmarshal gates: %w", err)
		}
		result = append(result, &ws)
	}
	return result, rows.Err()
}

// ---------------------------------------------------------------------------
// Cleanup
// ---------------------------------------------------------------------------

// CleanupExpired removes completed/cancelled workflow states and understanding
// sessions whose updated_at is older than the given duration.
func (s *SQLiteStore) CleanupExpired(olderThan time.Duration) error {
	cutoff := time.Now().Add(-olderThan).UTC()

	if _, err := s.db.Exec(
		`DELETE FROM workflow_states WHERE status IN ('completed','cancelled') AND updated_at < ?`,
		cutoff,
	); err != nil {
		return fmt.Errorf("cleanup workflow_states: %w", err)
	}

	if _, err := s.db.Exec(
		`DELETE FROM understanding_sessions WHERE state IN ('confirmed','cancelled','expired') AND updated_at < ?`,
		cutoff,
	); err != nil {
		return fmt.Errorf("cleanup understanding_sessions: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func ensureParentDir(dbPath string) error {
	if dbPath == "" || dbPath == ":memory:" {
		return nil
	}
	parent := filepath.Dir(dbPath)
	if parent == "." || parent == "" {
		return nil
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create workflow data dir: %w", err)
	}
	return nil
}

func applyWorkflowPragmas(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA foreign_keys = ON;",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("apply pragma %q: %w", p, err)
		}
	}
	return nil
}

func createWorkflowTables(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS understanding_sessions (
			id          TEXT PRIMARY KEY,
			user_id     TEXT NOT NULL UNIQUE,
			intent_json TEXT NOT NULL,
			rounds_json TEXT NOT NULL,
			state       TEXT NOT NULL DEFAULT 'active',
			created_at  DATETIME NOT NULL,
			updated_at  DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS workflow_states (
			id            TEXT PRIMARY KEY,
			user_id       TEXT NOT NULL UNIQUE,
			type          TEXT NOT NULL,
			intent_json   TEXT NOT NULL,
			current_phase TEXT NOT NULL,
			phase_index   INTEGER NOT NULL DEFAULT 0,
			outputs_json  TEXT NOT NULL DEFAULT '{}',
			gates_json    TEXT NOT NULL DEFAULT '{}',
			status        TEXT NOT NULL DEFAULT 'active',
			created_at    DATETIME NOT NULL,
			updated_at    DATETIME NOT NULL,
			state_json    TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ws_user_status ON workflow_states(user_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_us_user_state ON understanding_sessions(user_id, state)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("create table: %w", err)
		}
	}
	return ensureWorkflowStateJSONColumn(db)
}

func ensureWorkflowStateJSONColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(workflow_states)`)
	if err != nil {
		return fmt.Errorf("inspect workflow_states columns: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue interface{}
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return fmt.Errorf("scan workflow_states column: %w", err)
		}
		if name == "state_json" {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := db.Exec(`ALTER TABLE workflow_states ADD COLUMN state_json TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("add workflow state_json column: %w", err)
	}
	return nil
}
