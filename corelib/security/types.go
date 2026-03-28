package security

import "time"

// RiskLevel represents the assessed risk level of a tool invocation.
type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

// RiskLevelOrder maps each RiskLevel to a numeric value for comparison.
var RiskLevelOrder = map[RiskLevel]int{
	RiskLow:      0,
	RiskMedium:   1,
	RiskHigh:     2,
	RiskCritical: 3,
}

// RiskContext contains contextual information for risk assessment.
type RiskContext struct {
	ToolName       string
	Arguments      map[string]interface{}
	SessionID      string
	ProjectPath    string
	PermissionMode string
	CallCount      int
}

// RiskAssessment is the result of a risk evaluation.
type RiskAssessment struct {
	Level   RiskLevel
	Reason  string
	Factors []string
}

// PolicyAction represents the action a policy rule dictates.
type PolicyAction string

const (
	PolicyAllow PolicyAction = "allow"
	PolicyDeny  PolicyAction = "deny"
	PolicyAsk   PolicyAction = "ask"
	PolicyAudit PolicyAction = "audit"
)

// PolicyRule defines a single policy rule.
type PolicyRule struct {
	Name        string       `json:"name"`
	Priority    int          `json:"priority"`
	ToolPattern string       `json:"tool_pattern"`
	ArgsPattern string       `json:"args_pattern"`
	RiskLevels  []RiskLevel  `json:"risk_levels"`
	Action      PolicyAction `json:"action"`
}

// AuditAction represents the type of auditable action.
type AuditAction string

const (
	AuditActionHubSkillInstall AuditAction = "hub_skill_install"
	AuditActionHubSkillUpdate  AuditAction = "hub_skill_update"
	AuditActionHubSkillReject  AuditAction = "hub_skill_reject"
)

// AuditEntry represents a single audit log record.
type AuditEntry struct {
	Timestamp           time.Time              `json:"timestamp"`
	UserID              string                 `json:"user_id"`
	SessionID           string                 `json:"session_id"`
	Action              AuditAction            `json:"action,omitempty"`
	ToolName            string                 `json:"tool_name"`
	Arguments           map[string]interface{} `json:"arguments"`
	RiskLevel           RiskLevel              `json:"risk_level"`
	PolicyAction        PolicyAction           `json:"policy_action"`
	Result              string                 `json:"result"`
	Source              string                 `json:"source,omitempty"`
	SensitiveDetected   bool                   `json:"sensitive_detected,omitempty"`
	SensitiveCategories []string               `json:"sensitive_categories,omitempty"`
	OutputSnippet       string                 `json:"output_snippet,omitempty"`
}

// AuditFilter defines criteria for querying audit log entries.
type AuditFilter struct {
	StartTime  *time.Time
	EndTime    *time.Time
	Action     AuditAction
	ToolName   string
	RiskLevels []RiskLevel
}

// RiskPattern defines a regex-based risk detection rule.
type RiskPattern struct {
	Name        string    `json:"name"`
	Category    string    `json:"category"`
	ToolMatch   string    `json:"tool_match"`
	ParamMatch  string    `json:"param_match"`
	ParamKey    string    `json:"param_key"`
	Level       RiskLevel `json:"level"`
	Description string    `json:"description"`
}

// CallContext provides context for risk assessment in the firewall.
type CallContext struct {
	UserMessage     string
	SessionID       string
	RecentApprovals []string
}

// LLMSecurityVerdict represents the safety verdict from LLM security review.
type LLMSecurityVerdict string

const (
	VerdictSafe      LLMSecurityVerdict = "safe"
	VerdictRisky     LLMSecurityVerdict = "risky"
	VerdictDangerous LLMSecurityVerdict = "dangerous"
)
