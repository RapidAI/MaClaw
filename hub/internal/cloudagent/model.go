// Package cloudagent stores Hub cloud-agent instances. The runtime lives in
// maclawsrv. This package does not store transcripts, LLM keys, or git tokens.
package cloudagent

import (
	"context"
	"errors"
	"time"
)

const (
	KindSession = "session"
	KindCoding  = "coding"

	LLMOfficial = "official"
	LLMCustom   = "custom"

	StatusProvisioning    = "provisioning"
	StatusIdle            = "idle"
	StatusRunning         = "running"
	StatusStopped         = "stopped"
	StatusProvisionFailed = "provision_failed"
	StatusDeleted         = "deleted"

	TurnRunning     = "running"
	TurnInterrupted = "interrupted"
	TurnCancelled   = "cancelled"
	TurnFinished    = "finished"

	DefaultMaxInstances       = 3
	DefaultMaxConcurrentTurns = 1

	// DisabledMessage is the closed-feature failure. Create and cloud ops
	// both return it while this account's opening scope is off.
	DisabledMessage = "云端管理尚未打开"
)

// HeartbeatMiss is how long a running turn may go without a heartbeat.
var HeartbeatMiss = 90 * time.Second

// Failure is a client-visible cloud-agent error.
type Failure struct {
	HTTP    int
	Code    string
	Message string
	// detail is the server-side reason for a generic failure. It stays out
	// of Message so it never reaches the client, but logDetail surfaces it.
	detail string
}

func (f *Failure) Error() string {
	if f == nil {
		return ""
	}
	return f.Message
}

func fail(status int, code, message string) error {
	return &Failure{HTTP: status, Code: code, Message: message}
}

func failDisabled() error {
	return fail(403, "CLOUD_AGENTS_DISABLED", DisabledMessage)
}

func failKind() error {
	return fail(400, "KIND_IMMUTABLE", "实例种类创建后不能修改")
}

func failInvalid(message string) error {
	return fail(400, "INVALID", message)
}

func failNotFound() error {
	return fail(404, "NOT_FOUND", "没有找到这个云端实例")
}

func failQuotaInstances() error {
	return fail(409, "QUOTA", "云端实例数量已达上限")
}

func failQuotaTurns() error {
	return fail(409, "QUOTA", "同时进行的云端轮次已达上限")
}

func failSessionGit() error {
	return fail(403, "FORBIDDEN", "会话实例不能连接代码库")
}

func failOffline() error {
	return fail(503, "OFFLINE", "云端运行时不可用")
}

// failOfflineDetail is failOffline with a server-side reason. The client
// still sees only the generic message; the reason is for logs.
func failOfflineDetail(detail string) error {
	f := failOffline().(*Failure)
	f.detail = detail
	return f
}

// logDetail renders an error for the server log, surfacing the hidden
// detail an OFFLINE Failure carries.
func logDetail(err error) string {
	var f *Failure
	if errors.As(err, &f) && f.detail != "" {
		return f.detail
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

// Instance is one user's cloud agent. Kind is fixed at creation.
type Instance struct {
	ID               string `json:"id"`
	TenantID         string `json:"tenant_id"`
	OwnerUserID      string `json:"owner_user_id"`
	Name             string `json:"name"`
	Kind             string `json:"kind"`
	LLMMode          string `json:"llm_mode"`
	LLMLabel         string `json:"llm_label"`
	LLMModel         string `json:"llm_model"`
	Status           string `json:"status"`
	CloudWorkspaceID string `json:"cloud_workspace_id"`
	// SrvNodeID is the private node srv this instance was provisioned on.
	// Empty means the public connection. The pin that created it may move;
	// the instance keeps running where it was provisioned.
	SrvNodeID     string     `json:"srv_node_id,omitempty"`
	RuntimeStatus string     `json:"runtime_status,omitempty"`
	Git           *GitPublic `json:"git,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	DeletedAt     time.Time  `json:"-"`
}

// AdminView is the tenant default the limits card reads and writes.
// SrvID is the global maclawsrv pin: empty means the public connection.
type AdminView struct {
	CloudAgentsEnabled bool   `json:"cloud_agents_enabled"`
	MaxInstances       int    `json:"max_instances"`
	MaxConcurrentTurns int    `json:"max_concurrent_turns"`
	SrvID              string `json:"srv_id"`
}

// AdminSettings is the PATCH body. The switch and both limits are required
// so a partial body cannot turn the feature on or clear a limit by
// omission. SrvID is optional: a nil pointer keeps the stored pin, an
// empty string pins the public connection, anything else is a registered
// srv link or node srv id.
type AdminSettings struct {
	CloudAgentsEnabled *bool   `json:"cloud_agents_enabled"`
	MaxInstances       *int    `json:"max_instances"`
	MaxConcurrentTurns *int    `json:"max_concurrent_turns"`
	SrvID              *string `json:"srv_id"`
}

// UserLimits is an optional per-user numeric override. A null field inherits.
// It does not change the opening switch.
type UserLimits struct {
	MaxInstances       *int `json:"max_instances"`
	MaxConcurrentTurns *int `json:"max_concurrent_turns"`
}

// CreateInput is the user create body.
type CreateInput struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	LLMMode string `json:"llm_mode"`
}

// PatchInput renames an instance or switches its LLM mode. Kind is rejected.
type PatchInput struct {
	Name     *string `json:"name"`
	Kind     *string `json:"kind"`
	LLMMode  *string `json:"llm_mode"`
	LLMLabel *string `json:"llm_label"`
	LLMModel *string `json:"llm_model"`
}

// CustomLLM is stored on maclawsrv. Hub keeps only the mode, label, and model.
type CustomLLM struct {
	URL           string `json:"url"`
	Key           string `json:"key"`
	Model         string `json:"model"`
	Protocol      string `json:"protocol"`
	WireAPI       string `json:"wire_api"`
	AgentType       string `json:"agent_type,omitempty"`
	ContextLength   int    `json:"context_length,omitempty"`
	TimeoutSec      int    `json:"timeout_sec,omitempty"`
	MaxOutputTokens int    `json:"max_output_tokens,omitempty"`
	Label           string `json:"label"`
}

// GitConnect is forwarded to maclawsrv. The token is not written to Hub.
type GitConnect struct {
	Service     string `json:"service"`
	Origin      string `json:"origin"`
	Repo        string `json:"repo"`
	Ref         string `json:"ref,omitempty"`
	Token       string `json:"token,omitempty"`
	AuthorName  string `json:"author_name"`
	AuthorEmail string `json:"author_email"`
	// Source "account" marks a token pushed from the signed-in GitHub
	// account; clients refresh it after re-login. Hub forwards, never stores.
	Source string `json:"source,omitempty"`
}

// GitPublic is the git view returned to the GUI. It has no token field.
type GitPublic struct {
	Connected   bool   `json:"connected"`
	Service     string `json:"service,omitempty"`
	Origin      string `json:"origin,omitempty"`
	Repo        string `json:"repo,omitempty"`
	Ref         string `json:"ref,omitempty"`
	AuthorName  string `json:"author_name,omitempty"`
	AuthorEmail string `json:"author_email,omitempty"`
	Source      string `json:"source,omitempty"`
	Status      string `json:"status,omitempty"`
}

// Settings reads the tenant bot-connection record. Cloud dispatch uses the
// base URL and admin secret from that record and does not call botmgmt.
type Settings interface {
	Get(ctx context.Context, key string) (string, error)
}
