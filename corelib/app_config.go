package corelib

// AppConfig is the complete application configuration for MaClaw.
import (
	"encoding/json"
	"strings"
)

type AppConfig struct {
	Claude              ToolConfig      `json:"claude"`
	Gemini              ToolConfig      `json:"gemini"`
	Codex               ToolConfig      `json:"codex"`
	Opencode            ToolConfig      `json:"opencode"`
	CodeBuddy           ToolConfig      `json:"codebuddy"`
	IFlow               ToolConfig      `json:"iflow"`
	Kilo                ToolConfig      `json:"kilo"`
	Cursor              ToolConfig      `json:"cursor"`
	Projects            []ProjectConfig `json:"projects"`
	CurrentProject      string          `json:"current_project"`
	ActiveTool          string          `json:"active_tool"`
	DefaultTool         string          `json:"default_tool"`
	DefaultToolProvider string          `json:"default_tool_provider"`
	HideStartupPopup    bool            `json:"hide_startup_popup"`
	HideMaclawLLMPopup  bool            `json:"hide_maclaw_llm_popup"`
	ShowGemini          bool            `json:"show_gemini"`
	ShowCodex           bool            `json:"show_codex"`
	ShowOpenCode        bool            `json:"show_opencode"`
	ShowCodeBuddy       bool            `json:"show_codebuddy"`
	ShowIFlow           bool            `json:"show_iflow"`
	ShowKilo            bool            `json:"show_kilo"`
	ShowCursor          bool            `json:"show_cursor"`
	// Sidebar navigation visibility (nil = visible by default).
	// AI 助手, 设置, 关于 are always visible and not configurable.
	ShowNavMonitor       *bool  `json:"show_nav_monitor,omitempty"`
	ShowNavSkills        *bool  `json:"show_nav_skills,omitempty"`
	ShowNavMCP           *bool  `json:"show_nav_mcp,omitempty"`
	ShowNavGossip        *bool  `json:"show_nav_gossip,omitempty"`
	Language             string `json:"language"`
	PowerOptimization    bool   `json:"power_optimization"`
	ScreenDimTimeoutMin  int    `json:"screen_dim_timeout_min"`
	WorkstationMode      bool   `json:"workstation_mode"`
	CheckUpdateOnStartup bool   `json:"check_update_on_startup"`
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
	UseWindowsTerminal      bool     `json:"use_windows_terminal"`
	RemoteEnabled           bool     `json:"remote_enabled"`
	RemoteHubURL            string   `json:"remote_hub_url"`
	RemoteHubCenterURL      string   `json:"remote_hubcenter_url"`
	RemoteHubCenterURLs     []string `json:"remote_hubcenter_urls,omitempty"`
	RemoteEmail             string   `json:"remote_email"`
	RemoteMobile            string   `json:"remote_mobile"`
	RemoteSN                string   `json:"remote_sn"`
	RemoteUserID            string   `json:"remote_user_id"`
	RemoteMachineID         string   `json:"remote_machine_id"`
	RemoteMachineToken      string   `json:"remote_machine_token"`
	RemoteViewerToken       string   `json:"remote_viewer_token,omitempty"`
	SkillMarketSessionToken string   `json:"skill_market_session_token,omitempty"`
	RemoteHeartbeatSec      int      `json:"remote_heartbeat_sec"`
	RemoteNickname          string   `json:"remote_nickname,omitempty"`
	RemoteClientID          string   `json:"remote_client_id"`
	DefaultLaunchMode       string   `json:"default_launch_mode"`
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
	// MIS data service configuration.
	MISData MISDataConfig `json:"mis_data,omitempty"`
	// Group Discussion configuration (current-Hub scoped collaboration).
	GroupDiscussion GroupDiscussionConfig `json:"group_discussion,omitempty"`
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
	// Security
	SecurityPolicyMode     string                 `json:"security_policy_mode,omitempty"`
	SandboxMode            string                 `json:"sandbox_mode,omitempty"`          // "none" (default), "os", "docker"
	NetworkLevel           string                 `json:"network_level,omitempty"`         // "none", "intranet", "full" (default)
	YoloModeAllowed        bool                   `json:"yolo_mode_allowed"`               // default true
	GossipEnabled          bool                   `json:"gossip_enabled"`                  // default true (local preference, overridden by Hub)
	FileOutboundEnabled    bool                   `json:"file_outbound_enabled"`           // default true
	ImageOutboundEnabled   bool                   `json:"image_outbound_enabled"`          // default true
	SkillSourcesAllowed    []string               `json:"skill_sources_allowed,omitempty"` // nil/empty = all; values: "skillhub","clawhub","github"
	CapabilityMarketPolicy CapabilityMarketPolicy `json:"capability_market_policy,omitempty"`
	MaclawDebugToolCalls   bool                   `json:"maclaw_debug_tool_calls,omitempty"`
	ShowAITraceEntry       bool                   `json:"show_ai_trace_entry,omitempty"`
	ShowAssistantEntry     bool                   `json:"show_assistant_entry"`
	PetEnabled             bool                   `json:"pet_enabled,omitempty"`
	PetSkin                string                 `json:"pet_skin,omitempty"`
	PetSize                int                    `json:"pet_size,omitempty"`
	PetMotionEnabled       *bool                  `json:"pet_motion_enabled,omitempty"`
	PetMotionSound         *bool                  `json:"pet_motion_sound_enabled,omitempty"`
	PetMotionSoundPreset   string                 `json:"pet_motion_sound_preset,omitempty"`
	PetTextInteraction     *bool                  `json:"pet_text_interaction_enabled,omitempty"`
	PetVoiceInput          bool                   `json:"pet_voice_input_enabled,omitempty"`
	PetVoiceReadback       bool                   `json:"pet_voice_readback_enabled,omitempty"`
	PetFileDropEnabled     *bool                  `json:"pet_file_drop_enabled,omitempty"`
	PetInteractionMode     string                 `json:"pet_interaction_mode,omitempty"`
	PetConversationMode    string                 `json:"pet_conversation_mode,omitempty"`
	PetReadbackMode        string                 `json:"pet_readback_mode,omitempty"`
	PetAutoRetryOnNoHear   bool                   `json:"pet_auto_retry_on_no_hear,omitempty"`
	PetContinuousTimeout   int                    `json:"pet_continuous_timeout_sec,omitempty"`
	PetQuietMode           bool                   `json:"pet_quiet_mode,omitempty"`
	FloatingBtnX           int                    `json:"floating_btn_x,omitempty"`
	FloatingBtnY           int                    `json:"floating_btn_y,omitempty"`
	FloatingBtnPositionSet bool                   `json:"floating_btn_position_set,omitempty"`
	LogDetailEnabled       bool                   `json:"log_detail_enabled,omitempty"`
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
	WeixinLocalMode *bool  `json:"weixin_local_mode,omitempty"` // nil or true = local (闂備礁鎲￠〃鍡椕哄Ο琛℃敠闁?, false = remote/Hub (濠电姰鍨奸崺鏍枈瀹ュ應鏀﹂柍?
	// IM 闂?Lansenger (闂備浇妫勯崯顖滄崲閳ь剙菐? client-side gateway
	LansengerEnabled    bool   `json:"lansenger_enabled,omitempty"`
	LansengerAppID      string `json:"lansenger_app_id,omitempty"`
	LansengerAppSecret  string `json:"lansenger_app_secret,omitempty"`
	LansengerGatewayURL string `json:"lansenger_gateway_url,omitempty"` // API gateway base URL, default https://apigw.lx.qianxin.com
	LansengerWSSURL     string `json:"lansenger_wss_url,omitempty"`     // optional WebSocket gateway override
	// IM 闂?local mode toggles for QQ Bot and Telegram (same semantics as WeChat)
	QQBotLocalMode     *bool `json:"qqbot_local_mode,omitempty"`     // nil = auto-detect, true = local, false = hub
	TelegramLocalMode  *bool `json:"telegram_local_mode,omitempty"`  // nil = auto-detect, true = local, false = hub
	LansengerLocalMode *bool `json:"lansenger_local_mode,omitempty"` // nil = auto-detect, true = local, false = hub
	// IM third-party local HTTP gateway.
	ThirdPartyGatewayEnabled   bool   `json:"thirdparty_gateway_enabled,omitempty"`
	ThirdPartyGatewayToken     string `json:"thirdparty_gateway_token,omitempty"`
	ThirdPartyGatewayHost      string `json:"thirdparty_gateway_host,omitempty"`
	ThirdPartyGatewayPort      int    `json:"thirdparty_gateway_port,omitempty"`
	ThirdPartyGatewayLocalMode *bool  `json:"thirdparty_gateway_local_mode,omitempty"`
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
	// Calibrated noise floor for voice input VAD.
	// 0 = not calibrated (use auto-calibration). Positive value = calibrated RMS baseline.
	// Set by the "Calibrate Microphone" button in settings.
	NoiseFloorCalibrated float64 `json:"noise_floor_calibrated,omitempty"`
	// Calibrated speech energy level for voice input VAD.
	// 0 = not calibrated (use multiplier-based threshold). Positive value = average speech RMS.
	// Set by the second phase of microphone calibration (user reads a sentence aloud).
	SpeechLevelCalibrated float64 `json:"speech_level_calibrated,omitempty"`
	// TTS toggle — enables voice readback of AI responses.
	TTSEnabled bool `json:"tts_enabled"`
	// TTSVoiceID stores the selected Kokoro voice id. Empty means default.
	TTSVoiceID string `json:"tts_voice_id,omitempty"`
	// TTS auto voice summary — when enabled, IM channel responses automatically
	// include a voice summary version (语音摘要). Only affects IM channels
	// (飞书/企微/QQ/钉钉), not the desktop panel.
	TTSAutoVoiceSummary bool `json:"tts_auto_voice_summary,omitempty"`
	// Audio device selection — stores the deviceId from navigator.mediaDevices.
	// Empty string = system default device.
	AudioInputDeviceID  string `json:"audio_input_device_id,omitempty"`
	AudioOutputDeviceID string `json:"audio_output_device_id,omitempty"`
	// Screen parsing (YOLO) toggle 鈥?enables vision-based UI element detection.
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
	// AuxiliaryLLM — lightweight LLM for background tasks (compression,
	// skill repair, session search summarization). When configured, used
	// in preference to the main LLM to reduce cost and latency.
	AuxiliaryLLM AuxiliaryLLMConfig `json:"auxiliary_llm,omitempty"`
	// ModelRoutes — per-task-type model overrides. Keys are task types
	// (intent/fast/reasoning/vision/summary/default), values override
	// the primary LLM config for that task type. When a task type has no
	// route, falls back to AuxiliaryLLM (for lightweight tasks) or primary.
	// Example: {"intent": {"model": "glm-4-flash"}, "reasoning": {"model": "deepseek-coder"}}
	ModelRoutes map[string]ModelRouteConfig `json:"model_routes,omitempty"`
	// DailyLLMBudgetUSD — daily LLM API cost budget in USD. When exceeded,
	// the agent warns the user and may throttle non-essential LLM calls.
	// 0 means unlimited (default).
	DailyLLMBudgetUSD float64 `json:"daily_llm_budget_usd,omitempty"`
	// AutoFetch — periodic data fetching from external sources.
	AutoFetchEnabled     bool     `json:"auto_fetch_enabled,omitempty"`
	AutoFetchIntervalMin int      `json:"auto_fetch_interval_min,omitempty"` // default 20
	AutoFetchRSSFeeds    []string `json:"auto_fetch_rss_feeds,omitempty"`
	AutoFetchWatchDirs   []string `json:"auto_fetch_watch_dirs,omitempty"`
	// NudgeDisabled — when true, the post-use skill nudge system is
	// completely disabled. No nudge messages will be injected into the
	// conversation after complex tasks, skill failures, or user corrections.
	NudgeDisabled bool `json:"nudge_disabled,omitempty"`
	// WorkingDirectory is the user-configured default working directory for
	// agent tasks (bash, craft_tool, confirmation panel, etc.). When empty,
	// falls back to ~/.maclaw/workspace via corelib.WorkspaceDir().
	WorkingDirectory string `json:"working_directory,omitempty"`
	// DataDir is the user-configured data directory for all maclaw persistent
	// data (memories, logs, skills, conversations, etc.). When empty, defaults
	// to ~/.maclaw. config.json always stays at ~/.maclaw/config.json regardless
	// of this setting. Changes take effect after restart.
	DataDir string `json:"data_dir,omitempty"`
	// WorkflowEnabled controls whether the workflow engine (multi-phase
	// guided workflows like coding, PPT design, etc.) is active. When false,
	// all messages bypass workflow interception and go directly to the normal
	// agent loop. Default: true (enabled).
	WorkflowEnabled *bool `json:"workflow_enabled,omitempty"`
	// FavoriteEmployees stores the IDs of up to 5 pinned digital employees
	// shown as quick-access buttons in the sidebar nav rail. Order matters.
	FavoriteEmployees []string `json:"favorite_employees,omitempty"`
	// ShowCodingToolEntry controls whether the coding tool selector (Claude Code,
	// Gemini CLI, etc.) is visible in the sidebar. Default: false (hidden).
	ShowCodingToolEntry bool `json:"show_coding_tool_entry,omitempty"`
}

