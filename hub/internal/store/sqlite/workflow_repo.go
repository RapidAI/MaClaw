package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type workflowRepo struct {
	db, readDB *sql.DB
}

// InitWorkflowTables creates the understanding_sessions and workflow_states
// tables if they do not already exist.
func InitWorkflowTables(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS understanding_sessions (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			user_id TEXT NOT NULL,
			intent_json TEXT NOT NULL DEFAULT '{}',
			rounds_json TEXT NOT NULL DEFAULT '[]',
			state TEXT NOT NULL DEFAULT 'active',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_understanding_sessions_user_state ON understanding_sessions(user_id, state);`,
		`CREATE INDEX IF NOT EXISTS idx_understanding_sessions_tenant_user_state ON understanding_sessions(tenant_id, user_id, state);`,
		`CREATE TABLE IF NOT EXISTS workflow_states (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			user_id TEXT NOT NULL,
			type TEXT NOT NULL,
			template_type TEXT NOT NULL,
			intent_json TEXT NOT NULL DEFAULT '{}',
			current_phase TEXT NOT NULL DEFAULT '',
			phase_outputs_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_states_user ON workflow_states(user_id);`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_states_tenant_user ON workflow_states(tenant_id, user_id);`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// UnderstandingSession CRUD
// ---------------------------------------------------------------------------

func (r *workflowRepo) SaveUnderstandingSession(ctx context.Context, s *store.UnderstandingSessionRow) error {
	tenantID := store.NormalizeTenantID(s.TenantID)
	if tenantID == store.DefaultTenantID {
		tenantID = store.TenantIDFromContext(ctx)
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO understanding_sessions (id, tenant_id, user_id, intent_json, rounds_json, state, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   tenant_id   = excluded.tenant_id,
		   intent_json = excluded.intent_json,
		   rounds_json = excluded.rounds_json,
		   state       = excluded.state,
		   updated_at  = excluded.updated_at`,
		s.ID,
		tenantID,
		s.UserID,
		s.IntentJSON,
		s.RoundsJSON,
		s.State,
		s.CreatedAt.Format(time.RFC3339),
		s.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (r *workflowRepo) GetActiveUnderstandingSession(ctx context.Context, userID string) (*store.UnderstandingSessionRow, error) {
	row := r.readDB.QueryRowContext(ctx,
		`SELECT id, tenant_id, user_id, intent_json, rounds_json, state, created_at, updated_at
		 FROM understanding_sessions
		 WHERE tenant_id = ? AND user_id = ? AND state = 'active'
		 ORDER BY updated_at DESC LIMIT 1`,
		store.TenantIDFromContext(ctx),
		userID,
	)
	var (
		s                    store.UnderstandingSessionRow
		createdAt, updatedAt string
	)
	if err := row.Scan(&s.ID, &s.TenantID, &s.UserID, &s.IntentJSON, &s.RoundsJSON, &s.State, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	s.CreatedAt = mustParseTime(createdAt)
	s.UpdatedAt = mustParseTime(updatedAt)
	return &s, nil
}

func (r *workflowRepo) DeleteUnderstandingSession(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM understanding_sessions WHERE id = ?`, id)
	return err
}

// ---------------------------------------------------------------------------
// WorkflowState CRUD
// ---------------------------------------------------------------------------

func (r *workflowRepo) SaveWorkflowState(ctx context.Context, ws *store.WorkflowStateRow) error {
	tenantID := store.NormalizeTenantID(ws.TenantID)
	if tenantID == store.DefaultTenantID {
		tenantID = store.TenantIDFromContext(ctx)
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO workflow_states (id, tenant_id, user_id, type, template_type, intent_json, current_phase, phase_outputs_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   tenant_id          = excluded.tenant_id,
		   intent_json        = excluded.intent_json,
		   current_phase      = excluded.current_phase,
		   phase_outputs_json = excluded.phase_outputs_json,
		   updated_at         = excluded.updated_at`,
		ws.ID,
		tenantID,
		ws.UserID,
		ws.Type,
		ws.TemplateType,
		ws.IntentJSON,
		ws.CurrentPhase,
		ws.PhaseOutputsJSON,
		ws.CreatedAt.Format(time.RFC3339),
		ws.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (r *workflowRepo) GetActiveWorkflowState(ctx context.Context, userID string) (*store.WorkflowStateRow, error) {
	row := r.readDB.QueryRowContext(ctx,
		`SELECT id, tenant_id, user_id, type, template_type, intent_json, current_phase, phase_outputs_json, created_at, updated_at
		 FROM workflow_states
		 WHERE tenant_id = ? AND user_id = ?
		 ORDER BY updated_at DESC LIMIT 1`,
		store.TenantIDFromContext(ctx),
		userID,
	)
	var (
		ws                   store.WorkflowStateRow
		createdAt, updatedAt string
	)
	if err := row.Scan(&ws.ID, &ws.TenantID, &ws.UserID, &ws.Type, &ws.TemplateType, &ws.IntentJSON, &ws.CurrentPhase, &ws.PhaseOutputsJSON, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	ws.CreatedAt = mustParseTime(createdAt)
	ws.UpdatedAt = mustParseTime(updatedAt)
	return &ws, nil
}

func (r *workflowRepo) DeleteWorkflowState(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM workflow_states WHERE id = ?`, id)
	return err
}

// ---------------------------------------------------------------------------
// CleanupExpired
// ---------------------------------------------------------------------------

// CleanupExpired deletes understanding_sessions where state is 'confirmed' or
// 'cancelled' and updated_at is older than olderThan, and deletes
// workflow_states whose updated_at is older than olderThan.
func (r *workflowRepo) CleanupExpired(ctx context.Context, olderThan time.Duration) error {
	threshold := time.Now().Add(-olderThan).Format(time.RFC3339)

	_, err := r.db.ExecContext(ctx,
		`DELETE FROM understanding_sessions WHERE state IN ('confirmed','cancelled') AND updated_at < ?`,
		threshold,
	)
	if err != nil {
		return err
	}

	_, err = r.db.ExecContext(ctx,
		`DELETE FROM workflow_states WHERE updated_at < ?`,
		threshold,
	)
	return err
}
