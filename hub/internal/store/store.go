package store

import (
	"context"
	"strings"
	"time"
)

const DefaultTenantID = "tenant_default"

type tenantContextKey struct{}

func WithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, NormalizeTenantID(tenantID))
}

func TenantIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return DefaultTenantID
	}
	if tenantID, ok := ctx.Value(tenantContextKey{}).(string); ok {
		return NormalizeTenantID(tenantID)
	}
	return DefaultTenantID
}

func TenantIDFromContextIfPresent(ctx context.Context) (string, bool) {
	if ctx == nil {
		return DefaultTenantID, false
	}
	tenantID, ok := ctx.Value(tenantContextKey{}).(string)
	if !ok {
		return DefaultTenantID, false
	}
	return NormalizeTenantID(tenantID), true
}

func NormalizeTenantID(tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return DefaultTenantID
	}
	return tenantID
}

type Tenant struct {
	ID               string
	Slug             string
	Name             string
	Status           string
	PrimaryDomain    string
	SettingsJSON     string
	CreatedByAdminID string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time
}

type AdminUser struct {
	ID           string
	Username     string
	PasswordHash string
	Email        string
	Scope        string
	Role         string
	TenantID     string
	DisplayName  string
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type AdminAuditLog struct {
	ID          string
	TenantID    string
	AdminUserID string
	Action      string
	PayloadJSON string
	CreatedAt   time.Time
}

type AdminAuditLogFilter struct {
	TenantID     string
	TenantScoped bool
	Limit        int
	Action       string
	Query        string
	CreatedFrom  time.Time
	CreatedTo    time.Time
}

type FailureEventLog struct {
	ID          string
	TenantID    string
	Category    string
	EventCode   string
	Message     string
	EntityID    string
	Email       string
	ClientIP    string
	DetailsJSON string
	CreatedAt   time.Time
}

type FailureEventLogFilter struct {
	TenantID     string
	TenantScoped bool
	Keyword      string
	Category     string
	Offset       int
	Limit        int
}

type User struct {
	ID               string
	TenantID         string
	Email            string
	SN               string
	Status           string
	EnrollmentStatus string
	SmartRoute       bool
	EmailVerified    bool
	EmailVerifiedAt  *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type UserEnrollment struct {
	ID        string
	TenantID  string
	Email     string
	Mobile    string
	Status    string
	Note      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type EmailBlockItem struct {
	ID        string
	TenantID  string
	Email     string
	Reason    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type InvitationCode struct {
	ID           string
	TenantID     string
	Code         string
	Status       string // "unused" | "used"
	UsedByEmail  string
	UsedAt       *time.Time
	ValidityDays int // 0 = no expiry; >0 = validity days
	Exported     bool
	VIP          bool
	CreatedAt    time.Time
}

type EmailInvite struct {
	ID        string
	TenantID  string
	Email     string
	Role      string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Machine struct {
	ID               string
	TenantID         string
	UserID           string
	ClientID         string
	Name             string
	Alias            string
	Platform         string
	Hostname         string
	Arch             string
	AppVersion       string
	HeartbeatSec     int
	MachineTokenHash string
	Status           string
	LastSeenAt       *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type MachineMetadata struct {
	Name                 string
	Platform             string
	Hostname             string
	Arch                 string
	AppVersion           string
	HeartbeatIntervalSec int
}

type ViewerToken struct {
	ID        string
	TenantID  string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
	RevokedAt *time.Time
}

type LoginToken struct {
	ID            string
	TenantID      string
	Email         string
	TokenHash     string
	PollTokenHash string
	Purpose       string
	ExpiresAt     time.Time
	ConsumedAt    *time.Time
	CreatedAt     time.Time
}

type Session struct {
	ID          string
	TenantID    string
	MachineID   string
	UserID      string
	Tool        string
	Title       string
	ProjectPath string
	Status      string
	SummaryJSON string
	PreviewText string
	OutputSeq   int64
	HostOnline  bool
	StartedAt   time.Time
	UpdatedAt   time.Time
	EndedAt     *time.Time
	ExitCode    *int
}

type UserDurationSummary struct {
	UserEmail       string
	DurationSeconds int64
}

type UserTokenUsage struct {
	InputTokens       int64
	OutputTokens      int64
	CachedInputTokens int64
	CacheWriteTokens  int64
}

func (u UserTokenUsage) TotalTokens() int64 {
	return u.InputTokens + u.OutputTokens
}

type UserTokenSummary struct {
	UserEmail string
	Usage     UserTokenUsage
}

type TenantRepository interface {
	Create(ctx context.Context, tenant *Tenant) error
	GetByID(ctx context.Context, id string) (*Tenant, error)
	GetBySlug(ctx context.Context, slug string) (*Tenant, error)
	List(ctx context.Context) ([]*Tenant, error)
	EnsureDefault(ctx context.Context) (*Tenant, error)
	DeleteByID(ctx context.Context, id string) error
}
type AdminUserRepository interface {
	Create(ctx context.Context, admin *AdminUser) error
	GetByUsername(ctx context.Context, username string) (*AdminUser, error)
	GetByUsernameScoped(ctx context.Context, username, scope, tenantID string) (*AdminUser, error)
	Count(ctx context.Context) (int, error)
	UpdatePassword(ctx context.Context, username, passwordHash string, updatedAt time.Time) error
	UpdatePasswordScoped(ctx context.Context, username, scope, tenantID, passwordHash string, updatedAt time.Time) error
	UpdateEmail(ctx context.Context, username, email string, updatedAt time.Time) error
	UpdateEmailScoped(ctx context.Context, username, scope, tenantID, email string, updatedAt time.Time) error
	DeleteAll(ctx context.Context) error
}

type SystemSettingsRepository interface {
	Set(ctx context.Context, key, valueJSON string) error
	Get(ctx context.Context, key string) (string, error)
}

type AdminAuditRepository interface {
	Create(ctx context.Context, log *AdminAuditLog) error
	List(ctx context.Context, filter AdminAuditLogFilter) ([]*AdminAuditLog, error)
}

type FailureEventLogRepository interface {
	Create(ctx context.Context, log *FailureEventLog) error
	List(ctx context.Context, filter FailureEventLogFilter) ([]*FailureEventLog, int, error)
}

type UserRepository interface {
	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id string) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	GetByTenantEmail(ctx context.Context, tenantID, email string) (*User, error)
	List(ctx context.Context) ([]*User, error)
	ListByTenant(ctx context.Context, tenantID string) ([]*User, error)
	DeleteByEmail(ctx context.Context, email string) error
	DeleteByTenantEmail(ctx context.Context, tenantID, email string) error
	UpdateSmartRoute(ctx context.Context, userID string, enabled bool) error
	MarkEmailVerified(ctx context.Context, tenantID, email string) error
}

type EnrollmentRepository interface {
	Create(ctx context.Context, item *UserEnrollment) error
	GetPendingByEmail(ctx context.Context, email string) (*UserEnrollment, error)
	GetPendingByTenantEmail(ctx context.Context, tenantID string, email string) (*UserEnrollment, error)
	ListPending(ctx context.Context) ([]*UserEnrollment, error)
	ListPendingByTenant(ctx context.Context, tenantID string) ([]*UserEnrollment, error)
	ListAll(ctx context.Context) ([]*UserEnrollment, error)
	ListAllByTenant(ctx context.Context, tenantID string) ([]*UserEnrollment, error)
	Approve(ctx context.Context, id string, updatedAt time.Time) error
	Reject(ctx context.Context, id string, updatedAt time.Time) error
	UpdateMobile(ctx context.Context, id string, mobile string) error
	GetByMobile(ctx context.Context, mobile string) (*UserEnrollment, error)
	DeleteByTenantEmail(ctx context.Context, tenantID, email string) (int64, error)
}

type EmailBlocklistRepository interface {
	Create(ctx context.Context, item *EmailBlockItem) error
	DeleteByEmail(ctx context.Context, email string) error
	DeleteByTenantEmail(ctx context.Context, tenantID string, email string) error
	GetByEmail(ctx context.Context, email string) (*EmailBlockItem, error)
	GetByTenantEmail(ctx context.Context, tenantID string, email string) (*EmailBlockItem, error)
	List(ctx context.Context) ([]*EmailBlockItem, error)
	ListByTenant(ctx context.Context, tenantID string) ([]*EmailBlockItem, error)
}

type InvitationCodeRepository interface {
	Create(ctx context.Context, item *InvitationCode) error
	GetByID(ctx context.Context, id string) (*InvitationCode, error)
	GetByCode(ctx context.Context, code string) (*InvitationCode, error)
	GetByTenantCode(ctx context.Context, tenantID, code string) (*InvitationCode, error)
	GetByEmail(ctx context.Context, email string) (*InvitationCode, error)
	GetByTenantEmail(ctx context.Context, tenantID, email string) (*InvitationCode, error)
	List(ctx context.Context, status string, search string) ([]*InvitationCode, error)
	ListPaged(ctx context.Context, status string, search string, offset, limit int) ([]*InvitationCode, int, error)
	ListPagedByTenant(ctx context.Context, tenantID string, status string, search string, offset, limit int) ([]*InvitationCode, int, error)
	MarkUsed(ctx context.Context, id string, email string, usedAt time.Time) error
	Unbind(ctx context.Context, id string) error
	DeleteByID(ctx context.Context, id string) error
	DeleteByEmail(ctx context.Context, email string) (int64, error)
	DeleteByTenantEmail(ctx context.Context, tenantID, email string) (int64, error)
	ListUnused(ctx context.Context, exportedFilter string, vipOnly ...bool) ([]*InvitationCode, error)
	ListUnusedByTenant(ctx context.Context, tenantID, exportedFilter string, vipOnly ...bool) ([]*InvitationCode, error)
	MarkExported(ctx context.Context, ids []string) error
}

type EmailInviteRepository interface {
	Create(ctx context.Context, item *EmailInvite) error
	List(ctx context.Context) ([]*EmailInvite, error)
	ListByTenant(ctx context.Context, tenantID string) ([]*EmailInvite, error)
	GetByID(ctx context.Context, id string) (*EmailInvite, error)
	DeleteByID(ctx context.Context, id string) error
}

type MachineRepository interface {
	Create(ctx context.Context, machine *Machine) error
	GetByID(ctx context.Context, id string) (*Machine, error)
	GetByUserAndClientID(ctx context.Context, userID, clientID string) (*Machine, error)
	ListByUserID(ctx context.Context, userID string) ([]*Machine, error)
	ListByTenant(ctx context.Context, tenantID string) ([]*Machine, error)
	ListAll(ctx context.Context) ([]*Machine, error)
	Delete(ctx context.Context, machineID string) error
	DeleteByUserID(ctx context.Context, userID string) (int64, error)
	DeleteByTenantUserID(ctx context.Context, tenantID, userID string) (int64, error)
	ForceDeleteByUserID(ctx context.Context, userID string) (int64, error)
	ForceDeleteByTenantUserID(ctx context.Context, tenantID, userID string) (int64, error)
	DeleteOffline(ctx context.Context) (int64, error)
	DeleteOfflineByTenant(ctx context.Context, tenantID string) (int64, error)
	DeleteOfflineByUserID(ctx context.Context, userID string) (int64, error)
	DeleteOfflineByTenantUserID(ctx context.Context, tenantID, userID string) (int64, error)
	UpdateMetadata(ctx context.Context, machineID string, metadata MachineMetadata) error
	UpdateStatus(ctx context.Context, machineID string, status string) error
	UpdateHeartbeat(ctx context.Context, machineID string, at time.Time) error
	UpdateTokenHash(ctx context.Context, machineID string, tokenHash string) error
	UpdateAlias(ctx context.Context, machineID string, alias string) error
	// ResetAllOnline sets status='offline' for every machine currently
	// marked 'online'. Called at Hub startup to clear stale state from
	// a previous unclean shutdown.
	ResetAllOnline(ctx context.Context) (int64, error)
}

type ViewerTokenRepository interface {
	Create(ctx context.Context, token *ViewerToken) error
	GetByTokenHash(ctx context.Context, tokenHash string) (*ViewerToken, error)
	ExtendExpiry(ctx context.Context, tokenID string, expiresAt time.Time) error
	DeleteByUserID(ctx context.Context, userID string) (int64, error)
}

type LoginTokenRepository interface {
	Create(ctx context.Context, token *LoginToken) error
	GetByTokenHash(ctx context.Context, tokenHash string) (*LoginToken, error)
	GetByPollTokenHash(ctx context.Context, pollTokenHash string) (*LoginToken, error)
	GetPendingByEmail(ctx context.Context, email string) (*LoginToken, error)
	GetPendingByTenantEmail(ctx context.Context, tenantID string, email string) (*LoginToken, error)
	ListPending(ctx context.Context) ([]*LoginToken, error)
	ListPendingByTenant(ctx context.Context, tenantID string) ([]*LoginToken, error)
	RefreshToken(ctx context.Context, tokenID string, tokenHash string, pollTokenHash string) error
	Consume(ctx context.Context, tokenID string, consumedAt time.Time) error
}

type SessionRepository interface {
	Create(ctx context.Context, session *Session) error
	UpdateSummary(ctx context.Context, sessionID string, summaryJSON string, status string, updatedAt time.Time) error
	UpdatePreview(ctx context.Context, sessionID string, previewText string, outputSeq int64, updatedAt time.Time) error
	UpdateHostOnline(ctx context.Context, sessionID string, hostOnline bool, updatedAt time.Time) error
	Close(ctx context.Context, sessionID string, exitCode *int, endedAt time.Time, status string) error
	RecordUserTokenUsageSnapshot(ctx context.Context, tenantID, sourceID, userID string, usage UserTokenUsage, observedAt time.Time) error
	SummarizeUserTokenUsage(ctx context.Context, tenantID string, start, end time.Time) ([]UserTokenSummary, error)
	SummarizeUserDurations(ctx context.Context, tenantID string, start, end, now time.Time) ([]UserDurationSummary, error)
}

// WorkflowRepository persists intent-understanding sessions and workflow
// states for the workflow engine.
type WorkflowRepository interface {
	SaveUnderstandingSession(ctx context.Context, s *UnderstandingSessionRow) error
	GetActiveUnderstandingSession(ctx context.Context, userID string) (*UnderstandingSessionRow, error)
	DeleteUnderstandingSession(ctx context.Context, id string) error

	SaveWorkflowState(ctx context.Context, ws *WorkflowStateRow) error
	GetActiveWorkflowState(ctx context.Context, userID string) (*WorkflowStateRow, error)
	DeleteWorkflowState(ctx context.Context, id string) error

	CleanupExpired(ctx context.Context, olderThan time.Duration) error
}

// UnderstandingSessionRow is the persistence-level representation of an
// understanding session. JSON fields are stored as opaque strings to avoid
// import cycles with the im package.
type UnderstandingSessionRow struct {
	ID         string
	TenantID   string
	UserID     string
	IntentJSON string
	RoundsJSON string
	State      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// WorkflowStateRow is the persistence-level representation of a workflow
// state. JSON fields are stored as opaque strings.
type WorkflowStateRow struct {
	ID               string
	TenantID         string
	UserID           string
	Type             string
	TemplateType     string
	IntentJSON       string
	CurrentPhase     string
	PhaseOutputsJSON string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type LLMPromptCacheEntry struct {
	CacheKey          string
	ProviderID        string
	Model             string
	Kind              string
	InputHash         string
	Payload           []byte
	PayloadBytes      int64
	CachedInputTokens int64
	CacheWriteTokens  int64
	HitCount          int64
	CreatedAt         time.Time
	AccessedAt        time.Time
	ExpiresAt         *time.Time
}

type LLMPromptCacheStats struct {
	Entries        int64
	TotalBytes     int64
	ExpiredEntries int64
	ExpiredBytes   int64
	TotalHits      int64
}

type LLMPromptCacheRepository interface {
	Get(ctx context.Context, cacheKey string) (*LLMPromptCacheEntry, error)
	Put(ctx context.Context, entry *LLMPromptCacheEntry) error
	Delete(ctx context.Context, cacheKey string) error
	Purge(ctx context.Context) (int64, error)
	DeleteExpired(ctx context.Context, now time.Time) (int64, error)
	TrimToBytes(ctx context.Context, maxBytes int64) (int64, error)
	ListRecent(ctx context.Context, limit int) ([]*LLMPromptCacheEntry, error)
	Stats(ctx context.Context, now time.Time) (*LLMPromptCacheStats, error)
}

type Store struct {
	Tenants         TenantRepository
	Admins          AdminUserRepository
	System          SystemSettingsRepository
	AdminAudit      AdminAuditRepository
	FailureLogs     FailureEventLogRepository
	Users           UserRepository
	Enrollments     EnrollmentRepository
	EmailBlocks     EmailBlocklistRepository
	InvitationCodes InvitationCodeRepository
	EmailInvites    EmailInviteRepository
	Machines        MachineRepository
	ViewerTokens    ViewerTokenRepository
	LoginTokens     LoginTokenRepository
	Sessions        SessionRepository
	WorkflowRepo    WorkflowRepository
	LLMPromptCache  LLMPromptCacheRepository
}
