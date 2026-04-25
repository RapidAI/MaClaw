package corelib

// AppConfig is the complete application configuration for MaClaw.
import (
	"encoding/json"
	"strings"
)

type AppConfig struct {
	Claude               ToolConfig      `json:"claude"`
	Gemini               ToolConfig      `json:"gemini"`
	Codex                ToolConfig      `json:"codex"`
	Opencode             ToolConfig      `json:"opencode"`
	CodeBuddy            ToolConfig      `json:"codebuddy"`
	IFlow                ToolConfig      `json:"iflow"`
	Kilo                 ToolConfig      `json:"kilo"`
	Cursor               ToolConfig      `json:"cursor"`
	Projects             []ProjectConfig `json:"projects"`
	CurrentProject       string          `json:"current_project"`
	ActiveTool           string          `json:"active_tool"`
	DefaultTool          string          `json:"default_tool"`
	DefaultToolProvider  string          `json:"default_tool_provider"`
	HideStartupPopup     bool            `json:"hide_startup_popup"`
	HideMaclawLLMPopup   bool            `json:"hide_maclaw_llm_popup"`
	ShowGemini           bool            `json:"show_gemini"`
	ShowCodex            bool            `json:"show_codex"`
	ShowOpenCode         bool            `json:"show_opencode"`
	ShowCodeBuddy        bool            `json:"show_codebuddy"`
	ShowIFlow            bool            `json:"show_iflow"`
	ShowKilo             bool            `json:"show_kilo"`
	ShowCursor           bool            `json:"show_cursor"`
	Language             string          `json:"language"`
	PowerOptimization    bool            `json:"power_optimization"`
	ScreenDimTimeoutMin  int             `json:"screen_dim_timeout_min"`
	WorkstationMode      bool            `json:"workstation_mode"`
	CheckUpdateOnStartup bool            `json:"check_update_on_startup"`
	// Environment check settings
	PauseEnvCheck    bool   `json:"pause_env_check"`
	EnvCheckDone     bool   `json:"env_check_done"`
	EnvCheckInterval int    `json:"env_check_interval"`
	LastEnvCheckTime string `json:"last_env_check_time"`
	// Proxy settings (global default)
	DefaultProxyEnabled          bool   `json:"default_proxy_enabled"`
	DefaultProxyProtocol         string `json:"default_proxy_protocol,omitempty"` // "http", "https", "socks5"
	DefaultProxyHost             string `json:"default_proxy_host"`
	DefaultProxyPort             string `json:"default_proxy_port"`
	DefaultProxyUsername         string `json:"default_proxy_username"`
	DefaultProxyPassword         string `json:"default_proxy_password"`
	DefaultProxyBypass           string `json:"default_proxy_bypass,omitempty"`             // semicolon-separated bypass list
	DefaultProxyScopeMaclaw      bool   `json:"default_proxy_scope_maclaw,omitempty"`       // MacClaw LLM
	DefaultProxyScopeCodingTools bool   `json:"default_proxy_scope_coding_tools,omitempty"` // coding tools (macOS/Linux only)
	DefaultProxyScopeAgent       bool   `json:"default_proxy_scope_agent,omitempty"`        // web_search / web_fetch
	// Terminal settings (Windows only)
	UseWindowsTerminal  bool     `json:"use_windows_terminal"`
	RemoteEnabled       bool     `json:"remote_enabled"`
	RemoteHubURL        string   `json:"remote_hub_url"`
	RemoteHubCenterURL  string   `json:"remote_hubcenter_url"`
	RemoteHubCenterURLs []string `json:"remote_hubcenter_urls,omitempty"`
	RemoteEmail         string   `json:"remote_email"`
	RemoteMobile        string   `json:"remote_mobile"`
	RemoteSN            string   `json:"remote_sn"`
	RemoteUserID        string   `json:"remote_user_id"`
	RemoteMachineID     string   `json:"remote_machine_id"`
	RemoteMachineToken  string   `json:"remote_machine_token"`
	RemoteViewerToken   string   `json:"remote_viewer_token,omitempty"`
	RemoteHeartbeatSec  int      `json:"remote_heartbeat_sec"`
	RemoteNickname      string   `json:"remote_nickname,omitempty"`
	RemoteClientID      string   `json:"remote_client_id"`
	DefaultLaunchMode   string   `json:"default_launch_mode"`
	// MaClaw LLM configuration
	MaclawLLMUrl             string              `json:"maclaw_llm_url"`
	MaclawLLMKey             string              `json:"maclaw_llm_key"`
	MaclawLLMModel           string              `json:"maclaw_llm_model"`
	MaclawLLMProtocol        string              `json:"maclaw_llm_protocol,omitempty"`
	MaclawLLMContextLength   int                 `json:"maclaw_llm_context_length,omitempty"`
	MaclawLLMTimeoutSec      int                 `json:"maclaw_llm_timeout_sec,omitempty"`
	MaclawLLMProviders       []MaclawLLMProvider `json:"maclaw_llm_providers,omitempty"`
	MaclawLLMCurrentProvider string              `json:"maclaw_llm_current_provider,omitempty"`
	WebSearchProviders       []WebSearchProvider `json:"web_search_providers,omitempty"`
	WebSearchCurrentProvider string              `json:"web_search_current_provider,omitempty"`
	MaclawAgentMaxIterations int                 `json:"maclaw_agent_max_iterations,omitempty"`
	// MaClaw Role configuration
	MaclawRoleName        string `json:"maclaw_role_name,omitempty"`
	MaclawRoleDescription string `json:"maclaw_role_description,omitempty"`
	// MCP Server registry
	MCPServers      []MCPServerEntry      `json:"mcp_servers,omitempty"`
	LocalMCPServers []LocalMCPServerEntry `json:"local_mcp_servers,omitempty"`
	// NL Skills
	NLSkills          []NLSkillEntry  `json:"nl_skills,omitempty"`
	SkillHubURLs      []SkillHubEntry `json:"skill_hub_urls,omitempty"`
	ExternalSkillDirs []string        `json:"external_skill_dirs,omitempty"` // user-added external skill directories
	// Memory
	MemoryAutoCompress bool `json:"memory_auto_compress,omitempty"`
	MemoryMaxBackups   int  `json:"memory_max_backups,omitempty"` // 0 means use default (20)
	// AgentNet
	AgentNetEnabled             bool    `json:"agentnet_enabled"`
	AgentNetAutoPickerEnabled   bool    `json:"agentnet_auto_picker_enabled,omitempty"`
	AgentNetAutoPickerPollMin   int     `json:"agentnet_auto_picker_poll_min,omitempty"`
	AgentNetAutoPickerMinReward float64 `json:"agentnet_auto_picker_min_reward,omitempty"`
	// Security
	SecurityPolicyMode   string `json:"security_policy_mode,omitempty"`
	SandboxMode          string `json:"sandbox_mode,omitempty"`  // "none" (default), "os", "docker"
	NetworkLevel         string `json:"network_level,omitempty"` // "none", "intranet", "full" (default)
	YoloModeAllowed      bool   `json:"yolo_mode_allowed"`       // default true
	GossipEnabled        bool   `json:"gossip_enabled"`          // default true (local preference, overridden by Hub)
	FileOutboundEnabled  bool   `json:"file_outbound_enabled"`   // default true
	ImageOutboundEnabled bool   `json:"image_outbound_enabled"`  // default true
	MaclawDebugToolCalls bool   `json:"maclaw_debug_tool_calls,omitempty"`
	ShowAITraceEntry     bool   `json:"show_ai_trace_entry,omitempty"`
	ShowAssistantEntry   bool   `json:"show_assistant_entry"`
	FloatingBtnX         int    `json:"floating_btn_x,omitempty"`
	FloatingBtnY         int    `json:"floating_btn_y,omitempty"`
	LogDetailEnabled     bool   `json:"log_detail_enabled,omitempty"`
	// IM 闂?per-user QQ Bot (client-side gateway)
	QQBotEnabled   bool   `json:"qqbot_enabled,omitempty"`
	QQBotAppID     string `json:"qqbot_app_id,omitempty"`
	QQBotAppSecret string `json:"qqbot_app_secret,omitempty"`
	// IM 闂?per-user Telegram Bot (client-side gateway)
	TelegramBotEnabled bool   `json:"telegram_bot_enabled,omitempty"`
	TelegramBotToken   string `json:"telegram_bot_token,omitempty"`
	// IM 闂?per-user WeChat (client-side gateway via iLink API)
	WeixinEnabled   bool   `json:"weixin_enabled,omitempty"`
	WeixinToken     string `json:"weixin_token,omitempty"`
	WeixinBaseURL   string `json:"weixin_base_url,omitempty"`
	WeixinCDNURL    string `json:"weixin_cdn_url,omitempty"`
	WeixinAccountID string `json:"weixin_account_id,omitempty"`
	WeixinLocalMode *bool  `json:"weixin_local_mode,omitempty"` // nil or true = local (闂佸憡顨嗗ú妯衡攦閳?, false = remote/Hub (婵犮垼鍩栫喊宥呪攦閳?
	// IM 闂?Lansenger (闂佽棄鍟换鈧ǎ? client-side gateway
	LansengerEnabled    bool   `json:"lansenger_enabled,omitempty"`
	LansengerAppID      string `json:"lansenger_app_id,omitempty"`
	LansengerAppSecret  string `json:"lansenger_app_secret,omitempty"`
	LansengerGatewayURL string `json:"lansenger_gateway_url,omitempty"` // API gateway base URL, default https://apigw.lx.qianxin.com
	// IM 闂?local mode toggles for QQ Bot and Telegram (same semantics as WeChat)
	QQBotLocalMode     *bool `json:"qqbot_local_mode,omitempty"`     // nil = auto-detect, true = local, false = hub
	TelegramLocalMode  *bool `json:"telegram_local_mode,omitempty"`  // nil = auto-detect, true = local, false = hub
	LansengerLocalMode *bool `json:"lansenger_local_mode,omitempty"` // nil = auto-detect, true = local, false = hub
	// Extra tool configs for OEM brands (keyed by ExtraToolDef.ConfigKey)
	ExtraToolConfigs map[string]ToolConfig `json:"extra_tool_configs,omitempty"`
	// UI mode: "pro" (full coding tools) or "lite" (default, simplified, no coding tools)
	UIMode string `json:"ui_mode,omitempty"`
	// Skill purchase mode.
	SkillPurchaseMode string `json:"skill_purchase_mode,omitempty"` // "auto" (default) | "free_only"
	// Gossip auto publish setting.
	GossipAutoPublish bool `json:"gossip_auto_publish"`
	// LLM trajectory logging.
	LLMTrajectoryLogging bool `json:"llm_trajectory_logging,omitempty"`
	// Trial-and-Reflect setting.
	TrialReflectEnabled bool `json:"trial_reflect_enabled,omitempty"`
	// LLM token usage statistics.
	LLMTokenUsage map[string]*TokenUsageStat `json:"llm_token_usage,omitempty"`
	// Onboarding completion flag.
	OnboardingDone bool `json:"onboarding_done,omitempty"`
	// Embedding / vector search toggle.
	VectorSearchEnabled bool `json:"vector_search_enabled"`
	// ASR toggle.
	ASREnabled bool `json:"asr_enabled"`
	// Screen parsing (YOLO) toggle — enables vision-based UI element detection.
	// Default: enabled (nil = true). Uses *bool so we can distinguish "not set" from "false".
	ScreenParsingEnabled *bool `json:"screen_parsing_enabled,omitempty"`
	// UI zoom factor (0.5 ~ 2.0, 0 = default 1.0).
	UIZoomFactor float64 `json:"ui_zoom_factor,omitempty"`
	// Chat font size in pixels (12 ~ 24, 0 = default 14).
	ChatFontSize int `json:"chat_font_size,omitempty"`
	// SSH host presets.
	SSHHosts []SSHHostEntry `json:"ssh_hosts,omitempty"`
	// Knowledge Skill token budget.
	KnowledgeSkillTokenBudget int `json:"knowledge_skill_token_budget,omitempty"`
	// AuxiliaryLLM 闂?lightweight LLM for background tasks (compression,
	// skill repair, session search summarization). When configured, used
	// in preference to the main LLM to reduce cost and latency.
	AuxiliaryLLM AuxiliaryLLMConfig `json:"auxiliary_llm,omitempty"`
	// NudgeDisabled — when true, the post-use skill nudge system is
	// completely disabled. No nudge messages will be injected into the
	// conversation after complex tasks, skill failures, or user corrections.
	NudgeDisabled bool `json:"nudge_disabled,omitempty"`
	// WorkingDirectory is the user-configured default working directory for
	// agent tasks (bash, craft_tool, confirmation panel, etc.). When empty,
	// falls back to ~/.maclaw/workspace via corelib.WorkspaceDir().
	WorkingDirectory string `json:"working_directory,omitempty"`
}

