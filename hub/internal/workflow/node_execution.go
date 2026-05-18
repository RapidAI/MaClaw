package workflow

import (
	"context"
	"fmt"
	"time"
)

// RecordNodeExecution persists a node execution record and appends the
// corresponding audit trail event. The event type is derived from the
// execution status: "node_completed", "node_skipped", or "node_failed".
func RecordNodeExecution(ctx context.Context, auditStore AuditStore, instanceStore InstanceStore, exec *NodeExecution) error {
	// Persist the node execution record.
	if err := instanceStore.CreateNodeExecution(ctx, exec); err != nil {
		return fmt.Errorf("create node execution: %w", err)
	}

	// Determine audit event type based on status.
	var eventType string
	switch exec.Status {
	case NodeCompleted:
		eventType = "node_completed"
	case NodeSkipped:
		eventType = "node_skipped"
	case NodeFailed:
		eventType = "node_failed"
	default:
		// For pending/running states we don't emit an audit event yet.
		return nil
	}

	// Append audit trail event.
	entry := &AuditEntry{
		ID:         generateID("audit"),
		InstanceID: exec.InstanceID,
		NodeID:     exec.NodeID,
		EventType:  eventType,
		Details:    fmt.Sprintf("node_type=%s", exec.NodeType),
		Timestamp:  NormalizeAuditTimestamp(time.Time{}),
	}
	if exec.FailReason != "" {
		entry.Details += fmt.Sprintf(", reason=%s", exec.FailReason)
	}

	if err := auditStore.Append(ctx, entry); err != nil {
		return fmt.Errorf("append audit entry: %w", err)
	}

	return nil
}
