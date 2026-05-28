package corelib

import (
	"math"
	"net/http"
	"path/filepath"
	"strings"
)

// RequiredNodeVersion 是项目要求的最低 Node.js 版本。
const RequiredNodeVersion = "24.13.0"

// DefaultContextTokens is the fallback context limit when no explicit
// context_length is configured on the LLM provider.
const DefaultContextTokens = 110_000

// DefaultLLMTimeoutSec is the fallback timeout in seconds when no explicit
// timeout_sec is configured on the LLM provider.
//
// Usage varies by caller:
//   - transport.ResponseHeaderTimeout: wait for first response byte only
//   - http.Client.Timeout: total request timeout (connect + headers + body)
//   - context.WithTimeout: single operation deadline
//
// 240s leaves enough room for slower coding/reasoning models and is the lower
// bound for user-configurable agent timeouts.
const DefaultLLMTimeoutSec = 240

const (
	MinAgentTimeoutSec     = 240
	DefaultAgentTimeoutSec = 240
	MaxAgentTimeoutSec     = 600
)

func NormalizeAgentTimeoutSec(seconds int) int {
	if seconds <= 0 {
		return DefaultAgentTimeoutSec
	}
	if seconds < MinAgentTimeoutSec {
		return MinAgentTimeoutSec
	}
	if seconds > MaxAgentTimeoutSec {
		return MaxAgentTimeoutSec
	}
	return seconds
}

// ModelConfig 描述一个 LLM 模型的配置。
type ModelConfig struct {
	ModelName       string `json:"model_name"`
	ModelId         string `json:"model_id"`
	ModelUrl        string `json:"model_url"`
	ApiKey          string `json:"api_key"`
	WireApi         string `json:"wire_api"` // tool-facing protocol, e.g. "anthropic" or "responses"
	IsCustom        bool   `json:"is_custom"`
	IsBuiltin       bool   `json:"is_builtin"`
	HasSubscription bool   `json:"has_subscription"`
}

// ProjectConfig 描述一个项目的配置。
type ProjectConfig struct {
	Id            string `json:"id"`
	Name          string `json:"name"`
	Path          string `json:"path"`
	YoloMode      bool   `json:"yolo_mode"`
	AdminMode     bool   `json:"admin_mode"`
	PythonProject bool   `json:"python_project"`
	PythonEnv     string `json:"python_env"`
	TeamMode      bool   `json:"team_mode"`
	UseProxy      bool   `json:"use_proxy"`
	ProxyHost     string `json:"proxy_host"`
	ProxyPort     string `json:"proxy_port"`
	ProxyUsername string `json:"proxy_username"`
	ProxyPassword string `json:"proxy_password"`
}

// PythonEnvironment 描述一个 Python 环境。
type PythonEnvironment struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"` // "conda", "venv", or "system"
}

// ToolConfig 描述一个工具的模型配置。
type ToolConfig struct {
	CurrentModel string        `json:"current_model"`
	Models       []ModelConfig `json:"models"`
}

// CodeBuddyModel 描述 CodeBuddy 的模型配置。
type CodeBuddyModel struct {
	Id               string `json:"id"`
	Name             string `json:"name"`
	Vendor           string `json:"vendor"`
	ApiKey           string `json:"apiKey"`
	MaxInputTokens   int    `json:"maxInputTokens"`
	MaxOutputTokens  int    `json:"maxOutputTokens"`
	Url              string `json:"url"`
	SupportsToolCall bool   `json:"supportsToolCall"`
	SupportsImages   bool   `json:"supportsImages"`
}

// CodeBuddyFileConfig 描述 CodeBuddy 的文件配置格式。
type CodeBuddyFileConfig struct {
	Models          []CodeBuddyModel `json:"Models"`
	AvailableModels []string         `json:"availableModels"`
}

// MCPServerSource 标识 MCP 服务器的来源。
type MCPServerSource string

const (
	MCPSourceManual  MCPServerSource = "manual"
	MCPSourceMDNS    MCPServerSource = "mdns"
	MCPSourceProject MCPServerSource = "project"
	MCPSourceMarket  MCPServerSource = "marketplace"
)