func (c AppConfig) MarshalJSON() ([]byte, error) {
	type appConfigAlias AppConfig
	return json.Marshal(appConfigAlias(c))
}

func (c *AppConfig) UnmarshalJSON(data []byte) error {
	type appConfigAlias AppConfig
	type rawAppConfig struct {
		appConfigAlias
		ShowAssistantEntry          *bool    `json:"show_assistant_entry"`
		AgentNetEnabled             *bool    `json:"agentnet_enabled"`
		AgentNetAutoPickerEnabled   *bool    `json:"agentnet_auto_picker_enabled,omitempty"`
		AgentNetAutoPickerPollMin   *int     `json:"agentnet_auto_picker_poll_min,omitempty"`
		AgentNetAutoPickerMinReward *float64 `json:"agentnet_auto_picker_min_reward,omitempty"`
	}

	var raw rawAppConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*c = AppConfig(raw.appConfigAlias)
	if raw.ShowAssistantEntry == nil {
		c.ShowAssistantEntry = true
	} else {
		c.ShowAssistantEntry = *raw.ShowAssistantEntry
	}
	if raw.AgentNetEnabled != nil {
		c.AgentNetEnabled = *raw.AgentNetEnabled
	}
	if raw.AgentNetAutoPickerEnabled != nil {
		c.AgentNetAutoPickerEnabled = *raw.AgentNetAutoPickerEnabled
	}
	if raw.AgentNetAutoPickerPollMin != nil {
		c.AgentNetAutoPickerPollMin = *raw.AgentNetAutoPickerPollMin
	}
	if raw.AgentNetAutoPickerMinReward != nil {
		c.AgentNetAutoPickerMinReward = *raw.AgentNetAutoPickerMinReward
	}
	return nil
}

