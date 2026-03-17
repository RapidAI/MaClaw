package main

import (
	"fmt"
	"sync"
	"time"
)

// PermissionMode controls how tool-use permission requests are handled.
// Inspired by hapi's unified permission model across Claude/Codex/OpenCode.
type PermissionMode string

const (
	// PermissionModeDefault requires explicit approval for each tool use.
	PermissionModeDefault PermissionMode = "default"
	// PermissionModeAutoApprove automatically approves all tool-use requests.
	PermissionModeAutoApprove PermissionMode = "auto-approve"
	// PermissionModeReadOnly denies any write/execute operations.
	PermissionModeReadOnly PermissionMode = "read-only"
)

// PermissionDecision represents the outcome of a permission request.
type PermissionDecision string

const (
	PermissionApproved          PermissionDecision = "approved"
	PermissionApprovedForSession PermissionDecision = "approved_for_session"
	PermissionDenied            PermissionDecision = "denied"
	PermissionAborted           PermissionDecision = "abort"
)

// PermissionRequest represents a pending tool-use permission request.
type PermissionRequest struct {
	RequestID string      `json:"request_id"`
	SessionID string      `json:"session_id"`
	ToolName  string      `json:"tool_name"`
	Input     interface{} `json:"input,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
}

// PermissionCompletion represents a resolved permission request.
type PermissionCompletion struct {
	RequestID string             `json:"request_id"`
	Decision  PermissionDecision `json:"decision"`
	Reason    string             `json:"reason,omitempty"`
}

// PermissionHandler manages tool-use permission requests for a session.
// It provides a unified interface that works across all execution backends
// (SDK control_request, ACP session/request_permission, etc.).
//
// Design inspired by hapi's CodexPermissionHandler which centralizes
// permission logic with configurable auto-approve policies.
//
// v2: Integrates the full security chain — RiskAssessor → PolicyEngine →
// [LLMSecurityReview] → AuditLog → Decision — in default mode.
type PermissionHandler struct {
	mu       sync.Mutex
	mode     PermissionMode
	pending  map[string]*pendingPermission
	onRequest  func(req PermissionRequest)
	onComplete func(comp PermissionCompletion)

	// sessionRules tracks tools that have been approved for the entire session.
	sessionRules map[string]bool

	// Security chain components (all optional / nil-safe).
	riskAssessor *RiskAssessor
	policyEngine *PolicyEngine
	llmReview    *LLMSecurityReview
	auditLog     *AuditLog

	// callCount tracks consecutive tool calls per session+tool for risk escalation.
	// Key format: "sessionID:toolName".
	callCount map[string]int

	// projectPath is the current project directory, used for risk context.
	projectPath string
}

type pendingPermission struct {
	request  PermissionRequest
	resultCh chan PermissionCompletion
}

// NewPermissionHandler creates a handler with the given mode and callbacks.
func NewPermissionHandler(
	mode PermissionMode,
	onRequest func(PermissionRequest),
	onComplete func(PermissionCompletion),
) *PermissionHandler {
	return &PermissionHandler{
		mode:         mode,
		pending:      make(map[string]*pendingPermission),
		sessionRules: make(map[string]bool),
		callCount:    make(map[string]int),
		onRequest:    onRequest,
		onComplete:   onComplete,
	}
}

// SetSecurityChain configures the security chain components.
// Any parameter may be nil to disable that component.
func (h *PermissionHandler) SetSecurityChain(ra *RiskAssessor, pe *PolicyEngine, llm *LLMSecurityReview, al *AuditLog) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.riskAssessor = ra
	h.policyEngine = pe
	h.llmReview = llm
	h.auditLog = al
}

// SetProjectPath sets the project path used for risk context.
func (h *PermissionHandler) SetProjectPath(path string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.projectPath = path
}

// SetMode updates the permission mode. Thread-safe.
func (h *PermissionHandler) SetMode(mode PermissionMode) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.mode = mode
}

// Mode returns the current permission mode. Thread-safe.
func (h *PermissionHandler) Mode() PermissionMode {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.mode
}

// HandleRequest processes a new permission request. Based on the current
// mode and session rules, it either auto-resolves or creates a pending
// request that must be resolved via Resolve().
//
// In default mode with security chain configured, the flow is:
//   RiskAssessor.Assess → PolicyEngine.Evaluate → [LLMSecurityReview] → AuditLog.Log → Decision
//
// Returns the decision synchronously for auto-resolved requests, or
// blocks until Resolve() is called for pending requests.
func (h *PermissionHandler) HandleRequest(req PermissionRequest) PermissionCompletion {
	h.mu.Lock()

	// Auto-approve mode: approve everything immediately.
	if h.mode == PermissionModeAutoApprove {
		h.mu.Unlock()
		comp := PermissionCompletion{
			RequestID: req.RequestID,
			Decision:  PermissionApproved,
			Reason:    "auto-approved by permission mode",
		}
		if h.onComplete != nil {
			h.onComplete(comp)
		}
		return comp
	}

	// Read-only mode: deny write/execute tools.
	if h.mode == PermissionModeReadOnly {
		if isWriteOrExecuteTool(req.ToolName) {
			h.mu.Unlock()
			comp := PermissionCompletion{
				RequestID: req.RequestID,
				Decision:  PermissionDenied,
				Reason:    "denied by read-only mode",
			}
			if h.onComplete != nil {
				h.onComplete(comp)
			}
			return comp
		}
	}

	// Check session-level approval rules.
	if h.sessionRules[req.ToolName] {
		h.mu.Unlock()
		comp := PermissionCompletion{
			RequestID: req.RequestID,
			Decision:  PermissionApproved,
			Reason:    fmt.Sprintf("tool %q approved for session", req.ToolName),
		}
		if h.onComplete != nil {
			h.onComplete(comp)
		}
		return comp
	}

	// --- Security chain (default mode only, when components are available) ---
	if h.mode == PermissionModeDefault && h.riskAssessor != nil && h.policyEngine != nil {
		// Track consecutive call count.
		callKey := req.SessionID + ":" + req.ToolName
		h.callCount[callKey]++
		count := h.callCount[callKey]
		projectPath := h.projectPath
		mode := h.mode

		// Snapshot security chain refs under lock, then release.
		ra := h.riskAssessor
		pe := h.policyEngine
		llm := h.llmReview
		al := h.auditLog
		h.mu.Unlock()

		// Extract arguments from Input for risk assessment.
		args := extractArgs(req.Input)

		// Step 1: Risk assessment.
		riskCtx := RiskContext{
			ToolName:       req.ToolName,
			Arguments:      args,
			SessionID:      req.SessionID,
			ProjectPath:    projectPath,
			PermissionMode: mode,
			CallCount:      count,
		}
		assessment := ra.Assess(riskCtx)

		// Step 2: Policy evaluation.
		policyAction := pe.Evaluate(req.ToolName, args, assessment.Level)

		// Step 3: LLM security review for high/critical risk.
		var llmVerdict LLMSecurityVerdict
		var llmExplanation string
		if llm != nil && (assessment.Level == RiskHigh || assessment.Level == RiskCritical) {
			llmVerdict, llmExplanation, _ = llm.Review(riskCtx, assessment)
			// If LLM says dangerous, override policy to deny.
			if llmVerdict == VerdictDangerous {
				policyAction = PolicyDeny
			}
		}

		// Step 4: Audit log.
		if al != nil {
			result := string(policyAction)
			if llmExplanation != "" {
				result = fmt.Sprintf("%s (llm: %s — %s)", policyAction, llmVerdict, llmExplanation)
			}
			_ = al.Log(AuditEntry{
				Timestamp:    time.Now(),
				SessionID:    req.SessionID,
				ToolName:     req.ToolName,
				Arguments:    args,
				RiskLevel:    assessment.Level,
				PolicyAction: policyAction,
				Result:       result,
			})
		}

		// Step 5: Make decision based on policy action.
		switch policyAction {
		case PolicyAllow:
			comp := PermissionCompletion{
				RequestID: req.RequestID,
				Decision:  PermissionApproved,
				Reason:    fmt.Sprintf("auto-approved: risk=%s, policy=allow", assessment.Level),
			}
			if h.onComplete != nil {
				h.onComplete(comp)
			}
			return comp

		case PolicyDeny:
			reason := fmt.Sprintf("denied: risk=%s, policy=deny", assessment.Level)
			if llmVerdict == VerdictDangerous {
				reason = fmt.Sprintf("denied: risk=%s, llm=dangerous — %s", assessment.Level, llmExplanation)
			}
			comp := PermissionCompletion{
				RequestID: req.RequestID,
				Decision:  PermissionDenied,
				Reason:    reason,
			}
			if h.onComplete != nil {
				h.onComplete(comp)
			}
			return comp

		case PolicyAudit:
			comp := PermissionCompletion{
				RequestID: req.RequestID,
				Decision:  PermissionApproved,
				Reason:    fmt.Sprintf("approved (audit): risk=%s, policy=audit", assessment.Level),
			}
			if h.onComplete != nil {
				h.onComplete(comp)
			}
			return comp

		case PolicyAsk:
			// Fall through to create a pending request below.
		}

		// PolicyAsk: re-acquire lock to create pending request.
		h.mu.Lock()
	}

	// Create pending request (default path or PolicyAsk from security chain).
	pending := &pendingPermission{
		request:  req,
		resultCh: make(chan PermissionCompletion, 1),
	}
	h.pending[req.RequestID] = pending
	h.mu.Unlock()

	// Notify external handler (e.g., Hub client, mobile UI).
	if h.onRequest != nil {
		h.onRequest(req)
	}

	// Block until resolved.
	return <-pending.resultCh
}

// Resolve completes a pending permission request with the given decision.
func (h *PermissionHandler) Resolve(requestID string, decision PermissionDecision, reason string) error {
	h.mu.Lock()
	pending, ok := h.pending[requestID]
	if !ok {
		h.mu.Unlock()
		return fmt.Errorf("no pending permission request: %s", requestID)
	}
	delete(h.pending, requestID)

	// Track session-level approvals.
	if decision == PermissionApprovedForSession {
		h.sessionRules[pending.request.ToolName] = true
	}
	h.mu.Unlock()

	comp := PermissionCompletion{
		RequestID: requestID,
		Decision:  decision,
		Reason:    reason,
	}
	pending.resultCh <- comp

	if h.onComplete != nil {
		h.onComplete(comp)
	}
	return nil
}

// Reset cancels all pending requests and clears session rules and call counts.
// Called when a session is aborted or restarted.
func (h *PermissionHandler) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for id, pending := range h.pending {
		pending.resultCh <- PermissionCompletion{
			RequestID: id,
			Decision:  PermissionAborted,
			Reason:    "session reset",
		}
	}
	h.pending = make(map[string]*pendingPermission)
	h.sessionRules = make(map[string]bool)
	h.callCount = make(map[string]int)
}

// PendingCount returns the number of unresolved permission requests.
func (h *PermissionHandler) PendingCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.pending)
}

// isWriteOrExecuteTool returns true for tools that modify files or execute commands.
func isWriteOrExecuteTool(toolName string) bool {
	switch toolName {
	case "Write", "WriteFile", "Edit", "MultiEdit",
		"Bash", "Execute", "Shell",
		"computer", "text_editor":
		return true
	}
	return false
}

// extractArgs converts a PermissionRequest.Input into a map[string]interface{}
// suitable for risk assessment. It handles the common cases: already a map,
// a JSON-like structure, or wraps the value under an "input" key.
func extractArgs(input interface{}) map[string]interface{} {
	if input == nil {
		return nil
	}
	if m, ok := input.(map[string]interface{}); ok {
		return m
	}
	// Wrap non-map inputs so the risk assessor can still scan them.
	return map[string]interface{}{"input": input}
}
