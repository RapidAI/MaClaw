package security

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Firewall integrates RiskAnalyzer + PolicyEngine + AuditLog to provide
// a unified security check before tool execution.
type Firewall struct {
	analyzer *RiskAnalyzer
	policy   *PolicyEngine
	audit    *AuditLog
	onAsk    func(toolName string, risk RiskAssessment) (bool, error)

	// Smart Approval: LLM-based false positive detection
	smartApproval *SmartApproval

	// Enhanced session allowlist with category-based matching
	allowlist *SessionAllowlist

	// Async approval manager for IM scenarios
	approvalMgr *ApprovalManager

	// Legacy session approvals (kept for backward compatibility)
	sessionApprovals map[string]map[string]bool
	mu               sync.RWMutex
}

// NewFirewall creates a firewall combining the three security components.
func NewFirewall(analyzer *RiskAnalyzer, policy *PolicyEngine, audit *AuditLog) *Firewall {
	return &Firewall{
		analyzer:         analyzer,
		policy:           policy,
		audit:            audit,
		allowlist:        NewSessionAllowlist(),
		sessionApprovals: make(map[string]map[string]bool),
	}
}

// SetSmartApproval configures the LLM-based smart approval bypass.
func (f *Firewall) SetSmartApproval(sa *SmartApproval) {
	f.smartApproval = sa
}

// SetApprovalManager configures the async approval manager for IM scenarios.
func (f *Firewall) SetApprovalManager(am *ApprovalManager) {
	f.approvalMgr = am
}

// Allowlist returns the session allowlist for external access.
func (f *Firewall) Allowlist() *SessionAllowlist {
	return f.allowlist
}

// SetOnAsk sets the callback for user confirmation when policy action is "ask".
func (f *Firewall) SetOnAsk(fn func(toolName string, risk RiskAssessment) (bool, error)) {
	f.onAsk = fn
}

// Check performs a security check before tool execution.
func (f *Firewall) Check(toolName string, args map[string]interface{}, ctx *CallContext) (bool, string) {
	if f.analyzer == nil {
		return true, ""
	}

	// Developer mode: bypass all security checks unconditionally.
	// Risk assessment is skipped entirely — no deny, no ask, no audit.
	if f.policy != nil && f.policy.IsDeveloperMode() {
		return true, ""
	}

	risk := f.analyzer.Assess(toolName, args, ctx)

	sessionID := ""
	if ctx != nil {
		sessionID = ctx.SessionID
	}

	// Check enhanced session allowlist (category-aware)
	if sessionID != "" && f.allowlist != nil {
		var categories []string
		if len(risk.Factors) > 0 {
			categories = CategoriesForAssessment(risk, f.analyzer)
		}
		if f.allowlist.IsApproved(sessionID, toolName, categories) {
			f.recordAudit(toolName, args, risk, PolicyAudit, "session_allowlist", sessionID)
			return true, ""
		}
	}

	// Legacy session approval check (backward compatibility)
	if sessionID != "" && f.isSessionApproved(sessionID, toolName) {
		f.recordAudit(toolName, args, risk, PolicyAudit, "session_approved", sessionID)
		return true, ""
	}

	action := PolicyAllow
	if f.policy != nil {
		action = f.policy.Evaluate(toolName, args, risk.Level)
	}

	f.recordAudit(toolName, args, risk, action, "", sessionID)

	switch action {
	case PolicyAllow, PolicyAudit:
		return true, ""
	case PolicyDeny:
		return false, fmt.Sprintf("⛔ 安全策略拒绝: %s (风险等级: %s, 原因: %s)", toolName, risk.Level, risk.Reason)
	case PolicyAsk:
		return f.handleAskAction(toolName, args, risk, sessionID)
	default:
		return true, ""
	}
}

