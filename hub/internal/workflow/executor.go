package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/condeval"
	"github.com/google/uuid"
)

// Sentinel errors for workflow execution.
var (
	ErrNoPublishedVersion = errors.New("no published version for workflow")
	ErrNoTriggerNode      = errors.New("workflow has no trigger node")
	ErrMultipleTriggers   = errors.New("workflow has multiple trigger nodes")
)

const (
	approvalDecisionApprove  = "approve"
	approvalDecisionReject   = "reject"
	approvalDecisionEscalate = "escalate"
)

// WorkflowExecutor manages the lifecycle of workflow instances.
type WorkflowExecutor struct {
	store           WorkflowStore
	instanceStore   InstanceStore
	auditStore      AuditStore
	dispatcher      ApprovalDispatcher
	notifier        WorkflowNotifier
	notifDispatcher *NotificationDispatcher
	confirmTracker  *ConfirmationTracker
}

// ApprovalDispatcher sends approval requests to VE approvers via A2A.
type ApprovalDispatcher interface {
	Dispatch(ctx context.Context, req *ApprovalRequest, approverID string) error
	DispatchFallback(ctx context.Context, req *ApprovalRequest, fallbackID string, reason string) error
}

// WorkflowNotifier sends notifications to workflow participants.
type WorkflowNotifier interface {
	// NotifyInitiator sends a notification to the workflow instance initiator.
	// reason describes why the notification is being sent (e.g., "approval node blocked").
	// details provides additional context (e.g., unavailable approver identity).
	NotifyInitiator(ctx context.Context, instanceID string, reason string, details string) error
}

