package main

import (
	"context"
	"fmt"
	"time"
)

// VEApprovalConfig holds the VE's approval capability configuration.
type VEApprovalConfig struct {
	Enabled          bool              `json:"enabled"`
	ACL              AccessControlList `json:"acl"`
	Rules            ApprovalRules     `json:"rules"`
	MaxQueueSize     int               `json:"max_queue_size"`
	TimeoutHours     int               `json:"timeout_hours"`
	DailyQuota       int               `json:"daily_quota"`
	FallbackApprover string            `json:"fallback_approver,omitempty"`
}

// AccessControlList defines who can submit requests to this VE.
type AccessControlList struct {
	Mode        ACLMode  `json:"mode"`
	Departments []string `json:"departments,omitempty"`
	Roles       []string `json:"roles,omitempty"`
	Skills      []string `json:"skills,omitempty"`
	Entities    []string `json:"entities,omitempty"`
}

type ACLMode string

const (
	ACLWhitelist ACLMode = "whitelist"
	ACLBlacklist ACLMode = "blacklist"
)

const (
	MaxACLEntriesPerList     = 500
	MaxACLEntriesPerCategory = 100
)

func DefaultVEApprovalConfig() VEApprovalConfig {
	return VEApprovalConfig{
		Enabled:      false,
		ACL:          AccessControlList{Mode: ACLWhitelist},
		Rules:        ApprovalRules{AutoReject: []ApprovalRule{}, AutoApprove: []ApprovalRule{}, RequireHuman: []ApprovalRule{}},
		MaxQueueSize: 50,
		TimeoutHours: 24,
		DailyQuota:   100,
	}
}

func (c *VEApprovalConfig) Validate() error {
	if err := c.ACL.Validate(); err != nil {
		return fmt.Errorf("acl: %w", err)
	}
	if c.MaxQueueSize < 1 || c.MaxQueueSize > 1000 {
		return fmt.Errorf("max_queue_size must be between 1 and 1000, got %d", c.MaxQueueSize)
	}
	if c.TimeoutHours < 1 || c.TimeoutHours > 720 {
		return fmt.Errorf("timeout_hours must be between 1 and 720, got %d", c.TimeoutHours)
	}
	if c.DailyQuota < 1 || c.DailyQuota > 10000 {
		return fmt.Errorf("daily_quota must be between 1 and 10000, got %d", c.DailyQuota)
	}
	return nil
}

func (acl *AccessControlList) Validate() error {
	if acl.Mode != ACLWhitelist && acl.Mode != ACLBlacklist {
		return fmt.Errorf("mode must be %q or %q, got %q", ACLWhitelist, ACLBlacklist, acl.Mode)
	}
	if len(acl.Departments) > MaxACLEntriesPerCategory {
		return fmt.Errorf("departments exceeds maximum of %d entries (got %d)", MaxACLEntriesPerCategory, len(acl.Departments))
	}
	if len(acl.Roles) > MaxACLEntriesPerCategory {
		return fmt.Errorf("roles exceeds maximum of %d entries (got %d)", MaxACLEntriesPerCategory, len(acl.Roles))
	}
	if len(acl.Skills) > MaxACLEntriesPerCategory {
		return fmt.Errorf("skills exceeds maximum of %d entries (got %d)", MaxACLEntriesPerCategory, len(acl.Skills))
	}
	if len(acl.Entities) > MaxACLEntriesPerCategory {
		return fmt.Errorf("entities exceeds maximum of %d entries (got %d)", MaxACLEntriesPerCategory, len(acl.Entities))
	}
	total := len(acl.Departments) + len(acl.Roles) + len(acl.Skills) + len(acl.Entities)
	if total > MaxACLEntriesPerList {
		return fmt.Errorf("total ACL entries exceeds maximum of %d (got %d)", MaxACLEntriesPerList, total)
	}
	return nil
}

func (acl *AccessControlList) TotalEntries() int {
	return len(acl.Departments) + len(acl.Roles) + len(acl.Skills) + len(acl.Entities)
}

// ApprovalDecision is the structured result returned by HandleApprovalRequest.
type ApprovalDecision struct {
	Decision    RoutingDecision `json:"decision"`
	Rationale   string          `json:"rationale,omitempty"`
	MatchedRule *ApprovalRule   `json:"matched_rule,omitempty"`
	DecidedAt   time.Time       `json:"decided_at"`
}

// VEApprovalHandler processes incoming approval requests on the desktop VE.
type VEApprovalHandler struct {
	config     *VEApprovalConfig
	ruleEngine *ApprovalRuleEngine
	queue      *ApprovalQueue
}

