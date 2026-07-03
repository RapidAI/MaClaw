package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ErrAlreadyRated is returned when a duplicate rating is attempted for the same (post_id, machine_id).
var ErrAlreadyRated = errors.New("already rated")

type AdminUser struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash,omitempty"`
	Email        string    `json:"email"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type AdminAuditLog struct {
	ID          string    `json:"id"`
	AdminUserID string    `json:"admin_user_id"`
	Action      string    `json:"action"`
	PayloadJSON string    `json:"payload_json"`
	CreatedAt   time.Time `json:"created_at"`
}

type FailureEventLog struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id,omitempty"`
	Category    string    `json:"category"`
	EventCode   string    `json:"event_code"`
	Message     string    `json:"message"`
	EntityID    string    `json:"entity_id"`
	Email       string    `json:"email"`
	ClientIP    string    `json:"client_ip"`
	DetailsJSON string    `json:"details_json"`
	CreatedAt   time.Time `json:"created_at"`
}

type FailureEventLogFilter struct {
	TenantID    string
	TenantIDSet bool
	Keyword     string
	Category    string
	Offset      int
	Limit       int
}

type HubInstance struct {
	ID                                    string     `json:"id"`
	InstallationID                        string     `json:"installation_id,omitempty"`
	HubOrigin                             string     `json:"hub_origin"`
	DefaultSignupScope                    string     `json:"default_signup_scope"`
	OwnerEmail                            string     `json:"owner_email"`
	Name                                  string     `json:"name"`
	Description                           string     `json:"description"`
	BaseURL                               string     `json:"base_url"`
	Host                                  string     `json:"host"`
	Port                                  int        `json:"port"`
	Visibility                            string     `json:"visibility"`
	EnrollmentMode                        string     `json:"enrollment_mode"`
	CorporateEmailDomain                  string     `json:"corporate_email_domain"`
	AcceptPublicSignup                    bool       `json:"accept_public_signup"`
	Status                                string     `json:"status"`
	IsDisabled                            bool       `json:"is_disabled"`
	DisabledReason                        string     `json:"disabled_reason"`
	CapabilitiesJSON                      string     `json:"capabilities_json,omitempty"`
	RegistrationPolicyJSON                string     `json:"registration_policy_json,omitempty"`
	HubSecretHash                         string     `json:"hub_secret_hash,omitempty"`
	InvitationCodeRequired                bool       `json:"invitation_code_required"`
	DigitalEmployeeQuota                  int        `json:"digital_employee_quota"`
	DigitalEmployeeAuthorizationEnabled   bool       `json:"digital_employee_authorization_enabled"`
	DigitalEmployeeAuthorizationExpiresAt *time.Time `json:"digital_employee_authorization_expires_at,omitempty"`
	AllowExternalProviders                bool       `json:"allow_external_providers"`
	LastSeenAt                            *time.Time `json:"last_seen_at"`
	CreatedAt                             time.Time  `json:"created_at"`
	UpdatedAt                             time.Time  `json:"updated_at"`
}

type HubTenantRegistrationPolicy struct {
	TenantID                string `json:"tenant_id,omitempty"`
	TenantName              string `json:"tenant_name,omitempty"`
	SignupScope             string `json:"signup_scope"`
	IsPublicFallback        bool   `json:"is_public_fallback"`
	InviteEnabled           bool   `json:"invite_enabled"`
	MaxActiveInvites        int    `json:"max_active_invites"`
	MonthlyInviteQuota      int    `json:"monthly_invite_quota"`
	PerInviteMaxUsesDefault int    `json:"per_invite_max_uses_default"`
	PerInviteMaxUsesMax     int    `json:"per_invite_max_uses_max"`
	Status                  string `json:"status"`
}