// MCPServerEntry 描述一个 MCP 服务器注册条目。
type MCPServerEntry struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	EndpointURL string                  `json:"endpoint_url"`
	AuthType    string                  `json:"auth_type"` // "none", "api_key", "bearer"
	AuthSecret  string                  `json:"auth_secret"`
	Headers     map[string]string       `json:"headers,omitempty"` // custom HTTP headers (e.g. Authorization, X-Custom-Key)
	CreatedAt   string                  `json:"created_at"`
	Source      MCPServerSource         `json:"source"`
	Capability  *MCPServerCapabilityRef `json:"capability,omitempty"`
}

type MCPServerCapabilityRef struct {
	CapabilityID string `json:"capability_id"`
	VersionKey   string `json:"version_key,omitempty"`
	Source       string `json:"source,omitempty"`
	GlobalKey    string `json:"global_key,omitempty"`
}

// SkillCapabilityRef links an installed Skill to a marketplace capability.
// HubSkillID/HubVersion continue to track the underlying SkillHub package,
// while this ref tracks the enterprise capability and version policy.
type SkillCapabilityRef struct {
	CapabilityID string `json:"capability_id"`
	VersionKey   string `json:"version_key,omitempty"`
	Source       string `json:"source,omitempty"`
	GlobalKey    string `json:"global_key,omitempty"`
}

// LocalMCPServerEntry 描述一个本地 MCP 服务器配置（通过命令启动，如 npx）。
type LocalMCPServerEntry struct {
	ID         string                  `json:"id"`
	Name       string                  `json:"name"`
	Command    string                  `json:"command"`
	Args       []string                `json:"args,omitempty"`
	Env        map[string]string       `json:"env,omitempty"`
	Disabled   bool                    `json:"disabled,omitempty"`
	AutoStart  bool                    `json:"auto_start,omitempty"` // only start on app launch when true
	CreatedAt  string                  `json:"created_at"`
	Source     MCPServerSource         `json:"source,omitempty"`
	Capability *MCPServerCapabilityRef `json:"capability,omitempty"`
}

// NLSkillStep 描述自然语言技能中的单个操作步骤。
// StepPollConfig configures repeated execution of a step until a condition is met.
// Used for async tasks where the runner needs to poll for completion.
type StepPollConfig struct {
	Interval    int    `json:"interval"`               // poll interval in seconds (default 5)
	MaxAttempts int    `json:"max_attempts"`           // max poll attempts (default 20)
	UntilMatch  string `json:"until_match,omitempty"`  // regex: stop when output matches
	UntilStatus string `json:"until_status,omitempty"` // shorthand: stop when output contains this string
}

// StepLoopConfig configures iterative execution of a step with verification.
// This implements the "do → verify → improve" cycle from the "5 Skill
// Architecture Patterns" article (Pattern 3: Iterative Loop).
//
// Unlike Poll (passive waiting for async results), Loop is active iteration:
// execute → verify → fix → repeat until verification passes or max iterations.
type StepLoopConfig struct {
	// MaxIterations is the hard upper bound on loop iterations. Required.
	MaxIterations int `json:"max_iterations" yaml:"max_iterations"`
	// UntilStep is the label of the verification step. After each loop body
	// execution, this step runs. If its output matches UntilMatch, the loop exits.
	UntilStep string `json:"until_step,omitempty" yaml:"until_step,omitempty"`
	// UntilMatch is a regex pattern. When the verification step's output matches,
	// the loop exits successfully.
	UntilMatch string `json:"until_match,omitempty" yaml:"until_match,omitempty"`
	// OnFailStep is the label of the repair step (optional). When verification
	// fails, this step runs before the next iteration.
	OnFailStep string `json:"on_fail_step,omitempty" yaml:"on_fail_step,omitempty"`
}

