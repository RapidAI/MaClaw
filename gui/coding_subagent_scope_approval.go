package main

// coding_subagent_scope_approval.go implements interactive scope approval for
// the CodingSubAgent. When a tool call targets a path outside the declared
// project directory, instead of a hard rejection, the system pauses and asks
// the user for a decision: Deny / Allow Once / Allow Directory.
//
// This replaces the previous hard-reject behavior with a user-in-the-loop
// confirmation, similar to how other coding agents (Cursor, Claude Code)
// handle out-of-scope file access.
//
// Mechanism:
//   - Each CodingSubAgent instance has a scopeApproval *scopeApprovalState
//   - When requireProjectWriteScope/Read/WorkingDir detects an out-of-scope path,
//     it calls scopeApproval.check(path) before rejecting
//   - check() looks at the approved directories list first (instant pass)
//   - If not approved, it calls the onScopeApproval callback (blocks until user responds)
//   - User's response is recorded: deny → reject, allow-once → pass this call,
//     allow-directory → add to approved set for remainder of task

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/security"
)

// scopeApprovalTimeout is the countdown before the backend resolves a pending
// approval. Every request resolves to deny when the user does not respond;
// the timeout is only a bounded wait for an explicit user decision.
const scopeApprovalTimeout = 10 * time.Second

// ScopeApprovalDecision is the user's response to a scope violation prompt.
type ScopeApprovalDecision string

const (
	ScopeApprovalDeny       ScopeApprovalDecision = "deny"
	ScopeApprovalAllowOnce  ScopeApprovalDecision = "allow_once"
	ScopeApprovalAllowDir   ScopeApprovalDecision = "allow_dir"
	ScopeApprovalFullAccess ScopeApprovalDecision = "full_access"

	localHighRiskApprovalKind = "local_high_risk_bash"
)

// ScopeApprovalRequest is sent to the user when an out-of-scope access is detected.
type ScopeApprovalRequest struct {
	ToolName    string // e.g. "write_file", "bash", "read_file"
	Path        string // the out-of-scope path being accessed
	ProjectPath string // the declared project boundary
	Directory   string // the directory that would be approved with "allow_dir"
	Kind        string // optional request kind; empty means project scope approval
	Message     string // optional user-facing reason
	AutoAllow   bool   // legacy metadata; current approval timeouts always deny
}

// ScopeApprovalCallback is the function type for requesting user approval.
// It blocks until the user responds. Implementations may use GUI events,
// IM messages, or terminal prompts depending on the platform.
// Returns the user's decision.
type ScopeApprovalCallback func(req ScopeApprovalRequest) ScopeApprovalDecision

// scopeApprovalState tracks approved directories and pending decisions
// for a single SubAgent task execution.
type scopeApprovalState struct {
	mu                 sync.Mutex
	fullAccess         bool                  // when true, all paths are allowed without prompting (persistent across app restarts via config)
	highRiskFullAccess bool                  // task-scoped approval for high-risk shell commands only
	approvedDirs       map[string]bool       // directories approved with "allow_dir" (case-insensitive keys on Windows)
	onScopeApproval    ScopeApprovalCallback // nil = hard reject (legacy behavior)
	auditApproval      func(ScopeApprovalRequest, ScopeApprovalDecision, string)
}

// newScopeApprovalState creates a new approval state.
// If callback is nil, all out-of-scope access is hard-rejected (backward compatible).
// If fullAccess is true, all scope checks pass immediately (user previously granted full access).
func newScopeApprovalState(callback ScopeApprovalCallback, fullAccess bool) *scopeApprovalState {
	return &scopeApprovalState{
		fullAccess:         fullAccess,
		highRiskFullAccess: fullAccess,
		approvedDirs:       make(map[string]bool),
		onScopeApproval:    callback,
	}
}