// handleAskAction processes the PolicyAsk action with Smart Approval bypass
// and enhanced approval flow.
func (f *Firewall) handleAskAction(toolName string, args map[string]interface{}, risk RiskAssessment, sessionID string) (bool, string) {
	// Step 1: Try Smart Approval — let LLM determine if this is a false positive
	if f.smartApproval != nil && f.smartApproval.IsConfigured() {
		result := f.smartApproval.Evaluate(toolName, args, risk)
		switch result.Verdict {
		case SmartVerdictSafe:
			log.Printf("[firewall] smart approval: %s deemed safe (%s) in %v",
				toolName, result.Explanation, result.Elapsed)
			f.recordAudit(toolName, args, risk, PolicyAudit, "smart_approved", sessionID)
			return true, ""
		case SmartVerdictUnsafe:
			log.Printf("[firewall] smart approval: %s confirmed unsafe (%s)",
				toolName, result.Explanation)
			// Fall through to user confirmation
		default:
			log.Printf("[firewall] smart approval: %s unknown (%s), falling back to user",
				toolName, result.Explanation)
		}
	}

	// Step 2: Try sync onAsk callback (CLI/GUI).
	// In sync mode we only record tool-level approval (not category),
	// because the CLI/GUI prompt is per-invocation — the user approves
	// this specific tool, not a whole category.
	if f.onAsk != nil {
		approved, err := f.onAsk(toolName, risk)
		if err != nil {
			return false, fmt.Sprintf("⛔ 用户确认失败: %v", err)
		}
		if approved {
			f.recordSessionApproval(sessionID, toolName, risk)
			return true, ""
		}
		return false, fmt.Sprintf("⛔ 用户拒绝执行: %s", toolName)
	}

	// Step 3: Try async approval manager (IM scenarios)
	if f.approvalMgr != nil {
		var categories []string
		if len(risk.Factors) > 0 {
			categories = CategoriesForAssessment(risk, f.analyzer)
		}
		req := ApprovalRequest{
			SessionID:  sessionID,
			ToolName:   toolName,
			Args:       args,
			Risk:       risk,
			Categories: categories,
		}
		resp, err := f.approvalMgr.RequestApproval(req)
		if err != nil {
			return false, fmt.Sprintf("⛔ IM 审批失败: %v", err)
		}
		if resp.Approved {
			f.applyApprovalScope(sessionID, toolName, risk, resp.ApproveScope)
			return true, ""
		}
		return false, fmt.Sprintf("⛔ 用户拒绝执行: %s", toolName)
	}

	// No confirmation channel available
	if risk.Level == RiskHigh || risk.Level == RiskCritical {
		return false, fmt.Sprintf("⚠️ 高风险操作需要确认但无确认通道: %s (风险: %s, 原因: %s)", toolName, risk.Level, risk.Reason)
	}
	return true, ""
}

// recordSessionApproval records an approval in both the legacy and enhanced allowlists.
func (f *Firewall) recordSessionApproval(sessionID, toolName string, risk RiskAssessment) {
	if sessionID == "" {
		return
	}
	// Legacy
	f.approveForSession(sessionID, toolName)
	// Enhanced: approve by tool name with no TTL (session lifetime)
	if f.allowlist != nil {
		f.allowlist.ApproveTool(sessionID, toolName, 0)
	}
}

// applyApprovalScope applies the user's chosen approval scope from IM responses.
func (f *Firewall) applyApprovalScope(sessionID, toolName string, risk RiskAssessment, scope string) {
	if sessionID == "" {
		return
	}
	switch scope {
	case "category":
		if len(risk.Factors) > 0 {
			categories := CategoriesForAssessment(risk, f.analyzer)
			for _, cat := range categories {
				if f.allowlist != nil {
					f.allowlist.ApproveCategory(sessionID, cat, 0)
				}
			}
		}
		// Also approve the specific tool
		f.recordSessionApproval(sessionID, toolName, risk)
	case "session":
		if f.allowlist != nil {
			f.allowlist.ApproveAll(sessionID, 0)
		}
	default: // "once" or "tool"
		f.recordSessionApproval(sessionID, toolName, risk)
	}
}

func (f *Firewall) recordAudit(toolName string, args map[string]interface{}, risk RiskAssessment, action PolicyAction, result, sessionID string) {
	if f.audit == nil {
		return
	}
	if result == "" {
		result = string(action)
	}
	_ = f.audit.Log(AuditEntry{
		Timestamp:    time.Now(),
		SessionID:    sessionID,
		ToolName:     toolName,
		Arguments:    args,
		RiskLevel:    risk.Level,
		PolicyAction: action,
		Result:       result,
	})
}

func (f *Firewall) isSessionApproved(sessionID, toolName string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	approvals, ok := f.sessionApprovals[sessionID]
	if !ok {
		return false
	}
	if approvals[toolName] || approvals["*"] {
		return true
	}
	for pattern := range approvals {
		if pattern != "" && pattern != toolName && strings.Contains(toolName, pattern) {
			return true
		}
	}
	return false
}

func (f *Firewall) approveForSession(sessionID, toolName string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sessionApprovals[sessionID] == nil {
		f.sessionApprovals[sessionID] = make(map[string]bool)
	}
	f.sessionApprovals[sessionID][toolName] = true
}

// ApproveForSession explicitly approves a tool pattern for a session.
func (f *Firewall) ApproveForSession(sessionID, toolPattern string) {
	f.approveForSession(sessionID, toolPattern)
}

// ClearSession removes all session-level approvals for a session.
func (f *Firewall) ClearSession(sessionID string) {
	f.mu.Lock()
	delete(f.sessionApprovals, sessionID)
	f.mu.Unlock()
	// Also clear enhanced allowlist
	if f.allowlist != nil {
		f.allowlist.ClearSession(sessionID)
	}
}

// LoadProjectPolicy loads project-level security policy from a file.
func (f *Firewall) LoadProjectPolicy(projectPath string) error {
	if f.policy == nil {
		return nil
	}
	policyPath := projectPath + "/.maclaw/security-policy.json"
	return f.policy.LoadRules(policyPath)
}
