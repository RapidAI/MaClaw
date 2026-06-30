package structureddata

import "time"

type Principal struct {
	TenantID   string        `json:"tenant_id"`
	UserID     string        `json:"user_id,omitempty"`
	Role       string        `json:"role,omitempty"`
	AdminScope string        `json:"admin_scope,omitempty"`
	APIKeyID   string        `json:"api_key_id,omitempty"`
	Policy     *APIKeyPolicy `json:"-"`
}

type APIKeyPolicy struct {
	ID                string   `json:"id"`
	Key               string   `json:"key,omitempty"`
	TenantID          string   `json:"tenant_id,omitempty"`
	UserID            string   `json:"user_id,omitempty"`
	Role              string   `json:"role,omitempty"`
	AllowedDomains    []string `json:"allowed_domains,omitempty"`
	AllowedDatasets   []string `json:"allowed_datasets,omitempty"`
	AllowedActions    []string `json:"allowed_actions,omitempty"`
	AllowedViews      []string `json:"allowed_views,omitempty"`
	AllowedReports    []string `json:"allowed_reports,omitempty"`
	AllowedDashboards []string `json:"allowed_dashboards,omitempty"`
	AllowRawData      bool     `json:"allow_raw_data,omitempty"`
	AllowSensitive    bool     `json:"allow_sensitive,omitempty"`
	AllowAdmin        bool     `json:"allow_admin,omitempty"`
}

type APIKeyPolicyRecord struct {
	ID                string     `json:"id"`
	TenantID          string     `json:"tenant_id"`
	UserID            string     `json:"user_id,omitempty"`
	Role              string     `json:"role,omitempty"`
	KeyPrefix         string     `json:"key_prefix,omitempty"`
	Enabled           bool       `json:"enabled"`
	Status            string     `json:"status,omitempty"`
	ExpiresInDays     int        `json:"expires_in_days,omitempty"`
	AllowedDomains    []string   `json:"allowed_domains,omitempty"`
	AllowedDatasets   []string   `json:"allowed_datasets,omitempty"`
	AllowedActions    []string   `json:"allowed_actions,omitempty"`
	AllowedViews      []string   `json:"allowed_views,omitempty"`
	AllowedReports    []string   `json:"allowed_reports,omitempty"`
	AllowedDashboards []string   `json:"allowed_dashboards,omitempty"`
	AllowRawData      bool       `json:"allow_raw_data,omitempty"`
	AllowSensitive    bool       `json:"allow_sensitive,omitempty"`
	AllowAdmin        bool       `json:"allow_admin,omitempty"`
	Note              string     `json:"note,omitempty"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	LastUsedAt        *time.Time `json:"last_used_at,omitempty"`
	LastUsedIP        string     `json:"last_used_ip,omitempty"`
	LastUsedUserAgent string     `json:"last_used_user_agent,omitempty"`
	CreatedBy         string     `json:"created_by,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type SetupStatus struct {
	Initialized     bool                   `json:"initialized"`
	TenantID        string                 `json:"tenant_id,omitempty"`
	Mode            string                 `json:"mode,omitempty"`
	AdminScopes     []string               `json:"admin_scopes,omitempty"`
	Tenants         []DataTenantInfo       `json:"tenants,omitempty"`
	HubRegistration *HubRegistrationStatus `json:"hub_registration,omitempty"`
	PasswordPolicy  *AdminPasswordPolicy   `json:"password_policy,omitempty"`
}

type DataTenantInfo struct {
	ID                string    `json:"id"`
	HubTenantID       string    `json:"hub_tenant_id,omitempty"`
	Slug              string    `json:"slug,omitempty"`
	Name              string    `json:"name,omitempty"`
	Status            string    `json:"status,omitempty"`
	PrimaryDomain     string    `json:"primary_domain,omitempty"`
	Domains           []string  `json:"domains,omitempty"`
	VirtualMailDomain string    `json:"virtual_mail_domain,omitempty"`
	Source            string    `json:"source,omitempty"`
	SyncedAt          time.Time `json:"synced_at,omitempty"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type SyncHubTenantsInput struct {
	Source  string           `json:"source,omitempty"`
	Tenants []DataTenantInfo `json:"tenants"`
}

type SyncHubTenantsResult struct {
	Synced  int              `json:"synced"`
	Tenants []DataTenantInfo `json:"tenants"`
}

type HubRegistrationStatus struct {
	Configured        bool       `json:"configured"`
	Registered        bool       `json:"registered"`
	HubBaseURL        string     `json:"hub_base_url,omitempty"`
	PlatformID        string     `json:"platform_id,omitempty"`
	PlatformName      string     `json:"platform_name,omitempty"`
	CallbackBaseURL   string     `json:"callback_base_url,omitempty"`
	VirtualMailDomain string     `json:"virtual_mail_domain,omitempty"`
	LastRegisteredAt  *time.Time `json:"last_registered_at,omitempty"`
	LastSyncedAt      *time.Time `json:"last_synced_at,omitempty"`
	LastError         string     `json:"last_error,omitempty"`
}

type SaveHubRegistrationInput struct {
	HubBaseURL        string `json:"hub_base_url"`
	PlatformID        string `json:"platform_id,omitempty"`
	PlatformName      string `json:"platform_name,omitempty"`
	CallbackBaseURL   string `json:"callback_base_url,omitempty"`
	VirtualMailDomain string `json:"virtual_mail_domain,omitempty"`
}

type HubRegistrationResult struct {
	Status HubRegistrationStatus `json:"status"`
}

type AdminPasswordPolicy struct {
	MinLength             int  `json:"min_length"`
	RotationDays          int  `json:"rotation_days,omitempty"`
	LockoutEnabled        bool `json:"lockout_enabled"`
	LoginMaxFailures      int  `json:"login_max_failures,omitempty"`
	LoginLockoutMinutes   int  `json:"login_lockout_minutes,omitempty"`
	OfflineResetAvailable bool `json:"offline_reset_available"`
}

type InitializeAdminInput struct {
	TenantID     string `json:"tenant_id,omitempty"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	DisplayName  string `json:"display_name,omitempty"`
	ExpiresHours int    `json:"expires_hours,omitempty"`
}

type InitializeAdminResult struct {
	Initialized bool      `json:"initialized"`
	TenantID    string    `json:"tenant_id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name,omitempty"`
	Role        string    `json:"role"`
	AdminScope  string    `json:"admin_scope,omitempty"`
	Token       string    `json:"token,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
}

type LoginInput struct {
	TenantID     string `json:"tenant_id,omitempty"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	ExpiresHours int    `json:"expires_hours,omitempty"`
}

type LoginResult struct {
	TenantID   string    `json:"tenant_id"`
	Username   string    `json:"username"`
	Role       string    `json:"role"`
	AdminScope string    `json:"admin_scope,omitempty"`
	Token      string    `json:"token"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type AdminAccountInfo struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	Username    string     `json:"username"`
	DisplayName string     `json:"display_name,omitempty"`
	Role        string     `json:"role"`
	AdminScope  string     `json:"admin_scope,omitempty"`
	Enabled     bool       `json:"enabled"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type ListAdminAccountsResult struct {
	Items []AdminAccountInfo `json:"items"`
}

type CreateAdminAccountInput struct {
	TenantID    string `json:"tenant_id,omitempty"`
	AdminScope  string `json:"admin_scope,omitempty"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name,omitempty"`
	Role        string `json:"role,omitempty"`
}

type UpdateAdminAccountInput struct {
	DisplayName *string `json:"display_name,omitempty"`
	Role        string  `json:"role,omitempty"`
	AdminScope  string  `json:"admin_scope,omitempty"`
	Enabled     *bool   `json:"enabled,omitempty"`
}

type AdminAccountResult struct {
	Account AdminAccountInfo `json:"account"`
}

type AdminSessionInfo struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id"`
	UserID     string    `json:"user_id"`
	Username   string    `json:"username"`
	Role       string    `json:"role"`
	AdminScope string    `json:"admin_scope,omitempty"`
	Current    bool      `json:"current,omitempty"`
	ExpiresAt  time.Time `json:"expires_at"`
	CreatedAt  time.Time `json:"created_at"`
}

