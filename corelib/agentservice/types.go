package agentservice

import (
	"context"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

type TenantStatus string

const (
	TenantStatusActive   TenantStatus = "active"
	TenantStatusDisabled TenantStatus = "disabled"
)

type UserStatus string

const (
	UserStatusActive   UserStatus = "active"
	UserStatusDisabled UserStatus = "disabled"
)

type CredentialStatus string

const (
	CredentialStatusActive    CredentialStatus = "active"
	CredentialStatusSuspended CredentialStatus = "suspended"
	CredentialStatusRevoked   CredentialStatus = "revoked"
)

type InstanceStatus string

const (
	InstanceStatusReady   InstanceStatus = "ready"
	InstanceStatusStopped InstanceStatus = "stopped"
)

type RunStatus string

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
)

type MessageRole string

const (
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleSystem    MessageRole = "system"
)

type Principal struct {
	TenantID string   `json:"tenant_id"`
	UserID   string   `json:"user_id"`
	Roles    []string `json:"roles,omitempty"`
}

type TenantQuota struct {
	MaxInstances int `json:"max_instances,omitempty"`
	MaxSessions  int `json:"max_sessions,omitempty"`
	MaxMessages  int `json:"max_messages,omitempty"`
	MaxRuns      int `json:"max_runs,omitempty"`
}

type Tenant struct {
	ID                     string       `json:"id"`
	Name                   string       `json:"name"`
	Status                 TenantStatus `json:"status"`
	Quota                  TenantQuota  `json:"quota,omitempty"`
	DeleteProtected        bool         `json:"delete_protected,omitempty"`
	DeleteProtectionReason string       `json:"delete_protection_reason,omitempty"`
	CreatedAt              time.Time    `json:"created_at"`
	UpdatedAt              time.Time    `json:"updated_at"`
}

type User struct {
	ID                     string      `json:"id"`
	TenantID               string      `json:"tenant_id"`
	Name                   string      `json:"name"`
	Email                  string      `json:"email,omitempty"`
	Status                 UserStatus  `json:"status"`
	Quota                  TenantQuota `json:"quota,omitempty"`
	DeleteProtected        bool        `json:"delete_protected,omitempty"`
	DeleteProtectionReason string      `json:"delete_protection_reason,omitempty"`
	CreatedAt              time.Time   `json:"created_at"`
	UpdatedAt              time.Time   `json:"updated_at"`
}

