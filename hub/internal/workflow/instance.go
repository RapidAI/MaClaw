package workflow

import (
	"context"
	"encoding/json"
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
