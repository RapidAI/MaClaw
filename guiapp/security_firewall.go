package guiapp

import (
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib/security"
	"strings"
	"sync"
	"time"
)

// SecurityFirewall integrates SecurityRiskAnalyzer + PolicyEngine + AuditLog
// to provide a unified security check before tool execution.
type SecurityFirewall struct {
	analyzer *SecurityRiskAnalyzer
	policy   *PolicyEngine
	audit    *AuditLog
	onAsk    func(toolName string, risk security.RiskAssessment) (bool, error)

	// Session-level approvals: sessionID -> set of approved tool patterns.
	sessionApprovals map[string]map[string]bool
	mu               sync.RWMutex
}

// NewSecurityFirewall creates a firewall combining the three security components.
func NewSecurityFirewall(analyzer *SecurityRiskAnalyzer, policy *PolicyEngine, audit *AuditLog) *SecurityFirewall {
	return &SecurityFirewall{
		analyzer:         analyzer,
		policy:           policy,
		audit:            audit,
		sessionApprovals: make(map[string]map[string]bool),
	}
}

// SetOnAsk sets the callback for user confirmation when policy action is "ask".
func (f *SecurityFirewall) SetOnAsk(fn func(toolName string, risk security.RiskAssessment) (bool, error)) {
	f.onAsk = fn
}

// Check performs a security check before tool execution.
// Returns (allowed, reason). If not allowed, reason explains why.
func (f *SecurityFirewall) Check(toolName string, args map[string]interface{}, ctx *SecurityCallContext) (bool, string) {
	// 0. Denial ledger auto-pause (OpenSquilla-inspired).
	if ledger := security.ProcessDenialLedger(); ledger != nil && ledger.IsPaused() {
		if msg := ledger.PauseBlockMessage(); msg != "" {
			return false, msg
		}
		return false, "autonomous tools paused after consecutive security denials"
	}

	if f.analyzer == nil {
		return true, ""
	}

	// 1. Risk assessment.
	risk := f.analyzer.Assess(toolName, args, ctx)
	mode := "standard"
	if f.policy != nil {
		mode = f.policy.Mode()
	}
	if mode == "developer" {
		f.recordAudit(toolName, args, risk, security.PolicyAllow, "developer_mode_allowed", sessionIDFromSecurityContext(ctx), userIDFromSecurityContext(ctx))
		return true, ""
	}

	// 2. Policy decision, before the session shortcut. A previous "allow bash"
	// click must not waive a later hard deny (shutdown, rm -rf, DROP TABLE).
	sessionID := sessionIDFromSecurityContext(ctx)
	userID := userIDFromSecurityContext(ctx)
	action := security.PolicyAllow
	if f.policy != nil {
		action = f.policy.Evaluate(toolName, args, risk.Level)
	}
	if sessionID != "" && f.isSessionApproved(sessionID, toolName) && !securityActionIsHardDeny(action, mode) {
		f.recordAudit(toolName, args, risk, security.PolicyAudit, "session_approved", sessionID, userID)
		return true, ""
	}
	// 完全控制 skips the confirmation, not the deny. Record the allow as an
	// audit so a later reader can see the grant that waived the prompt.
	resultNote := ""
	if action == security.PolicyAsk && securityCallHasFullControl(ctx) {
		action = security.PolicyAudit
		resultNote = "full_control_auto_allow"
	}

	// 4. Record audit.
	f.recordAudit(toolName, args, risk, action, resultNote, sessionID, userID)

	// 5. Execute decision.
	switch action {
	case security.PolicyAllow:
		security.ProcessDenialLedger().RecordAllow()
		return true, ""
	case security.PolicyAudit:
		security.ProcessDenialLedger().RecordAllow()
		return true, ""
	case security.PolicyDeny:
		if mode == "developer" || mode == "relaxed" {
			security.ProcessDenialLedger().RecordAllow()
			return true, ""
		}
		if mode == "standard" {
			ok, reason := f.confirmOrAllowWithoutChannel(toolName, risk, sessionID)
			if ok {
				security.ProcessDenialLedger().RecordAllow()
				return true, ""
			}
			return f.denyWithLedger(toolName, reason)
		}
		msg := fmt.Sprintf("security policy deny: %s (risk: %s, reason: %s)", toolName, risk.Level, risk.Reason)
		return f.denyWithLedger(toolName, msg)
	case security.PolicyAsk:
		if mode == "developer" || mode == "relaxed" {
			security.ProcessDenialLedger().RecordAllow()
			return true, ""
		}
		ok, reason := f.confirmOrAllowWithoutChannel(toolName, risk, sessionID)
		if ok {
			security.ProcessDenialLedger().RecordAllow()
			return true, ""
		}
		return f.denyWithLedger(toolName, reason)
	default:
		security.ProcessDenialLedger().RecordAllow()
		return true, ""
	}
}

