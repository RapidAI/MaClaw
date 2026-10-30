package tool

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func usageDBPath(path string) string {
	ext := filepath.Ext(path)
	if strings.EqualFold(ext, ".json") {
		return strings.TrimSuffix(path, ext) + ".db"
	}
	return path + ".db"
}

const usageSQLiteSchema = `
CREATE TABLE IF NOT EXISTS usage_records (
    id INTEGER PRIMARY KEY,
    seq INTEGER NOT NULL,
    tool_name TEXT NOT NULL DEFAULT '',
    success INTEGER NOT NULL DEFAULT 0,
    follow_up TEXT NOT NULL DEFAULT '',
    timestamp TEXT NOT NULL DEFAULT '',
    task_type TEXT NOT NULL DEFAULT '',
    error_class TEXT NOT NULL DEFAULT '',
    retry_count INTEGER NOT NULL DEFAULT 0,
    recovery_tool TEXT NOT NULL DEFAULT '',
    final_outcome TEXT NOT NULL DEFAULT '',
    tokens_used INTEGER NOT NULL DEFAULT 0,
    query_text TEXT NOT NULL DEFAULT '',
    query_tokens TEXT NOT NULL DEFAULT '[]',
    tool_sequence TEXT NOT NULL DEFAULT '[]',
    run_args TEXT NOT NULL DEFAULT '{}'
);
CREATE TABLE IF NOT EXISTS usage_invalidations (
    tool_name TEXT PRIMARY KEY,
    state_json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS usage_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`

func (t *UsageTracker) openDB() error {
	dbPath := usageDBPath(t.path)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	if testing.Testing() {
		db.SetMaxIdleConns(0)
	} else {
		db.SetMaxIdleConns(1)
	}
	if _, err := db.Exec(usageSQLiteSchema); err != nil {
		_ = db.Close()
		return err
	}
	// The live ring is the search set. These indexes only slowed inserts.
	for _, stmt := range []string{
		`DROP INDEX IF EXISTS idx_usage_tool_time`,
		`DROP INDEX IF EXISTS idx_usage_recovery`,
		`DROP INDEX IF EXISTS idx_usage_task`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			return err
		}
	}
	t.db = db
	return nil
}

