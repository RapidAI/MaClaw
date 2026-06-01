package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
)

// workflowNotificationWireType is the WebSocket message type used to deliver a
// workflow completion/reminder notification to a recipient machine. It mirrors
// the `ve:`-prefixed convention used by HubApprovalDispatcher
// (ve:approval_request) so desktop-client handler routing stays consistent.
const workflowNotificationWireType = "ve:workflow_notification"

// HubNotifier is the real HubInAppNotifier. It delivers workflow completion
// notifications and confirmation reminders to recipient machines over the Hub
// machine sender (device.Service.SendToMachine) — the same mechanism
// HubApprovalDispatcher uses for approval requests. It implements the unchanged
// HubInAppNotifier interface so the NotificationDispatcher, ConfirmationTracker,
// and WorkflowExecutor call sites are untouched; only the concrete dependency
// wired into NewNotificationDispatcher changes from nil to a real implementation.
//
// HubNotifier holds NO cadence, counting, interval, deadline, or dedup state.
// It delivers exactly one machine message per Send invocation and defers all
// reminder cadence/counting control to ConfirmationTracker.
type HubNotifier struct {
	sender   machineCommandSender
	presence machinePresenceChecker // optional; nil is supported
}

// Compile-time assertion mirroring var _ workflow.ApprovalDispatcher = (*HubApprovalDispatcher)(nil).
var _ workflow.HubInAppNotifier = (*HubNotifier)(nil)

// NewHubNotifier constructs a real HubInAppNotifier backed by the given machine
// sender (e.g. *device.Service). The presence source is optional and used only
// for an opt-in presence read; SendToMachine already returns ErrMachineOffline
// authoritatively, so delivery correctness does not depend on it.
func NewHubNotifier(sender machineCommandSender) *HubNotifier {
	return &HubNotifier{sender: sender}
}

// WithPresence attaches an optional presence source. Returns the notifier for
// chaining, mirroring ConfirmationTracker.SetWorkflowStore's setter pattern.
func (n *HubNotifier) WithPresence(p machinePresenceChecker) *HubNotifier {
	n.presence = p
	return n
}

// Send delivers a single workflow notification to the recipient's machine via
// the machine sender. It makes exactly one SendToMachine call on every path
// that reaches the transport. The empty-recipient, nil-payload, and nil-sender
// guards return before any transport call (zero SendToMachine calls). A non-nil
// send error is wrapped with %w, preserving device.ErrMachineOffline so callers
// can distinguish the offline condition via errors.Is.
func (n *HubNotifier) Send(ctx context.Context, recipientID string, notif *workflow.InAppNotification) error {
	if n == nil || n.sender == nil {
		return errors.New("hub notifier has no machine sender configured")
	}
	if notif == nil {
		return errors.New("in-app notification payload is nil")
	}
	recipientID = strings.TrimSpace(recipientID)
	if recipientID == "" {
		return errors.New("recipient id is required")
	}

	payload, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshal in-app notification: %w", err)
	}

	sendErr := n.sender.SendToMachine(recipientID, map[string]any{
		"type": workflowNotificationWireType,
		"ts":   time.Now().Unix(),
		"payload": map[string]any{
			"notification_type": notif.Type, // discriminator (result_executor | notifier | reminder | escalation | ...)
			"notification":      json.RawMessage(payload),
		},
	})
	if sendErr != nil {
		// %w preserves the underlying sentinel (notably device.ErrMachineOffline,
		// which SendToMachine returns for both no-connection and full-buffer cases)
		// so the dispatcher and other callers can distinguish the offline condition
		// via errors.Is without this notifier branching on it.
		return fmt.Errorf("deliver workflow notification to %s: %w", recipientID, sendErr)
	}
	return nil
}