// CapabilityMarketPolicy controls enterprise capability discovery and install behavior.
type CapabilityMarketPolicy struct {
	ViewMode              string                                  `json:"view_mode,omitempty"`
	EnterpriseOnlyInstall *bool                                   `json:"enterprise_only_install,omitempty"`
	EnterpriseOnlySearch  *bool                                   `json:"enterprise_only_search,omitempty"`
	ManagedDeployment     CapabilityManagedDeploymentPolicy       `json:"managed_deployment,omitempty"`
	RecommendedCapability CapabilityRecommendedCapabilityPolicy   `json:"recommended_capability,omitempty"`
	UpdatePolicy          CapabilityUpdatePolicy                  `json:"update_policy,omitempty"`
	SourcePriority        map[string]int                          `json:"source_priority,omitempty"`
	ResourceTypes         map[string]CapabilityResourceTypePolicy `json:"resource_types,omitempty"`
}

type CapabilityManagedDeploymentPolicy struct {
	Enabled              *bool `json:"enabled,omitempty"`
	RetryIntervalMinutes int   `json:"retry_interval_minutes,omitempty"`
	ReinstallIfRemoved   *bool `json:"reinstall_if_removed,omitempty"`
}

type CapabilityRecommendedCapabilityPolicy struct {
	Enabled          *bool `json:"enabled,omitempty"`
	AllowUserDismiss *bool `json:"allow_user_dismiss,omitempty"`
}
type CapabilityUpdatePolicy struct {
	EnterpriseHub CapabilitySourceUpdatePolicy `json:"enterprise_hub,omitempty"`
	HubCenter     CapabilitySourceUpdatePolicy `json:"hubcenter,omitempty"`
}

