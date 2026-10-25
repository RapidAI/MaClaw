package codingruntime

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
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
	// SQLite here is one writer. A second pooled connection can still hold an
	// earlier transaction, so the semantic-anchor insert misses the attempt
	// row this process just committed and the implementation turn loses its
	// writable surface. One connection keeps that insert behind the commit.
	db.SetMaxOpenConns(1)
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
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
CREATE TABLE IF NOT EXISTS coding_runtime_semantic_anchors (
 runtime_attempt_id TEXT PRIMARY KEY, runtime_task_id TEXT NOT NULL,
 tenant_id TEXT NOT NULL, principal_id TEXT NOT NULL, session_id TEXT NOT NULL,
 root_task_id TEXT NOT NULL, turn_id TEXT NOT NULL, created_at INTEGER NOT NULL,
 FOREIGN KEY(runtime_task_id) REFERENCES coding_runtime_tasks(task_id),
 FOREIGN KEY(runtime_attempt_id) REFERENCES coding_runtime_attempts(attempt_id)
);
CREATE INDEX IF NOT EXISTS coding_runtime_semantic_anchor_task ON coding_runtime_semantic_anchors(runtime_task_id);`)
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

// activeCodingRuntimeStatusPriority ranks live statuses so a ref with several
// in-flight runs surfaces under its strongest state.
func activeCodingRuntimeStatusPriority(status TaskStatus) int {
	switch status {
	case TaskRunning:
		return 4
	case TaskWaitingApproval, TaskBlocked:
		return 3
	case TaskInterrupted:
		return 2
	case TaskQueued, TaskWaitingChild:
		return 1
	default:
		return 0
	}
}

// activeCodingRuntimeStatusIsLive is a run the user can still see moving or
// still has to decide. Those outrank an older or newer finished outcome.
// Queued is not live: a row that never started must not keep a later write
// looking in progress.
func activeCodingRuntimeStatusIsLive(status TaskStatus) bool {
	switch status {
	case TaskRunning, TaskWaitingChild, TaskWaitingApproval:
		return true
	default:
		return false
	}
}

type activeCodingRuntimeRefChoice struct {
	status    string
	rawStatus string
	updatedAt int64
	live      bool
}

// preferActiveCodingRuntimeStatus keeps a live run ahead of any finished
// outcome. Two finished outcomes follow the newer ledger row, so an old probe
// failure does not hide a later write that already completed.
func preferActiveCodingRuntimeStatus(cur activeCodingRuntimeRefChoice, next activeCodingRuntimeRefChoice) bool {
	if next.live != cur.live {
		return next.live
	}
	if next.live {
		nextRank := activeCodingRuntimeStatusPriority(TaskStatus(next.status))
		curRank := activeCodingRuntimeStatusPriority(TaskStatus(cur.status))
		if nextRank != curRank {
			return nextRank > curRank
		}
	}
	if next.updatedAt != cur.updatedAt {
		return next.updatedAt > cur.updatedAt
	}
	return activeCodingRuntimeStatusPriority(TaskStatus(next.status)) > activeCodingRuntimeStatusPriority(TaskStatus(cur.status))
}

// ListActiveTaskStatusesByProjectRef returns one coding runtime status per
// project_ref. Pure agent loop coding runs (local and remote) never create a
// workflow snapshot, so the task list merges this ledger status into its
// status stats. A newest completed or cancelled row is omitted: the registry
// already shows that write. A newest failed row is kept, because the registry
// only knows that a file exists and would otherwise list the turn as completed.
// A live run or an approval still needs the user and outranks any sibling,
// older or newer. A running row is that live run only while its lease is
// still open, or while this process is still inside the attempt. A dead
// lease is not a holder. StartAttempt and ExpireLeases record that death at
// the lease end. This read applies the same predicate, and ranks that corpse
// at the lease end, so a list drawn before the sweep agrees with one drawn
// after it. ReadTaskList also reports that a dead lease was seen, including a
// child row the list then skips, so the sidebar persists that death only then.
// A later turn on the same directory, including a failed goal, must be able to
// replace it. A child shares the parent's project_ref, so a child's own
// finished row is not the task outcome: its failure would fail the task, and
// its completion would be dropped and take the parent's failure with it. An
// interrupted or still-queued child is the same kind of row: it is newer than
// the parent and would replace that failure. A dead running child is skipped
// the same way, and its waiting parent is ranked interrupted at the earliest
// such child's lease end. A later child does not move that stamp forward. An
// earlier child, noticed afterward, moves it back. This read uses that same
// earliest instant, including when the parent was already stamped later, so
// the list matches the ledger after the sweep. A live child or a gate block
// still counts. Otherwise
// the newest row wins, so a later completed run suppresses an older
// interrupted row instead of letting that stale recovery reminder surface
// forever. A gate block is a failure, except a final_workspace_unchanged row that already
// recorded host file activity: that write is complete and is reported as
// completed.
func (s *SQLiteStore) ListActiveTaskStatusesByProjectRef() (map[string]string, error) {
	statuses, _, err := s.listActiveTaskStatuses(time.Now().UTC())
	return statuses, err
}

// ReadTaskList is the sidebar read. The bool is true when at least one
// running attempt's lease has ended and this process is not inside it,
// including a child row the list then skips. Statuses already rank that
// corpse at the lease end. Call ExpireLeases only when the bool is true.
func (s *SQLiteStore) ReadTaskList(now time.Time) (map[string]string, bool, error) {
	return s.listActiveTaskStatuses(now)
}

func (s *SQLiteStore) listActiveTaskStatuses(now time.Time) (map[string]string, bool, error) {
	if s == nil || s.db == nil {
		return nil, false, nil
	}
	rows, err := s.db.Query(`SELECT t.task_id, t.project_ref, t.status, t.updated_at, t.parent_task_id,
		COALESCE(a.error_code, ''),
		EXISTS(SELECT 1 FROM coding_runtime_events e WHERE e.attempt_id = a.attempt_id AND e.type IN ('file_activity', 'remote_file_activity')),
		COALESCE(a.lease_until, 0),
		COALESCE(a.attempt_id, '')
		FROM coding_runtime_tasks t
		LEFT JOIN coding_runtime_attempts a ON a.task_id = t.task_id
			AND a.attempt_no = (SELECT MAX(attempt_no) FROM coding_runtime_attempts WHERE task_id = t.task_id)`)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	nowUnix := unixNanos(now)
	type statusRow struct {
		taskID, ref, status, parentID, errorCode, attemptID string
		updatedAt, leaseUntil                               int64
		hasFileActivity                                     int
	}
	var scanned []statusRow
	for rows.Next() {
		var row statusRow
		if err := rows.Scan(&row.taskID, &row.ref, &row.status, &row.updatedAt, &row.parentID, &row.errorCode, &row.hasFileActivity, &row.leaseUntil, &row.attemptID); err != nil {
			return nil, false, err
		}
		scanned = append(scanned, row)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	// Same order as ExpireLeases. The first dead child stamps a waiting
	// parent, so the read ranks that parent at the same child's lease end.
	type childLease struct {
		leaseUntil int64
		attemptID  string
		died       int64
	}
	deadChild := map[string]childLease{}
	stale := false
	for _, row := range scanned {
		if row.attemptID == "" || TaskStatus(row.status) != TaskRunning || !runningAttemptDead(fromUnixNanos(row.leaseUntil), now, row.attemptID) {
			continue
		}
		stale = true
		parentID := strings.TrimSpace(row.parentID)
		if parentID == "" {
			continue
		}
		prev, ok := deadChild[parentID]
		if ok && (row.leaseUntil > prev.leaseUntil || (row.leaseUntil == prev.leaseUntil && row.attemptID >= prev.attemptID)) {
			continue
		}
		deadChild[parentID] = childLease{leaseUntil: row.leaseUntil, attemptID: row.attemptID, died: unixNanos(leaseDeathTime(fromUnixNanos(row.leaseUntil), now))}
	}
	choices := map[string]activeCodingRuntimeRefChoice{}
	for _, row := range scanned {
		ref := strings.TrimSpace(row.ref)
		if ref == "" {
			continue
		}
		status := row.status
		updatedAt := row.updatedAt
		// Only the latest attempt counts. An earlier write must not turn a later
		// unchanged-workspace block into a completed run.
		rawStatus := status
		if TaskStatus(status) == TaskBlocked && row.errorCode == "final_workspace_unchanged" && row.hasFileActivity != 0 {
			status = string(TaskCompleted)
		}
		// Same dead-lease rule as StartAttempt and ExpireLeases. A suspended
		// executor is still in localWriterLive, so its expired clock stays
		// running here too. Ranking uses the lease end, which is the instant
		// the sweep writes onto the task. A missing lease has no death time
		// to rank, so that row keeps the task's own updated_at.
		if row.attemptID != "" && TaskStatus(status) == TaskRunning && runningAttemptDead(fromUnixNanos(row.leaseUntil), now, row.attemptID) {
			status = string(TaskInterrupted)
			if row.leaseUntil > 0 && row.leaseUntil <= nowUnix {
				updatedAt = row.leaseUntil
			}
		}
		// The sweep stamps the parent at the earliest dead child's lease end.
		// A stamp that landed later is ranked back at that earlier instant.
		// It is not persisted here.
		if death, ok := deadChild[row.taskID]; ok {
			switch TaskStatus(row.status) {
			case TaskWaitingChild:
				status = string(TaskInterrupted)
				updatedAt = death.died
			case TaskInterrupted:
				if death.died < updatedAt {
					updatedAt = death.died
				}
			}
		}
		// A child shares the parent's project_ref. A finished child is not the
		// task outcome. Skipping it after the rewrite also ignores a child
		// whose unchanged-workspace block was reported as completed. The dead
		// child was already counted above, so skipping it still wakes the sweep.
		if strings.TrimSpace(row.parentID) != "" {
			switch TaskStatus(status) {
			case TaskFailed, TaskCompleted, TaskCancelled, TaskInterrupted, TaskQueued:
				continue
			}
		}
		next := activeCodingRuntimeRefChoice{status: status, rawStatus: rawStatus, updatedAt: updatedAt, live: activeCodingRuntimeStatusIsLive(TaskStatus(status))}
		if cur, ok := choices[ref]; !ok || preferActiveCodingRuntimeStatus(cur, next) {
			choices[ref] = next
		}
	}
	byRef := make(map[string]string, len(choices))
	for ref, choice := range choices {
		// Drop a newest completed or cancelled row: the task list already shows
		// that write. Keep a newest failure, and keep a blocked row that was
		// rewritten to completed (raw status is still blocked).
		switch TaskStatus(choice.rawStatus) {
		case TaskCompleted, TaskCancelled:
			continue
		}
		byRef[ref] = choice.status
	}
	return byRef, stale, nil
}

func (s *SQLiteStore) ListChildTasks(parentTaskID string) ([]*Task, error) {
	if _, err := s.GetTask(parentTaskID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT task_id FROM coding_runtime_tasks WHERE parent_task_id=? ORDER BY task_id`, parentTaskID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// The ledger keeps one connection. Loading each task while this cursor is
	// open waits for that connection and never returns.
	out := make([]*Task, 0, len(ids))
	for _, id := range ids {
		task, err := s.getTask(s.db, id)
		if err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, nil
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
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]*Attempt, 0, len(ids))
	for _, id := range ids {
		attempt, err := s.getAttempt(s.db, id)
		if err != nil {
			return nil, err
		}
		out = append(out, attempt)
	}
	return out, nil
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
		// A live lease, or this process still inside the attempt, really is
		// held. That first read takes no write lock. An expired lease with no
		// local executor is the same death ExpireLeases records. Another
		// process can renew between that read and the interrupt, so a dead
		// clock is taken again under the admission guard. Commit only when
		// the interrupt wrote a row. A no-op leaves the lease held, and the
		// rollback drops the guard bump.
		attemptID, leaseUntil, qerr := runningAttemptClock(tx, taskID)
		if qerr != nil {
			return nil, qerr
		}
		if attemptID == "" || !runningAttemptDead(fromUnixNanos(leaseUntil), now, attemptID) {
			return nil, ErrLeaseHeld
		}
		if _, err = tx.Exec(`UPDATE coding_runtime_writer_admission_guard SET revision=revision+1 WHERE guard_id=1`); err != nil {
			return nil, err
		}
		attemptID, leaseUntil, qerr = runningAttemptClock(tx, taskID)
		if qerr != nil {
			return nil, qerr
		}
		if attemptID == "" || !runningAttemptDead(fromUnixNanos(leaseUntil), now, attemptID) {
			return nil, ErrLeaseHeld
		}
		interrupted, ierr := s.interruptExpiredAttemptTx(tx, attemptID, now)
		if ierr != nil {
			return nil, ierr
		}
		if interrupted == nil {
			return nil, ErrLeaseHeld
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%w: start attempt", ErrInvalidTransition)
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
	rows, err := tx.Query(`SELECT a.attempt_id,a.policy_json,a.task_id,a.lease_until FROM coding_runtime_attempts a WHERE a.status=?`, TaskRunning)
	if err != nil {
		return nil, err
	}
	type runningAttempt struct {
		attemptID  string
		policyJSON string
		taskID     string
		leaseUntil int64
	}
	var running []runningAttempt
	for rows.Next() {
		var row runningAttempt
		if err = rows.Scan(&row.attemptID, &row.policyJSON, &row.taskID, &row.leaseUntil); err != nil {
			rows.Close()
			return nil, err
		}
		running = append(running, row)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	// One ledger connection. The task lookup has to wait until this cursor is
	// closed, or admission blocks and a second writer never sees the conflict.
	// Dead holders are interrupted only after every live holder has been
	// checked, and in this same transaction, so a rejected writer does not
	// leave a half-applied death behind.
	// Earliest lease end first, then attempt id. That is the child that stamps
	// a waiting parent. ExpireLeases uses the same order.
	type deadAttempt struct {
		id         string
		leaseUntil int64
	}
	var deadAttempts []deadAttempt
	for _, row := range running {
		if row.taskID == taskID {
			continue
		}
		if runningAttemptDead(fromUnixNanos(row.leaseUntil), now, row.attemptID) {
			deadAttempts = append(deadAttempts, deadAttempt{id: row.attemptID, leaseUntil: row.leaseUntil})
			continue
		}
		var otherPolicy PolicySnapshot
		if err = json.Unmarshal([]byte(row.policyJSON), &otherPolicy); err != nil {
			return nil, err
		}
		otherTask, getErr := s.getTask(tx, row.taskID)
		if getErr != nil {
			return nil, getErr
		}
		if conflict := WriterAdmissionConflict(*task, policy, *otherTask, otherPolicy); conflict.Conflicts {
			return nil, WriterAdmissionError{Conflict: conflict}
		}
	}
	sort.Slice(deadAttempts, func(i, j int) bool {
		if deadAttempts[i].leaseUntil != deadAttempts[j].leaseUntil {
			return deadAttempts[i].leaseUntil < deadAttempts[j].leaseUntil
		}
		return deadAttempts[i].id < deadAttempts[j].id
	})
	for _, dead := range deadAttempts {
		id := dead.id
		interrupted, ierr := s.interruptExpiredAttemptTx(tx, id, now)
		if ierr != nil {
			return nil, ierr
		}
		if interrupted != nil {
			continue
		}
		// The holder was renewed, or this process entered it, between the
		// scan and the interrupt. Admitting beside a still-running attempt
		// would leave two writers. A row that already left running is not a
		// holder. Rolling back drops every interrupt this transaction wrote.
		var attemptStatus string
		qerr := tx.QueryRow(`SELECT status FROM coding_runtime_attempts WHERE attempt_id=?`, id).Scan(&attemptStatus)
		if errors.Is(qerr, sql.ErrNoRows) {
			continue
		}
		if qerr != nil {
			return nil, qerr
		}
		if TaskStatus(attemptStatus) == TaskRunning {
			return nil, ErrLeaseHeld
		}
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

func (s *SQLiteStore) RenewAttemptLease(attemptID, leaseOwner string, leaseFor time.Duration, now time.Time) error {
	if leaseOwner == "" || leaseFor <= 0 {
		return fmt.Errorf("%w: renew attempt lease", ErrInvalidTransition)
	}
	res, err := s.db.Exec(`UPDATE coding_runtime_attempts SET lease_until=? WHERE attempt_id=? AND lease_owner=? AND status=?`, unixNanos(now.Add(leaseFor)), attemptID, leaseOwner, TaskRunning)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	attempt, getErr := s.GetAttempt(attemptID)
	if getErr != nil {
		return getErr
	}
	if attempt.LeaseOwner != leaseOwner {
		return ErrLeaseOwnerMismatch
	}
	if attempt.Status != TaskRunning {
		return ErrAttemptNotRunning
	}
	return ErrNotFound
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
	if err = rows.Err(); err != nil {
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
		if queryErr = attemptRows.Err(); queryErr != nil {
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
	if s == nil || s.db == nil {
		return nil, nil
	}
	// Startup sweeps unconditionally. A task-list read calls this only after it
	// has already seen a dead lease. This store has one connection. A write
	// transaction here would queue behind, and in front of, the live coding
	// writer even when the re-check finds every lease still open. Collect the
	// dead ids first and skip the transaction when none remain.
	// lease_until then attempt id matches the parent stamp the task list ranks.
	// The first dead child in this order is the one that interrupts a waiting parent.
	rows, err := s.db.Query(`SELECT attempt_id FROM coding_runtime_attempts WHERE status=? AND lease_until<=? ORDER BY lease_until ASC, attempt_id ASC`, TaskRunning, unixNanos(now))
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
		// The process can still be inside an attempt whose stored clock has
		// passed. That holder stays running; a startup sweep has an empty set.
		if localWriterLive(id) {
			continue
		}
		ids = append(ids, id)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var out []*Attempt
	for _, id := range ids {
		a, err := s.interruptExpiredAttemptTx(tx, id, now)
		if err != nil {
			return nil, err
		}
		if a != nil {
			out = append(out, a)
		}
	}
	if len(out) == 0 {
		return nil, nil
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
	if err := rows.Err(); err != nil {
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

// interruptExpiredAttemptTx closes a running attempt whose lease has ended
// and whose executor is not on this process. finished_at and the owning
// task's updated_at are the lease end (or now, when the lease has no end),
// so noticing the row later does not move it in front of a newer outcome.
// The task update matches only status=running, so a task that already
// reached a later status is not rewritten.
func (s *SQLiteStore) interruptExpiredAttemptTx(tx *sql.Tx, attemptID string, now time.Time) (*Attempt, error) {
	// The attempt row carries the workspace probes. Lease death only needs
	// the status, the lease end, and the parent link.
	var taskID, status, parentID string
	var leaseUntil int64
	err := tx.QueryRow(`SELECT a.task_id, a.status, a.lease_until, t.parent_task_id FROM coding_runtime_attempts a JOIN coding_runtime_tasks t ON t.task_id=a.task_id WHERE a.attempt_id=?`, attemptID).Scan(&taskID, &status, &leaseUntil, &parentID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	leaseTime := fromUnixNanos(leaseUntil)
	if TaskStatus(status) != TaskRunning || !runningAttemptDead(leaseTime, now, attemptID) {
		return nil, nil
	}
	died := leaseDeathTime(leaseTime, now)
	res, err := tx.Exec(`UPDATE coding_runtime_attempts SET status=?,side_effect_state=?,lease_until=0,finished_at=? WHERE attempt_id=? AND status=?`, TaskInterrupted, SideEffectUncertain, unixNanos(died), attemptID, TaskRunning)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	if _, err = tx.Exec(`UPDATE coding_runtime_tasks SET status=?,updated_at=? WHERE task_id=? AND status=?`, TaskInterrupted, unixNanos(died), taskID, TaskRunning); err != nil {
		return nil, err
	}
	// A parent without a lease waits only for child delivery. When the
	// process loses a running child, interrupt that parent too so recovery
	// is explicit instead of leaving it permanently waiting_child.
	if parentID != "" {
		if err = s.interruptWaitingParentTx(tx, parentID, died); err != nil {
			return nil, err
		}
	}
	if _, err = s.appendEventTx(tx, attemptID, taskID, "lease_expired", "", died); err != nil {
		return nil, err
	}
	return &Attempt{AttemptID: attemptID, TaskID: taskID, Status: TaskInterrupted, SideEffectState: SideEffectUncertain, FinishedAt: died}, nil
}

func (s *SQLiteStore) interruptWaitingParentTx(tx *sql.Tx, parentTaskID string, now time.Time) error {
	var parentStatus string
	var updatedAt int64
	err := tx.QueryRow(`SELECT status, updated_at FROM coding_runtime_tasks WHERE task_id=?`, parentTaskID).Scan(&parentStatus, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	// A child noticed later can have an earlier lease end. The parent stays
	// interrupted, and the stamp moves back to that lease end when it is
	// still the task's current time. A newer write on the parent is left
	// alone. A later child never moves the stamp forward.
	if TaskStatus(parentStatus) == TaskInterrupted {
		var attemptID string
		var finishedAt int64
		err = tx.QueryRow(`SELECT a.attempt_id, a.finished_at FROM coding_runtime_attempts a JOIN coding_runtime_events e ON e.attempt_id=a.attempt_id AND e.type='child_lease_expired' WHERE a.task_id=? AND a.status=? ORDER BY a.attempt_no DESC LIMIT 1`, parentTaskID, TaskInterrupted).Scan(&attemptID, &finishedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		died := unixNanos(now)
		if finishedAt <= died || updatedAt != finishedAt {
			return nil
		}
		if _, err = tx.Exec(`UPDATE coding_runtime_tasks SET updated_at=? WHERE task_id=? AND status=? AND updated_at=?`, died, parentTaskID, TaskInterrupted, finishedAt); err != nil {
			return err
		}
		if _, err = tx.Exec(`UPDATE coding_runtime_attempts SET finished_at=? WHERE attempt_id=? AND finished_at=?`, died, attemptID, finishedAt); err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE coding_runtime_events SET created_at=? WHERE attempt_id=? AND type='child_lease_expired' AND created_at=?`, died, attemptID, finishedAt)
		return err
	}
	if TaskStatus(parentStatus) != TaskWaitingChild {
		return nil
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
	// Queued siblings never took a lease. This is the same moment the parent
	// stops being able to dispatch them, so they must not stay startable.
	// A sibling that is still running keeps its own lease.
	if _, err = tx.Exec(`UPDATE coding_runtime_tasks SET status=?,updated_at=? WHERE parent_task_id=? AND status=?`, TaskInterrupted, unixNanos(now), parentTaskID, TaskQueued); err != nil {
		return err
	}
	_, err = s.appendEventTx(tx, attemptID, parentTaskID, "child_lease_expired", "", now)
	return err
}

func (s *SQLiteStore) ListRecoveryCandidates() ([]*Attempt, error) {
	// A candidate whose logical task already ended (completed, failed, or
	// cancelled — e.g. a declined recovery) is history, not a recovery offer.
	rows, err := s.db.Query(`SELECT a.attempt_id FROM coding_runtime_attempts a
		JOIN coding_runtime_tasks t ON t.task_id = a.task_id
		WHERE (a.status = ? OR a.side_effect_state = ?) AND t.status NOT IN (?, ?, ?)
		ORDER BY a.started_at`, TaskInterrupted, SideEffectUncertain, TaskCompleted, TaskFailed, TaskCancelled)
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
	if err = rows.Err(); err != nil {
		return nil, err
	}
	out := make([]*Attempt, 0, len(ids))
	for _, id := range ids {
		a, err := s.getAttempt(s.db, id)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func (s *SQLiteStore) RegisterSemanticTaskAnchor(anchor SemanticTaskAnchor) (*SemanticTaskAnchor, error) {
	normalized, err := normalizeSemanticTaskAnchor(anchor)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var taskID string
	if err = tx.QueryRow(`SELECT task_id FROM coding_runtime_attempts WHERE attempt_id=?`, normalized.RuntimeAttemptID).Scan(&taskID); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSemanticAnchorNotFound
	} else if err != nil {
		return nil, err
	}
	if taskID != normalized.RuntimeTaskID {
		return nil, ErrSemanticAnchorNotFound
	}
	var existing SemanticTaskAnchor
	var createdAt int64
	err = tx.QueryRow(`SELECT runtime_task_id,tenant_id,principal_id,session_id,root_task_id,turn_id,created_at FROM coding_runtime_semantic_anchors WHERE runtime_attempt_id=?`, normalized.RuntimeAttemptID).Scan(&existing.RuntimeTaskID, &existing.TenantID, &existing.PrincipalID, &existing.SessionID, &existing.RootTaskID, &existing.TurnID, &createdAt)
	if err == nil {
		existing.RuntimeAttemptID, existing.CreatedAt = normalized.RuntimeAttemptID, fromUnixNanos(createdAt)
		if existing.RuntimeTaskID == normalized.RuntimeTaskID && existing.TenantID == normalized.TenantID && existing.PrincipalID == normalized.PrincipalID && existing.SessionID == normalized.SessionID && existing.RootTaskID == normalized.RootTaskID && existing.TurnID == normalized.TurnID {
			if err = tx.Commit(); err != nil {
				return nil, err
			}
			return &existing, nil
		}
		return nil, ErrSemanticAnchorConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var conflict int
	if err = tx.QueryRow(`SELECT COUNT(1) FROM coding_runtime_semantic_anchors WHERE runtime_task_id=? AND (tenant_id<>? OR principal_id<>? OR session_id<>? OR root_task_id<>?)`, normalized.RuntimeTaskID, normalized.TenantID, normalized.PrincipalID, normalized.SessionID, normalized.RootTaskID).Scan(&conflict); err != nil {
		return nil, err
	}
	if conflict != 0 {
		return nil, ErrSemanticAnchorConflict
	}
	if _, err = tx.Exec(`INSERT INTO coding_runtime_semantic_anchors(runtime_attempt_id,runtime_task_id,tenant_id,principal_id,session_id,root_task_id,turn_id,created_at) VALUES(?,?,?,?,?,?,?,?)`, normalized.RuntimeAttemptID, normalized.RuntimeTaskID, normalized.TenantID, normalized.PrincipalID, normalized.SessionID, normalized.RootTaskID, normalized.TurnID, unixNanos(normalized.CreatedAt)); err != nil {
		if isConstraint(err) {
			return nil, ErrSemanticAnchorConflict
		}
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &normalized, nil
}

func (s *SQLiteStore) ResolveSemanticTaskAnchor(runtimeTaskID, runtimeAttemptID string) (*SemanticTaskAnchor, error) {
	runtimeTaskID, runtimeAttemptID = strings.TrimSpace(runtimeTaskID), strings.TrimSpace(runtimeAttemptID)
	if runtimeTaskID == "" || runtimeAttemptID == "" {
		return nil, ErrSemanticAnchorNotFound
	}
	var anchor SemanticTaskAnchor
	var createdAt int64
	err := s.db.QueryRow(`SELECT runtime_task_id,runtime_attempt_id,tenant_id,principal_id,session_id,root_task_id,turn_id,created_at FROM coding_runtime_semantic_anchors WHERE runtime_task_id=? AND runtime_attempt_id=?`, runtimeTaskID, runtimeAttemptID).Scan(&anchor.RuntimeTaskID, &anchor.RuntimeAttemptID, &anchor.TenantID, &anchor.PrincipalID, &anchor.SessionID, &anchor.RootTaskID, &anchor.TurnID, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSemanticAnchorNotFound
	}
	if err != nil {
		return nil, err
	}
	anchor.CreatedAt = fromUnixNanos(createdAt)
	return &anchor, nil
}

type rowQuerier interface{ QueryRow(string, ...any) *sql.Row }

// runningAttemptClock reads the newest running attempt on a task. An empty
// attempt id means that task has no running attempt.
func runningAttemptClock(q rowQuerier, taskID string) (attemptID string, leaseUntil int64, err error) {
	err = q.QueryRow(`SELECT attempt_id, lease_until FROM coding_runtime_attempts WHERE task_id=? AND status=? ORDER BY attempt_no DESC LIMIT 1`, taskID, TaskRunning).Scan(&attemptID, &leaseUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, nil
	}
	return attemptID, leaseUntil, err
}

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