// check evaluates whether a path outside the project scope should be allowed.
// Returns "" if access is granted (either pre-approved or user approved).
// Returns a rejection message if denied.
//
// Parameters:
//   - toolName: the tool attempting the access (for display)
//   - path: the out-of-scope path being accessed
//   - projectPath: the declared project boundary
func (s *scopeApprovalState) check(toolName, path, projectPath string) string {
	if s == nil {
		// No approval state = hard reject (legacy behavior).
		return formatScopeRejection(toolName, path, projectPath)
	}

	// Full access granted — skip all scope checks.
	s.mu.Lock()
	if s.fullAccess {
		audit := s.auditApproval
		s.mu.Unlock()
		if audit != nil {
			audit(ScopeApprovalRequest{ToolName: toolName, Path: path, ProjectPath: projectPath}, ScopeApprovalFullAccess, "automatic")
		}
		return ""
	}
	s.mu.Unlock()

	// Normalize the path for directory lookup.
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = filepath.Clean(path)
	}
	dir := filepath.Dir(absPath)

	// Check if this directory (or a parent) is already approved.
	if s.isApproved(absPath) {
		s.mu.Lock()
		audit := s.auditApproval
		s.mu.Unlock()
		if audit != nil {
			audit(ScopeApprovalRequest{ToolName: toolName, Path: path, ProjectPath: projectPath, Directory: dir}, ScopeApprovalAllowDir, "automatic")
		}
		return "" // pre-approved, allow immediately
	}

	// No callback = hard reject.
	if s.onScopeApproval == nil {
		return formatScopeRejection(toolName, path, projectPath)
	}

	// Ask the user.
	decision := s.onScopeApproval(ScopeApprovalRequest{
		ToolName:    toolName,
		Path:        path,
		ProjectPath: projectPath,
		Directory:   dir,
		AutoAllow:   false,
	})

	switch decision {
	case ScopeApprovalAllowOnce:
		return "" // allow this single call
	case ScopeApprovalAllowDir:
		s.approveDir(dir)
		return "" // allow and remember for this task
	case ScopeApprovalFullAccess:
		s.grantFullAccess()
		return "" // allow everything permanently (persisted by callback layer)
	default:
		return formatScopeRejection(toolName, path, projectPath)
	}
}

// checkHighRisk asks the user before a shell command covered by a guardrail is
// executed. An allow_dir decision is not meaningful for a command, and a
// timeout must remain a denial.
func (s *scopeApprovalState) checkHighRisk(toolName, command, projectPath, workingDir, rejection string) string {
	if s == nil {
		return rejection
	}
	s.mu.Lock()
	if s.highRiskFullAccess {
		audit := s.auditApproval
		s.mu.Unlock()
		if audit != nil {
			audit(ScopeApprovalRequest{ToolName: toolName, Path: command, ProjectPath: projectPath, Directory: workingDir, Kind: localHighRiskApprovalKind}, ScopeApprovalFullAccess, "automatic")
		}
		return ""
	}
	callback := s.onScopeApproval
	s.mu.Unlock()
	if callback == nil {
		return rejection
	}
	decision := callback(ScopeApprovalRequest{
		ToolName:    toolName,
		Path:        command,
		ProjectPath: projectPath,
		Directory:   workingDir,
		Kind:        localHighRiskApprovalKind,
		Message:     rejection,
		AutoAllow:   false,
	})
	switch decision {
	case ScopeApprovalAllowOnce:
		return ""
	case ScopeApprovalFullAccess:
		s.grantHighRiskFullAccess()
		return ""
	default:
		return rejection
	}
}

func (s *scopeApprovalState) setAuditCallback(callback func(ScopeApprovalRequest, ScopeApprovalDecision, string)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.auditApproval = callback
	s.mu.Unlock()
}

