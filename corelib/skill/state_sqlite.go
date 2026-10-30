package skill

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/fileutil"

	_ "modernc.org/sqlite"
)

const skillStateLegacyMetaKey = "legacy_import"

func skillStateDBPath(skillDir string) string {
	return filepath.Join(skillDir, stateDir, stateDBFile)
}

func skillStateJSONPath(skillDir string) string {
	return filepath.Join(skillDir, stateDir, stateFile)
}

func openSkillStateDB(path string, create bool) (*sql.DB, error) {
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
	if err := ensureSkillStateSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func ensureSkillStateSchema(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS skill_state (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  current_phase TEXT NOT NULL DEFAULT '',
  next_prompt TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS skill_state_vars (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS skill_state_history (
  position INTEGER PRIMARY KEY,
  timestamp TEXT NOT NULL DEFAULT '',
  phase TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL DEFAULT '',
  success INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS skill_state_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);`)
	return err
}

func skillStateImported(db *sql.DB) (bool, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM skill_state_meta WHERE key = ?`, skillStateLegacyMetaKey).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value == "1", nil
}

func loadSkillState(skillDir string) (*SkillState, error) {
	dbPath := skillStateDBPath(skillDir)
	jsonPath := skillStateJSONPath(skillDir)
	dbExists := fileExists(dbPath)
	if !dbExists && !fileExists(jsonPath) {
		_ = os.Remove(jsonPath + ".tmp")
		return &SkillState{}, nil
	}
	db, err := openSkillStateDB(dbPath, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := importSkillStateJSON(db, jsonPath); err != nil {
		return nil, err
	}
	return readSkillState(db)
}

func saveSkillState(skillDir string, state *SkillState) error {
	db, err := openSkillStateDB(skillStateDBPath(skillDir), true)
	if err != nil {
		return err
	}
	defer db.Close()
	jsonPath := skillStateJSONPath(skillDir)
	seal := skillLegacySeal(db, jsonPath)
	if err := writeSkillState(db, state, seal); err != nil {
		return err
	}
	if seal {
		removeSkillStateJSON(jsonPath)
	}
	return nil
}

// skillLegacySeal reports whether this save may record the import and remove
// state.json. A readable file that has not been copied stays, so the next
// open can still import it.
func skillLegacySeal(db *sql.DB, jsonPath string) bool {
	done, err := skillStateImported(db)
	if err == nil && done {
		return true
	}
	return !skillLegacyPending(jsonPath)
}

func skillLegacyPending(path string) bool {
	return fileutil.LegacyImportPending(path, func(data []byte) bool {
		_, err := unmarshalSkillState(data)
		return err == nil
	})
}

func unmarshalSkillState(data []byte) (SkillState, error) {
	var state SkillState
	err := json.Unmarshal(data, &state)
	return state, err
}

func importSkillStateJSON(db *sql.DB, jsonPath string) error {
	done, err := skillStateImported(db)
	if err != nil || done {
		return err
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		if os.IsNotExist(err) {
			return markSkillStateImported(db)
		}
		// Unreadable legacy JSON must not replace rows already stored.
		_ = os.Remove(jsonPath + ".tmp")
		return nil
	}
	state, err := unmarshalSkillState(data)
	if err != nil {
		// A torn legacy file must not replace rows already stored.
		_ = os.Remove(jsonPath + ".tmp")
		return nil
	}
	if err := writeSkillState(db, &state, true); err != nil {
		return err
	}
	removeSkillStateJSON(jsonPath)
	return nil
}

func writeSkillState(db *sql.DB, state *SkillState, markImported bool) error {
	if state == nil {
		state = &SkillState{}
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM skill_state_vars`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM skill_state_history`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`INSERT INTO skill_state(id, current_phase, next_prompt, updated_at) VALUES(1, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET current_phase = excluded.current_phase, next_prompt = excluded.next_prompt, updated_at = excluded.updated_at`,
		state.CurrentPhase, state.NextPrompt, state.UpdatedAt); err != nil {
		_ = tx.Rollback()
		return err
	}
	varStmt, err := tx.Prepare(`INSERT INTO skill_state_vars(key, value) VALUES(?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer varStmt.Close()
	for key, value := range state.Vars {
		if _, err := varStmt.Exec(key, value); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	histStmt, err := tx.Prepare(`INSERT INTO skill_state_history(position, timestamp, phase, summary, success) VALUES(?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer histStmt.Close()
	for i, entry := range state.History {
		success := 0
		if entry.Success {
			success = 1
		}
		if _, err := histStmt.Exec(i, entry.Timestamp, entry.Phase, entry.Summary, success); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if markImported {
		if _, err := tx.Exec(`INSERT INTO skill_state_meta(key, value) VALUES(?, '1') ON CONFLICT(key) DO UPDATE SET value = excluded.value`, skillStateLegacyMetaKey); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func markSkillStateImported(db *sql.DB) error {
	_, err := db.Exec(`INSERT INTO skill_state_meta(key, value) VALUES(?, '1') ON CONFLICT(key) DO UPDATE SET value = excluded.value`, skillStateLegacyMetaKey)
	return err
}

func readSkillState(db *sql.DB) (*SkillState, error) {
	state := &SkillState{}
	err := db.QueryRow(`SELECT current_phase, next_prompt, updated_at FROM skill_state WHERE id = 1`).Scan(&state.CurrentPhase, &state.NextPrompt, &state.UpdatedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	rows, err := db.Query(`SELECT key, value FROM skill_state_vars ORDER BY key`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			_ = rows.Close()
			return nil, err
		}
		state.SetVar(key, value)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	history, err := db.Query(`SELECT timestamp, phase, summary, success FROM skill_state_history ORDER BY position`)
	if err != nil {
		return nil, err
	}
	defer history.Close()
	for history.Next() {
		var entry StateHistoryEntry
		var success int
		if err := history.Scan(&entry.Timestamp, &entry.Phase, &entry.Summary, &success); err != nil {
			return nil, err
		}
		entry.Success = success != 0
		state.History = append(state.History, entry)
	}
	return state, history.Err()
}

func removeSkillStateJSON(jsonPath string) {
	if err := os.Remove(jsonPath); err != nil && !os.IsNotExist(err) {
		return
	}
	_ = os.Remove(jsonPath + ".tmp")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