// AuxiliaryLLMConfig holds the configuration for a lightweight LLM used for
// background tasks: memory compression, skill repair, session search
// summarization, etc. Mirrors llm.AuxiliaryConfig but lives in corelib to
// avoid circular imports.
type AuxiliaryLLMConfig struct {
	URL      string `json:"url"`
	Key      string `json:"key"`
	Model    string `json:"model"`
	Protocol string `json:"protocol,omitempty"` // "openai" (default) or "anthropic"
}

// IsConfigured returns true if the auxiliary LLM has a URL and key set.
func (c AuxiliaryLLMConfig) IsConfigured() bool {
	return c.URL != "" && c.Key != ""
}

// SSHHostEntry describes a preconfigured SSH remote host.
type SSHHostEntry struct {
	Label      string `json:"label"`                 // Human-readable label, e.g. "prod-web-01"
	Host       string `json:"host"`                  // Hostname or IP address
	Port       int    `json:"port,omitempty"`        // Default 22
	User       string `json:"user"`                  // Login username
	AuthMethod string `json:"auth_method,omitempty"` // key/password/agent
	KeyPath    string `json:"key_path,omitempty"`    // Private key path
}

// IsWeixinLocalMode returns the effective WeChat local mode setting.
// When the field has never been explicitly set (nil):
//   - If Hub is activated (RemoteMachineID is set), default to Hub/婵犮垼鍩栫喊宥呪攦閳?mode (false)
//   - Otherwise, default to local/闂佸憡顨嗗ú妯衡攦閳?mode (true)
func (c *AppConfig) IsWeixinLocalMode() bool {
	if c.WeixinLocalMode == nil {
		// Auto-detect: if Hub is activated, default to Hub mode
		if c.RemoteMachineID != "" {
			return false
		}
		return true
	}
	return *c.WeixinLocalMode
}

