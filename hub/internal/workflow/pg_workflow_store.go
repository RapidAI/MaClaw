package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
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
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO workflow_definitions (id, owner_id, name, description, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		def.ID,
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
		`SELECT id, owner_id, name, description, created_at, updated_at
		 FROM workflow_definitions WHERE id = $1`, id)

	var def WorkflowDefinition
	if err := row.Scan(&def.ID, &def.OwnerID, &def.Name, &def.Description, &def.CreatedAt, &def.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	def.CreatedAt = def.CreatedAt.UTC()
	def.UpdatedAt = def.UpdatedAt.UTC()
	return &def, nil
}

func (s *PGWorkflowStore) ListWorkflows(ctx context.Context, ownerID string) ([]WorkflowDefinition, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_id, name, description, created_at, updated_at
		 FROM workflow_definitions WHERE owner_id = $1 ORDER BY updated_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var defs []WorkflowDefinition
	for rows.Next() {
		var def WorkflowDefinition
		if err := rows.Scan(&def.ID, &def.OwnerID, &def.Name, &def.Description, &def.CreatedAt, &def.UpdatedAt); err != nil {
			return nil, err
		}
		def.CreatedAt = def.CreatedAt.UTC()
		def.UpdatedAt = def.UpdatedAt.UTC()
		defs = append(defs, def)
	}
	return defs, rows.Err()
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

func (s *PGWorkflowStore) GetVersion(ctx context.Context, id string) (*WorkflowVersion, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, workflow_id, version_number, status, graph_json, submitted_at, published_at, rejection_reason, created_at, updated_at
		 FROM workflow_versions WHERE id = $1`, id)
	return s.scanVersion(row)
}

func (s *PGWorkflowStore) GetPublishedVersion(ctx context.Context, workflowID string) (*WorkflowVersion, error) {
	// Uses the unique partial index: idx_wf_ver_published ON workflow_versions(workflow_id) WHERE status = 'published'
	row := s.db.QueryRowContext(ctx,
		`SELECT id, workflow_id, version_number, status, graph_json, submitted_at, published_at, rejection_reason, created_at, updated_at
		 FROM workflow_versions WHERE workflow_id = $1 AND status = 'published'`, workflowID)
	return s.scanVersion(row)
}

func (s *PGWorkflowStore) UpdateVersionStatus(ctx context.Context, id string, status VersionStatus, reason string) error {
	now := time.Now().UTC()

	switch status {
	case VersionPendingReview:
		_, err := s.db.ExecContext(ctx,
			`UPDATE workflow_versions SET status = $1, submitted_at = $2, updated_at = $3 WHERE id = $4`,
			string(status), now, now, id)
		return err
	case VersionPublished:
		_, err := s.db.ExecContext(ctx,
			`UPDATE workflow_versions SET status = $1, published_at = $2, updated_at = $3 WHERE id = $4`,
			string(status), now, now, id)
		return err
	case VersionRejected:
		_, err := s.db.ExecContext(ctx,
			`UPDATE workflow_versions SET status = $1, rejection_reason = $2, updated_at = $3 WHERE id = $4`,
			string(status), reason, now, id)
		return err
	default:
		_, err := s.db.ExecContext(ctx,
			`UPDATE workflow_versions SET status = $1, updated_at = $2 WHERE id = $3`,
			string(status), now, id)
		return err
	}
}

func (s *PGWorkflowStore) ListVersions(ctx context.Context, workflowID string) ([]WorkflowVersion, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, workflow_id, version_number, status, graph_json, submitted_at, published_at, rejection_reason, created_at, updated_at
		 FROM workflow_versions WHERE workflow_id = $1 ORDER BY created_at DESC`, workflowID)
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
		`SELECT COUNT(*) FROM workflow_versions WHERE status = 'pending_review'`).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return nil, 0, nil
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, workflow_id, version_number, status, graph_json, submitted_at, published_at, rejection_reason, created_at, updated_at
		 FROM workflow_versions
		 WHERE status = 'pending_review'
		 ORDER BY submitted_at ASC
		 LIMIT $1 OFFSET $2`, pageSize, offset)
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
		submittedAt     *time.Time
		publishedAt     *time.Time
		rejectionReason string
	)
	if err := row.Scan(
		&ver.ID, &ver.WorkflowID, &ver.VersionNumber, &status, &graphJSON,
		&submittedAt, &publishedAt, &rejectionReason, &ver.CreatedAt, &ver.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	ver.Status = VersionStatus(status)
	ver.RejectionReason = rejectionReason
	ver.CreatedAt = ver.CreatedAt.UTC()
	ver.UpdatedAt = ver.UpdatedAt.UTC()

	if submittedAt != nil {
		t := submittedAt.UTC()
		ver.SubmittedAt = &t
	}
	if publishedAt != nil {
		t := publishedAt.UTC()
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
		submittedAt     *time.Time
		publishedAt     *time.Time
		rejectionReason string
	)
	if err := rows.Scan(
		&ver.ID, &ver.WorkflowID, &ver.VersionNumber, &status, &graphJSON,
		&submittedAt, &publishedAt, &rejectionReason, &ver.CreatedAt, &ver.UpdatedAt,
	); err != nil {
		return nil, err
	}

	ver.Status = VersionStatus(status)
	ver.RejectionReason = rejectionReason
	ver.CreatedAt = ver.CreatedAt.UTC()
	ver.UpdatedAt = ver.UpdatedAt.UTC()

	if submittedAt != nil {
		t := submittedAt.UTC()
		ver.SubmittedAt = &t
	}
	if publishedAt != nil {
		t := publishedAt.UTC()
		ver.PublishedAt = &t
	}

	if err := json.Unmarshal(graphJSON, &ver.Graph); err != nil {
		return nil, err
	}

	return &ver, nil
}
