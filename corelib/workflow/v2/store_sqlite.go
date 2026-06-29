package v2

import (
	"database/sql"
	"encoding/json"
	"time"

	_ "modernc.org/sqlite" // SQLite driver (pure Go, no CGO required)
)

// SQLiteStore persists workflow state to a SQLite database.
// Uses a separate file (workflow_v2.db).
type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS workflows (
		user_id    TEXT PRIMARY KEY,
		state_json TEXT NOT NULL,
		updated_at DATETIME NOT NULL
	)`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Save(state *WorkflowState) error {
	state.UpdatedAt = time.Now()
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT OR REPLACE INTO workflows (user_id, state_json, updated_at) VALUES (?, ?, ?)`,
		state.UserID, string(data), state.UpdatedAt,
	)
	return err
}

func (s *SQLiteStore) Load(userID string) (*WorkflowState, error) {
	var raw string
	err := s.db.QueryRow(`SELECT state_json FROM workflows WHERE user_id = ?`, userID).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state WorkflowState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (s *SQLiteStore) Delete(userID string) error {
	_, err := s.db.Exec(`DELETE FROM workflows WHERE user_id = ?`, userID)
	return err
}

func (s *SQLiteStore) ListAllUserIDs() ([]string, error) {
	rows, err := s.db.Query(`SELECT user_id FROM workflows`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *SQLiteStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