type NLSkillStep struct {
	Action    string                 `json:"action"`
	Params    map[string]interface{} `json:"params"`
	OnError   string                 `json:"on_error"`            // "stop" (default), "continue"
	Name      string                 `json:"name,omitempty"`      // optional descriptive name
	Condition string                 `json:"condition,omitempty"` // "" (always), "on_failure", "on_success"
	When      string                 `json:"when,omitempty"`      // conditional expression, e.g. "{{operation}} == generate"
	Label     string                 `json:"label,omitempty"`     // step selector label for api_workflow mode
	Capture   map[string]string      `json:"capture,omitempty"`   // output capture: varName → regex pattern (first submatch group)
	Poll      *StepPollConfig        `json:"poll,omitempty"`      // poll config for async steps
	// Loop configures iterative execution with verification gate.
	// See StepLoopConfig for the "do → verify → improve" cycle.
	Loop *StepLoopConfig `json:"loop,omitempty"`
	// FallbackStep holds the original step before solidification promotion.
	// When a promoted bash step fails, the Runner reverts to this step.
	// See corelib/skill/solidify.go for the revert mechanism.
	FallbackStep *NLSkillStep `json:"fallback_step,omitempty"`
}

// NLSkillOperation describes a named operation within an api_workflow skill.
// Operations map user intent to step labels, enabling the runner to execute
// only the relevant subset of steps.
type NLSkillOperation struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Params      []string `json:"params,omitempty"`
	Labels      []string `json:"labels,omitempty"`
}

// NLSkillEntry 描述一个自然语言技能条目。
type NLSkillEntry struct {
	Name          string              `json:"name"`
	DirName       string              `json:"dir_name,omitempty"` // 目录名（当与 Name 不同时用于别名查找）
	Description   string              `json:"description"`
	Triggers      []string            `json:"triggers"`
	Steps         []NLSkillStep       `json:"steps"`
	Status        string              `json:"status"` // "active", "disabled"
	CreatedAt     string              `json:"created_at"`
	Source        string              `json:"source"` // "manual" | "learned" | "hub" | "crafted" | "file" | "zip_import" | "github" | "clawhub" | "auto_hub" | "auto_github" | "auto_clawhub"
	SourceProject string              `json:"source_project"`
	HubSkillID    string              `json:"hub_skill_id,omitempty"`
	HubVersion    string              `json:"hub_version,omitempty"`
	Capability    *SkillCapabilityRef `json:"capability,omitempty"`
	TrustLevel    string              `json:"trust_level,omitempty"`
	Type          string              `json:"type,omitempty"`         // "executable" (default) | "knowledge"
	Content       string              `json:"content,omitempty"`      // Markdown content for knowledge-type skills
	Platforms     []string            `json:"platforms,omitempty"`    // "windows","linux","macos"; empty = universal
	RequiresGUI   bool                `json:"requires_gui,omitempty"` // Linux 下是否需要 GUI 环境

	// Tool availability conditions
	RequiresTools       []string `json:"requires_tools,omitempty"`
	FallbackForTools    []string `json:"fallback_for_tools,omitempty"`
	RequiresToolsets    []string `json:"requires_toolsets,omitempty"`
	FallbackForToolsets []string `json:"fallback_for_toolsets,omitempty"`

	// Credential file mounting
	RequiredCredentialFiles []string `json:"required_credential_files,omitempty"`

	// Dependency auto-install
	RequiresPython []string `json:"requires_python,omitempty"` // pip packages to auto-install
	RequiresNode   []string `json:"requires_node,omitempty"`   // npm packages to auto-install
	RequiresBins   []string `json:"requires_bins,omitempty"`   // command-line binaries expected on PATH

	// Plugin namespace
	Publisher string `json:"publisher,omitempty"` // e.g. "lovstudio"

	SkillDir           string              `json:"skill_dir,omitempty"`       // 自包含 skill 目录的绝对路径（运行时填充）
	Mode               string              `json:"mode,omitempty"`            // "sequential" (default) | "interactive" | "api_workflow"
	ExecMode           string              `json:"exec_mode,omitempty"`       // "all" (default) | "first" | "named"
	GlobalTimeout      int                 `json:"global_timeout,omitempty"`  // per-skill global timeout in seconds (0 = use default 300s)
	ProducesArtifact   bool                `json:"produces_artifact"`         // true = expects file output (default); false = diagnostic/instruction only
	Operations         []NLSkillOperation  `json:"operations,omitempty"`      // named operations for api_workflow mode
	RequiredArgs       []string            `json:"required_args,omitempty"`   // required template variables (e.g. "input", "output")
	RequiredEnv        []string            `json:"required_env,omitempty"`    // required environment variables (e.g. "API_KEY")
	PreferredShell     string              `json:"preferred_shell,omitempty"` // "bash" or "cmd"; empty = auto-detect
	UsageCount         int                 `json:"usage_count"`
	SuccessCount       int                 `json:"success_count"`
	FailureCount       int                 `json:"failure_count"`
	WorkaroundCount    int                 `json:"workaround_count"`
	LastUsedAt         string              `json:"last_used_at,omitempty"`
	LastError          string              `json:"last_error,omitempty"`
	RepairAttemptCount int                 `json:"repair_attempt_count,omitempty"`
	LastRepairAt       string              `json:"last_repair_at,omitempty"`
	RepairHistory      []SkillRepairRecord `json:"repair_history,omitempty"`

	// Params is the parameter schema for this skill. When explicitly declared
	// in skill.yaml, it provides aliases, CLI flags, defaults, and descriptions.
	// When absent, SynthesizeParams auto-generates it from command templates.
	// All skills flow through the same BindParams path regardless of source.
	Params []NLSkillParam `json:"params,omitempty"`

	// SolidificationCandidates tracks craft_tool steps that are candidates
	// for promotion to bash steps. See corelib/skill/solidify.go.
	SolidificationCandidates []SolidificationCandidate `json:"solidification_candidates,omitempty"`

	// Stateful marks this skill as having cross-invocation persistent state.
	// When true, the Runner loads state from {skillDir}/.state/state.json
	// before execution and saves it after. See corelib/skill/state.go.
	Stateful bool `json:"stateful,omitempty"`

	// References lists on-demand reference documents in the skill's
	// references/ directory. These are NOT injected into LLM context by
	// default — only an index (filename + description) is injected.
	// LLM loads full content via manage_skill(action=read_ref).
	References []SkillReference `json:"references,omitempty"`

	// Pipeline declares a multi-skill orchestration sequence.
	// When Mode=="pipeline", the Runner delegates to PipelineRunner
	// instead of executing Steps directly.
	Pipeline []SkillPipelineStep `json:"pipeline,omitempty"`
}

