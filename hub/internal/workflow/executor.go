package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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

// pendingDispatch is a deferred side-effect computed during decision
// evaluation and executed exactly once AFTER the approval state is durably
// persisted, so an optimistic-lock retry cannot deliver a duplicate request.
type pendingDispatch struct {
	req        *ApprovalRequest
	approverID string
}

// WorkflowExecutor manages the lifecycle of workflow instances.
type WorkflowExecutor struct {
	store            WorkflowStore
	instanceStore    InstanceStore
	auditStore       AuditStore
	dispatcher       ApprovalDispatcher
	approverResolver ApprovalApproverResolver
	notifier         WorkflowNotifier
	notifDispatcher  *NotificationDispatcher
	confirmTracker   *ConfirmationTracker
	// escalationMgr retries fallback human delivery when DispatchFallback fails.
	// Optional; without it cascading fallback failure blocks immediately.
	escalationMgr *EscalationManager
	// resumeLocks serializes the approval read-modify-write-persist cycle in
	// ResumeInstance per instance, so concurrent decisions on the same node
	// cannot lose a vote (Requirement 2.6). See instance_locks.go.
	resumeLocks *instanceLocks
}

// ApprovalDispatcher sends approval requests to VE approvers via A2A.
type ApprovalDispatcher interface {
	Dispatch(ctx context.Context, req *ApprovalRequest, approverID string) error
	DispatchFallback(ctx context.Context, req *ApprovalRequest, fallbackID string, reason string) error
}