type CapabilitySourceUpdatePolicy struct {
	Default               string   `json:"default,omitempty"`
	FreeCapability        string   `json:"free_capability,omitempty"`
	PaidCapability        string   `json:"paid_capability,omitempty"`
	LicenseOrPriceChanged string   `json:"license_or_price_changed,omitempty"`
	ApplyTo               []string `json:"apply_to,omitempty"`
	Options               []string `json:"options,omitempty"`
}

type CapabilityResourceTypePolicy struct {
	AllowedSources          []string `json:"allowed_sources,omitempty"`
	DefaultSources          []string `json:"default_sources,omitempty"`
	UserConfigurableSources []string `json:"user_configurable_sources,omitempty"`
}

func DefaultCapabilityMarketPolicy() CapabilityMarketPolicy {
	enterpriseOnlyInstall := true
	enterpriseOnlySearch := false
	managedEnabled := true
	reinstallIfRemoved := true
	recommendedEnabled := true
	allowUserDismiss := true
	return CapabilityMarketPolicy{
		ViewMode:              "merged",
		EnterpriseOnlyInstall: &enterpriseOnlyInstall,
		EnterpriseOnlySearch:  &enterpriseOnlySearch,
		ManagedDeployment: CapabilityManagedDeploymentPolicy{
			Enabled:              &managedEnabled,
			RetryIntervalMinutes: 60,
			ReinstallIfRemoved:   &reinstallIfRemoved,
		},
		RecommendedCapability: CapabilityRecommendedCapabilityPolicy{
			Enabled:          &recommendedEnabled,
			AllowUserDismiss: &allowUserDismiss,
		},
		UpdatePolicy: CapabilityUpdatePolicy{
			EnterpriseHub: CapabilitySourceUpdatePolicy{
				Default: "auto_update_approved",
				ApplyTo: []string{"managed_deployments", "installed_enterprise_capabilities", "recommended_capabilities_installed_by_user"},
			},
			HubCenter: CapabilitySourceUpdatePolicy{
				FreeCapability:        "auto_update",
				PaidCapability:        "require_license_and_purchase_policy",
				LicenseOrPriceChanged: "require_admin_or_purchase_policy",
				Options:               []string{"auto_update_disabled", "notify_admin", "auto_import_pending_review", "auto_update_patch_only", "auto_update_trusted_publisher"},
			},
		},
		SourcePriority: map[string]int{
			"enterprise_hub": 100,
			"hubcenter":      80,
			"clawhub":        40,
			"github":         20,
		},
		ResourceTypes: map[string]CapabilityResourceTypePolicy{
			"skill": {
				AllowedSources:          []string{"enterprise_hub", "hubcenter", "clawhub", "github"},
				DefaultSources:          []string{"enterprise_hub", "hubcenter"},
				UserConfigurableSources: []string{"clawhub", "github"},
			},
			"mcp": {
				AllowedSources: []string{"enterprise_hub", "hubcenter"},
				DefaultSources: []string{"enterprise_hub"},
			},
		},
	}
}