// isApproved checks if a path falls within any previously approved directory.
func (s *scopeApprovalState) isApproved(absPath string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.approvedDirs) == 0 {
		return false
	}

	pathLower := strings.ToLower(filepath.Clean(absPath))
	for dir := range s.approvedDirs {
		if strings.HasPrefix(pathLower, dir+string(filepath.Separator)) || pathLower == dir {
			return true
		}
	}
	return false
}

// approveDir adds a directory to the approved set.
func (s *scopeApprovalState) approveDir(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = filepath.Clean(dir)
	}
	s.approvedDirs[strings.ToLower(absDir)] = true
}

// grantFullAccess permanently disables scope checking for this SubAgent
// and all future SubAgent instances (persisted via onFullAccessGranted callback).
func (s *scopeApprovalState) grantFullAccess() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fullAccess = true
}

func (s *scopeApprovalState) highRiskApproved() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.highRiskFullAccess
}

func (s *scopeApprovalState) grantHighRiskFullAccess() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.highRiskFullAccess = true
}

func shouldPersistLocalScopeFullAccess(req ScopeApprovalRequest, decision ScopeApprovalDecision) bool {
	return decision == ScopeApprovalFullAccess && req.Kind != localHighRiskApprovalKind
}

// formatScopeRejection generates the rejection message shown to the LLM.
func formatScopeRejection(toolName, path, projectPath string) string {
	switch toolName {
	case "read_file", "Glob", "ripgrep", "list_directory":
		return fmt.Sprintf("\u62d2\u7edd\u8bfb\u53d6\u9879\u76ee\u76ee\u5f55\u5916\u7684\u8def\u5f84\uff1a%s\u3002\u7f16\u7801 SubAgent \u53ea\u80fd\u7528 %s \u8bfb\u53d6/\u641c\u7d22\u9879\u76ee\u8def\u5f84 %s \u5185\u7684\u6587\u4ef6\u3002", path, toolName, projectPath)
	case "bash":
		return fmt.Sprintf("\u62d2\u7edd\u5728\u9879\u76ee\u76ee\u5f55\u5916\u6267\u884c\u547d\u4ee4\uff1a%s\u3002\u7f16\u7801 SubAgent \u7684 bash working_dir \u5fc5\u987b\u4f4d\u4e8e\u9879\u76ee\u8def\u5f84 %s \u5185\u3002", path, projectPath)
	case "git_diff":
		return fmt.Sprintf("\u62d2\u7edd\u67e5\u770b\u9879\u76ee\u76ee\u5f55\u5916\u7684 diff\uff1a%s\u3002\u7f16\u7801 SubAgent \u53ea\u80fd\u68c0\u67e5\u9879\u76ee\u8def\u5f84 %s \u5185\u7684 diff\u3002", path, projectPath)
	default:
		return fmt.Sprintf("\u62d2\u7edd\u4fee\u6539\u9879\u76ee\u76ee\u5f55\u5916\u7684\u6587\u4ef6\uff1a%s\u3002\u7f16\u7801 SubAgent \u53ea\u80fd\u4fee\u6539\u9879\u76ee\u8def\u5f84 %s \u5185\u7684\u6587\u4ef6\u3002", path, projectPath)
	}
}

