package longhorizon

// store_sqlite.go implements SQLite persistence for horizon task state.
// {root}/horizon/horizon.db holds one row per task: scalar columns mirror the
// fields used for retrieval (owner, status, resumability, recency) and the
// full state travels as a JSON payload. task_state.json under per-task
// directories is only the pre-migration file: the first open imports every
// readable one and removes it.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func horizonDBPath(root string) string {
	return filepath.Join(root, "horizon", "horizon.db")
}

func openHorizonDB(root string, create bool) (*sql.DB, error) {
	path := horizonDBPath(root)
	if !create {
		if _, err := os.Stat(path); err != nil {
			return nil, err
		}
	} else if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if testing.Testing() {
		db.SetMaxIdleConns(0)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS task_states (
  task_id TEXT PRIMARY KEY,
  owner_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  completed INTEGER NOT NULL DEFAULT 0,
  round_index INTEGER NOT NULL DEFAULT 0,
  max_rounds INTEGER NOT NULL DEFAULT 0,
  resumable INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL DEFAULT '',
  payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_task_states_owner_resumable ON task_states(owner_id, resumable, updated_at);
CREATE TABLE IF NOT EXISTS horizon_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// importHorizonJSON copies readable per-task task_state.json files into the
// database once, then removes them. Unreadable or torn files stay on disk and
// keep the import unmarked so a later open retries instead of losing them.
func importHorizonJSON(db *sql.DB, root string) error {
	var marker string
	err := db.QueryRow(`SELECT value FROM horizon_meta WHERE key = 'legacy_import'`).Scan(&marker)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if marker == "1" {
		return nil
	}
	base := filepath.Join(root, "horizon")
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return markHorizonImported(db)
		}
		return nil
	}
	remaining := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		jsonPath := filepath.Join(base, entry.Name(), "task_state.json")
		data, err := os.ReadFile(jsonPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			remaining = true
			continue
		}
		var state TaskState
		if err := json.Unmarshal(data, &state); err != nil || strings.TrimSpace(state.TaskID) == "" {
			// A torn legacy file must not replace rows already stored.
			_ = os.Remove(jsonPath + ".tmp")
			remaining = true
			continue
		}
		if err := upsertTaskState(db, &state); err != nil {
			remaining = true
			continue
		}
		removeTaskStateJSON(jsonPath)
	}
	if remaining {
		return nil
	}
	return markHorizonImported(db)
}

func markHorizonImported(db *sql.DB) error {
	_, err := db.Exec(`INSERT INTO horizon_meta(key, value) VALUES('legacy_import','1') ON CONFLICT(key) DO UPDATE SET value = excluded.value`)
	return err
}

func upsertTaskState(db *sql.DB, state *TaskState) error {
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	completed, resumable := 0, 0
	if state.Completed {
		completed = 1
	}
	if Resumable(state) {
		resumable = 1
	}
	updatedAt := ""
	if !state.UpdatedAt.IsZero() {
		updatedAt = state.UpdatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
	}
	_, err = db.Exec(`INSERT INTO task_states(
		task_id, owner_id, status, completed, round_index, max_rounds, resumable, updated_at, payload)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(task_id) DO UPDATE SET
		owner_id = excluded.owner_id, status = excluded.status, completed = excluded.completed,
		round_index = excluded.round_index, max_rounds = excluded.max_rounds,
		resumable = excluded.resumable, updated_at = excluded.updated_at, payload = excluded.payload`,
		state.TaskID, state.Policy.OwnerID, state.Status, completed, state.RoundIndex, state.MaxRounds,
		resumable, updatedAt, string(payload))
	return err
}

func saveTaskStateDB(root string, state *TaskState) error {
	db, err := openHorizonDB(root, true)
	if err != nil {
		return err
	}
	defer db.Close()
	// Import first so a legacy file for the same task cannot shadow this write.
	if err := importHorizonJSON(db, root); err != nil {
		return err
	}
	return upsertTaskState(db, state)
}

func loadTaskStateDB(root, taskID string) (*TaskState, error) {
	db, err := openHorizonDB(root, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := importHorizonJSON(db, root); err != nil {
		return nil, err
	}
	var payload string
	err = db.QueryRow(`SELECT payload FROM task_states WHERE task_id = ?`, taskID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("longhorizon: task state not found: %s", taskID)
	}
	if err != nil {
		return nil, err
	}
	var state TaskState
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return nil, err
	}
	state.Carryover = ClipCarryover(state.Carryover)
	return &state, nil
}

// findIncompleteTaskDB answers the resume lookup with one indexed query
// instead of reading and parsing every per-task JSON file.
func findIncompleteTaskDB(root, ownerID string) (*TaskState, error) {
	base := filepath.Join(root, "horizon")
	if _, err := os.Stat(base); err != nil {
		return nil, nil
	}
	db, err := openHorizonDB(root, false)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer db.Close()
	if err := importHorizonJSON(db, root); err != nil {
		return nil, err
	}
	var payload string
	err = db.QueryRow(`SELECT payload FROM task_states
		WHERE owner_id = ? AND resumable = 1
		ORDER BY updated_at DESC, task_id DESC LIMIT 1`, ownerID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state TaskState
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return nil, err
	}
	state.Carryover = ClipCarryover(state.Carryover)
	return &state, nil
}

func removeTaskStateJSON(jsonPath string) {
	if err := os.Remove(jsonPath); err != nil && !os.IsNotExist(err) {
		return
	}
	_ = os.Remove(jsonPath + ".tmp")
}