// NLSkillParam describes a single parameter in a skill's parameter schema.
// This is the contract between the LLM (which provides args) and the skill
// (which consumes them via command template placeholders or CLI flags).
type NLSkillParam struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Aliases     []string `json:"aliases,omitempty"`   // alternative names the LLM might use
	CLIFlag     string   `json:"cli_flag,omitempty"`  // e.g. "--format" — appended to command
	Default     string   `json:"default,omitempty"`   // default value when not provided
	Required    bool     `json:"required,omitempty"`  // must be provided for execution
	Synthetic   bool     `json:"synthetic,omitempty"` // true = auto-generated from template
}

// SolidificationCandidate tracks a craft_tool step's promotion progress
// toward becoming a fixed bash step. See corelib/skill/solidify.go for the
// three-stage pipeline (Record → Promote → Revert).
type SolidificationCandidate struct {
	StepIndex    int      `json:"step_index"`
	ScriptPath   string   `json:"script_path"`
	Language     string   `json:"language"`
	ParamSlots   []string `json:"param_slots,omitempty"`
	SuccessCount int      `json:"success_count"`
	// Signature is the structural hash of the last recorded script.
	// Consecutive successes only count toward promotion when all scripts
	// share the same signature (same code structure, different parameters).
	// A signature change resets the streak.
	Signature string `json:"signature,omitempty"`
	LastUsed  string `json:"last_used,omitempty"`
}