type ListAdminSessionsResult struct {
	Items []AdminSessionInfo `json:"items"`
}

type UpdateAdminSessionInput struct {
	ExpiresHours int        `json:"expires_hours,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

type AdminSessionResult struct {
	Session AdminSessionInfo `json:"session"`
}

type RevokeAdminSessionResult struct {
	SessionID string `json:"session_id"`
	Revoked   bool   `json:"revoked"`
}

type ResetAdminPasswordInput struct {
	TenantID string `json:"tenant_id,omitempty"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type ResetAdminPasswordResult struct {
	TenantID  string    `json:"tenant_id"`
	Username  string    `json:"username"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateAPIKeyPolicyInput struct {
	ID                string   `json:"id,omitempty"`
	Key               string   `json:"key,omitempty"`
	UserID            string   `json:"user_id,omitempty"`
	Role              string   `json:"role,omitempty"`
	AllowedDomains    []string `json:"allowed_domains,omitempty"`
	AllowedDatasets   []string `json:"allowed_datasets,omitempty"`
	AllowedActions    []string `json:"allowed_actions,omitempty"`
	AllowedViews      []string `json:"allowed_views,omitempty"`
	AllowedReports    []string `json:"allowed_reports,omitempty"`
	AllowedDashboards []string `json:"allowed_dashboards,omitempty"`
	AllowRawData      bool     `json:"allow_raw_data,omitempty"`
	AllowSensitive    bool     `json:"allow_sensitive,omitempty"`
	AllowAdmin        bool     `json:"allow_admin,omitempty"`
	Note              string   `json:"note,omitempty"`
	ExpiresAt         string   `json:"expires_at,omitempty"`
}

type UpdateAPIKeyPolicyInput struct {
	UserID            *string  `json:"user_id,omitempty"`
	Role              string   `json:"role,omitempty"`
	Enabled           *bool    `json:"enabled,omitempty"`
	AllowedDomains    []string `json:"allowed_domains,omitempty"`
	AllowedDatasets   []string `json:"allowed_datasets,omitempty"`
	AllowedActions    []string `json:"allowed_actions,omitempty"`
	AllowedViews      []string `json:"allowed_views,omitempty"`
	AllowedReports    []string `json:"allowed_reports,omitempty"`
	AllowedDashboards []string `json:"allowed_dashboards,omitempty"`
	AllowRawData      *bool    `json:"allow_raw_data,omitempty"`
	AllowSensitive    *bool    `json:"allow_sensitive,omitempty"`
	AllowAdmin        *bool    `json:"allow_admin,omitempty"`
	Note              *string  `json:"note,omitempty"`
	ExpiresAt         *string  `json:"expires_at,omitempty"`
}

type CreateAPIKeyPolicyResult struct {
	Policy APIKeyPolicyRecord `json:"policy"`
	Key    string             `json:"key,omitempty"`
}

type QueryAPIKeyPoliciesInput struct {
	Enabled  *bool  `json:"enabled,omitempty"`
	Status   string `json:"status,omitempty"`
	Q        string `json:"q,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Before   string `json:"before,omitempty"`
	BeforeID string `json:"before_id,omitempty"`
}

type AccessPolicyPreset struct {
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	Description       string   `json:"description,omitempty"`
	Role              string   `json:"role,omitempty"`
	AllowedDomains    []string `json:"allowed_domains,omitempty"`
	AllowedDatasets   []string `json:"allowed_datasets,omitempty"`
	AllowedActions    []string `json:"allowed_actions,omitempty"`
	AllowedViews      []string `json:"allowed_views,omitempty"`
	AllowedReports    []string `json:"allowed_reports,omitempty"`
	AllowedDashboards []string `json:"allowed_dashboards,omitempty"`
	AllowRawData      bool     `json:"allow_raw_data,omitempty"`
	AllowSensitive    bool     `json:"allow_sensitive,omitempty"`
	AllowAdmin        bool     `json:"allow_admin,omitempty"`
}

type AccessCheckInput struct {
	KeyID        string `json:"key_id,omitempty"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id,omitempty"`
}

type AccessCheckResult struct {
	Allowed      bool     `json:"allowed"`
	APIKeyID     string   `json:"api_key_id,omitempty"`
	TenantID     string   `json:"tenant_id,omitempty"`
	UserID       string   `json:"user_id,omitempty"`
	Role         string   `json:"role,omitempty"`
	ResourceType string   `json:"resource_type"`
	ResourceID   string   `json:"resource_id,omitempty"`
	Reasons      []string `json:"reasons,omitempty"`
}

type AccessReviewInput struct {
	MinSeverity string `json:"min_severity,omitempty"`
}

type AccessReviewResult struct {
	TenantID    string             `json:"tenant_id"`
	GeneratedAt time.Time          `json:"generated_at"`
	Total       int                `json:"total"`
	Filtered    int                `json:"filtered"`
	ByStatus    map[string]int     `json:"by_status"`
	BySeverity  map[string]int     `json:"by_severity"`
	MinSeverity string             `json:"min_severity,omitempty"`
	Findings    []AccessReviewItem `json:"findings"`
}

type AccessReviewItem struct {
	KeyID       string   `json:"key_id"`
	UserID      string   `json:"user_id,omitempty"`
	Role        string   `json:"role,omitempty"`
	Status      string   `json:"status,omitempty"`
	Severity    string   `json:"severity"`
	Codes       []string `json:"codes"`
	Summary     string   `json:"summary"`
	Recommended string   `json:"recommended,omitempty"`
}

type AccessRemediationPlanInput struct {
	MinSeverity string `json:"min_severity,omitempty"`
}

type AccessRemediationPlan struct {
	TenantID    string                  `json:"tenant_id"`
	GeneratedAt time.Time               `json:"generated_at"`
	Total       int                     `json:"total"`
	Items       []AccessRemediationItem `json:"items"`
}

type AccessRemediationItem struct {
	KeyID       string         `json:"key_id"`
	Severity    string         `json:"severity"`
	Codes       []string       `json:"codes"`
	Action      string         `json:"action"`
	Requires    []string       `json:"requires,omitempty"`
	Reason      string         `json:"reason"`
	Payload     map[string]any `json:"payload,omitempty"`
	Endpoint    string         `json:"endpoint,omitempty"`
	Method      string         `json:"method,omitempty"`
	Destructive bool           `json:"destructive,omitempty"`
}

type GovernanceEvidencePackInput struct {
	MinSeverity string `json:"min_severity,omitempty"`
	Lang        string `json:"lang,omitempty"`
}

type GovernanceEvidencePack struct {
	TenantID       string                      `json:"tenant_id"`
	ExportedAt     time.Time                   `json:"exported_at"`
	EvidenceID     string                      `json:"evidence_id,omitempty"`
	EvidenceSHA256 string                      `json:"evidence_sha256,omitempty"`
	GeneratedBy    GovernanceEvidenceActor     `json:"generated_by"`
	Summary        GovernanceEvidenceSummary   `json:"summary"`
	SummaryText    string                      `json:"summary_text,omitempty"`
	Sections       []GovernanceEvidenceSection `json:"sections"`
}

type GovernanceEvidenceActor struct {
	UserID   string `json:"user_id,omitempty"`
	Role     string `json:"role,omitempty"`
	APIKeyID string `json:"api_key_id,omitempty"`
}

type GovernanceEvidenceSection struct {
	Name  string `json:"name"`
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

type GovernanceEvidenceSummary struct {
	Status             string              `json:"status"`
	RiskLevel          string              `json:"risk_level"`
	Recommendations    []string            `json:"recommendations,omitempty"`
	Controls           []GovernanceControl `json:"controls,omitempty"`
	ControlFailures    int                 `json:"control_failures"`
	ControlWarnings    int                 `json:"control_warnings"`
	SectionCount       int                 `json:"section_count"`
	OKSections         int                 `json:"ok_sections"`
	FailedSections     int                 `json:"failed_sections"`
	BackupCount        int                 `json:"backup_count"`
	ManagedKeys        int                 `json:"managed_keys"`
	AccessFindings     int                 `json:"access_findings"`
	AccessBySeverity   map[string]int      `json:"access_by_severity,omitempty"`
	RemediationActions int                 `json:"remediation_actions"`
	OpenWorkItems      int                 `json:"open_work_items"`
	CriticalWorkItems  int                 `json:"critical_work_items"`
	HighWorkItems      int                 `json:"high_work_items"`
	OverdueWorkItems   int                 `json:"overdue_work_items"`
	Connectors         int                 `json:"connectors"`
	ConnectorIssues    int                 `json:"connector_issues"`
	AuditItems         int                 `json:"audit_items"`
}

type GovernanceControl struct {
	ID                string `json:"id"`
	Title             string `json:"title"`
	Status            string `json:"status"`
	Detail            string `json:"detail,omitempty"`
	RecommendedAction string `json:"recommended_action,omitempty"`
	ActionTarget      string `json:"action_target,omitempty"`
}

type QueryDatasetsInput struct {
	Limit    int    `json:"limit,omitempty"`
	Before   string `json:"before,omitempty"`
	BeforeID string `json:"before_id,omitempty"`
}

type Dataset struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	Domain        string    `json:"domain"`
	Name          string    `json:"name"`
	Title         string    `json:"title,omitempty"`
	Description   string    `json:"description,omitempty"`
	SchemaVersion int       `json:"schema_version"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type QueryFieldsInput struct {
	Limit    int    `json:"limit,omitempty"`
	Before   string `json:"before,omitempty"`
	BeforeID string `json:"before_id,omitempty"`
}

type FieldDefinition struct {
	ID        string         `json:"id"`
	TenantID  string         `json:"tenant_id"`
	DatasetID string         `json:"dataset_id"`
	Key       string         `json:"key"`
	Type      string         `json:"type"`
	Title     string         `json:"title,omitempty"`
	Required  bool           `json:"required,omitempty"`
	Indexed   bool           `json:"indexed,omitempty"`
	Sensitive bool           `json:"sensitive,omitempty"`
	Config    map[string]any `json:"config,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type Record struct {
	ID        string         `json:"id"`
	TenantID  string         `json:"tenant_id"`
	DatasetID string         `json:"dataset_id"`
	Title     string         `json:"title,omitempty"`
	Tags      []string       `json:"tags,omitempty"`
	Data      map[string]any `json:"data"`
	SourceID  string         `json:"source_id,omitempty"`
	CreatedBy string         `json:"created_by,omitempty"`
	UpdatedBy string         `json:"updated_by,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type DataEventLog struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	Source         string    `json:"source"`
	EventType      string    `json:"event_type"`
	Operation      string    `json:"operation"`
	BusinessAction string    `json:"business_action_id,omitempty"`
	DatasetID      string    `json:"dataset_id"`
	RecordID       string    `json:"record_id,omitempty"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
	ResultStatus   string    `json:"result_status"`
	CreatedBy      string    `json:"created_by,omitempty"`
	AppliedAt      time.Time `json:"applied_at"`
}

type DataEventDeadLetter struct {
	ID             string         `json:"id"`
	TenantID       string         `json:"tenant_id,omitempty"`
	Status         string         `json:"status"`
	Source         string         `json:"source,omitempty"`
	EventType      string         `json:"event_type,omitempty"`
	BusinessAction string         `json:"business_action_id,omitempty"`
	DatasetID      string         `json:"dataset_id,omitempty"`
	RecordID       string         `json:"record_id,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	Error          string         `json:"error,omitempty"`
	Payload        map[string]any `json:"payload,omitempty"`
	CreatedBy      string         `json:"created_by,omitempty"`
	ResolvedBy     string         `json:"resolved_by,omitempty"`
	Resolution     string         `json:"resolution,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	ResolvedAt     time.Time      `json:"resolved_at,omitempty"`
}

type RecordRevision struct {
	RowID     int64          `json:"-"`
	ID        string         `json:"id"`
	TenantID  string         `json:"tenant_id"`
	DatasetID string         `json:"dataset_id"`
	RecordID  string         `json:"record_id"`
	Action    string         `json:"action"`
	Title     string         `json:"title,omitempty"`
	Tags      []string       `json:"tags,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	SourceID  string         `json:"source_id,omitempty"`
	CreatedBy string         `json:"created_by,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

type QueryRecordRevisionsInput struct {
	Limit    int    `json:"limit,omitempty"`
	Before   string `json:"before,omitempty"`
	BeforeID string `json:"before_id,omitempty"`
}

type QueryDataEventsInput struct {
	DatasetID      string `json:"dataset_id,omitempty"`
	RecordID       string `json:"record_id,omitempty"`
	Source         string `json:"source,omitempty"`
	EventType      string `json:"event_type,omitempty"`
	BusinessAction string `json:"business_action_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	Limit          int    `json:"limit,omitempty"`
	Before         string `json:"before,omitempty"`
	BeforeID       string `json:"before_id,omitempty"`
}

type QueryDataEventDeadLettersInput struct {
	Status         string `json:"status,omitempty"`
	Source         string `json:"source,omitempty"`
	EventType      string `json:"event_type,omitempty"`
	BusinessAction string `json:"business_action_id,omitempty"`
	DatasetID      string `json:"dataset_id,omitempty"`
	RecordID       string `json:"record_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	Limit          int    `json:"limit,omitempty"`
	Before         string `json:"before,omitempty"`
	BeforeID       string `json:"before_id,omitempty"`
}

type ResolveDataEventDeadLetterInput struct {
	Resolution string `json:"resolution,omitempty"`
}

type RetryDataEventDeadLetterResult struct {
	DeadLetter DataEventDeadLetter `json:"dead_letter"`
	Result     *DataEventResult    `json:"result,omitempty"`
}

type ExternalConnector struct {
	ID                string         `json:"id"`
	TenantID          string         `json:"tenant_id,omitempty"`
	Domain            string         `json:"domain,omitempty"`
	Name              string         `json:"name"`
	Kind              string         `json:"kind,omitempty"`
	BaseURL           string         `json:"base_url,omitempty"`
	AuthType          string         `json:"auth_type,omitempty"`
	TokenRef          string         `json:"token_ref,omitempty"`
	Enabled           bool           `json:"enabled"`
	SubscribedActions []string       `json:"subscribed_actions,omitempty"`
	Config            map[string]any `json:"config,omitempty"`
	CreatedBy         string         `json:"created_by,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

type UpsertExternalConnectorInput struct {
	ID                string         `json:"id,omitempty"`
	Domain            string         `json:"domain,omitempty"`
	Name              string         `json:"name"`
	Kind              string         `json:"kind,omitempty"`
	BaseURL           string         `json:"base_url,omitempty"`
	AuthType          string         `json:"auth_type,omitempty"`
	TokenRef          string         `json:"token_ref,omitempty"`
	Enabled           *bool          `json:"enabled,omitempty"`
	SubscribedActions []string       `json:"subscribed_actions,omitempty"`
	Config            map[string]any `json:"config,omitempty"`
}

type QueryExternalConnectorsInput struct {
	Domain   string `json:"domain,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Enabled  *bool  `json:"enabled,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Before   string `json:"before,omitempty"`
	BeforeID string `json:"before_id,omitempty"`
}

type ConnectorContractBinding struct {
	ActionID string         `json:"action_id"`
	Valid    bool           `json:"valid"`
	Error    string         `json:"error,omitempty"`
	Contract *EventContract `json:"contract,omitempty"`
}

type ConnectorTestResult struct {
	Connector ExternalConnector          `json:"connector"`
	Valid     bool                       `json:"valid"`
	Bindings  []ConnectorContractBinding `json:"bindings"`
	EventAPI  string                     `json:"event_api"`
	Flow      []string                   `json:"recommended_flow"`
	Metadata  map[string]any             `json:"metadata,omitempty"`
}

type ConnectorEventPreview struct {
	Connector        ExternalConnector `json:"connector"`
	BusinessAction   string            `json:"business_action_id"`
	Source           string            `json:"source"`
	EventType        string            `json:"event_type"`
	Operation        string            `json:"operation"`
	DatasetID        string            `json:"dataset_id"`
	RecordID         string            `json:"record_id,omitempty"`
	IdempotencyKey   string            `json:"idempotency_key,omitempty"`
	OriginalData     map[string]any    `json:"original_data,omitempty"`
	MappedData       map[string]any    `json:"mapped_data,omitempty"`
	MappingApplied   bool              `json:"mapping_applied"`
	MissingMappings  []string          `json:"missing_mappings,omitempty"`
	NormalizedEvent  DataEventInput    `json:"normalized_event"`
	DryRunResult     *DataEventResult  `json:"dry_run_result,omitempty"`
	RecommendedWrite string            `json:"recommended_write"`
}

type SuggestConnectorMappingInput struct {
	BusinessAction string         `json:"business_action_id"`
	SampleData     map[string]any `json:"sample_data"`
}

type PatchConnectorConfigInput struct {
	Patch  map[string]any `json:"patch"`
	DryRun bool           `json:"dry_run,omitempty"`
}

type ConnectorConfigPatchResult struct {
	Connector      ExternalConnector `json:"connector"`
	DryRun         bool              `json:"dry_run"`
	PreviousConfig map[string]any    `json:"previous_config,omitempty"`
	PatchedConfig  map[string]any    `json:"patched_config,omitempty"`
}

type ConnectorConfigValidationIssue struct {
	Severity       string `json:"severity"`
	Code           string `json:"code"`
	BusinessAction string `json:"business_action_id,omitempty"`
	Field          string `json:"field,omitempty"`
	Path           string `json:"path,omitempty"`
	Message        string `json:"message"`
}

type ConnectorConfigValidationAction struct {
	ActionID       string            `json:"action_id"`
	DatasetID      string            `json:"dataset_id"`
	RequiredFields []string          `json:"required_fields,omitempty"`
	MappedFields   []string          `json:"mapped_fields,omitempty"`
	MissingFields  []string          `json:"missing_fields,omitempty"`
	ExtraFields    []string          `json:"extra_fields,omitempty"`
	Mapping        map[string]string `json:"mapping,omitempty"`
}

type ConnectorConfigValidationResult struct {
	Connector       ExternalConnector                 `json:"connector"`
	Valid           bool                              `json:"valid"`
	Issues          []ConnectorConfigValidationIssue  `json:"issues,omitempty"`
	Warnings        []ConnectorConfigValidationIssue  `json:"warnings,omitempty"`
	Actions         []ConnectorConfigValidationAction `json:"actions,omitempty"`
	RecommendedNext []string                          `json:"recommended_next,omitempty"`
}

type ConnectorReadinessInput struct {
	SampleEvent *DataEventInput `json:"sample_event,omitempty"`
}

type ConnectorReadinessCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type ConnectorReadinessResult struct {
	Connector       ExternalConnector                `json:"connector"`
	Ready           bool                             `json:"ready"`
	Checks          []ConnectorReadinessCheck        `json:"checks"`
	Test            *ConnectorTestResult             `json:"test,omitempty"`
	Config          *ConnectorConfigValidationResult `json:"config,omitempty"`
	Health          *ConnectorHealth                 `json:"health,omitempty"`
	Preview         *ConnectorEventPreview           `json:"preview,omitempty"`
	RecommendedNext []string                         `json:"recommended_next,omitempty"`
}

type ConnectorSyncPlanInput struct {
	SampleEvent     *DataEventInput  `json:"sample_event,omitempty"`
	FirstPageEvents []DataEventInput `json:"first_page_events,omitempty"`
	PageSize        int              `json:"page_size,omitempty"`
	Cursor          string           `json:"cursor,omitempty"`
}

type ConnectorSyncPlanStep struct {
	Order            int            `json:"order"`
	Name             string         `json:"name"`
	Action           string         `json:"action"`
	Endpoint         string         `json:"endpoint,omitempty"`
	Method           string         `json:"method,omitempty"`
	Required         bool           `json:"required"`
	Status           string         `json:"status,omitempty"`
	Description      string         `json:"description,omitempty"`
	Body             map[string]any `json:"body,omitempty"`
	ToolCallTemplate map[string]any `json:"tool_call_template,omitempty"`
}

type ConnectorSyncPlanResult struct {
	Connector       ExternalConnector         `json:"connector"`
	Ready           bool                      `json:"ready"`
	Readiness       *ConnectorReadinessResult `json:"readiness,omitempty"`
	DryRunBatch     *ConnectorSyncBatchResult `json:"dry_run_batch,omitempty"`
	PageSize        int                       `json:"page_size"`
	Cursor          string                    `json:"cursor,omitempty"`
	Steps           []ConnectorSyncPlanStep   `json:"steps"`
	Rollback        []ConnectorSyncPlanStep   `json:"rollback,omitempty"`
	RecommendedNext []string                  `json:"recommended_next,omitempty"`
}

type ConnectorMappingSuggestion struct {
	Connector        ExternalConnector  `json:"connector"`
	BusinessAction   string             `json:"business_action_id"`
	DatasetID        string             `json:"dataset_id"`
	RequiredFields   []string           `json:"required_fields,omitempty"`
	SuggestedMapping map[string]string  `json:"suggested_mapping"`
	Confidence       map[string]float64 `json:"confidence,omitempty"`
	UnmatchedFields  []string           `json:"unmatched_fields,omitempty"`
	SamplePaths      []string           `json:"sample_paths,omitempty"`
	ConfigPatch      map[string]any     `json:"config_patch,omitempty"`
	RecommendedNext  []string           `json:"recommended_next,omitempty"`
}

type ConnectorHealthAction struct {
	ActionID        string         `json:"action_id"`
	RecentEvents    int            `json:"recent_events"`
	RecentFailures  int            `json:"recent_failures"`
	OpenDeadLetters int            `json:"open_dead_letters"`
	LastEvent       *DataEventLog  `json:"last_event,omitempty"`
	LastFailure     map[string]any `json:"last_failure,omitempty"`
	Status          string         `json:"status"`
}

type ConnectorHealth struct {
	Connector       ExternalConnector       `json:"connector"`
	Status          string                  `json:"status"`
	Source          string                  `json:"source"`
	Enabled         bool                    `json:"enabled"`
	SubscribedCount int                     `json:"subscribed_count"`
	RecentEvents    int                     `json:"recent_events"`
	RecentFailures  int                     `json:"recent_failures"`
	OpenDeadLetters int                     `json:"open_dead_letters"`
	LastEvent       *DataEventLog           `json:"last_event,omitempty"`
	SyncState       *ConnectorSyncState     `json:"sync_state,omitempty"`
	Actions         []ConnectorHealthAction `json:"actions,omitempty"`
	Recommendations []string                `json:"recommendations,omitempty"`
	CheckedAt       time.Time               `json:"checked_at"`
}

type QueryConnectorSyncRunsInput struct {
	Limit    int    `json:"limit,omitempty"`
	Before   string `json:"before,omitempty"`
	BeforeID string `json:"before_id,omitempty"`
}

type UpdateConnectorSyncStateInput struct {
	Status        string         `json:"status,omitempty"`
	Cursor        string         `json:"cursor,omitempty"`
	Checkpoint    map[string]any `json:"checkpoint,omitempty"`
	LastError     string         `json:"last_error,omitempty"`
	Message       string         `json:"message,omitempty"`
	SyncedRecords *int           `json:"synced_records,omitempty"`
	StartedAt     string         `json:"started_at,omitempty"`
	FinishedAt    string         `json:"finished_at,omitempty"`
}

type ConnectorSyncState struct {
	Status        string         `json:"status"`
	Cursor        string         `json:"cursor,omitempty"`
	Checkpoint    map[string]any `json:"checkpoint,omitempty"`
	LastError     string         `json:"last_error,omitempty"`
	Message       string         `json:"message,omitempty"`
	SyncedRecords int            `json:"synced_records,omitempty"`
	StartedAt     time.Time      `json:"started_at,omitempty"`
	FinishedAt    time.Time      `json:"finished_at,omitempty"`
	UpdatedBy     string         `json:"updated_by,omitempty"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type ConnectorSyncBatchInput struct {
	Events      []DataEventInput               `json:"events"`
	DryRun      bool                           `json:"dry_run,omitempty"`
	StopOnError bool                           `json:"stop_on_error,omitempty"`
	SyncState   *UpdateConnectorSyncStateInput `json:"sync_state,omitempty"`
}

type ConnectorSyncBatchItem struct {
	Index      int                  `json:"index"`
	Status     string               `json:"status"`
	Result     *DataEventResult     `json:"result,omitempty"`
	Error      string               `json:"error,omitempty"`
	DeadLetter *DataEventDeadLetter `json:"dead_letter,omitempty"`
}

type ConnectorSyncBatchResult struct {
	Connector       ExternalConnector        `json:"connector"`
	Run             *ConnectorSyncRun        `json:"run,omitempty"`
	Total           int                      `json:"total"`
	Succeeded       int                      `json:"succeeded"`
	Failed          int                      `json:"failed"`
	DryRun          bool                     `json:"dry_run,omitempty"`
	StoppedOnError  bool                     `json:"stopped_on_error,omitempty"`
	Items           []ConnectorSyncBatchItem `json:"items"`
	SyncState       *ConnectorSyncState      `json:"sync_state,omitempty"`
	RecommendedNext []string                 `json:"recommended_next,omitempty"`
}

type ConnectorSyncRun struct {
	ID             string    `json:"id"`
	Status         string    `json:"status"`
	Total          int       `json:"total"`
	Succeeded      int       `json:"succeeded"`
	Failed         int       `json:"failed"`
	DryRun         bool      `json:"dry_run,omitempty"`
	StoppedOnError bool      `json:"stopped_on_error,omitempty"`
	Cursor         string    `json:"cursor,omitempty"`
	ErrorSummary   string    `json:"error_summary,omitempty"`
	StartedAt      time.Time `json:"started_at"`
	FinishedAt     time.Time `json:"finished_at"`
	CreatedBy      string    `json:"created_by,omitempty"`
}

type CreateDatasetInput struct {
	ID          string `json:"id,omitempty"`
	Domain      string `json:"domain"`
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

type UpdateDatasetInput struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
}

type UpsertFieldsInput struct {
	Fields []FieldDefinition `json:"fields"`
}

type CreateRecordInput struct {
	ID       string         `json:"id,omitempty"`
	Title    string         `json:"title,omitempty"`
	Tags     []string       `json:"tags,omitempty"`
	Data     map[string]any `json:"data"`
	SourceID string         `json:"source_id,omitempty"`
}

type UpdateRecordInput struct {
	Title *string        `json:"title,omitempty"`
	Tags  []string       `json:"tags,omitempty"`
	Data  map[string]any `json:"data,omitempty"`
}

type BulkUpdateRecordsInput struct {
	Query       QueryRecordsInput `json:"query,omitempty"`
	SetData     map[string]any    `json:"set,omitempty"`
	UnsetFields []string          `json:"unset,omitempty"`
	Title       *string           `json:"title,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Limit       int               `json:"limit,omitempty"`
	DryRun      bool              `json:"dry_run,omitempty"`
	Confirm     bool              `json:"confirm,omitempty"`
	Reason      string            `json:"reason,omitempty"`
}

type BulkUpdateRecordsResult struct {
	DatasetID   string                       `json:"dataset_id"`
	DryRun      bool                         `json:"dry_run"`
	Valid       bool                         `json:"valid"`
	Total       int                          `json:"total"`
	Updated     int                          `json:"updated"`
	Records     []Record                     `json:"records,omitempty"`
	Validations []BulkUpdateRecordValidation `json:"validations"`
}

type BulkUpdateRecordValidation struct {
	Index         int      `json:"index"`
	ID            string   `json:"id"`
	Valid         bool     `json:"valid"`
	Errors        []string `json:"errors,omitempty"`
	UnknownFields []string `json:"unknown_fields,omitempty"`
}

type BulkDeleteRecordsInput struct {
	Query   QueryRecordsInput `json:"query,omitempty"`
	Limit   int               `json:"limit,omitempty"`
	DryRun  bool              `json:"dry_run,omitempty"`
	Confirm bool              `json:"confirm,omitempty"`
	Reason  string            `json:"reason,omitempty"`
}

type BulkDeleteRecordsResult struct {
	DatasetID string   `json:"dataset_id"`
	DryRun    bool     `json:"dry_run"`
	Total     int      `json:"total"`
	Deleted   int      `json:"deleted"`
	RecordIDs []string `json:"record_ids"`
	Records   []Record `json:"records,omitempty"`
}

type RestoreRecordInput struct {
	Confirm    bool   `json:"confirm"`
	RevisionID string `json:"revision_id,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type QueryRecordTimelineInput struct {
	Limit    int    `json:"limit,omitempty"`
	Before   string `json:"before,omitempty"`
	BeforeID string `json:"before_id,omitempty"`
}

type RecordTimelineItem struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Action    string         `json:"action"`
	UserID    string         `json:"user_id,omitempty"`
	Source    string         `json:"source,omitempty"`
	Summary   string         `json:"summary,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

type RecordTimelineResult struct {
	DatasetID    string               `json:"dataset_id"`
	RecordID     string               `json:"record_id"`
	Items        []RecordTimelineItem `json:"items"`
	Limit        int                  `json:"limit"`
	HasMore      bool                 `json:"has_more"`
	NextBefore   string               `json:"next_before,omitempty"`
	NextBeforeID string               `json:"next_before_id,omitempty"`
}

type MISInboxItem struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	Severity    string         `json:"severity,omitempty"`
	Status      string         `json:"status,omitempty"`
	DatasetID   string         `json:"dataset_id,omitempty"`
	RecordID    string         `json:"record_id,omitempty"`
	Title       string         `json:"title"`
	Summary     string         `json:"summary,omitempty"`
	Action      string         `json:"action,omitempty"`
	TargetURL   string         `json:"target_url,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedBy   string         `json:"created_by,omitempty"`
	CreatedAt   time.Time      `json:"created_at,omitempty"`
	UpdatedAt   time.Time      `json:"updated_at,omitempty"`
	Recommended string         `json:"recommended_action,omitempty"`
}

type QueryMISInboxInput struct {
	Limit              int    `json:"limit,omitempty"`
	DatasetID          string `json:"dataset_id,omitempty"`
	AppID              string `json:"app_id,omitempty"`
	BlueprintID        string `json:"blueprint_id,omitempty"`
	ObjectRole         string `json:"object_role,omitempty"`
	WorkflowSkillID    string `json:"workflow_skill_id,omitempty"`
	WorkflowVersion    string `json:"workflow_version,omitempty"`
	WorkflowInstanceID string `json:"workflow_instance_id,omitempty"`
	WorkflowNodeID     string `json:"workflow_node_id,omitempty"`
	BusinessStatus     string `json:"business_status,omitempty"`
	ResultStatus       string `json:"result_status,omitempty"`
	Lane               string `json:"lane,omitempty"`
	UserID             string `json:"user_id,omitempty"`
	Type               string `json:"type,omitempty"`
	Status             string `json:"status,omitempty"`
	IncludeOK          bool   `json:"include_ok,omitempty"`
	Before             string `json:"before,omitempty"`
	BeforeID           string `json:"before_id,omitempty"`
}

type MISInboxResult struct {
	Items        []MISInboxItem `json:"items"`
	Limit        int            `json:"limit"`
	HasMore      bool           `json:"has_more"`
	NextBefore   string         `json:"next_before,omitempty"`
	NextBeforeID string         `json:"next_before_id,omitempty"`
	GeneratedAt  time.Time      `json:"generated_at"`
}

type MISInboxSummary struct {
	Total       int            `json:"total"`
	ByType      map[string]int `json:"by_type"`
	BySeverity  map[string]int `json:"by_severity"`
	ByStatus    map[string]int `json:"by_status"`
	Overdue     int            `json:"overdue"`
	Critical    int            `json:"critical"`
	High        int            `json:"high"`
	GeneratedAt time.Time      `json:"generated_at"`
}

type DashboardDefinition struct {
	ID          string   `json:"id"`
	Domain      string   `json:"domain"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	ReportIDs   []string `json:"report_ids"`
}

type DashboardResult struct {
	Dashboard      DashboardDefinition `json:"dashboard"`
	Stats          *SystemStats        `json:"stats,omitempty"`
	InboxSummary   *MISInboxSummary    `json:"inbox_summary,omitempty"`
	Reports        []DashboardReport   `json:"reports"`
	GeneratedAt    time.Time           `json:"generated_at"`
	PrimaryResult  string              `json:"primary_result,omitempty"`
	BusinessStatus string              `json:"business_status,omitempty"`
	ResultStatus   string              `json:"result_status,omitempty"`
	ResultPayload  map[string]any      `json:"result_payload,omitempty"`
	Outputs        []map[string]any    `json:"outputs,omitempty"`
	Artifacts      []map[string]any    `json:"artifacts,omitempty"`
}

type DashboardReport struct {
	ReportID string        `json:"report_id"`
	Title    string        `json:"title,omitempty"`
	Error    string        `json:"error,omitempty"`
	Result   *ReportResult `json:"result,omitempty"`
}

type RecordApproval struct {
	ID                  string                   `json:"id"`
	TenantID            string                   `json:"tenant_id,omitempty"`
	DatasetID           string                   `json:"dataset_id"`
	RecordID            string                   `json:"record_id"`
	AppID               string                   `json:"app_id,omitempty"`
	BlueprintID         string                   `json:"blueprint_id,omitempty"`
	ObjectRole          string                   `json:"object_role,omitempty"`
	ApprovalWorkflowID  string                   `json:"approval_workflow_id,omitempty"`
	TriggerEvent        string                   `json:"trigger_event,omitempty"`
	SubmittedBy         string                   `json:"submitted_by,omitempty"`
	CurrentAssignee     string                   `json:"current_assignee,omitempty"`
	CurrentAssigneeType string                   `json:"current_assignee_type,omitempty"`
	FromStatus          string                   `json:"from_status,omitempty"`
	ToStatus            string                   `json:"to_status,omitempty"`
	Status              string                   `json:"status"`
	Kind                string                   `json:"kind,omitempty"`
	Priority            string                   `json:"priority,omitempty"`
	Summary             string                   `json:"summary,omitempty"`
	Request             map[string]any           `json:"request,omitempty"`
	WorkflowSkillID     string                   `json:"workflow_skill_id,omitempty"`
	WorkflowVersion     string                   `json:"workflow_version,omitempty"`
	WorkflowInstanceID  string                   `json:"workflow_instance_id,omitempty"`
	WorkflowNodeID      string                   `json:"workflow_node_id,omitempty"`
	WorkflowNodeIDs     []string                 `json:"workflow_node_ids,omitempty"`
	WorkflowDecisionID  string                   `json:"workflow_decision_id,omitempty"`
	DetailURL           string                   `json:"detail_url,omitempty"`
	BusinessStatus      string                   `json:"business_status,omitempty"`
	ResultStatus        string                   `json:"result_status,omitempty"`
	ResultPayload       map[string]any           `json:"result_payload,omitempty"`
	Outputs             []RecordApprovalOutput   `json:"outputs,omitempty"`
	Artifacts           []RecordApprovalArtifact `json:"artifacts,omitempty"`
	Decision            string                   `json:"decision,omitempty"`
	Reason              string                   `json:"reason,omitempty"`
	AssignedTo          string                   `json:"assigned_to,omitempty"`
	Reused              bool                     `json:"reused,omitempty"`
	CreatedBy           string                   `json:"created_by,omitempty"`
	ReviewedBy          string                   `json:"reviewed_by,omitempty"`
	CreatedAt           time.Time                `json:"created_at"`
	DueAt               time.Time                `json:"due_at,omitempty"`
	ReviewedAt          time.Time                `json:"reviewed_at,omitempty"`
	UpdatedAt           time.Time                `json:"updated_at"`
}

type RecordApprovalArtifact struct {
	ID            string `json:"id,omitempty"`
	URI           string `json:"uri,omitempty"`
	Name          string `json:"name,omitempty"`
	Path          string `json:"path,omitempty"`
	MimeType      string `json:"mime_type,omitempty"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
	RemoteURL     string `json:"remote_url,omitempty"`
	Checksum      string `json:"checksum,omitempty"`
	DownloadState string `json:"download_state,omitempty"`
	Status        string `json:"status,omitempty"`
	Presentation  string `json:"presentation,omitempty"`
}

type RecordApprovalOutput struct {
	Type       string                  `json:"type,omitempty"`
	Kind       string                  `json:"kind,omitempty"`
	Title      string                  `json:"title,omitempty"`
	Text       string                  `json:"text,omitempty"`
	Status     string                  `json:"status,omitempty"`
	ArtifactID string                  `json:"artifact_id,omitempty"`
	Artifact   *RecordApprovalArtifact `json:"artifact,omitempty"`
	Data       map[string]any          `json:"data,omitempty"`
}

type CreateRecordApprovalInput struct {
	AppID               string                   `json:"app_id,omitempty"`
	BlueprintID         string                   `json:"blueprint_id,omitempty"`
	ObjectRole          string                   `json:"object_role,omitempty"`
	ApprovalWorkflowID  string                   `json:"approval_workflow_id,omitempty"`
	TriggerEvent        string                   `json:"trigger_event,omitempty"`
	SubmittedBy         string                   `json:"submitted_by,omitempty"`
	CurrentAssignee     string                   `json:"current_assignee,omitempty"`
	CurrentAssigneeType string                   `json:"current_assignee_type,omitempty"`
	FromStatus          string                   `json:"from_status,omitempty"`
	ToStatus            string                   `json:"to_status,omitempty"`
	Kind                string                   `json:"kind,omitempty"`
	Priority            string                   `json:"priority,omitempty"`
	Summary             string                   `json:"summary,omitempty"`
	Request             map[string]any           `json:"request,omitempty"`
	WorkflowSkillID     string                   `json:"workflow_skill_id,omitempty"`
	WorkflowVersion     string                   `json:"workflow_version,omitempty"`
	WorkflowInstanceID  string                   `json:"workflow_instance_id,omitempty"`
	WorkflowNodeID      string                   `json:"workflow_node_id,omitempty"`
	WorkflowNodeIDs     []string                 `json:"workflow_node_ids,omitempty"`
	WorkflowDecisionID  string                   `json:"workflow_decision_id,omitempty"`
	DetailURL           string                   `json:"detail_url,omitempty"`
	BusinessStatus      string                   `json:"business_status,omitempty"`
	ResultStatus        string                   `json:"result_status,omitempty"`
	ResultPayload       map[string]any           `json:"result_payload,omitempty"`
	Outputs             []RecordApprovalOutput   `json:"outputs,omitempty"`
	Artifacts           []RecordApprovalArtifact `json:"artifacts,omitempty"`
	AssignedTo          string                   `json:"assigned_to,omitempty"`
	DueAt               string                   `json:"due_at,omitempty"`
}

type QueryRecordApprovalsInput struct {
	DatasetID           string `json:"dataset_id,omitempty"`
	RecordID            string `json:"record_id,omitempty"`
	AppID               string `json:"app_id,omitempty"`
	BlueprintID         string `json:"blueprint_id,omitempty"`
	ObjectRole          string `json:"object_role,omitempty"`
	ApprovalWorkflowID  string `json:"approval_workflow_id,omitempty"`
	TriggerEvent        string `json:"trigger_event,omitempty"`
	SubmittedBy         string `json:"submitted_by,omitempty"`
	CurrentAssignee     string `json:"current_assignee,omitempty"`
	CurrentAssigneeType string `json:"current_assignee_type,omitempty"`
	FromStatus          string `json:"from_status,omitempty"`
	ToStatus            string `json:"to_status,omitempty"`
	Status              string `json:"status,omitempty"`
	Kind                string `json:"kind,omitempty"`
	WorkflowSkillID     string `json:"workflow_skill_id,omitempty"`
	WorkflowVersion     string `json:"workflow_version,omitempty"`
	WorkflowInstanceID  string `json:"workflow_instance_id,omitempty"`
	WorkflowNodeID      string `json:"workflow_node_id,omitempty"`
	BusinessStatus      string `json:"business_status,omitempty"`
	ResultStatus        string `json:"result_status,omitempty"`
	AssignedTo          string `json:"assigned_to,omitempty"`
	CreatedBy           string `json:"created_by,omitempty"`
	ReviewedBy          string `json:"reviewed_by,omitempty"`
	Lane                string `json:"lane,omitempty"`
	UserID              string `json:"user_id,omitempty"`
	Overdue             bool   `json:"overdue,omitempty"`
	Limit               int    `json:"limit,omitempty"`
	Before              string `json:"before,omitempty"`
	BeforeID            string `json:"before_id,omitempty"`
}

type ReviewRecordApprovalInput struct {
	Decision            string                   `json:"decision"`
	Reason              string                   `json:"reason,omitempty"`
	WorkflowInstanceID  string                   `json:"workflow_instance_id,omitempty"`
	CurrentAssignee     string                   `json:"current_assignee,omitempty"`
	CurrentAssigneeType string                   `json:"current_assignee_type,omitempty"`
	FromStatus          string                   `json:"from_status,omitempty"`
	ToStatus            string                   `json:"to_status,omitempty"`
	WorkflowNodeID      string                   `json:"workflow_node_id,omitempty"`
	WorkflowNodeIDs     []string                 `json:"workflow_node_ids,omitempty"`
	WorkflowVersion     string                   `json:"workflow_version,omitempty"`
	WorkflowDecisionID  string                   `json:"workflow_decision_id,omitempty"`
	DetailURL           string                   `json:"detail_url,omitempty"`
	BusinessStatus      string                   `json:"business_status,omitempty"`
	ResultStatus        string                   `json:"result_status,omitempty"`
	ResultPayload       map[string]any           `json:"result_payload,omitempty"`
	Outputs             []RecordApprovalOutput   `json:"outputs,omitempty"`
	Artifacts           []RecordApprovalArtifact `json:"artifacts,omitempty"`
}
type UpdateRecordApprovalProgressInput struct {
	WorkflowInstanceID  string                   `json:"workflow_instance_id,omitempty"`
	CurrentAssignee     string                   `json:"current_assignee,omitempty"`
	CurrentAssigneeType string                   `json:"current_assignee_type,omitempty"`
	FromStatus          string                   `json:"from_status,omitempty"`
	ToStatus            string                   `json:"to_status,omitempty"`
	WorkflowNodeID      string                   `json:"workflow_node_id,omitempty"`
	WorkflowNodeIDs     []string                 `json:"workflow_node_ids,omitempty"`
	WorkflowVersion     string                   `json:"workflow_version,omitempty"`
	WorkflowDecisionID  string                   `json:"workflow_decision_id,omitempty"`
	DetailURL           string                   `json:"detail_url,omitempty"`
	BusinessStatus      string                   `json:"business_status,omitempty"`
	ResultStatus        string                   `json:"result_status,omitempty"`
	ResultPayload       map[string]any           `json:"result_payload,omitempty"`
	Outputs             []RecordApprovalOutput   `json:"outputs,omitempty"`
	Artifacts           []RecordApprovalArtifact `json:"artifacts,omitempty"`
	Progress            string                   `json:"progress,omitempty"`
}

type OperationPlan struct {
	ID         string         `json:"id"`
	TenantID   string         `json:"tenant_id,omitempty"`
	DatasetID  string         `json:"dataset_id,omitempty"`
	Operation  string         `json:"operation"`
	Status     string         `json:"status"`
	Summary    string         `json:"summary,omitempty"`
	RiskLevel  string         `json:"risk_level,omitempty"`
	Request    map[string]any `json:"request,omitempty"`
	Preview    map[string]any `json:"preview,omitempty"`
	CreatedBy  string         `json:"created_by,omitempty"`
	ReviewedBy string         `json:"reviewed_by,omitempty"`
	AppliedBy  string         `json:"applied_by,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	ReviewedAt time.Time      `json:"reviewed_at,omitempty"`
	AppliedAt  time.Time      `json:"applied_at,omitempty"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

type CreateOperationPlanInput struct {
	DatasetID string         `json:"dataset_id,omitempty"`
	Operation string         `json:"operation"`
	Summary   string         `json:"summary,omitempty"`
	RiskLevel string         `json:"risk_level,omitempty"`
	Request   map[string]any `json:"request"`
}

type QueryOperationPlansInput struct {
	DatasetID string `json:"dataset_id,omitempty"`
	Operation string `json:"operation,omitempty"`
	Status    string `json:"status,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Before    string `json:"before,omitempty"`
	BeforeID  string `json:"before_id,omitempty"`
}

type ApplyOperationPlanInput struct {
	Confirm bool   `json:"confirm"`
	Reason  string `json:"reason,omitempty"`
}

type ReviewOperationPlanInput struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}

type OperationPlanApplyResult struct {
	Plan   OperationPlan `json:"plan"`
	Result any           `json:"result"`
}

type ValidateRecordInput struct {
	Data map[string]any `json:"data"`
}

type ValidateRecordResult struct {
	Valid         bool     `json:"valid"`
	DatasetID     string   `json:"dataset_id"`
	Errors        []string `json:"errors,omitempty"`
	UnknownFields []string `json:"unknown_fields,omitempty"`
	FieldCount    int      `json:"field_count"`
}

type QualityCheckDefinition struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Severity    string `json:"severity,omitempty"`
}