// NewWorkflowExecutor creates a new WorkflowExecutor with the given dependencies.
func NewWorkflowExecutor(store WorkflowStore, instanceStore InstanceStore, auditStore AuditStore, dispatcher ApprovalDispatcher, opts ...ExecutorOption) *WorkflowExecutor {
	e := &WorkflowExecutor{
		store:         store,
		instanceStore: instanceStore,
		auditStore:    auditStore,
		dispatcher:    dispatcher,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// ExecutorOption configures optional dependencies on WorkflowExecutor.
type ExecutorOption func(*WorkflowExecutor)

// WithNotifier sets the workflow notifier for sending notifications to initiators.
func WithNotifier(n WorkflowNotifier) ExecutorOption {
	return func(e *WorkflowExecutor) {
		e.notifier = n
	}
}

// WithNotificationDispatcher sets the notification dispatcher for terminal node delivery.
func WithNotificationDispatcher(nd *NotificationDispatcher) ExecutorOption {
	return func(e *WorkflowExecutor) {
		e.notifDispatcher = nd
	}
}

// WithConfirmationTracker sets the confirmation tracker for post-completion tracking.
func WithConfirmationTracker(ct *ConfirmationTracker) ExecutorOption {
	return func(e *WorkflowExecutor) {
		e.confirmTracker = ct
	}
}

// StartInstance creates and begins executing a new workflow instance.
// It creates the instance bound to the current published version, executes
// from the trigger node, and advances through non-blocking nodes until
// reaching a blocking node (e.g., approval) or the end of the graph.
func (e *WorkflowExecutor) StartInstance(ctx context.Context, workflowID, triggerData string) (*WorkflowInstance, error) {
	ver, err := e.store.GetPublishedVersion(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("get published version: %w", err)
	}
	if ver == nil {
		return nil, ErrNoPublishedVersion
	}

	// Validate exactly one trigger node
	triggerNode, err := findSingleTriggerNode(ver.Graph)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	inst := &WorkflowInstance{
		ID:            generateID("inst"),
		WorkflowID:    workflowID,
		VersionID:     ver.ID,
		Status:        InstanceRunning,
		CurrentNodeID: triggerNode.ID,
		InstanceData:  make(map[string]interface{}),
		TriggerData:   triggerData,
		CreatedAt:     now,
	}

	// Parse trigger data into instance data
	if triggerData != "" {
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(triggerData), &data); err == nil {
			inst.InstanceData = data
		}
	}

	if err := e.instanceStore.Create(ctx, inst); err != nil {
		return nil, fmt.Errorf("create instance: %w", err)
	}

	_ = e.auditStore.Append(ctx, &AuditEntry{
		ID:         generateID("audit"),
		InstanceID: inst.ID,
		EventType:  "instance_created",
		Details:    fmt.Sprintf(`{"workflow_id":"%s","version_id":"%s"}`, workflowID, ver.ID),
		Timestamp:  NormalizeAuditTimestamp(time.Time{}),
	})

	// Execute from trigger node
	if err := e.executeNode(ctx, inst, triggerNode, &ver.Graph); err != nil {
		return inst, err
	}

	return inst, nil
}

// approvalNodeState tracks per-node approval decisions for multi-approver modes.
// Stored in InstanceData under the key "_approval_state_<nodeID>".
type approvalNodeState struct {
	Decisions map[string]string `json:"decisions"` // approverID -> "approve"/"reject"/"escalate"
}

// getApprovalNodeState retrieves the approval state for a node from instance data.
func getApprovalNodeState(inst *WorkflowInstance, nodeID string) *approvalNodeState {
	key := "_approval_state_" + nodeID
	if inst.InstanceData == nil {
		return &approvalNodeState{Decisions: make(map[string]string)}
	}
	raw, ok := inst.InstanceData[key]
	if !ok {
		return &approvalNodeState{Decisions: make(map[string]string)}
	}
	// The value may be stored as a map[string]interface{} after JSON round-trip.
	switch v := raw.(type) {
	case map[string]interface{}:
		state := &approvalNodeState{Decisions: make(map[string]string)}
		if decisions, ok := v["decisions"].(map[string]interface{}); ok {
			for k, val := range decisions {
				if s, ok := val.(string); ok {
					state.Decisions[k] = s
				}
			}
		}
		return state
	default:
		return &approvalNodeState{Decisions: make(map[string]string)}
	}
}

// setApprovalNodeState stores the approval state for a node in instance data.
func setApprovalNodeState(inst *WorkflowInstance, nodeID string, state *approvalNodeState) {
	key := "_approval_state_" + nodeID
	if inst.InstanceData == nil {
		inst.InstanceData = make(map[string]interface{})
	}
	// Store decisions as map[string]interface{} for consistent type assertion on read-back.
	decisions := make(map[string]interface{}, len(state.Decisions))
	for k, v := range state.Decisions {
		decisions[k] = v
	}
	inst.InstanceData[key] = map[string]interface{}{
		"decisions": decisions,
	}
}

// ResumeInstance continues execution after receiving an approval response.
// It handles the different approval modes:
//   - Single: one approver decides, advance immediately
//   - Countersign: all must approve; reject immediately on first reject; advance when all approve
//   - Any N of M: track approval count; advance when N approvals reached; reject when impossible to reach N
//   - Sequential: ordered list; advance to next on approve; reject immediately on reject
func (e *WorkflowExecutor) ResumeInstance(ctx context.Context, instanceID string, nodeID string, response ApprovalResponse) error {
	inst, err := e.instanceStore.Get(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("get instance: %w", err)
	}
	if inst == nil {
		return fmt.Errorf("instance %s not found", instanceID)
	}

	// Verify instance is in a resumable state.
	if inst.Status != InstanceRunning {
		return fmt.Errorf("instance %s is not running (status: %s), cannot process approval response", instanceID, inst.Status)
	}

	// Record the decision in audit trail
	_ = e.auditStore.Append(ctx, &AuditEntry{
		ID:          generateID("audit"),
		InstanceID:  instanceID,
		NodeID:      nodeID,
		EventType:   "approval_decision",
		ActorID:     response.ApproverID,
		Decision:    response.Decision,
		MatchedRule: response.MatchedRule,
		Rationale:   response.Rationale,
		Timestamp:   NormalizeAuditTimestamp(time.Time{}),
	})

	// Get the workflow version graph
	ver, err := e.store.GetVersion(ctx, inst.VersionID)
	if err != nil {
		return fmt.Errorf("get version: %w", err)
	}

	// Find the approval node and parse its config
	approvalNode := findNodeByID(&ver.Graph, nodeID)
	if approvalNode == nil {
		return fmt.Errorf("node %s not found in graph", nodeID)
	}

	var cfg ApprovalNodeConfig
	if err := json.Unmarshal(approvalNode.Config, &cfg); err != nil {
		return fmt.Errorf("parse approval node config: %w", err)
	}

	// Determine whether to advance based on approval mode
	shouldAdvance, shouldReject, err := e.processApprovalResponse(ctx, inst, nodeID, &cfg, response)
	if err != nil {
		return err
	}

	// Persist updated instance data (approval state) to the store.
	if err := e.instanceStore.UpdateInstanceData(ctx, inst.ID, inst.InstanceData); err != nil {
		return fmt.Errorf("persist approval state: %w", err)
	}

	if shouldReject {
		// Mark the node as failed due to rejection
		_ = e.auditStore.Append(ctx, &AuditEntry{
			ID:         generateID("audit"),
			InstanceID: instanceID,
			NodeID:     nodeID,
			EventType:  "node_failed",
			Details:    `{"reason":"approval rejected"}`,
			Timestamp:  NormalizeAuditTimestamp(time.Time{}),
		})
		return e.markInstanceFailed(ctx, inst, "approval rejected at node "+nodeID)
	}

	if !shouldAdvance {
		// Still waiting for more approvals; do not advance yet.
		return nil
	}

	// Record node completion
	_ = e.auditStore.Append(ctx, &AuditEntry{
		ID:         generateID("audit"),
		InstanceID: instanceID,
		NodeID:     nodeID,
		EventType:  "node_completed",
		Timestamp:  NormalizeAuditTimestamp(time.Time{}),
	})

	// Find next nodes and continue execution
	nextNodes := findOutgoingNodes(&ver.Graph, nodeID)
	if len(nextNodes) == 0 {
		return e.markInstanceCompleted(ctx, inst)
	}
	for _, nextNode := range nextNodes {
		if err := e.executeNode(ctx, inst, nextNode, &ver.Graph); err != nil {
			return err
		}
	}

	return nil
}

// processApprovalResponse evaluates the response against the approval mode and
// returns (shouldAdvance, shouldReject, error).
// - shouldAdvance=true means the approval node is satisfied and workflow should continue.
// - shouldReject=true means the approval node is rejected and workflow should fail.
// - Both false means still waiting for more responses.
func (e *WorkflowExecutor) processApprovalResponse(ctx context.Context, inst *WorkflowInstance, nodeID string, cfg *ApprovalNodeConfig, response ApprovalResponse) (bool, bool, error) {
	if !isAllowedApprovalDecision(response.Decision) {
		return false, false, fmt.Errorf("invalid approval decision %q", response.Decision)
	}
	if !isConfiguredApprover(cfg, response.ApproverID) {
		return false, false, fmt.Errorf("approver %q is not assigned to approval node %s", response.ApproverID, nodeID)
	}

	switch cfg.Mode {
	case ModeSingle:
		return e.processSingleMode(response)
	case ModeCountersign:
		return e.processCountersignMode(ctx, inst, nodeID, cfg, response)
	case ModeAnyNofM:
		return e.processAnyNofMMode(ctx, inst, nodeID, cfg, response)
	case ModeSequential:
		return e.processSequentialMode(ctx, inst, nodeID, cfg, response)
	default:
		// Unknown mode; treat as single approver.
		return e.processSingleMode(response)
	}
}

func isAllowedApprovalDecision(decision string) bool {
	switch decision {
	case approvalDecisionApprove, approvalDecisionReject, approvalDecisionEscalate:
		return true
	default:
		return false
	}
}

func isConfiguredApprover(cfg *ApprovalNodeConfig, approverID string) bool {
	if approverID == "" {
		return false
	}
	for _, id := range cfg.ApproverIDs {
		if id == approverID {
			return true
		}
	}
	for _, id := range cfg.ApproverOrder {
		if id == approverID {
			return true
		}
	}
	return cfg.FallbackApprover == approverID
}

// processSingleMode handles single-approver mode: one decision immediately determines outcome.
func (e *WorkflowExecutor) processSingleMode(response ApprovalResponse) (bool, bool, error) {
	if response.Decision == approvalDecisionReject {
		return false, true, nil
	}
	if response.Decision == approvalDecisionEscalate {
		return false, false, nil
	}
	// "approve" advances the workflow.
	return true, false, nil
}

// processCountersignMode handles countersign mode:
// - All assigned approvers must approve for the node to pass.
// - Reject immediately on any single rejection.
// - Advance when all approvers have approved.
func (e *WorkflowExecutor) processCountersignMode(ctx context.Context, inst *WorkflowInstance, nodeID string, cfg *ApprovalNodeConfig, response ApprovalResponse) (bool, bool, error) {
	// Reject immediately on any rejection
	if response.Decision == approvalDecisionReject {
		return false, true, nil
	}
	if response.Decision == approvalDecisionEscalate {
		return false, false, nil
	}

	// Track the approval
	state := getApprovalNodeState(inst, nodeID)
	state.Decisions[response.ApproverID] = response.Decision
	setApprovalNodeState(inst, nodeID, state)

	// Check if all approvers have approved
	allApproved := true
	for _, approverID := range cfg.ApproverIDs {
		decision, responded := state.Decisions[approverID]
		if !responded || decision != approvalDecisionApprove {
			allApproved = false
			break
		}
	}

	if allApproved {
		return true, false, nil
	}

	// Still waiting for more approvals
	return false, false, nil
}

// processAnyNofMMode handles any-N-of-M mode:
//   - Track approval count; advance when N approvals are reached.
//   - Reject when it becomes impossible to reach N approvals
//     (i.e., rejections > M - N, meaning not enough remaining approvers can reach N).
func (e *WorkflowExecutor) processAnyNofMMode(ctx context.Context, inst *WorkflowInstance, nodeID string, cfg *ApprovalNodeConfig, response ApprovalResponse) (bool, bool, error) {
	if response.Decision == approvalDecisionEscalate {
		return false, false, nil
	}

	state := getApprovalNodeState(inst, nodeID)
	state.Decisions[response.ApproverID] = response.Decision
	setApprovalNodeState(inst, nodeID, state)

	// Count approvals and rejections
	approvalCount := 0
	rejectionCount := 0
	for _, decision := range state.Decisions {
		switch decision {
		case approvalDecisionApprove:
			approvalCount++
		case approvalDecisionReject:
			rejectionCount++
		}
	}

	// Determine N (required approvals) and M (total approvers)
	requiredApprovals := cfg.MinApprovals
	if requiredApprovals <= 0 {
		requiredApprovals = 1
	}
	totalApprovers := len(cfg.ApproverIDs)

	// Advance when N approvals reached
	if approvalCount >= requiredApprovals {
		return true, false, nil
	}

	// Reject when it's impossible to reach N:
	// remaining = totalApprovers - len(state.Decisions)
	// maxPossibleApprovals = approvalCount + remaining
	// If maxPossibleApprovals < requiredApprovals, reject.
	remaining := totalApprovers - len(state.Decisions)
	maxPossibleApprovals := approvalCount + remaining
	if maxPossibleApprovals < requiredApprovals {
		return false, true, nil
	}

	// Still waiting for more responses
	return false, false, nil
}

// processSequentialMode handles sequential mode:
// - Approvers are consulted in a defined order.
// - On approve: advance to the next approver in the sequence, or complete if last.
// - On reject: reject immediately (stop the sequence).
func (e *WorkflowExecutor) processSequentialMode(ctx context.Context, inst *WorkflowInstance, nodeID string, cfg *ApprovalNodeConfig, response ApprovalResponse) (bool, bool, error) {
	// Reject immediately on any rejection
	if response.Decision == approvalDecisionReject {
		return false, true, nil
	}
	if response.Decision == approvalDecisionEscalate {
		return false, false, nil
	}

	// Track the approval
	state := getApprovalNodeState(inst, nodeID)
	state.Decisions[response.ApproverID] = response.Decision
	setApprovalNodeState(inst, nodeID, state)

	// Determine the approver order
	order := cfg.ApproverOrder
	if len(order) == 0 {
		order = cfg.ApproverIDs
	}

	// Find the current position in the sequence
	currentIdx := -1
	for i, approverID := range order {
		if approverID == response.ApproverID {
			currentIdx = i
			break
		}
	}
	if currentIdx == -1 {
		return false, false, fmt.Errorf("approver %q is not in sequential approval order", response.ApproverID)
	}
	for i := 0; i < currentIdx; i++ {
		if state.Decisions[order[i]] != approvalDecisionApprove {
			return false, false, fmt.Errorf("sequential approval response from %q arrived before approver %q completed", response.ApproverID, order[i])
		}
	}

	// If this is the last approver in the sequence, advance the workflow
	if currentIdx >= len(order)-1 {
		return true, false, nil
	}

	// Dispatch to the next approver in the sequence
	nextApproverID := order[currentIdx+1]

	// Build the approval request for the next approver
	req := &ApprovalRequest{
		ID:         generateID("areq"),
		InstanceID: inst.ID,
		NodeID:     nodeID,
		CreatedAt:  time.Now().UTC(),
	}
	if title, ok := inst.InstanceData["title"].(string); ok {
		req.Title = title
	}
	if summary, ok := inst.InstanceData["summary"].(string); ok {
		req.Summary = summary
	}
	if details, ok := inst.InstanceData["details"].(map[string]interface{}); ok {
		req.Details = details
	}

	if err := e.dispatcher.Dispatch(ctx, req, nextApproverID); err != nil {
		return false, false, fmt.Errorf("dispatch to next sequential approver %s: %w", nextApproverID, err)
	}

	// Still waiting; next approver needs to respond.
	return false, false, nil
}

// markInstanceFailed marks the workflow instance as failed and records the event.
func (e *WorkflowExecutor) markInstanceFailed(ctx context.Context, inst *WorkflowInstance, reason string) error {
	if err := e.instanceStore.UpdateStatus(ctx, inst.ID, InstanceFailed); err != nil {
		return fmt.Errorf("mark instance failed: %w", err)
	}
	inst.Status = InstanceFailed

	_ = e.auditStore.Append(ctx, &AuditEntry{
		ID:         generateID("audit"),
		InstanceID: inst.ID,
		EventType:  "instance_failed",
		Details:    fmt.Sprintf(`{"reason":"%s"}`, reason),
		Timestamp:  NormalizeAuditTimestamp(time.Time{}),
	})
	return nil
}

// HandleTimeout processes timeout events for pending approval nodes.
// When a timeout is detected:
// 1. If a fallback approver is configured, dispatch to fallback.
// 2. If no fallback is configured, mark the node as "blocked" and notify the initiator.
// 3. If the fallback approver is also unavailable (cascading failure), mark as "blocked".
// All events are recorded in the audit trail.
func (e *WorkflowExecutor) HandleTimeout(ctx context.Context, instanceID, nodeID string) error {
	inst, err := e.instanceStore.Get(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("get instance: %w", err)
	}
	if inst == nil {
		return fmt.Errorf("instance %s not found", instanceID)
	}

	// Get the workflow version to access the node config.
	ver, err := e.store.GetVersion(ctx, inst.VersionID)
	if err != nil {
		return fmt.Errorf("get version: %w", err)
	}

	node := findNodeByID(&ver.Graph, nodeID)
	if node == nil {
		return fmt.Errorf("node %s not found in graph", nodeID)
	}

	// Record the timeout event in audit trail.
	_ = e.auditStore.Append(ctx, &AuditEntry{
		ID:         generateID("audit"),
		InstanceID: instanceID,
		NodeID:     nodeID,
		EventType:  "node_timeout",
		Details:    `{"reason":"timeout_exceeded"}`,
		Timestamp:  NormalizeAuditTimestamp(time.Time{}),
	})

	// Parse the approval node config to check for fallback approver.
	var cfg ApprovalNodeConfig
	if err := json.Unmarshal(node.Config, &cfg); err != nil {
		return fmt.Errorf("parse approval node config: %w", err)
	}

	return e.handleFallbackRouting(ctx, inst, node, &cfg, "timeout")
}

// HandleUnavailable processes unavailability events for approval nodes.
// This is called when a VE approver is detected as unavailable (capability disabled).
// Routes to fallback within 30 seconds of detecting unavailability.
func (e *WorkflowExecutor) HandleUnavailable(ctx context.Context, instanceID, nodeID, approverID string) error {
	inst, err := e.instanceStore.Get(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("get instance: %w", err)
	}
	if inst == nil {
		return fmt.Errorf("instance %s not found", instanceID)
	}

	ver, err := e.store.GetVersion(ctx, inst.VersionID)
	if err != nil {
		return fmt.Errorf("get version: %w", err)
	}

	node := findNodeByID(&ver.Graph, nodeID)
	if node == nil {
		return fmt.Errorf("node %s not found in graph", nodeID)
	}

	// Record unavailability in audit trail.
	_ = e.auditStore.Append(ctx, &AuditEntry{
		ID:         generateID("audit"),
		InstanceID: instanceID,
		NodeID:     nodeID,
		EventType:  "approver_unavailable",
		ActorID:    approverID,
		Details:    `{"reason":"capability_disabled"}`,
		Timestamp:  NormalizeAuditTimestamp(time.Time{}),
	})

	var cfg ApprovalNodeConfig
	if err := json.Unmarshal(node.Config, &cfg); err != nil {
		return fmt.Errorf("parse approval node config: %w", err)
	}

	return e.handleFallbackRouting(ctx, inst, node, &cfg, "unavailable")
}

// HandleQueueFull processes queue-full events for approval nodes.
// This is called when a VE approver's pending queue has reached the maximum limit.
func (e *WorkflowExecutor) HandleQueueFull(ctx context.Context, instanceID, nodeID, approverID string) error {
	inst, err := e.instanceStore.Get(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("get instance: %w", err)
	}
	if inst == nil {
		return fmt.Errorf("instance %s not found", instanceID)
	}

	ver, err := e.store.GetVersion(ctx, inst.VersionID)
	if err != nil {
		return fmt.Errorf("get version: %w", err)
	}

	node := findNodeByID(&ver.Graph, nodeID)
	if node == nil {
		return fmt.Errorf("node %s not found in graph", nodeID)
	}

	// Record queue full event in audit trail.
	_ = e.auditStore.Append(ctx, &AuditEntry{
		ID:         generateID("audit"),
		InstanceID: instanceID,
		NodeID:     nodeID,
		EventType:  "approver_queue_full",
		ActorID:    approverID,
		Details:    `{"reason":"queue_full"}`,
		Timestamp:  NormalizeAuditTimestamp(time.Time{}),
	})

	var cfg ApprovalNodeConfig
	if err := json.Unmarshal(node.Config, &cfg); err != nil {
		return fmt.Errorf("parse approval node config: %w", err)
	}

	return e.handleFallbackRouting(ctx, inst, node, &cfg, "queue_full")
}

// handleFallbackRouting implements the core fallback logic shared by timeout,
// unavailability, and queue-full handlers.
// reason is one of: "timeout", "unavailable", "queue_full".
func (e *WorkflowExecutor) handleFallbackRouting(ctx context.Context, inst *WorkflowInstance, node *WorkflowNode, cfg *ApprovalNodeConfig, reason string) error {
	if cfg.FallbackApprover == "" {
		// No fallback configured; mark node as blocked and notify initiator.
		return e.markNodeBlocked(ctx, inst, node, reason, "no fallback approver configured")
	}

	// Build the approval request for the fallback approver (same payload as primary).
	req := e.buildApprovalRequestFromInstance(inst, node)

	// Attempt to dispatch to fallback approver.
	err := e.dispatcher.DispatchFallback(ctx, req, cfg.FallbackApprover, reason)
	if err != nil {
		// Cascading failure; fallback approver is also unavailable.
		_ = e.auditStore.Append(ctx, &AuditEntry{
			ID:         generateID("audit"),
			InstanceID: inst.ID,
			NodeID:     node.ID,
			EventType:  "fallback_failed",
			ActorID:    cfg.FallbackApprover,
			Details:    fmt.Sprintf(`{"reason":"cascading_failure","fallback_error":"%s","original_reason":"%s"}`, escapeJSON(err.Error()), reason),
			Timestamp:  NormalizeAuditTimestamp(time.Time{}),
		})

		// Mark node as blocked due to cascading failure.
		return e.markNodeBlocked(ctx, inst, node, reason, "fallback approver also unavailable: "+err.Error())
	}

	// Fallback dispatch succeeded; record the event.
	_ = e.auditStore.Append(ctx, &AuditEntry{
		ID:         generateID("audit"),
		InstanceID: inst.ID,
		NodeID:     node.ID,
		EventType:  "fallback_routed",
		ActorID:    cfg.FallbackApprover,
		Details:    fmt.Sprintf(`{"reason":"%s","original_approvers":%s,"fallback_approver":"%s"}`, reason, marshalStringSlice(cfg.ApproverIDs), cfg.FallbackApprover),
		Timestamp:  NormalizeAuditTimestamp(time.Time{}),
	})

	return nil
}

// markNodeBlocked marks an approval node as "blocked", updates the instance status,
// notifies the workflow initiator, and records the event in the audit trail.
func (e *WorkflowExecutor) markNodeBlocked(ctx context.Context, inst *WorkflowInstance, node *WorkflowNode, reason, details string) error {
	// Update node execution status to blocked.
	// We look up the node execution by instance+node ID.
	pendingExecs, _ := e.instanceStore.GetPendingApprovals(ctx, "")
	for _, exec := range pendingExecs {
		if exec.InstanceID == inst.ID && exec.NodeID == node.ID {
			_ = e.instanceStore.UpdateNodeExecution(ctx, exec.ID, NodeBlocked, nil, reason+": "+details)
			break
		}
	}

	// Update instance status to blocked.
	_ = e.instanceStore.UpdateStatus(ctx, inst.ID, InstanceBlocked)
	inst.Status = InstanceBlocked

	// Record blocked event in audit trail.
	_ = e.auditStore.Append(ctx, &AuditEntry{
		ID:         generateID("audit"),
		InstanceID: inst.ID,
		NodeID:     node.ID,
		EventType:  "node_blocked",
		Details:    fmt.Sprintf(`{"reason":"%s","details":"%s"}`, reason, escapeJSON(details)),
		Timestamp:  NormalizeAuditTimestamp(time.Time{}),
	})

	// Notify the workflow initiator (within 60 seconds per requirement 11.4).
	if e.notifier != nil {
		notifyReason := fmt.Sprintf("Approval node '%s' is blocked: %s", node.Label, reason)
		notifyDetails := fmt.Sprintf("Node ID: %s, Reason: %s, Details: %s", node.ID, reason, details)
		_ = e.notifier.NotifyInitiator(ctx, inst.ID, notifyReason, notifyDetails)
	}

	return nil
}

// buildApprovalRequestFromInstance constructs an ApprovalRequest from instance data.
// The fallback approver receives the same payload and hint_rules as the primary approver.
func (e *WorkflowExecutor) buildApprovalRequestFromInstance(inst *WorkflowInstance, node *WorkflowNode) *ApprovalRequest {
	req := &ApprovalRequest{
		ID:         generateID("areq"),
		InstanceID: inst.ID,
		NodeID:     node.ID,
		CreatedAt:  time.Now().UTC(),
	}
	if title, ok := inst.InstanceData["title"].(string); ok {
		req.Title = title
	}
	if summary, ok := inst.InstanceData["summary"].(string); ok {
		req.Summary = summary
	}
	if details, ok := inst.InstanceData["details"].(map[string]interface{}); ok {
		req.Details = details
	}
	if hints, ok := inst.InstanceData["hint_rules"].([]interface{}); ok {
		for _, h := range hints {
			if s, ok := h.(string); ok {
				req.HintRules = append(req.HintRules, s)
			}
		}
	}
	if requesterID, ok := inst.InstanceData["requester_id"].(string); ok {
		req.RequesterID = requesterID
	}
	return req
}

// escapeJSON escapes a string for safe inclusion in a JSON value.
func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

// marshalStringSlice marshals a string slice to a JSON array string.
func marshalStringSlice(ss []string) string {
	data, err := json.Marshal(ss)
	if err != nil {
		return "[]"
	}
	return string(data)
}

// executeNode dispatches execution to the appropriate handler based on node type.
func (e *WorkflowExecutor) executeNode(ctx context.Context, inst *WorkflowInstance, node *WorkflowNode, graph *WorkflowGraph) error {
	// Update current node
	_ = e.instanceStore.UpdateCurrentNode(ctx, inst.ID, node.ID)
	inst.CurrentNodeID = node.ID

	// Create node execution record
	now := time.Now().UTC()
	nodeExec := &NodeExecution{
		ID:         generateID("nexec"),
		InstanceID: inst.ID,
		NodeID:     node.ID,
		NodeType:   node.Type,
		Status:     NodeRunning,
		StartedAt:  now,
	}
	_ = e.instanceStore.CreateNodeExecution(ctx, nodeExec)

	var execErr error
	switch node.Type {
	case NodeTrigger:
		execErr = e.executeTriggerNode(ctx, inst, node, graph)
	case NodeConditionBranch:
		execErr = e.executeConditionBranchNode(ctx, inst, node, graph)
	case NodeApproval:
		execErr = e.executeApprovalNode(ctx, inst, node, graph)
	case NodeAction:
		execErr = e.executeActionNode(ctx, inst, node, graph)
	case NodeNotification:
		execErr = e.executeNotificationNode(ctx, inst, node, graph)
	case NodeForm:
		execErr = e.executeFormNode(ctx, inst, node, graph)
	case NodeSubProcess:
		execErr = e.executeSubProcessNode(ctx, inst, node, graph)
	case NodeTypeTerminal:
		execErr = e.executeTerminalNode(ctx, inst, node, graph)
	default:
		execErr = fmt.Errorf("unknown node type: %s", node.Type)
	}

	if execErr != nil {
		_ = e.instanceStore.UpdateNodeExecution(ctx, nodeExec.ID, NodeFailed, nil, execErr.Error())

		// Record node_failed audit event
		_ = e.auditStore.Append(ctx, &AuditEntry{
			ID:         generateID("audit"),
			InstanceID: inst.ID,
			NodeID:     node.ID,
			EventType:  "node_failed",
			Details:    fmt.Sprintf("node_type=%s, reason=%s", node.Type, execErr.Error()),
			Timestamp:  NormalizeAuditTimestamp(time.Time{}),
		})
		return execErr
	}

	// Mark node as completed (for non-blocking nodes)
	if node.Type != NodeApproval {
		_ = e.instanceStore.UpdateNodeExecution(ctx, nodeExec.ID, NodeCompleted, nil, "")

		_ = e.auditStore.Append(ctx, &AuditEntry{
			ID:         generateID("audit"),
			InstanceID: inst.ID,
			NodeID:     node.ID,
			EventType:  "node_completed",
			Details:    fmt.Sprintf("node_type=%s", node.Type),
			Timestamp:  NormalizeAuditTimestamp(time.Time{}),
		})
	}

	return nil
}

// executeTriggerNode handles the trigger node and advances to next nodes.
func (e *WorkflowExecutor) executeTriggerNode(ctx context.Context, inst *WorkflowInstance, node *WorkflowNode, graph *WorkflowGraph) error {
	nextNodes := findOutgoingNodes(graph, node.ID)
	if len(nextNodes) == 0 {
		return e.markInstanceCompleted(ctx, inst)
	}
	for _, next := range nextNodes {
		if err := e.executeNode(ctx, inst, next, graph); err != nil {
			return err
		}
	}
	return nil
}

// executeConditionBranchNode evaluates branch conditions and routes to the matching branch.
// Conditions are evaluated in priority order (ascending). The first matching branch is selected.
// If no condition matches and a default branch is configured, routes to the default.
// If no condition matches and no default branch exists, the node fails.
func (e *WorkflowExecutor) executeConditionBranchNode(ctx context.Context, inst *WorkflowInstance, node *WorkflowNode, graph *WorkflowGraph) error {
	var config ConditionBranchConfig
	if err := json.Unmarshal(node.Config, &config); err != nil {
		return fmt.Errorf("parse condition branch config: %w", err)
	}

	// Sort branches by priority (ascending; lower number = higher priority)
	branches := make([]BranchCondition, len(config.Branches))
	copy(branches, config.Branches)
	sort.Slice(branches, func(i, j int) bool {
		return branches[i].Priority < branches[j].Priority
	})

	// Evaluate each branch condition against instance data
	for _, branch := range branches {
		if evaluateConditionExpr(branch.Expression, inst.InstanceData) {
			// Route to the first matching branch
			targetNode := findNodeByID(graph, branch.TargetNodeID)
			if targetNode == nil {
				return fmt.Errorf("branch target node %s not found in graph", branch.TargetNodeID)
			}
			return e.executeNode(ctx, inst, targetNode, graph)
		}
	}

	// No branch matched; check for default branch.
	if config.DefaultBranch != "" {
		targetNode := findNodeByID(graph, config.DefaultBranch)
		if targetNode == nil {
			return fmt.Errorf("default branch target node %s not found in graph", config.DefaultBranch)
		}
		return e.executeNode(ctx, inst, targetNode, graph)
	}

	// No match and no default; mark node as failed.
	failReason := "no condition branch matched and no default branch configured"
	_ = e.auditStore.Append(ctx, &AuditEntry{
		ID:         generateID("audit"),
		InstanceID: inst.ID,
		NodeID:     node.ID,
		EventType:  "node_failed",
		Details:    fmt.Sprintf(`{"reason":"%s"}`, failReason),
		Timestamp:  NormalizeAuditTimestamp(time.Time{}),
	})
	return fmt.Errorf("%s", failReason)
}

// evaluateConditionExpr evaluates a single condition expression against instance data.
// Delegates to the shared condeval package.
func evaluateConditionExpr(expr ConditionExpr, data map[string]interface{}) bool {
	return condeval.EvaluateCondition(expr.Field, expr.Operator, expr.Value, data)
}

// resolveFieldPath extracts a value from nested map data using dot-notation path.
// Thin wrapper over condeval.ResolveField for backward compatibility.
func resolveFieldPath(data map[string]interface{}, fieldPath string) (interface{}, bool) {
	return condeval.ResolveField(data, fieldPath)
}

// isEmptyValue checks if a value is considered empty.
// Thin wrapper over condeval.IsEmpty for backward compatibility.
func isEmptyValue(val interface{}) bool {
	return condeval.IsEmpty(val)
}

// exprEquals checks equality between field value and condition value.
func exprEquals(fieldVal, condVal interface{}) bool {
	return condeval.Equals(fieldVal, condVal)
}

// exprCompareNumeric compares two values numerically.
func exprCompareNumeric(fieldVal, condVal interface{}) int {
	return condeval.CompareNumeric(fieldVal, condVal)
}

// toFloat converts a value to float64 if possible.
func toFloat(val interface{}) (float64, bool) {
	return condeval.ToFloat64(val)
}

// exprContains checks if the field value contains the condition value.
func exprContains(fieldVal, condVal interface{}) bool {
	return condeval.Contains(fieldVal, condVal)
}

// exprInList checks if the field value is in the condition value list.
func exprInList(fieldVal, condVal interface{}) bool {
	return condeval.InList(fieldVal, condVal)
}

// executeApprovalNode dispatches an approval request to the assigned VE approver(s).
// Approval nodes are blocking: they dispatch the request and wait for
// response via ResumeInstance. The node stays in "running" status until
// a response is received.
func (e *WorkflowExecutor) executeApprovalNode(ctx context.Context, inst *WorkflowInstance, node *WorkflowNode, graph *WorkflowGraph) error {
	var cfg ApprovalNodeConfig
	if err := json.Unmarshal(node.Config, &cfg); err != nil {
		return fmt.Errorf("parse approval node config: %w", err)
	}

	if len(cfg.ApproverIDs) == 0 {
		return fmt.Errorf("approval node %s has no approvers configured", node.ID)
	}

	// Build the approval request from instance data.
	req := &ApprovalRequest{
		ID:         generateID("areq"),
		InstanceID: inst.ID,
		NodeID:     node.ID,
		CreatedAt:  time.Now().UTC(),
	}
	if title, ok := inst.InstanceData["title"].(string); ok {
		req.Title = title
	}
	if summary, ok := inst.InstanceData["summary"].(string); ok {
		req.Summary = summary
	}
	if details, ok := inst.InstanceData["details"].(map[string]interface{}); ok {
		req.Details = details
	}

	// Dispatch based on approval mode.
	switch cfg.Mode {
	case ModeSingle:
		if err := e.dispatcher.Dispatch(ctx, req, cfg.ApproverIDs[0]); err != nil {
			return fmt.Errorf("dispatch to approver: %w", err)
		}
	case ModeCountersign:
		// Dispatch to all approvers; all must approve.
		var dispatched []string
		for _, approverID := range cfg.ApproverIDs {
			if err := e.dispatcher.Dispatch(ctx, req, approverID); err != nil {
				// Partial dispatch failure: some approvers already received the request.
				_ = e.auditStore.Append(ctx, &AuditEntry{
					ID:         generateID("audit"),
					InstanceID: inst.ID,
					NodeID:     node.ID,
					EventType:  "dispatch_partial_failure",
					Details:    fmt.Sprintf(`{"failed_approver":"%s","dispatched":%s,"error":"%s"}`, approverID, marshalStringSlice(dispatched), escapeJSON(err.Error())),
					Timestamp:  NormalizeAuditTimestamp(time.Time{}),
				})
				// Mark node as blocked so timeout handler can retry or route to fallback.
				pendingExecs, _ := e.instanceStore.GetPendingApprovals(ctx, "")
				for _, exec := range pendingExecs {
					if exec.InstanceID == inst.ID && exec.NodeID == node.ID {
						_ = e.instanceStore.UpdateNodeExecution(ctx, exec.ID, NodeBlocked, nil, fmt.Sprintf("partial dispatch failure at approver %s", approverID))
						break
					}
				}
				_ = e.instanceStore.UpdateStatus(ctx, inst.ID, InstanceBlocked)
				inst.Status = InstanceBlocked
				return nil // Don't fail the node; let timeout handler deal with it.
			}
			dispatched = append(dispatched, approverID)
		}
	case ModeAnyNofM:
		// Dispatch to all approvers; N of M must approve.
		var dispatched []string
		for _, approverID := range cfg.ApproverIDs {
			if err := e.dispatcher.Dispatch(ctx, req, approverID); err != nil {
				// Partial dispatch failure.
				_ = e.auditStore.Append(ctx, &AuditEntry{
					ID:         generateID("audit"),
					InstanceID: inst.ID,
					NodeID:     node.ID,
					EventType:  "dispatch_partial_failure",
					Details:    fmt.Sprintf(`{"failed_approver":"%s","dispatched":%s,"error":"%s"}`, approverID, marshalStringSlice(dispatched), escapeJSON(err.Error())),
					Timestamp:  NormalizeAuditTimestamp(time.Time{}),
				})
				pendingExecs, _ := e.instanceStore.GetPendingApprovals(ctx, "")
				for _, exec := range pendingExecs {
					if exec.InstanceID == inst.ID && exec.NodeID == node.ID {
						_ = e.instanceStore.UpdateNodeExecution(ctx, exec.ID, NodeBlocked, nil, fmt.Sprintf("partial dispatch failure at approver %s", approverID))
						break
					}
				}
				_ = e.instanceStore.UpdateStatus(ctx, inst.ID, InstanceBlocked)
				inst.Status = InstanceBlocked
				return nil
			}
			dispatched = append(dispatched, approverID)
		}
	case ModeSequential:
		// Dispatch to the first approver in the sequence.
		order := cfg.ApproverOrder
		if len(order) == 0 {
			order = cfg.ApproverIDs
		}
		if err := e.dispatcher.Dispatch(ctx, req, order[0]); err != nil {
			return fmt.Errorf("dispatch to first sequential approver: %w", err)
		}
	default:
		return fmt.Errorf("unknown approval mode: %s", cfg.Mode)
	}

	// Execution pauses here and will be resumed via ResumeInstance when response arrives.
	return nil
}

// executeActionNode handles action nodes (placeholder for future implementation).
func (e *WorkflowExecutor) executeActionNode(ctx context.Context, inst *WorkflowInstance, node *WorkflowNode, graph *WorkflowGraph) error {
	nextNodes := findOutgoingNodes(graph, node.ID)
	if len(nextNodes) == 0 {
		return e.markInstanceCompleted(ctx, inst)
	}
	for _, next := range nextNodes {
		if err := e.executeNode(ctx, inst, next, graph); err != nil {
			return err
		}
	}
	return nil
}

// executeNotificationNode handles notification nodes (placeholder for future implementation).
func (e *WorkflowExecutor) executeNotificationNode(ctx context.Context, inst *WorkflowInstance, node *WorkflowNode, graph *WorkflowGraph) error {
	nextNodes := findOutgoingNodes(graph, node.ID)
	if len(nextNodes) == 0 {
		return e.markInstanceCompleted(ctx, inst)
	}
	for _, next := range nextNodes {
		if err := e.executeNode(ctx, inst, next, graph); err != nil {
			return err
		}
	}
	return nil
}

// executeFormNode handles form nodes (placeholder for future implementation).
func (e *WorkflowExecutor) executeFormNode(ctx context.Context, inst *WorkflowInstance, node *WorkflowNode, graph *WorkflowGraph) error {
	nextNodes := findOutgoingNodes(graph, node.ID)
	if len(nextNodes) == 0 {
		return e.markInstanceCompleted(ctx, inst)
	}
	for _, next := range nextNodes {
		if err := e.executeNode(ctx, inst, next, graph); err != nil {
			return err
		}
	}
	return nil
}

// executeSubProcessNode handles sub-process nodes (placeholder for future implementation).
func (e *WorkflowExecutor) executeSubProcessNode(ctx context.Context, inst *WorkflowInstance, node *WorkflowNode, graph *WorkflowGraph) error {
	nextNodes := findOutgoingNodes(graph, node.ID)
	if len(nextNodes) == 0 {
		return e.markInstanceCompleted(ctx, inst)
	}
	for _, next := range nextNodes {
		if err := e.executeNode(ctx, inst, next, graph); err != nil {
			return err
		}
	}
	return nil
}

// executeTerminalNode handles terminal/end nodes.
// When a workflow reaches a terminal node:
// 1. Update instance status to completed
// 2. Record "instance_completed" audit event
// 3. Parse terminal node config for executor/notifier assignments
// 4. Build WorkflowNotification for each executor and notifier
// 5. Dispatch all notifications via NotificationDispatcher.DispatchBatch
// 6. Create confirmation records via ConfirmationTracker.StartTracking
//
// DATA CONSISTENCY NOTE: Step 1 marks the instance as completed BEFORE steps 5-6
// dispatch notifications and create confirmations. If the process crashes between
// step 1 and step 6, the instance will be "completed" but have no confirmation records.
// The RunReminderLoop's FindOverdue query only checks the confirmations table, so these
// orphaned instances will never be found. Use ConfirmationTracker.ReconcileOrphanedInstances
// (called on startup or periodically) to detect and repair this condition.
func (e *WorkflowExecutor) executeTerminalNode(ctx context.Context, inst *WorkflowInstance, node *WorkflowNode, graph *WorkflowGraph) error {
	// 1. Mark instance as completed
	if err := e.instanceStore.UpdateStatus(ctx, inst.ID, InstanceCompleted); err != nil {
		return fmt.Errorf("mark instance completed at terminal node: %w", err)
	}
	inst.Status = InstanceCompleted

	// 2. Record instance_completed audit event
	_ = e.auditStore.Append(ctx, &AuditEntry{
		ID:         generateID("audit"),
		InstanceID: inst.ID,
		NodeID:     node.ID,
		EventType:  "instance_completed",
		Details:    fmt.Sprintf(`{"terminal_node_id":"%s"}`, node.ID),
		Timestamp:  NormalizeAuditTimestamp(time.Time{}),
	})

	// 3. Parse terminal node config
	var termConfig TerminalNodeConfig
	if node.Config != nil {
		if err := json.Unmarshal(node.Config, &termConfig); err != nil {
			// Config parse failure is non-fatal; instance is already completed.
			_ = e.auditStore.Append(ctx, &AuditEntry{
				ID:         generateID("audit"),
				InstanceID: inst.ID,
				NodeID:     node.ID,
				EventType:  "terminal_config_parse_error",
				Details:    fmt.Sprintf(`{"error":"%s"}`, escapeJSON(err.Error())),
				Timestamp:  NormalizeAuditTimestamp(time.Time{}),
			})
			return nil
		}
	}

	// Apply defaults for zero-valued fields
	ApplyTerminalNodeDefaults(&termConfig)

	// 4. Build notifications for executors and notifiers
	var notifs []*WorkflowNotification
	now := time.Now().UTC().Truncate(time.Millisecond)

	// Derive workflow name and result from instance data
	workflowName := ""
	if name, ok := inst.InstanceData["workflow_name"].(string); ok {
		workflowName = name
	}
	result := "completed"
	if r, ok := inst.InstanceData["result"].(string); ok && r != "" {
		result = r
	}
	formDataSummary := buildFormDataSummary(inst.InstanceData)
	instanceURL := fmt.Sprintf("/workflows/instances/%s", inst.ID)
	initiatorID := ""
	if id, ok := inst.InstanceData["initiator_id"].(string); ok {
		initiatorID = id
	}
	initiatorName := ""
	if name, ok := inst.InstanceData["initiator_name"].(string); ok {
		initiatorName = name
	}

	for _, exec := range termConfig.ResultExecutors {
		notifs = append(notifs, &WorkflowNotification{
			ID:              GenerateNotificationID(),
			InstanceID:      inst.ID,
			Type:            NotifTypeResultExecutor,
			RecipientID:     exec.UserID,
			WorkflowName:    workflowName,
			Result:          result,
			FormDataSummary: formDataSummary,
			InitiatorID:     initiatorID,
			InitiatorName:   initiatorName,
			InstanceURL:     instanceURL,
			CreatedAt:       now,
		})
	}

	for _, notifier := range termConfig.Notifiers {
		notifs = append(notifs, &WorkflowNotification{
			ID:              GenerateNotificationID(),
			InstanceID:      inst.ID,
			Type:            NotifTypeNotifier,
			RecipientID:     notifier.UserID,
			WorkflowName:    workflowName,
			Result:          result,
			FormDataSummary: formDataSummary,
			InitiatorID:     initiatorID,
			InitiatorName:   initiatorName,
			InstanceURL:     instanceURL,
			CreatedAt:       now,
		})
	}

	// 5. Dispatch notifications
	if e.notifDispatcher != nil && len(notifs) > 0 {
		if err := e.notifDispatcher.DispatchBatch(ctx, notifs); err != nil {
			// Notification dispatch failure is non-fatal; instance is already completed.
			_ = e.auditStore.Append(ctx, &AuditEntry{
				ID:         generateID("audit"),
				InstanceID: inst.ID,
				NodeID:     node.ID,
				EventType:  "notification_dispatch_error",
				Details:    fmt.Sprintf(`{"error":"%s","recipient_count":%d}`, escapeJSON(err.Error()), len(notifs)),
				Timestamp:  NormalizeAuditTimestamp(time.Time{}),
			})
		}
	}

	// 6. Start confirmation tracking
	if e.confirmTracker != nil {
		if err := e.confirmTracker.StartTracking(ctx, inst, &termConfig); err != nil {
			// Confirmation tracking failure is non-fatal; instance is already completed.
			_ = e.auditStore.Append(ctx, &AuditEntry{
				ID:         generateID("audit"),
				InstanceID: inst.ID,
				NodeID:     node.ID,
				EventType:  "confirmation_tracking_error",
				Details:    fmt.Sprintf(`{"error":"%s"}`, escapeJSON(err.Error())),
				Timestamp:  NormalizeAuditTimestamp(time.Time{}),
			})
		}
	}

	return nil
}

// buildFormDataSummary creates a brief text summary of form data for notifications.
func buildFormDataSummary(instanceData map[string]interface{}) string {
	formData, ok := instanceData["form_data"]
	if !ok {
		return ""
	}
	formMap, ok := formData.(map[string]interface{})
	if !ok {
		// Try to marshal the whole thing as a fallback
		data, err := json.Marshal(formData)
		if err != nil {
			return ""
		}
		return string(data)
	}

	var parts []string
	for key, val := range formMap {
		parts = append(parts, fmt.Sprintf("%s: %v", key, val))
	}
	summary := strings.Join(parts, "; ")
	// Truncate to reasonable length for notification display
	if len(summary) > 500 {
		summary = summary[:497] + "..."
	}
	return summary
}

// markInstanceCompleted marks the workflow instance as completed and records the event.
func (e *WorkflowExecutor) markInstanceCompleted(ctx context.Context, inst *WorkflowInstance) error {
	if err := e.instanceStore.UpdateStatus(ctx, inst.ID, InstanceCompleted); err != nil {
		return fmt.Errorf("mark instance completed: %w", err)
	}
	inst.Status = InstanceCompleted

	_ = e.auditStore.Append(ctx, &AuditEntry{
		ID:         generateID("audit"),
		InstanceID: inst.ID,
		EventType:  "instance_completed",
		Timestamp:  NormalizeAuditTimestamp(time.Time{}),
	})
	return nil
}

// --- Graph helper functions ---

// findSingleTriggerNode returns the trigger node, or an error if there are zero or multiple.
func findSingleTriggerNode(graph WorkflowGraph) (*WorkflowNode, error) {
	var triggers []*WorkflowNode
	for i := range graph.Nodes {
		if graph.Nodes[i].Type == NodeTrigger {
			triggers = append(triggers, &graph.Nodes[i])
		}
	}
	switch len(triggers) {
	case 0:
		return nil, ErrNoTriggerNode
	case 1:
		return triggers[0], nil
	default:
		return nil, ErrMultipleTriggers
	}
}

// findNodeByID returns the node with the given ID, or nil if not found.
func findNodeByID(graph *WorkflowGraph, nodeID string) *WorkflowNode {
	for i := range graph.Nodes {
		if graph.Nodes[i].ID == nodeID {
			return &graph.Nodes[i]
		}
	}
	return nil
}

// findOutgoingNodes returns all nodes that are targets of edges from the given source node.
func findOutgoingNodes(graph *WorkflowGraph, sourceNodeID string) []*WorkflowNode {
	var targets []*WorkflowNode
	for _, edge := range graph.Edges {
		if edge.SourceID == sourceNodeID {
			if node := findNodeByID(graph, edge.TargetID); node != nil {
				targets = append(targets, node)
			}
		}
	}
	return targets
}

// generateID creates a globally unique ID with the given prefix using UUID v4.
func generateID(prefix string) string {
	return prefix + "_" + uuid.New().String()
}