// SetWeixinLocal sets the WeixinLocalMode pointer field.
func (c *AppConfig) SetWeixinLocal(v bool) {
	c.WeixinLocalMode = &v
}

// IsQQBotLocalMode returns the effective QQ Bot local mode setting.
func (c *AppConfig) IsQQBotLocalMode() bool {
	if c.QQBotLocalMode == nil {
		if c.RemoteMachineID != "" {
			return false
		}
		return true
	}
	return *c.QQBotLocalMode
}

// SetQQBotLocal sets the QQBotLocalMode pointer field.
func (c *AppConfig) SetQQBotLocal(v bool) {
	c.QQBotLocalMode = &v
}

// IsTelegramLocalMode returns the effective Telegram local mode setting.
func (c *AppConfig) IsTelegramLocalMode() bool {
	if c.TelegramLocalMode == nil {
		if c.RemoteMachineID != "" {
			return false
		}
		return true
	}
	return *c.TelegramLocalMode
}

// SetTelegramLocal sets the TelegramLocalMode pointer field.
func (c *AppConfig) SetTelegramLocal(v bool) {
	c.TelegramLocalMode = &v
}

// LansengerApiGatewayURL returns the effective API gateway URL.
// Falls back to the default Lansenger gateway if not configured.
func (c *AppConfig) LansengerApiGatewayURL() string {
	url := strings.TrimSpace(c.LansengerGatewayURL)
	if url != "" {
		return url
	}
	return "https://apigw.lx.qianxin.com"
}