type QueryQualityRunsInput struct {
	Limit    int    `json:"limit,omitempty"`
	Before   string `json:"before,omitempty"`
	BeforeID string `json:"before_id,omitempty"`
}
type RunQualityCheckInput struct {
	Checks          []string `json:"checks,omitempty"`
	Limit           int      `json:"limit,omitempty"`
	IncludeWarnings bool     `json:"include_warnings,omitempty"`
}

type QualityIssue struct {
	Severity  string `json:"severity"`
	Check     string `json:"check"`
	DatasetID string `json:"dataset_id"`
	RecordID  string `json:"record_id,omitempty"`
	Field     string `json:"field,omitempty"`
	Message   string `json:"message"`
	Value     any    `json:"value,omitempty"`
}

type QualityCheckResult struct {
	ID              string         `json:"id,omitempty"`
	TenantID        string         `json:"tenant_id,omitempty"`
	DatasetID       string         `json:"dataset_id"`
	Checks          []string       `json:"checks,omitempty"`
	Scanned         int            `json:"scanned"`
	Valid           bool           `json:"valid"`
	IssueCount      int            `json:"issue_count"`
	Issues          []QualityIssue `json:"issues,omitempty"`
	Limit           int            `json:"limit"`
	IncludeWarnings bool           `json:"include_warnings,omitempty"`
	CreatedBy       string         `json:"created_by,omitempty"`
	CreatedAt       time.Time      `json:"created_at,omitempty"`
}