// SkillRepairRecord stores a single self-repair attempt for audit trail.
type SkillRepairRecord struct {
	Timestamp   string `json:"timestamp"`
	ErrorClass  string `json:"error_class,omitempty"`
	Explanation string `json:"explanation"`
	Success     bool   `json:"success"`
}

// SkillReference describes an on-demand reference document in a skill's
// references/ directory. Only the index (filename + description) is injected
// into LLM context; full content is loaded via manage_skill(action=read_ref).
type SkillReference struct {
	Filename    string `json:"filename"`
	Description string `json:"description,omitempty"` // one-line summary (from first heading)
	TokenCount  int    `json:"token_count,omitempty"` // estimated token count
}

// SkillPipelineStep declares one step in a skill pipeline (Pattern 5:
// Multi-Phase + Checkpoint). See corelib/skill/pipeline.go.
type SkillPipelineStep struct {
	Skill              string            `json:"skill" yaml:"skill"`
	Params             map[string]string `json:"params,omitempty" yaml:"params,omitempty"`
	Checkpoint         bool              `json:"checkpoint,omitempty" yaml:"checkpoint,omitempty"`
	CheckpointMessage  string            `json:"checkpoint_message,omitempty" yaml:"checkpoint_message,omitempty"`
	ContinueOnFail     bool              `json:"continue_on_fail,omitempty" yaml:"continue_on_fail,omitempty"`
	TimeImpactOnReject string            `json:"time_impact_on_reject,omitempty" yaml:"time_impact_on_reject,omitempty"`
}

// MatchesName checks if the skill matches the given name by comparing against
// the qualified name (Publisher:Name), display name (Name), directory name
// (DirName), and the SkillDir basename.
// This allows run_skill to find skills by any of their known identifiers.
// Matching is case-insensitive to improve usability when skill names contain
// mixed-case or CJK characters that may be normalised differently.
//
// When the query contains ":" and the skill has a Publisher, the qualified
// name (Publisher + ":" + Name) is checked first. This gives qualified names
// precedence over bare name matching when resolving collisions.
func (e *NLSkillEntry) MatchesName(query string) bool {
	if query == "" {
		return false
	}
	// Qualified name match: if query contains ":" and skill has a Publisher,
	// check against "Publisher:Name".
	if strings.Contains(query, ":") && e.Publisher != "" {
		qualified := e.Publisher + ":" + e.Name
		if qualified == query || strings.EqualFold(qualified, query) {
			return true
		}
	}
	if e.Name == query || strings.EqualFold(e.Name, query) {
		return true
	}
	if e.DirName != "" && (e.DirName == query || strings.EqualFold(e.DirName, query)) {
		return true
	}
	// Fallback: match by directory basename from SkillDir for skills
	// that were loaded from config without DirName populated.
	if e.SkillDir != "" && e.DirName == "" {
		base := filepath.Base(e.SkillDir)
		if base == query || strings.EqualFold(base, query) {
			return true
		}
	}
	return false
}

// learnedSources is the set of Source values that classify a skill as
// "learned" (self-generated by Maclaw from experience or crafted via craft_tool).
// Auto-installed skills (auto_hub, auto_github, auto_clawhub) are NOT included
// because they originate from external hubs, not from Maclaw's own learning.
var learnedSources = map[string]bool{
	"learned": true,
	"crafted": true,
}

// IsLearnedSource returns true if the given source value indicates a skill
// that was self-generated by Maclaw (learned from experience or crafted via craft_tool).
func IsLearnedSource(source string) bool {
	return learnedSources[strings.ToLower(strings.TrimSpace(source))]
}

