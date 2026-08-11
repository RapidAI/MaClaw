package codingruntime

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// SQLiteStore is the durable execution ledger. It uses a standalone database
// so deployment and recovery can evolve independently from workflow state.
type SQLiteStore struct{ db *sql.DB }

func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	s := &SQLiteStore{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLiteStore) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS coding_runtime_tasks (
 task_id TEXT PRIMARY KEY, workflow_id TEXT NOT NULL, phase_id TEXT NOT NULL, owner_id TEXT NOT NULL,
 parent_task_id TEXT NOT NULL, project_ref TEXT NOT NULL, mode TEXT NOT NULL, requested_work TEXT NOT NULL,
 policy_digest TEXT NOT NULL, status TEXT NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS coding_runtime_attempts (
 attempt_id TEXT PRIMARY KEY, task_id TEXT NOT NULL, attempt_no INTEGER NOT NULL, lease_owner TEXT NOT NULL,
 lease_until INTEGER NOT NULL, status TEXT NOT NULL, policy_json TEXT NOT NULL, side_effect_state TEXT NOT NULL,
 workspace_before_json TEXT NOT NULL, workspace_after_json TEXT NOT NULL, error_code TEXT NOT NULL,
 error_summary TEXT NOT NULL, started_at INTEGER NOT NULL, finished_at INTEGER NOT NULL,
 UNIQUE(task_id, attempt_no), FOREIGN KEY(task_id) REFERENCES coding_runtime_tasks(task_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS coding_runtime_active_lease ON coding_runtime_attempts(task_id) WHERE status = 'running';
CREATE TABLE IF NOT EXISTS coding_runtime_writer_admission_guard (
 guard_id INTEGER PRIMARY KEY CHECK(guard_id=1), revision INTEGER NOT NULL
);
INSERT OR IGNORE INTO coding_runtime_writer_admission_guard(guard_id,revision) VALUES(1,0);
CREATE TABLE IF NOT EXISTS coding_runtime_events (
 attempt_id TEXT NOT NULL, sequence INTEGER NOT NULL, task_id TEXT NOT NULL, type TEXT NOT NULL,
 payload_digest TEXT NOT NULL, created_at INTEGER NOT NULL, PRIMARY KEY(attempt_id, sequence),
 FOREIGN KEY(attempt_id) REFERENCES coding_runtime_attempts(attempt_id)
);
CREATE TABLE IF NOT EXISTS coding_runtime_child_results (
 child_task_id TEXT PRIMARY KEY, attempt_id TEXT NOT NULL, status TEXT NOT NULL,
 summary TEXT NOT NULL, evidence_digest TEXT NOT NULL, completed_at INTEGER NOT NULL,
 FOREIGN KEY(child_task_id) REFERENCES coding_runtime_tasks(task_id)
);
CREATE TABLE IF NOT EXISTS coding_runtime_consumed_continuations (
 parent_attempt_id TEXT PRIMARY KEY, task_id TEXT NOT NULL, review_attempt_id TEXT NOT NULL UNIQUE,
 consumed_at INTEGER NOT NULL,
 FOREIGN KEY(parent_attempt_id) REFERENCES coding_runtime_attempts(attempt_id),
 FOREIGN KEY(review_attempt_id) REFERENCES coding_runtime_attempts(attempt_id)
);`)
	return err
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteStore) CreateTask(task Task) (*Task, error) {
	task = normalizeTaskForLedger(task)
	if task.TaskID == "" {
		task.TaskID = uuid.NewString()
	}
	if task.Status == "" {
		task.Status = TaskQueued
	}
	if !validStartStatus(task.Status) {
		return nil, fmt.Errorf("%w: create task in %s", ErrInvalidTransition, task.Status)
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now().UTC()
	}
	task.UpdatedAt = task.CreatedAt
	_, err := s.db.Exec(`INSERT INTO coding_runtime_tasks(task_id,workflow_id,phase_id,owner_id,parent_task_id,project_ref,mode,requested_work,policy_digest,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, task.TaskID, task.WorkflowID, task.PhaseID, task.OwnerID, task.ParentTaskID, task.ProjectRef, task.Mode, task.RequestedWork, task.PolicyDigest, task.Status, unixNanos(task.CreatedAt), unixNanos(task.UpdatedAt))
	if err != nil {
		return nil, err
	}
	return cloneTask(&task), nil
}

func (s *SQLiteStore) GetTask(taskID string) (*Task, error) {
	return s.getTask(s.db, taskID)
}

func (s *SQLiteStore) ListChildTasks(parentTaskID string) ([]*Task, error) {
	if _, err := s.GetTask(parentTaskID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT task_id FROM coding_runtime_tasks WHERE parent_task_id=? ORDER BY task_id`, parentTaskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Task
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		task, err := s.getTask(s.db, id)
		if err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) getTask(q rowQuerier, taskID string) (*Task, error) {
	var task Task
	var status string
	var created, updated int64
	err := q.QueryRow(`SELECT task_id,workflow_id,phase_id,owner_id,parent_task_id,project_ref,mode,requested_work,policy_digest,status,created_at,updated_at FROM coding_runtime_tasks WHERE task_id=?`, taskID).Scan(&task.TaskID, &task.WorkflowID, &task.PhaseID, &task.OwnerID, &task.ParentTaskID, &task.ProjectRef, &task.Mode, &task.RequestedWork, &task.PolicyDigest, &status, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	task.Status, task.CreatedAt, task.UpdatedAt = TaskStatus(status), fromUnixNanos(created), fromUnixNanos(updated)
	return &task, nil
}

func (s *SQLiteStore) appendEventTx(tx *sql.Tx, attemptID, taskID, eventType, payloadDigest string, now time.Time) (*Event, error) {
	var seq uint64
	if err := tx.QueryRow(`SELECT COALESCE(MAX(sequence),0)+1 FROM coding_runtime_events WHERE attempt_id=?`, attemptID).Scan(&seq); err != nil {
		return nil, err
	}
	event := &Event{TaskID: taskID, AttemptID: attemptID, Sequence: seq, Type: eventType, PayloadDigest: payloadDigest, CreatedAt: now}
	if _, err := tx.Exec(`INSERT INTO coding_runtime_events(attempt_id,sequence,task_id,type,payload_digest,created_at) VALUES(?,?,?,?,?,?)`, event.AttemptID, event.Sequence, event.TaskID, event.Type, event.PayloadDigest, unixNanos(now)); err != nil {
		return nil, err
	}
	return event, nil
}

func (s *SQLiteStore) GetAttempt(attemptID string) (*Attempt, error) {
	return s.getAttempt(s.db, attemptID)
}

func (s *SQLiteStore) ListAttempts(taskID string) ([]*Attempt, error) {
	if _, err := s.GetTask(taskID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT attempt_id FROM coding_runtime_attempts WHERE task_id=? ORDER BY attempt_no`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Attempt
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		attempt, err := s.getAttempt(s.db, id)
		if err != nil {
			return nil, err
		}
		out = append(out, attempt)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) MarkTaskWaitingApproval(taskID string, now time.Time) (*Task, error) {
	result, err := s.db.Exec(`UPDATE coding_runtime_tasks SET status=?,updated_at=? WHERE task_id=? AND status IN (?,?)`, TaskWaitingApproval, unixNanos(now), taskID, TaskQueued, TaskWaitingApproval)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		if _, err := s.GetTask(taskID); errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("%w: waiting approval", ErrInvalidTransition)
	}
	return s.GetTask(taskID)
}

func (s *SQLiteStore) MarkTaskReadyForRecovery(taskID string, now time.Time) (*Task, error) {
	result, err := s.db.Exec(`UPDATE coding_runtime_tasks SET status=?,updated_at=? WHERE task_id=? AND status=?`, TaskQueued, unixNanos(now), taskID, TaskInterrupted)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		if _, err := s.GetTask(taskID); errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("%w: ready for recovery", ErrInvalidTransition)
	}
	return s.GetTask(taskID)
}

func (s *SQLiteStore) StartAttempt(taskID, leaseOwner string, leaseFor time.Duration, policy PolicySnapshot, now time.Time) (*Attempt, error) {
	if leaseOwner == "" || leaseFor <= 0 {
		return nil, fmt.Errorf("%w: start attempt", ErrInvalidTransition)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRow(`SELECT status FROM coding_runtime_tasks WHERE task_id=?`, taskID).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if status == string(TaskRunning) {
		return nil, ErrLeaseHeld
	}
	if !validStartStatus(TaskStatus(status)) {
		return nil, fmt.Errorf("%w: start attempt", ErrInvalidTransition)
	}
	task, err := s.getTask(tx, taskID)
	if err != nil {
		return nil, err
	}
	policy, err = NormalizeWriterPolicy(*task, policy)
	if err != nil {
		return nil, err
	}
	// Serialize the read-check-insert admission window across every process
	// sharing this ledger. The task lease index only protects a single task;
	// this guard protects cross-task write scopes.
	if _, err = tx.Exec(`UPDATE coding_runtime_writer_admission_guard SET revision=revision+1 WHERE guard_id=1`); err != nil {
		return nil, err
	}
	rows, err := tx.Query(`SELECT a.attempt_id,a.policy_json,a.task_id FROM coding_runtime_attempts a WHERE a.status=? AND a.lease_until>?`, TaskRunning, unixNanos(now))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var attemptID, policyJSON, otherTaskID string
		if err = rows.Scan(&attemptID, &policyJSON, &otherTaskID); err != nil {
			rows.Close()
			return nil, err
		}
		if otherTaskID == taskID {
			continue
		}
		var otherPolicy PolicySnapshot
		if err = json.Unmarshal([]byte(policyJSON), &otherPolicy); err != nil {
			rows.Close()
			return nil, err
		}
		otherTask, getErr := s.getTask(tx, otherTaskID)
		if getErr != nil {
			rows.Close()
			return nil, getErr
		}
		if conflict := WriterAdmissionConflict(*task, policy, *otherTask, otherPolicy); conflict.Conflicts {
			rows.Close()
			return nil, WriterAdmissionError{Conflict: conflict}
		}
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	var no int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(attempt_no),0)+1 FROM coding_runtime_attempts WHERE task_id=?`, taskID).Scan(&no); err != nil {
		return nil, err
	}
	policyJSON, _ := json.Marshal(policy)
	attempt := &Attempt{AttemptID: uuid.NewString(), TaskID: taskID, AttemptNo: no, LeaseOwner: leaseOwner, LeaseUntil: now.Add(leaseFor), Status: TaskRunning, Policy: policy, SideEffectState: SideEffectNone, StartedAt: now}
	_, err = tx.Exec(`INSERT INTO coding_runtime_attempts(attempt_id,task_id,attempt_no,lease_owner,lease_until,status,policy_json,side_effect_state,workspace_before_json,workspace_after_json,error_code,error_summary,started_at,finished_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, attempt.AttemptID, attempt.TaskID, attempt.AttemptNo, attempt.LeaseOwner, unixNanos(attempt.LeaseUntil), attempt.Status, string(policyJSON), attempt.SideEffectState, "", "", "", "", unixNanos(now), 0)
	if err != nil {
		if isConstraint(err) {
			return nil, ErrLeaseHeld
		}
		return nil, err
	}
	if _, err = tx.Exec(`UPDATE coding_runtime_tasks SET status=?,updated_at=? WHERE task_id=?`, TaskRunning, unixNanos(now), taskID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return cloneAttempt(attempt), nil
}

func (s *SQLiteStore) ConsumeParentContinuation(taskID, parentAttemptID, reviewAttemptID string, now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	parentAttempt, err := s.getAttempt(tx, parentAttemptID)
	if err != nil {
		return err
	}
	reviewAttempt, err := s.getAttempt(tx, reviewAttemptID)
	if err != nil {
		return err
	}
	if parentAttempt.TaskID != taskID || parentAttempt.Status != TaskWaitingChild || reviewAttempt.TaskID != taskID || reviewAttempt.Status != TaskRunning {
		return fmt.Errorf("%w: consume parent continuation", ErrInvalidTransition)
	}
	if _, err = tx.Exec(`INSERT INTO coding_runtime_consumed_continuations(parent_attempt_id,task_id,review_attempt_id,consumed_at) VALUES(?,?,?,?)`, parentAttemptID, taskID, reviewAttemptID, unixNanos(now)); err != nil {
		if isConstraint(err) {
			return ErrContinuationConsumed
		}
		return err
	}
	if _, err = s.appendEventTx(tx, reviewAttemptID, taskID, "parent_continuation_consumed", codingRuntimeErrorDigest(parentAttemptID+"|"+reviewAttemptID), now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) IsParentContinuationConsumed(parentAttemptID string) (bool, error) {
	if _, err := s.GetAttempt(parentAttemptID); err != nil {
		return false, err
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM coding_runtime_consumed_continuations WHERE parent_attempt_id=?`, parentAttemptID).Scan(&count); err != nil {
		return false, err
	}
	return count != 0, nil
}

// RecordWorkspaceBefore persists a best-effort, read-only workspace baseline.
// A probe failure is recorded as an event by Runner but does not prevent an
// otherwise authorized attempt from beginning.
func (s *SQLiteStore) RecordWorkspaceBefore(attemptID, leaseOwner string, probe *WorkspaceProbe, now time.Time) (*Attempt, error) {
	if probe == nil {
		return nil, fmt.Errorf("%w: nil workspace before probe", ErrInvalidTransition)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	attempt, err := s.getAttempt(tx, attemptID)
	if err != nil {
		return nil, err
	}
	if attempt.Status != TaskRunning {
		return nil, ErrAttemptNotRunning
	}
	if attempt.LeaseOwner != leaseOwner {
		return nil, ErrLeaseOwnerMismatch
	}
	before, err := json.Marshal(probe)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(`UPDATE coding_runtime_attempts SET workspace_before_json=? WHERE attempt_id=?`, string(before), attemptID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	attempt.WorkspaceBefore = cloneProbe(probe)
	return attempt, nil
}

// AdmitReadOnlyChildren commits sibling child records and closes the parent
// attempt in one transaction. Its lease is removed before any child becomes
// visible as queued, so a parent cannot write while children inspect.
func (s *SQLiteStore) AdmitReadOnlyChildren(parentAttemptID, leaseOwner string, specs []ChildTaskSpec, policy PolicySnapshot, now time.Time) ([]ChildTaskHandle, error) {
	if leaseOwner == "" || len(specs) == 0 {
		return nil, fmt.Errorf("%w: admit child", ErrInvalidTransition)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	parentAttempt, err := s.getAttempt(tx, parentAttemptID)
	if err != nil {
		return nil, err
	}
	if parentAttempt.Status != TaskRunning {
		return nil, ErrAttemptNotRunning
	}
	if parentAttempt.LeaseOwner != leaseOwner {
		return nil, ErrLeaseOwnerMismatch
	}
	parent, err := s.getTask(tx, parentAttempt.TaskID)
	if err != nil {
		return nil, err
	}
	for _, spec := range specs {
		if err := validateReadOnlyChildSpec(*parent, spec, policy); err != nil {
			return nil, err
		}
	}
	handles := make([]ChildTaskHandle, 0, len(specs))
	for _, spec := range specs {
		childID := uuid.NewString()
		child := normalizeTaskForLedger(Task{TaskID: childID, WorkflowID: parent.WorkflowID, PhaseID: parent.PhaseID, OwnerID: parent.OwnerID, ParentTaskID: parent.TaskID, ProjectRef: firstChildValue(spec.ProjectRef, parent.ProjectRef), Mode: firstChildValue(spec.Mode, parent.Mode), RequestedWork: spec.RequestedWork, PolicyDigest: policy.Digest, Status: TaskQueued, CreatedAt: now, UpdatedAt: now})
		if _, err = tx.Exec(`INSERT INTO coding_runtime_tasks(task_id,workflow_id,phase_id,owner_id,parent_task_id,project_ref,mode,requested_work,policy_digest,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, child.TaskID, child.WorkflowID, child.PhaseID, child.OwnerID, child.ParentTaskID, child.ProjectRef, child.Mode, child.RequestedWork, child.PolicyDigest, child.Status, unixNanos(now), unixNanos(now)); err != nil {
			return nil, err
		}
		handles = append(handles, ChildTaskHandle{TaskID: childID, ParentTaskID: parent.TaskID, ParentAttemptID: parentAttemptID, Name: spec.Name, Status: TaskQueued, ExecutionTarget: child.Mode})
	}
	if _, err = tx.Exec(`UPDATE coding_runtime_attempts SET status=?,lease_until=0,finished_at=? WHERE attempt_id=? AND status=? AND lease_owner=?`, TaskWaitingChild, unixNanos(now), parentAttemptID, TaskRunning, leaseOwner); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(`UPDATE coding_runtime_tasks SET status=?,updated_at=? WHERE task_id=? AND status=?`, TaskWaitingChild, unixNanos(now), parent.TaskID, TaskRunning); err != nil {
		return nil, err
	}
	if _, err = s.appendEventTx(tx, parentAttemptID, parent.TaskID, "children_admitted", childHandlesDigest(handles), now); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return handles, nil
}

func (s *SQLiteStore) CompleteChildTask(childTaskID string, result ChildTaskResult, now time.Time) (*Task, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	child, err := s.getTask(tx, childTaskID)
	if err != nil {
		return nil, err
	}
	if child.ParentTaskID == "" || !child.Status.Terminal() {
		return nil, fmt.Errorf("%w: child task is not terminal", ErrInvalidTransition)
	}
	if result.Status != "" && result.Status != child.Status {
		return nil, fmt.Errorf("%w: child result status differs from task", ErrInvalidTransition)
	}
	var exists int
	if err = tx.QueryRow(`SELECT COUNT(1) FROM coding_runtime_child_results WHERE child_task_id=?`, childTaskID).Scan(&exists); err != nil {
		return nil, err
	}
	if exists != 0 {
		return nil, fmt.Errorf("%w: child result already delivered", ErrInvalidTransition)
	}
	result.TaskID, result.Status, result.Summary, result.EvidenceDigest, result.CompletedAt = childTaskID, child.Status, boundedChildTaskSummary(result.Summary), boundedLedgerText(result.EvidenceDigest, maxPayloadDigestRunes), now
	if _, err = tx.Exec(`INSERT INTO coding_runtime_child_results(child_task_id,attempt_id,status,summary,evidence_digest,completed_at) VALUES(?,?,?,?,?,?)`, childTaskID, result.AttemptID, result.Status, result.Summary, result.EvidenceDigest, unixNanos(now)); err != nil {
		return nil, err
	}
	parent, err := s.getTask(tx, child.ParentTaskID)
	if err != nil {
		return nil, err
	}
	var pending int
	if err = tx.QueryRow(`SELECT COUNT(1) FROM coding_runtime_tasks c LEFT JOIN coding_runtime_child_results r ON r.child_task_id=c.task_id WHERE c.parent_task_id=? AND r.child_task_id IS NULL`, parent.TaskID).Scan(&pending); err != nil {
		return nil, err
	}
	if pending == 0 && parent.Status == TaskWaitingChild {
		if _, err = tx.Exec(`UPDATE coding_runtime_tasks SET status=?,updated_at=? WHERE task_id=?`, TaskQueued, unixNanos(now), parent.TaskID); err != nil {
			return nil, err
		}
		parent.Status, parent.UpdatedAt = TaskQueued, now
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return parent, nil
}

func (s *SQLiteStore) ListChildResults(parentTaskID string) ([]ChildTaskResult, error) {
	if _, err := s.GetTask(parentTaskID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT r.child_task_id,r.attempt_id,r.status,r.summary,r.evidence_digest,r.completed_at FROM coding_runtime_child_results r JOIN coding_runtime_tasks c ON c.task_id=r.child_task_id WHERE c.parent_task_id=? ORDER BY r.completed_at,r.child_task_id`, parentTaskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChildTaskResult
	for rows.Next() {
		var result ChildTaskResult
		var status string
		var completed int64
		if err := rows.Scan(&result.TaskID, &result.AttemptID, &status, &result.Summary, &result.EvidenceDigest, &completed); err != nil {
			return nil, err
		}
		result.Status, result.CompletedAt = TaskStatus(status), fromUnixNanos(completed)
		out = append(out, result)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) AppendEvent(attemptID, leaseOwner, eventType, payloadDigest string, now time.Time) (*Event, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	attempt, err := s.getAttempt(tx, attemptID)
	if err != nil {
		return nil, err
	}
	if attempt.Status != TaskRunning {
		return nil, ErrAttemptNotRunning
	}
	if attempt.LeaseOwner != leaseOwner {
		return nil, ErrLeaseOwnerMismatch
	}
	eventType, payloadDigest = normalizeEventForLedger(eventType, payloadDigest)
	var seq uint64
	if err = tx.QueryRow(`SELECT COALESCE(MAX(sequence),0)+1 FROM coding_runtime_events WHERE attempt_id=?`, attemptID).Scan(&seq); err != nil {
		return nil, err
	}
	event := &Event{TaskID: attempt.TaskID, AttemptID: attemptID, Sequence: seq, Type: eventType, PayloadDigest: payloadDigest, CreatedAt: now}
	_, err = tx.Exec(`INSERT INTO coding_runtime_events(attempt_id,sequence,task_id,type,payload_digest,created_at) VALUES(?,?,?,?,?,?)`, event.AttemptID, event.Sequence, event.TaskID, event.Type, event.PayloadDigest, unixNanos(now))
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return event, nil
}

func (s *SQLiteStore) RecordStaleCallback(attemptID, payloadDigest string, now time.Time) (*Event, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	attempt, err := s.getAttempt(tx, attemptID)
	if err != nil {
		return nil, err
	}
	if attempt.Status == TaskRunning {
		return nil, fmt.Errorf("%w: running attempt", ErrInvalidTransition)
	}
	var seq uint64
	if err = tx.QueryRow(`SELECT COALESCE(MAX(sequence),0)+1 FROM coding_runtime_events WHERE attempt_id=?`, attemptID).Scan(&seq); err != nil {
		return nil, err
	}
	event := &Event{TaskID: attempt.TaskID, AttemptID: attemptID, Sequence: seq, Type: "stale_callback_discarded", PayloadDigest: boundedLedgerText(payloadDigest, maxPayloadDigestRunes), CreatedAt: now}
	if _, err = tx.Exec(`INSERT INTO coding_runtime_events(attempt_id,sequence,task_id,type,payload_digest,created_at) VALUES(?,?,?,?,?,?)`, event.AttemptID, event.Sequence, event.TaskID, event.Type, event.PayloadDigest, unixNanos(now)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return event, nil
}

func (s *SQLiteStore) ListEvents(attemptID string) ([]Event, error) {
	if _, err := s.GetAttempt(attemptID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT task_id,attempt_id,sequence,type,payload_digest,created_at FROM coding_runtime_events WHERE attempt_id=? ORDER BY sequence ASC`, attemptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var event Event
		var created int64
		if err := rows.Scan(&event.TaskID, &event.AttemptID, &event.Sequence, &event.Type, &event.PayloadDigest, &created); err != nil {
			return nil, err
		}
		event.CreatedAt = fromUnixNanos(created)
		out = append(out, event)
	}
	return out, rows.Err()
}

// AppendRecoveryEvent records a recovery probe or explicit user decision for
// an already-ended uncertain attempt. It cannot mutate attempt execution state
// and therefore does not accept a lease owner.
func (s *SQLiteStore) AppendRecoveryEvent(attemptID, eventType, payloadDigest string, now time.Time) (*Event, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	attempt, err := s.getAttempt(tx, attemptID)
	if err != nil {
		return nil, err
	}
	if attempt.Status != TaskInterrupted && attempt.SideEffectState != SideEffectUncertain {
		return nil, fmt.Errorf("%w: recovery event", ErrInvalidTransition)
	}
	eventType, payloadDigest = normalizeEventForLedger(eventType, payloadDigest)
	var seq uint64
	if err = tx.QueryRow(`SELECT COALESCE(MAX(sequence),0)+1 FROM coding_runtime_events WHERE attempt_id=?`, attemptID).Scan(&seq); err != nil {
		return nil, err
	}
	event := &Event{TaskID: attempt.TaskID, AttemptID: attemptID, Sequence: seq, Type: eventType, PayloadDigest: payloadDigest, CreatedAt: now}
	if _, err = tx.Exec(`INSERT INTO coding_runtime_events(attempt_id,sequence,task_id,type,payload_digest,created_at) VALUES(?,?,?,?,?,?)`, event.AttemptID, event.Sequence, event.TaskID, event.Type, event.PayloadDigest, unixNanos(now)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return event, nil
}

func (s *SQLiteStore) FinishAttempt(attemptID, leaseOwner string, input FinishInput, now time.Time) (*Attempt, error) {
	input = normalizeFinishInputForLedger(input)
	if !validTerminalStatus(input.Status) {
		return nil, fmt.Errorf("%w: finish attempt", ErrInvalidTransition)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	attempt, err := s.getAttempt(tx, attemptID)
	if err != nil {
		return nil, err
	}
	if attempt.Status != TaskRunning {
		return nil, fmt.Errorf("%w: finish attempt", ErrInvalidTransition)
	}
	if attempt.LeaseOwner != leaseOwner {
		return nil, ErrLeaseOwnerMismatch
	}
	after, _ := json.Marshal(input.WorkspaceAfter)
	_, err = tx.Exec(`UPDATE coding_runtime_attempts SET lease_until=0,status=?,side_effect_state=?,workspace_after_json=?,error_code=?,error_summary=?,finished_at=? WHERE attempt_id=?`, input.Status, input.SideEffectState, string(after), input.ErrorCode, input.ErrorSummary, unixNanos(now), attemptID)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(`UPDATE coding_runtime_tasks SET status=?,updated_at=? WHERE task_id=?`, input.Status, unixNanos(now), attempt.TaskID)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	attempt.Status, attempt.SideEffectState, attempt.WorkspaceAfter, attempt.ErrorCode, attempt.ErrorSummary, attempt.FinishedAt, attempt.LeaseUntil = input.Status, input.SideEffectState, cloneProbe(input.WorkspaceAfter), input.ErrorCode, input.ErrorSummary, now, time.Time{}
	return attempt, nil
}

// CancelTask durably cancels a root task and all unfinished descendants. This
// handles parents in waiting_child as well as leased children, so a child
// cannot complete after an explicit cancellation and silently re-queue its
// parent.
func (s *SQLiteStore) CancelTask(taskID string, now time.Time) ([]*Attempt, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = s.getTask(tx, taskID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(`WITH RECURSIVE subtree(task_id) AS (
  SELECT task_id FROM coding_runtime_tasks WHERE task_id=?
  UNION ALL
  SELECT child.task_id FROM coding_runtime_tasks child JOIN subtree parent ON child.parent_task_id=parent.task_id
) SELECT task_id FROM subtree`, taskID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	sort.Strings(ids)
	var cancelled []*Attempt
	for _, id := range ids {
		attemptRows, queryErr := tx.Query(`SELECT attempt_id FROM coding_runtime_attempts WHERE task_id=? AND status NOT IN (?,?,?,?,?) ORDER BY attempt_no`, id, TaskCompleted, TaskFailed, TaskBlocked, TaskCancelled, TaskInterrupted)
		if queryErr != nil {
			return nil, queryErr
		}
		var attemptIDs []string
		for attemptRows.Next() {
			var id string
			if queryErr = attemptRows.Scan(&id); queryErr != nil {
				attemptRows.Close()
				return nil, queryErr
			}
			attemptIDs = append(attemptIDs, id)
		}
		if queryErr = attemptRows.Close(); queryErr != nil {
			return nil, queryErr
		}
		for _, attemptID := range attemptIDs {
			attempt, getErr := s.getAttempt(tx, attemptID)
			if getErr != nil {
				return nil, getErr
			}
			sideEffect := attempt.SideEffectState
			if attempt.Status == TaskRunning && sideEffect == SideEffectNone {
				sideEffect = SideEffectUncertain
			}
			if _, queryErr = tx.Exec(`UPDATE coding_runtime_attempts SET status=?,side_effect_state=?,lease_until=0,finished_at=? WHERE attempt_id=?`, TaskCancelled, sideEffect, unixNanos(now), attemptID); queryErr != nil {
				return nil, queryErr
			}
			if _, queryErr = s.appendEventTx(tx, attemptID, id, "task_cancelled", "", now); queryErr != nil {
				return nil, queryErr
			}
			attempt.Status, attempt.SideEffectState, attempt.LeaseUntil, attempt.FinishedAt = TaskCancelled, sideEffect, time.Time{}, now
			cancelled = append(cancelled, attempt)
		}
		if _, err = tx.Exec(`UPDATE coding_runtime_tasks SET status=?,updated_at=? WHERE task_id=? AND status NOT IN (?,?,?,?)`, TaskCancelled, unixNanos(now), id, TaskCompleted, TaskFailed, TaskBlocked, TaskCancelled); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return cancelled, nil
}

func (s *SQLiteStore) ExpireLeases(now time.Time) ([]*Attempt, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT attempt_id FROM coding_runtime_attempts WHERE status=? AND lease_until<=?`, TaskRunning, unixNanos(now))
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	var out []*Attempt
	for _, id := range ids {
		a, err := s.getAttempt(tx, id)
		if err != nil {
			return nil, err
		}
		if _, err = tx.Exec(`UPDATE coding_runtime_attempts SET status=?,side_effect_state=?,lease_until=0,finished_at=? WHERE attempt_id=?`, TaskInterrupted, SideEffectUncertain, unixNanos(now), id); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(`UPDATE coding_runtime_tasks SET status=?,updated_at=? WHERE task_id=?`, TaskInterrupted, unixNanos(now), a.TaskID); err != nil {
			return nil, err
		}
		// A parent without a lease waits only for child delivery. When the
		// process loses a running child, interrupt that parent too so recovery
		// is explicit instead of leaving it permanently waiting_child.
		var parentID string
		if err = tx.QueryRow(`SELECT parent_task_id FROM coding_runtime_tasks WHERE task_id=?`, a.TaskID).Scan(&parentID); err != nil {
			return nil, err
		}
		if parentID != "" {
			if err = s.interruptWaitingParentTx(tx, parentID, now); err != nil {
				return nil, err
			}
		}
		if _, err = tx.Exec(`INSERT INTO coding_runtime_events(attempt_id,sequence,task_id,type,payload_digest,created_at) VALUES(?,(SELECT COALESCE(MAX(sequence),0)+1 FROM coding_runtime_events WHERE attempt_id=?),?, 'lease_expired','',?)`, id, id, a.TaskID, unixNanos(now)); err != nil {
			return nil, err
		}
		a.Status, a.SideEffectState, a.LeaseUntil, a.FinishedAt = TaskInterrupted, SideEffectUncertain, time.Time{}, now
		out = append(out, a)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// InterruptUnstartedChildren reconciles queued child dispatches at startup.
// Unlike a running attempt, a queued child has no lease or executor that can
// survive process exit. It is never auto-dispatched from durable state.
func (s *SQLiteStore) InterruptUnstartedChildren(now time.Time) ([]*Attempt, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT c.task_id,c.parent_task_id FROM coding_runtime_tasks c JOIN coding_runtime_tasks p ON p.task_id=c.parent_task_id WHERE c.parent_task_id<>'' AND c.status=? AND p.status=? ORDER BY c.parent_task_id,c.task_id`, TaskQueued, TaskWaitingChild)
	if err != nil {
		return nil, err
	}
	type childRef struct{ childID, parentID string }
	var children []childRef
	for rows.Next() {
		var childID, parentID string
		if err := rows.Scan(&childID, &parentID); err != nil {
			rows.Close()
			return nil, err
		}
		children = append(children, childRef{childID: childID, parentID: parentID})
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	parents := map[string]bool{}
	for _, child := range children {
		if _, err := tx.Exec(`UPDATE coding_runtime_tasks SET status=?,updated_at=? WHERE task_id=? AND status=?`, TaskInterrupted, unixNanos(now), child.childID, TaskQueued); err != nil {
			return nil, err
		}
		parents[child.parentID] = true
	}
	out := make([]*Attempt, 0, len(parents))
	for parentID := range parents {
		var attemptID string
		err := tx.QueryRow(`SELECT attempt_id FROM coding_runtime_attempts WHERE task_id=? AND status=? ORDER BY attempt_no DESC LIMIT 1`, parentID, TaskWaitingChild).Scan(&attemptID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		attempt, err := s.getAttempt(tx, attemptID)
		if err != nil {
			return nil, err
		}
		if err := s.interruptWaitingParentTx(tx, parentID, now); err != nil {
			return nil, err
		}
		attempt.Status, attempt.SideEffectState, attempt.FinishedAt = TaskInterrupted, SideEffectNone, now
		out = append(out, attempt)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *SQLiteStore) interruptWaitingParentTx(tx *sql.Tx, parentTaskID string, now time.Time) error {
	var parentStatus string
	err := tx.QueryRow(`SELECT status FROM coding_runtime_tasks WHERE task_id=?`, parentTaskID).Scan(&parentStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil || TaskStatus(parentStatus) != TaskWaitingChild {
		return err
	}
	var attemptID string
	err = tx.QueryRow(`SELECT attempt_id FROM coding_runtime_attempts WHERE task_id=? AND status=? ORDER BY attempt_no DESC LIMIT 1`, parentTaskID, TaskWaitingChild).Scan(&attemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE coding_runtime_tasks SET status=?,updated_at=? WHERE task_id=? AND status=?`, TaskInterrupted, unixNanos(now), parentTaskID, TaskWaitingChild); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE coding_runtime_attempts SET status=?,side_effect_state=?,finished_at=? WHERE attempt_id=? AND status=?`, TaskInterrupted, SideEffectNone, unixNanos(now), attemptID, TaskWaitingChild); err != nil {
		return err
	}
	_, err = s.appendEventTx(tx, attemptID, parentTaskID, "child_lease_expired", "", now)
	return err
}

func (s *SQLiteStore) ListRecoveryCandidates() ([]*Attempt, error) {
	rows, err := s.db.Query(`SELECT attempt_id FROM coding_runtime_attempts WHERE status=? OR side_effect_state=? ORDER BY started_at`, TaskInterrupted, SideEffectUncertain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Attempt
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		a, err := s.getAttempt(s.db, id)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type rowQuerier interface{ QueryRow(string, ...any) *sql.Row }

func (s *SQLiteStore) getAttempt(q rowQuerier, id string) (*Attempt, error) {
	var a Attempt
	var status, side, policy, before, after string
	var lease, started, finished int64
	err := q.QueryRow(`SELECT attempt_id,task_id,attempt_no,lease_owner,lease_until,status,policy_json,side_effect_state,workspace_before_json,workspace_after_json,error_code,error_summary,started_at,finished_at FROM coding_runtime_attempts WHERE attempt_id=?`, id).Scan(&a.AttemptID, &a.TaskID, &a.AttemptNo, &a.LeaseOwner, &lease, &status, &policy, &side, &before, &after, &a.ErrorCode, &a.ErrorSummary, &started, &finished)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	a.LeaseUntil, a.Status, a.SideEffectState, a.StartedAt, a.FinishedAt = fromUnixNanos(lease), TaskStatus(status), SideEffectState(side), fromUnixNanos(started), fromUnixNanos(finished)
	_ = json.Unmarshal([]byte(policy), &a.Policy)
	if before != "" {
		var p WorkspaceProbe
		_ = json.Unmarshal([]byte(before), &p)
		a.WorkspaceBefore = &p
	}
	if after != "" {
		var p WorkspaceProbe
		_ = json.Unmarshal([]byte(after), &p)
		a.WorkspaceAfter = &p
	}
	return &a, nil
}
func unixNanos(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().UnixNano()
}
func fromUnixNanos(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}
func isConstraint(err error) bool {
	return err != nil && (contains(err.Error(), "constraint") || contains(err.Error(), "unique"))
}
func contains(s, part string) bool {
	for i := 0; i+len(part) <= len(s); i++ {
		if s[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
