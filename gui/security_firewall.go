package main

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
		f.recordAudit(toolName, args, risk, security.PolicyAllow, "developer_mode_allowed", sessionIDFromSecurityContext(ctx))
		return true, ""
	}

	// 2. Check session-level approvals.
	sessionID := ""
	if ctx != nil {
		sessionID = ctx.SessionID
	}
	if sessionID != "" && f.isSessionApproved(sessionID, toolName) {
		// Already approved for this session - allow but audit.
		f.recordAudit(toolName, args, risk, security.PolicyAudit, "session_approved", sessionID)
		return true, ""
	}

	// 3. Policy decision.
	action := security.PolicyAllow
	if f.policy != nil {
		action = f.policy.Evaluate(toolName, args, risk.Level)
	}

	// 4. Record audit.
	f.recordAudit(toolName, args, risk, action, "", sessionID)

	// 5. Execute decision.
	switch action {
	case security.PolicyAllow:
		return true, ""
	case security.PolicyAudit:
		return true, ""
	case security.PolicyDeny:
		if mode == "developer" || mode == "relaxed" {
			return true, ""
		}
		if mode == "standard" {
			return f.confirmOrAllowWithoutChannel(toolName, risk, sessionID)
		}
		return false, fmt.Sprintf("鐎瑰鍙忕粵鏍殣閹锋帞绮? %s (妞嬪酣娅撶粵澶岄獓: %s, 閸樼喎娲? %s)", toolName, risk.Level, risk.Reason)
	case security.PolicyAsk:
		if mode == "developer" || mode == "relaxed" {
			return true, ""
		}
		return f.confirmOrAllowWithoutChannel(toolName, risk, sessionID)
	default:
		return true, ""
	}
}

func (f *SecurityFirewall) confirmOrAllowWithoutChannel(toolName string, risk security.RiskAssessment, sessionID string) (bool, string) {
	if f.onAsk != nil {
		approved, err := f.onAsk(toolName, risk)
		if err != nil {
			return false, fmt.Sprintf("閻劍鍩涚涵顔款吇婢惰精瑙? %v", err)
		}
		if approved {
			if sessionID != "" {
				f.approveForSession(sessionID, toolName)
			}
			return true, ""
		}
		return false, fmt.Sprintf("閻劍鍩涢幏鎺旂卜閹笛嗩攽: %s", toolName)
	}
	return true, ""
}

func sessionIDFromSecurityContext(ctx *SecurityCallContext) string {
	if ctx == nil {
		return ""
	}
	return ctx.SessionID
}
func (f *SecurityFirewall) recordAudit(toolName string, args map[string]interface{}, risk security.RiskAssessment, action security.PolicyAction, result, sessionID string) {
	if f.audit == nil {
		return
	}
	if result == "" {
		result = string(action)
	}
	_ = f.audit.Log(security.AuditEntry{
		Timestamp:    time.Now(),
		SessionID:    sessionID,
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
	// Check exact match or wildcard.
	if approvals[toolName] || approvals["*"] {
		return true
	}
	// Check prefix match - skip empty patterns to avoid matching everything.
	for pattern := range approvals {
		if pattern != "" && pattern != toolName && strings.Contains(toolName, pattern) {
			return true
		}
	}
	return false
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
