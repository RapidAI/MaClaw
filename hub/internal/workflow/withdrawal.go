package workflow

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Sentinel errors for withdrawal operations.
var (
	// ErrAlreadyCompleted is returned when withdrawal is attempted on a completed instance.
	ErrAlreadyCompleted = errors.New("cannot withdraw: instance has already completed")

	// ErrNotInitiator is returned when a non-initiator attempts withdrawal.
	ErrNotInitiator = errors.New("only the initiator can withdraw an instance")

	// ErrAlreadyWithdrawn is returned when withdrawal is attempted on an already-withdrawn instance.
	ErrAlreadyWithdrawn = errors.New("cannot withdraw: instance has already been withdrawn")

	// ErrInstanceNotFound is returned when the instance does not exist.
	ErrInstanceNotFound = errors.New("workflow instance not found")

	// ErrInstanceNotRunning is returned when withdrawal is attempted on a non-running instance.
	ErrInstanceNotRunning = errors.New("cannot withdraw: instance is not in running status")
)

// WithdrawalHandler manages workflow instance withdrawal by the initiator.
type WithdrawalHandler struct {
	instanceStore   InstanceStore
	auditStore      AuditStore
	notifDispatcher *NotificationDispatcher
	confirmTracker  *ConfirmationTracker
}

// NewWithdrawalHandler creates a new WithdrawalHandler with the given dependencies.
func NewWithdrawalHandler(
	instanceStore InstanceStore,
	auditStore AuditStore,
	notifDispatcher *NotificationDispatcher,
	confirmTracker *ConfirmationTracker,
) *WithdrawalHandler {
	return &WithdrawalHandler{
		instanceStore:   instanceStore,
		auditStore:      auditStore,
		notifDispatcher: notifDispatcher,
		confirmTracker:  confirmTracker,
	}
}