type BatchRecordInput struct {
	ID       string         `json:"id,omitempty"`
	Title    string         `json:"title,omitempty"`
	Tags     []string       `json:"tags,omitempty"`
	Data     map[string]any `json:"data"`
	SourceID string         `json:"source_id,omitempty"`
}

type BatchImportRecordsInput struct {
	Records []BatchRecordInput `json:"records"`
	DryRun  bool               `json:"dry_run,omitempty"`
}

type ImportCSVInput struct {
	CSVText string `json:"csv,omitempty"`
	DryRun  bool   `json:"dry_run,omitempty"`
}

type ImportJSONLInput struct {
	JSONLText string `json:"jsonl,omitempty"`
	DryRun    bool   `json:"dry_run,omitempty"`
}

type ImportJob struct {
	ID         string                    `json:"id"`
	TenantID   string                    `json:"tenant_id,omitempty"`
	DatasetID  string                    `json:"dataset_id"`
	Kind       string                    `json:"kind"`
	Status     string                    `json:"status"`
	DryRun     bool                      `json:"dry_run,omitempty"`
	Total      int                       `json:"total"`
	Imported   int                       `json:"imported"`
	Valid      bool                      `json:"valid"`
	Error      string                    `json:"error,omitempty"`
	Result     *BatchImportRecordsResult `json:"result,omitempty"`
	CreatedBy  string                    `json:"created_by,omitempty"`
	CreatedAt  time.Time                 `json:"created_at"`
	StartedAt  time.Time                 `json:"started_at,omitempty"`
	FinishedAt time.Time                 `json:"finished_at,omitempty"`
}

