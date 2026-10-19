package agentservice

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

// DesktopStepper is the service database this process already opened.
// A memory store has no step journal. The table is not a workflow engine:
// it has no scheduler, no retry count, and no next fire. schema_version of
// agentservice_state stays 1.
type DesktopStepper interface {
	RunDesktopStep(step desktopbot.Step, command string, run func() (string, error)) (text string, replayed bool, err error)
	ResumeDesktopRound(runID, progressNext string) (desktopbot.ResumePlan, error)
	FinishedDesktopStep(key string) (text string, done bool, err error)
}

// DesktopStepper returns the sqlite step journal, or nil when this service
// has no database. Resume does not send and does not persist from EndDesktopRound.
func (s *Service) DesktopStepper() DesktopStepper {
	if s == nil {
		return nil
	}
	db, ok := s.store.(*SQLiteStore)
	if !ok || db == nil {
		return nil
	}
	return db
}

// The row is one future Temporal step result. This commit does not install
// Temporal. The table stops at step number, tool, idempotency key, result,
// and done.
func (s *SQLiteStore) ensureDesktopStepTableLocked() error {
	if err := s.ensureOpenLocked(); err != nil {
		return err
	}
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS desktop_step_journal (
  idempotency_key TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  step_no INTEGER NOT NULL,
  tool TEXT NOT NULL,
  result TEXT NOT NULL,
  done INTEGER NOT NULL
)`)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`CREATE INDEX IF NOT EXISTS desktop_step_journal_run ON desktop_step_journal(run_id, step_no)`)
	return err
}

func (s *SQLiteStore) guardDesktopStepsLocked() error {
	if err := s.ensureOpenLocked(); err != nil {
		return err
	}
	if desktopbot.JournalOnVM(s.path) {
		return desktopbot.ErrJournalOnVM
	}
	return nil
}

// FinishedDesktopStep returns the recorded result only when that key is done.
func (s *SQLiteStore) FinishedDesktopStep(key string) (string, bool, error) {
	if s == nil {
		return "", false, ErrServiceClosed
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.guardDesktopStepsLocked(); err != nil {
		return "", false, err
	}
	if err := s.ensureDesktopStepTableLocked(); err != nil {
		return "", false, err
	}
	return s.finishedDesktopStepLocked(key)
}

func (s *SQLiteStore) finishedDesktopStepLocked(key string) (string, bool, error) {
	var result string
	var done int
	err := s.db.QueryRow(`SELECT result, done FROM desktop_step_journal WHERE idempotency_key=?`, key).Scan(&result, &done)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if done != 1 {
		return "", false, nil
	}
	return result, true, nil
}

// SaveDesktopStep inserts one completed step. The first result for a key
// sticks. A failed step is not stored. schema_version is not changed.
func (s *SQLiteStore) SaveDesktopStep(step desktopbot.Step) (desktopbot.Step, error) {
	if s == nil {
		return step, ErrServiceClosed
	}
	if !step.Done {
		return step, fmt.Errorf("a desktop step is stored only after it succeeds")
	}
	step.Key = strings.TrimSpace(step.Key)
	if step.Key == "" {
		return step, fmt.Errorf("desktop step key is empty")
	}
	step.RunID = strings.TrimSpace(step.RunID)
	if step.RunID == "" {
		return step, fmt.Errorf("desktop step run id is empty")
	}
	step.Tool = desktopbot.EffectName(step.Tool, "")
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.guardDesktopStepsLocked(); err != nil {
		return step, err
	}
	if err := s.ensureDesktopStepTableLocked(); err != nil {
		return step, err
	}
	if step.Number <= 0 {
		var max sql.NullInt64
		if err := s.db.QueryRow(`SELECT MAX(step_no) FROM desktop_step_journal WHERE run_id=?`, step.RunID).Scan(&max); err != nil {
			return step, err
		}
		if max.Valid {
			step.Number = int(max.Int64) + 1
		} else {
			step.Number = 1
		}
	}
	res, err := s.db.Exec(`INSERT OR IGNORE INTO desktop_step_journal(idempotency_key, run_id, step_no, tool, result, done) VALUES(?,?,?,?,?,1)`,
		step.Key, step.RunID, step.Number, step.Tool, step.Result)
	if err != nil {
		return step, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var done int
		stored := desktopbot.Step{Key: step.Key}
		err := s.db.QueryRow(`SELECT run_id, step_no, tool, result, done FROM desktop_step_journal WHERE idempotency_key=?`, step.Key).Scan(
			&stored.RunID, &stored.Number, &stored.Tool, &stored.Result, &done)
		if err != nil {
			return step, err
		}
		stored.Done = done == 1
		return stored, nil
	}
	return step, nil
}

// ListDesktopSteps returns the completed steps of one run, in step order.
func (s *SQLiteStore) ListDesktopSteps(runID string) ([]desktopbot.Step, error) {
	if s == nil {
		return nil, ErrServiceClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.guardDesktopStepsLocked(); err != nil {
		return nil, err
	}
	if err := s.ensureDesktopStepTableLocked(); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT idempotency_key, run_id, step_no, tool, result, done FROM desktop_step_journal WHERE run_id=? ORDER BY step_no, idempotency_key`, strings.TrimSpace(runID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []desktopbot.Step
	for rows.Next() {
		var step desktopbot.Step
		var done int
		if err := rows.Scan(&step.Key, &step.RunID, &step.Number, &step.Tool, &step.Result, &done); err != nil {
			return nil, err
		}
		step.Done = done == 1
		out = append(out, step)
	}
	return out, rows.Err()
}

// RunDesktopStep runs a side effect once. The same key returns the recorded
// result and does not call run. A read-only step always runs and is not
// stored. An empty key is not stored. A failed run is not marked done.
func (s *SQLiteStore) RunDesktopStep(step desktopbot.Step, command string, run func() (string, error)) (string, bool, error) {
	if s == nil {
		return "", false, ErrServiceClosed
	}
	if run == nil {
		return "", false, fmt.Errorf("desktop step has no body")
	}
	if !desktopbot.SideEffect(step.Tool, command) {
		text, err := run()
		return text, false, err
	}
	s.mu.Lock()
	if err := s.guardDesktopStepsLocked(); err != nil {
		s.mu.Unlock()
		return "", false, err
	}
	if strings.TrimSpace(step.Key) == "" {
		s.mu.Unlock()
		text, err := run()
		return text, false, err
	}
	if err := s.ensureDesktopStepTableLocked(); err != nil {
		s.mu.Unlock()
		return "", false, err
	}
	text, done, err := s.finishedDesktopStepLocked(strings.TrimSpace(step.Key))
	s.mu.Unlock()
	if err != nil {
		return "", false, err
	}
	if done {
		return text, true, nil
	}
	text, err = run()
	if err != nil {
		return text, false, err
	}
	step.Key = strings.TrimSpace(step.Key)
	step.Result = text
	step.Done = true
	step.Tool = desktopbot.EffectName(step.Tool, command)
	if _, err := s.SaveDesktopStep(step); err != nil {
		return text, false, err
	}
	return text, false, nil
}

// ResumeDesktopRound starts at the step after the last completed one.
// It does not call send_to_user. The progress line stays the display line.
func (s *SQLiteStore) ResumeDesktopRound(runID, progressNext string) (desktopbot.ResumePlan, error) {
	steps, err := s.ListDesktopSteps(runID)
	if err != nil {
		return desktopbot.ResumePlan{}, err
	}
	plan := desktopbot.ResumeExecution("", desktopbot.Sections{NextStep: progressNext}, steps)
	plan.Sentence = ""
	return plan, nil
}