func (t *UsageTracker) loadFromDB() error {
	rows, err := t.db.Query(`SELECT id, seq, tool_name, success, follow_up, timestamp, task_type, error_class,
		retry_count, recovery_tool, final_outcome, tokens_used, query_tokens, tool_sequence, run_args
		FROM usage_records ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var records []UsageRecord
	var maxSeq uint64
	for rows.Next() {
		rec, err := scanUsageRecord(rows)
		if err != nil {
			return err
		}
		if rec.seq > maxSeq {
			maxSeq = rec.seq
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	invRows, err := t.db.Query(`SELECT tool_name, state_json FROM usage_invalidations`)
	if err != nil {
		return err
	}
	defer invRows.Close()
	invalidations := map[string]ToolInvalidationState{}
	for invRows.Next() {
		var name, raw string
		if err := invRows.Scan(&name, &raw); err != nil {
			return err
		}
		var state ToolInvalidationState
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			continue
		}
		invalidations[name] = state
	}
	if err := invRows.Err(); err != nil {
		return err
	}
	invRaw, err := json.Marshal(invalidations)
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.records = records
	t.invalidations = invalidations
	t.savedInvalidations = invRaw
	t.dropped = nil
	if t.seq < maxSeq {
		t.seq = maxSeq
	}
	t.rebuildToolIndexLocked()
	t.mu.Unlock()
	return nil
}

func scanUsageRecord(row interface{ Scan(...any) error }) (UsageRecord, error) {
	var rec UsageRecord
	var success int
	var stamp, tokens, sequence, args string
	if err := row.Scan(&rec.persistID, &rec.seq, &rec.ToolName, &success, &rec.FollowUp, &stamp, &rec.TaskType, &rec.ErrorClass,
		&rec.RetryCount, &rec.RecoveryTool, &rec.FinalOutcome, &rec.TokensUsed, &tokens, &sequence, &args); err != nil {
		return UsageRecord{}, err
	}
	rec.Success = success != 0
	rec.Timestamp = parseUsageTime(stamp)
	_ = json.Unmarshal([]byte(tokens), &rec.QueryTokens)
	_ = json.Unmarshal([]byte(sequence), &rec.ToolSequence)
	_ = json.Unmarshal([]byte(args), &rec.RunArgs)
	return rec, nil
}

func (t *UsageTracker) writeDBSnapshot() error {
	return t.writeDBSnapshotMode(false)
}

// writeDBSnapshotMode writes the live ring. replace is the one-shot JSON import:
// it deletes the previous rows and marks the import in the same transaction.
func (t *UsageTracker) writeDBSnapshotMode(replace bool) error {
	t.mu.RLock()
	dropped := append([]int64(nil), t.dropped...)
	var insert []UsageRecord
	for _, rec := range t.records {
		if replace || (rec.persistID == 0 && rec.seq > 0) {
			insert = append(insert, rec)
		}
	}
	invalidations := copyInvalidations(t.invalidations)
	invRaw, _ := json.Marshal(invalidations)
	invSame := !replace && bytes.Equal(invRaw, t.savedInvalidations)
	t.mu.RUnlock()

	tx, err := t.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if replace {
		if _, err := tx.Exec(`DELETE FROM usage_records`); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM usage_invalidations`); err != nil {
			return err
		}
	} else if err := deleteUsageIDs(tx, dropped); err != nil {
		return err
	}
	assigned := make(map[uint64]int64, len(insert))
	var stmt *sql.Stmt
	if len(insert) > 0 {
		stmt, err = tx.Prepare(`INSERT INTO usage_records(
			seq, tool_name, success, follow_up, timestamp, task_type, error_class, retry_count,
			recovery_tool, final_outcome, tokens_used, query_text, query_tokens, tool_sequence, run_args
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
	}
	for _, rec := range insert {
		tokens, _ := json.Marshal(rec.QueryTokens)
		sequence, _ := json.Marshal(rec.ToolSequence)
		args, _ := json.Marshal(rec.RunArgs)
		if tokens == nil {
			tokens = []byte("[]")
		}
		if sequence == nil {
			sequence = []byte("[]")
		}
		if args == nil {
			args = []byte("{}")
		}
		success := 0
		if rec.Success {
			success = 1
		}
		res, err := stmt.Exec(
			rec.seq, rec.ToolName, success, rec.FollowUp, formatUsageTime(rec.Timestamp), rec.TaskType, rec.ErrorClass, rec.RetryCount,
			rec.RecoveryTool, rec.FinalOutcome, rec.TokensUsed, strings.Join(rec.QueryTokens, " "), string(tokens), string(sequence), string(args))
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		assigned[rec.seq] = id
	}
	if !invSame {
		if _, err := tx.Exec(`DELETE FROM usage_invalidations`); err != nil {
			return err
		}
		for name, state := range invalidations {
			raw, err := json.Marshal(state)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO usage_invalidations(tool_name, state_json) VALUES(?,?)`, name, string(raw)); err != nil {
				return err
			}
		}
	}
	if replace {
		if _, err := tx.Exec(`INSERT INTO usage_meta(key, value) VALUES('legacy_import','1') ON CONFLICT(key) DO UPDATE SET value=excluded.value`); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	t.mu.Lock()
	removed := make(map[int64]struct{}, len(dropped))
	for _, id := range dropped {
		removed[id] = struct{}{}
	}
	if len(removed) > 0 {
		kept := t.dropped[:0]
		for _, id := range t.dropped {
			if _, ok := removed[id]; !ok {
				kept = append(kept, id)
			}
		}
		t.dropped = kept
	}
	for i := range t.records {
		if t.records[i].persistID != 0 {
			continue
		}
		id, ok := assigned[t.records[i].seq]
		if !ok {
			continue
		}
		t.records[i].persistID = id
		delete(assigned, t.records[i].seq)
	}
	for _, id := range assigned {
		t.dropped = append(t.dropped, id)
	}
	if !invSame {
		t.savedInvalidations = invRaw
	}
	t.mu.Unlock()
	return nil
}

func deleteUsageIDs(tx *sql.Tx, ids []int64) error {
	const batch = 400
	for len(ids) > 0 {
		n := len(ids)
		if n > batch {
			n = batch
		}
		args := make([]any, n)
		for i := 0; i < n; i++ {
			args[i] = ids[i]
		}
		marks := strings.TrimRight(strings.Repeat("?,", n), ",")
		if _, err := tx.Exec(`DELETE FROM usage_records WHERE id IN (`+marks+`)`, args...); err != nil {
			return err
		}
		ids = ids[n:]
	}
	return nil
}

// Search reads the live ring by tool name and a text fragment. The fragment
// matches query tokens, task type, error class, follow-up, and final outcome.
// The database copy lags the debounced save, so a trimmed row can still be
// stored while a new row is not.
func (t *UsageTracker) Search(toolName, text string, limit int) []UsageRecord {
	if t == nil {
		return nil
	}
	if limit <= 0 {
		limit = 20
	}
	toolName = normalizeUsageToolName(toolName)
	text = strings.TrimSpace(text)
	needle := strings.ToLower(text)
	t.mu.RLock()
	defer t.mu.RUnlock()
	var out []UsageRecord
	for i := len(t.records) - 1; i >= 0 && len(out) < limit; i-- {
		rec := t.records[i]
		if toolName != "" && rec.ToolName != toolName {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(strings.Join(rec.QueryTokens, " ")+" "+rec.TaskType+" "+rec.ErrorClass+" "+rec.FollowUp+" "+rec.FinalOutcome), needle) {
			continue
		}
		out = append(out, rec)
	}
	return out
}

func (t *UsageTracker) usageMeta(key string) (string, error) {
	var value string
	err := t.db.QueryRow(`SELECT value FROM usage_meta WHERE key=?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (t *UsageTracker) setUsageMeta(key, value string) error {
	_, err := t.db.Exec(`INSERT INTO usage_meta(key, value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func formatUsageTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseUsageTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed
	}
	parsed, _ := time.Parse(time.RFC3339, value)
	return parsed
}