func (p *HubTenantRegistrationPolicy) UnmarshalJSON(data []byte) error {
	type alias HubTenantRegistrationPolicy
	var next alias
	if err := json.Unmarshal(data, &next); err != nil {
		return err
	}
	var probe struct {
		InviteEnabled *bool `json:"invite_enabled"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if probe.InviteEnabled == nil {
		next.InviteEnabled = true
	}
	*p = HubTenantRegistrationPolicy(next)
	return nil
}

type HubRegistrationPolicyState struct {
	Tenants map[string]HubTenantRegistrationPolicy `json:"tenants"`
}

type HubUserLink struct {
	ID        string    `json:"id"`
	HubID     string    `json:"hub_id"`
	TenantID  string    `json:"tenant_id,omitempty"`
	Email     string    `json:"email"`
	IsDefault bool      `json:"is_default"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type HubTenantUserCount struct {
	HubID      string `json:"hub_id"`
	TenantID   string `json:"tenant_id,omitempty"`
	Count      int    `json:"count"`
	AllTenants bool   `json:"all_tenants,omitempty"`
}

type HubTenantUserDomain struct {
	HubID    string `json:"hub_id"`
	TenantID string `json:"tenant_id,omitempty"`
	Domain   string `json:"domain"`
}

type HubUserFirstSeen struct {
	HubID     string    `json:"hub_id"`
	TenantID  string    `json:"tenant_id,omitempty"`
	Email     string    `json:"email"`
	FirstSeen time.Time `json:"first_seen"`
}

type HubDomainRoute struct {
	ID        string    `json:"id"`
	HubID     string    `json:"hub_id"`
	TenantID  string    `json:"tenant_id,omitempty"`
	Domain    string    `json:"domain"`
	Enabled   bool      `json:"enabled"`
	Priority  int       `json:"priority"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type BlockedEmail struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type BlockedIP struct {
	ID        string    `json:"id"`
	IP        string    `json:"ip"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type InvitationCodeRoute struct {
	Code        string    `json:"code"`
	HubID       string    `json:"hub_id"`
	TenantID    string    `json:"tenant_id"`
	UsedByEmail string    `json:"used_by_email,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type HASyncOp struct {
	Seq           int64     `json:"seq"`
	OpID          string    `json:"op_id"`
	SourceNodeID  string    `json:"source_node_id"`
	EntityType    string    `json:"entity_type"`
	EntityID      string    `json:"entity_id"`
	OpType        string    `json:"op_type"`
	EntityVersion int64     `json:"entity_version"`
	OccurredAt    time.Time `json:"occurred_at"`
	PayloadJSON   string    `json:"payload_json"`
	PayloadHash   string    `json:"payload_hash"`
}

type HAAppliedOp struct {
	OpID         string    `json:"op_id"`
	SourceNodeID string    `json:"source_node_id"`
	EntityType   string    `json:"entity_type"`
	EntityID     string    `json:"entity_id"`
	AppliedAt    time.Time `json:"applied_at"`
}

type HAPruneResult struct {
	DeletedOps        int64 `json:"deleted_ops"`
	DeletedAppliedOps int64 `json:"deleted_applied_ops"`
	RemainingOps      int64 `json:"remaining_ops"`
	MaxSeq            int64 `json:"max_seq"`
}

type HAPeerCursor struct {
	PeerNodeID    string     `json:"peer_node_id"`
	LastPulledSeq int64      `json:"last_pulled_seq"`
	LastPulledAt  *time.Time `json:"last_pulled_at,omitempty"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
}

type HAEntityVersion struct {
	EntityType      string    `json:"entity_type"`
	EntityID        string    `json:"entity_id"`
	Version         int64     `json:"version"`
	UpdatedAt       time.Time `json:"updated_at"`
	UpdatedByNodeID string    `json:"updated_by_node_id"`
}

type HAHeartbeatSyncState struct {
	HubID            string     `json:"hub_id"`
	LastSyncedSeenAt *time.Time `json:"last_synced_seen_at,omitempty"`
}

type HubUserUsageDaily struct {
	HubID             string    `json:"hub_id"`
	TenantID          string    `json:"tenant_id,omitempty"`
	UserEmail         string    `json:"user_email"`
	Day               string    `json:"day"`
	InputTokens       int64     `json:"input_tokens"`
	OutputTokens      int64     `json:"output_tokens"`
	CachedInputTokens int64     `json:"cached_input_tokens"`
	CacheWriteTokens  int64     `json:"cache_write_tokens"`
	DurationSeconds   int64     `json:"duration_seconds"`
	UpdatedAt         time.Time `json:"updated_at"`
	HubName           string    `json:"hub_name,omitempty"`
}

func (u HubUserUsageDaily) TotalTokens() int64 {
	return u.InputTokens + u.OutputTokens
}

type SystemSettingEntry struct {
	Key       string    `json:"key"`
	ValueJSON string    `json:"value_json"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AdminUserRepository interface {
	Create(ctx context.Context, admin *AdminUser) error
	GetByUsername(ctx context.Context, username string) (*AdminUser, error)
	Count(ctx context.Context) (int, error)
	UpdatePassword(ctx context.Context, username, passwordHash string, updatedAt time.Time) error
	UpdateEmail(ctx context.Context, username, email string, updatedAt time.Time) error
	DeleteAll(ctx context.Context) error
}

type SystemSettingsRepository interface {
	Set(ctx context.Context, key, valueJSON string) error
	Get(ctx context.Context, key string) (string, error)
	List(ctx context.Context) ([]*SystemSettingEntry, error)
}

type AdminAuditRepository interface {
	Create(ctx context.Context, log *AdminAuditLog) error
}

type FailureEventLogRepository interface {
	Create(ctx context.Context, log *FailureEventLog) error
	List(ctx context.Context, filter FailureEventLogFilter) ([]*FailureEventLog, int, error)
}

type HubRepository interface {
	Create(ctx context.Context, hub *HubInstance) error
	GetByID(ctx context.Context, id string) (*HubInstance, error)
	GetByInstallationID(ctx context.Context, installationID string) (*HubInstance, error)
	GetByEndpoint(ctx context.Context, host string, port int, baseURL string) (*HubInstance, error)
	UpdateHeartbeat(ctx context.Context, hubID string, at time.Time) error
	ListByEmail(ctx context.Context, email string) ([]*HubInstance, error)
	ListAll(ctx context.Context) ([]*HubInstance, error)
	UpdateName(ctx context.Context, hubID string, name string, updatedAt time.Time) error
	UpdateVisibility(ctx context.Context, hubID string, visibility string, updatedAt time.Time) error
	SetDisabled(ctx context.Context, hubID string, disabled bool, reason string, updatedAt time.Time) error
	UpdateRegistration(ctx context.Context, hub *HubInstance) error
	UpdateInvitationCodeRequired(ctx context.Context, hubID string, required bool, updatedAt time.Time) error
	UpdateDigitalEmployeeAuthorization(ctx context.Context, hubID string, quota int, enabled bool, expiresAt *time.Time, updatedAt time.Time) error
	DeleteByID(ctx context.Context, hubID string) error
}

type HubUserLinkRepository interface {
	Create(ctx context.Context, link *HubUserLink) error
	Upsert(ctx context.Context, link *HubUserLink) error
	ListByEmail(ctx context.Context, email string) ([]*HubUserLink, error)
	ListAll(ctx context.Context) ([]*HubUserLink, error)
	GetDefaultByEmail(ctx context.Context, email string) (*HubUserLink, error)
	DeleteByID(ctx context.Context, id string) error
	DeleteByHubID(ctx context.Context, hubID string) error
	DeleteByEmail(ctx context.Context, email string) (int64, error)
	DeleteByHubEmail(ctx context.Context, hubID, email string) (int64, error)
}

type HubDomainRouteRepository interface {
	Upsert(ctx context.Context, route *HubDomainRoute) error
	ListAll(ctx context.Context) ([]*HubDomainRoute, error)
	DeleteByID(ctx context.Context, id string) error
	DeleteByHubID(ctx context.Context, hubID string) error
}

type BlockedEmailRepository interface {
	GetByEmail(ctx context.Context, email string) (*BlockedEmail, error)
	Create(ctx context.Context, item *BlockedEmail) error
	DeleteByEmail(ctx context.Context, email string) error
	List(ctx context.Context) ([]*BlockedEmail, error)
}

type BlockedIPRepository interface {
	GetByIP(ctx context.Context, ip string) (*BlockedIP, error)
	Create(ctx context.Context, item *BlockedIP) error
	DeleteByIP(ctx context.Context, ip string) error
	List(ctx context.Context) ([]*BlockedIP, error)
}

type InvitationCodeRouteRepository interface {
	Upsert(ctx context.Context, code string, hubID string, tenantID string) error
	GetByCode(ctx context.Context, code string) (*InvitationCodeRoute, error)
	DeleteByCode(ctx context.Context, code string) error
	DeleteByHubID(ctx context.Context, hubID string) error
	ListAll(ctx context.Context) ([]*InvitationCodeRoute, error)
	MarkUsedByEmail(ctx context.Context, code string, email string) error
}

type HASyncOpRepository interface {
	Append(ctx context.Context, op *HASyncOp) error
	ListAfterSeq(ctx context.Context, afterSeq int64, limit int) ([]*HASyncOp, error)
	GetMaxSeq(ctx context.Context) (int64, error)
	HasApplied(ctx context.Context, opID string) (bool, error)
	MarkApplied(ctx context.Context, item *HAAppliedOp) error
}

type HAPeerCursorRepository interface {
	Get(ctx context.Context, peerNodeID string) (*HAPeerCursor, error)
	Upsert(ctx context.Context, item *HAPeerCursor) error
}

type HAEntityVersionRepository interface {
	Get(ctx context.Context, entityType, entityID string) (*HAEntityVersion, error)
	Upsert(ctx context.Context, item *HAEntityVersion) error
}

type HAHeartbeatSyncStateRepository interface {
	Get(ctx context.Context, hubID string) (*HAHeartbeatSyncState, error)
	Upsert(ctx context.Context, item *HAHeartbeatSyncState) error
}

type HubUserUsageRepository interface {
	UpsertDaily(ctx context.Context, items []*HubUserUsageDaily) error
	ReplaceDaily(ctx context.Context, hubID string, tenantIDs []string, startDay, endDay string, items []*HubUserUsageDaily) error
	Summarize(ctx context.Context, hubID, tenantID string, start, end time.Time) ([]*HubUserUsageDaily, error)
}

type GossipPost struct {
	ID        string
	MachineID string
	UserEmail string
	Nickname  string
	Content   string
	Category  string
	Score     int
	Votes     int
	Locked    bool
	Flagged   bool
	CreatedAt time.Time
}

type GossipComment struct {
	ID        string
	PostID    string
	MachineID string
	UserEmail string
	Nickname  string
	Content   string
	Rating    int
	CreatedAt time.Time
}

type GossipRepository interface {
	CreatePost(ctx context.Context, post *GossipPost) error
	ListPosts(ctx context.Context, offset, limit int) ([]*GossipPost, int, error)
	ListAllPosts(ctx context.Context, offset, limit int) ([]*GossipPost, int, error)
	ListFlaggedPosts(ctx context.Context, offset, limit int) ([]*GossipPost, int, error)
	GetPost(ctx context.Context, id string) (*GossipPost, error)
	DeletePost(ctx context.Context, id string) error
	DeleteFlaggedPosts(ctx context.Context) (int, error)
	LockPost(ctx context.Context, id string, locked bool) error
	FlagPost(ctx context.Context, id string, flagged bool) error
	ReplaceAll(ctx context.Context, posts []*GossipPost, comments []*GossipComment) error
	CreateComment(ctx context.Context, comment *GossipComment) error
	ListComments(ctx context.Context, postID string, offset, limit int) ([]*GossipComment, int, error)
	DeleteComment(ctx context.Context, id string) error
	UpdatePostScore(ctx context.Context, postID string) error
	HasRated(ctx context.Context, postID, machineID string) (bool, error)
	RateComment(ctx context.Context, comment *GossipComment) error
}

type NewsArticle struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Category  string    `json:"category"`
	Pinned    bool      `json:"pinned"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type NewsRepository interface {
	Create(ctx context.Context, article *NewsArticle) error
	Update(ctx context.Context, article *NewsArticle) error
	Delete(ctx context.Context, id string) error
	GetByID(ctx context.Context, id string) (*NewsArticle, error)
	List(ctx context.Context, offset, limit int) ([]*NewsArticle, int, error)
	ListLatest(ctx context.Context, limit int) ([]*NewsArticle, error)
	CountPinned(ctx context.Context) (int, error)
}

type Store struct {
	Admins               AdminUserRepository
	System               SystemSettingsRepository
	AdminAudit           AdminAuditRepository
	FailureLogs          FailureEventLogRepository
	Hubs                 HubRepository
	HubUserLinks         HubUserLinkRepository
	HubDomainRoutes      HubDomainRouteRepository
	BlockedEmails        BlockedEmailRepository
	BlockedIPs           BlockedIPRepository
	InvitationCodeRoutes InvitationCodeRouteRepository
	HASyncOps            HASyncOpRepository
	HAPeerCursors        HAPeerCursorRepository
	HAEntityVersions     HAEntityVersionRepository
	HAHeartbeatSync      HAHeartbeatSyncStateRepository
	HubUserUsage         HubUserUsageRepository
	Gossip               GossipRepository
	News                 NewsRepository
}

// ---------------------------------------------------------------------------
// LLM Node Binding (HA anti-double-spend)
// ---------------------------------------------------------------------------

// LLMNodeBinding represents a tenant's exclusive binding to a HubCenter node.
type LLMNodeBinding struct {
	HubID      string    `json:"hub_id"`
	TenantID   string    `json:"tenant_id"`
	NodeID     string    `json:"node_id"`
	BoundAt    time.Time `json:"bound_at"`
	LastActive time.Time `json:"last_active"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// LLMNodeBindingRepository persists binding state.
type LLMNodeBindingRepository interface {
	Upsert(ctx context.Context, binding *LLMNodeBinding) error
	Get(ctx context.Context, hubID, tenantID string) (*LLMNodeBinding, error)
	Delete(ctx context.Context, hubID, tenantID string) error
	ListByNode(ctx context.Context, nodeID string) ([]*LLMNodeBinding, error)
	ListAll(ctx context.Context) ([]*LLMNodeBinding, error)
	DeleteExpired(ctx context.Context, now time.Time) (int64, error)
}
