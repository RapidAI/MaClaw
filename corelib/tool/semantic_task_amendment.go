package tool

import (
	cryptorand "crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// TaskAmendmentCommand is an opaque, host-issued proof that a user explicitly
// selected a current task to refine. Digest is an immutable host-computed
// description of the proposed goal change; it is not a provider binding,
// invocation grant, model argument, or route authorization by itself.
//
// A command is consumed only inside PublishSurface's child-revision
// transaction. This prevents an abandoned planner run, failed CAS, or outbox
// retry from leaving a task goal half-applied.
type TaskAmendmentCommand struct {
	ID string
	// SourceContinuationHandle is persistence-only retry correlation. It is
	// never sent to the model and never grants execution: it permits the same
	// opaque UI handle to resume the exact still-active amendment after a
	// planner/CAS failure without selecting another task.
	SourceContinuationHandle string
	Digest                   string
	TenantID                 string
	PrincipalID              string
	SessionID                string
	RootTaskID               string
	ParentRevision           uint64
	ParentFencingToken       uint64
	IssuedAt                 time.Time
	ExpiresAt                time.Time
	ConsumedAt               *time.Time
}

const defaultTaskAmendmentCommandTTL = 10 * time.Minute

func (c *SQLiteSemanticExecutionCoordinator) initTaskAmendmentCommands() error {
	if c == nil || c.db == nil {
		return fmt.Errorf("semantic execution coordinator is unavailable")
	}
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS semantic_task_amendment_commands (
			command_id TEXT PRIMARY KEY, source_continuation_handle TEXT NOT NULL DEFAULT '', digest TEXT NOT NULL,
			tenant_id TEXT NOT NULL, principal_id TEXT NOT NULL, session_id TEXT NOT NULL,
			root_task_id TEXT NOT NULL, parent_revision INTEGER NOT NULL,
			parent_fencing_token INTEGER NOT NULL,
			state TEXT NOT NULL CHECK(state IN ('active','consumed','revoked','expired')),
			issued_at TEXT NOT NULL, expires_at TEXT NOT NULL, consumed_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_semantic_task_amendment_scope ON semantic_task_amendment_commands(tenant_id, principal_id, session_id, root_task_id, state, expires_at)`,
	} {
		if _, err := c.db.Exec(statement); err != nil {
			return err
		}
	}
	if _, err := c.db.Exec(`ALTER TABLE semantic_task_amendment_commands ADD COLUMN source_continuation_handle TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return err
	}
	_, err := c.db.Exec(`CREATE INDEX IF NOT EXISTS idx_semantic_task_amendment_source ON semantic_task_amendment_commands(source_continuation_handle, state)`)
	return err
}

// IssueTaskAmendmentCommand is a host-only control-plane API. Callers must
// derive digest from a trusted amendment command after an explicit task action;
// accepting a raw user-supplied digest at a public transport boundary would
// defeat this protocol.
func (c *SQLiteSemanticExecutionCoordinator) IssueTaskAmendmentCommand(tenantID, principalID, sessionID, rootTaskID, digest string, ttl time.Duration, now time.Time) (TaskAmendmentCommand, error) {
	if c == nil || c.db == nil {
		return TaskAmendmentCommand{}, fmt.Errorf("semantic execution coordinator is unavailable")
	}
	tenantID, principalID, sessionID, rootTaskID, digest = strings.TrimSpace(tenantID), strings.TrimSpace(principalID), strings.TrimSpace(sessionID), strings.TrimSpace(rootTaskID), strings.TrimSpace(digest)
	if tenantID == "" || principalID == "" || sessionID == "" || rootTaskID == "" || digest == "" {
		return TaskAmendmentCommand{}, fmt.Errorf("task_amendment_command_required")
	}
	if ttl <= 0 {
		ttl = defaultTaskAmendmentCommandTTL
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	id, err := newTaskAmendmentCommandID()
	if err != nil {
		return TaskAmendmentCommand{}, err
	}
	tx, err := c.db.Begin()
	if err != nil {
		return TaskAmendmentCommand{}, err
	}
	defer func() { _ = tx.Rollback() }()
	route, err := currentTaskContinuationRouteTx(tx, tenantID, principalID, sessionID, rootTaskID)
	if err != nil {
		return TaskAmendmentCommand{}, err
	}
	command := TaskAmendmentCommand{
		ID: id, Digest: digest, TenantID: tenantID, PrincipalID: principalID, SessionID: sessionID, RootTaskID: rootTaskID,
		ParentRevision: route.Revision, ParentFencingToken: route.FencingToken, IssuedAt: now, ExpiresAt: now.Add(ttl),
	}
	if _, err := tx.Exec(`INSERT INTO semantic_task_amendment_commands(command_id, source_continuation_handle, digest, tenant_id, principal_id, session_id, root_task_id, parent_revision, parent_fencing_token, state, issued_at, expires_at, consumed_at) VALUES (?, '', ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?, '')`, command.ID, command.Digest, command.TenantID, command.PrincipalID, command.SessionID, command.RootTaskID, command.ParentRevision, command.ParentFencingToken, routeStateTime(command.IssuedAt), routeStateTime(command.ExpiresAt)); err != nil {
		return TaskAmendmentCommand{}, err
	}
	if err := tx.Commit(); err != nil {
		return TaskAmendmentCommand{}, err
	}
	return command, nil
}

// PrepareTaskRefinement atomically consumes a fresh continuation selector and
// creates the matching amendment command. If a later planner/CAS failure left
// that command active, supplying the same opaque selector and the same
// service-computed digest returns only that exact command for retry; it never
// discovers tasks by text or by owner. The command still must win
// PublishSurface's expected-parent transaction before it becomes a child
// revision.
func (c *SQLiteSemanticExecutionCoordinator) PrepareTaskRefinement(handleID, tenantID, principalID, sessionID, digest string, now time.Time) (TaskContinuationHandle, TaskAmendmentCommand, error) {
	if c == nil || c.db == nil {
		return TaskContinuationHandle{}, TaskAmendmentCommand{}, fmt.Errorf("semantic execution coordinator is unavailable")
	}
	handleID, tenantID, principalID, sessionID, digest = strings.TrimSpace(handleID), strings.TrimSpace(tenantID), strings.TrimSpace(principalID), strings.TrimSpace(sessionID), strings.TrimSpace(digest)
	if handleID == "" || tenantID == "" || principalID == "" || sessionID == "" || digest == "" {
		return TaskContinuationHandle{}, TaskAmendmentCommand{}, fmt.Errorf("task_amendment_command_required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	tx, err := c.db.Begin()
	if err != nil {
		return TaskContinuationHandle{}, TaskAmendmentCommand{}, err
	}
	defer func() { _ = tx.Rollback() }()
	handle, state, err := readTaskContinuationHandleTx(tx, handleID)
	if err != nil {
		return TaskContinuationHandle{}, TaskAmendmentCommand{}, err
	}
	if handle.TenantID != tenantID || handle.PrincipalID != principalID || handle.SessionID != sessionID {
		return TaskContinuationHandle{}, TaskAmendmentCommand{}, fmt.Errorf("task_continuation_handle_scope_mismatch")
	}
	if state != "active" {
		command, commandState, commandErr := readActiveTaskAmendmentForSourceTx(tx, handleID)
		if commandErr != nil {
			return TaskContinuationHandle{}, TaskAmendmentCommand{}, fmt.Errorf("task_continuation_handle_not_active")
		}
		if commandState != "active" || command.Digest != digest || command.TenantID != tenantID || command.PrincipalID != principalID || command.SessionID != sessionID || command.RootTaskID != handle.RootTaskID {
			return TaskContinuationHandle{}, TaskAmendmentCommand{}, fmt.Errorf("task_continuation_handle_not_active")
		}
		if !now.Before(command.ExpiresAt) {
			if _, err := tx.Exec(`UPDATE semantic_task_amendment_commands SET state='expired' WHERE command_id=? AND state='active'`, command.ID); err != nil {
				return TaskContinuationHandle{}, TaskAmendmentCommand{}, err
			}
			if err := tx.Commit(); err != nil {
				return TaskContinuationHandle{}, TaskAmendmentCommand{}, err
			}
			return TaskContinuationHandle{}, TaskAmendmentCommand{}, fmt.Errorf("task_amendment_command_expired")
		}
		route, routeErr := currentTaskContinuationRouteTx(tx, tenantID, principalID, sessionID, handle.RootTaskID)
		if routeErr != nil || route.Revision != command.ParentRevision || route.FencingToken != command.ParentFencingToken {
			if _, updateErr := tx.Exec(`UPDATE semantic_task_amendment_commands SET state='revoked' WHERE command_id=? AND state='active'`, command.ID); updateErr != nil {
				return TaskContinuationHandle{}, TaskAmendmentCommand{}, updateErr
			}
			if commitErr := tx.Commit(); commitErr != nil {
				return TaskContinuationHandle{}, TaskAmendmentCommand{}, commitErr
			}
			return TaskContinuationHandle{}, TaskAmendmentCommand{}, fmt.Errorf("task_amendment_command_superseded")
		}
		if err := tx.Commit(); err != nil {
			return TaskContinuationHandle{}, TaskAmendmentCommand{}, err
		}
		return handle, command, nil
	}
	if !now.Before(handle.ExpiresAt) {
		if _, err := tx.Exec(`UPDATE semantic_task_continuation_handles SET state='expired' WHERE handle_id=? AND state='active'`, handleID); err != nil {
			return TaskContinuationHandle{}, TaskAmendmentCommand{}, err
		}
		if err := tx.Commit(); err != nil {
			return TaskContinuationHandle{}, TaskAmendmentCommand{}, err
		}
		return TaskContinuationHandle{}, TaskAmendmentCommand{}, fmt.Errorf("task_continuation_handle_expired")
	}
	route, err := currentTaskContinuationRouteTx(tx, tenantID, principalID, sessionID, handle.RootTaskID)
	if err != nil || route.Revision != handle.Revision || route.PlanID != handle.PlanID || route.PlanDigest != handle.PlanDigest || route.FencingToken != handle.FencingToken {
		if _, updateErr := tx.Exec(`UPDATE semantic_task_continuation_handles SET state='revoked' WHERE handle_id=? AND state='active'`, handleID); updateErr != nil {
			return TaskContinuationHandle{}, TaskAmendmentCommand{}, updateErr
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return TaskContinuationHandle{}, TaskAmendmentCommand{}, commitErr
		}
		return TaskContinuationHandle{}, TaskAmendmentCommand{}, fmt.Errorf("task_continuation_handle_superseded")
	}
	commandID, err := newTaskAmendmentCommandID()
	if err != nil {
		return TaskContinuationHandle{}, TaskAmendmentCommand{}, err
	}
	command := TaskAmendmentCommand{ID: commandID, SourceContinuationHandle: handle.ID, Digest: digest, TenantID: tenantID, PrincipalID: principalID, SessionID: sessionID, RootTaskID: handle.RootTaskID, ParentRevision: route.Revision, ParentFencingToken: route.FencingToken, IssuedAt: now, ExpiresAt: now.Add(defaultTaskAmendmentCommandTTL)}
	if _, err := tx.Exec(`UPDATE semantic_task_continuation_handles SET state='consumed', consumed_at=? WHERE handle_id=? AND state='active'`, routeStateTime(now), handleID); err != nil {
		return TaskContinuationHandle{}, TaskAmendmentCommand{}, err
	}
	if _, err := tx.Exec(`INSERT INTO semantic_task_amendment_commands(command_id, source_continuation_handle, digest, tenant_id, principal_id, session_id, root_task_id, parent_revision, parent_fencing_token, state, issued_at, expires_at, consumed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?, '')`, command.ID, command.SourceContinuationHandle, command.Digest, command.TenantID, command.PrincipalID, command.SessionID, command.RootTaskID, command.ParentRevision, command.ParentFencingToken, routeStateTime(command.IssuedAt), routeStateTime(command.ExpiresAt)); err != nil {
		return TaskContinuationHandle{}, TaskAmendmentCommand{}, err
	}
	if err := tx.Commit(); err != nil {
		return TaskContinuationHandle{}, TaskAmendmentCommand{}, err
	}
	handle.ConsumedAt = &now
	return handle, command, nil
}

// ValidateTaskAmendmentCommand verifies a command without consuming it. The
// final consume belongs to PublishSurface so a planner cancellation or CAS
// conflict cannot make a legitimate refine command disappear before a child
// revision exists.
func (c *SQLiteSemanticExecutionCoordinator) ValidateTaskAmendmentCommand(commandID, tenantID, principalID, sessionID, rootTaskID string, now time.Time) (TaskAmendmentCommand, error) {
	if c == nil || c.db == nil {
		return TaskAmendmentCommand{}, fmt.Errorf("semantic execution coordinator is unavailable")
	}
	commandID, tenantID, principalID, sessionID, rootTaskID = strings.TrimSpace(commandID), strings.TrimSpace(tenantID), strings.TrimSpace(principalID), strings.TrimSpace(sessionID), strings.TrimSpace(rootTaskID)
	if commandID == "" || tenantID == "" || principalID == "" || sessionID == "" || rootTaskID == "" {
		return TaskAmendmentCommand{}, fmt.Errorf("task_amendment_command_required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	tx, err := c.db.Begin()
	if err != nil {
		return TaskAmendmentCommand{}, err
	}
	defer func() { _ = tx.Rollback() }()
	command, state, err := readTaskAmendmentCommandTx(tx, commandID)
	if err != nil {
		return TaskAmendmentCommand{}, err
	}
	if command.TenantID != tenantID || command.PrincipalID != principalID || command.SessionID != sessionID || command.RootTaskID != rootTaskID {
		return TaskAmendmentCommand{}, fmt.Errorf("task_amendment_command_scope_mismatch")
	}
	if state != "active" {
		return TaskAmendmentCommand{}, fmt.Errorf("task_amendment_command_not_active")
	}
	if !now.Before(command.ExpiresAt) {
		if _, err := tx.Exec(`UPDATE semantic_task_amendment_commands SET state='expired' WHERE command_id=? AND state='active'`, command.ID); err != nil {
			return TaskAmendmentCommand{}, err
		}
		if err := tx.Commit(); err != nil {
			return TaskAmendmentCommand{}, err
		}
		return TaskAmendmentCommand{}, fmt.Errorf("task_amendment_command_expired")
	}
	route, err := currentTaskContinuationRouteTx(tx, tenantID, principalID, sessionID, rootTaskID)
	if err != nil || route.Revision != command.ParentRevision || route.FencingToken != command.ParentFencingToken {
		if _, updateErr := tx.Exec(`UPDATE semantic_task_amendment_commands SET state='revoked' WHERE command_id=? AND state='active'`, command.ID); updateErr != nil {
			return TaskAmendmentCommand{}, updateErr
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return TaskAmendmentCommand{}, commitErr
		}
		if err != nil {
			return TaskAmendmentCommand{}, fmt.Errorf("task_amendment_command_route_unavailable")
		}
		return TaskAmendmentCommand{}, fmt.Errorf("task_amendment_command_superseded")
	}
	if err := tx.Commit(); err != nil {
		return TaskAmendmentCommand{}, err
	}
	return command, nil
}

func readTaskAmendmentCommandTx(tx *sql.Tx, commandID string) (TaskAmendmentCommand, string, error) {
	var command TaskAmendmentCommand
	var state, issuedAt, expiresAt, consumedAt string
	err := tx.QueryRow(`SELECT command_id, source_continuation_handle, digest, tenant_id, principal_id, session_id, root_task_id, parent_revision, parent_fencing_token, state, issued_at, expires_at, consumed_at FROM semantic_task_amendment_commands WHERE command_id=?`, commandID).Scan(&command.ID, &command.SourceContinuationHandle, &command.Digest, &command.TenantID, &command.PrincipalID, &command.SessionID, &command.RootTaskID, &command.ParentRevision, &command.ParentFencingToken, &state, &issuedAt, &expiresAt, &consumedAt)
	if err == sql.ErrNoRows {
		return TaskAmendmentCommand{}, "", fmt.Errorf("task_amendment_command_not_found")
	}
	if err != nil {
		return TaskAmendmentCommand{}, "", err
	}
	var parseErr error
	if command.IssuedAt, parseErr = time.Parse(time.RFC3339Nano, issuedAt); parseErr != nil {
		return TaskAmendmentCommand{}, "", fmt.Errorf("task_amendment_command_corrupt")
	}
	if command.ExpiresAt, parseErr = time.Parse(time.RFC3339Nano, expiresAt); parseErr != nil {
		return TaskAmendmentCommand{}, "", fmt.Errorf("task_amendment_command_corrupt")
	}
	if consumedAt != "" {
		value, err := time.Parse(time.RFC3339Nano, consumedAt)
		if err != nil {
			return TaskAmendmentCommand{}, "", fmt.Errorf("task_amendment_command_corrupt")
		}
		command.ConsumedAt = &value
	}
	return command, state, nil
}

func readActiveTaskAmendmentForSourceTx(tx *sql.Tx, handleID string) (TaskAmendmentCommand, string, error) {
	var command TaskAmendmentCommand
	var state, issuedAt, expiresAt, consumedAt string
	err := tx.QueryRow(`SELECT command_id, source_continuation_handle, digest, tenant_id, principal_id, session_id, root_task_id, parent_revision, parent_fencing_token, state, issued_at, expires_at, consumed_at FROM semantic_task_amendment_commands WHERE source_continuation_handle=? ORDER BY issued_at DESC LIMIT 1`, handleID).Scan(&command.ID, &command.SourceContinuationHandle, &command.Digest, &command.TenantID, &command.PrincipalID, &command.SessionID, &command.RootTaskID, &command.ParentRevision, &command.ParentFencingToken, &state, &issuedAt, &expiresAt, &consumedAt)
	if err == sql.ErrNoRows {
		return TaskAmendmentCommand{}, "", err
	}
	if err != nil {
		return TaskAmendmentCommand{}, "", err
	}
	var parseErr error
	if command.IssuedAt, parseErr = time.Parse(time.RFC3339Nano, issuedAt); parseErr != nil {
		return TaskAmendmentCommand{}, "", fmt.Errorf("task_amendment_command_corrupt")
	}
	if command.ExpiresAt, parseErr = time.Parse(time.RFC3339Nano, expiresAt); parseErr != nil {
		return TaskAmendmentCommand{}, "", fmt.Errorf("task_amendment_command_corrupt")
	}
	if consumedAt != "" {
		value, err := time.Parse(time.RFC3339Nano, consumedAt)
		if err != nil {
			return TaskAmendmentCommand{}, "", fmt.Errorf("task_amendment_command_corrupt")
		}
		command.ConsumedAt = &value
	}
	return command, state, nil
}

func consumeTaskAmendmentCommandTx(tx *sql.Tx, amendment *RouteAmendmentRef, tenantID string, scope InvocationScope, now time.Time) error {
	if tx == nil || amendment == nil {
		return fmt.Errorf("route_amendment_invalid")
	}
	command, state, err := readTaskAmendmentCommandTx(tx, amendment.CommandID)
	if err != nil {
		return err
	}
	if command.TenantID != strings.TrimSpace(tenantID) || command.PrincipalID != strings.TrimSpace(scope.PrincipalID) || command.SessionID != strings.TrimSpace(scope.SessionID) || command.RootTaskID != strings.TrimSpace(scope.RootTaskID) || command.Digest != strings.TrimSpace(amendment.Digest) || command.ParentRevision != amendment.ParentRevision || command.ParentFencingToken != amendment.ParentFencingToken {
		return fmt.Errorf("task_amendment_command_scope_mismatch")
	}
	if state != "active" {
		return fmt.Errorf("task_amendment_command_not_active")
	}
	if !now.Before(command.ExpiresAt) {
		if _, err := tx.Exec(`UPDATE semantic_task_amendment_commands SET state='expired' WHERE command_id=? AND state='active'`, command.ID); err != nil {
			return err
		}
		return fmt.Errorf("task_amendment_command_expired")
	}
	if _, err := tx.Exec(`UPDATE semantic_task_amendment_commands SET state='consumed', consumed_at=? WHERE command_id=? AND state='active'`, routeStateTime(now), command.ID); err != nil {
		return err
	}
	return nil
}

func newTaskAmendmentCommandID() (string, error) {
	bytes := make([]byte, 32)
	if _, err := cryptorand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate task amendment command: %w", err)
	}
	return "tac_" + base64.RawURLEncoding.EncodeToString(bytes), nil
}
