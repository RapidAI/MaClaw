package corelib

import (
	"net/http"
	"path/filepath"
	"strings"
)

// RequiredNodeVersion 是项目要求的最低 Node.js 版本。
const RequiredNodeVersion = "24.13.0"

// DefaultContextTokens is the fallback context limit when no explicit
// context_length is configured on the LLM provider.
const DefaultContextTokens = 128_000

// DefaultLLMTimeoutSec is the fallback response-header timeout in seconds
// when no explicit timeout_sec is configured on the LLM provider.
const DefaultLLMTimeoutSec = 360

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
)

// MCPServerEntry 描述一个 MCP 服务器注册条目。
type MCPServerEntry struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	EndpointURL string            `json:"endpoint_url"`
	AuthType    string            `json:"auth_type"` // "none", "api_key", "bearer"
	AuthSecret  string            `json:"auth_secret"`
	Headers     map[string]string `json:"headers,omitempty"` // custom HTTP headers (e.g. Authorization, X-Custom-Key)
	CreatedAt   string            `json:"created_at"`
	Source      MCPServerSource   `json:"source"`
}

// LocalMCPServerEntry 描述一个本地 MCP 服务器配置（通过命令启动，如 npx）。
type LocalMCPServerEntry struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Command   string            `json:"command"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Disabled  bool              `json:"disabled,omitempty"`
	AutoStart bool              `json:"auto_start,omitempty"` // only start on app launch when true
	CreatedAt string            `json:"created_at"`
}

// NLSkillStep 描述自然语言技能中的单个操作步骤。
// StepPollConfig configures repeated execution of a step until a condition is met.
// Used for async tasks where the runner needs to poll for completion.
type StepPollConfig struct {
	Interval    int    `json:"interval"`              // poll interval in seconds (default 5)
	MaxAttempts int    `json:"max_attempts"`           // max poll attempts (default 20)
	UntilMatch  string `json:"until_match,omitempty"`  // regex: stop when output matches
	UntilStatus string `json:"until_status,omitempty"` // shorthand: stop when output contains this string
}

type NLSkillStep struct {
	Action    string                 `json:"action"`
	Params    map[string]interface{} `json:"params"`
	OnError   string                 `json:"on_error"`   // "stop" (default), "continue"
	Name      string                 `json:"name,omitempty"`      // optional descriptive name
	Condition string                 `json:"condition,omitempty"` // "" (always), "on_failure", "on_success"
	When      string                 `json:"when,omitempty"`      // conditional expression, e.g. "{{operation}} == generate"
	Label     string                 `json:"label,omitempty"`     // step selector label for api_workflow mode
	Capture   map[string]string      `json:"capture,omitempty"`   // output capture: varName → regex pattern (first submatch group)
	Poll      *StepPollConfig        `json:"poll,omitempty"`      // poll config for async steps
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
	Name          string        `json:"name"`
	DirName       string        `json:"dir_name,omitempty"`     // 目录名（当与 Name 不同时用于别名查找）
	Description   string        `json:"description"`
	Triggers      []string      `json:"triggers"`
	Steps         []NLSkillStep `json:"steps"`
	Status        string        `json:"status"` // "active", "disabled"
	CreatedAt     string        `json:"created_at"`
	Source        string        `json:"source"` // "manual" | "learned" | "hub" | "crafted" | "file" | "zip_import" | "github" | "clawhub" | "auto_hub" | "auto_github" | "auto_clawhub"
	SourceProject string        `json:"source_project"`
	HubSkillID    string        `json:"hub_skill_id,omitempty"`
	HubVersion    string        `json:"hub_version,omitempty"`
	TrustLevel    string        `json:"trust_level,omitempty"`
	Type          string        `json:"type,omitempty"`          // "executable" (default) | "knowledge"
	Content       string        `json:"content,omitempty"`       // Markdown content for knowledge-type skills
	Platforms        []string      `json:"platforms,omitempty"`    // "windows","linux","macos"; empty = universal
	RequiresGUI      bool          `json:"requires_gui,omitempty"` // Linux 下是否需要 GUI 环境

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

	// Plugin namespace
	Publisher string `json:"publisher,omitempty"` // e.g. "lovstudio"

	SkillDir         string        `json:"skill_dir,omitempty"`    // 自包含 skill 目录的绝对路径（运行时填充）
	Mode             string        `json:"mode,omitempty"`         // "sequential" (default) | "interactive" | "api_workflow"
	ExecMode         string        `json:"exec_mode,omitempty"`    // "all" (default) | "first" | "named"
	GlobalTimeout    int           `json:"global_timeout,omitempty"` // per-skill global timeout in seconds (0 = use default 300s)
	ProducesArtifact bool                `json:"produces_artifact"`          // true = expects file output (default); false = diagnostic/instruction only
	Operations       []NLSkillOperation  `json:"operations,omitempty"`       // named operations for api_workflow mode
	RequiredArgs     []string      `json:"required_args,omitempty"`  // required template variables (e.g. "input", "output")
	RequiredEnv      []string      `json:"required_env,omitempty"`   // required environment variables (e.g. "API_KEY")
	PreferredShell   string        `json:"preferred_shell,omitempty"` // "bash" or "cmd"; empty = auto-detect
	UsageCount       int           `json:"usage_count"`
	SuccessCount     int           `json:"success_count"`
	FailureCount     int           `json:"failure_count"`
	WorkaroundCount  int           `json:"workaround_count"`
	LastUsedAt       string        `json:"last_used_at,omitempty"`
	LastError        string        `json:"last_error,omitempty"`
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
	return learnedSources[source]
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
	SupportsVision bool   `json:"supports_vision"`
	AgentType      string `json:"agent_type,omitempty"` // "openclaw" (default) or "claude" → controls User-Agent header
	// ── 新增 OAuth 字段 ──
	AuthType         string `json:"auth_type,omitempty"`
	RefreshToken     string `json:"refresh_token,omitempty"`
	TokenExpiresAt   int64  `json:"token_expires_at,omitempty"`
	OAuthAccessToken string `json:"oauth_access_token,omitempty"` // 原始 access_token，仅用于 Costs/Usage API 查询
	WireAPI          string `json:"wire_api,omitempty"`           // "chat" or "responses"; empty defaults to "chat"
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
// falling back to DefaultLLMTimeoutSec when unset or invalid.
func (c MaclawLLMConfig) EffectiveTimeoutSec() int {
	if c.TimeoutSec > 0 {
		return c.TimeoutSec
	}
	return DefaultLLMTimeoutSec
}

// AnthropicMessagesEndpoint returns the Anthropic Messages API endpoint
// derived from the configured URL. If the URL already ends with "/v1",
// it appends "/messages" directly; otherwise it appends "/v1/messages".
// This avoids double "/v1" when the base URL is e.g. "https://host/api/v1".
func AnthropicMessagesEndpoint(baseURL string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(trimmed, "/v1") {
		return trimmed + "/messages"
	}
	return trimmed + "/v1/messages"
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
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
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
