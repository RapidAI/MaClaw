package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// Compile-time interface check.
var _ WorkflowStore = (*PGWorkflowStore)(nil)

// PGWorkflowStore implements WorkflowStore using PostgreSQL.
type PGWorkflowStore struct {
	db *sql.DB
}

// NewPGWorkflowStore creates a new WorkflowStore backed by the given PostgreSQL DB.
func NewPGWorkflowStore(db *sql.DB) *PGWorkflowStore {
	return &PGWorkflowStore{db: db}
}

// ---------------------------------------------------------------------------
// Workflow Definition CRUD
// ---------------------------------------------------------------------------

func (s *PGWorkflowStore) CreateWorkflow(ctx context.Context, def *WorkflowDefinition) error {
	tenantID := store.TenantIDFromContext(ctx)
	if def.TenantID == "" {
		def.TenantID = tenantID
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO workflow_definitions (id, tenant_id, owner_id, name, description, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		def.ID,
		tenantID,
		def.OwnerID,
		def.Name,
		def.Description,
		def.CreatedAt.UTC(),
		def.UpdatedAt.UTC(),
	)
	return err
}

func (s *PGWorkflowStore) GetWorkflow(ctx context.Context, id string) (*WorkflowDefinition, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, owner_id, name, description, created_at, updated_at
		 FROM workflow_definitions WHERE tenant_id = $1 AND id = $2`, store.TenantIDFromContext(ctx), id)

	var (
		def                  WorkflowDefinition
		createdAt, updatedAt any
	)
	if err := row.Scan(&def.ID, &def.TenantID, &def.OwnerID, &def.Name, &def.Description, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var err error
	if def.CreatedAt, err = scanWorkflowStoreTime(createdAt); err != nil {
		return nil, fmt.Errorf("scan created_at: %w", err)
	}
	if def.UpdatedAt, err = scanWorkflowStoreTime(updatedAt); err != nil {
		return nil, fmt.Errorf("scan updated_at: %w", err)
	}
	return &def, nil
}

func (s *PGWorkflowStore) ListWorkflows(ctx context.Context, ownerID string) ([]WorkflowDefinition, error) {
	if ownerID == "" {
		return nil, errors.New("ownerID is required")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, owner_id, name, description, created_at, updated_at
		 FROM workflow_definitions WHERE tenant_id = $1 AND owner_id = $2 ORDER BY updated_at DESC`, store.TenantIDFromContext(ctx), ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var defs []WorkflowDefinition
	for rows.Next() {
		var (
			def                  WorkflowDefinition
			createdAt, updatedAt any
		)
		if err := rows.Scan(&def.ID, &def.TenantID, &def.OwnerID, &def.Name, &def.Description, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		var parseErr error
		if def.CreatedAt, parseErr = scanWorkflowStoreTime(createdAt); parseErr != nil {
			return nil, fmt.Errorf("scan created_at: %w", parseErr)
		}
		if def.UpdatedAt, parseErr = scanWorkflowStoreTime(updatedAt); parseErr != nil {
			return nil, fmt.Errorf("scan updated_at: %w", parseErr)
		}
		defs = append(defs, def)
	}
	return defs, rows.Err()
}

// UpdateWorkflow updates mutable workflow definition metadata.
func (s *PGWorkflowStore) UpdateWorkflow(ctx context.Context, def *WorkflowDefinition) error {
	if def == nil {
		return errors.New("workflow definition is required")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE workflow_definitions SET name = $1, description = $2, updated_at = $3 WHERE tenant_id = $4 AND id = $5`,
		def.Name,
		def.Description,
		def.UpdatedAt.UTC(),
		store.TenantIDFromContext(ctx),
		def.ID,
	)
	if err != nil {
		return err
	}
	return ensurePGRowsAffected(res)
}

// DeleteWorkflow deletes a workflow definition and its versions when it has no running instances.
func (s *PGWorkflowStore) DeleteWorkflow(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var running int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workflow_instances WHERE tenant_id = $1 AND workflow_id = $2 AND status IN ('running', 'blocked')`, store.TenantIDFromContext(ctx), id,
	).Scan(&running); err != nil {
		return err
	}
	if running > 0 {
		return errors.New("cannot delete workflow with running instances")
	}

	var published int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workflow_versions v JOIN workflow_definitions d ON d.id = v.workflow_id WHERE d.tenant_id = $1 AND v.workflow_id = $2 AND v.status IN ('published', 'superseded', 'unpublished')`, store.TenantIDFromContext(ctx), id,
	).Scan(&published); err != nil {
		return err
	}
	if published > 0 {
		return errors.New("cannot delete published workflow")
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM workflow_versions WHERE workflow_id = $1 AND EXISTS (SELECT 1 FROM workflow_definitions d WHERE d.id = workflow_versions.workflow_id AND d.tenant_id = $2)`, id, store.TenantIDFromContext(ctx)); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM workflow_definitions WHERE tenant_id = $1 AND id = $2`, store.TenantIDFromContext(ctx), id)
	if err != nil {
		return err
	}
	if err := ensurePGRowsAffected(res); err != nil {
		return err
	}
	return tx.Commit()
}

func ensurePGRowsAffected(res sql.Result) error {
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

func (s *PGWorkflowStore) CreateVersion(ctx context.Context, ver *WorkflowVersion) error {
	graphJSON, err := json.Marshal(ver.Graph)
	if err != nil {
		return err
	}

	var submittedAt, publishedAt *time.Time
	if ver.SubmittedAt != nil {
		t := ver.SubmittedAt.UTC()
		submittedAt = &t
	}
	if ver.PublishedAt != nil {
		t := ver.PublishedAt.UTC()
		publishedAt = &t
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO workflow_versions (id, workflow_id, version_number, status, graph_json, submitted_at, published_at, rejection_reason, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		ver.ID,
		ver.WorkflowID,
		ver.VersionNumber,
		string(ver.Status),
		graphJSON,
		submittedAt,
		publishedAt,
		ver.RejectionReason,
		ver.CreatedAt.UTC(),
		ver.UpdatedAt.UTC(),
	)
	return err
}

// UpdateVersion updates an existing draft version in place: its version number
// and graph, leaving the status unchanged. Used by SaveDraft's "update existing
// draft" branch so re-saving a draft mutates the same row rather than creating
// a new version row.
func (s *PGWorkflowStore) UpdateVersion(ctx context.Context, ver *WorkflowVersion) error {
	graphJSON, err := json.Marshal(ver.Graph)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx,
		`UPDATE workflow_versions SET version_number = $1, graph_json = $2, updated_at = $3
		 WHERE id = $4 AND EXISTS (SELECT 1 FROM workflow_definitions d WHERE d.id = workflow_versions.workflow_id AND d.tenant_id = $5)`,
		ver.VersionNumber,
		graphJSON,
		now,
		ver.ID,
		store.TenantIDFromContext(ctx),
	)
	return err
}

func (s *PGWorkflowStore) GetVersion(ctx context.Context, id string) (*WorkflowVersion, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, workflow_id, version_number, status, graph_json, submitted_at, published_at, rejection_reason, created_at, updated_at
		 FROM workflow_versions v
		 WHERE v.id = $1
		   AND EXISTS (SELECT 1 FROM workflow_definitions d WHERE d.id = v.workflow_id AND d.tenant_id = $2)`, id, store.TenantIDFromContext(ctx))
	return s.scanVersion(row)
}

