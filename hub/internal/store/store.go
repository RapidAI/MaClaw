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

type KnowledgeShare struct {
	KnowledgeID         string
	TenantID            string
	OwnerUserID         string
	OwnerUserEmail      string
	Title               string
	Description         string
	VisibilityScope     string
	VisibilityUsersJSON string
	SourceSummaryJSON   string
	ShareURL            string
	HubID               string
	StorageRef          string
	Status              string
	ViewCount           int64
	ImportCount         int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
	PublishedAt         time.Time
	ExpiresAt           *time.Time
	ForcedDeletedBy     string
	ForcedDeletedReason string
	ForcedDeletedAt     *time.Time
}

type KnowledgeShareFilter struct {
	TenantID       string
	TenantScoped   bool
	OwnerUserID    string
	OwnerUserEmail string
	User           string
	Keyword        string
	Sort           string
	Offset         int
	Limit          int
	IncludeDeleted bool
}

type KnowledgeShareForceDeleteRequest struct {
	KnowledgeID string
	AdminUserID string
	Reason      string
	DeletedAt   time.Time
}

// Digital asset library statuses.
const (
	DigitalAssetStatusActive   = "active"
	DigitalAssetStatusArchived = "archived"
	DigitalAssetStatusDeleted  = "deleted"
)

// Digital asset ACL modes.
const (
	DigitalAssetACLAllMembers = "all_members"
	DigitalAssetACLRestricted = "restricted"
)

// DigitalAssetLibrary is a tenant-owned enterprise knowledge library.
type DigitalAssetLibrary struct {
	ID                 string
	TenantID           string
	Name               string
	Description        string
	Status             string
	SyncEnabled        bool
	ACLMode            string
	ACLDepartmentsJSON string
	ACLUsersJSON       string
	ContentRev         int64
	ContentHash        string
	StorePath          string
	SourceCount        int64
	CardCount          int64
	ByteSize           int64
	CreatedBy          string
	UpdatedBy          string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	DeletedAt          *time.Time
}

// DigitalAssetLibraryFilter lists libraries within a tenant.
type DigitalAssetLibraryFilter struct {
	TenantID       string
	Status         string // empty = all non-deleted unless IncludeDeleted
	IncludeDeleted bool
	Keyword        string
	Offset         int
	Limit          int
}

// DigitalAssetChangelog is one content revision of a library.
type DigitalAssetChangelog struct {
	TenantID      string
	LibraryID     string
	Rev           int64
	Op            string
	PackageStatus string
	PackageRef    string
	PackageSHA256 string
	PackageBytes  int64
	PayloadJSON   string
	ContentHash   string
	ErrorMessage  string
	CreatedAt     time.Time
	ReadyAt       *time.Time
}