func (p CapabilityMarketPolicy) WithDefaults() CapabilityMarketPolicy {
	defaults := DefaultCapabilityMarketPolicy()
	if strings.TrimSpace(p.ViewMode) == "" {
		p.ViewMode = defaults.ViewMode
	}
	if p.EnterpriseOnlyInstall == nil {
		p.EnterpriseOnlyInstall = defaults.EnterpriseOnlyInstall
	}
	if p.EnterpriseOnlySearch == nil {
		p.EnterpriseOnlySearch = defaults.EnterpriseOnlySearch
	}
	if p.ManagedDeployment.Enabled == nil {
		p.ManagedDeployment.Enabled = defaults.ManagedDeployment.Enabled
	}
	if p.ManagedDeployment.RetryIntervalMinutes <= 0 {
		p.ManagedDeployment.RetryIntervalMinutes = defaults.ManagedDeployment.RetryIntervalMinutes
	}
	if p.ManagedDeployment.ReinstallIfRemoved == nil {
		p.ManagedDeployment.ReinstallIfRemoved = defaults.ManagedDeployment.ReinstallIfRemoved
	}
	if p.RecommendedCapability.Enabled == nil {
		p.RecommendedCapability.Enabled = defaults.RecommendedCapability.Enabled
	}
	if p.RecommendedCapability.AllowUserDismiss == nil {
		p.RecommendedCapability.AllowUserDismiss = defaults.RecommendedCapability.AllowUserDismiss
	}
	if strings.TrimSpace(p.UpdatePolicy.EnterpriseHub.Default) == "" && len(p.UpdatePolicy.EnterpriseHub.ApplyTo) == 0 {
		p.UpdatePolicy.EnterpriseHub = defaults.UpdatePolicy.EnterpriseHub
	}
	if strings.TrimSpace(p.UpdatePolicy.HubCenter.FreeCapability) == "" && strings.TrimSpace(p.UpdatePolicy.HubCenter.PaidCapability) == "" {
		p.UpdatePolicy.HubCenter = defaults.UpdatePolicy.HubCenter
	}
	if len(p.SourcePriority) == 0 {
		p.SourcePriority = defaults.SourcePriority
	}
	if len(p.ResourceTypes) == 0 {
		p.ResourceTypes = defaults.ResourceTypes
	}
	return p
}

