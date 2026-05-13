package security

import "time"

// SecurityGroup represents a user group in the hierarchy.
type SecurityGroup struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ParentID  string    `json:"parent_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// GroupTreeNode is the tree representation returned by the API.
type GroupTreeNode struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	ParentID    string           `json:"parent_id"`
	MemberCount int              `json:"member_count"`
	Children    []*GroupTreeNode `json:"children,omitempty"`
}

// GroupPolicyView shows a group's policy with inheritance info.
type GroupPolicyView struct {
	GroupID string                    `json:"group_id"`
	Items   map[string]PolicyItemView `json:"items"`
}

// PolicyItemView is a single policy field with source info.
type PolicyItemView struct {
	Value       interface{} `json:"value"`
	Source      string      `json:"source"`       // "self" or "inherited"
	SourceGroup string      `json:"source_group"`
	SourceName  string      `json:"source_name"`
}

// EffectivePolicy is the resolved policy for a user.
type EffectivePolicy struct {
	FileOutboundEnabled  bool     `json:"file_outbound_enabled"`
	ImageOutboundEnabled bool     `json:"image_outbound_enabled"`
	GossipEnabled        bool     `json:"gossip_enabled"`
	GuardrailMode        string   `json:"guardrail_mode"`
	SandboxMode          string   `json:"sandbox_mode"`
	NetworkLevel         string   `json:"network_level"`
	YoloModeAllowed      bool     `json:"yolo_mode_allowed"`
	SmartRouteEnabled    bool     `json:"smart_route_enabled"`
	SkillSourcesAllowed  []string `json:"skill_sources_allowed,omitempty"` // nil/empty = all allowed; values: "skillhub","clawhub","github"
}

// DefaultEffectivePolicy is the root group default.
var DefaultEffectivePolicy = EffectivePolicy{
	FileOutboundEnabled:  true,
	ImageOutboundEnabled: true,
	GossipEnabled:        true,
	GuardrailMode:        "standard",
	SandboxMode:          "none",
	NetworkLevel:         "full",
	YoloModeAllowed:      true,
	SmartRouteEnabled:    true,
}

// SecuritySettings holds system-level security toggles.
type SecuritySettings struct {
	CentralizedSecurityEnabled bool   `json:"centralized_security_enabled"`
	OrgStructureEnabled        bool   `json:"org_structure_enabled"`
	DefaultGroupID             string `json:"default_group_id,omitempty"`
}
