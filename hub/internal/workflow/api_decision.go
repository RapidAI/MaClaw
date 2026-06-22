package workflow

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// DecisionAPI provides the production HTTP entry point that routes an approver's
// decision into WorkflowExecutor.ResumeInstance.
//
// This is the caller the runtime half of the approval chain was missing: before
// this handler was wired, an instance that reached an approval node had no way
// to be advanced or rejected (no HTTP handler, no A2A inbound receiver invoked
// ResumeInstance), so it blocked forever (bug condition 1.1 / expected 2.1).
//
// The route is POST /api/v1/instances/{id}/nodes/{nodeID}/decision. The handler
// authorizes the caller with the existing isConfiguredApprover predicate before
// delegating to ResumeInstance; non-approvers receive 403. ResumeInstance and
// the four process*Mode handlers are left unchanged (preserves 3.2, 3.3).
type DecisionAPI struct {
	executor      *WorkflowExecutor
	instanceStore InstanceStore
	workflowStore WorkflowStore
}

// NewDecisionAPI creates a new DecisionAPI with the given dependencies. The
// workflowStore is required so the handler can load the approval node config and
// authorize the caller against the configured approvers for that node.
func NewDecisionAPI(executor *WorkflowExecutor, instanceStore InstanceStore, workflowStore WorkflowStore) *DecisionAPI {
	return &DecisionAPI{
		executor:      executor,
		instanceStore: instanceStore,
		workflowStore: workflowStore,
	}
}

// RegisterRoutes registers the decision route on the given mux. The
// authMiddleware extracts the authenticated user ID and sets it in the
// X-Owner-ID header (mirroring InstanceAPI's convention).
func (api *DecisionAPI) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("POST /api/v1/instances/{id}/nodes/{nodeID}/decision", authMiddleware(api.handleSubmitDecision))
}

// decisionRequest is the request body for submitting an approval decision.
// The ApproverID is taken from the authenticated caller, never from the body.
type decisionRequest struct {
	Decision    string `json:"decision"` // "approve", "reject", "escalate"
	Rationale   string `json:"rationale,omitempty"`
	MatchedRule string `json:"matched_rule,omitempty"`
}

// handleSubmitDecision routes an approver's decision into ResumeInstance.
//
// POST /api/v1/instances/:id/nodes/:nodeID/decision
// Body: {"decision": "approve"|"reject"|"escalate", "rationale": "...", "matched_rule": "..."}
//
// Flow:
//  1. Extract the authenticated caller from the X-Owner-ID header (set by auth middleware).
//  2. Load the instance and its workflow version graph; locate the approval node.
//  3. Resolve Hub approval-role references, then authorize the caller via
//     isConfiguredApprover(cfg, callerID); non-approvers get 403.
//  4. Build an ApprovalResponse with ApproverID = caller and delegate to ResumeInstance.
func (api *DecisionAPI) handleSubmitDecision(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-Owner-ID")
	if userID == "" {
		apiWriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "user identification required")
		return
	}

	instanceID := r.PathValue("id")
	if instanceID == "" {
		apiWriteError(w, http.StatusBadRequest, "INVALID_INPUT", "instance id is required")
		return
	}
	nodeID := r.PathValue("nodeID")
	if nodeID == "" {
		apiWriteError(w, http.StatusBadRequest, "INVALID_INPUT", "node id is required")
		return
	}

	// Load the instance.
	inst, err := api.instanceStore.Get(r.Context(), instanceID)
	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "GET_FAILED", "failed to get instance: "+err.Error())
		return
	}
	if inst == nil {
		apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "instance not found")
		return
	}

	// Load the workflow version graph to resolve the approval node config.
	ver, err := api.workflowStore.GetVersion(r.Context(), inst.VersionID)
	if err != nil {
		apiWriteError(w, http.StatusInternalServerError, "GET_FAILED", "failed to get workflow version: "+err.Error())
		return
	}
	if ver == nil {
		apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "workflow version not found")
		return
	}

	node := findNodeByID(&ver.Graph, nodeID)
	if node == nil {
		apiWriteError(w, http.StatusNotFound, "NOT_FOUND", "node not found in workflow graph")
		return
	}
	if node.Type != NodeApproval {
		apiWriteError(w, http.StatusBadRequest, "NOT_APPROVAL_NODE", "node is not an approval node")
		return
	}

	var cfg ApprovalNodeConfig
	if err := json.Unmarshal(node.Config, &cfg); err != nil {
		apiWriteError(w, http.StatusInternalServerError, "INVALID_NODE_CONFIG", "failed to parse approval node config: "+err.Error())
		return
	}

	if err := api.executor.resolveApprovalNodeConfig(r.Context(), &cfg); err != nil {
		apiWriteError(w, http.StatusInternalServerError, "RESOLVE_APPROVERS_FAILED", "failed to resolve approval role references: "+err.Error())
		return
	}

	// Authorize: only a resolved approver for this node may submit a decision.
	if !isConfiguredApprover(&cfg, userID) {
		apiWriteError(w, http.StatusForbidden, "FORBIDDEN", "caller is not a configured approver for this node")
		return
	}

	// Parse the decision body.
	var req decisionRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apiWriteError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body: "+err.Error())
			return
		}
	}
	if req.Decision == "" {
		apiWriteError(w, http.StatusBadRequest, "INVALID_INPUT", "decision is required")
		return
	}
	// Validate the decision value at the HTTP boundary so a malformed client
	// input (e.g. "maybe") is rejected as 400 rather than surfacing from deep
	// inside ResumeInstance's per-mode logic as a 500. ResumeInstance's own
	// isAllowedApprovalDecision check remains the authority; this mirrors it.
	if !isAllowedApprovalDecision(req.Decision) {
		apiWriteError(w, http.StatusBadRequest, "INVALID_DECISION", "decision must be one of: approve, reject, escalate")
		return
	}

	response := ApprovalResponse{
		Decision:    req.Decision,
		Rationale:   req.Rationale,
		MatchedRule: req.MatchedRule,
		ApproverID:  userID,
		DecidedAt:   time.Now().UTC(),
	}

	if err := api.executor.ResumeInstance(r.Context(), instanceID, nodeID, response); err != nil {
		// A non-running instance cannot accept a decision; surface as a conflict.
		if strings.Contains(err.Error(), "is not running") {
			apiWriteError(w, http.StatusConflict, "NOT_RESUMABLE", err.Error())
			return
		}
		apiWriteError(w, http.StatusInternalServerError, "RESUME_FAILED", "failed to process decision: "+err.Error())
		return
	}

	// Report the post-decision instance status.
	status := inst.Status
	if updated, gerr := api.instanceStore.Get(r.Context(), instanceID); gerr == nil && updated != nil {
		status = updated.Status
	}

	apiWriteJSON(w, http.StatusOK, map[string]any{
		"instance_id": instanceID,
		"node_id":     nodeID,
		"status":      status,
	})
}
