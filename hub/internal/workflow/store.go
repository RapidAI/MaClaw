package workflow

import (
	"context"
	"time"
)

// WorkflowDefinition represents a complete workflow owned by a user.
type WorkflowDefinition struct {
	ID          string    `json:"id"`
	OwnerID     string    `json:"owner_id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// WorkflowVersion represents a specific revision of a workflow.
type WorkflowVersion struct {
	ID              string        `json:"id"`
	WorkflowID      string        `json:"workflow_id"`
	VersionNumber   string        `json:"version_number"` // major.minor.patch
	Status          VersionStatus `json:"status"`
	Graph           WorkflowGraph `json:"graph"`
	SubmittedAt     *time.Time    `json:"submitted_at,omitempty"`
	PublishedAt     *time.Time    `json:"published_at,omitempty"`
	RejectionReason string        `json:"rejection_reason,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
}

// VersionStatus represents the lifecycle state of a workflow version.
type VersionStatus string

const (
	VersionDraft         VersionStatus = "draft"
	VersionPendingReview VersionStatus = "pending_review"
	VersionPublished     VersionStatus = "published"
	VersionRejected      VersionStatus = "rejected"
	VersionSuperseded    VersionStatus = "superseded"
	VersionUnpublished   VersionStatus = "unpublished"
)

// WorkflowStore provides CRUD operations for workflow definitions and versions.
type WorkflowStore interface {
	// CreateWorkflow creates a new workflow definition.
	CreateWorkflow(ctx context.Context, def *WorkflowDefinition) error
	// GetWorkflow retrieves a workflow definition by ID.
	GetWorkflow(ctx context.Context, id string) (*WorkflowDefinition, error)
	// ListWorkflows returns all workflow definitions owned by the given user.
	ListWorkflows(ctx context.Context, ownerID string) ([]WorkflowDefinition, error)

	// CreateVersion creates a new workflow version.
	CreateVersion(ctx context.Context, ver *WorkflowVersion) error
	// GetVersion retrieves a workflow version by ID.
	GetVersion(ctx context.Context, id string) (*WorkflowVersion, error)
	// GetPublishedVersion returns the currently published version for a workflow.
	GetPublishedVersion(ctx context.Context, workflowID string) (*WorkflowVersion, error)
	// UpdateVersionStatus transitions a version to a new status with an optional reason.
	UpdateVersionStatus(ctx context.Context, id string, status VersionStatus, reason string) error
	// ListVersions returns all versions for a given workflow definition.
	ListVersions(ctx context.Context, workflowID string) ([]WorkflowVersion, error)

	// ListPendingReviews returns versions in pending_review status with pagination.
	// Returns the matching versions, total count, and any error.
	ListPendingReviews(ctx context.Context, page, pageSize int) ([]WorkflowVersion, int, error)
}
