package corelib

// RequiredNodeVersion 是项目要求的最低 Node.js 版本。
const RequiredNodeVersion = "24.13.0"

// DefaultContextTokens is the fallback context limit when no explicit
// context_length is configured on the LLM provider.
const DefaultContextTokens = 128_000

// ModelConfig 描述一个 LLM 模型的配置。
type ModelConfig struct {
	ModelName       string `json:"model_name"`
	ModelId         string `json:"model_id"`
	ModelUrl        string `json:"model_url"`
	ApiKey          string `json:"api_key"`
	WireApi         string `json:"wire_api"`
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
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	EndpointURL string          `json:"endpoint_url"`
	AuthType    string          `json:"auth_type"`   // "none", "api_key", "bearer"
	AuthSecret  string          `json:"auth_secret"`
	CreatedAt   string          `json:"created_at"`
	Source      MCPServerSource `json:"source"`
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
type NLSkillStep struct {
	Action  string                 `json:"action"`
	Params  map[string]interface{} `json:"params"`
	OnError string                 `json:"on_error"` // "stop" (default), "continue"
}

// NLSkillEntry 描述一个自然语言技能条目。
type NLSkillEntry struct {
	Name          string        `json:"name"`
	Description   string        `json:"description"`
	Triggers      []string      `json:"triggers"`
	Steps         []NLSkillStep `json:"steps"`
	Status        string        `json:"status"` // "active", "disabled"
	CreatedAt     string        `json:"created_at"`
	Source        string        `json:"source"`         // "manual" | "learned" | "hub" | "crafted" | "file" | "zip_import"
	SourceProject string        `json:"source_project"`
	HubSkillID    string        `json:"hub_skill_id,omitempty"`
	HubVersion    string        `json:"hub_version,omitempty"`
	TrustLevel    string        `json:"trust_level,omitempty"`
	Platforms     []string      `json:"platforms,omitempty"`    // "windows","linux","macos"; empty = universal
	RequiresGUI   bool          `json:"requires_gui,omitempty"` // Linux 下是否需要 GUI 环境
	SkillDir      string        `json:"skill_dir,omitempty"`    // 自包含 skill 目录的绝对路径（运行时填充）
	UsageCount    int           `json:"usage_count"`
	SuccessCount  int           `json:"success_count"`
	LastUsedAt    string        `json:"last_used_at,omitempty"`
	LastError     string        `json:"last_error,omitempty"`
}

// MaclawLLMProvider 描述一个 MaClaw LLM 提供商配置。
type MaclawLLMProvider struct {
	Name           string `json:"name"`
	URL            string `json:"url"`
	Key            string `json:"key"`
	Model          string `json:"model"`
	Protocol       string `json:"protocol,omitempty"`
	ContextLength  int    `json:"context_length,omitempty"`
	IsCustom       bool   `json:"is_custom,omitempty"`
	SupportsVision bool   `json:"supports_vision,omitempty"`
	AgentType      string `json:"agent_type,omitempty"` // Deprecated: ignored; UserAgent() always returns "claude-code/2.0.0"
	// ── 新增 OAuth 字段 ──
	AuthType       string `json:"auth_type,omitempty"`
	RefreshToken   string `json:"refresh_token,omitempty"`
	TokenExpiresAt int64  `json:"token_expires_at,omitempty"`
}

// MaclawLLMConfig 是 MaClaw 桌面 Agent 的 LLM 配置。
type MaclawLLMConfig struct {
	URL            string `json:"url"`
	Key            string `json:"key"`
	Model          string `json:"model"`
	Protocol       string `json:"protocol,omitempty"`
	ContextLength  int    `json:"context_length,omitempty"`
	SupportsVision bool   `json:"supports_vision,omitempty"`
	AgentType      string `json:"agent_type,omitempty"` // Deprecated: ignored; UserAgent() always returns "claude-code/2.0.0"
}

// UserAgent returns the User-Agent header value for LLM API requests.
// Uses "claude-code/2.0.0" as the system-wide default — compatible with
// all major providers and satisfies Kimi's coding-agent whitelist.
func (c MaclawLLMConfig) UserAgent() string {
	return "claude-code/2.0.0"
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
	Type        string `json:"type"`      // "address" or "zip"
	Value       string `json:"value"`
	Installed   bool   `json:"installed"`
}