func (p CapabilityMarketPolicy) EffectiveEnterpriseOnlyInstall() bool {
	if p.EnterpriseOnlyInstall == nil {
		return true
	}
	return *p.EnterpriseOnlyInstall
}

func (p CapabilityMarketPolicy) EffectiveEnterpriseOnlySearch() bool {
	if p.EnterpriseOnlySearch == nil {
		return false
	}
	return *p.EnterpriseOnlySearch
}

// MISDataConfig stores the MaClawDataSrv connection used by MaClaw UI and agent tools.
type MISDataConfig struct {
	Enabled  bool   `json:"enabled"`
	Endpoint string `json:"endpoint,omitempty"`
	Token    string `json:"token,omitempty"`
	TenantID string `json:"tenant_id,omitempty"`
	UserID   string `json:"user_id,omitempty"`
	Role     string `json:"role,omitempty"`
}

func (c MISDataConfig) WithDefaults() MISDataConfig {
	if strings.TrimSpace(c.Endpoint) == "" {
		c.Endpoint = "http://127.0.0.1:18180"
	}
	if strings.TrimSpace(c.TenantID) == "" {
		c.TenantID = "default"
	}
	if strings.TrimSpace(c.UserID) == "" {
		c.UserID = "maclaw"
	}
	if strings.TrimSpace(c.Role) == "" {
		c.Role = "data_user"
	}
	return c
}

