package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// R1a owns the semantic task relation before a Coding runtime attempt exists.
// It intentionally does not reuse RuntimeContext, LoopContext, UserID, a
// workspace path, or a runtime task ID: all of those are routing/transport
// facts, not proof of a principal's task-session relation.
var (
	errVerifiedCodingSubjectInvalid = errors.New("verified coding subject is invalid")
	errCodingTaskHandleNotFound     = errors.New("verified coding task handle not found")
	errCodingTaskHandleForbidden    = errors.New("verified coding task handle scope mismatch")
	errCodingTaskHandleConsumed     = errors.New("verified coding task handle already consumed")
	errCodingTaskHandleRevoked      = errors.New("verified coding task handle is revoked")
	errCodingTaskHandleExpired      = errors.New("verified coding task handle is expired")
)

// verifiedCodingSubject may only be made by the authenticated host adapter.
// The type and constructor are package-private so model callbacks, Wails
// payloads and CodingSubAgent itself never receive a stringly-typed identity
// construction API. A production adapter must obtain these three fields from
// distinct authentication/session authorities before calling this constructor.
type verifiedCodingSubject struct {
	tenantID    string
	principalID string
	sessionID   string
}

func newVerifiedCodingSubject(tenantID, principalID, sessionID string) (verifiedCodingSubject, error) {
	subject := verifiedCodingSubject{
		tenantID:    strings.TrimSpace(tenantID),
		principalID: strings.TrimSpace(principalID),
		sessionID:   strings.TrimSpace(sessionID),
	}
	// Equal values are a common compatibility shortcut (for example copying a
	// UserID into both fields). Reject it at the only issuance boundary rather
	// than relying on later callers to remember the distinction.
	if subject.tenantID == "" || subject.principalID == "" || subject.sessionID == "" || subject.principalID == subject.sessionID {
		return verifiedCodingSubject{}, errVerifiedCodingSubjectInvalid
	}
	return subject, nil
}

type codingTaskRelationStatus string

const (
	codingTaskRelationActive   codingTaskRelationStatus = "active"
	codingTaskRelationConsumed codingTaskRelationStatus = "consumed"
	codingTaskRelationRevoked  codingTaskRelationStatus = "revoked"
	codingTaskRelationExpired  codingTaskRelationStatus = "expired"
)

// verifiedCodingTaskHandle is an opaque, durable host capability. Its handle
// ID is not a model parameter, a root ID supplied by UI, or a continuation
// assertion: every use is rechecked against the persisted subject scope and
// lifecycle state.
type verifiedCodingTaskHandle struct {
	handleID    string
	tenantID    string
	principalID string
	sessionID   string
	rootTaskID  string
	turnID      string
	expiresAt   time.Time
}

func (h verifiedCodingTaskHandle) complete() bool {
	return strings.TrimSpace(h.handleID) != "" && strings.TrimSpace(h.tenantID) != "" &&
		strings.TrimSpace(h.principalID) != "" && strings.TrimSpace(h.sessionID) != "" &&
		strings.TrimSpace(h.rootTaskID) != "" && strings.TrimSpace(h.turnID) != "" && !h.expiresAt.IsZero()
}

// codingTaskRelationService is deliberately separate from the execution
// ledger. R1a creates/verifies/revokes semantic task handles; R1b will bind a
// verified handle to a fresh runtime attempt and register SemanticTaskAnchor.
// Keeping the boundary explicit prevents a runtime task from becoming a
// backdoor creator of semantic roots.
type codingTaskRelationService struct{ db *sql.DB }

func newCodingTaskRelationService(dbPath string) (*codingTaskRelationService, error) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return nil, fmt.Errorf("coding task relation database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("create coding task relation directory: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	service := &codingTaskRelationService{db: db}
	if err := service.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return service, nil
}