// buildSubAgentScopeApprovalCallback creates a ScopeApprovalCallback that
// uses the GUI's event system to ask the user for approval.
// It emits a "subagent-scope-approval" event and blocks until the user responds
// via ResolveScopeApproval, the loop is cancelled, or the approval times out.
func buildSubAgentScopeApprovalCallback(handler *IMMessageHandler, loopCtx *LoopContext, onProgress func(string)) ScopeApprovalCallback {
	return func(req ScopeApprovalRequest) ScopeApprovalDecision {
		// If already cancelled, deny immediately.
		if loopCtx != nil && loopCtx.IsCancelled() {
			recordScopeApprovalAudit(handler, "", req, ScopeApprovalDeny, "cancelled")
			return ScopeApprovalDeny
		}

		// Send progress update so user knows what's happening.
		if onProgress != nil {
			message := strings.TrimSpace(req.Message)
			if message == "" {
				message = "\u7f16\u7801 SubAgent \u5c1d\u8bd5\u8bbf\u95ee\u9879\u76ee\u76ee\u5f55\u5916\u7684\u8def\u5f84\uff0c\u7b49\u5f85\u786e\u8ba4..."
			}
			onProgress(fmt.Sprintf("%s\n\u8def\u5f84: %s\n\u9879\u76ee\u8303\u56f4: %s", message, req.Path, req.ProjectPath))
		}

		// Create a channel for the response.
		responseCh := make(chan ScopeApprovalDecision, 1)

		// Store pending approval request.
		approvalID := storePendingScopeApproval(handler, req, responseCh)

		// Emit event to frontend.
		if handler != nil && handler.app != nil {
			emitScopeApprovalEvent(handler.app, approvalID, req)
		}

		// Timeout behavior depends on the request kind. Directory scope remains
		// notification-like; high-risk commands must never be auto-approved.
		timeout := time.NewTimer(scopeApprovalTimeout)
		defer timeout.Stop()

		// Block until response, cancellation, or timeout.
		if loopCtx != nil {
			select {
			case decision := <-responseCh:
				recordScopeApprovalAudit(handler, approvalID, req, decision, "user")
				if shouldPersistLocalScopeFullAccess(req, decision) && handler != nil && handler.app != nil {
					handler.app.persistSubAgentFullAccess()
				}
				return decision
			case <-loopCtx.CancelC:
				pendingScopeApprovals.Delete(approvalID)
				recordScopeApprovalAudit(handler, approvalID, req, ScopeApprovalDeny, "cancelled")
				return ScopeApprovalDeny
			case <-timeout.C:
				pendingScopeApprovals.Delete(approvalID)
				decision := remoteScopeApprovalTimeoutDecision(req)
				if onProgress != nil {
					onProgress(remoteScopeApprovalTimeoutProgress(req, decision))
				}
				recordScopeApprovalAudit(handler, approvalID, req, decision, "timeout")
				return decision
			}
		}
		// No loopCtx — wait with timeout only.
		select {
		case decision := <-responseCh:
			recordScopeApprovalAudit(handler, approvalID, req, decision, "user")
			if shouldPersistLocalScopeFullAccess(req, decision) && handler != nil && handler.app != nil {
				handler.app.persistSubAgentFullAccess()
			}
			return decision
		case <-timeout.C:
			pendingScopeApprovals.Delete(approvalID)
			decision := remoteScopeApprovalTimeoutDecision(req)
			recordScopeApprovalAudit(handler, approvalID, req, decision, "timeout")
			return decision
		}
	}
}

// recordScopeApprovalAudit records every interactive scope/high-risk decision,
// including approvals. The audit log sanitizes sensitive command arguments.
func recordScopeApprovalAudit(handler *IMMessageHandler, approvalID string, req ScopeApprovalRequest, decision ScopeApprovalDecision, source string) {
	if handler == nil || handler.app == nil {
		return
	}
	handler.app.ensureAuditLog()
	if handler.app.auditLog == nil {
		return
	}
	risk := security.RiskMedium
	if strings.Contains(req.Kind, "high_risk") {
		risk = security.RiskHigh
	}
	policy := security.PolicyUserOverride
	if decision == ScopeApprovalDeny {
		policy = security.PolicyDeny
	}
	_ = handler.app.auditLog.Log(security.AuditEntry{
		Timestamp: time.Now(),
		SessionID: approvalID,
		ToolName:  req.ToolName,
		Arguments: map[string]interface{}{
			"path":         req.Path,
			"directory":    req.Directory,
			"project_path": req.ProjectPath,
			"kind":         req.Kind,
			"decision":     string(decision),
			"source":       source,
		},
		RiskLevel:    risk,
		PolicyAction: policy,
		Result:       fmt.Sprintf("scope_approval_%s (%s)", decision, source),
		Source:       "coding_subagent",
	})
}