type QueryImportJobsInput struct {
	DatasetID string `json:"dataset_id,omitempty"`
	Status    string `json:"status,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Before    string `json:"before,omitempty"`
	BeforeID  string `json:"before_id,omitempty"`
}

type StartExportJobInput struct {
	QueryRecordsInput
	Format string `json:"format,omitempty"`
}

type ExportJob struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id,omitempty"`
	DatasetID    string    `json:"dataset_id"`
	Format       string    `json:"format"`
	Status       string    `json:"status"`
	Total        int       `json:"total"`
	Bytes        int       `json:"bytes"`
	Error        string    `json:"error,omitempty"`
	ResultText   string    `json:"result,omitempty"`
	DownloadPath string    `json:"download_path,omitempty"`
	CreatedBy    string    `json:"created_by,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	FinishedAt   time.Time `json:"finished_at,omitempty"`
}

type QueryExportJobsInput struct {
	DatasetID string `json:"dataset_id,omitempty"`
	Status    string `json:"status,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Before    string `json:"before,omitempty"`
	BeforeID  string `json:"before_id,omitempty"`
}

type RunMaintenanceInput struct {
	Tasks []string `json:"tasks,omitempty"`
}

type MaintenanceTaskResult struct {
	Task       string    `json:"task"`
	Status     string    `json:"status"`
	Message    string    `json:"message,omitempty"`
	DurationMS int64     `json:"duration_ms"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

type MaintenanceResult struct {
	Engine     string                  `json:"engine"`
	TenantID   string                  `json:"tenant_id,omitempty"`
	Tasks      []MaintenanceTaskResult `json:"tasks"`
	Valid      bool                    `json:"valid"`
	StartedAt  time.Time               `json:"started_at"`
	FinishedAt time.Time               `json:"finished_at"`
}

type SystemStats struct {
	Engine          string                 `json:"engine"`
	TenantID        string                 `json:"tenant_id"`
	SchemaVersion   int                    `json:"schema_version"`
	DatasetCount    int                    `json:"dataset_count"`
	RecordCount     int                    `json:"record_count"`
	FieldCount      int                    `json:"field_count"`
	ImportJobs      map[string]int         `json:"import_jobs"`
	ExportJobs      map[string]int         `json:"export_jobs"`
	QualityRunCount int                    `json:"quality_run_count"`
	AuditLogCount   int                    `json:"audit_log_count"`
	BackupCount     int                    `json:"backup_count"`
	DatabaseBytes   int64                  `json:"database_bytes,omitempty"`
	Datasets        []DatasetStats         `json:"datasets"`
	GeneratedAt     time.Time              `json:"generated_at"`
	Extra           map[string]interface{} `json:"extra,omitempty"`
}

type DatasetStats struct {
	DatasetID     string    `json:"dataset_id"`
	Domain        string    `json:"domain,omitempty"`
	Name          string    `json:"name,omitempty"`
	Title         string    `json:"title,omitempty"`
	SchemaVersion int       `json:"schema_version"`
	FieldCount    int       `json:"field_count"`
	RecordCount   int       `json:"record_count"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
}

