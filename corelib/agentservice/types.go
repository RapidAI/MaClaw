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
	CredentialStatusActive  CredentialStatus = "active"
	CredentialStatusRevoked CredentialStatus = "revoked"
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

type Tenant struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Status    TenantStatus `json:"status"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

type User struct {
	ID        string     `json:"id"`
	TenantID  string     `json:"tenant_id"`
	Name      string     `json:"name"`
	Email     string     `json:"email,omitempty"`
	Status    UserStatus `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type Credential struct {
	ID           string           `json:"id"`
	TenantID     string           `json:"tenant_id"`
	UserID       string           `json:"user_id"`
	Name         string           `json:"name"`
	APIKey       string           `json:"api_key,omitempty"`
	APIKeyPrefix string           `json:"api_key_prefix,omitempty"`
	APIKeyHash   string           `json:"api_key_hash,omitempty"`
	Status       CredentialStatus `json:"status"`
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
	TenantID     string `json:"tenant_id,omitempty"`
	UserID       string `json:"user_id,omitempty"`
	Action       string `json:"action,omitempty"`
	ResourceType string `json:"resource_type,omitempty"`
}

type ListRunsInput struct {
	Status    RunStatus `json:"status,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
}

type UsageSummary struct {
	TenantID          string            `json:"tenant_id"`
	UserID            string            `json:"user_id"`
	DataDir           string            `json:"data_dir"`
	Instances         int               `json:"instances"`
	ReadyInstances    int               `json:"ready_instances"`
	StoppedInstances  int               `json:"stopped_instances"`
	Sessions          int               `json:"sessions"`
	Messages          int               `json:"messages"`
	UserMessages      int               `json:"user_messages"`
	AssistantMessages int               `json:"assistant_messages"`
	Runs              int               `json:"runs"`
	RunsByStatus      map[RunStatus]int `json:"runs_by_status"`
	LastActivityAt    *time.Time        `json:"last_activity_at,omitempty"`
}

type CreateTenantInput struct {
	Name string `json:"name"`
}

type CreateUserInput struct {
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`
	Email    string `json:"email,omitempty"`
}

type UpdateTenantInput struct {
	Name   *string       `json:"name,omitempty"`
	Status *TenantStatus `json:"status,omitempty"`
}

type UpdateUserInput struct {
	Name   *string     `json:"name,omitempty"`
	Email  *string     `json:"email,omitempty"`
	Status *UserStatus `json:"status,omitempty"`
}

type CreateCredentialInput struct {
	TenantID  string `json:"tenant_id"`
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	APIKey    string `json:"api_key"`
	APISecret string `json:"api_secret"`
}

type IssueTokenInput struct {
	APIKey    string `json:"api_key"`
	APISecret string `json:"api_secret"`
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

type CreateSessionInput struct {
	AgentID  string            `json:"agent_id"`
	Title    string            `json:"title,omitempty"`
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
