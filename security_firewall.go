package main

import (
	"fmt"
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
	onAsk    func(toolName string, risk RiskAssessment) (bool, error)

	// Session-level approvals: sessionID → set of approved tool patterns.
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
func (f *SecurityFirewall) SetOnAsk(fn func(toolName string, risk RiskAssessment) (bool, error)) {
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

	// 2. Check session-level approvals.
	sessionID := ""
	if ctx != nil {
		sessionID = ctx.SessionID
	}
	if sessionID != "" && f.isSessionApproved(sessionID, toolName) {
		// Already approved for this session — allow but audit.
		f.recordAudit(toolName, args, risk, PolicyAudit, "session_approved", sessionID)
		return true, ""
	}

	// 3. Policy decision.
	action := PolicyAllow
	if f.policy != nil {
		action = f.policy.Evaluate(toolName, args, risk.Level)
	}

	// 4. Record audit.
	f.recordAudit(toolName, args, risk, action, "", sessionID)

	// 5. Execute decision.
	switch action {
	case PolicyAllow:
		return true, ""
	case PolicyAudit:
		return true, ""
	case PolicyDeny:
		return false, fmt.Sprintf("⛔ 安全策略拒绝: %s (风险等级: %s, 原因: %s)", toolName, risk.Level, risk.Reason)
	case PolicyAsk:
		if f.onAsk != nil {
			approved, err := f.onAsk(toolName, risk)
			if err != nil {
				return false, fmt.Sprintf("⛔ 用户确认失败: %v", err)
			}
			if approved {
				// Record approval for this session.
				if sessionID != "" {
					f.approveForSession(sessionID, toolName)
				}
				return true, ""
			}
			return false, fmt.Sprintf("⛔ 用户拒绝执行: %s", toolName)
		}
		// No onAsk callback — default to allow with warning for medium, deny for high/critical.
		if risk.Level == RiskHigh || risk.Level == RiskCritical {
			return false, fmt.Sprintf("⚠️ 高风险操作需要确认但无确认通道: %s (风险: %s, 原因: %s)", toolName, risk.Level, risk.Reason)
		}
		return true, ""
	default:
		return true, ""
	}
}

func (f *SecurityFirewall) recordAudit(toolName string, args map[string]interface{}, risk RiskAssessment, action PolicyAction, result, sessionID string) {
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
	// Check prefix match (e.g., "bash" matches "bash").
	for pattern := range approvals {
		if strings.Contains(toolName, pattern) {
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

// LoadProjectPolicy loads project-level security policy from a file.
func (f *SecurityFirewall) LoadProjectPolicy(projectPath string) error {
	if f.policy == nil {
		return nil
	}
	policyPath := projectPath + "/.maclaw/security-policy.json"
	return f.policy.LoadRules(policyPath)
}
