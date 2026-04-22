package store

import (
	"context"
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

type HubInstance struct {
	ID                     string     `json:"id"`
	InstallationID         string     `json:"installation_id"`
	OwnerEmail             string     `json:"owner_email"`
	Name                   string     `json:"name"`
	Description            string     `json:"description"`
	BaseURL                string     `json:"base_url"`
	Host                   string     `json:"host"`
	Port                   int        `json:"port"`
	Visibility             string     `json:"visibility"`
	EnrollmentMode         string     `json:"enrollment_mode"`
	Status                 string     `json:"status"`
	IsDisabled             bool       `json:"is_disabled"`
	DisabledReason         string     `json:"disabled_reason"`
	CapabilitiesJSON       string     `json:"capabilities_json,omitempty"`
	HubSecretHash          string     `json:"hub_secret_hash,omitempty"`
	InvitationCodeRequired bool       `json:"invitation_code_required"`
	LastSeenAt             *time.Time `json:"last_seen_at"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type HubUserLink struct {
	ID        string    `json:"id"`
	HubID     string    `json:"hub_id"`
	Email     string    `json:"email"`
	IsDefault bool      `json:"is_default"`
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
}

type AdminAuditRepository interface {
	Create(ctx context.Context, log *AdminAuditLog) error
}

type HubRepository interface {
	Create(ctx context.Context, hub *HubInstance) error
	GetByID(ctx context.Context, id string) (*HubInstance, error)
	GetByInstallationID(ctx context.Context, installationID string) (*HubInstance, error)
	UpdateHeartbeat(ctx context.Context, hubID string, at time.Time) error
	ListByEmail(ctx context.Context, email string) ([]*HubInstance, error)
	ListAll(ctx context.Context) ([]*HubInstance, error)
	UpdateVisibility(ctx context.Context, hubID string, visibility string, updatedAt time.Time) error
	SetDisabled(ctx context.Context, hubID string, disabled bool, reason string, updatedAt time.Time) error
	UpdateRegistration(ctx context.Context, hub *HubInstance) error
	UpdateInvitationCodeRequired(ctx context.Context, hubID string, required bool, updatedAt time.Time) error
	DeleteByID(ctx context.Context, hubID string) error
}

type HubUserLinkRepository interface {
	Create(ctx context.Context, link *HubUserLink) error
	ListByEmail(ctx context.Context, email string) ([]*HubUserLink, error)
	GetDefaultByEmail(ctx context.Context, email string) (*HubUserLink, error)
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
	LockPost(ctx context.Context, id string, locked bool) error
	FlagPost(ctx context.Context, id string, flagged bool) error
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
	Admins           AdminUserRepository
	System           SystemSettingsRepository
	AdminAudit       AdminAuditRepository
	Hubs             HubRepository
	HubUserLinks     HubUserLinkRepository
	BlockedEmails    BlockedEmailRepository
	BlockedIPs       BlockedIPRepository
	HASyncOps        HASyncOpRepository
	HAPeerCursors    HAPeerCursorRepository
	HAEntityVersions HAEntityVersionRepository
	HAHeartbeatSync  HAHeartbeatSyncStateRepository
	Gossip           GossipRepository
	News             NewsRepository
}
