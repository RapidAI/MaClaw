package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
)

// Compile-time interface check.
var _ workflow.WorkflowStore = (*WorkflowStoreSQLite)(nil)

// WorkflowStoreSQLite implements workflow.WorkflowStore using SQLite.
type WorkflowStoreSQLite struct {
	db *sql.DB
}

// NewWorkflowStore creates a new WorkflowStore backed by the given write DB.
func NewWorkflowStore(db *sql.DB) *WorkflowStoreSQLite {
	return &WorkflowStoreSQLite{db: db}
}

// ---------------------------------------------------------------------------
// Workflow Definition CRUD
// ---------------------------------------------------------------------------

func (s *WorkflowStoreSQLite) CreateWorkflow(ctx context.Context, def *workflow.WorkflowDefinition) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO workflow_definitions (id, tenant_id, owner_id, name, description, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		def.ID,
		store.TenantIDFromContext(ctx),
		def.OwnerID,
		def.Name,
		def.Description,
		def.CreatedAt.UTC().Format(time.RFC3339),
		def.UpdatedAt.UTC().Format(time.RFC3339),
	)
	return err
}

func (s *WorkflowStoreSQLite) GetWorkflow(ctx context.Context, id string) (*workflow.WorkflowDefinition, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, owner_id, name, description, created_at, updated_at
		 FROM workflow_definitions WHERE id = ? AND tenant_id = ?`, id, store.TenantIDFromContext(ctx))

	var (
		def                  workflow.WorkflowDefinition
		createdAt, updatedAt string
	)
	if err := row.Scan(&def.ID, &def.TenantID, &def.OwnerID, &def.Name, &def.Description, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	def.CreatedAt = mustParseTime(createdAt)
	def.UpdatedAt = mustParseTime(updatedAt)
	return &def, nil
}

func (s *WorkflowStoreSQLite) ListWorkflows(ctx context.Context, ownerID string) ([]workflow.WorkflowDefinition, error) {
	if ownerID == "" {
		return nil, errors.New("ownerID is required")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, owner_id, name, description, created_at, updated_at
		 FROM workflow_definitions WHERE tenant_id = ? AND owner_id = ? ORDER BY updated_at DESC`, store.TenantIDFromContext(ctx), ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var defs []workflow.WorkflowDefinition
	for rows.Next() {
		var (
			def                  workflow.WorkflowDefinition
			createdAt, updatedAt string
		)
		if err := rows.Scan(&def.ID, &def.TenantID, &def.OwnerID, &def.Name, &def.Description, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		def.CreatedAt = mustParseTime(createdAt)
		def.UpdatedAt = mustParseTime(updatedAt)
		defs = append(defs, def)
	}
	return defs, rows.Err()
}

// UpdateWorkflow updates mutable workflow definition metadata.
func (s *WorkflowStoreSQLite) UpdateWorkflow(ctx context.Context, def *workflow.WorkflowDefinition) error {
	if def == nil {
		return errors.New("workflow definition is required")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE workflow_definitions SET name = ?, description = ?, updated_at = ? WHERE id = ? AND tenant_id = ?`,
		def.Name,
		def.Description,
		def.UpdatedAt.UTC().Format(time.RFC3339),
		def.ID,
		store.TenantIDFromContext(ctx),
	)
	if err != nil {
		return err
	}
	return ensureRowsAffected(res)
}

// DeleteWorkflow deletes a workflow definition and its versions when it has no running instances.
func (s *WorkflowStoreSQLite) DeleteWorkflow(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var running int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workflow_instances WHERE tenant_id = ? AND workflow_id = ? AND status IN ('running', 'blocked')`, store.TenantIDFromContext(ctx), id,
	).Scan(&running); err != nil {
		return err
	}
	if running > 0 {
		return errors.New("cannot delete workflow with running instances")
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM workflow_versions WHERE workflow_id = ? AND workflow_id IN (SELECT id FROM workflow_definitions WHERE tenant_id = ?)`, id, store.TenantIDFromContext(ctx)); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM workflow_definitions WHERE id = ? AND tenant_id = ?`, id, store.TenantIDFromContext(ctx))
	if err != nil {
		return err
	}
	if err := ensureRowsAffected(res); err != nil {
		return err
	}
	return tx.Commit()
}

func ensureRowsAffected(res sql.Result) error {
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ---------------------------------------------------------------------------
// Workflow Version CRUD
// ---------------------------------------------------------------------------

func (s *WorkflowStoreSQLite) CreateVersion(ctx context.Context, ver *workflow.WorkflowVersion) error {
	graphJSON, err := json.Marshal(ver.Graph)
	if err != nil {
		return err
	}

	var submittedAt, publishedAt sql.NullString
	if ver.SubmittedAt != nil {
		submittedAt = sql.NullString{String: ver.SubmittedAt.UTC().Format(time.RFC3339), Valid: true}
	}
	if ver.PublishedAt != nil {
		publishedAt = sql.NullString{String: ver.PublishedAt.UTC().Format(time.RFC3339), Valid: true}
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO workflow_versions (id, workflow_id, version_number, status, graph_json, submitted_at, published_at, rejection_reason, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ver.ID,
		ver.WorkflowID,
		ver.VersionNumber,
		string(ver.Status),
		string(graphJSON),
		submittedAt,
		publishedAt,
		ver.RejectionReason,
		ver.CreatedAt.UTC().Format(time.RFC3339),
		ver.UpdatedAt.UTC().Format(time.RFC3339),
	)
	return err
}

// UpdateVersion updates an existing draft version in place: its version number
// and graph, leaving the status unchanged. Used by SaveDraft's "update existing
// draft" branch so re-saving a draft mutates the same row rather than creating
// a new version row.
func (s *WorkflowStoreSQLite) UpdateVersion(ctx context.Context, ver *workflow.WorkflowVersion) error {
	graphJSON, err := json.Marshal(ver.Graph)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`UPDATE workflow_versions SET version_number = ?, graph_json = ?, updated_at = ? WHERE id = ? AND workflow_id IN (SELECT id FROM workflow_definitions WHERE tenant_id = ?)`,
		ver.VersionNumber,
		string(graphJSON),
		now,
		ver.ID,
		store.TenantIDFromContext(ctx),
	)
	return err
}

func (s *WorkflowStoreSQLite) GetVersion(ctx context.Context, id string) (*workflow.WorkflowVersion, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT v.id, v.workflow_id, v.version_number, v.status, v.graph_json, v.submitted_at, v.published_at, v.rejection_reason, v.created_at, v.updated_at
		 FROM workflow_versions v JOIN workflow_definitions d ON d.id = v.workflow_id
		 WHERE v.id = ? AND d.tenant_id = ?`, id, store.TenantIDFromContext(ctx))
	return s.scanVersion(row)
}

func (s *WorkflowStoreSQLite) GetPublishedVersion(ctx context.Context, workflowID string) (*workflow.WorkflowVersion, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT v.id, v.workflow_id, v.version_number, v.status, v.graph_json, v.submitted_at, v.published_at, v.rejection_reason, v.created_at, v.updated_at
		 FROM workflow_versions v JOIN workflow_definitions d ON d.id = v.workflow_id
		 WHERE v.workflow_id = ? AND v.status = 'published' AND d.tenant_id = ?`, workflowID, store.TenantIDFromContext(ctx))
	return s.scanVersion(row)
}

func (s *WorkflowStoreSQLite) UpdateVersionStatus(ctx context.Context, id string, status workflow.VersionStatus, reason string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	switch status {
	case workflow.VersionPendingReview:
		_, err := s.db.ExecContext(ctx,
			`UPDATE workflow_versions SET status = ?, submitted_at = ?, updated_at = ? WHERE id = ? AND workflow_id IN (SELECT id FROM workflow_definitions WHERE tenant_id = ?)`,
			string(status), now, now, id, store.TenantIDFromContext(ctx))
		return err
	case workflow.VersionPublished:
		_, err := s.db.ExecContext(ctx,
			`UPDATE workflow_versions SET status = ?, published_at = ?, updated_at = ? WHERE id = ? AND workflow_id IN (SELECT id FROM workflow_definitions WHERE tenant_id = ?)`,
			string(status), now, now, id, store.TenantIDFromContext(ctx))
		return err
	case workflow.VersionRejected:
		_, err := s.db.ExecContext(ctx,
			`UPDATE workflow_versions SET status = ?, rejection_reason = ?, updated_at = ? WHERE id = ? AND workflow_id IN (SELECT id FROM workflow_definitions WHERE tenant_id = ?)`,
			string(status), reason, now, id, store.TenantIDFromContext(ctx))
		return err
	default:
		_, err := s.db.ExecContext(ctx,
			`UPDATE workflow_versions SET status = ?, updated_at = ? WHERE id = ? AND workflow_id IN (SELECT id FROM workflow_definitions WHERE tenant_id = ?)`,
			string(status), now, id, store.TenantIDFromContext(ctx))
		return err
	}
}

func (s *WorkflowStoreSQLite) ListVersions(ctx context.Context, workflowID string) ([]workflow.WorkflowVersion, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT v.id, v.workflow_id, v.version_number, v.status, v.graph_json, v.submitted_at, v.published_at, v.rejection_reason, v.created_at, v.updated_at
		 FROM workflow_versions v JOIN workflow_definitions d ON d.id = v.workflow_id
		 WHERE v.workflow_id = ? AND d.tenant_id = ? ORDER BY v.created_at DESC`, workflowID, store.TenantIDFromContext(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var versions []workflow.WorkflowVersion
	for rows.Next() {
		ver, err := s.scanVersionFromRows(rows)
		if err != nil {
			return nil, err
		}
		versions = append(versions, *ver)
	}
	return versions, rows.Err()
}

// ---------------------------------------------------------------------------
// ListPendingReviews 閳?paginated, sorted by submitted_at ASC (oldest first)
// ---------------------------------------------------------------------------

func (s *WorkflowStoreSQLite) ListPendingReviews(ctx context.Context, page, pageSize int) ([]workflow.WorkflowVersion, int, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * pageSize

	var total int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workflow_versions v JOIN workflow_definitions d ON d.id = v.workflow_id WHERE v.status = 'pending_review' AND d.tenant_id = ?`,
		store.TenantIDFromContext(ctx),
	).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, nil
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT v.id, v.workflow_id, v.version_number, v.status, v.graph_json, v.submitted_at, v.published_at, v.rejection_reason, v.created_at, v.updated_at
		 FROM workflow_versions v JOIN workflow_definitions d ON d.id = v.workflow_id
		 WHERE v.status = 'pending_review' AND d.tenant_id = ?
		 ORDER BY v.submitted_at ASC
		 LIMIT ? OFFSET ?`, store.TenantIDFromContext(ctx), pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var versions []workflow.WorkflowVersion
	for rows.Next() {
		ver, err := s.scanVersionFromRows(rows)
		if err != nil {
			return nil, 0, err
		}
		versions = append(versions, *ver)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return versions, total, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// scanVersion scans a single row into a WorkflowVersion.
func (s *WorkflowStoreSQLite) scanVersion(row *sql.Row) (*workflow.WorkflowVersion, error) {
	var (
		ver                      workflow.WorkflowVersion
		status                   string
		graphJSON                string
		submittedAt, publishedAt sql.NullString
		rejectionReason          string
		createdAt, updatedAt     string
	)
	if err := row.Scan(
		&ver.ID, &ver.WorkflowID, &ver.VersionNumber, &status, &graphJSON,
		&submittedAt, &publishedAt, &rejectionReason, &createdAt, &updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	ver.Status = workflow.VersionStatus(status)
	ver.RejectionReason = rejectionReason
	ver.CreatedAt = mustParseTime(createdAt)
	ver.UpdatedAt = mustParseTime(updatedAt)

	if submittedAt.Valid {
		t := mustParseTime(submittedAt.String)
		ver.SubmittedAt = &t
	}
	if publishedAt.Valid {
		t := mustParseTime(publishedAt.String)
		ver.PublishedAt = &t
	}

	if err := json.Unmarshal([]byte(graphJSON), &ver.Graph); err != nil {
		return nil, err
	}

	return &ver, nil
}

// scanVersionFromRows scans the current row from sql.Rows into a WorkflowVersion.
func (s *WorkflowStoreSQLite) scanVersionFromRows(rows *sql.Rows) (*workflow.WorkflowVersion, error) {
	var (
		ver                      workflow.WorkflowVersion
		status                   string
		graphJSON                string
		submittedAt, publishedAt sql.NullString
		rejectionReason          string
		createdAt, updatedAt     string
	)
	if err := rows.Scan(
		&ver.ID, &ver.WorkflowID, &ver.VersionNumber, &status, &graphJSON,
		&submittedAt, &publishedAt, &rejectionReason, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}

	ver.Status = workflow.VersionStatus(status)
	ver.RejectionReason = rejectionReason
	ver.CreatedAt = mustParseTime(createdAt)
	ver.UpdatedAt = mustParseTime(updatedAt)

	if submittedAt.Valid {
		t := mustParseTime(submittedAt.String)
		ver.SubmittedAt = &t
	}
	if publishedAt.Valid {
		t := mustParseTime(publishedAt.String)
		ver.PublishedAt = &t
	}

	if err := json.Unmarshal([]byte(graphJSON), &ver.Graph); err != nil {
		return nil, err
	}

	return &ver, nil
}
