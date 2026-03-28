package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/corelib/security"
)

// SecurityCheckRequest is the universal input format for the security-check command.
type SecurityCheckRequest struct {
	ToolName    string                 `json:"tool_name"`
	ToolInput   map[string]interface{} `json:"tool_input,omitempty"`
	SessionID   string                 `json:"session_id,omitempty"`
	Source      string                 `json:"source,omitempty"`
	ProjectPath string                 `json:"project_path,omitempty"`
}

// SecurityCheckResult is the output format for the security-check command.
type SecurityCheckResult struct {
	Allowed     bool     `json:"allowed"`
	RiskLevel   string   `json:"risk_level"`
	Reason      string   `json:"reason,omitempty"`
	Factors     []string `json:"factors,omitempty"`
	ModeUpgrade string   `json:"mode_upgrade,omitempty"`
}

// runSecurityCheck executes the security-check subcommand logic.
// It returns an exit code: 0 for allow, 2 for deny.
func runSecurityCheck(mode, projectPath string) int {
	// Read all stdin
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "maclaw security-check: failed to read stdin: %v\n", err)
		return 0
	}

	// Empty stdin — exit 0 with usage hint
	if len(data) == 0 {
		fmt.Fprintln(os.Stderr, "maclaw security-check: empty stdin, nothing to check")
		return 0
	}

	// Parse JSON — try SecurityCheckRequest first, fall back to Claude Code Hook_Input
	var req SecurityCheckRequest
	if err := json.Unmarshal(data, &req); err != nil {
		fmt.Fprintf(os.Stderr, "maclaw security-check: JSON parse error: %v\n", err)
		return 0
	}

	// If tool_name is empty, try converting from Claude Code Hook_Input format
	if req.ToolName == "" {
		if converted, err := convertClaudeHookInput(data); err == nil && converted.ToolName != "" {
			req = *converted
		}
	}

	// Validate required field
	if req.ToolName == "" {
		fmt.Fprintln(os.Stderr, "maclaw security-check: missing required field: tool_name")
		return 0
	}

	// Use projectPath from flag, fall back to request field
	if projectPath == "" {
		projectPath = req.ProjectPath
	}

	// Initialize security components
	analyzer := security.NewRiskAnalyzer()
	policy := security.NewPolicyEngineWithMode(mode)

	auditDir := defaultAuditDir()
	audit, err := security.NewAuditLog(auditDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "maclaw security-check: audit log init warning: %v\n", err)
		// Continue without audit
	}

	fw := security.NewFirewall(analyzer, policy, audit)

	// Load project-level policy if available
	if projectPath != "" {
		if err := fw.LoadProjectPolicy(projectPath); err != nil {
			// Non-fatal: use default policy
			fmt.Fprintf(os.Stderr, "maclaw security-check: project policy load warning: %v\n", err)
		}
	}

	// Set onAsk callback based on security mode BEFORE calling Check
	var policyAskTriggered bool
	switch mode {
	case "strict":
		fw.SetOnAsk(func(toolName string, risk security.RiskAssessment) (bool, error) {
			policyAskTriggered = true
			return false, nil // deny
		})
	case "relaxed":
		fw.SetOnAsk(func(toolName string, risk security.RiskAssessment) (bool, error) {
			policyAskTriggered = true
			return true, nil // allow silently
		})
	default: // "standard"
		fw.SetOnAsk(func(toolName string, risk security.RiskAssessment) (bool, error) {
			policyAskTriggered = true
			return true, nil // allow with warning
		})
	}

	// Perform risk assessment for session state tracking
	callCtx := &security.CallContext{SessionID: req.SessionID}
	riskAssessment := analyzer.Assess(req.ToolName, req.ToolInput, callCtx)

	// Call Firewall.Check
	allowed, reason := fw.Check(req.ToolName, req.ToolInput, callCtx)

	// Load and update session state
	var modeUpgrade string
	originalMode := mode // save before potential upgrade
	if req.SessionID != "" {
		state, _ := security.LoadSessionState(req.SessionID)
		state.IncrementToolCall()

		if riskAssessment.Level == security.RiskHigh || riskAssessment.Level == security.RiskCritical {
			if state.IncrementHighRisk() {
				modeUpgrade = "strict"
				// Update mode for this check if upgraded
				mode = "strict"
			}
		}

		_ = state.Save()
	}

	// Build result
	result := SecurityCheckResult{
		Allowed:   allowed,
		RiskLevel: string(riskAssessment.Level),
		Factors:   riskAssessment.Factors,
	}

	if allowed {
		if policyAskTriggered && originalMode == "standard" {
			// Standard mode PolicyAsk: allow with risk warning on stdout
			result.Reason = fmt.Sprintf("⚠️ 风险提示: %s (风险等级: %s, 原因: %s)",
				req.ToolName, riskAssessment.Level, riskAssessment.Reason)
		}

		if modeUpgrade != "" {
			result.ModeUpgrade = modeUpgrade
			result.Reason = appendReason(result.Reason,
				fmt.Sprintf("🔒 安全模式已自动升级为 strict（5分钟内累积高风险操作）"))
		}

		out, _ := json.Marshal(result)
		fmt.Fprintln(os.Stdout, string(out))
		return 0
	}

	// Denied
	result.Reason = reason
	if modeUpgrade != "" {
		result.ModeUpgrade = modeUpgrade
	}
	fmt.Fprintln(os.Stderr, reason)
	return 2
}

// defaultAuditDir returns the default audit log directory (~/.maclaw/audit/).
func defaultAuditDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "maclaw-audit")
	}
	return filepath.Join(home, ".maclaw", "audit")
}

// appendReason appends additional info to an existing reason string.
func appendReason(existing, addition string) string {
	if existing == "" {
		return addition
	}
	return existing + " | " + addition
}

// claudeHookInput represents the JSON format that Claude Code passes to hooks
// via stdin. Fields like hook_type are ignored; tool_name, tool_input, and
// session_id are transparently mapped to SecurityCheckRequest.
type claudeHookInput struct {
	HookType  string                 `json:"hook_type"`
	ToolName  string                 `json:"tool_name"`
	ToolInput map[string]interface{} `json:"tool_input,omitempty"`
	SessionID string                 `json:"session_id,omitempty"`
}

// convertClaudeHookInput attempts to parse raw JSON as a Claude Code Hook_Input
// and convert it to a SecurityCheckRequest. Returns an error if parsing fails.
func convertClaudeHookInput(raw json.RawMessage) (*SecurityCheckRequest, error) {
	var input claudeHookInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	if input.ToolName == "" {
		return nil, fmt.Errorf("no tool_name in hook input")
	}
	return &SecurityCheckRequest{
		ToolName:  input.ToolName,
		ToolInput: input.ToolInput,
		SessionID: input.SessionID,
		Source:    "claude-code",
	}, nil
}