// MaclawLLMProvider 描述一个 MaClaw LLM 提供商配置。
type MaclawLLMProvider struct {
	Name           string `json:"name"`
	URL            string `json:"url"`
	Key            string `json:"key"`
	Model          string `json:"model"`
	Protocol       string `json:"protocol,omitempty"`
	ContextLength  int    `json:"context_length,omitempty"`
	TimeoutSec     int    `json:"timeout_sec,omitempty"`
	IsCustom       bool   `json:"is_custom,omitempty"`
	IsHubService   bool   `json:"is_hub_service,omitempty"`
	SupportsVision bool   `json:"supports_vision"`
	AgentType      string `json:"agent_type,omitempty"` // "openclaw" (default) or "claude" → controls User-Agent header
	// ── 新增 OAuth 字段 ──
	AuthType                 string  `json:"auth_type,omitempty"`
	RefreshToken             string  `json:"refresh_token,omitempty"`
	TokenExpiresAt           int64   `json:"token_expires_at,omitempty"`
	OAuthAccessToken         string  `json:"oauth_access_token,omitempty"` // 原始 access_token，仅用于 Costs/Usage API 查询
	WireAPI                  string  `json:"wire_api,omitempty"`           // "chat" or "responses"; empty defaults to "chat"
	InputPricePerMTokensRMB  float64 `json:"input_price_per_m_tokens_rmb,omitempty"`
	OutputPricePerMTokensRMB float64 `json:"output_price_per_m_tokens_rmb,omitempty"`
}

// UserAgent returns the User-Agent header value for LLM API requests.
func (p MaclawLLMProvider) UserAgent() string {
	if p.AgentType != "" {
		return p.AgentType
	}
	return "openclaw"
}

// MaclawLLMConfig 是 MaClaw 桌面 Agent 的 LLM 配置。
type MaclawLLMConfig struct {
	URL            string `json:"url"`
	Key            string `json:"key"`
	Model          string `json:"model"`
	Protocol       string `json:"protocol,omitempty"`
	ContextLength  int    `json:"context_length,omitempty"`
	TimeoutSec     int    `json:"timeout_sec,omitempty"`
	SupportsVision bool   `json:"supports_vision"`
	AgentType      string `json:"agent_type,omitempty"`    // "openclaw" (default) or "claude" → controls User-Agent header
	WireAPI        string `json:"wire_api,omitempty"`      // "chat" or "responses"; empty defaults to "chat"
	ProviderName   string `json:"provider_name,omitempty"` // human-readable provider name (e.g. "智谱编程")
}

// IsResponsesAPI reports whether this config targets the OpenAI Responses API.
// This matches both "responses" (HTTP+SSE) and "responses-ws" (WebSocket) so
// that non-streaming fallback paths work for WebSocket providers.
func (c MaclawLLMConfig) IsResponsesAPI() bool {
	w := strings.ToLower(strings.TrimSpace(c.WireAPI))
	return w == "responses" || w == "responses-ws"
}

// IsResponsesWebSocket reports whether this config targets the OpenAI
// Responses API over WebSocket transport.
func (c MaclawLLMConfig) IsResponsesWebSocket() bool {
	return strings.EqualFold(strings.TrimSpace(c.WireAPI), "responses-ws")
}

type MaclawLLMTestResult struct {
	Message        string `json:"message"`
	SupportsVision bool   `json:"supports_vision"`
}