type Credential struct {
	ID           string           `json:"id"`
	TenantID     string           `json:"tenant_id"`
	UserID       string           `json:"user_id"`
	Name         string           `json:"name"`
	APIKey       string           `json:"api_key,omitempty"`
	APISecret    string           `json:"api_secret,omitempty"`
	APIKeyPrefix string           `json:"api_key_prefix,omitempty"`
	APIKeyHash   string           `json:"api_key_hash,omitempty"`
	Status       CredentialStatus `json:"status"`
	ExpiresAt    *time.Time       `json:"expires_at,omitempty"`
	TokenVersion int              `json:"token_version,omitempty"`
	SecretDigest string           `json:"-"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
}

type ParameterDefinition struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
	Secret      bool   `json:"secret,omitempty"`
	Type        string `json:"type"`
	Example     string `json:"example,omitempty"`
}

type UserConfig struct {
	TenantID  string            `json:"tenant_id"`
	UserID    string            `json:"user_id"`
	AppConfig corelib.AppConfig `json:"app_config"`
	UpdatedAt time.Time         `json:"updated_at"`
}

type ConfigValidationIssue struct {
	Key     string `json:"key"`
	Message string `json:"message"`
}

type ConfigValidationResult struct {
	Valid  bool                    `json:"valid"`
	Issues []ConfigValidationIssue `json:"issues,omitempty"`
}

type ConfigTestResult struct {
	Success      bool                    `json:"success"`
	Message      string                  `json:"message,omitempty"`
	Error        string                  `json:"error,omitempty"`
	LatencyMs    int64                   `json:"latency_ms,omitempty"`
	Endpoint     string                  `json:"endpoint,omitempty"`
	ProviderName string                  `json:"provider_name,omitempty"`
	Model        string                  `json:"model,omitempty"`
	Protocol     string                  `json:"protocol,omitempty"`
	WireAPI      string                  `json:"wire_api,omitempty"`
	Validation   *ConfigValidationResult `json:"validation,omitempty"`
}

type InstanceReadiness struct {
	Ready        bool   `json:"ready"`
	Reason       string `json:"reason"`
	ConfigValid  bool   `json:"config_valid"`
	HasLLMConfig bool   `json:"has_llm_config"`
}

type Instance struct {
	ID               string                 `json:"id"`
	TenantID         string                 `json:"tenant_id"`
	UserID           string                 `json:"user_id"`
	Name             string                 `json:"name"`
	DataDir          string                 `json:"data_dir"`
	RuntimeDir       string                 `json:"runtime_dir"`
	Workspace        string                 `json:"workspace_dir"`
	Status           InstanceStatus         `json:"status"`
	Ready            bool                   `json:"ready"`
	ReadyReason      string                 `json:"ready_reason,omitempty"`
	Readiness        InstanceReadiness      `json:"readiness"`
	Description      string                 `json:"description,omitempty"`
	Metadata         map[string]string      `json:"metadata,omitempty"`
	ConfigValidation ConfigValidationResult `json:"config_validation"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

type InstanceBootstrap struct {
	InstanceID            string            `json:"instance_id"`
	TenantID              string            `json:"tenant_id"`
	UserID                string            `json:"user_id"`
	DataDir               string            `json:"data_dir"`
	RuntimeDir            string            `json:"runtime_dir"`
	WorkspaceDir          string            `json:"workspace_dir"`
	ConfigPath            string            `json:"config_path"`
	ConversationStorePath string            `json:"conversation_store_path"`
	ConfirmationStorePath string            `json:"confirmation_store_path"`
	Metadata              map[string]string `json:"metadata,omitempty"`
	GeneratedAt           time.Time         `json:"generated_at"`
}

type SessionPendingAsk struct {
	Question  string   `json:"question,omitempty"`
	InputType string   `json:"input_type,omitempty"`
	Options   []string `json:"options,omitempty"`
}

type Session struct {
	ID             string             `json:"id"`
	TenantID       string             `json:"tenant_id"`
	UserID         string             `json:"user_id"`
	InstanceID     string             `json:"instance_id"`
	AgentID        string             `json:"agent_id"`
	Title          string             `json:"title,omitempty"`
	Metadata       map[string]string  `json:"metadata,omitempty"`
	Archived       bool               `json:"archived,omitempty"`
	ArchivedAt     *time.Time         `json:"archived_at,omitempty"`
	WaitingForUser bool               `json:"waiting_for_user,omitempty"`
	PendingAsk     *SessionPendingAsk `json:"pending_ask,omitempty"`
	LastMessageAt  *time.Time         `json:"last_message_at,omitempty"`
	CreatedAt      time.Time          `json:"created_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
}

type Message struct {
	ID         string            `json:"id"`
	SessionID  string            `json:"session_id"`
	TenantID   string            `json:"tenant_id"`
	UserID     string            `json:"user_id"`
	InstanceID string            `json:"instance_id"`
	Role       MessageRole       `json:"role"`
	InputType  string            `json:"input_type,omitempty"`
	OutputType string            `json:"output_type,omitempty"`
	Content    string            `json:"content"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
}

type Run struct {
	ID                 string            `json:"id"`
	TenantID           string            `json:"tenant_id"`
	UserID             string            `json:"user_id"`
	InstanceID         string            `json:"instance_id"`
	SessionID          string            `json:"session_id"`
	UserMessageID      string            `json:"user_message_id"`
	AssistantMessageID string            `json:"assistant_message_id,omitempty"`
	Status             RunStatus         `json:"status"`
	Error              string            `json:"error,omitempty"`
	ResponseSource     string            `json:"response_source,omitempty"`
	WaitingForUser     bool              `json:"waiting_for_user,omitempty"`
	DurationMs         int64             `json:"duration_ms,omitempty"`
	StartedAt          time.Time         `json:"started_at"`
	CompletedAt        *time.Time        `json:"completed_at,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
}

type AuditEvent struct {
	ID           string            `json:"id"`
	TenantID     string            `json:"tenant_id,omitempty"`
	UserID       string            `json:"user_id,omitempty"`
	ActorType    string            `json:"actor_type"`
	ActorTenant  string            `json:"actor_tenant_id,omitempty"`
	ActorUser    string            `json:"actor_user_id,omitempty"`
	Action       string            `json:"action"`
	ResourceType string            `json:"resource_type"`
	ResourceID   string            `json:"resource_id,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
}

type ListAuditEventsInput struct {
	TenantID     string     `json:"tenant_id,omitempty"`
	UserID       string     `json:"user_id,omitempty"`
	Action       string     `json:"action,omitempty"`
	ResourceType string     `json:"resource_type,omitempty"`
	ResourceID   string     `json:"resource_id,omitempty"`
	ActorType    string     `json:"actor_type,omitempty"`
	Since        *time.Time `json:"since,omitempty"`
	Until        *time.Time `json:"until,omitempty"`
}

type ListSessionsInput struct {
	IncludeArchived bool `json:"include_archived,omitempty"`
}

type ListMessagesInput struct {
	Role  MessageRole `json:"role,omitempty"`
	Since *time.Time  `json:"since,omitempty"`
	Until *time.Time  `json:"until,omitempty"`
}

type ListRunsInput struct {
	Status         RunStatus `json:"status,omitempty"`
	SessionID      string    `json:"session_id,omitempty"`
	ResponseSource string    `json:"response_source,omitempty"`
	WaitingForUser *bool     `json:"waiting_for_user,omitempty"`
}

type QuotaUsageItem struct {
	Limit     int  `json:"limit,omitempty"`
	Used      int  `json:"used"`
	Remaining *int `json:"remaining,omitempty"`
	Unlimited bool `json:"unlimited,omitempty"`
}

type QuotaUsageSnapshot struct {
	Instances QuotaUsageItem `json:"instances"`
	Sessions  QuotaUsageItem `json:"sessions"`
	Messages  QuotaUsageItem `json:"messages"`
	Runs      QuotaUsageItem `json:"runs"`
}

type UsageSummary struct {
	TenantID             string             `json:"tenant_id"`
	UserID               string             `json:"user_id"`
	DataDir              string             `json:"data_dir"`
	Quota                TenantQuota        `json:"quota,omitempty"`
	QuotaUsage           QuotaUsageSnapshot `json:"quota_usage"`
	Instances            int                `json:"instances"`
	ReadyInstances       int                `json:"ready_instances"`
	StoppedInstances     int                `json:"stopped_instances"`
	Sessions             int                `json:"sessions"`
	Messages             int                `json:"messages"`
	UserMessages         int                `json:"user_messages"`
	AssistantMessages    int                `json:"assistant_messages"`
	Runs                 int                `json:"runs"`
	RunsByStatus         map[RunStatus]int  `json:"runs_by_status"`
	Credentials          int                `json:"credentials"`
	ActiveCredentials    int                `json:"active_credentials"`
	SuspendedCredentials int                `json:"suspended_credentials"`
	RevokedCredentials   int                `json:"revoked_credentials"`
	ExpiredCredentials   int                `json:"expired_credentials"`
	ExpiringCredentials  int                `json:"expiring_credentials"`
	LastActivityAt       *time.Time         `json:"last_activity_at,omitempty"`
}

type TenantUserSummary struct {
	UserID               string             `json:"user_id"`
	Name                 string             `json:"name"`
	Email                string             `json:"email,omitempty"`
	Status               UserStatus         `json:"status"`
	DataDir              string             `json:"data_dir"`
	Quota                TenantQuota        `json:"quota,omitempty"`
	EffectiveQuota       TenantQuota        `json:"effective_quota,omitempty"`
	QuotaUsage           QuotaUsageSnapshot `json:"quota_usage"`
	Instances            int                `json:"instances"`
	ReadyInstances       int                `json:"ready_instances"`
	StoppedInstances     int                `json:"stopped_instances"`
	Sessions             int                `json:"sessions"`
	Messages             int                `json:"messages"`
	UserMessages         int                `json:"user_messages"`
	AssistantMessages    int                `json:"assistant_messages"`
	Runs                 int                `json:"runs"`
	RunsByStatus         map[RunStatus]int  `json:"runs_by_status"`
	Credentials          int                `json:"credentials"`
	ActiveCredentials    int                `json:"active_credentials"`
	SuspendedCredentials int                `json:"suspended_credentials"`
	RevokedCredentials   int                `json:"revoked_credentials"`
	ExpiredCredentials   int                `json:"expired_credentials"`
	ExpiringCredentials  int                `json:"expiring_credentials"`
	LastActivityAt       *time.Time         `json:"last_activity_at,omitempty"`
}

type TenantSummary struct {
	TenantID             string              `json:"tenant_id"`
	Name                 string              `json:"name"`
	Status               TenantStatus        `json:"status"`
	Quota                TenantQuota         `json:"quota,omitempty"`
	QuotaUsage           QuotaUsageSnapshot  `json:"quota_usage"`
	Users                int                 `json:"users"`
	ActiveUsers          int                 `json:"active_users"`
	DisabledUsers        int                 `json:"disabled_users"`
	Instances            int                 `json:"instances"`
	ReadyInstances       int                 `json:"ready_instances"`
	StoppedInstances     int                 `json:"stopped_instances"`
	Sessions             int                 `json:"sessions"`
	Messages             int                 `json:"messages"`
	UserMessages         int                 `json:"user_messages"`
	AssistantMessages    int                 `json:"assistant_messages"`
	Runs                 int                 `json:"runs"`
	RunsByStatus         map[RunStatus]int   `json:"runs_by_status"`
	Credentials          int                 `json:"credentials"`
	ActiveCredentials    int                 `json:"active_credentials"`
	SuspendedCredentials int                 `json:"suspended_credentials"`
	RevokedCredentials   int                 `json:"revoked_credentials"`
	ExpiredCredentials   int                 `json:"expired_credentials"`
	ExpiringCredentials  int                 `json:"expiring_credentials"`
	LastActivityAt       *time.Time          `json:"last_activity_at,omitempty"`
	UserSummaries        []TenantUserSummary `json:"user_summaries,omitempty"`
}

type AdminOverview struct {
	Tenants              int               `json:"tenants"`
	ActiveTenants        int               `json:"active_tenants"`
	DisabledTenants      int               `json:"disabled_tenants"`
	Users                int               `json:"users"`
	ActiveUsers          int               `json:"active_users"`
	DisabledUsers        int               `json:"disabled_users"`
	Credentials          int               `json:"credentials"`
	ActiveCredentials    int               `json:"active_credentials"`
	SuspendedCredentials int               `json:"suspended_credentials"`
	RevokedCredentials   int               `json:"revoked_credentials"`
	ExpiredCredentials   int               `json:"expired_credentials"`
	ExpiringCredentials  int               `json:"expiring_credentials"`
	Instances            int               `json:"instances"`
	ReadyInstances       int               `json:"ready_instances"`
	StoppedInstances     int               `json:"stopped_instances"`
	Sessions             int               `json:"sessions"`
	Messages             int               `json:"messages"`
	UserMessages         int               `json:"user_messages"`
	AssistantMessages    int               `json:"assistant_messages"`
	Runs                 int               `json:"runs"`
	RunsByStatus         map[RunStatus]int `json:"runs_by_status"`
	AuditEvents          int               `json:"audit_events"`
	Snapshots            int               `json:"snapshots"`
	SnapshotBytes        int64             `json:"snapshot_bytes"`
	LastActivityAt       *time.Time        `json:"last_activity_at,omitempty"`
	LastAuditAt          *time.Time        `json:"last_audit_at,omitempty"`
}

type AdminTrendPoint struct {
	BucketStart  time.Time         `json:"bucket_start"`
	Messages     int               `json:"messages"`
	Runs         int               `json:"runs"`
	RunsByStatus map[RunStatus]int `json:"runs_by_status,omitempty"`
	AuditEvents  int               `json:"audit_events"`
}

type AdminDashboard struct {
	Overview          AdminOverview     `json:"overview"`
	RecentAuditEvents []AuditEvent      `json:"recent_audit_events,omitempty"`
	Last24Hours       []AdminTrendPoint `json:"last_24_hours,omitempty"`
	Last7Days         []AdminTrendPoint `json:"last_7_days,omitempty"`
	GeneratedAt       time.Time         `json:"generated_at"`
}

type AdminInsightsInput struct {
	InactiveForDays int `json:"inactive_for_days,omitempty"`
	Limit           int `json:"limit,omitempty"`
}

type AdminTenantInsight struct {
	TenantID       string       `json:"tenant_id"`
	Name           string       `json:"name"`
	Status         TenantStatus `json:"status"`
	Users          int          `json:"users"`
	ActiveUsers    int          `json:"active_users"`
	Instances      int          `json:"instances"`
	Messages       int          `json:"messages"`
	Runs           int          `json:"runs"`
	ActivityScore  int          `json:"activity_score"`
	LastActivityAt *time.Time   `json:"last_activity_at,omitempty"`
}

type AdminInactiveUserInsight struct {
	TenantID       string     `json:"tenant_id"`
	UserID         string     `json:"user_id"`
	Name           string     `json:"name"`
	Email          string     `json:"email,omitempty"`
	Status         UserStatus `json:"status"`
	Instances      int        `json:"instances"`
	Messages       int        `json:"messages"`
	Runs           int        `json:"runs"`
	LastActivityAt *time.Time `json:"last_activity_at,omitempty"`
	InactiveDays   int        `json:"inactive_days"`
	Reason         string     `json:"reason"`
}

type AdminQuotaPressureInsight struct {
	Scope          string     `json:"scope"`
	Metric         string     `json:"metric"`
	TenantID       string     `json:"tenant_id"`
	TenantName     string     `json:"tenant_name,omitempty"`
	UserID         string     `json:"user_id,omitempty"`
	UserName       string     `json:"user_name,omitempty"`
	Limit          int        `json:"limit"`
	Used           int        `json:"used"`
	Remaining      *int       `json:"remaining,omitempty"`
	PressureRatio  float64    `json:"pressure_ratio"`
	Status         string     `json:"status"`
	LastActivityAt *time.Time `json:"last_activity_at,omitempty"`
}

type AdminInsights struct {
	GeneratedAt    time.Time                   `json:"generated_at"`
	InactiveCutoff time.Time                   `json:"inactive_cutoff"`
	TopTenants     []AdminTenantInsight        `json:"top_tenants,omitempty"`
	InactiveUsers  []AdminInactiveUserInsight  `json:"inactive_users,omitempty"`
	QuotaPressure  []AdminQuotaPressureInsight `json:"quota_pressure,omitempty"`
}

type DeleteBlocker struct {
	Kind       string `json:"kind"`
	TenantID   string `json:"tenant_id,omitempty"`
	UserID     string `json:"user_id,omitempty"`
	InstanceID string `json:"instance_id,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	RunID      string `json:"run_id,omitempty"`
	Reason     string `json:"reason"`
}

type TenantDeleteCheck struct {
	TenantID               string          `json:"tenant_id"`
	CanDelete              bool            `json:"can_delete"`
	DeleteProtected        bool            `json:"delete_protected,omitempty"`
	DeleteProtectionReason string          `json:"delete_protection_reason,omitempty"`
	Users                  int             `json:"users"`
	Credentials            int             `json:"credentials"`
	Instances              int             `json:"instances"`
	Sessions               int             `json:"sessions"`
	Messages               int             `json:"messages"`
	Runs                   int             `json:"runs"`
	Blockers               []DeleteBlocker `json:"blockers,omitempty"`
	GeneratedAt            time.Time       `json:"generated_at"`
}

type UserDeleteCheck struct {
	TenantID               string          `json:"tenant_id"`
	UserID                 string          `json:"user_id"`
	CanDelete              bool            `json:"can_delete"`
	DeleteProtected        bool            `json:"delete_protected,omitempty"`
	DeleteProtectionReason string          `json:"delete_protection_reason,omitempty"`
	Credentials            int             `json:"credentials"`
	Instances              int             `json:"instances"`
	Sessions               int             `json:"sessions"`
	Messages               int             `json:"messages"`
	Runs                   int             `json:"runs"`
	Blockers               []DeleteBlocker `json:"blockers,omitempty"`
	GeneratedAt            time.Time       `json:"generated_at"`
}

type TenantRetirePlan struct {
	DeleteCheck TenantDeleteCheck        `json:"delete_check"`
	Export      ExportServiceStateOutput `json:"export"`
	GeneratedAt time.Time                `json:"generated_at"`
}

type UserRetirePlan struct {
	DeleteCheck UserDeleteCheck          `json:"delete_check"`
	Export      ExportServiceStateOutput `json:"export"`
	GeneratedAt time.Time                `json:"generated_at"`
}

type AdminAlertItem struct {
	Kind            string     `json:"kind"`
	Severity        string     `json:"severity"`
	Title           string     `json:"title"`
	SuggestedAction string     `json:"suggested_action,omitempty"`
	TenantID        string     `json:"tenant_id,omitempty"`
	UserID          string     `json:"user_id,omitempty"`
	InstanceID      string     `json:"instance_id,omitempty"`
	SessionID       string     `json:"session_id,omitempty"`
	RunID           string     `json:"run_id,omitempty"`
	CredentialID    string     `json:"credential_id,omitempty"`
	OccurredAt      *time.Time `json:"occurred_at,omitempty"`
	Reason          string     `json:"reason,omitempty"`
}

type AdminAlertsInput struct {
	TenantID                   string     `json:"tenant_id,omitempty"`
	UserID                     string     `json:"user_id,omitempty"`
	Kind                       string     `json:"kind,omitempty"`
	Since                      *time.Time `json:"since,omitempty"`
	Limit                      int        `json:"limit,omitempty"`
	CredentialExpiryWindowDays int        `json:"credential_expiry_window_days,omitempty"`
}

type AdminAlerts struct {
	Items            []AdminAlertItem `json:"items,omitempty"`
	UnreadyInstances []Instance       `json:"unready_instances,omitempty"`
	WaitingRuns      []Run            `json:"waiting_runs,omitempty"`
	FailedRuns       []Run            `json:"failed_runs,omitempty"`
	CredentialAlerts []Credential     `json:"credential_alerts,omitempty"`
	GeneratedAt      time.Time        `json:"generated_at"`
}

type ExportServiceStateInput struct {
	TenantID        string `json:"tenant_id,omitempty"`
	UserID          string `json:"user_id,omitempty"`
	IncludeMessages bool   `json:"include_messages,omitempty"`
	IncludeRuns     bool   `json:"include_runs,omitempty"`
	IncludeAudit    bool   `json:"include_audit,omitempty"`
	IncludeSecrets  bool   `json:"include_secrets,omitempty"`
}

type ExportedCredential struct {
	ID           string           `json:"id"`
	TenantID     string           `json:"tenant_id"`
	UserID       string           `json:"user_id"`
	Name         string           `json:"name"`
	APIKey       string           `json:"api_key,omitempty"`
	APIKeyPrefix string           `json:"api_key_prefix,omitempty"`
	APIKeyHash   string           `json:"api_key_hash,omitempty"`
	Status       CredentialStatus `json:"status"`
	ExpiresAt    *time.Time       `json:"expires_at,omitempty"`
	TokenVersion int              `json:"token_version,omitempty"`
	SecretDigest string           `json:"secret_digest,omitempty"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
}

type ExportedSession struct {
	Session  Session   `json:"session"`
	Messages []Message `json:"messages,omitempty"`
}

type ExportedInstance struct {
	Instance Instance          `json:"instance"`
	Sessions []ExportedSession `json:"sessions,omitempty"`
	Runs     []Run             `json:"runs,omitempty"`
}

type ExportedUser struct {
	User        User                 `json:"user"`
	Config      *UserConfig          `json:"config,omitempty"`
	Credentials []ExportedCredential `json:"credentials,omitempty"`
	Instances   []ExportedInstance   `json:"instances,omitempty"`
}

type ExportServiceStateOutput struct {
	Scope           string         `json:"scope"`
	TenantID        string         `json:"tenant_id,omitempty"`
	UserID          string         `json:"user_id,omitempty"`
	IncludeMessages bool           `json:"include_messages"`
	IncludeRuns     bool           `json:"include_runs"`
	IncludeAudit    bool           `json:"include_audit"`
	IncludeSecrets  bool           `json:"include_secrets"`
	Tenants         []Tenant       `json:"tenants,omitempty"`
	Users           []ExportedUser `json:"users,omitempty"`
	AuditEvents     []AuditEvent   `json:"audit_events,omitempty"`
	ExportedAt      time.Time      `json:"exported_at"`
}

type CreateServiceSnapshotInput struct {
	Name            string `json:"name,omitempty"`
	TenantID        string `json:"tenant_id,omitempty"`
	UserID          string `json:"user_id,omitempty"`
	IncludeMessages *bool  `json:"include_messages,omitempty"`
	IncludeRuns     *bool  `json:"include_runs,omitempty"`
	IncludeAudit    *bool  `json:"include_audit,omitempty"`
	IncludeSecrets  *bool  `json:"include_secrets,omitempty"`
}

type ListServiceSnapshotsInput struct {
	TenantID string `json:"tenant_id,omitempty"`
	UserID   string `json:"user_id,omitempty"`
}

type ServiceSnapshot struct {
	ID              string    `json:"id"`
	Name            string    `json:"name,omitempty"`
	Scope           string    `json:"scope"`
	TenantID        string    `json:"tenant_id,omitempty"`
	UserID          string    `json:"user_id,omitempty"`
	Path            string    `json:"path,omitempty"`
	SizeBytes       int64     `json:"size_bytes"`
	IncludeMessages bool      `json:"include_messages"`
	IncludeRuns     bool      `json:"include_runs"`
	IncludeAudit    bool      `json:"include_audit"`
	IncludeSecrets  bool      `json:"include_secrets"`
	CreatedAt       time.Time `json:"created_at"`
}

type ServiceSnapshotEnvelope struct {
	Snapshot ServiceSnapshot          `json:"snapshot"`
	Data     ExportServiceStateOutput `json:"data"`
}

type PruneServiceSnapshotsInput struct {
	TenantID   string     `json:"tenant_id,omitempty"`
	UserID     string     `json:"user_id,omitempty"`
	OlderThan  *time.Time `json:"older_than,omitempty"`
	KeepLatest int        `json:"keep_latest,omitempty"`
	DryRun     bool       `json:"dry_run,omitempty"`
}

type PruneServiceSnapshotsOutput struct {
	TenantID      string            `json:"tenant_id,omitempty"`
	UserID        string            `json:"user_id,omitempty"`
	OlderThan     *time.Time        `json:"older_than,omitempty"`
	KeepLatest    int               `json:"keep_latest,omitempty"`
	DryRun        bool              `json:"dry_run"`
	Matched       int               `json:"matched"`
	Deleted       int               `json:"deleted"`
	FreedBytes    int64             `json:"freed_bytes"`
	KeptSnapshots []ServiceSnapshot `json:"kept_snapshots,omitempty"`
	Snapshots     []ServiceSnapshot `json:"snapshots,omitempty"`
	GeneratedAt   time.Time         `json:"generated_at"`
}

type RestoreServiceSnapshotInput struct {
	Overwrite bool `json:"overwrite,omitempty"`
	DryRun    bool `json:"dry_run,omitempty"`
}

type RestoreServiceSnapshotOutput struct {
	Snapshot ServiceSnapshot          `json:"snapshot"`
	Import   ImportServiceStateOutput `json:"import"`
}

type ImportServiceStateRequest struct {
	Data      ExportServiceStateOutput `json:"data"`
	Overwrite bool                     `json:"overwrite,omitempty"`
	DryRun    bool                     `json:"dry_run,omitempty"`
}

type ImportPlanItem struct {
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	Action       string `json:"action"`
	Message      string `json:"message,omitempty"`
}

type ImportServiceStateOutput struct {
	Scope       string           `json:"scope"`
	TenantID    string           `json:"tenant_id,omitempty"`
	UserID      string           `json:"user_id,omitempty"`
	Overwrite   bool             `json:"overwrite"`
	DryRun      bool             `json:"dry_run"`
	Tenants     int              `json:"tenants"`
	Users       int              `json:"users"`
	Credentials int              `json:"credentials"`
	Instances   int              `json:"instances"`
	Sessions    int              `json:"sessions"`
	Messages    int              `json:"messages"`
	Runs        int              `json:"runs"`
	AuditEvents int              `json:"audit_events"`
	Conflicts   []string         `json:"conflicts,omitempty"`
	Warnings    []string         `json:"warnings,omitempty"`
	Plan        []ImportPlanItem `json:"plan,omitempty"`
	ImportedAt  time.Time        `json:"imported_at"`
}

type InstanceSummary struct {
	InstanceID        string            `json:"instance_id"`
	TenantID          string            `json:"tenant_id"`
	UserID            string            `json:"user_id"`
	Status            InstanceStatus    `json:"status"`
	Ready             bool              `json:"ready"`
	ReadyReason       string            `json:"ready_reason,omitempty"`
	Sessions          int               `json:"sessions"`
	ArchivedSessions  int               `json:"archived_sessions"`
	WaitingSessions   int               `json:"waiting_sessions"`
	Messages          int               `json:"messages"`
	UserMessages      int               `json:"user_messages"`
	AssistantMessages int               `json:"assistant_messages"`
	Runs              int               `json:"runs"`
	WaitingRuns       int               `json:"waiting_runs"`
	RunsByStatus      map[RunStatus]int `json:"runs_by_status"`
	LastActivityAt    *time.Time        `json:"last_activity_at,omitempty"`
}

type CreateTenantInput struct {
	Name                   string `json:"name"`
	DeleteProtected        bool   `json:"delete_protected,omitempty"`
	DeleteProtectionReason string `json:"delete_protection_reason,omitempty"`
}

type ListTenantsInput struct {
	Status TenantStatus `json:"status,omitempty"`
	Name   string       `json:"name,omitempty"`
}

type ListUsersAdminInput struct {
	Status UserStatus `json:"status,omitempty"`
	Name   string     `json:"name,omitempty"`
	Email  string     `json:"email,omitempty"`
}

type ListAllUsersAdminInput struct {
	TenantID string     `json:"tenant_id,omitempty"`
	Status   UserStatus `json:"status,omitempty"`
	Name     string     `json:"name,omitempty"`
	Email    string     `json:"email,omitempty"`
}

type CreateUserInput struct {
	TenantID               string `json:"tenant_id"`
	Name                   string `json:"name"`
	Email                  string `json:"email,omitempty"`
	DeleteProtected        bool   `json:"delete_protected,omitempty"`
	DeleteProtectionReason string `json:"delete_protection_reason,omitempty"`
}

type UpdateTenantInput struct {
	Name                   *string       `json:"name,omitempty"`
	Status                 *TenantStatus `json:"status,omitempty"`
	DeleteProtected        *bool         `json:"delete_protected,omitempty"`
	DeleteProtectionReason *string       `json:"delete_protection_reason,omitempty"`
	MaxInstances           *int          `json:"max_instances,omitempty"`
	MaxSessions            *int          `json:"max_sessions,omitempty"`
	MaxMessages            *int          `json:"max_messages,omitempty"`
	MaxRuns                *int          `json:"max_runs,omitempty"`
}

type UpdateUserInput struct {
	Name                   *string     `json:"name,omitempty"`
	Email                  *string     `json:"email,omitempty"`
	Status                 *UserStatus `json:"status,omitempty"`
	DeleteProtected        *bool       `json:"delete_protected,omitempty"`
	DeleteProtectionReason *string     `json:"delete_protection_reason,omitempty"`
	MaxInstances           *int        `json:"max_instances,omitempty"`
	MaxSessions            *int        `json:"max_sessions,omitempty"`
	MaxMessages            *int        `json:"max_messages,omitempty"`
	MaxRuns                *int        `json:"max_runs,omitempty"`
}

type CreateCredentialInput struct {
	TenantID  string     `json:"tenant_id"`
	UserID    string     `json:"user_id"`
	Name      string     `json:"name"`
	APIKey    string     `json:"api_key,omitempty"`
	APISecret string     `json:"api_secret,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type UpdateCredentialInput struct {
	Name           *string           `json:"name,omitempty"`
	Status         *CredentialStatus `json:"status,omitempty"`
	ExpiresAt      *time.Time        `json:"expires_at,omitempty"`
	ClearExpiresAt bool              `json:"clear_expires_at,omitempty"`
}

type RotateCredentialSecretInput struct {
	APISecret string `json:"api_secret,omitempty"`
}

type RotateCredentialKeyInput struct {
	APIKey string `json:"api_key,omitempty"`
}

type IssueTokenInput struct {
	APIKey    string `json:"api_key,omitempty"`
	APISecret string `json:"api_secret,omitempty"`
}

type IssueTokenOutput struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresAt   time.Time `json:"expires_at"`
	Principal   Principal `json:"principal"`
}

type CreateInstanceInput struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type UpdateInstanceInput struct {
	Name        *string           `json:"name,omitempty"`
	Description *string           `json:"description,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type CreateSessionInput struct {
	AgentID  string            `json:"agent_id"`
	Title    string            `json:"title,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type UpdateSessionInput struct {
	Title    *string           `json:"title,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type PostMessageInput struct {
	Content   string            `json:"content"`
	InputType string            `json:"input_type,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type SendMessageInput struct {
	SessionID        string            `json:"session_id,omitempty"`
	AgentID          string            `json:"agent_id,omitempty"`
	Title            string            `json:"title,omitempty"`
	Content          string            `json:"content"`
	InputType        string            `json:"input_type,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	SessionMetadata  map[string]string `json:"session_metadata,omitempty"`
	ClientSessionKey string            `json:"client_session_key,omitempty"`
	ClientMessageID  string            `json:"client_message_id,omitempty"`
}

type AgentToolCapability struct {
	Name           string                 `json:"name"`
	Description    string                 `json:"description,omitempty"`
	Enabled        bool                   `json:"enabled"`
	DisabledReason string                 `json:"disabled_reason,omitempty"`
	Parameters     map[string]interface{} `json:"parameters,omitempty"`
}

type AgentCapabilities struct {
	Executor          string                `json:"executor"`
	SupportsSessions  bool                  `json:"supports_sessions"`
	SupportsAskUser   bool                  `json:"supports_ask_user"`
	SupportsSSH       bool                  `json:"supports_ssh"`
	SupportsLocalBash bool                  `json:"supports_local_bash"`
	Tools             []AgentToolCapability `json:"tools,omitempty"`
	Metadata          map[string]string     `json:"metadata,omitempty"`
}

type ExecuteRequest struct {
	Principal Principal
	Tenant    Tenant
	User      User
	Instance  Instance
	Session   Session
	Message   Message
	History   []Message
	DataDir   string
	Config    corelib.AppConfig
}

type ExecuteResult struct {
	Content    string            `json:"content"`
	OutputType string            `json:"output_type,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type Executor interface {
	Execute(ctx context.Context, req ExecuteRequest) (*ExecuteResult, error)
}

type CapabilityDescriber interface {
	DescribeCapabilities(ctx context.Context, req ExecuteRequest) (*AgentCapabilities, error)
}
