package tool

import (
	cryptorand "crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// TaskContinuationHandle is the server-side record behind an opaque UI/API
// continuation token. It deliberately contains task lineage identity only:
// no grants, materializations, provider bindings, model arguments, or effect
// authorization can cross this boundary.
//
// The handle is single-use. A successful verification consumes it before the
// next turn begins; the host may issue a fresh handle only after it has a
// durable current route to present to the user.
type TaskContinuationHandle struct {
	ID           string
	TenantID     string
	PrincipalID  string
	SessionID    string
	RootTaskID   string
	Revision     uint64
	PlanID       string
	PlanDigest   string
	FencingToken uint64
	IssuedAt     time.Time
	ExpiresAt    time.Time
	ConsumedAt   *time.Time
}

const defaultTaskContinuationHandleTTL = 30 * time.Minute

func (c *SQLiteSemanticExecutionCoordinator) initTaskContinuationHandles() error {
	if c == nil || c.db == nil {
		return fmt.Errorf("semantic execution coordinator is unavailable")
	}
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS semantic_task_continuation_handles (
			handle_id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, principal_id TEXT NOT NULL,
			session_id TEXT NOT NULL, root_task_id TEXT NOT NULL, revision INTEGER NOT NULL,
			plan_id TEXT NOT NULL, plan_digest TEXT NOT NULL, fencing_token INTEGER NOT NULL,
			state TEXT NOT NULL CHECK(state IN ('active','consumed','revoked','expired')),
			issued_at TEXT NOT NULL, expires_at TEXT NOT NULL, consumed_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_semantic_task_continuation_scope ON semantic_task_continuation_handles(tenant_id, principal_id, session_id, root_task_id, state, expires_at)`,
	} {
		if _, err := c.db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

// IssueTaskContinuationHandle issues an opaque, single-use task selector for
// the current durable route only. It is intentionally a host API rather than a
// model-callable tool: callers provide authenticated tenant/principal/session
// identity and an already host-owned root task, never provider or grant data.
func (c *SQLiteSemanticExecutionCoordinator) IssueTaskContinuationHandle(tenantID, principalID, sessionID, rootTaskID string, ttl time.Duration, now time.Time) (TaskContinuationHandle, error) {
	if c == nil || c.db == nil {
		return TaskContinuationHandle{}, fmt.Errorf("semantic execution coordinator is unavailable")
	}
	tenantID, principalID, sessionID, rootTaskID = strings.TrimSpace(tenantID), strings.TrimSpace(principalID), strings.TrimSpace(sessionID), strings.TrimSpace(rootTaskID)
	if tenantID == "" || principalID == "" || sessionID == "" || rootTaskID == "" {
		return TaskContinuationHandle{}, fmt.Errorf("task_continuation_handle_scope_required")
	}
	if ttl <= 0 {
		ttl = defaultTaskContinuationHandleTTL
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	handleID, err := newTaskContinuationHandleID()
	if err != nil {
		return TaskContinuationHandle{}, err
	}
	tx, err := c.db.Begin()
	if err != nil {
		return TaskContinuationHandle{}, err
	}
	defer func() { _ = tx.Rollback() }()
	record, err := currentTaskContinuationRouteTx(tx, tenantID, principalID, sessionID, rootTaskID)
	if err != nil {
		return TaskContinuationHandle{}, err
	}
	record.ID, record.IssuedAt, record.ExpiresAt = handleID, now, now.Add(ttl)
	if _, err := tx.Exec(`INSERT INTO semantic_task_continuation_handles(handle_id, tenant_id, principal_id, session_id, root_task_id, revision, plan_id, plan_digest, fencing_token, state, issued_at, expires_at, consumed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?, '')`, record.ID, record.TenantID, record.PrincipalID, record.SessionID, record.RootTaskID, record.Revision, record.PlanID, record.PlanDigest, record.FencingToken, routeStateTime(record.IssuedAt), routeStateTime(record.ExpiresAt)); err != nil {
		return TaskContinuationHandle{}, err
	}
	if err := tx.Commit(); err != nil {
		return TaskContinuationHandle{}, err
	}
	return record, nil
}

// ConsumeTaskContinuationHandle validates and consumes an opaque handle in one
// transaction. Besides owner/session/root matching, it rechecks that the
// revision and fencing token recorded at issue time are still current. A
// signature or an otherwise well-formed token is therefore never enough to
// continue a superseded route.
func (c *SQLiteSemanticExecutionCoordinator) ConsumeTaskContinuationHandle(handleID, tenantID, principalID, sessionID string, now time.Time) (TaskContinuationHandle, error) {
	if c == nil || c.db == nil {
		return TaskContinuationHandle{}, fmt.Errorf("semantic execution coordinator is unavailable")
	}
	handleID, tenantID, principalID, sessionID = strings.TrimSpace(handleID), strings.TrimSpace(tenantID), strings.TrimSpace(principalID), strings.TrimSpace(sessionID)
	if handleID == "" || tenantID == "" || principalID == "" || sessionID == "" {
		return TaskContinuationHandle{}, fmt.Errorf("task_continuation_handle_scope_required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	tx, err := c.db.Begin()
	if err != nil {
		return TaskContinuationHandle{}, err
	}
	defer func() { _ = tx.Rollback() }()
	record, state, err := readTaskContinuationHandleTx(tx, handleID)
	if err != nil {
		return TaskContinuationHandle{}, err
	}
	if record.TenantID != tenantID || record.PrincipalID != principalID || record.SessionID != sessionID {
		return TaskContinuationHandle{}, fmt.Errorf("task_continuation_handle_scope_mismatch")
	}
	if state != "active" {
		return TaskContinuationHandle{}, fmt.Errorf("task_continuation_handle_not_active")
	}
	if !now.Before(record.ExpiresAt) {
		if _, err := tx.Exec(`UPDATE semantic_task_continuation_handles SET state='expired' WHERE handle_id=? AND state='active'`, handleID); err != nil {
			return TaskContinuationHandle{}, err
		}
		if err := tx.Commit(); err != nil {
			return TaskContinuationHandle{}, err
		}
		return TaskContinuationHandle{}, fmt.Errorf("task_continuation_handle_expired")
	}
	current, err := currentTaskContinuationRouteTx(tx, tenantID, principalID, sessionID, record.RootTaskID)
	if err != nil || current.Revision != record.Revision || current.PlanID != record.PlanID || current.PlanDigest != record.PlanDigest || current.FencingToken != record.FencingToken {
		if _, updateErr := tx.Exec(`UPDATE semantic_task_continuation_handles SET state='revoked' WHERE handle_id=? AND state='active'`, handleID); updateErr != nil {
			return TaskContinuationHandle{}, updateErr
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return TaskContinuationHandle{}, commitErr
		}
		if err != nil {
			return TaskContinuationHandle{}, fmt.Errorf("task_continuation_handle_route_unavailable")
		}
		return TaskContinuationHandle{}, fmt.Errorf("task_continuation_handle_superseded")
	}
	if _, err := tx.Exec(`UPDATE semantic_task_continuation_handles SET state='consumed', consumed_at=? WHERE handle_id=? AND state='active'`, routeStateTime(now), handleID); err != nil {
		return TaskContinuationHandle{}, err
	}
	if err := tx.Commit(); err != nil {
		return TaskContinuationHandle{}, err
	}
	record.ConsumedAt = &now
	return record, nil
}

func currentTaskContinuationRouteTx(tx *sql.Tx, tenantID, principalID, sessionID, rootTaskID string) (TaskContinuationHandle, error) {
	lineageKey := routeLineageKey(InvocationScope{RootTaskID: rootTaskID, SessionID: sessionID, PrincipalID: principalID})
	var record TaskContinuationHandle
	err := tx.QueryRow(`SELECT rs.tenant_id, rl.root_task_id, rl.session_id, rl.principal_id, rl.current_revision, rl.current_plan_id, rl.current_plan_digest, rl.fencing_token
		FROM semantic_route_lineages rl JOIN semantic_route_states rs ON rs.route_key=rl.current_route_key
		WHERE rl.lineage_key=?`, lineageKey).Scan(&record.TenantID, &record.RootTaskID, &record.SessionID, &record.PrincipalID, &record.Revision, &record.PlanID, &record.PlanDigest, &record.FencingToken)
	if err == sql.ErrNoRows {
		return TaskContinuationHandle{}, fmt.Errorf("task_continuation_handle_route_not_found")
	}
	if err != nil {
		return TaskContinuationHandle{}, err
	}
	if record.TenantID != tenantID || record.PrincipalID != principalID || record.SessionID != sessionID || record.RootTaskID != rootTaskID || record.Revision == 0 || record.PlanID == "" || record.PlanDigest == "" || record.FencingToken == 0 {
		return TaskContinuationHandle{}, fmt.Errorf("task_continuation_handle_route_invalid")
	}
	return record, nil
}

func readTaskContinuationHandleTx(tx *sql.Tx, handleID string) (TaskContinuationHandle, string, error) {
	var record TaskContinuationHandle
	var state, issuedAt, expiresAt, consumedAt string
	err := tx.QueryRow(`SELECT handle_id, tenant_id, principal_id, session_id, root_task_id, revision, plan_id, plan_digest, fencing_token, state, issued_at, expires_at, consumed_at FROM semantic_task_continuation_handles WHERE handle_id=?`, handleID).Scan(&record.ID, &record.TenantID, &record.PrincipalID, &record.SessionID, &record.RootTaskID, &record.Revision, &record.PlanID, &record.PlanDigest, &record.FencingToken, &state, &issuedAt, &expiresAt, &consumedAt)
	if err == sql.ErrNoRows {
		return TaskContinuationHandle{}, "", fmt.Errorf("task_continuation_handle_not_found")
	}
	if err != nil {
		return TaskContinuationHandle{}, "", err
	}
	var parseErr error
	if record.IssuedAt, parseErr = time.Parse(time.RFC3339Nano, issuedAt); parseErr != nil {
		return TaskContinuationHandle{}, "", fmt.Errorf("task_continuation_handle_corrupt")
	}
	if record.ExpiresAt, parseErr = time.Parse(time.RFC3339Nano, expiresAt); parseErr != nil {
		return TaskContinuationHandle{}, "", fmt.Errorf("task_continuation_handle_corrupt")
	}
	if consumedAt != "" {
		value, err := time.Parse(time.RFC3339Nano, consumedAt)
		if err != nil {
			return TaskContinuationHandle{}, "", fmt.Errorf("task_continuation_handle_corrupt")
		}
		record.ConsumedAt = &value
	}
	return record, state, nil
}

func newTaskContinuationHandleID() (string, error) {
	bytes := make([]byte, 32)
	if _, err := cryptorand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate task continuation handle: %w", err)
	}
	return "tch_" + base64.RawURLEncoding.EncodeToString(bytes), nil
}
