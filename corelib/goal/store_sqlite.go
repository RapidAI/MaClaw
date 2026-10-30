package goal

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func (s *Store) openDB() error {
	db, err := sql.Open("sqlite", s.goalDBPath()+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	if testing.Testing() {
		db.SetMaxIdleConns(0)
	} else {
		db.SetMaxIdleConns(1)
	}
	if _, err := db.Exec(goalSQLiteSchema); err != nil {
		_ = db.Close()
		return err
	}
	if err := ensureGoalColumns(db); err != nil {
		_ = db.Close()
		return err
	}
	s.db = db
	return nil
}

func ensureGoalColumns(db *sql.DB) error {
	have := map[string]bool{}
	rows, err := db.Query(`PRAGMA table_info(goals)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			_ = rows.Close()
			return err
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, col := range []struct{ name, stmt string }{
		{"state_rev", `ALTER TABLE goals ADD COLUMN state_rev INTEGER NOT NULL DEFAULT 0`},
		{"text_rev", `ALTER TABLE goals ADD COLUMN text_rev INTEGER NOT NULL DEFAULT 0`},
	} {
		if have[col.name] {
			continue
		}
		if _, err := db.Exec(col.stmt); err != nil {
			return err
		}
	}
	for _, stmt := range []string{
		`DROP INDEX IF EXISTS idx_goals_status`,
		`DROP INDEX IF EXISTS idx_goals_project`,
		`DROP INDEX IF EXISTS idx_goals_updated`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) goalDBPath() string {
	return filepath.Join(s.dataDir, "goals.db")
}

const goalSQLiteSchema = `
CREATE TABLE IF NOT EXISTS goals (
    user_id TEXT PRIMARY KEY,
    goal_id TEXT NOT NULL DEFAULT '',
    objective TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    project_path TEXT NOT NULL DEFAULT '',
    phase TEXT NOT NULL DEFAULT '',
    token_budget INTEGER NOT NULL DEFAULT 0,
    token_window INTEGER NOT NULL DEFAULT 0,
    max_turns INTEGER NOT NULL DEFAULT 0,
    tokens_used INTEGER NOT NULL DEFAULT 0,
    time_used_seconds INTEGER NOT NULL DEFAULT 0,
    turns_used INTEGER NOT NULL DEFAULT 0,
    consecutive_no_tool_turns INTEGER NOT NULL DEFAULT 0,
    clock_hold_seconds INTEGER NOT NULL DEFAULT 0,
    understanding TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    acceptance_json TEXT NOT NULL DEFAULT '[]',
    steps_json TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT '',
    stopped_at TEXT NOT NULL DEFAULT '',
    state_rev INTEGER NOT NULL DEFAULT 0,
    text_rev INTEGER NOT NULL DEFAULT 0
);
CREATE VIRTUAL TABLE IF NOT EXISTS goals_fts USING fts5(
    user_id UNINDEXED,
    objective,
    understanding,
    summary,
    project_path,
    tokenize='trigram'
);
CREATE TABLE IF NOT EXISTS goal_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`

func (s *Store) loadFromDB() {
	rows, err := s.db.Query(`SELECT user_id, goal_id, objective, status, project_path, phase,
		token_budget, token_window, max_turns, tokens_used, time_used_seconds, turns_used,
		consecutive_no_tool_turns, clock_hold_seconds, understanding, summary,
		acceptance_json, steps_json, created_at, updated_at, stopped_at, state_rev, text_rev FROM goals`)
	if err != nil {
		fmt.Printf("[goal] load: %v\n", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		g, err := scanGoal(rows)
		if err != nil || g.UserID == "" {
			continue
		}
		s.goals[g.UserID] = g
	}
	if err := rows.Err(); err != nil {
		fmt.Printf("[goal] load: %v\n", err)
	}
}

type goalRow interface {
	Scan(dest ...any) error
}

func scanGoal(row goalRow) (*Goal, error) {
	var g Goal
	var status, phase, acceptance, steps, created, updated, stopped string
	if err := row.Scan(
		&g.UserID, &g.GoalID, &g.Objective, &status, &g.ProjectPath, &phase,
		&g.TokenBudget, &g.TokenWindow, &g.MaxTurns, &g.TokensUsed, &g.TimeUsedSeconds, &g.TurnsUsed,
		&g.ConsecutiveNoToolTurns, &g.ClockHoldSeconds, &g.Understanding, &g.Summary,
		&acceptance, &steps, &created, &updated, &stopped,
		&g.StateRev, &g.TextRev,
	); err != nil {
		return nil, err
	}
	g.Status = Status(status)
	g.Phase = Phase(phase)
	g.CreatedAt = parseGoalTime(created)
	g.UpdatedAt = parseGoalTime(updated)
	g.StoppedAt = parseGoalTime(stopped)
	if acceptance != "" && acceptance != "null" {
		_ = json.Unmarshal([]byte(acceptance), &g.AcceptanceCriteria)
	}
	if steps != "" && steps != "null" {
		_ = json.Unmarshal([]byte(steps), &g.Steps)
	}
	return &g, nil
}

func (s *Store) importLegacyJSON() {
	done, err := s.goalMeta("goals_legacy_import")
	if err != nil || done == "1" {
		return
	}
	before := make(map[string]struct{}, len(s.goals))
	for id := range s.goals {
		before[id] = struct{}{}
	}
	clean := s.loadLegacyJSONFiles(false)
	for id, g := range s.goals {
		if _, ok := before[id]; ok || g == nil {
			continue
		}
		if err := s.upsertGoal(g); err != nil {
			fmt.Printf("[goal] legacy import %s: %v\n", id, err)
			return
		}
	}
	if !clean {
		return
	}
	_ = s.setGoalMeta("goals_legacy_import", "1")
}

type goalExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// updateGoalState writes status, phase, summary, counters, and times.
// A slower snapshot with an older state revision does not land.
// wrote is false when the row is missing or a newer revision is already stored.
func (s *Store) updateGoalState(g *Goal) (bool, error) {
	if s == nil || s.db == nil {
		return false, nil
	}
	return execGoalState(s.db, g)
}

func execGoalState(db goalExecer, g *Goal) (bool, error) {
	if db == nil || g == nil || strings.TrimSpace(g.UserID) == "" || strings.TrimSpace(g.GoalID) == "" {
		return false, nil
	}
	res, err := db.Exec(`UPDATE goals SET
		goal_id=?, status=?, phase=?, summary=?,
		token_budget=?, token_window=?, max_turns=?,
		tokens_used=?, time_used_seconds=?, turns_used=?,
		consecutive_no_tool_turns=?, clock_hold_seconds=?, updated_at=?, stopped_at=?,
		state_rev=?
		WHERE user_id=? AND goal_id=? AND state_rev<=?`,
		g.GoalID, string(g.Status), string(g.Phase), g.Summary,
		g.TokenBudget, g.TokenWindow, g.MaxTurns,
		g.TokensUsed, g.TimeUsedSeconds, g.TurnsUsed, g.ConsecutiveNoToolTurns,
		g.ClockHoldSeconds, formatGoalTime(g.UpdatedAt), formatGoalTime(g.StoppedAt), g.StateRev,
		g.UserID, g.GoalID, g.StateRev)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// updateGoalText writes the objective and the plan. Counter columns stay put,
// so a usage tick that already landed is not rolled back by an older plan write.
func (s *Store) updateGoalText(g *Goal) (bool, error) {
	if s == nil || s.db == nil {
		return false, nil
	}
	return execGoalText(s.db, g)
}

func execGoalText(db goalExecer, g *Goal) (bool, error) {
	if db == nil || g == nil || strings.TrimSpace(g.UserID) == "" || strings.TrimSpace(g.GoalID) == "" {
		return false, nil
	}
	acceptance, steps := goalNestedJSON(g)
	res, err := db.Exec(`UPDATE goals SET
		objective=?, understanding=?, project_path=?, acceptance_json=?, steps_json=?, text_rev=?
		WHERE user_id=? AND goal_id=? AND text_rev<=?`,
		g.Objective, g.Understanding, g.ProjectPath, acceptance, steps, g.TextRev,
		g.UserID, g.GoalID, g.TextRev)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// persistGoalSnapshot applies the text and state groups in one transaction.
// A missing row is inserted only while g is still the in-memory goal.
func (s *Store) persistGoalSnapshot(g *Goal) error {
	if s == nil || s.db == nil || g == nil {
		return nil
	}
	textWrote, stateWrote, textErr, stateErr := s.commitGoalSnapshot(g)
	if textErr != nil || stateErr != nil || (!textWrote && !stateWrote) {
		if err := s.insertGoalIfAbsent(g); err != nil {
			return err
		}
	}
	if textErr != nil {
		return textErr
	}
	return stateErr
}

// commitGoalSnapshot commits both column groups before the caller takes the
// store lock. Holding that lock across an open transaction deadlocks Clear,
// which deletes the row on the same connection.
func (s *Store) commitGoalSnapshot(g *Goal) (bool, bool, error, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, false, err, nil
	}
	defer tx.Rollback()
	textWrote, textErr := execGoalText(tx, g)
	if textErr != nil {
		return false, false, textErr, nil
	}
	stateWrote, stateErr := execGoalState(tx, g)
	if stateErr != nil {
		return false, false, nil, stateErr
	}
	if err := tx.Commit(); err != nil {
		return false, false, err, nil
	}
	return textWrote, stateWrote, nil, nil
}

// goalCurrent reports whether g is still this user's in-memory goal.
// The caller holds s.mu.
func (s *Store) goalCurrent(g *Goal) bool {
	if s == nil || g == nil || strings.TrimSpace(g.GoalID) == "" {
		return false
	}
	cur := s.goals[g.UserID]
	return cur != nil && cur.GoalID == g.GoalID
}

func (s *Store) insertGoalIfAbsent(g *Goal) error {
	if s == nil || s.db == nil || g == nil || strings.TrimSpace(g.UserID) == "" || strings.TrimSpace(g.GoalID) == "" {
		return nil
	}
	acceptance, steps := goalNestedJSON(g)
	// The read lock stays across the insert. Clear takes the write lock before
	// it deletes the row, so a snapshot of a cleared goal cannot recreate it.
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.goalCurrent(g) {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO goals(
		user_id, goal_id, objective, status, project_path, phase,
		token_budget, token_window, max_turns, tokens_used, time_used_seconds, turns_used,
		consecutive_no_tool_turns, clock_hold_seconds, understanding, summary,
		acceptance_json, steps_json, created_at, updated_at, stopped_at, state_rev, text_rev
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(user_id) DO NOTHING`,
		g.UserID, g.GoalID, g.Objective, string(g.Status), g.ProjectPath, string(g.Phase),
		g.TokenBudget, g.TokenWindow, g.MaxTurns, g.TokensUsed, g.TimeUsedSeconds, g.TurnsUsed,
		g.ConsecutiveNoToolTurns, g.ClockHoldSeconds, g.Understanding, g.Summary,
		acceptance, steps, formatGoalTime(g.CreatedAt), formatGoalTime(g.UpdatedAt), formatGoalTime(g.StoppedAt),
		g.StateRev, g.TextRev)
	return err
}

func goalNestedJSON(g *Goal) (string, string) {
	acceptance := "[]"
	if g != nil && len(g.AcceptanceCriteria) > 0 {
		if raw, err := json.Marshal(g.AcceptanceCriteria); err == nil {
			acceptance = string(raw)
		}
	}
	steps := "[]"
	if g != nil && len(g.Steps) > 0 {
		if raw, err := json.Marshal(g.Steps); err == nil {
			steps = string(raw)
		}
	}
	return acceptance, steps
}

func (s *Store) upsertGoal(g *Goal) error {
	if s == nil || s.db == nil || g == nil || strings.TrimSpace(g.UserID) == "" || strings.TrimSpace(g.GoalID) == "" {
		return nil
	}
	acceptance, steps := goalNestedJSON(g)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.goalCurrent(g) {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO goals(
		user_id, goal_id, objective, status, project_path, phase,
		token_budget, token_window, max_turns, tokens_used, time_used_seconds, turns_used,
		consecutive_no_tool_turns, clock_hold_seconds, understanding, summary,
		acceptance_json, steps_json, created_at, updated_at, stopped_at, state_rev, text_rev
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(user_id) DO UPDATE SET
		goal_id=excluded.goal_id, objective=excluded.objective, status=excluded.status,
		project_path=excluded.project_path, phase=excluded.phase,
		token_budget=excluded.token_budget, token_window=excluded.token_window,
		max_turns=excluded.max_turns, tokens_used=excluded.tokens_used,
		time_used_seconds=excluded.time_used_seconds, turns_used=excluded.turns_used,
		consecutive_no_tool_turns=excluded.consecutive_no_tool_turns,
		clock_hold_seconds=excluded.clock_hold_seconds, understanding=excluded.understanding,
		summary=excluded.summary, acceptance_json=excluded.acceptance_json,
		steps_json=excluded.steps_json, created_at=excluded.created_at,
		updated_at=excluded.updated_at, stopped_at=excluded.stopped_at,
		state_rev=excluded.state_rev, text_rev=excluded.text_rev
	WHERE goals.state_rev <= excluded.state_rev AND goals.text_rev <= excluded.text_rev`,
		g.UserID, g.GoalID, g.Objective, string(g.Status), g.ProjectPath, string(g.Phase),
		g.TokenBudget, g.TokenWindow, g.MaxTurns, g.TokensUsed, g.TimeUsedSeconds, g.TurnsUsed,
		g.ConsecutiveNoToolTurns, g.ClockHoldSeconds, g.Understanding, g.Summary,
		acceptance, steps, formatGoalTime(g.CreatedAt), formatGoalTime(g.UpdatedAt), formatGoalTime(g.StoppedAt),
		g.StateRev, g.TextRev)
	return err
}

func (s *Store) deleteGoal(userID string) error {
	if s == nil || s.db == nil || userID == "" {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM goals WHERE user_id=?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// Search finds goals by objective, plan, acceptance, summary, project path,
// status, or phase. Every goal is already resident, so the scan sees the plan
// text, status, and letter case together.
func (s *Store) Search(query string) []*Goal {
	if s == nil {
		return nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	needle := strings.ToLower(query)
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Goal, 0)
	for _, g := range s.goals {
		if g == nil {
			continue
		}
		if strings.Contains(goalSearchText(g), needle) {
			out = append(out, cloneGoal(g))
		}
	}
	return out
}

func goalSearchText(g *Goal) string {
	parts := make([]string, 0, 6+len(g.AcceptanceCriteria)+len(g.Steps)*4)
	parts = append(parts, g.Objective, g.Understanding, g.Summary, g.ProjectPath, string(g.Status), string(g.Phase))
	parts = append(parts, g.AcceptanceCriteria...)
	for _, step := range g.Steps {
		parts = append(parts, step.Title, step.Detail, step.Result, string(step.Status))
	}
	return strings.ToLower(strings.Join(parts, "\n"))
}

func (s *Store) goalMeta(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM goal_meta WHERE key=?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (s *Store) setGoalMeta(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO goal_meta(key, value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func formatGoalTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseGoalTime(value string) time.Time {
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