// GroupDiscussionConfig controls current-Hub MaClaw-to-MaClaw consultations.
// The feature is intentionally scoped to the current Hub and does not imply
// HubCenter or public discovery participation.
type GroupDiscussionConfig struct {
	Enabled                          bool     `json:"enabled"`
	Discoverable                     bool     `json:"discoverable"`
	Availability                     string   `json:"availability,omitempty"`
	SuggestConsultation              bool     `json:"suggest_consultation"`
	ConfirmBeforeStart               bool     `json:"confirm_before_start"`
	DisplayName                      string   `json:"display_name,omitempty"`
	SecurityGroupID                  string   `json:"security_group_id,omitempty"`
	Skills                           []string `json:"skills,omitempty"`
	Description                      string   `json:"description,omitempty"`
	ModelVisibility                  string   `json:"model_visibility,omitempty"`
	Languages                        []string `json:"languages,omitempty"`
	InvitePolicy                     string   `json:"invite_policy,omitempty"`
	AllowSecurityGroupFreeDiscussion bool     `json:"allow_security_group_free_discussion"`
	UseCrossAgentExperience          *bool    `json:"use_cross_agent_experience,omitempty"`
	AllowedRoles                     []string `json:"allowed_roles,omitempty"`
	MaxRiskLevel                     string   `json:"max_risk_level,omitempty"`
	ContextPolicy                    string   `json:"context_policy,omitempty"`
	RejectWhenDND                    bool     `json:"reject_when_dnd"`
	MaxRounds                        int      `json:"max_rounds,omitempty"`
	TimeoutSeconds                   int      `json:"timeout_seconds,omitempty"`
	ConcurrentLimit                  int      `json:"concurrent_limit,omitempty"`
	ContributionScore                float64  `json:"contribution_score,omitempty"`
	ContributionEvidence             int      `json:"contribution_evidence,omitempty"`
	SensitiveQueryPolicy             string   `json:"sensitive_query_policy,omitempty"`
}

func (gd GroupDiscussionConfig) CrossAgentExperienceEnabled() bool {
	return gd.UseCrossAgentExperience == nil || *gd.UseCrossAgentExperience
}

func (c AppConfig) MarshalJSON() ([]byte, error) {
	type appConfigAlias AppConfig
	return json.Marshal(appConfigAlias(c))
}