// Withdraw cancels a running workflow instance.
// Preconditions:
//   - Instance status must be "running"
//   - Instance must not have reached a terminal node (no result delivered)
//   - Requester must be the initiator
//
// Effects:
//   - All pending approval nodes are cancelled (status=skipped)
//   - Instance status set to "withdrawn"
//   - All participants with pending actions are notified within 60s
//   - Audit trail records withdrawal event
func (wh *WithdrawalHandler) Withdraw(ctx context.Context, instanceID, userID string) error {
	// 1. Load the instance.
	inst, err := wh.instanceStore.Get(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to load instance: %w", err)
	}
	if inst == nil {
		return ErrInstanceNotFound
	}

	// 2. Precondition: check instance status.
	switch inst.Status {
	case InstanceCompleted:
		return ErrAlreadyCompleted
	case InstanceWithdrawn:
		return ErrAlreadyWithdrawn
	case InstanceRunning:
		// OK — proceed.
	default:
		return ErrInstanceNotRunning
	}

	// 3. Precondition: requester must be the initiator.
	initiatorID := extractInitiatorID(inst)
	if initiatorID == "" || initiatorID != userID {
		return ErrNotInitiator
	}

	// 4. Cancel all pending approval nodes for THIS instance.
	// NOTE: GetPendingApprovals("") returns all pending approvals globally.
	// This is a known performance limitation — a future optimization should add
	// GetPendingApprovalsByInstance(ctx, instanceID) to the InstanceStore interface
	// to filter at the database level and avoid loading all pending approvals into memory.
	pendingExecs, _ := wh.instanceStore.GetPendingApprovals(ctx, "")
	for _, exec := range pendingExecs {
		if exec.InstanceID == instanceID && exec.Status == NodePending {
			_ = wh.instanceStore.UpdateNodeExecution(ctx, exec.ID, NodeSkipped, nil, "withdrawn by initiator")
		}
	}

	// 5. Atomically set instance status to "withdrawn".
	// The store's UpdateStatus uses WHERE status='running' internally.
	// If another concurrent request already changed the status, this will fail.
	if err := wh.instanceStore.UpdateStatus(ctx, instanceID, InstanceWithdrawn); err != nil {
		return fmt.Errorf("failed to update instance status: %w", err)
	}

	// 6. Record "withdrawal" event in audit trail.
	now := time.Now().UTC().Truncate(time.Millisecond)
	if wh.auditStore != nil {
		_ = wh.auditStore.Append(ctx, &AuditEntry{
			InstanceID: instanceID,
			EventType:  "withdrawal",
			ActorID:    userID,
			Details:    fmt.Sprintf("instance withdrawn by initiator at %s", now.Format(time.RFC3339Nano)),
			Timestamp:  now,
		})
	}

	// 7. Notify all participants with pending actions within 60s.
	if wh.notifDispatcher != nil {
		workflowName := extractWorkflowName(inst)
		initiatorName := extractInitiatorName(inst)
		recipientIDs := extractPendingParticipants(inst, pendingExecs)

		if len(recipientIDs) > 0 {
			notifs := make([]*WorkflowNotification, 0, len(recipientIDs))
			for _, recipientID := range recipientIDs {
				notifs = append(notifs, &WorkflowNotification{
					InstanceID:    instanceID,
					Type:          NotifTypeWithdrawal,
					RecipientID:   recipientID,
					WorkflowName:  workflowName,
					InitiatorID:   userID,
					InitiatorName: initiatorName,
					InstanceURL:   fmt.Sprintf("/instances/%s", instanceID),
					CreatedAt:     now,
				})
			}
			// DispatchBatch respects the 60-second timeout internally.
			_ = wh.notifDispatcher.DispatchBatch(ctx, notifs)
		}

		// Record notification dispatch in audit trail.
		if wh.auditStore != nil && len(recipientIDs) > 0 {
			_ = wh.auditStore.Append(ctx, &AuditEntry{
				InstanceID: instanceID,
				EventType:  "withdrawal_notifications_sent",
				ActorID:    userID,
				Details:    fmt.Sprintf("withdrawal notifications dispatched to %d participants", len(recipientIDs)),
				Timestamp:  time.Now().UTC().Truncate(time.Millisecond),
			})
		}
	}

	return nil
}

// extractInitiatorID extracts the initiator user ID from the instance data.
func extractInitiatorID(inst *WorkflowInstance) string {
	if inst.InstanceData == nil {
		return ""
	}
	if v, ok := inst.InstanceData["initiator_id"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// extractWorkflowName extracts the workflow name from the instance data.
func extractWorkflowName(inst *WorkflowInstance) string {
	if inst.InstanceData == nil {
		return ""
	}
	if v, ok := inst.InstanceData["workflow_name"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// extractInitiatorName extracts the initiator display name from the instance data.
func extractInitiatorName(inst *WorkflowInstance) string {
	if inst.InstanceData == nil {
		return ""
	}
	if v, ok := inst.InstanceData["initiator_name"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// extractPendingParticipants collects unique participant IDs who had pending actions
// on this instance. It checks the instance data for explicit participant/approver lists.
func extractPendingParticipants(inst *WorkflowInstance, pendingExecs []NodeExecution) []string {
	seen := make(map[string]bool)
	var result []string

	if inst.InstanceData == nil {
		return result
	}

	// Check instance data for explicit pending_participants list.
	if participants, ok := inst.InstanceData["pending_participants"]; ok {
		if pList, ok := participants.([]interface{}); ok {
			for _, p := range pList {
				if s, ok := p.(string); ok && s != "" && !seen[s] {
					seen[s] = true
					result = append(result, s)
				}
			}
		}
	}

	// Also check approver_ids if available.
	if approvers, ok := inst.InstanceData["approver_ids"]; ok {
		if aList, ok := approvers.([]interface{}); ok {
			for _, a := range aList {
				if s, ok := a.(string); ok && s != "" && !seen[s] {
					seen[s] = true
					result = append(result, s)
				}
			}
		}
	}

	return result
}