func NewVEApprovalHandler(config *VEApprovalConfig) *VEApprovalHandler {
	var q *ApprovalQueue
	if config != nil {
		q = NewApprovalQueueFromConfig(config)
	}
	return &VEApprovalHandler{config: config, ruleEngine: &ApprovalRuleEngine{}, queue: q}
}

var (
	ErrCapabilityDisabled = fmt.Errorf("capability disabled")
	ErrACLDenied          = fmt.Errorf("requester not permitted by access control list")
	ErrQueueFull          = fmt.Errorf("queue full")
	ErrDailyQuotaExceeded = fmt.Errorf("daily quota exceeded")
)

// HandleApprovalRequest processes an incoming approval request.
func (h *VEApprovalHandler) HandleApprovalRequest(ctx context.Context, req *VEApprovalRequest) (*ApprovalDecision, error) {
	if h.config == nil || !h.config.Enabled {
		return nil, ErrCapabilityDisabled
	}
	if !h.evaluateACL(req.RequesterID, req.RequesterDepartment, req.RequesterRole, req.RequesterSkills) {
		return nil, ErrACLDenied
	}
	if h.queue != nil {
		result := h.queue.Submit(req.ID)
		if !result.Accepted {
			if result.RejectionReason == "daily quota exceeded" {
				return nil, ErrDailyQuotaExceeded
			}
			return nil, ErrQueueFull
		}
		defer h.queue.Dequeue(req.ID)
	}
	payload := &ApprovalRequestPayload{Data: req.Payload}
	evalCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	decision, matchedRule, err := h.ruleEngine.Evaluate(evalCtx, &h.config.Rules, payload)
	if err != nil {
		return &ApprovalDecision{
			Decision:  DecisionRequireHuman,
			Rationale: fmt.Sprintf("rule evaluation error: %v", err),
			DecidedAt: time.Now(),
		}, nil
	}
	ad := &ApprovalDecision{Decision: decision, MatchedRule: matchedRule, DecidedAt: time.Now()}
	switch decision {
	case DecisionAutoApprove:
		if matchedRule != nil {
			ad.Rationale = fmt.Sprintf("auto-approved by rule: %s", matchedRule.Name)
		} else {
			ad.Rationale = "auto-approved"
		}
	case DecisionAutoReject:
		if matchedRule != nil && matchedRule.Reason != "" {
			ad.Rationale = matchedRule.Reason
		} else if matchedRule != nil {
			ad.Rationale = fmt.Sprintf("auto-rejected by rule: %s", matchedRule.Name)
		} else {
			ad.Rationale = "auto-rejected"
		}
	case DecisionRequireHuman:
		if matchedRule != nil {
			ad.Rationale = fmt.Sprintf("escalated to human by rule: %s", matchedRule.Name)
		} else {
			ad.Rationale = "no matching rule; escalated to human review"
		}
	}
	return ad, nil
}

func (h *VEApprovalHandler) evaluateACL(requesterID, department, role string, requesterSkills []string) bool {
	acl := &h.config.ACL
	if acl.TotalEntries() == 0 {
		return acl.Mode == ACLBlacklist
	}
	matched := h.aclMatchesRequester(acl, requesterID, department, role, requesterSkills)
	switch acl.Mode {
	case ACLWhitelist:
		return matched
	case ACLBlacklist:
		return !matched
	default:
		return false
	}
}

func (h *VEApprovalHandler) aclMatchesRequester(acl *AccessControlList, requesterID, department, role string, requesterSkills []string) bool {
	for _, entity := range acl.Entities {
		if entity == requesterID {
			return true
		}
	}
	if department != "" {
		for _, dept := range acl.Departments {
			if dept == department {
				return true
			}
		}
	}
	if role != "" {
		for _, r := range acl.Roles {
			if r == role {
				return true
			}
		}
	}
	if len(requesterSkills) > 0 {
		for _, rs := range requesterSkills {
			for _, s := range acl.Skills {
				if s == rs {
					return true
				}
			}
		}
	}
	return false
}

// VEApprovalRequest is the approval request as received by the VE handler.
type VEApprovalRequest struct {
	ID                  string                 `json:"id"`
	RequesterID         string                 `json:"requester_id"`
	RequesterName       string                 `json:"requester_name,omitempty"`
	RequesterDepartment string                 `json:"requester_department,omitempty"`
	RequesterRole       string                 `json:"requester_role,omitempty"`
	RequesterSkills     []string               `json:"requester_skills,omitempty"`
	Payload             map[string]interface{} `json:"payload"`
}