type BatchRecordValidation struct {
	Index         int      `json:"index"`
	ID            string   `json:"id,omitempty"`
	Valid         bool     `json:"valid"`
	Errors        []string `json:"errors,omitempty"`
	UnknownFields []string `json:"unknown_fields,omitempty"`
}

type BatchImportRecordsResult struct {
	DatasetID   string                  `json:"dataset_id"`
	DryRun      bool                    `json:"dry_run"`
	Valid       bool                    `json:"valid"`
	Total       int                     `json:"total"`
	Imported    int                     `json:"imported"`
	Records     []Record                `json:"records,omitempty"`
	Validations []BatchRecordValidation `json:"validations"`
}

type QueryRecordsInput struct {
	Q        string         `json:"q,omitempty"`
	Tag      string         `json:"tag,omitempty"`
	Filter   map[string]any `json:"filter,omitempty"`
	Sort     []SortSpec     `json:"sort,omitempty"`
	Limit    int            `json:"limit,omitempty"`
	Before   string         `json:"before,omitempty"`
	BeforeID string         `json:"before_id,omitempty"`
}

type QueryBusinessViewInput struct {
	Q        string         `json:"q,omitempty"`
	Tag      string         `json:"tag,omitempty"`
	Filter   map[string]any `json:"filter,omitempty"`
	Sort     []SortSpec     `json:"sort,omitempty"`
	Limit    int            `json:"limit,omitempty"`
	Before   string         `json:"before,omitempty"`
	BeforeID string         `json:"before_id,omitempty"`
}