// WebSearchProvider 描述一个网页搜索 provider 配置。
type WebSearchProvider struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Key     string `json:"key,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
}

// UserAgent returns the User-Agent header value for LLM API requests.
// Returns AgentType directly as the User-Agent string.
// Default is "openclaw" when AgentType is empty.
func (c MaclawLLMConfig) UserAgent() string {
	if c.AgentType != "" {
		return c.AgentType
	}
	return "openclaw"
}

// EffectiveTimeoutSec returns the configured response-header timeout in seconds,
// clamped to the supported agent range.
func (c MaclawLLMConfig) EffectiveTimeoutSec() int {
	return NormalizeAgentTimeoutSec(c.TimeoutSec)
}

// AnthropicMessagesEndpoint returns the Anthropic Messages API endpoint
// derived from the configured URL. If the URL already ends with "/v1",
// it appends "/messages" directly; otherwise it appends "/v1/messages".
// This avoids double "/v1" when the base URL is e.g. "https://host/api/v1".
func AnthropicMessagesEndpoint(baseURL string) string {
	return appendV1Path(baseURL, "/messages")
}

// SetAnthropicAuthHeaders sets both x-api-key and Authorization Bearer headers
// on the request for Anthropic-protocol compatibility. Standard Anthropic uses
// x-api-key; CodeGen and other gateways use Authorization Bearer.
func SetAnthropicAuthHeaders(req *http.Request, key string) {
	if key == "" {
		return
	}
	req.Header.Set("x-api-key", key)
	req.Header.Set("Authorization", "Bearer "+key)
}

// IsAnthropicWireAPI reports whether a tool-facing provider uses the
// Anthropic-compatible wire protocol.
func IsAnthropicWireAPI(wireAPI string) bool {
	return strings.EqualFold(strings.TrimSpace(wireAPI), "anthropic")
}

// NeedsSystemMerge returns true for providers that do not support the "system"
// role in the messages array (e.g. MiniMax). For these providers the system
// content must be merged into the first user message instead.
func NeedsSystemMerge(cfg MaclawLLMConfig) bool {
	return strings.Contains(cfg.URL, "minimaxi.com")
}

// IsDeepSeekThinking reports whether this config targets a DeepSeek model
// that uses thinking mode (reasoning_content). DeepSeek V4+ models have
// thinking mode enabled by default. When tools are present in the request,
// the API requires reasoning_content to be preserved on ALL assistant
// messages — not just those with tool_calls.
//
// Detection: URL contains "deepseek.com" OR model name starts with "deepseek".
// This covers both direct DeepSeek API and third-party proxies that use
// DeepSeek model names.
func IsDeepSeekThinking(cfg MaclawLLMConfig) bool {
	lower := strings.ToLower(cfg.URL)
	if strings.Contains(lower, "deepseek.com") {
		return true
	}
	model := strings.ToLower(cfg.Model)
	return strings.HasPrefix(model, "deepseek")
}

// MergeSystemIntoUser extracts system messages and prepends their content to
// the first user message. Returns a new slice; the original is not modified.
func MergeSystemIntoUser(messages []interface{}) []interface{} {
	var systemParts []string
	var rest []interface{}
	for _, m := range messages {
		role := ""
		content := ""
		switch mm := m.(type) {
		case map[string]interface{}:
			role, _ = mm["role"].(string)
			content, _ = mm["content"].(string)
		case map[string]string:
			role = mm["role"]
			content = mm["content"]
		}
		if role == "system" {
			if content != "" {
				systemParts = append(systemParts, content)
			}
			continue
		}
		rest = append(rest, m)
	}
	if len(systemParts) == 0 {
		return messages // no system messages, return original slice as-is
	}
	prefix := strings.Join(systemParts, "\n")
	for i, m := range rest {
		switch mm := m.(type) {
		case map[string]interface{}:
			if r, _ := mm["role"].(string); r == "user" {
				c, _ := mm["content"].(string)
				patched := make(map[string]interface{}, len(mm))
				for k, v := range mm {
					patched[k] = v
				}
				patched["content"] = prefix + "\n\n" + c
				rest[i] = patched
				return rest
			}
		case map[string]string:
			if mm["role"] == "user" {
				patched := make(map[string]string, len(mm))
				for k, v := range mm {
					patched[k] = v
				}
				patched["content"] = prefix + "\n\n" + patched["content"]
				rest[i] = patched
				return rest
			}
		}
	}
	rest = append([]interface{}{map[string]interface{}{"role": "user", "content": prefix}}, rest...)
	return rest
}

// EffectiveContextTokens returns the usable context window in tokens.
// It uses the configured ContextLength, falling back to DefaultContextTokens.
// A safety margin of 20% is reserved for the model's output.
func (c MaclawLLMConfig) EffectiveContextTokens() int {
	limit := c.ContextLength
	if limit <= 0 {
		limit = DefaultContextTokens
	}
	return limit * 80 / 100 // reserve 20% for output
}

// TokenUsageStat 记录某个 LLM 服务商的累计 token 用量。
type TokenUsageStat struct {
	InputTokens              int64   `json:"input_tokens"`
	OutputTokens             int64   `json:"output_tokens"`
	TotalTokens              int64   `json:"total_tokens"`
	CachedInputTokens        int64   `json:"cached_input_tokens,omitempty"`
	CacheWriteTokens         int64   `json:"cache_write_tokens,omitempty"`
	InputPricePerMTokensRMB  float64 `json:"input_price_per_m_tokens_rmb,omitempty"`
	OutputPricePerMTokensRMB float64 `json:"output_price_per_m_tokens_rmb,omitempty"`
	InputCostRMB             float64 `json:"input_cost_rmb,omitempty"`
	OutputCostRMB            float64 `json:"output_cost_rmb,omitempty"`
	TotalCostRMB             float64 `json:"total_cost_rmb,omitempty"`
	Requests                 int64   `json:"requests,omitempty"`
	CachedRequests           int64   `json:"cached_requests,omitempty"`
}

const (
	DefaultLLMInputPricePerMTokensRMB  = 1.0
	DefaultLLMOutputPricePerMTokensRMB = 2.0
)

func NormalizeLLMTokenPricePerMTokensRMB(value, fallback float64) float64 {
	if !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 {
		return value
	}
	if !math.IsNaN(fallback) && !math.IsInf(fallback, 0) && fallback >= 0 {
		return fallback
	}
	return 0
}

func CalculateLLMCostRMB(inputTokens, outputTokens int64, inputPricePerM, outputPricePerM float64) (float64, float64, float64) {
	inputPricePerM = NormalizeLLMTokenPricePerMTokensRMB(inputPricePerM, DefaultLLMInputPricePerMTokensRMB)
	outputPricePerM = NormalizeLLMTokenPricePerMTokensRMB(outputPricePerM, DefaultLLMOutputPricePerMTokensRMB)
	inputCost := float64(inputTokens) * inputPricePerM / 1_000_000
	outputCost := float64(outputTokens) * outputPricePerM / 1_000_000
	return inputCost, outputCost, inputCost + outputCost
}

// SkillHubEntry 描述一个 SkillHUB 注册端点。
type SkillHubEntry struct {
	Label string `json:"label"`
	URL   string `json:"url"`
	Type  string `json:"type,omitempty"` // "standard", "clawhub", "clawhub_mirror", ""(auto-detect)
}

// Skill 描述一个技能。
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"` // "address" or "zip"
	Value       string `json:"value"`
	Installed   bool   `json:"installed"`
}

