package security

import (
	"context"
	"time"
)

// SecurityGroup 用户组
type SecurityGroup struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ParentID  string    `json:"parent_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// GroupTreeNode 树形节点（API 返回用）
type GroupTreeNode struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	ParentID    string           `json:"parent_id"`
	MemberCount int              `json:"member_count"`
	HasChildren bool             `json:"has_children,omitempty"`
	Children    []*GroupTreeNode `json:"children,omitempty"`
}

// EffectivePolicy 生效策略
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

// DefaultPolicy 根组默认策略
var DefaultPolicy = EffectivePolicy{
	FileOutboundEnabled:  true,
	ImageOutboundEnabled: true,
	GossipEnabled:        true,
	GuardrailMode:        "standard",
	SandboxMode:          "none",
	NetworkLevel:         "full",
	YoloModeAllowed:      true,
	SmartRouteEnabled:    true,
	SkillSourcesAllowed:  nil, // nil = all sources allowed (skillhub, clawhub, github)
}

// AllSkillSources re-exports the canonical list from corelib/skill.
// Kept here for admin UI reference when displaying policy options.
var AllSkillSources = []string{"skillhub", "clawhub", "github"}

// GroupPolicyView 组策略视图（含继承信息）
type GroupPolicyView struct {
	GroupID string                    `json:"group_id"`
	Items   map[string]PolicyItemView `json:"items"`
}

// PolicyItemView 单个策略项视图
type PolicyItemView struct {
	Value       interface{} `json:"value"`
	Source      string      `json:"source"`       // "self" 或 "inherited"
	SourceGroup string      `json:"source_group"` // 来源组 ID
	SourceName  string      `json:"source_name"`  // 来源组名称
}

// SecuritySettings 系统安全设置
type SecuritySettings struct {
	CentralizedSecurityEnabled bool   `json:"centralized_security_enabled"`
	OrgStructureEnabled        bool   `json:"org_structure_enabled"`
	DefaultGroupID             string `json:"default_group_id,omitempty"`
}

// HeartbeatSecurityPayload 心跳下发的安全策略
type HeartbeatSecurityPayload struct {
	CentralizedSecurity bool             `json:"centralized_security"`
	Policy              *EffectivePolicy `json:"policy,omitempty"`
	// SkillSourcesAllowed is set independently of CentralizedSecurity.
	// When centralized=true, sources are in Policy.SkillSourcesAllowed (merged).
	// When centralized=false but source control is configured, this field carries
	// the restriction so clients can enforce it without full centralized mode.
	SkillSourcesAllowed []string `json:"skill_sources_allowed,omitempty"`
}

// SecurityPolicyProvider 安全策略提供者接口（供 OutboundInterceptor 和 ws.Gateway 使用）
type SecurityPolicyProvider interface {
	GetEffectivePolicyByUserID(ctx context.Context, userID string) (*EffectivePolicy, error)
	IsCentralizedEnabled(ctx context.Context) (bool, error)
	GetHeartbeatPolicy(ctx context.Context, userID string) (*HeartbeatSecurityPayload, error)
}