// ApprovalApproverResolver expands stable approver references, such as Hub
// approval-role IDs, into concrete runtime approver identities.
type ApprovalApproverResolver interface {
	ResolveApproverIDs(ctx context.Context, approverIDs []string) ([]string, error)
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
		resumeLocks:   newInstanceLocks(),
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

// WithApprovalApproverResolver sets the resolver used before dispatching and
// validating approval decisions.
func WithApprovalApproverResolver(resolver ApprovalApproverResolver) ExecutorOption {
	return func(e *WorkflowExecutor) {
		e.approverResolver = resolver
	}
}

// WithEscalationManager wires the pending-escalation retry queue used when
// fallback delivery fails. Also registers hooks that mark the node blocked after
// max retries and clear per-approver pending markers on successful redelivery.
func WithEscalationManager(mgr *EscalationManager) ExecutorOption {
	return func(e *WorkflowExecutor) {
		e.escalationMgr = mgr
		if mgr != nil {
			mgr.SetFailedHook(e.onEscalationFailed)
			mgr.SetDeliveredHook(e.onEscalationDelivered)
		}
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

	e.surfaceWriteError(ctx, inst.ID, triggerNode.ID, "audit_instance_created",
		e.auditStore.Append(ctx, &AuditEntry{
			ID:         generateID("audit"),
			InstanceID: inst.ID,
			EventType:  "instance_created",
			Details:    fmt.Sprintf(`{"workflow_id":"%s","version_id":"%s"}`, workflowID, ver.ID),
			Timestamp:  NormalizeAuditTimestamp(time.Time{}),
		}))

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
	// Serialize the approval read-modify-write-persist cycle per instance so
	// concurrent decisions on the same node (countersign / any-N-of-M) cannot
	// clobber each other's persisted vote (Requirement 2.6). Decisions on
	// different instances never contend. This changes only HOW approvalNodeState
	// is persisted; the per-mode decision logic below is unchanged.
	if e.resumeLocks != nil {
		release := e.resumeLocks.acquire(instanceID)
		defer release()
	}

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
	if inst != nil {
		ctx = WithApprovalResolveContext(ctx, ApprovalResolveContextFromInstanceData(inst.InstanceData))
	}
	if err := e.resolveApprovalNodeConfig(ctx, &cfg); err != nil {
		return err
	}

	// Determine whether to advance based on approval mode, and persist the
	// resulting approval state under an optimistic-locking guard so that
	// near-simultaneous decisions on the same node (countersign / any-N-of-M)
	// cannot lose a vote across processes (Requirement 2.6). The per-mode
	// decision logic in process*Mode is unchanged; only HOW approvalNodeState is
	// persisted changes.
	shouldAdvance, shouldReject, pending, err := e.applyDecisionWithOptimisticLock(ctx, inst, nodeID, &cfg, response)
	if err != nil {
		return err
	}

	// Execute the deferred side-effect (dispatch to the next sequential
	// approver) exactly once, AFTER the approval state has been durably
	// persisted by applyDecisionWithOptimisticLock. The dispatch intent is
	// computed during decision evaluation — possibly recomputed on each CAS
	// retry — but it is never executed inside that retry loop, so a
	// cross-process optimistic-lock conflict that re-runs the evaluation cannot
	// deliver a duplicate approval-request envelope to the next approver. If the
	// CAS path declined a late decision (instance no longer running), err is
	// non-nil above and we returned before reaching here, so no dispatch occurs.
	if pending != nil {
		if derr := e.dispatcher.Dispatch(ctx, pending.req, pending.approverID); derr != nil {
			return fmt.Errorf("dispatch to next sequential approver %s: %w", pending.approverID, derr)
		}
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

// maxResumeDecisionCASRetries bounds how many times ResumeInstance re-reads and
// re-applies an approval decision when the optimistic-lock guard reports that a
// concurrent decision on the same instance won the race. Conflicts are rare
// (only under genuine same-node concurrency) and each retry observes the prior
// writer's persisted state, so a small bound converges.
const maxResumeDecisionCASRetries = 8

// applyDecisionWithOptimisticLock evaluates the approval response against the
// node's mode and persists the resulting approval state under an
// optimistic-locking guard (Finding 1.6 / Requirement 2.6).
//
// When the instance store implements OptimisticInstanceDataUpdater, the persist
// is a conditional UPDATE guarded by the row version observed at read time. If a
// concurrent decision on the same instance committed first, the CAS reports a
// conflict; this function re-reads the fresh instance state, re-applies the
// per-mode decision logic against it, and retries — so neither vote is lost,
// even across processes that share one database (where a per-process mutex
// cannot help).
//
// When the store does not implement the capability (e.g. a lightweight test
// mock), it falls back to the existing unconditional UpdateInstanceData; the
// per-instance mutex held by the caller still serializes writers within one
// process.
//
// The per-mode decision logic in process*Mode is unchanged — only HOW the
// approval state is persisted changes (preserves 3.2, 3.3, 3.12).
func (e *WorkflowExecutor) applyDecisionWithOptimisticLock(ctx context.Context, inst *WorkflowInstance, nodeID string, cfg *ApprovalNodeConfig, response ApprovalResponse) (shouldAdvance, shouldReject bool, pending *pendingDispatch, err error) {
	cas, ok := e.instanceStore.(OptimisticInstanceDataUpdater)
	if !ok {
		// No optimistic-locking capability: evaluate and persist with the
		// existing unconditional overwrite (legacy path for stores/mocks that
		// do not implement the capability). Return the pending dispatch from the
		// single evaluation.
		shouldAdvance, shouldReject, pending, err = e.processApprovalResponse(ctx, inst, nodeID, cfg, response)
		if err != nil {
			return false, false, nil, err
		}
		if perr := e.instanceStore.UpdateInstanceData(ctx, inst.ID, inst.InstanceData); perr != nil {
			return false, false, nil, fmt.Errorf("persist approval state: %w", perr)
		}
		return shouldAdvance, shouldReject, pending, nil
	}

	for attempt := 0; attempt < maxResumeDecisionCASRetries; attempt++ {
		// Evaluate the per-mode decision logic against the current in-memory
		// instance state (fresh on the first attempt from ResumeInstance's Get,
		// re-read below on each conflict). This re-applies this approver's vote
		// on top of whatever state is currently persisted. The pending dispatch
		// intent is recomputed on each attempt, which is harmless: it is never
		// executed here, only by the caller after the winning CAS commit.
		shouldAdvance, shouldReject, pending, err = e.processApprovalResponse(ctx, inst, nodeID, cfg, response)
		if err != nil {
			return false, false, nil, err
		}

		newVersion, perr := cas.UpdateInstanceDataCAS(ctx, inst.ID, inst.RowVersion, inst.InstanceData)
		if perr == nil {
			inst.RowVersion = newVersion
			// Return the pending dispatch from the attempt whose CAS commit
			// SUCCEEDED, so the caller dispatches exactly once against durably
			// persisted state.
			return shouldAdvance, shouldReject, pending, nil
		}
		if !errors.Is(perr, ErrInstanceVersionConflict) {
			return false, false, nil, fmt.Errorf("persist approval state: %w", perr)
		}

		// A concurrent decision committed first. Re-read the fresh instance
		// state (which now includes the other writer's vote) and retry: the
		// per-mode logic re-applies this decision on top of the merged state so
		// no vote is lost.
		fresh, gerr := e.instanceStore.Get(ctx, inst.ID)
		if gerr != nil {
			return false, false, nil, fmt.Errorf("reload instance after version conflict: %w", gerr)
		}
		if fresh == nil {
			return false, false, nil, fmt.Errorf("instance %s disappeared during concurrent decision", inst.ID)
		}
		inst.InstanceData = fresh.InstanceData
		inst.RowVersion = fresh.RowVersion
		inst.Status = fresh.Status

		// If the concurrent decision that won the race already SETTLED the
		// instance (the winning writer advanced past this node and reached a
		// terminal node, rejected, or blocked it), this decision arrived too
		// late: the node's outcome is already decided. Re-applying it on top of
		// the settled state and returning shouldAdvance/shouldReject again would
		// re-execute downstream nodes or re-complete/re-fail the instance. Mirror
		// the top-level running-state guard in ResumeInstance so the
		// cross-process CAS path behaves exactly like the single-process mutex
		// path, where the second writer's Get observes the settled status and is
		// rejected here. The winning writer's vote is preserved (it is in
		// fresh.InstanceData); only this redundant late decision is declined.
		if inst.Status != InstanceRunning {
			return false, false, nil, fmt.Errorf("instance %s is not running (status: %s), cannot process approval response", inst.ID, inst.Status)
		}
	}
	return false, false, nil, fmt.Errorf("persist approval state: exhausted %d optimistic-lock retries for instance %s node %s", maxResumeDecisionCASRetries, inst.ID, nodeID)
}

// processApprovalResponse evaluates the response against the approval mode and
// returns (shouldAdvance, shouldReject, pending, error).
//   - shouldAdvance=true means the approval node is satisfied and workflow should continue.
//   - shouldReject=true means the approval node is rejected and workflow should fail.
//   - Both false means still waiting for more responses.
//   - pending is a deferred dispatch intent (sequential mode only); nil otherwise.
//     It is NOT executed here — the caller dispatches it exactly once after the
//     approval state is durably committed, so an optimistic-lock retry cannot
//     deliver a duplicate request.
func (e *WorkflowExecutor) processApprovalResponse(ctx context.Context, inst *WorkflowInstance, nodeID string, cfg *ApprovalNodeConfig, response ApprovalResponse) (bool, bool, *pendingDispatch, error) {
	if !isAllowedApprovalDecision(response.Decision) {
		return false, false, nil, fmt.Errorf("invalid approval decision %q", response.Decision)
	}
	if !isConfiguredApprover(cfg, response.ApproverID) {
		return false, false, nil, fmt.Errorf("approver %q is not assigned to approval node %s", response.ApproverID, nodeID)
	}

	switch cfg.Mode {
	case ModeSingle:
		shouldAdvance, shouldReject, err := e.processSingleMode(response)
		return shouldAdvance, shouldReject, nil, err
	case ModeCountersign:
		shouldAdvance, shouldReject, err := e.processCountersignMode(ctx, inst, nodeID, cfg, response)
		return shouldAdvance, shouldReject, nil, err
	case ModeAnyNofM:
		shouldAdvance, shouldReject, err := e.processAnyNofMMode(ctx, inst, nodeID, cfg, response)
		return shouldAdvance, shouldReject, nil, err
	case ModeSequential:
		return e.processSequentialMode(ctx, inst, nodeID, cfg, response)
	default:
		// Unknown mode; treat as single approver.
		shouldAdvance, shouldReject, err := e.processSingleMode(response)
		return shouldAdvance, shouldReject, nil, err
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
//
// The per-mode DECISION logic (state tracking, ordering validation,
// advance/reject/wait outcomes) is unchanged. The only difference from the
// historic behavior is that the dispatch to the next approver is NOT executed
// here: instead this function builds the *ApprovalRequest and the
// nextApproverID exactly as before and RETURNS them as a *pendingDispatch. The
// caller executes the dispatch exactly once, AFTER the approval state is
// durably persisted, so a cross-process optimistic-lock retry that re-runs this
// evaluation cannot deliver a duplicate approval-request envelope. pending is
// nil whenever there is nothing to dispatch (reject, escalate,
// last-approver-advance, or the pre-dispatch ordering-error cases).
func (e *WorkflowExecutor) processSequentialMode(ctx context.Context, inst *WorkflowInstance, nodeID string, cfg *ApprovalNodeConfig, response ApprovalResponse) (bool, bool, *pendingDispatch, error) {
	// Reject immediately on any rejection
	if response.Decision == approvalDecisionReject {
		return false, true, nil, nil
	}
	if response.Decision == approvalDecisionEscalate {
		return false, false, nil, nil
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
		return false, false, nil, fmt.Errorf("approver %q is not in sequential approval order", response.ApproverID)
	}
	for i := 0; i < currentIdx; i++ {
		if state.Decisions[order[i]] != approvalDecisionApprove {
			return false, false, nil, fmt.Errorf("sequential approval response from %q arrived before approver %q completed", response.ApproverID, order[i])
		}
	}

	// If this is the last approver in the sequence, advance the workflow
	if currentIdx >= len(order)-1 {
		return true, false, nil, nil
	}

	// Build the deferred dispatch intent for the next approver in the sequence.
	// This is NOT executed here; the caller dispatches it exactly once after the
	// approval state is durably committed (see ResumeInstance), so an
	// optimistic-lock retry cannot deliver a duplicate request.
	nextApproverID := order[currentIdx+1]

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

	// Still waiting; next approver needs to respond. Return the dispatch intent
	// for the caller to execute after persistence.
	return false, false, &pendingDispatch{req: req, approverID: nextApproverID}, nil
}

// markInstanceFailed marks the workflow instance as failed and records the event.
func (e *WorkflowExecutor) markInstanceFailed(ctx context.Context, inst *WorkflowInstance, reason string) error {
	if err := e.instanceStore.UpdateStatus(ctx, inst.ID, InstanceFailed); err != nil {
		return fmt.Errorf("mark instance failed: %w", err)
	}
	inst.Status = InstanceFailed

	e.surfaceWriteError(ctx, inst.ID, "", "audit_instance_failed",
		e.auditStore.Append(ctx, &AuditEntry{
			ID:         generateID("audit"),
			InstanceID: inst.ID,
			EventType:  "instance_failed",
			Details:    fmt.Sprintf(`{"reason":"%s"}`, reason),
			Timestamp:  NormalizeAuditTimestamp(time.Time{}),
		}))
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
	e.surfaceWriteError(ctx, instanceID, nodeID, "audit_node_timeout",
		e.auditStore.Append(ctx, &AuditEntry{
			ID:         generateID("audit"),
			InstanceID: instanceID,
			NodeID:     nodeID,
			EventType:  "node_timeout",
			Details:    `{"reason":"timeout_exceeded"}`,
			Timestamp:  NormalizeAuditTimestamp(time.Time{}),
		}))

	// Parse the approval node config to check for fallback approver.
	var cfg ApprovalNodeConfig
	if err := json.Unmarshal(node.Config, &cfg); err != nil {
		return fmt.Errorf("parse approval node config: %w", err)
	}
	if inst != nil {
		ctx = WithApprovalResolveContext(ctx, ApprovalResolveContextFromInstanceData(inst.InstanceData))
	}
	if err := e.resolveApprovalNodeConfig(ctx, &cfg); err != nil {
		return err
	}
	// EscalationManager already owns delivery retries for this node — wait for it.
	if e.escalationMgr != nil && e.escalationMgr.HasPendingForInstance(instanceID, nodeID) {
		return nil
	}
	// A fallback is dispatched only once. Its routing state is persisted on the
	// running node execution so the next timeout check blocks the instance
	// instead of sending the same approval to the fallback approver again.
	if e.fallbackAlreadyDispatched(ctx, instanceID, nodeID) {
		return e.markNodeBlocked(ctx, inst, node, "timeout", "fallback approver timed out")
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
	e.surfaceWriteError(ctx, instanceID, nodeID, "audit_approver_unavailable",
		e.auditStore.Append(ctx, &AuditEntry{
			ID:         generateID("audit"),
			InstanceID: instanceID,
			NodeID:     nodeID,
			EventType:  "approver_unavailable",
			ActorID:    approverID,
			Details:    `{"reason":"capability_disabled"}`,
			Timestamp:  NormalizeAuditTimestamp(time.Time{}),
		}))

	var cfg ApprovalNodeConfig
	if err := json.Unmarshal(node.Config, &cfg); err != nil {
		return fmt.Errorf("parse approval node config: %w", err)
	}
	if inst != nil {
		ctx = WithApprovalResolveContext(ctx, ApprovalResolveContextFromInstanceData(inst.InstanceData))
	}
	if err := e.resolveApprovalNodeConfig(ctx, &cfg); err != nil {
		return err
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
	e.surfaceWriteError(ctx, instanceID, nodeID, "audit_approver_queue_full",
		e.auditStore.Append(ctx, &AuditEntry{
			ID:         generateID("audit"),
			InstanceID: instanceID,
			NodeID:     nodeID,
			EventType:  "approver_queue_full",
			ActorID:    approverID,
			Details:    `{"reason":"queue_full"}`,
			Timestamp:  NormalizeAuditTimestamp(time.Time{}),
		}))

	var cfg ApprovalNodeConfig
	if err := json.Unmarshal(node.Config, &cfg); err != nil {
		return fmt.Errorf("parse approval node config: %w", err)
	}
	if inst != nil {
		ctx = WithApprovalResolveContext(ctx, ApprovalResolveContextFromInstanceData(inst.InstanceData))
	}
	if err := e.resolveApprovalNodeConfig(ctx, &cfg); err != nil {
		return err
	}

	return e.handleFallbackRouting(ctx, inst, node, &cfg, "queue_full")
}

// handleFallbackRouting implements the core fallback logic shared by timeout,
// unavailability, and queue-full handlers.
// reason is one of: "timeout", "unavailable", "queue_full".
func (e *WorkflowExecutor) handleFallbackRouting(ctx context.Context, inst *WorkflowInstance, node *WorkflowNode, cfg *ApprovalNodeConfig, reason string) error {
	// Primary or fallback delivery already in EscalationManager retry — do not
	// double-route or block until the queue exhausts (onEscalationFailed).
	if e.escalationMgr != nil && inst != nil && node != nil &&
		e.escalationMgr.HasPendingForInstance(inst.ID, node.ID) {
		return nil
	}
	if cfg.FallbackApprover == "" {
		// No fallback: if primary dispatch earlier left escalation_approver on
		// instance data we could re-queue, but without a live EscalationManager
		// entry just block and notify.
		return e.markNodeBlocked(ctx, inst, node, reason, "no fallback approver configured")
	}

	// Persist the active assignee before delivery. This closes the crash window
	// between sending a fallback request and recording it: a restarted timeout
	// ticker must not send the same approval every five minutes.
	if err := e.markFallbackDispatched(ctx, inst.ID, node.ID, cfg.FallbackApprover); err != nil {
		return err
	}

	// Build the approval request for the fallback approver (same payload as primary).
	req := e.buildApprovalRequestFromInstance(inst, node)

	// Attempt to dispatch to fallback approver.
	err := e.dispatcher.DispatchFallback(ctx, req, cfg.FallbackApprover, reason)
	if err != nil {
		// Cascading failure; fallback approver is also unavailable.
		e.surfaceWriteError(ctx, inst.ID, node.ID, "audit_fallback_failed",
			e.auditStore.Append(ctx, &AuditEntry{
				ID:         generateID("audit"),
				InstanceID: inst.ID,
				NodeID:     node.ID,
				EventType:  "fallback_failed",
				ActorID:    cfg.FallbackApprover,
				Details:    fmt.Sprintf(`{"reason":"cascading_failure","fallback_error":"%s","original_reason":"%s"}`, escapeJSON(err.Error()), reason),
				Timestamp:  NormalizeAuditTimestamp(time.Time{}),
			}))

		// Prefer EscalationManager retry queue over immediate block when wired.
		// Max-retries failure re-enters via onEscalationFailed → markNodeBlocked.
		return e.enqueueEscalationOrBlock(ctx, inst, node, req, cfg.FallbackApprover, reason,
			"fallback approver also unavailable: "+err.Error())
	}

	// Fallback dispatch succeeded; record the event.
	e.surfaceWriteError(ctx, inst.ID, node.ID, "audit_fallback_routed",
		e.auditStore.Append(ctx, &AuditEntry{
			ID:         generateID("audit"),
			InstanceID: inst.ID,
			NodeID:     node.ID,
			EventType:  "fallback_routed",
			ActorID:    cfg.FallbackApprover,
			Details:    fmt.Sprintf(`{"reason":"%s","original_approvers":%s,"fallback_approver":"%s"}`, reason, marshalStringSlice(cfg.ApproverIDs), cfg.FallbackApprover),
			Timestamp:  NormalizeAuditTimestamp(time.Time{}),
		}))

	return nil
}

func (e *WorkflowExecutor) fallbackAlreadyDispatched(ctx context.Context, instanceID, nodeID string) bool {
	pendingExecs, err := e.instanceStore.GetPendingApprovals(ctx, "")
	if err != nil {
		e.surfaceWriteError(ctx, instanceID, nodeID, "get_pending_approvals_for_fallback_state", err)
		return false
	}
	for _, exec := range pendingExecs {
		if exec.InstanceID != instanceID || exec.NodeID != nodeID {
			continue
		}
		var metadata struct {
			FallbackActive bool `json:"fallback_active"`
		}
		return json.Unmarshal(exec.Result, &metadata) == nil && metadata.FallbackActive
	}
	return false
}

func (e *WorkflowExecutor) markFallbackDispatched(ctx context.Context, instanceID, nodeID, fallbackApprover string) error {
	pendingExecs, err := e.instanceStore.GetPendingApprovals(ctx, "")
	if err != nil {
		return fmt.Errorf("query node execution for fallback state: %w", err)
	}
	for _, exec := range pendingExecs {
		if exec.InstanceID != instanceID || exec.NodeID != nodeID {
			continue
		}
		metadata := make(map[string]interface{})
		if len(exec.Result) > 0 && json.Unmarshal(exec.Result, &metadata) != nil {
			return fmt.Errorf("parse node execution metadata for fallback state")
		}
		metadata["fallback_active"] = true
		metadata["fallback_approver"] = fallbackApprover
		metadata["approver_ids"] = []string{fallbackApprover}
		result, err := json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("marshal fallback state: %w", err)
		}
		if err := e.instanceStore.UpdateNodeExecution(ctx, exec.ID, NodeRunning, result, ""); err != nil {
			return fmt.Errorf("persist fallback state: %w", err)
		}
		return nil
	}
	// Some lightweight InstanceStore implementations only model instance state
	// and do not retain node executions. Keep fallback delivery compatible with
	// them; production SQLite stores always find the durable execution above.
	return nil
}

// enqueueEscalationOrBlock queues EscalationManager retries for approverID.
// When no manager is wired, marks the node blocked immediately.
// Returns nil when the failure was absorbed (queued or blocked).
func (e *WorkflowExecutor) enqueueEscalationOrBlock(ctx context.Context, inst *WorkflowInstance, node *WorkflowNode, req *ApprovalRequest, approverID, reason, blockDetails string) error {
	if e == nil || inst == nil || node == nil {
		return fmt.Errorf("enqueue escalation: missing executor/instance/node")
	}
	approverID = strings.TrimSpace(approverID)
	if e.escalationMgr == nil || approverID == "" {
		return e.markNodeBlocked(ctx, inst, node, reason, blockDetails)
	}
	if req == nil {
		req = e.buildApprovalRequestFromInstance(inst, node)
	}
	if qerr := e.escalationMgr.Escalate(ctx, req, approverID); qerr != nil {
		return e.markNodeBlocked(ctx, inst, node, reason, "escalation queue failed: "+qerr.Error())
	}
	// Escalate may redeliver immediately when the approver is now online. Only
	// mark pending when THIS approver is still in the queue — not when some
	// other peer is pending on the same node (countersign multi-fail).
	if !e.escalationMgr.HasPendingApprover(inst.ID, node.ID, approverID) {
		return nil
	}
	if e.notifier != nil {
		_ = e.notifier.NotifyInitiator(ctx, inst.ID,
			fmt.Sprintf("escalation pending: approver unavailable (%s)", reason),
			fmt.Sprintf("Node ID: %s, Approver: %s, Details: %s", node.ID, approverID, blockDetails),
		)
	}
	// Live queue is SoT for approver lists (same path as deliver/fail hooks).
	if !e.syncEscalationMarkersFromQueue(inst, node.ID) {
		return nil
	}
	inst.InstanceData["escalation_reason"] = reason
	e.surfaceWriteError(ctx, inst.ID, node.ID, "update_instance_data_escalation_pending",
		e.instanceStore.UpdateInstanceData(ctx, inst.ID, inst.InstanceData))
	return nil
}

// stringSliceFromInstanceData coerces instance-data list fields that may be
// []string or []interface{} after JSON round-trips.
func stringSliceFromInstanceData(raw interface{}) []string {
	switch v := raw.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, s := range v {
			if t := strings.TrimSpace(s); t != "" {
				out = append(out, t)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				if t := strings.TrimSpace(s); t != "" {
					out = append(out, t)
				}
			}
		}
		return out
	default:
		return nil
	}
}

// handlePrimaryDispatchFailure absorbs a failed first-hop Dispatch for single /
// sequential modes: prefer configured fallback, else EscalationManager retry of
// the primary approver, else return the original error (legacy hard-fail).
func (e *WorkflowExecutor) handlePrimaryDispatchFailure(ctx context.Context, inst *WorkflowInstance, node *WorkflowNode, cfg *ApprovalNodeConfig, req *ApprovalRequest, approverID string, dispatchErr error) error {
	if dispatchErr == nil {
		return nil
	}
	approverID = strings.TrimSpace(approverID)
	e.surfaceWriteError(ctx, inst.ID, node.ID, "audit_dispatch_failed",
		e.auditStore.Append(ctx, &AuditEntry{
			ID:         generateID("audit"),
			InstanceID: inst.ID,
			NodeID:     node.ID,
			EventType:  "dispatch_failed",
			ActorID:    approverID,
			Details:    fmt.Sprintf(`{"error":"%s"}`, escapeJSON(dispatchErr.Error())),
			Timestamp:  NormalizeAuditTimestamp(time.Time{}),
		}))

	if cfg != nil && strings.TrimSpace(cfg.FallbackApprover) != "" {
		return e.handleFallbackRouting(ctx, inst, node, cfg, "unavailable")
	}
	if e.escalationMgr != nil && approverID != "" {
		return e.enqueueEscalationOrBlock(ctx, inst, node, req, approverID, "unavailable",
			"primary dispatch failed: "+dispatchErr.Error())
	}
	return fmt.Errorf("dispatch to approver: %w", dispatchErr)
}

// handlePartialMultiDispatchFailure absorbs a mid-fan-out Dispatch failure for
// countersign / any-N-of-M. When EscalationManager is wired, the failed approver
// is queued for retry and the caller may continue delivering to remaining
// approvers. Without a manager, preserves legacy soft-block (instance blocked,
// timeout may later route fallback).
//
// Returns continueLoop=true when the executor should keep dispatching the rest
// of the approver list; false when fan-out should stop (legacy block path).
func (e *WorkflowExecutor) handlePartialMultiDispatchFailure(
	ctx context.Context,
	inst *WorkflowInstance,
	node *WorkflowNode,
	req *ApprovalRequest,
	failedApprover string,
	dispatched []string,
	dispatchErr error,
) (continueLoop bool, err error) {
	if dispatchErr == nil {
		return true, nil
	}
	failedApprover = strings.TrimSpace(failedApprover)
	e.surfaceWriteError(ctx, inst.ID, node.ID, "audit_dispatch_partial_failure",
		e.auditStore.Append(ctx, &AuditEntry{
			ID:         generateID("audit"),
			InstanceID: inst.ID,
			NodeID:     node.ID,
			EventType:  "dispatch_partial_failure",
			ActorID:    failedApprover,
			Details: fmt.Sprintf(`{"failed_approver":"%s","dispatched":%s,"error":"%s"}`,
				failedApprover, marshalStringSlice(dispatched), escapeJSON(dispatchErr.Error())),
			Timestamp: NormalizeAuditTimestamp(time.Time{}),
		}))

	if e.escalationMgr != nil && failedApprover != "" {
		// Keep instance running so already-notified approvers can still decide;
		// EscalationManager retries the unreachable peer.
		if qerr := e.enqueueEscalationOrBlock(ctx, inst, node, req, failedApprover, "partial_dispatch",
			fmt.Sprintf("partial dispatch failure at approver %s: %s", failedApprover, dispatchErr.Error())); qerr != nil {
			return false, qerr
		}
		return true, nil
	}

	// Legacy: soft-block so timeout / fallback handlers can recover.
	pendingExecs, _ := e.instanceStore.GetPendingApprovals(ctx, "")
	for _, exec := range pendingExecs {
		if exec.InstanceID == inst.ID && exec.NodeID == node.ID {
			e.surfaceWriteError(ctx, inst.ID, node.ID, "update_node_execution_blocked",
				e.instanceStore.UpdateNodeExecution(ctx, exec.ID, NodeBlocked, nil,
					fmt.Sprintf("partial dispatch failure at approver %s", failedApprover)))
			break
		}
	}
	e.surfaceWriteError(ctx, inst.ID, node.ID, "update_status_blocked",
		e.instanceStore.UpdateStatus(ctx, inst.ID, InstanceBlocked))
	inst.Status = InstanceBlocked
	return false, nil
}

// syncEscalationMarkersFromQueue rewrites instance_data escalation_* fields from
// the live EscalationManager queue (source of truth after deliver/fail).
// Returns whether any peers remain pending for this node.
func (e *WorkflowExecutor) syncEscalationMarkersFromQueue(inst *WorkflowInstance, nodeID string) bool {
	if inst == nil {
		return false
	}
	if inst.InstanceData == nil {
		inst.InstanceData = map[string]interface{}{}
	}
	var remaining []string
	if e.escalationMgr != nil {
		remaining = e.escalationMgr.PendingApprovers(inst.ID, nodeID)
	}
	if len(remaining) == 0 {
		delete(inst.InstanceData, "escalation_pending")
		delete(inst.InstanceData, "escalation_reason")
		delete(inst.InstanceData, "escalation_approvers")
		delete(inst.InstanceData, "escalation_approver")
		return false
	}
	inst.InstanceData["escalation_pending"] = true
	inst.InstanceData["escalation_approvers"] = remaining
	inst.InstanceData["escalation_approver"] = remaining[len(remaining)-1]
	return true
}

// onEscalationDelivered clears the delivered approver from instance-data lists.
// When no pending peers remain and EscalationManager has no more queue items for
// the instance, escalation_pending is cleared.
func (e *WorkflowExecutor) onEscalationDelivered(ctx context.Context, esc *EscalationRequest) {
	if e == nil || esc == nil || strings.TrimSpace(esc.InstanceID) == "" {
		return
	}
	inst, err := e.instanceStore.Get(ctx, esc.InstanceID)
	if err != nil || inst == nil {
		return
	}
	// Queue entry already removed; rebuild markers from remaining live peers.
	_ = e.syncEscalationMarkersFromQueue(inst, esc.NodeID)
	e.surfaceWriteError(ctx, inst.ID, esc.NodeID, "update_instance_data_escalation_delivered",
		e.instanceStore.UpdateInstanceData(ctx, inst.ID, inst.InstanceData))
}

// onEscalationFailed is the EscalationManager max-retries hook.
// For multi-approver nodes, only this peer is exhausted — drop them from the
// pending list and keep the instance running while other peers still retry.
// Mark the node blocked only when no escalations remain for this node.
func (e *WorkflowExecutor) onEscalationFailed(ctx context.Context, esc *EscalationRequest) {
	if e == nil || esc == nil || strings.TrimSpace(esc.InstanceID) == "" {
		return
	}
	inst, err := e.instanceStore.Get(ctx, esc.InstanceID)
	if err != nil || inst == nil {
		return
	}
	// Queue entry was already removed. Rebuild markers from live peers.
	// Peers still queued → keep instance running (countersign).
	peersStillPending := e.syncEscalationMarkersFromQueue(inst, esc.NodeID)
	if peersStillPending {
		e.surfaceWriteError(ctx, inst.ID, esc.NodeID, "update_instance_data_escalation_peer_failed",
			e.instanceStore.UpdateInstanceData(ctx, inst.ID, inst.InstanceData))
		if e.notifier != nil {
			_ = e.notifier.NotifyInitiator(ctx, inst.ID,
				fmt.Sprintf("escalation failed for approver %s (other peers still retrying)", strings.TrimSpace(esc.HumanApprover)),
				fmt.Sprintf("node=%s attempts=%d", esc.NodeID, esc.Attempts),
			)
		}
		return
	}
	// No remaining retries for this node — block.
	var node *WorkflowNode
	if e.store != nil && strings.TrimSpace(inst.VersionID) != "" {
		if ver, verr := e.store.GetVersion(ctx, inst.VersionID); verr == nil && ver != nil {
			node = findNodeByID(&ver.Graph, esc.NodeID)
		}
	}
	if node == nil {
		node = &WorkflowNode{ID: firstNonEmptyString(esc.NodeID, inst.CurrentNodeID), Label: esc.NodeID}
	}
	details := fmt.Sprintf("escalation max retries exhausted for approver %s (attempts=%d)", esc.HumanApprover, esc.Attempts)
	_ = e.markNodeBlocked(ctx, inst, node, "escalation_failed", details)
}

// markNodeBlocked marks an approval node as "blocked", updates the instance status,
// notifies the workflow initiator, and records the event in the audit trail.
func (e *WorkflowExecutor) markNodeBlocked(ctx context.Context, inst *WorkflowInstance, node *WorkflowNode, reason, details string) error {
	// Update node execution status to blocked.
	// We look up the node execution by instance+node ID.
	pendingExecs, _ := e.instanceStore.GetPendingApprovals(ctx, "")
	for _, exec := range pendingExecs {
		if exec.InstanceID == inst.ID && exec.NodeID == node.ID {
			e.surfaceWriteError(ctx, inst.ID, node.ID, "update_node_execution_blocked",
				e.instanceStore.UpdateNodeExecution(ctx, exec.ID, NodeBlocked, nil, reason+": "+details))
			break
		}
	}

	// Update instance status to blocked. This conditional transition (the store
	// guards WHERE status = running) leaves a blocked instance the timeout/
	// reconciliation path can pick up; surface a failure rather than drop it so
	// drift between the in-memory and persisted status is diagnosable.
	e.surfaceWriteError(ctx, inst.ID, node.ID, "update_status_blocked",
		e.instanceStore.UpdateStatus(ctx, inst.ID, InstanceBlocked))
	inst.Status = InstanceBlocked

	// Persist blocked reason on instance data so desktop reconcile / directory
	// projections can surface attention without relying only on status.
	if inst.InstanceData == nil {
		inst.InstanceData = map[string]interface{}{}
	}
	inst.InstanceData["blocked_reason"] = reason
	inst.InstanceData["blocked_details"] = details
	inst.InstanceData["blocked_node_id"] = node.ID
	inst.InstanceData["blocked_at"] = time.Now().UTC().Format(time.RFC3339)
	delete(inst.InstanceData, "escalation_pending")
	delete(inst.InstanceData, "escalation_approver")
	delete(inst.InstanceData, "escalation_approvers")
	delete(inst.InstanceData, "escalation_reason")
	e.surfaceWriteError(ctx, inst.ID, node.ID, "update_instance_data_blocked",
		e.instanceStore.UpdateInstanceData(ctx, inst.ID, inst.InstanceData))

	// Record blocked event in audit trail.
	e.surfaceWriteError(ctx, inst.ID, node.ID, "audit_node_blocked",
		e.auditStore.Append(ctx, &AuditEntry{
			ID:         generateID("audit"),
			InstanceID: inst.ID,
			NodeID:     node.ID,
			EventType:  "node_blocked",
			Details:    fmt.Sprintf(`{"reason":"%s","details":"%s"}`, reason, escapeJSON(details)),
			Timestamp:  NormalizeAuditTimestamp(time.Time{}),
		}))

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

// surfaceWriteError records a non-fatal persistence/audit write failure that
// occurs AFTER a node's work has already happened (completion bookkeeping,
// audit trail). These writes record progress that has already been made, so
// aborting would discard genuine work; but they must NOT be silently dropped
// (Finding 1.7 / Requirement 2.7). Surfacing them via the log (and, where a
// store is available, the audit trail) keeps a mid-graph crash diagnosable and
// the orphan-reconciliation path able to repair drift. The caller decides
// whether the failure is fatal; this helper only ensures it is observable.
func (e *WorkflowExecutor) surfaceWriteError(ctx context.Context, instanceID, nodeID, op string, err error) {
	if err == nil {
		return
	}
	log.Printf("[workflow-executor] critical write dropped: instance=%s node=%s op=%s err=%v",
		instanceID, nodeID, op, err)
	// Best-effort: leave a breadcrumb in the audit trail so the drift is
	// visible to operators and reconciliation. If the audit store itself is the
	// failing dependency, this is a no-op beyond the log line above.
	_ = e.auditStore.Append(ctx, &AuditEntry{
		ID:         generateID("audit"),
		InstanceID: instanceID,
		NodeID:     nodeID,
		EventType:  "critical_write_failed",
		Details:    fmt.Sprintf(`{"op":"%s","error":"%s"}`, op, escapeJSON(err.Error())),
		Timestamp:  NormalizeAuditTimestamp(time.Time{}),
	})
}

// executeNode dispatches execution to the appropriate handler based on node type.
//
// Recoverable mid-graph execution (Finding 1.7 / Requirement 2.7): the writes
// that establish WHERE execution will resume — advancing current_node and
// creating the node-execution record — are propagated as fatal. If either
// fails, the node is NOT executed and the in-memory cursor is NOT advanced, so
// the instance is left at a clean, resumable pre-execution boundary instead of
// a state where current_node has drifted ahead of the persisted node-exec /
// status records. Writes that merely RECORD work already done (node-completion
// status, audit events) are surfaced via surfaceWriteError rather than silently
// dropped, so a crash leaves a diagnosable, reconcilable trail.
func (e *WorkflowExecutor) executeNode(ctx context.Context, inst *WorkflowInstance, node *WorkflowNode, graph *WorkflowGraph) error {
	// Advance the resume cursor BEFORE executing the node. This write decides
	// where ResumeInstance / reconciliation will pick up, so a failure here is
	// fatal: do not advance the in-memory cursor and do not execute, leaving the
	// instance resumable at its prior consistent position.
	if err := e.instanceStore.UpdateCurrentNode(ctx, inst.ID, node.ID); err != nil {
		return fmt.Errorf("advance current node to %s: %w", node.ID, err)
	}
	inst.CurrentNodeID = node.ID

	// Create node execution record. This is the durable record that the node is
	// in-flight; a failure means we cannot account for the node's execution, so
	// it is fatal and we stop before running the node.
	now := time.Now().UTC()
	nodeExec := &NodeExecution{
		ID:         generateID("nexec"),
		InstanceID: inst.ID,
		NodeID:     node.ID,
		NodeType:   node.Type,
		Status:     NodeRunning,
		StartedAt:  now,
	}
	if err := e.instanceStore.CreateNodeExecution(ctx, nodeExec); err != nil {
		return fmt.Errorf("create node execution for %s: %w", node.ID, err)
	}

	var execErr error
	switch node.Type {
	case NodeTrigger:
		execErr = e.executeTriggerNode(ctx, inst, node, graph)
	case NodeConditionBranch:
		execErr = e.executeConditionBranchNode(ctx, inst, node, graph)
	case NodeApproval:
		execErr = e.executeApprovalNode(ctx, inst, node, graph, nodeExec.ID)
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
		e.surfaceWriteError(ctx, inst.ID, node.ID, "update_node_execution_failed",
			e.instanceStore.UpdateNodeExecution(ctx, nodeExec.ID, NodeFailed, nil, execErr.Error()))

		// Record node_failed audit event
		e.surfaceWriteError(ctx, inst.ID, node.ID, "audit_node_failed",
			e.auditStore.Append(ctx, &AuditEntry{
				ID:         generateID("audit"),
				InstanceID: inst.ID,
				NodeID:     node.ID,
				EventType:  "node_failed",
				Details:    fmt.Sprintf("node_type=%s, reason=%s", node.Type, execErr.Error()),
				Timestamp:  NormalizeAuditTimestamp(time.Time{}),
			}))
		return execErr
	}

	// Mark node as completed (for non-blocking nodes)
	if node.Type != NodeApproval {
		e.surfaceWriteError(ctx, inst.ID, node.ID, "update_node_execution_completed",
			e.instanceStore.UpdateNodeExecution(ctx, nodeExec.ID, NodeCompleted, nil, ""))

		e.surfaceWriteError(ctx, inst.ID, node.ID, "audit_node_completed",
			e.auditStore.Append(ctx, &AuditEntry{
				ID:         generateID("audit"),
				InstanceID: inst.ID,
				NodeID:     node.ID,
				EventType:  "node_completed",
				Details:    fmt.Sprintf("node_type=%s", node.Type),
				Timestamp:  NormalizeAuditTimestamp(time.Time{}),
			}))
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
	e.surfaceWriteError(ctx, inst.ID, node.ID, "audit_node_failed",
		e.auditStore.Append(ctx, &AuditEntry{
			ID:         generateID("audit"),
			InstanceID: inst.ID,
			NodeID:     node.ID,
			EventType:  "node_failed",
			Details:    fmt.Sprintf(`{"reason":"%s"}`, failReason),
			Timestamp:  NormalizeAuditTimestamp(time.Time{}),
		}))
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
func (e *WorkflowExecutor) executeApprovalNode(ctx context.Context, inst *WorkflowInstance, node *WorkflowNode, graph *WorkflowGraph, nodeExecID string) error {
	var cfg ApprovalNodeConfig
	if err := json.Unmarshal(node.Config, &cfg); err != nil {
		return fmt.Errorf("parse approval node config: %w", err)
	}
	originalCfg := cfg
	if inst != nil {
		ctx = WithApprovalResolveContext(ctx, ApprovalResolveContextFromInstanceData(inst.InstanceData))
	}
	if err := e.resolveApprovalNodeConfig(ctx, &cfg); err != nil {
		return err
	}

	if len(cfg.ApproverIDs) == 0 {
		// Prefer fallback approver when role expansion left nobody (e.g. empty department role).
		if fb := strings.TrimSpace(cfg.FallbackApprover); fb != "" {
			cfg.ApproverIDs = []string{fb}
		}
	}
	if len(cfg.ApproverIDs) == 0 {
		reason := fmt.Sprintf("approval node %s has no resolvable approvers (check approval roles / applicant department)", node.ID)
		if e.notifier != nil && inst != nil {
			_ = e.notifier.NotifyInitiator(ctx, inst.ID, "approval node blocked", reason)
		}
		return fmt.Errorf("%s", reason)
	}
	// Two-phase digital modes: when the node is single-approver but expansion
	// produced multiple identities (digital first + human), promote to sequential
	// so the VE can suggest/review before the human finalizes.
	cfg = applyDigitalTwoPhaseDispatchShape(cfg)
	e.recordApprovalRuntimeMetadata(ctx, inst.ID, node.ID, nodeExecID, originalCfg, cfg)

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
		req.Details = cloneStringAnyMap(details)
	} else {
		req.Details = map[string]interface{}{}
	}
	// Enrich details with applicant org context for VE ACL / rules.
	if resolveCtx := ApprovalResolveContextFrom(ctx); resolveCtx != nil {
		if resolveCtx.ApplicantID != "" {
			req.RequesterID = firstNonEmptyString(req.RequesterID, resolveCtx.ApplicantID)
			req.Details["requester_id"] = resolveCtx.ApplicantID
		}
		if resolveCtx.ApplicantName != "" {
			req.RequesterName = firstNonEmptyString(req.RequesterName, resolveCtx.ApplicantName)
			req.Details["requester_name"] = resolveCtx.ApplicantName
		}
		if resolveCtx.DepartmentID != "" {
			req.Details["requester_department"] = resolveCtx.DepartmentID
			req.Details["department_id"] = resolveCtx.DepartmentID
		}
	}
	// Publish execution mode so VE two-phase policy can enforce human confirmation.
	execMode := normalizeApprovalExecutionMode(cfg.ExecutionMode)
	if execMode != "" {
		req.Details["execution_mode"] = execMode
		req.Details["needs_human_confirm"] = execMode == "digital_suggest" || execMode == "digital_review"
	}

	// Dispatch based on approval mode.
	switch cfg.Mode {
	case ModeSingle:
		if err := e.dispatcher.Dispatch(ctx, req, cfg.ApproverIDs[0]); err != nil {
			return e.handlePrimaryDispatchFailure(ctx, inst, node, &cfg, req, cfg.ApproverIDs[0], err)
		}
	case ModeCountersign:
		// Dispatch to all approvers; all must approve.
		var dispatched []string
		for _, approverID := range cfg.ApproverIDs {
			if err := e.dispatcher.Dispatch(ctx, req, approverID); err != nil {
				cont, herr := e.handlePartialMultiDispatchFailure(ctx, inst, node, req, approverID, dispatched, err)
				if herr != nil {
					return herr
				}
				if !cont {
					return nil // legacy soft-block; timeout may recover
				}
				// Escalation queued for this peer; keep fan-out to remaining approvers.
				continue
			}
			dispatched = append(dispatched, approverID)
		}
	case ModeAnyNofM:
		// Dispatch to all approvers; N of M must approve.
		var dispatched []string
		for _, approverID := range cfg.ApproverIDs {
			if err := e.dispatcher.Dispatch(ctx, req, approverID); err != nil {
				cont, herr := e.handlePartialMultiDispatchFailure(ctx, inst, node, req, approverID, dispatched, err)
				if herr != nil {
					return herr
				}
				if !cont {
					return nil
				}
				continue
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
			return e.handlePrimaryDispatchFailure(ctx, inst, node, &cfg, req, order[0], err)
		}
	default:
		return fmt.Errorf("unknown approval mode: %s", cfg.Mode)
	}

	// Execution pauses here and will be resumed via ResumeInstance when response arrives.
	return nil
}

func (e *WorkflowExecutor) recordApprovalRuntimeMetadata(ctx context.Context, instanceID, nodeID, nodeExecID string, originalCfg, resolvedCfg ApprovalNodeConfig) {
	if strings.TrimSpace(nodeExecID) == "" {
		return
	}
	payload := map[string]interface{}{
		"mode":               resolvedCfg.Mode,
		"min_approvals":      resolvedCfg.MinApprovals,
		"timeout_hours":      resolvedCfg.TimeoutHours,
		"approver_ids":       resolvedCfg.ApproverIDs,
		"approver_order":     resolvedCfg.ApproverOrder,
		"fallback_approver":  resolvedCfg.FallbackApprover,
		"original_approvers": originalCfg.ApproverIDs,
		"original_order":     originalCfg.ApproverOrder,
		"original_fallback":  originalCfg.FallbackApprover,
	}
	if hasApprovalRoleReference(originalCfg) {
		payload["approval_role_refs"] = collectApprovalRoleReferences(originalCfg)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		e.surfaceWriteError(ctx, instanceID, nodeID, "marshal_approval_runtime_metadata", err)
		return
	}
	e.surfaceWriteError(ctx, instanceID, nodeID, "update_approval_runtime_metadata",
		e.instanceStore.UpdateNodeExecution(ctx, nodeExecID, NodeRunning, data, ""))
}

func hasApprovalRoleReference(cfg ApprovalNodeConfig) bool {
	return len(collectApprovalRoleReferences(cfg)) > 0
}

func collectApprovalRoleReferences(cfg ApprovalNodeConfig) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, id := range append(append([]string{}, cfg.ApproverIDs...), cfg.ApproverOrder...) {
		if strings.HasPrefix(strings.TrimSpace(id), "role:") {
			appendUniqueString(&out, seen, id)
		}
	}
	if strings.HasPrefix(strings.TrimSpace(cfg.FallbackApprover), "role:") {
		appendUniqueString(&out, seen, cfg.FallbackApprover)
	}
	return out
}

func appendUniqueString(out *[]string, seen map[string]struct{}, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if _, ok := seen[value]; ok {
		return
	}
	seen[value] = struct{}{}
	*out = append(*out, value)
}

func (e *WorkflowExecutor) resolveApprovalNodeConfig(ctx context.Context, cfg *ApprovalNodeConfig) error {
	if cfg == nil || e.approverResolver == nil {
		return nil
	}
	originalIDs := append([]string(nil), cfg.ApproverIDs...)
	var err error
	cfg.ApproverIDs, err = e.approverResolver.ResolveApproverIDs(ctx, cfg.ApproverIDs)
	if err != nil {
		return fmt.Errorf("resolve approval approvers: %w", err)
	}
	if len(cfg.ApproverOrder) > 0 {
		cfg.ApproverOrder, err = e.approverResolver.ResolveApproverIDs(ctx, cfg.ApproverOrder)
		if err != nil {
			return fmt.Errorf("resolve approval order: %w", err)
		}
	}
	if strings.TrimSpace(cfg.FallbackApprover) != "" {
		resolved, err := e.approverResolver.ResolveApproverIDs(ctx, []string{cfg.FallbackApprover})
		if err != nil {
			return fmt.Errorf("resolve fallback approver: %w", err)
		}
		if len(resolved) > 0 {
			cfg.FallbackApprover = resolved[0]
		}
	}
	// Derive execution mode from approval roles when the node config omits it.
	if strings.TrimSpace(cfg.ExecutionMode) == "" {
		if modeLookup, ok := e.approverResolver.(interface {
			ResolveExecutionMode(context.Context, []string) string
		}); ok {
			if mode := modeLookup.ResolveExecutionMode(ctx, originalIDs); mode != "" {
				cfg.ExecutionMode = mode
			}
		}
	}
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
	e.surfaceWriteError(ctx, inst.ID, node.ID, "audit_instance_completed",
		e.auditStore.Append(ctx, &AuditEntry{
			ID:         generateID("audit"),
			InstanceID: inst.ID,
			NodeID:     node.ID,
			EventType:  "instance_completed",
			Details:    fmt.Sprintf(`{"terminal_node_id":"%s"}`, node.ID),
			Timestamp:  NormalizeAuditTimestamp(time.Time{}),
		}))

	// 3. Parse terminal node config
	var termConfig TerminalNodeConfig
	if node.Config != nil {
		if err := json.Unmarshal(node.Config, &termConfig); err != nil {
			// Config parse failure is non-fatal; instance is already completed.
			e.surfaceWriteError(ctx, inst.ID, node.ID, "audit_terminal_config_parse_error",
				e.auditStore.Append(ctx, &AuditEntry{
					ID:         generateID("audit"),
					InstanceID: inst.ID,
					NodeID:     node.ID,
					EventType:  "terminal_config_parse_error",
					Details:    fmt.Sprintf(`{"error":"%s"}`, escapeJSON(err.Error())),
					Timestamp:  NormalizeAuditTimestamp(time.Time{}),
				}))
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
			e.surfaceWriteError(ctx, inst.ID, node.ID, "audit_notification_dispatch_error",
				e.auditStore.Append(ctx, &AuditEntry{
					ID:         generateID("audit"),
					InstanceID: inst.ID,
					NodeID:     node.ID,
					EventType:  "notification_dispatch_error",
					Details:    fmt.Sprintf(`{"error":"%s","recipient_count":%d}`, escapeJSON(err.Error()), len(notifs)),
					Timestamp:  NormalizeAuditTimestamp(time.Time{}),
				}))
		}
	}

	// 6. Start confirmation tracking
	if e.confirmTracker != nil {
		if err := e.confirmTracker.StartTracking(ctx, inst, &termConfig); err != nil {
			// Confirmation tracking failure is non-fatal; instance is already completed.
			e.surfaceWriteError(ctx, inst.ID, node.ID, "audit_confirmation_tracking_error",
				e.auditStore.Append(ctx, &AuditEntry{
					ID:         generateID("audit"),
					InstanceID: inst.ID,
					NodeID:     node.ID,
					EventType:  "confirmation_tracking_error",
					Details:    fmt.Sprintf(`{"error":"%s"}`, escapeJSON(err.Error())),
					Timestamp:  NormalizeAuditTimestamp(time.Time{}),
				}))
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

	e.surfaceWriteError(ctx, inst.ID, "", "audit_instance_completed",
		e.auditStore.Append(ctx, &AuditEntry{
			ID:         generateID("audit"),
			InstanceID: inst.ID,
			EventType:  "instance_completed",
			Timestamp:  NormalizeAuditTimestamp(time.Time{}),
		}))
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
