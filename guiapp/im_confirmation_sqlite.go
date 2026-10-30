package guiapp

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/fileutil"

	_ "modernc.org/sqlite"
)

func confirmationStorePaths(storePath string) (jsonPath, dbPath string) {
	storePath = strings.TrimSpace(storePath)
	if storePath == "" {
		return "", ""
	}
	info, err := os.Stat(storePath)
	if err == nil && info.IsDir() {
		return "", filepath.Join(storePath, "confirmations.db")
	}
	if strings.EqualFold(filepath.Ext(storePath), ".json") {
		return storePath, strings.TrimSuffix(storePath, filepath.Ext(storePath)) + ".db"
	}
	return "", storePath
}

func cloneConfirmations(items map[string]*pendingConfirmation) map[string]*pendingConfirmation {
	out := make(map[string]*pendingConfirmation, len(items))
	for userID, item := range items {
		if item == nil || strings.TrimSpace(userID) == "" {
			continue
		}
		clone := *item
		clone.Status = normalizeConfirmationStatus(clone.Status.String())
		clone.TargetPaths = append([]string(nil), item.TargetPaths...)
		clone.PlannedActions = append([]string(nil), item.PlannedActions...)
		clone.RiskFlags = append([]string(nil), item.RiskFlags...)
		clone.RevisionHints = append([]string(nil), item.RevisionHints...)
		clone.WorkflowGoals = append([]string(nil), item.WorkflowGoals...)
		clone.WorkflowConstraints = append([]string(nil), item.WorkflowConstraints...)
		out[userID] = &clone
	}
	return out
}