func (s *PGWorkflowStore) GetPublishedVersion(ctx context.Context, workflowID string) (*WorkflowVersion, error) {
	// Uses the unique partial index: idx_wf_ver_published ON workflow_versions(workflow_id) WHERE status = 'published'
	row := s.db.QueryRowContext(ctx,
		`SELECT id, workflow_id, version_number, status, graph_json, submitted_at, published_at, rejection_reason, created_at, updated_at
		 FROM workflow_versions v
		 WHERE v.workflow_id = $1 AND v.status = 'published'
		   AND EXISTS (SELECT 1 FROM workflow_definitions d WHERE d.id = v.workflow_id AND d.tenant_id = $2)`, workflowID, store.TenantIDFromContext(ctx))
	return s.scanVersion(row)
}

func (s *PGWorkflowStore) UpdateVersionStatus(ctx context.Context, id string, status VersionStatus, reason string) error {
	now := time.Now().UTC()

	switch status {
	case VersionPendingReview:
		_, err := s.db.ExecContext(ctx,
			`UPDATE workflow_versions SET status = $1, submitted_at = $2, updated_at = $3
			 WHERE id = $4 AND EXISTS (SELECT 1 FROM workflow_definitions d WHERE d.id = workflow_versions.workflow_id AND d.tenant_id = $5)`,
			string(status), now, now, id, store.TenantIDFromContext(ctx))
		return err
	case VersionPublished:
		_, err := s.db.ExecContext(ctx,
			`UPDATE workflow_versions SET status = $1, published_at = $2, updated_at = $3
			 WHERE id = $4 AND EXISTS (SELECT 1 FROM workflow_definitions d WHERE d.id = workflow_versions.workflow_id AND d.tenant_id = $5)`,
			string(status), now, now, id, store.TenantIDFromContext(ctx))
		return err
	case VersionRejected:
		_, err := s.db.ExecContext(ctx,
			`UPDATE workflow_versions SET status = $1, rejection_reason = $2, updated_at = $3
			 WHERE id = $4 AND EXISTS (SELECT 1 FROM workflow_definitions d WHERE d.id = workflow_versions.workflow_id AND d.tenant_id = $5)`,
			string(status), reason, now, id, store.TenantIDFromContext(ctx))
		return err
	default:
		_, err := s.db.ExecContext(ctx,
			`UPDATE workflow_versions SET status = $1, updated_at = $2
			 WHERE id = $3 AND EXISTS (SELECT 1 FROM workflow_definitions d WHERE d.id = workflow_versions.workflow_id AND d.tenant_id = $4)`,
			string(status), now, id, store.TenantIDFromContext(ctx))
		return err
	}
}

