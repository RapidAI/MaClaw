package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// The Apps panel contains user-owned state (custom apps, ordering, pins and
// visibility). Keep it in the MaClaw data directory so it survives WebView
// profile changes and can be included in the normal data backup.
const maxMaclawAppsPanelStateBytes = 10 << 20

var maclawAppsPanelStateMu sync.Mutex

func (a *App) maclawAppsPanelStateDBPath() string {
	return filepath.Join(a.GetDataDir(), "apps_panel.db")
}

func openMaclawAppsPanelStateDB(dbPath string) (*sql.DB, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, fmt.Errorf("apps panel database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create apps panel database directory: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open apps panel database: %w", err)
	}
	for _, pragma := range []string{"PRAGMA journal_mode=WAL", "PRAGMA busy_timeout=5000", "PRAGMA foreign_keys=ON"} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("configure apps panel database: %w", err)
		}
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS apps_panel_state (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		state_json TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create apps panel state table: %w", err)
	}
	return db, nil
}

// LoadMaclawAppsPanelState returns the persisted Apps-panel JSON object, or an
// empty string when no state has been saved yet.
func (a *App) LoadMaclawAppsPanelState() (string, error) {
	maclawAppsPanelStateMu.Lock()
	defer maclawAppsPanelStateMu.Unlock()
	db, err := openMaclawAppsPanelStateDB(a.maclawAppsPanelStateDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	var state string
	err = db.QueryRow(`SELECT state_json FROM apps_panel_state WHERE id = 1`).Scan(&state)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read apps panel state: %w", err)
	}
	return state, nil
}

// SaveMaclawAppsPanelState writes the frontend's panel state atomically into
// the app data SQLite database. The value must be a JSON object, never an
// arbitrary JSON scalar or array.
func (a *App) SaveMaclawAppsPanelState(stateJSON string) error {
	stateJSON = strings.TrimSpace(stateJSON)
	if stateJSON == "" {
		return fmt.Errorf("apps panel state is required")
	}
	if len(stateJSON) > maxMaclawAppsPanelStateBytes {
		return fmt.Errorf("apps panel state exceeds %d bytes", maxMaclawAppsPanelStateBytes)
	}
	var state map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil || state == nil {
		return fmt.Errorf("apps panel state must be a JSON object")
	}

	maclawAppsPanelStateMu.Lock()
	defer maclawAppsPanelStateMu.Unlock()
	db, err := openMaclawAppsPanelStateDB(a.maclawAppsPanelStateDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	canonicalJSON, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("canonicalize apps panel state: %w", err)
	}
	_, err = db.Exec(`INSERT INTO apps_panel_state (id, state_json, updated_at)
		VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET state_json = excluded.state_json, updated_at = excluded.updated_at`,
		string(canonicalJSON), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("save apps panel state: %w", err)
	}
	return nil
}
