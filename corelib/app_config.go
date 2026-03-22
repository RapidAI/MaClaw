package corelib

// AppConfig 是 MaClaw 的完整应用配置。
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
	CheckUpdateOnStartup bool            `json:"check_update_on_startup"`
	// Environment check settings
	PauseEnvCheck    bool   `json:"pause_env_check"`
	EnvCheckDone     bool   `json:"env_check_done"`
	EnvCheckInterval int    `json:"env_check_interval"`
	LastEnvCheckTime string `json:"last_env_check_time"`
	// Proxy settings (global default)
	DefaultProxyHost     string `json:"default_proxy_host"`
	DefaultProxyPort     string `json:"default_proxy_port"`
	DefaultProxyUsername string `json:"default_proxy_username"`
	DefaultProxyPassword string `json:"default_proxy_password"`
	// Terminal settings (Windows only)
	UseWindowsTerminal bool   `json:"use_windows_terminal"`
	RemoteEnabled      bool   `json:"remote_enabled"`
	RemoteHubURL       string `json:"remote_hub_url"`
	RemoteHubCenterURL string `json:"remote_hubcenter_url"`
	RemoteEmail        string `json:"remote_email"`
	RemoteMobile       string `json:"remote_mobile"`
	RemoteSN           string `json:"remote_sn"`
	RemoteUserID       string `json:"remote_user_id"`
	RemoteMachineID    string `json:"remote_machine_id"`
	RemoteMachineToken string `json:"remote_machine_token"`
	RemoteHeartbeatSec int    `json:"remote_heartbeat_sec"`
	RemoteNickname     string `json:"remote_nickname,omitempty"`
	RemoteClientID     string `json:"remote_client_id"`
	DefaultLaunchMode  string `json:"default_launch_mode"`
	// MaClaw LLM configuration
	MaclawLLMUrl             string              `json:"maclaw_llm_url"`
	MaclawLLMKey             string              `json:"maclaw_llm_key"`
	MaclawLLMModel           string              `json:"maclaw_llm_model"`
	MaclawLLMProtocol        string              `json:"maclaw_llm_protocol,omitempty"`
	MaclawLLMContextLength   int                 `json:"maclaw_llm_context_length,omitempty"`
	MaclawLLMProviders       []MaclawLLMProvider `json:"maclaw_llm_providers,omitempty"`
	MaclawLLMCurrentProvider string              `json:"maclaw_llm_current_provider,omitempty"`
	MaclawAgentMaxIterations int                 `json:"maclaw_agent_max_iterations,omitempty"`
	// MaClaw Role configuration
	MaclawRoleName        string `json:"maclaw_role_name,omitempty"`
	MaclawRoleDescription string `json:"maclaw_role_description,omitempty"`
	// MCP Server registry
	MCPServers      []MCPServerEntry      `json:"mcp_servers,omitempty"`
	LocalMCPServers []LocalMCPServerEntry `json:"local_mcp_servers,omitempty"`
	// NL Skills
	NLSkills     []NLSkillEntry `json:"nl_skills,omitempty"`
	SkillHubURLs []SkillHubEntry `json:"skill_hub_urls,omitempty"`
	// Memory
	MemoryAutoCompress bool `json:"memory_auto_compress,omitempty"`
	MemoryMaxBackups   int  `json:"memory_max_backups,omitempty"` // 0 means use default (20)
	// ClawNet
	ClawNetEnabled            bool    `json:"clawnet_enabled"`
	ClawNetAutoPickerEnabled  bool    `json:"clawnet_auto_picker_enabled,omitempty"`
	ClawNetAutoPickerPollMin  int     `json:"clawnet_auto_picker_poll_min,omitempty"`
	ClawNetAutoPickerMinReward float64 `json:"clawnet_auto_picker_min_reward,omitempty"`
	// Security
	SecurityPolicyMode   string `json:"security_policy_mode,omitempty"`
	MaclawDebugToolCalls bool   `json:"maclaw_debug_tool_calls,omitempty"`
	// IM — per-user QQ Bot (client-side gateway)
	QQBotEnabled   bool   `json:"qqbot_enabled,omitempty"`
	QQBotAppID     string `json:"qqbot_app_id,omitempty"`
	QQBotAppSecret string `json:"qqbot_app_secret,omitempty"`
	// IM — per-user Telegram Bot (client-side gateway)
	TelegramBotEnabled bool   `json:"telegram_bot_enabled,omitempty"`
	TelegramBotToken   string `json:"telegram_bot_token,omitempty"`
	// IM — per-user WeChat (client-side gateway via iLink API)
	WeixinEnabled   bool   `json:"weixin_enabled,omitempty"`
	WeixinToken     string `json:"weixin_token,omitempty"`
	WeixinBaseURL   string `json:"weixin_base_url,omitempty"`
	WeixinCDNURL    string `json:"weixin_cdn_url,omitempty"`
	WeixinAccountID string `json:"weixin_account_id,omitempty"`
	WeixinLocalMode *bool  `json:"weixin_local_mode,omitempty"` // nil or true = local (单机), false = remote/Hub (多机)
	// UI mode: "pro" (full coding tools) or "lite" (default, simplified, no coding tools)
	UIMode string `json:"ui_mode,omitempty"`
	// SkillMarket — Skill 获取策略
	SkillPurchaseMode string `json:"skill_purchase_mode,omitempty"` // "auto" (default) | "free_only"
	// Gossip — 聊天八卦自动发布（默认开启）
	GossipAutoPublish bool `json:"gossip_auto_publish"`
}

// IsWeixinLocalMode returns the effective WeChat local mode setting.
// Default is true (单机/local) when the field has never been set.
func (c *AppConfig) IsWeixinLocalMode() bool {
	if c.WeixinLocalMode == nil {
		return true
	}
	return *c.WeixinLocalMode
}

// SetWeixinLocal sets the WeixinLocalMode pointer field.
func (c *AppConfig) SetWeixinLocal(v bool) {
	c.WeixinLocalMode = &v
}