func (s *codingTaskRelationService) migrate() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("coding task relation service is unavailable")
	}
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS coding_task_relations (
 handle_id TEXT PRIMARY KEY,
 tenant_id TEXT NOT NULL,
 principal_id TEXT NOT NULL,
 session_id TEXT NOT NULL,
 root_task_id TEXT NOT NULL,
 turn_id TEXT NOT NULL,
 parent_handle_id TEXT NOT NULL,
 status TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 expires_at INTEGER NOT NULL,
 consumed_at INTEGER NOT NULL,
 revoked_at INTEGER NOT NULL,
 UNIQUE(root_task_id, turn_id)
);
CREATE INDEX IF NOT EXISTS coding_task_relations_scope ON coding_task_relations(tenant_id, principal_id, session_id, root_task_id);
CREATE INDEX IF NOT EXISTS coding_task_relations_parent ON coding_task_relations(parent_handle_id);
CREATE TABLE IF NOT EXISTS coding_task_attempt_bindings (
 handle_id TEXT PRIMARY KEY,
 runtime_task_id TEXT NOT NULL,
 runtime_attempt_id TEXT NOT NULL UNIQUE,
 bound_at INTEGER NOT NULL,
 FOREIGN KEY(handle_id) REFERENCES coding_task_relations(handle_id)
);
CREATE INDEX IF NOT EXISTS coding_task_attempt_bindings_task ON coding_task_attempt_bindings(runtime_task_id);`)
	return err
}

func (s *codingTaskRelationService) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// CreateCodingTask issues a new semantic root and first turn. RootTaskID and
// TurnID are generated here, never accepted from the UI/agent/runtime.
func (s *codingTaskRelationService) CreateCodingTask(subject verifiedCodingSubject, now time.Time, ttl time.Duration) (verifiedCodingTaskHandle, error) {
	if !validVerifiedCodingSubject(subject) {
		return verifiedCodingTaskHandle{}, errVerifiedCodingSubjectInvalid
	}
	return s.insertHandle(subject, uuid.NewString(), uuid.NewString(), "", now, ttl)
}

// VerifyCodingContinuation consumes exactly one active task handle and issues
// a fresh turn for the same root. This gives retry/amendment flows an explicit
// continuation proof and makes replaying an old handle fail closed.
func (s *codingTaskRelationService) VerifyCodingContinuation(subject verifiedCodingSubject, previous verifiedCodingTaskHandle, now time.Time, ttl time.Duration) (verifiedCodingTaskHandle, error) {
	if !validVerifiedCodingSubject(subject) {
		return verifiedCodingTaskHandle{}, errVerifiedCodingSubjectInvalid
	}
	if s == nil || s.db == nil || !previous.complete() {
		return verifiedCodingTaskHandle{}, errCodingTaskHandleNotFound
	}
	now = normalizedCodingRelationTime(now)
	if ttl <= 0 {
		return verifiedCodingTaskHandle{}, fmt.Errorf("coding task handle ttl is required")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return verifiedCodingTaskHandle{}, err
	}
	defer tx.Rollback()
	current, status, err := loadCodingTaskHandle(tx, previous.handleID)
	if err != nil {
		return verifiedCodingTaskHandle{}, err
	}
	if err := authorizeCodingTaskHandle(subject, previous, current, status, now); err != nil {
		return verifiedCodingTaskHandle{}, err
	}
	if _, err = tx.Exec(`UPDATE coding_task_relations SET status=?,consumed_at=? WHERE handle_id=? AND status=?`, codingTaskRelationConsumed, now.UnixNano(), current.handleID, codingTaskRelationActive); err != nil {
		return verifiedCodingTaskHandle{}, err
	}
	next := verifiedCodingTaskHandle{
		handleID: uuid.NewString(), tenantID: subject.tenantID, principalID: subject.principalID, sessionID: subject.sessionID,
		rootTaskID: current.rootTaskID, turnID: uuid.NewString(), expiresAt: now.Add(ttl),
	}
	if _, err = tx.Exec(`INSERT INTO coding_task_relations(handle_id,tenant_id,principal_id,session_id,root_task_id,turn_id,parent_handle_id,status,created_at,expires_at,consumed_at,revoked_at) VALUES(?,?,?,?,?,?,?,?,?,?,0,0)`,
		next.handleID, next.tenantID, next.principalID, next.sessionID, next.rootTaskID, next.turnID, current.handleID, codingTaskRelationActive, now.UnixNano(), next.expiresAt.UnixNano()); err != nil {
		return verifiedCodingTaskHandle{}, err
	}
	if err = tx.Commit(); err != nil {
		return verifiedCodingTaskHandle{}, err
	}
	return next, nil
}

// IssueChildCodingTurn derives a separately auditable child turn after the
// runtime has admitted that child. It retains the already-verified parent
// scope and root, but never copies a parent grant, alias, request surface or
// TurnID. Parent handles remain active: one parent may admit several bounded
// read-only children, each with its own child lineage record.
func (s *codingTaskRelationService) IssueChildCodingTurn(subject verifiedCodingSubject, parent verifiedCodingTaskHandle, now time.Time, ttl time.Duration) (verifiedCodingTaskHandle, error) {
	if !validVerifiedCodingSubject(subject) {
		return verifiedCodingTaskHandle{}, errVerifiedCodingSubjectInvalid
	}
	if s == nil || s.db == nil || !parent.complete() {
		return verifiedCodingTaskHandle{}, errCodingTaskHandleNotFound
	}
	now = normalizedCodingRelationTime(now)
	if ttl <= 0 {
		return verifiedCodingTaskHandle{}, fmt.Errorf("coding task handle ttl is required")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return verifiedCodingTaskHandle{}, err
	}
	defer tx.Rollback()
	current, status, err := loadCodingTaskHandle(tx, parent.handleID)
	if err != nil {
		return verifiedCodingTaskHandle{}, err
	}
	if err := authorizeCodingTaskHandle(subject, parent, current, status, now); err != nil {
		return verifiedCodingTaskHandle{}, err
	}
	child := verifiedCodingTaskHandle{
		handleID: uuid.NewString(), tenantID: current.tenantID, principalID: current.principalID, sessionID: current.sessionID,
		rootTaskID: current.rootTaskID, turnID: uuid.NewString(), expiresAt: now.Add(ttl),
	}
	if _, err = tx.Exec(`INSERT INTO coding_task_relations(handle_id,tenant_id,principal_id,session_id,root_task_id,turn_id,parent_handle_id,status,created_at,expires_at,consumed_at,revoked_at) VALUES(?,?,?,?,?,?,?,?,?,?,0,0)`,
		child.handleID, child.tenantID, child.principalID, child.sessionID, child.rootTaskID, child.turnID, current.handleID, codingTaskRelationActive, now.UnixNano(), child.expiresAt.UnixNano()); err != nil {
		return verifiedCodingTaskHandle{}, err
	}
	if err = tx.Commit(); err != nil {
		return verifiedCodingTaskHandle{}, err
	}
	return child, nil
}

// RevokeCodingTaskHandle is the R1a cancellation boundary. A revoked handle
// cannot later be used for continuation or R1b attempt binding.
func (s *codingTaskRelationService) RevokeCodingTaskHandle(subject verifiedCodingSubject, handle verifiedCodingTaskHandle, now time.Time) error {
	if !validVerifiedCodingSubject(subject) {
		return errVerifiedCodingSubjectInvalid
	}
	if s == nil || s.db == nil || !handle.complete() {
		return errCodingTaskHandleNotFound
	}
	now = normalizedCodingRelationTime(now)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, status, err := loadCodingTaskHandle(tx, handle.handleID)
	if err != nil {
		return err
	}
	if current.tenantID != subject.tenantID || current.principalID != subject.principalID || current.sessionID != subject.sessionID ||
		handle.tenantID != current.tenantID || handle.principalID != current.principalID || handle.sessionID != current.sessionID || handle.rootTaskID != current.rootTaskID || handle.turnID != current.turnID {
		return errCodingTaskHandleForbidden
	}
	if status == codingTaskRelationRevoked {
		return errCodingTaskHandleRevoked
	}
	// Revocation is cancellation, not continuation: it must remain available
	// after a parent was consumed into a later turn and must fence every active
	// child descendant as well. Otherwise a child issued before cancellation
	// could obtain a new runtime attempt after its parent task was stopped.
	if _, err = tx.Exec(`WITH RECURSIVE descendants(handle_id) AS (
 SELECT handle_id FROM coding_task_relations WHERE handle_id=?
 UNION ALL
 SELECT relation.handle_id FROM coding_task_relations relation
 JOIN descendants parent ON relation.parent_handle_id=parent.handle_id
)
UPDATE coding_task_relations SET status=?,revoked_at=?
WHERE handle_id IN descendants AND status=?`, current.handleID, codingTaskRelationRevoked, now.UnixNano(), codingTaskRelationActive); err != nil {
		return err
	}
	return tx.Commit()
}

// BindCodingAttempt is R1b's sole bridge from an authenticated task relation
// to a runtime attempt. It verifies the opaque handle in its durable scope,
// fences it to exactly one fresh attempt, and then registers the resulting
// anchor. Neither the runtime nor the agent is allowed to supply root/turn
// fields directly. Repeating the same binding is idempotent; any drift in the
// attempt or anchor is rejected.
func (s *codingTaskRelationService) BindCodingAttempt(subject verifiedCodingSubject, handle verifiedCodingTaskHandle, store codingruntime.Store, request codingruntime.ExecutionRequest, now time.Time) (*trustedCodingInvocationIdentity, error) {
	if !validVerifiedCodingSubject(subject) {
		return nil, errVerifiedCodingSubjectInvalid
	}
	if s == nil || s.db == nil || !handle.complete() || store == nil || strings.TrimSpace(request.Task.TaskID) == "" || strings.TrimSpace(request.Attempt.AttemptID) == "" || request.Attempt.TaskID != request.Task.TaskID {
		return nil, errCodingTaskHandleNotFound
	}
	anchors, ok := store.(codingruntime.SemanticTaskAnchorStore)
	if !ok || anchors == nil {
		return nil, fmt.Errorf("coding runtime semantic anchor store is unavailable")
	}
	now = normalizedCodingRelationTime(now)
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	current, status, err := loadCodingTaskHandle(tx, handle.handleID)
	if err != nil {
		return nil, err
	}
	if err := authorizeCodingTaskHandle(subject, handle, current, status, now); err != nil {
		if errors.Is(err, errCodingTaskHandleExpired) && status == codingTaskRelationActive {
			_, _ = tx.Exec(`UPDATE coding_task_relations SET status=? WHERE handle_id=? AND status=?`, codingTaskRelationExpired, current.handleID, codingTaskRelationActive)
			_ = tx.Commit()
		}
		return nil, err
	}
	var boundTaskID, boundAttemptID string
	err = tx.QueryRow(`SELECT runtime_task_id,runtime_attempt_id FROM coding_task_attempt_bindings WHERE handle_id=?`, current.handleID).Scan(&boundTaskID, &boundAttemptID)
	if err == nil {
		if boundTaskID != request.Task.TaskID || boundAttemptID != request.Attempt.AttemptID {
			return nil, errCodingTaskHandleConsumed
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		if _, err = tx.Exec(`INSERT INTO coding_task_attempt_bindings(handle_id,runtime_task_id,runtime_attempt_id,bound_at) VALUES(?,?,?,?)`, current.handleID, request.Task.TaskID, request.Attempt.AttemptID, now.UnixNano()); err != nil {
			return nil, errCodingTaskHandleConsumed
		}
	} else {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	identity := &trustedCodingInvocationIdentity{
		TenantID: current.tenantID, PrincipalID: current.principalID, SessionID: current.sessionID,
		RootTaskID: current.rootTaskID, TurnID: current.turnID,
	}
	if !registerTrustedCodingInvocationIdentity(store, request, identity) {
		// A same-attempt retry may arrive after the anchor was already created;
		// stores use independently generated CreatedAt, so structural equality
		// is not a safe idempotence test. Resolve and compare only the immutable
		// semantic identity before deciding that the bind drifted.
		if resolved, ok := resolveTrustedCodingInvocationIdentity(store, request); !ok || resolved == nil || *resolved != *identity {
			return nil, fmt.Errorf("register coding semantic task anchor")
		}
	}
	resolved, ok := resolveTrustedCodingInvocationIdentity(store, request)
	if !ok || resolved == nil || *resolved != *identity {
		return nil, fmt.Errorf("resolve coding semantic task anchor")
	}
	return resolved, nil
}

func (s *codingTaskRelationService) insertHandle(subject verifiedCodingSubject, rootTaskID, turnID, parentHandleID string, now time.Time, ttl time.Duration) (verifiedCodingTaskHandle, error) {
	if s == nil || s.db == nil {
		return verifiedCodingTaskHandle{}, fmt.Errorf("coding task relation service is unavailable")
	}
	if ttl <= 0 {
		return verifiedCodingTaskHandle{}, fmt.Errorf("coding task handle ttl is required")
	}
	now = normalizedCodingRelationTime(now)
	handle := verifiedCodingTaskHandle{
		handleID: uuid.NewString(), tenantID: subject.tenantID, principalID: subject.principalID, sessionID: subject.sessionID,
		rootTaskID: rootTaskID, turnID: turnID, expiresAt: now.Add(ttl),
	}
	_, err := s.db.Exec(`INSERT INTO coding_task_relations(handle_id,tenant_id,principal_id,session_id,root_task_id,turn_id,parent_handle_id,status,created_at,expires_at,consumed_at,revoked_at) VALUES(?,?,?,?,?,?,?,?,?,?,0,0)`,
		handle.handleID, handle.tenantID, handle.principalID, handle.sessionID, handle.rootTaskID, handle.turnID, parentHandleID, codingTaskRelationActive, now.UnixNano(), handle.expiresAt.UnixNano())
	if err != nil {
		return verifiedCodingTaskHandle{}, err
	}
	return handle, nil
}

func validVerifiedCodingSubject(subject verifiedCodingSubject) bool {
	return strings.TrimSpace(subject.tenantID) != "" && strings.TrimSpace(subject.principalID) != "" &&
		strings.TrimSpace(subject.sessionID) != "" && subject.principalID != subject.sessionID
}

func normalizedCodingRelationTime(now time.Time) time.Time {
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now.UTC()
}

func loadCodingTaskHandle(q interface{ QueryRow(string, ...any) *sql.Row }, handleID string) (verifiedCodingTaskHandle, codingTaskRelationStatus, error) {
	var handle verifiedCodingTaskHandle
	var expiresAt int64
	var status codingTaskRelationStatus
	err := q.QueryRow(`SELECT handle_id,tenant_id,principal_id,session_id,root_task_id,turn_id,expires_at,status FROM coding_task_relations WHERE handle_id=?`, strings.TrimSpace(handleID)).Scan(
		&handle.handleID, &handle.tenantID, &handle.principalID, &handle.sessionID, &handle.rootTaskID, &handle.turnID, &expiresAt, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return verifiedCodingTaskHandle{}, "", errCodingTaskHandleNotFound
	}
	if err != nil {
		return verifiedCodingTaskHandle{}, "", err
	}
	handle.expiresAt = time.Unix(0, expiresAt).UTC()
	return handle, status, nil
}

func authorizeCodingTaskHandle(subject verifiedCodingSubject, supplied, stored verifiedCodingTaskHandle, status codingTaskRelationStatus, now time.Time) error {
	if supplied.tenantID != stored.tenantID || supplied.principalID != stored.principalID || supplied.sessionID != stored.sessionID ||
		supplied.rootTaskID != stored.rootTaskID || supplied.turnID != stored.turnID ||
		subject.tenantID != stored.tenantID || subject.principalID != stored.principalID || subject.sessionID != stored.sessionID {
		return errCodingTaskHandleForbidden
	}
	switch status {
	case codingTaskRelationConsumed:
		return errCodingTaskHandleConsumed
	case codingTaskRelationRevoked:
		return errCodingTaskHandleRevoked
	case codingTaskRelationExpired:
		return errCodingTaskHandleExpired
	case codingTaskRelationActive:
		if !now.Before(stored.expiresAt) {
			return errCodingTaskHandleExpired
		}
		return nil
	default:
		return errCodingTaskHandleNotFound
	}
}
