package security

// PolicyType enumerates the kinds of security policies.
const (
	PolicyTypeKeywordBlock  = "keyword_block"  // block messages containing keywords
	PolicyTypeRateLimit     = "rate_limit"      // limit request frequency
	PolicyTypeModelRestrict = "model_restrict"  // restrict certain models for roles
	PolicyTypeContentFilter = "content_filter"  // filter sensitive content
)

// Policy represents a security rule.
type Policy struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	PolicyType  string   `json:"policy_type"`
	Description string   `json:"description"`
	Rules       string   `json:"rules"`       // JSON-encoded rule config
	Scope       string   `json:"scope"`       // all, role:<code>, colleague:<id>
	Priority    int      `json:"priority"`    // higher = checked first
	Status      string   `json:"status"`      // active, disabled
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

// HitRecord logs when a policy is triggered.
type HitRecord struct {
	ID         string `json:"id"`
	PolicyID   string `json:"policy_id"`
	PolicyName string `json:"policy_name"`
	ActorID    string `json:"actor_id"`
	Action     string `json:"action"` // blocked, warned, logged
	Detail     string `json:"detail"`
	CreatedAt  string `json:"created_at"`
}

// CheckInput is the context for a security check.
type CheckInput struct {
	Content     string `json:"content"`
	RoleCode    string `json:"role_code"`
	ColleagueID string `json:"colleague_id"`
	Model       string `json:"model"`
	RequestType string `json:"request_type"` // chat, collaboration, workflow
}

// CheckResult is the outcome of a security check.
type CheckResult struct {
	Allowed    bool   `json:"allowed"`
	PolicyID   string `json:"policy_id,omitempty"`
	PolicyName string `json:"policy_name,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// KeywordBlockRules is the config for keyword_block policies.
type KeywordBlockRules struct {
	Keywords []string `json:"keywords"`
	Action   string   `json:"action"` // block, warn, log
}

// RateLimitRules is the config for rate_limit policies.
type RateLimitRules struct {
	MaxRequests int `json:"max_requests"`
	WindowSecs  int `json:"window_secs"`
}