func (c *AppConfig) UnmarshalJSON(data []byte) error {
	type appConfigAlias AppConfig
	type rawAppConfig struct {
		appConfigAlias
		ShowAssistantEntry          *bool                  `json:"show_assistant_entry"`
		GroupDiscussion             *GroupDiscussionConfig `json:"group_discussion,omitempty"`
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
	if raw.GroupDiscussion == nil {
		c.GroupDiscussion = defaultGroupDiscussionConfig()
	} else {
		c.GroupDiscussion = *raw.GroupDiscussion
		c.applyGroupDiscussionFieldDefaults()
	}
	c.CapabilityMarketPolicy = c.CapabilityMarketPolicy.WithDefaults()
	return nil
}

func defaultGroupDiscussionConfig() GroupDiscussionConfig {
	gd := GroupDiscussionConfig{
		Enabled:             true,
		Discoverable:        true,
		SuggestConsultation: true,
		ConfirmBeforeStart:  true,
		RejectWhenDND:       true,
	}
	applyGroupDiscussionFieldDefaults(&gd)
	return gd
}

func (c *AppConfig) applyGroupDiscussionFieldDefaults() {
	applyGroupDiscussionFieldDefaults(&c.GroupDiscussion)
}

func applyGroupDiscussionFieldDefaults(gd *GroupDiscussionConfig) {
	if gd.Availability == "" {
		gd.Availability = "available"
	}
	if gd.InvitePolicy == "" {
		gd.InvitePolicy = "ask_always"
	}
	if gd.ContextPolicy == "" {
		gd.ContextPolicy = "summary_only"
	}
	if gd.MaxRiskLevel == "" {
		gd.MaxRiskLevel = "medium"
	}
	if gd.ModelVisibility == "" {
		gd.ModelVisibility = "class_only"
	}
	if gd.MaxRounds <= 0 {
		gd.MaxRounds = 3
	}
	if gd.TimeoutSeconds <= 0 {
		gd.TimeoutSeconds = 300
	}
	if gd.ConcurrentLimit <= 0 {
		gd.ConcurrentLimit = 1
	}
	if len(gd.AllowedRoles) == 0 {
		gd.AllowedRoles = []string{"observe", "speak", "review"}
	}
	if len(gd.Languages) == 0 {
		gd.Languages = []string{"zh-Hans"}
	}
	switch strings.ToLower(strings.TrimSpace(gd.SensitiveQueryPolicy)) {
	case "deny":
		gd.SensitiveQueryPolicy = "deny"
	case "allow":
		gd.SensitiveQueryPolicy = "allow"
	case "confirm":
		gd.SensitiveQueryPolicy = "confirm"
	default:
		gd.SensitiveQueryPolicy = "confirm"
	}
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

// ModelRouteConfig defines a model override for a specific task type.
// Used in AppConfig.ModelRoutes to route different task types to different models.
// Empty fields inherit from the primary LLM config.
type ModelRouteConfig struct {
	Model    string `json:"model"`              // model name override (required)
	URL      string `json:"url,omitempty"`      // API URL override
	Key      string `json:"key,omitempty"`      // API key override
	Protocol string `json:"protocol,omitempty"` // protocol override
	Provider string `json:"provider,omitempty"` // provider name (display only)
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
//   - If Hub is activated (RemoteMachineID is set), default to Hub/濠电姰鍨奸崺鏍枈瀹ュ應鏀﹂柍?mode (false)
//   - Otherwise, default to local/闂備礁鎲￠〃鍡椕哄Ο琛℃敠闁?mode (true)
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

// LansengerWebSocketGatewayURL returns the optional WebSocket gateway override.
func (c *AppConfig) LansengerWebSocketGatewayURL() string {
	return strings.TrimSpace(c.LansengerWSSURL)
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

// IsThirdPartyGatewayLocalMode returns the effective third-party gateway local mode setting.
func (c *AppConfig) IsThirdPartyGatewayLocalMode() bool {
	if c.ThirdPartyGatewayLocalMode == nil {
		if c.RemoteMachineID != "" {
			return false
		}
		return true
	}
	return *c.ThirdPartyGatewayLocalMode
}

// SetThirdPartyGatewayLocal sets the ThirdPartyGatewayLocalMode pointer field.
func (c *AppConfig) SetThirdPartyGatewayLocal(v bool) {
	c.ThirdPartyGatewayLocalMode = &v
}

// IsWorkflowEnabled returns the effective workflow enabled setting.
// Default is true (enabled) when the field has never been explicitly set (nil).
func (c *AppConfig) IsWorkflowEnabled() bool {
	if c.WorkflowEnabled == nil {
		return true
	}
	return *c.WorkflowEnabled
}

// SetWorkflowEnabled sets the WorkflowEnabled pointer field.
func (c *AppConfig) SetWorkflowEnabled(v bool) {
	c.WorkflowEnabled = &v
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