func (s *PGWorkflowStore) ListVersions(ctx context.Context, workflowID string) ([]WorkflowVersion, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, workflow_id, version_number, status, graph_json, submitted_at, published_at, rejection_reason, created_at, updated_at
		 FROM workflow_versions v
		 WHERE v.workflow_id = $1
		   AND EXISTS (SELECT 1 FROM workflow_definitions d WHERE d.id = v.workflow_id AND d.tenant_id = $2)
		 ORDER BY created_at DESC`, workflowID, store.TenantIDFromContext(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var versions []WorkflowVersion
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
// ListPendingReviews — paginated at 50 items per page, sorted by submitted_at ASC (oldest first)
// ---------------------------------------------------------------------------

func (s *PGWorkflowStore) ListPendingReviews(ctx context.Context, page, pageSize int) ([]WorkflowVersion, int, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * pageSize

	// Get total count of pending reviews
	var total int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*)
		 FROM workflow_versions v
		 JOIN workflow_definitions d ON d.id = v.workflow_id
		 WHERE v.status = 'pending_review' AND d.tenant_id = $1`, store.TenantIDFromContext(ctx)).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return nil, 0, nil
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT v.id, v.workflow_id, v.version_number, v.status, v.graph_json, v.submitted_at, v.published_at, v.rejection_reason, v.created_at, v.updated_at
		 FROM workflow_versions v
		 JOIN workflow_definitions d ON d.id = v.workflow_id
		 WHERE v.status = 'pending_review' AND d.tenant_id = $1
		 ORDER BY submitted_at ASC
		 LIMIT $2 OFFSET $3`, store.TenantIDFromContext(ctx), pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var versions []WorkflowVersion
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
func (s *PGWorkflowStore) scanVersion(row *sql.Row) (*WorkflowVersion, error) {
	var (
		ver             WorkflowVersion
		status          string
		graphJSON       []byte
		submittedAt     any
		publishedAt     any
		rejectionReason string
		createdAt       any
		updatedAt       any
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

	ver.Status = VersionStatus(status)
	ver.RejectionReason = rejectionReason
	var err error
	if ver.CreatedAt, err = scanWorkflowStoreTime(createdAt); err != nil {
		return nil, fmt.Errorf("scan created_at: %w", err)
	}
	if ver.UpdatedAt, err = scanWorkflowStoreTime(updatedAt); err != nil {
		return nil, fmt.Errorf("scan updated_at: %w", err)
	}

	if t, ok, err := scanNullableWorkflowStoreTime(submittedAt); err != nil {
		return nil, fmt.Errorf("scan submitted_at: %w", err)
	} else if ok {
		ver.SubmittedAt = &t
	}
	if t, ok, err := scanNullableWorkflowStoreTime(publishedAt); err != nil {
		return nil, fmt.Errorf("scan published_at: %w", err)
	} else if ok {
		ver.PublishedAt = &t
	}

	if err := json.Unmarshal(graphJSON, &ver.Graph); err != nil {
		return nil, err
	}

	return &ver, nil
}

// scanVersionFromRows scans the current row from sql.Rows into a WorkflowVersion.
func (s *PGWorkflowStore) scanVersionFromRows(rows *sql.Rows) (*WorkflowVersion, error) {
	var (
		ver             WorkflowVersion
		status          string
		graphJSON       []byte
		submittedAt     any
		publishedAt     any
		rejectionReason string
		createdAt       any
		updatedAt       any
	)
	if err := rows.Scan(
		&ver.ID, &ver.WorkflowID, &ver.VersionNumber, &status, &graphJSON,
		&submittedAt, &publishedAt, &rejectionReason, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}

	ver.Status = VersionStatus(status)
	ver.RejectionReason = rejectionReason
	var err error
	if ver.CreatedAt, err = scanWorkflowStoreTime(createdAt); err != nil {
		return nil, fmt.Errorf("scan created_at: %w", err)
	}
	if ver.UpdatedAt, err = scanWorkflowStoreTime(updatedAt); err != nil {
		return nil, fmt.Errorf("scan updated_at: %w", err)
	}

	if t, ok, err := scanNullableWorkflowStoreTime(submittedAt); err != nil {
		return nil, fmt.Errorf("scan submitted_at: %w", err)
	} else if ok {
		ver.SubmittedAt = &t
	}
	if t, ok, err := scanNullableWorkflowStoreTime(publishedAt); err != nil {
		return nil, fmt.Errorf("scan published_at: %w", err)
	} else if ok {
		ver.PublishedAt = &t
	}

	if err := json.Unmarshal(graphJSON, &ver.Graph); err != nil {
		return nil, err
	}

	return &ver, nil
}

func scanNullableWorkflowStoreTime(value any) (time.Time, bool, error) {
	if value == nil {
		return time.Time{}, false, nil
	}
	if ns, ok := value.(sql.NullString); ok && !ns.Valid {
		return time.Time{}, false, nil
	}
	t, err := scanWorkflowStoreTime(value)
	if err != nil {
		return time.Time{}, false, err
	}
	if t.IsZero() {
		return time.Time{}, false, nil
	}
	return t, true, nil
}

func scanWorkflowStoreTime(value any) (time.Time, error) {
	switch v := value.(type) {
	case time.Time:
		return v.UTC(), nil
	case string:
		return parseWorkflowStoreTimeString(v)
	case []byte:
		return parseWorkflowStoreTimeString(string(v))
	case sql.NullString:
		if !v.Valid {
			return time.Time{}, nil
		}
		return parseWorkflowStoreTimeString(v.String)
	default:
		return time.Time{}, fmt.Errorf("unsupported timestamp type %T", value)
	}
}

func parseWorkflowStoreTimeString(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid timestamp %q", value)
}
