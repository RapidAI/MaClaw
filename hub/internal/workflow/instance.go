package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// InstanceStatus represents the execution status of a workflow instance.
type InstanceStatus string

const (
	InstanceRunning   InstanceStatus = "running"
	InstanceCompleted InstanceStatus = "completed"
	InstanceFailed    InstanceStatus = "failed"
	InstanceBlocked   InstanceStatus = "blocked"
	InstanceWithdrawn InstanceStatus = "withdrawn"
	InstanceCancelled InstanceStatus = "cancelled"
)

// WorkflowInstance represents a running execution of a workflow.
type WorkflowInstance struct {
	ID            string                 `json:"id"`
	TenantID      string                 `json:"tenant_id,omitempty"`
	WorkflowID    string                 `json:"workflow_id"`
	VersionID     string                 `json:"version_id"`
	Status        InstanceStatus         `json:"status"`
	CurrentNodeID string                 `json:"current_node_id"`
	InstanceData  map[string]interface{} `json:"instance_data"`
	TriggerData   string                 `json:"trigger_data"`
	CreatedAt     time.Time              `json:"created_at"`
	CompletedAt   *time.Time             `json:"completed_at,omitempty"`

	// RowVersion is an optimistic-locking guard on the instance row, used to
	// serialize concurrent approval-state writes in ResumeInstance so that
	// near-simultaneous decisions on the same node (countersign / any-N-of-M)
	// cannot lose a vote across processes (Requirement 2.6). It is loaded by
	// Get and bumped by UpdateInstanceDataCAS. This is an internal persistence
	// detail, not part of the API contract, hence json:"-".
	RowVersion int64 `json:"-"`
}

// NodeStatus represents the execution status of a single node within an instance.
type NodeStatus string

const (
	NodePending   NodeStatus = "pending"
	NodeRunning   NodeStatus = "running"
	NodeCompleted NodeStatus = "completed"
	NodeFailed    NodeStatus = "failed"
	NodeBlocked   NodeStatus = "blocked"
	NodeSkipped   NodeStatus = "skipped"
)

// NodeExecution tracks the state of a single node within an instance.
type NodeExecution struct {
	ID          string          `json:"id"`
	InstanceID  string          `json:"instance_id"`
	NodeID      string          `json:"node_id"`
	NodeType    NodeType        `json:"node_type"`
	Status      NodeStatus      `json:"status"`
	StartedAt   time.Time       `json:"started_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	FailReason  string          `json:"fail_reason,omitempty"`
}

// InstanceStore provides CRUD operations for workflow instances and node executions.
type InstanceStore interface {
	Create(ctx context.Context, inst *WorkflowInstance) error
	Get(ctx context.Context, id string) (*WorkflowInstance, error)
	// UpdateStatus atomically transitions the instance to the new status.
	// Implementations MUST use a conditional update (e.g., WHERE status = current_running_status)
	// to prevent race conditions in concurrent withdrawal/completion scenarios.
	// Returns an error if the status transition fails (e.g., instance already changed by another request).
	UpdateStatus(ctx context.Context, id string, status InstanceStatus) error
	UpdateCurrentNode(ctx context.Context, id, nodeID string) error
	UpdateInstanceData(ctx context.Context, id string, data map[string]interface{}) error

	CreateNodeExecution(ctx context.Context, exec *NodeExecution) error
	UpdateNodeExecution(ctx context.Context, id string, status NodeStatus, result json.RawMessage, failReason string) error
	GetPendingApprovals(ctx context.Context, approverID string) ([]NodeExecution, error)

	// Directory query methods for categorized workflow views.
	QueryMyInitiated(ctx context.Context, userID string, filter DirectoryFilter) ([]DirectoryItem, int, error)
	QueryPendingMyAction(ctx context.Context, userID string, filter DirectoryFilter) ([]DirectoryItem, int, error)
	QueryPendingMyConfirmation(ctx context.Context, userID string, filter DirectoryFilter) ([]DirectoryItem, int, error)
	QueryCompleted(ctx context.Context, userID string, filter DirectoryFilter) ([]DirectoryItem, int, error)
}

// OptimisticInstanceDataUpdater is an OPTIONAL capability an InstanceStore may
// implement to persist instance_data under an optimistic-locking guard, so that
// near-simultaneous approval decisions on the same node cannot clobber each
// other's persisted vote across processes (Finding 1.6 / Requirement 2.6).
//
// The plain InstanceStore.UpdateInstanceData does a full unconditional
// overwrite: two concurrent read-modify-write cycles each read the old data,
// merge their own decision, and write back — the second writer's overwrite
// silently discards the first writer's vote. A per-process mutex closes that
// window within one process, but a multi-process Hub sharing one database still
// races. UpdateInstanceDataCAS guards the write with the row version observed at
// read time (conditional UPDATE ... WHERE row_version = expectedVersion); on a
// version mismatch it reports a conflict so the caller can re-read and re-apply.
//
// This is a wiring-level mechanism: it changes only HOW approvalNodeState is
// persisted. The per-mode decision logic and the conditional UpdateStatus
// contract are unchanged. Both production stores (PgInstanceStore and the
// sqlite instanceStore) satisfy it; ResumeInstance type-asserts for it at
// runtime and falls back to UpdateInstanceData when it is absent.
type OptimisticInstanceDataUpdater interface {
	// UpdateInstanceDataCAS persists data only if the instance row's version
	// still equals expectedVersion. On success it returns the new (bumped)
	// version. On a version mismatch it returns (0, ErrInstanceVersionConflict)
	// without writing, so the caller can re-read and retry.
	UpdateInstanceDataCAS(ctx context.Context, id string, expectedVersion int64, data map[string]interface{}) (int64, error)
}

// ErrInstanceVersionConflict is returned by UpdateInstanceDataCAS when the
// instance row's version no longer matches the version observed at read time —
// i.e. another writer applied a decision concurrently. The caller re-reads the
// instance and re-applies its decision against the fresh state.
var ErrInstanceVersionConflict = errors.New("workflow instance version conflict")