// DigitalAssetImportJob tracks admin import/export/merge work.
type DigitalAssetImportJob struct {
	ID           string
	TenantID     string
	LibraryID    string
	Kind         string
	Status       string
	ProgressJSON string
	ErrorMessage string
	CreatedBy    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// DigitalAssetSyncCursor records last successful client pull.
type DigitalAssetSyncCursor struct {
	TenantID   string
	LibraryID  string
	UserID     string
	DeviceID   string
	LastRev    int64
	LastSyncAt time.Time
	LastStatus string
}

// DigitalAssetRepository persists digital asset libraries and related rows.
type DigitalAssetRepository interface {
	CreateLibrary(ctx context.Context, lib *DigitalAssetLibrary) error
	GetLibrary(ctx context.Context, tenantID, libraryID string) (*DigitalAssetLibrary, error)
	ListLibraries(ctx context.Context, filter DigitalAssetLibraryFilter) ([]*DigitalAssetLibrary, int, error)
	UpdateLibrary(ctx context.Context, lib *DigitalAssetLibrary) error
	SoftDeleteLibrary(ctx context.Context, tenantID, libraryID string, deletedAt time.Time, updatedBy string) error
	ArchiveLibrary(ctx context.Context, tenantID, libraryID string, updatedAt time.Time, updatedBy string) error

	InsertChangelog(ctx context.Context, row *DigitalAssetChangelog) error
	UpdateChangelogPackage(ctx context.Context, tenantID, libraryID string, rev int64, status, ref, sha256 string, bytes int64, contentHash, errMsg string, readyAt *time.Time) error
	ListChangelogSince(ctx context.Context, tenantID, libraryID string, sinceRev int64, readyOnly bool, limit int) ([]*DigitalAssetChangelog, error)
	GetChangelog(ctx context.Context, tenantID, libraryID string, rev int64) (*DigitalAssetChangelog, error)
	LatestReadyRev(ctx context.Context, tenantID, libraryID string) (int64, error)

	CreateJob(ctx context.Context, job *DigitalAssetImportJob) error
	GetJob(ctx context.Context, tenantID, jobID string) (*DigitalAssetImportJob, error)
	UpdateJob(ctx context.Context, job *DigitalAssetImportJob) error
	CountRunningJobs(ctx context.Context, tenantID string) (int, error)
	// FailStaleRunningJobs marks queued/running jobs whose updated_at is older than before
	// as failed (crash recovery so a tenant is not permanently blocked by ≤1 running job).
	FailStaleRunningJobs(ctx context.Context, tenantID string, before time.Time, errMsg string) (int, error)
	// ListJobs returns recent import/export jobs for a library (newest first).
	ListJobs(ctx context.Context, tenantID, libraryID string, limit int) ([]*DigitalAssetImportJob, error)

	UpsertSyncCursor(ctx context.Context, cur *DigitalAssetSyncCursor) error
	GetSyncCursor(ctx context.Context, tenantID, libraryID, userID, deviceID string) (*DigitalAssetSyncCursor, error)
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

type UserIdentity struct {
	ID         string
	TenantID   string
	UserID     string
	Type       string
	Value      string
	Verified   bool
	VerifiedAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
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
	ID                   string
	TenantID             string
	Code                 string
	Status               string // "unused" | "used"
	UsedByEmail          string
	UsedAt               *time.Time
	ValidityDays         int // 0 = no expiry; >0 = validity days
	Exported             bool
	VIP                  bool
	LLMServiceGroupID    string
	LLMGrantDurationDays int
	LLMGrantCredits      float64
	CreatedAt            time.Time
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

// UserReferralCode is a durable, tenant-scoped personal invitation link. The
// plaintext code is never persisted: CodeHash is used for lookup and
// EncryptedCode only exists so the owner can retrieve their own link again.
type UserReferralCode struct {
	ID            string
	TenantID      string
	InviterUserID string
	CodeHash      string
	EncryptedCode string
	Status        string
	CreatedAt     time.Time
	RotatedAt     *time.Time
}

// UserReferral is the immutable attribution snapshot created after an invitee
// has completed registration. Credits and duration are copied from the rule at
// this point so later rule changes never alter historical rewards.
type UserReferral struct {
	ID             string
	TenantID       string
	ReferralCodeID string
	InviterUserID  string
	InviteeUserID  string
	Status         string
	RegisteredAt   time.Time
	ServiceGroupID string
	InviterCredits float64
	InviteeCredits float64
	DurationDays   int
	InviterGrantID string
	InviteeGrantID string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// UserReferralRegistrationIdempotency retains the completed public
// registration response long enough for network retries to be safe across Hub
// restarts and multiple Hub processes. KeyHash is deliberately opaque: raw
// Idempotency-Key header values are never stored.
type UserReferralRegistrationIdempotency struct {
	TenantID    string
	KeyHash     string
	Fingerprint string
	Status      int
	Payload     []byte
	ExpiresAt   time.Time
	CreatedAt   time.Time
}

// UserReferralRegistrationSession is a short-lived, browser-bound admission
// token for public referral registration. TokenHash, rather than the browser
// cookie value, is persisted so a database leak cannot replay a session.
type UserReferralRegistrationSession struct {
	TenantID      string
	TokenHash     string
	CodeHash      string
	ConfigEpoch   string
	UserAgentHash string
	// InviteeUserID and ReferralID are written only after the session has
	// completed a new-user registration. They make a short browser/desktop
	// retry recoverable without retaining a raw email or phone number.
	InviteeUserID string
	ReferralID    string
	CompletedAt   *time.Time
	ExpiresAt     time.Time
	CreatedAt     time.Time
}

// UserReferralRegistrationCleanupResult reports only short-lived public
// registration artifacts removed after their expiry. Referral attributions,
// their status history, and reward grants are deliberately never removed by
// this cleanup path because they are part of the tenant audit ledger.
type UserReferralRegistrationCleanupResult struct {
	IdempotencyRecords   int
	Sessions             int
	IdentityReservations int
	Handoffs             int
}

// UserReferralIdentityReservation pins an in-progress new-user identity to
// one referral code. It prevents two invitation flows from racing to attribute
// the same verified email or phone to different inviters.
type UserReferralIdentityReservation struct {
	TenantID     string
	IdentityHash string
	CodeHash     string
	SessionHash  string
	ReservedAt   time.Time
	ExpiresAt    time.Time
}

// UserReferralHandoff is an opaque, short-lived bridge from the browser
// invitation landing page to an installed desktop client. TokenHash is the
// only representation of the bearer token kept by Hub. The referral rule is
// snapshotted here so a later settings edit cannot change a registration that
// has already been handed off to the desktop application.
type UserReferralHandoff struct {
	TokenHash      string
	TenantID       string
	CodeHash       string
	ReferralCodeID string
	InviterUserID  string
	ConfigEpoch    string
	ServiceGroupID string
	InviterCredits float64
	InviteeCredits float64
	DurationDays   int
	ExpiresAt      time.Time
	UsedAt         *time.Time
	CreatedAt      time.Time
}

// UserReferralStatusHistory is an append-only audit trail for attribution,
// reward delivery and administrator moderation. Reasons are captured only for
// actions which need an operator explanation.
type UserReferralStatusHistory struct {
	ID          string
	TenantID    string
	ReferralID  string
	FromStatus  string
	ToStatus    string
	Reason      string
	ActorUserID string
	CreatedAt   time.Time
}

type UserReferralInviterSummary struct {
	InviterUserID    string
	InviterEmail     string
	InviteeCount     int
	CreditsGranted   float64
	CreditsConsumed  float64
	LastRegisteredAt *time.Time
	InviterGrantIDs  []string
}

type UserReferralInvitee struct {
	ReferralID     string
	InviteeUserID  string
	InviteeEmail   string
	RegisteredAt   time.Time
	Status         string
	InviterCredits float64
	InviteeCredits float64
	InviterGrantID string
	InviteeGrantID string
}

type UserReferralFilter struct {
	TenantID      string
	InviterUserID string
	Search        string
	Offset        int
	Limit         int
}

// UserReferralDailyMetric is an aggregate operational counter. It deliberately
// contains no referral code, user, contact, IP, browser or device identifiers.
// The day is always stored as a UTC ISO-8601 date.
type UserReferralDailyMetric struct {
	TenantID string
	Date     string
	Event    string
	Count    int64
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
	UserID          string
	UserEmail       string
	DurationSeconds int64
	OnlineSeconds   int64 // connection uptime from sessions (tie-breaker for ranking)
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
	UserID    string
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

type KnowledgeShareRepository interface {
	Create(ctx context.Context, share *KnowledgeShare) error
	List(ctx context.Context, filter KnowledgeShareFilter) ([]*KnowledgeShare, int, error)
	Get(ctx context.Context, knowledgeID string) (*KnowledgeShare, error)
	UpdateOwner(ctx context.Context, share *KnowledgeShare) error
	DeleteOwner(ctx context.Context, knowledgeID, tenantID, ownerUserID string, deletedAt time.Time) error
	ForceDelete(ctx context.Context, req KnowledgeShareForceDeleteRequest) error
	DeleteExpired(ctx context.Context, now time.Time) (int64, error)
	IncrementCounters(ctx context.Context, knowledgeID string, viewDelta, importDelta int64, at time.Time) error
}

type UserRepository interface {
	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id string) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	GetByTenantEmail(ctx context.Context, tenantID, email string) (*User, error)
	GetByTenantIdentity(ctx context.Context, tenantID, identityType, value string) (*User, error)
	ListIdentitiesByUser(ctx context.Context, tenantID, userID string) ([]*UserIdentity, error)
	UpsertIdentity(ctx context.Context, identity *UserIdentity) error
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

type UserReferralRepository interface {
	GetActiveCodeForInviter(ctx context.Context, tenantID, inviterUserID string) (*UserReferralCode, error)
	GetCodeByHash(ctx context.Context, tenantID, codeHash string) (*UserReferralCode, error)
	CreateCode(ctx context.Context, code *UserReferralCode) error
	RotateCode(ctx context.Context, tenantID, codeID string, rotatedAt time.Time) error
	// ReplaceActiveCode atomically retires the current active link and stores
	// replacement. It prevents a failed create from leaving an inviter without
	// a shareable link after they choose to rotate it.
	ReplaceActiveCode(ctx context.Context, tenantID, inviterUserID string, code *UserReferralCode, rotatedAt time.Time) error
	CreateReferral(ctx context.Context, referral *UserReferral) error
	GetReferralForInvitee(ctx context.Context, tenantID, inviteeUserID string) (*UserReferral, error)
	GetReferralByID(ctx context.Context, tenantID, referralID string) (*UserReferral, error)
	// ListReferralsForInvitees returns referral attributions for the supplied
	// users in one tenant. It is used by user-management lists to avoid one
	// database query per rendered user.
	ListReferralsForInvitees(ctx context.Context, tenantID string, inviteeUserIDs []string) (map[string]*UserReferral, error)
	UpdateRewardGrants(ctx context.Context, tenantID, referralID, status, inviterGrantID, inviteeGrantID string, updatedAt time.Time) error
	TransitionReferralStatus(ctx context.Context, tenantID string, referralID string, fromStatuses []string, toStatus string, updatedAt time.Time) (bool, error)
	GetRegistrationIdempotency(ctx context.Context, tenantID, keyHash string, now time.Time) (*UserReferralRegistrationIdempotency, error)
	SaveRegistrationIdempotency(ctx context.Context, item *UserReferralRegistrationIdempotency) error
	GetRegistrationSession(ctx context.Context, tenantID, tokenHash string, now time.Time) (*UserReferralRegistrationSession, error)
	SaveRegistrationSession(ctx context.Context, item *UserReferralRegistrationSession) error
	MarkRegistrationSessionCompleted(ctx context.Context, tenantID, tokenHash, inviteeUserID, referralID string, completedAt time.Time) error
	// ExpireReservedReferrals atomically transitions overdue review records to
	// expired and appends their reserved -> expired audit history. It returns
	// the affected IDs so callers can emit operational metrics without reading
	// a second time. Terminal and reward-pipeline statuses are never touched.
	ExpireReservedReferrals(ctx context.Context, tenantID string, before, updatedAt time.Time) ([]string, error)
	// CleanupExpiredRegistrationArtifacts removes only expired, hash-only,
	// short-lived public-registration state. It never removes referrals,
	// status history, referral codes, or reward grants.
	CleanupExpiredRegistrationArtifacts(ctx context.Context, before time.Time) (UserReferralRegistrationCleanupResult, error)
	ReserveIdentity(ctx context.Context, item *UserReferralIdentityReservation, now time.Time) (bool, error)
	GetIdentityReservation(ctx context.Context, tenantID, identityHash string, now time.Time) (*UserReferralIdentityReservation, error)
	ReleaseIdentityReservation(ctx context.Context, tenantID, identityHash, sessionHash string) error
	CreateHandoff(ctx context.Context, item *UserReferralHandoff) error
	GetHandoff(ctx context.Context, tokenHash string, now time.Time) (*UserReferralHandoff, error)
	ConsumeHandoff(ctx context.Context, tokenHash string, usedAt time.Time) (bool, error)
	CreateStatusHistory(ctx context.Context, item *UserReferralStatusHistory) error
	ListStatusHistory(ctx context.Context, tenantID, referralID string) ([]*UserReferralStatusHistory, error)
	CountInviterRewardedOnOrAfter(ctx context.Context, tenantID, inviterUserID string, start time.Time) (int, error)
	// ListRewardRecoveryCandidates returns durable referral attributions whose
	// reward grants can be safely replayed after a transient registry failure.
	// An empty limit requests the complete tenant-scoped recovery set.
	ListRewardRecoveryCandidates(ctx context.Context, tenantID string, limit int) ([]*UserReferral, error)
	IncrementDailyMetric(ctx context.Context, tenantID, event string, occurredAt time.Time) error
	// RecordRewardMetricEvent records a grant lifecycle observation exactly once
	// for the supplied event key. It also increments the matching daily metric
	// in the same durable operation, so retries and process restarts cannot
	// inflate referral-reward usage or expiry reporting.
	RecordRewardMetricEvent(ctx context.Context, tenantID, eventKey, event string, occurredAt time.Time) (bool, error)
	ListDailyMetrics(ctx context.Context, tenantID string, from, to time.Time) ([]*UserReferralDailyMetric, error)
	// ListReservedReferrals returns risk-reviewed referrals for the tenant in a
	// stable order. It powers the tenant administrator's approval work queue.
	ListReservedReferrals(ctx context.Context, tenantID string, offset, limit int) ([]*UserReferralInvitee, int, error)
	// ListReferralInviteesForReview returns all registrations associated with an
	// inviter, including referrals awaiting risk review and terminal audit
	// records. It is deliberately separate from ListInvitees, whose narrower
	// contract powers the user-facing successful-invitation history.
	ListReferralInviteesForReview(ctx context.Context, filter UserReferralFilter) ([]*UserReferralInvitee, int, error)
	ListInviterSummaries(ctx context.Context, filter UserReferralFilter) ([]*UserReferralInviterSummary, int, error)
	ListInvitees(ctx context.Context, filter UserReferralFilter) ([]*UserReferralInvitee, int, error)
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
	RecordHeartbeat(ctx context.Context, tenantID, machineID, userID string, at time.Time) error
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
	UserReferrals   UserReferralRepository
	Machines        MachineRepository
	ViewerTokens    ViewerTokenRepository
	LoginTokens     LoginTokenRepository
	Sessions        SessionRepository
	WorkflowRepo    WorkflowRepository
	LLMPromptCache  LLMPromptCacheRepository
	KnowledgeShares KnowledgeShareRepository
	DigitalAssets   DigitalAssetRepository
}