func (f *SecurityFirewall) denyWithLedger(toolName, reason string) (bool, string) {
	if security.ProcessDenialLedger().RecordDeny(toolName, reason) {
		if pause := security.ProcessDenialLedger().PauseBlockMessage(); pause != "" {
			return false, reason + " | " + pause
		}
	}
	return false, reason
}

func (f *SecurityFirewall) confirmOrAllowWithoutChannel(toolName string, risk security.RiskAssessment, sessionID string) (bool, string) {
	if f.onAsk != nil {
		approved, err := f.onAsk(toolName, risk)
		if err != nil {
			return false, fmt.Sprintf("user confirmation failed: %v", err)
		}
		if approved {
			if sessionID != "" {
				f.approveForSession(sessionID, toolName)
			}
			return true, ""
		}
		return false, fmt.Sprintf("user denied execution: %s", toolName)
	}
	return true, ""
}

func securityCallHasFullControl(ctx *SecurityCallContext) bool {
	return ctx != nil && ctx.FullControl
}

// securityApprovalSessionID keeps a "remember this tool" grant inside one task.
// Bash and most host tools omit session_id, so the raw id collapses to "local"
// and one approval would unlock the same tool in every other chat.
func securityApprovalSessionID(rawSessionID, ownerID string) string {
	rawSessionID = strings.TrimSpace(rawSessionID)
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return rawSessionID
	}
	if rawSessionID == "" || rawSessionID == "local" {
		return "owner:" + ownerID
	}
	return "owner:" + ownerID + "|session:" + rawSessionID
}

// securityActionIsHardDeny is a block, not a confirmation. Standard mode turns
// PolicyDeny into an ask; strict mode keeps it as a deny.
func securityActionIsHardDeny(action security.PolicyAction, mode string) bool {
	if action != security.PolicyDeny {
		return false
	}
	switch mode {
	case "developer", "relaxed", "standard":
		return false
	default:
		return true
	}
}

// stampFullControl copies the tier the permission button is actually showing.
// A pure-coding tab uses the sticky session tier, so "请求授权" still prompts
// and a sticky full grant skips prompts even before the global flag is set.
// Every other chat follows the global 「完全控制」 switch alone; a leftover
// coding "request" record must not hide that switch. Hard denies stay.
func (h *IMMessageHandler) stampFullControl(ctx *SecurityCallContext) {
	if h == nil || ctx == nil {
		return
	}
	global := h.app != nil && h.app.isSubAgentFullAccessGranted()
	userID := strings.TrimSpace(ctx.UserID)
	if userID != "" && h.isPureCodingWorkbenchSession(userID) {
		ctx.FullControl = h.stickyCodingEffectiveFullAccess(userID, global)
		return
	}
	ctx.FullControl = global
}

func sessionIDFromSecurityContext(ctx *SecurityCallContext) string {
	if ctx == nil {
		return ""
	}
	return strings.TrimSpace(ctx.SessionID)
}

func userIDFromSecurityContext(ctx *SecurityCallContext) string {
	if ctx == nil {
		return ""
	}
	return strings.TrimSpace(ctx.UserID)
}

func (f *SecurityFirewall) recordAudit(toolName string, args map[string]interface{}, risk security.RiskAssessment, action security.PolicyAction, result, sessionID, userID string) {
	if f.audit == nil {
		return
	}
	if result == "" {
		result = string(action)
	}
	_ = f.audit.Log(security.AuditEntry{
		Timestamp:    time.Now(),
		SessionID:    strings.TrimSpace(sessionID),
		UserID:       strings.TrimSpace(userID),
		ToolName:     toolName,
		Arguments:    args,
		RiskLevel:    risk.Level,
		PolicyAction: action,
		Result:       result,
	})
}

func (f *SecurityFirewall) isSessionApproved(sessionID, toolName string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	approvals, ok := f.sessionApprovals[sessionID]
	if !ok {
		return false
	}
	// Exact tool name, or an explicit "*" grant. A shorter name must not
	// unlock a longer one: "sh" is not bash, and "database" is not database_query.
	return approvals[toolName] || approvals["*"]
}

func (f *SecurityFirewall) approveForSession(sessionID, toolName string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sessionApprovals[sessionID] == nil {
		f.sessionApprovals[sessionID] = make(map[string]bool)
	}
	f.sessionApprovals[sessionID][toolName] = true
}

// ApproveForSession explicitly approves a tool pattern for a session.
func (f *SecurityFirewall) ApproveForSession(sessionID, toolPattern string) {
	f.approveForSession(sessionID, toolPattern)
}

// ClearSession removes all session-level approvals for a session.
// Call this when a session ends to prevent unbounded memory growth.
func (f *SecurityFirewall) ClearSession(sessionID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessionApprovals, sessionID)
}

// LoadProjectPolicy loads project-level security policy from a file.
func (f *SecurityFirewall) LoadProjectPolicy(projectPath string) error {
	if f.policy == nil {
		return nil
	}
	policyPath := projectPath + "/.maclaw/security-policy.json"
	return f.policy.LoadRules(policyPath)
}