// ---------------------------------------------------------------------------
// Token estimation (CJK-aware) — single source of truth
// ---------------------------------------------------------------------------

// EstimateTextTokens estimates the token count of a text string using
// character-based heuristics: ~4 chars/token for ASCII, ~1.5 chars/token
// for CJK. This is the single source of truth for token estimation across
// the entire codebase. All packages MUST call this function instead of
// implementing their own estimation.
func EstimateTextTokens(text string) int {
	var asciiChars, cjkChars int
	for _, r := range text {
		if IsCJKRune(r) {
			cjkChars++
		} else {
			asciiChars++
		}
	}
	// Integer ceiling division: (n + d - 1) / d
	asciiTokens := (asciiChars + 3) / 4
	// For CJK: ceil(cjkChars / 1.5) = (cjkChars*2 + 2) / 3
	cjkTokens := (cjkChars*2 + 2) / 3
	return asciiTokens + cjkTokens
}

// IsCJKRune returns true if the rune is a CJK character.
// Covers: CJK Unified Ideographs, Extension A, Compatibility Ideographs,
// Radicals Supplement, Symbols/Punctuation, Hiragana, Katakana, Hangul.
func IsCJKRune(r rune) bool {
	switch {
	case r >= 0x4E00 && r <= 0x9FFF:
		return true
	case r >= 0x3400 && r <= 0x4DBF:
		return true
	case r >= 0xF900 && r <= 0xFAFF:
		return true
	case r >= 0x2E80 && r <= 0x2EFF:
		return true
	case r >= 0x3000 && r <= 0x303F:
		return true
	case r >= 0x3040 && r <= 0x309F:
		return true
	case r >= 0x30A0 && r <= 0x30FF:
		return true
	case r >= 0xAC00 && r <= 0xD7AF:
		return true
	default:
		return false
	}
}