func recordSubAgentPermissionModeAudit(app *App, fullControl bool) {
	if app == nil {
		return
	}
	app.ensureAuditLog()
	if app.auditLog == nil {
		return
	}
	decision := "request_authorization"
	if fullControl {
		decision = "full_control"
	}
	_ = app.auditLog.Log(security.AuditEntry{
		Timestamp:    time.Now(),
		ToolName:     "coding_subagent_permission",
		Arguments:    map[string]interface{}{"full_control": fullControl},
		RiskLevel:    security.RiskHigh,
		PolicyAction: security.PolicyUserOverride,
		Result:       "permission_mode_" + decision,
		Source:       "coding_subagent",
	})
}

// pendingScopeApproval tracks an outstanding approval request.
type pendingScopeApproval struct {
	ID         string
	Request    ScopeApprovalRequest
	ResponseCh chan ScopeApprovalDecision
}

var (
	pendingScopeApprovals  sync.Map // approvalID → *pendingScopeApproval
	scopeApprovalIDCounter uint64
)

func storePendingScopeApproval(handler *IMMessageHandler, req ScopeApprovalRequest, ch chan ScopeApprovalDecision) string {
	id := fmt.Sprintf("scope_%d", atomic.AddUint64(&scopeApprovalIDCounter, 1))
	pendingScopeApprovals.Store(id, &pendingScopeApproval{
		ID:         id,
		Request:    req,
		ResponseCh: ch,
	})
	return id
}

// ResolveScopeApproval is called by the frontend when the user responds to
// a scope approval prompt. This is exposed as a Wails binding.
func ResolveScopeApproval(approvalID string, decision string) {
	val, ok := pendingScopeApprovals.LoadAndDelete(approvalID)
	if !ok {
		return
	}
	pending := val.(*pendingScopeApproval)

	var d ScopeApprovalDecision
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "allow_once", "allowonce", "allow once":
		d = ScopeApprovalAllowOnce
	case "allow_dir", "allowdir", "allow dir", "allow directory":
		d = ScopeApprovalAllowDir
	case "full_access", "fullaccess", "full access":
		d = ScopeApprovalFullAccess
	default:
		d = ScopeApprovalDeny
	}

	select {
	case pending.ResponseCh <- d:
	default:
	}
}

// emitScopeApprovalEvent sends the approval request to the frontend UI.
func emitScopeApprovalEvent(app *App, approvalID string, req ScopeApprovalRequest) {
	if app == nil {
		return
	}
	app.emitEvent("subagent-scope-approval", map[string]interface{}{
		"id":              approvalID,
		"tool":            req.ToolName,
		"path":            req.Path,
		"project_path":    req.ProjectPath,
		"directory":       req.Directory,
		"kind":            req.Kind,
		"message":         req.Message,
		"auto_allow":      req.AutoAllow,
		"timeout_seconds": int(scopeApprovalTimeout / time.Second),
	})
}

// isSubAgentFullAccessGranted checks if the user has permanently granted
// full filesystem access to CodingSubAgent (persisted in AppConfig).
func (a *App) isSubAgentFullAccessGranted() bool {
	if a == nil {
		return false
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return false
	}
	return cfg.SubAgentFullAccess
}

// persistSubAgentFullAccess saves the full-access grant to config so it
// survives app restarts.
func (a *App) persistSubAgentFullAccess() {
	if a == nil {
		return
	}
	a.PatchConfigFields(map[string]interface{}{
		"subagent_full_access": true,
	})
}

// clearSubAgentFullAccess removes the permanent full-access grant from config.
func (a *App) clearSubAgentFullAccess() error {
	if a == nil {
		return fmt.Errorf("app unavailable")
	}
	_, err := a.PatchConfigFields(map[string]interface{}{
		"subagent_full_access": false,
	})
	return err
}