// IsLansengerLocalMode returns the effective Lansenger local mode setting.
func (c *AppConfig) IsLansengerLocalMode() bool {
	if c.LansengerLocalMode == nil {
		if c.RemoteMachineID != "" {
			return false
		}
		return true
	}
	return *c.LansengerLocalMode
}

// SetLansengerLocal sets the LansengerLocalMode pointer field.
func (c *AppConfig) SetLansengerLocal(v bool) {
	c.LansengerLocalMode = &v
}

// SkillHubBaseURL returns the base URL for SkillHub APIs (/api/v1/skills/*).
// SkillHub is hosted on the HubCenter server, NOT on the user's private Hub.
// All Skill search, download, install, rate, and update operations use this URL.
func (c *AppConfig) HubCenterBaseURLs(defaultHubCenterURL string, defaultHubCenterURLs []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(defaultHubCenterURLs)+2)
	add := func(value string) {
		value = strings.TrimRight(strings.TrimSpace(value), "/")
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	add(c.RemoteHubCenterURL)
	for _, value := range c.RemoteHubCenterURLs {
		add(value)
	}
	add(defaultHubCenterURL)
	for _, value := range defaultHubCenterURLs {
		add(value)
	}
	return out
}

func (c *AppConfig) SkillHubBaseURL(defaultHubCenterURL string) string {
	u := strings.TrimSpace(c.RemoteHubCenterURL)
	if u == "" {
		u = defaultHubCenterURL
	}
	return strings.TrimRight(u, "/")
}

// SkillMarketBaseURL returns the base URL for SkillMarket APIs (/api/v1/skillmarket/*).
// SkillMarket is hosted on the same HubCenter server as SkillHub.
func (c *AppConfig) SkillMarketBaseURL(defaultHubCenterURL string) string {
	return c.SkillHubBaseURL(defaultHubCenterURL)
}