func loadConfirmations(storePath string) (map[string]*pendingConfirmation, error) {
	jsonPath, dbPath := confirmationStorePaths(storePath)
	if dbPath == "" {
		return nil, nil
	}
	if !fileExists(dbPath) && (jsonPath == "" || !fileExists(jsonPath)) {
		return nil, nil
	}
	db, err := openConfirmationDB(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	importErr := importConfirmationJSON(db, jsonPath)
	items, err := readConfirmations(db)
	if err != nil {
		return nil, err
	}
	if importErr != nil && len(items) == 0 {
		return nil, importErr
	}
	return items, nil
}

func saveConfirmations(storePath string, items map[string]*pendingConfirmation) error {
	_, dbPath := confirmationStorePaths(storePath)
	if dbPath == "" {
		return nil
	}
	db, err := openConfirmationDB(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	jsonPath, _ := confirmationStorePaths(storePath)
	seal := confirmationLegacySeal(db, jsonPath)
	if err := writeConfirmations(db, items, seal); err != nil {
		return err
	}
	if seal && jsonPath != "" {
		_ = os.Remove(jsonPath)
		_ = os.Remove(jsonPath + ".tmp")
	}
	return nil
}

// confirmationLegacySeal reports whether this save may record the import and
// remove the pre-migration file. A readable file that has not been copied
// stays, so the next open can still import it.
func confirmationLegacySeal(db *sql.DB, jsonPath string) bool {
	var marker string
	err := db.QueryRow(`SELECT value FROM confirmation_meta WHERE key = 'legacy_import'`).Scan(&marker)
	if err == nil && marker == "1" {
		return true
	}
	return !confirmationLegacyPending(jsonPath)
}

func confirmationLegacyPending(path string) bool {
	return fileutil.LegacyImportPending(path, func(data []byte) bool {
		_, err := unmarshalConfirmationSnapshot(data)
		return err == nil
	})
}

func unmarshalConfirmationSnapshot(data []byte) (confirmationSnapshot, error) {
	var snapshot confirmationSnapshot
	err := json.Unmarshal(data, &snapshot)
	return snapshot, err
}

func openConfirmationDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
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
CREATE TABLE IF NOT EXISTS confirmations (
  user_id TEXT PRIMARY KEY,
  id TEXT NOT NULL DEFAULT '',
  original_text TEXT NOT NULL DEFAULT '',
  resume_text TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL DEFAULT '',
  task_type TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT '',
  last_project_path TEXT NOT NULL DEFAULT '',
  enhanced_summary TEXT NOT NULL DEFAULT '',
  enhanced_instruction TEXT NOT NULL DEFAULT '',
  workflow_type TEXT NOT NULL DEFAULT '',
  workflow_summary TEXT NOT NULL DEFAULT '',
  workflow_confidence REAL NOT NULL DEFAULT 0,
  workflow_start_reply TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS confirmation_lists (
  user_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  position INTEGER NOT NULL,
  value TEXT NOT NULL,
  PRIMARY KEY (user_id, kind, position)
);
CREATE TABLE IF NOT EXISTS confirmation_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func importConfirmationJSON(db *sql.DB, jsonPath string) error {
	var marker string
	err := db.QueryRow(`SELECT value FROM confirmation_meta WHERE key = 'legacy_import'`).Scan(&marker)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if marker == "1" {
		return nil
	}
	if jsonPath == "" {
		return markConfirmationsImported(db)
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		if os.IsNotExist(err) {
			return markConfirmationsImported(db)
		}
		return nil
	}
	snapshot, err := unmarshalConfirmationSnapshot(data)
	if err != nil {
		return err
	}
	if err := writeConfirmations(db, snapshot.Items, true); err != nil {
		return err
	}
	_ = os.Remove(jsonPath)
	_ = os.Remove(jsonPath + ".tmp")
	return nil
}

func markConfirmationsImported(db *sql.DB) error {
	_, err := db.Exec(`INSERT INTO confirmation_meta(key, value) VALUES('legacy_import','1') ON CONFLICT(key) DO UPDATE SET value = excluded.value`)
	return err
}

func writeConfirmations(db *sql.DB, items map[string]*pendingConfirmation, mark bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM confirmation_lists`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM confirmations`); err != nil {
		_ = tx.Rollback()
		return err
	}
	itemStmt, err := tx.Prepare(`INSERT INTO confirmations(
		user_id, id, original_text, resume_text, summary, task_type, status, created_at, updated_at,
		last_project_path, enhanced_summary, enhanced_instruction, workflow_type, workflow_summary,
		workflow_confidence, workflow_start_reply) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer itemStmt.Close()
	listStmt, err := tx.Prepare(`INSERT INTO confirmation_lists(user_id, kind, position, value) VALUES(?,?,?,?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer listStmt.Close()
	for userID, item := range items {
		if item == nil || strings.TrimSpace(userID) == "" {
			continue
		}
		if _, err := itemStmt.Exec(
			userID, item.ID, item.OriginalText, item.ResumeText, item.Summary, item.TaskType, item.Status.String(),
			formatConfirmationTime(item.CreatedAt), formatConfirmationTime(item.UpdatedAt), item.LastProjectPath,
			item.EnhancedSummary, item.EnhancedInstruction, item.WorkflowType, item.WorkflowSummary,
			item.WorkflowConfidence, item.WorkflowStartReply,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
		lists := []struct {
			kind   string
			values []string
		}{
			{"target_paths", item.TargetPaths},
			{"planned_actions", item.PlannedActions},
			{"risk_flags", item.RiskFlags},
			{"revision_hints", item.RevisionHints},
			{"workflow_goals", item.WorkflowGoals},
			{"workflow_constraints", item.WorkflowConstraints},
		}
		for _, list := range lists {
			for i, value := range list.values {
				if _, err := listStmt.Exec(userID, list.kind, i, value); err != nil {
					_ = tx.Rollback()
					return err
				}
			}
		}
	}
	if mark {
		if _, err := tx.Exec(`INSERT INTO confirmation_meta(key, value) VALUES('legacy_import','1') ON CONFLICT(key) DO UPDATE SET value = excluded.value`); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func readConfirmations(db *sql.DB) (map[string]*pendingConfirmation, error) {
	rows, err := db.Query(`SELECT user_id, id, original_text, resume_text, summary, task_type, status, created_at, updated_at,
		last_project_path, enhanced_summary, enhanced_instruction, workflow_type, workflow_summary, workflow_confidence, workflow_start_reply
		FROM confirmations`)
	if err != nil {
		return nil, err
	}
	items := map[string]*pendingConfirmation{}
	for rows.Next() {
		var item pendingConfirmation
		var userID, status, created, updated string
		if err := rows.Scan(&userID, &item.ID, &item.OriginalText, &item.ResumeText, &item.Summary, &item.TaskType, &status, &created, &updated,
			&item.LastProjectPath, &item.EnhancedSummary, &item.EnhancedInstruction, &item.WorkflowType, &item.WorkflowSummary, &item.WorkflowConfidence, &item.WorkflowStartReply); err != nil {
			_ = rows.Close()
			return nil, err
		}
		item.UserID = userID
		item.Status = normalizeConfirmationStatus(status)
		item.CreatedAt = parseConfirmationTime(created)
		item.UpdatedAt = parseConfirmationTime(updated)
		items[userID] = &item
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	listRows, err := db.Query(`SELECT user_id, kind, value FROM confirmation_lists ORDER BY user_id, kind, position`)
	if err != nil {
		return nil, err
	}
	defer listRows.Close()
	for listRows.Next() {
		var userID, kind, value string
		if err := listRows.Scan(&userID, &kind, &value); err != nil {
			return nil, err
		}
		item := items[userID]
		if item == nil {
			continue
		}
		switch kind {
		case "target_paths":
			item.TargetPaths = append(item.TargetPaths, value)
		case "planned_actions":
			item.PlannedActions = append(item.PlannedActions, value)
		case "risk_flags":
			item.RiskFlags = append(item.RiskFlags, value)
		case "revision_hints":
			item.RevisionHints = append(item.RevisionHints, value)
		case "workflow_goals":
			item.WorkflowGoals = append(item.WorkflowGoals, value)
		case "workflow_constraints":
			item.WorkflowConstraints = append(item.WorkflowConstraints, value)
		}
	}
	return items, listRows.Err()
}

func formatConfirmationTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseConfirmationTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed
	}
	parsed, _ := time.Parse(time.RFC3339, value)
	return parsed
}