type SortSpec struct {
	Field     string `json:"field"`
	Direction string `json:"direction,omitempty"`
}

type ListResponse[T any] struct {
	Items        []T    `json:"items"`
	Limit        int    `json:"limit"`
	HasMore      bool   `json:"has_more"`
	NextBefore   string `json:"next_before,omitempty"`
	NextBeforeID string `json:"next_before_id,omitempty"`
}
type AuditLog struct {
	ID         string         `json:"id"`
	TenantID   string         `json:"tenant_id"`
	UserID     string         `json:"user_id,omitempty"`
	Action     string         `json:"action"`
	DatasetID  string         `json:"dataset_id,omitempty"`
	TargetType string         `json:"target_type,omitempty"`
	TargetID   string         `json:"target_id,omitempty"`
	Summary    string         `json:"summary,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
}

type QueryAuditLogsInput struct {
	DatasetID  string `json:"dataset_id,omitempty"`
	Action     string `json:"action,omitempty"`
	UserID     string `json:"user_id,omitempty"`
	TargetType string `json:"target_type,omitempty"`
	TargetID   string `json:"target_id,omitempty"`
	Q          string `json:"q,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	Before     string `json:"before,omitempty"`
	BeforeID   string `json:"before_id,omitempty"`
}

type QueryBackupsInput struct {
	Limit    int    `json:"limit,omitempty"`
	Before   string `json:"before,omitempty"`
	BeforeID string `json:"before_id,omitempty"`
}

type CreateBackupInput struct {
	Name string `json:"name,omitempty"`
	Note string `json:"note,omitempty"`
}

type RestoreBackupInput struct {
	Confirm bool   `json:"confirm"`
	Reason  string `json:"reason,omitempty"`
}

type BackupInfo struct {
	ID          string    `json:"id"`
	Name        string    `json:"name,omitempty"`
	Note        string    `json:"note,omitempty"`
	Engine      string    `json:"engine"`
	Path        string    `json:"path,omitempty"`
	SizeBytes   int64     `json:"size_bytes"`
	SHA256      string    `json:"sha256,omitempty"`
	DownloadURL string    `json:"download_url,omitempty"`
	CreatedBy   string    `json:"created_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type RestoreResult struct {
	Status     string     `json:"status"`
	Backup     BackupInfo `json:"backup"`
	RestoredBy string     `json:"restored_by,omitempty"`
	RestoredAt time.Time  `json:"restored_at"`
}
type Readiness struct {
	Status        string `json:"status"`
	Engine        string `json:"engine"`
	SchemaVersion int    `json:"schema_version"`
}
