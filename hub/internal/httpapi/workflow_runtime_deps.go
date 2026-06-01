package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	corea2a "github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
)

// approvalEngineFromID identifies the Hub workflow engine as the sender of
// approval-request A2A envelopes. ValidateCurrentHub requires a non-empty
// from_id, and approvers use it to attribute the request to the engine.
const approvalEngineFromID = "hub-workflow-engine"

// approvalRequestWireType is the WebSocket message type used to deliver an
// approval-request A2A envelope to an approver machine. It mirrors the
// `ve:`-prefixed convention used by VE/group messaging (handleVEEvent /
// isVEEvent) and carries the GroupEnvelope under payload.envelope, matching
// the wrapping used by the group-discussion sender.
const approvalRequestWireType = "ve:approval_request"

// HubApprovalDispatcher is the real ApprovalDispatcher. It delivers approval
// requests to approver machines over the Hub machine sender
// (device.Service.SendToMachine) — the same mechanism the NotificationDispatcher
// and VE/group messaging already use. It implements the unchanged
// ApprovalDispatcher interface (Dispatch / DispatchFallback) so the executor
// call sites in executeApprovalNode and handleFallbackRouting are untouched;
// only the concrete implementation changes (Preservation 3.4).
type HubApprovalDispatcher struct {
	sender machineCommandSender
}

// Compile-time assertion that HubApprovalDispatcher satisfies the unchanged
// ApprovalDispatcher interface the executor depends on.
var _ workflow.ApprovalDispatcher = (*HubApprovalDispatcher)(nil)

// NewHubApprovalDispatcher constructs a real ApprovalDispatcher backed by the
// given machine sender (e.g. *device.Service). The router wires this into the
// WorkflowExecutor as the ApprovalDispatcher.
func NewHubApprovalDispatcher(sender machineCommandSender) *HubApprovalDispatcher {
	return &HubApprovalDispatcher{sender: sender}
}

// Dispatch delivers an approval request to the primary approver machine.
func (d *HubApprovalDispatcher) Dispatch(ctx context.Context, req *workflow.ApprovalRequest, approverID string) error {
	return d.deliver(ctx, req, approverID, "")
}

// DispatchFallback delivers an approval request to a fallback approver machine,
// annotating the delivery with the reason the primary approver was bypassed.
func (d *HubApprovalDispatcher) DispatchFallback(ctx context.Context, req *workflow.ApprovalRequest, fallbackID string, reason string) error {
	return d.deliver(ctx, req, fallbackID, reason)
}

// deliver validates the approval request, builds an approval-request A2A
// envelope, and sends it to the approver machine via the machine sender.
func (d *HubApprovalDispatcher) deliver(ctx context.Context, req *workflow.ApprovalRequest, approverID string, fallbackReason string) error {
	if d == nil || d.sender == nil {
		return errors.New("approval dispatcher has no machine sender configured")
	}
	if req == nil {
		return errors.New("approval request is nil")
	}
	approverID = strings.TrimSpace(approverID)
	if approverID == "" {
		return errors.New("approver id is required")
	}

	// Validate (and, if oversized, truncate Details) before delivery. This is
	// the same contract ValidateApprovalRequest enforces everywhere else.
	if err := workflow.ValidateApprovalRequest(req); err != nil {
		return fmt.Errorf("validate approval request: %w", err)
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal approval request: %w", err)
	}

	envelope := corea2a.NewGroupEnvelope(newGroupDiscussionID("approval-env"), corea2a.GroupMessageApprovalRequest, approvalEngineFromID, time.Now().UTC())
	envelope.ToIDs = []string{approverID}
	envelope.SessionID = strings.TrimSpace(req.InstanceID)
	envelope.Payload = payload
	if err := envelope.ValidateCurrentHub(); err != nil {
		return fmt.Errorf("invalid approval envelope: %w", err)
	}

	msgPayload := map[string]any{"envelope": envelope}
	if reason := strings.TrimSpace(fallbackReason); reason != "" {
		msgPayload["is_fallback"] = true
		msgPayload["fallback_reason"] = reason
	}

	return d.sender.SendToMachine(approverID, map[string]any{
		"type":    approvalRequestWireType,
		"ts":      time.Now().Unix(),
		"payload": msgPayload,
	})
}

// machinePresenceChecker is the minimal slice of the Hub device service the
// availability checker depends on: whether a machine currently has a live
// desktop connection. *device.Service satisfies this via IsMachineOnline.
type machinePresenceChecker interface {
	IsMachineOnline(machineID string) bool
}

// HubAvailabilityChecker is the real HumanApproverChecker. It mirrors real
// approver presence by delegating to the Hub's device/presence system
// (device.Service.IsMachineOnline) — the same source SendToMachine delivery
// relies on. It implements the unchanged HumanApproverChecker interface, so
// EscalationManager and HandleUnavailable / HandleTimeout / HandleQueueFull are
// untouched; only the availability source changes (Preservation 3.6).
type HubAvailabilityChecker struct {
	presence machinePresenceChecker
}

// Compile-time assertion that HubAvailabilityChecker satisfies the unchanged
// HumanApproverChecker interface the EscalationManager depends on.
var _ workflow.HumanApproverChecker = (*HubAvailabilityChecker)(nil)

// NewHubAvailabilityChecker constructs a real HumanApproverChecker backed by the
// given presence source (e.g. *device.Service). The router wires this into the
// EscalationManager as the HumanApproverChecker.
func NewHubAvailabilityChecker(presence machinePresenceChecker) *HubAvailabilityChecker {
	return &HubAvailabilityChecker{presence: presence}
}

// IsAvailable reports whether the approver's machine currently has a live
// connection. An unconfigured presence source or an empty approver id reports
// unavailable so escalation/queueing fires rather than silently assuming the
// approver is reachable.
func (c *HubAvailabilityChecker) IsAvailable(ctx context.Context, approverID string) bool {
	if c == nil || c.presence == nil {
		return false
	}
	approverID = strings.TrimSpace(approverID)
	if approverID == "" {
		return false
	}
	return c.presence.IsMachineOnline(approverID)
}
