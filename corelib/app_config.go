package corelib

// AppConfig is the complete application configuration for MaClaw.
import (
	"encoding/json"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// DefaultMaclawRoleDescription is the identity used when no custom role
// description has been configured.
const DefaultMaclawRoleDescription = "你的全能数智伴侣MaClaw"

type AppConfig struct {
	Claude               ToolConfig      `json:"claude"`
	Codex                ToolConfig      `json:"codex"`
	Opencode             ToolConfig      `json:"opencode"`
	CodeBuddy            ToolConfig      `json:"codebuddy"`
	IFlow                ToolConfig      `json:"iflow"`
	Kilo                 ToolConfig      `json:"kilo"`
	Projects             []ProjectConfig `json:"projects"`
	CurrentProject       string          `json:"current_project"`
	ActiveTool           string          `json:"active_tool"`
	DefaultTool          string          `json:"default_tool"`
	DefaultToolProvider  string          `json:"default_tool_provider"`
	HideStartupPopup     bool            `json:"hide_startup_popup"`
	HideMaclawLLMPopup   bool            `json:"hide_maclaw_llm_popup"`
	ShowCodex            bool            `json:"show_codex"`
	ShowOpenCode         bool            `json:"show_opencode"`
	ShowCodeBuddy        bool            `json:"show_codebuddy"`
	ShowIFlow            bool            `json:"show_iflow"`
	ShowKilo             bool            `json:"show_kilo"`
	Language             string          `json:"language"`
	PowerOptimization    bool            `json:"power_optimization"`
	ScreenDimTimeoutMin  int             `json:"screen_dim_timeout_min"`
	WorkstationMode      bool            `json:"workstation_mode"`
	CheckUpdateOnStartup bool            `json:"check_update_on_startup"`
	PreferBetaChannel    bool            `json:"prefer_beta_channel,omitempty"`
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
	RemoteHubID             string   `json:"remote_hub_id,omitempty"`
	RemoteHubURL            string   `json:"remote_hub_url"`
	RemoteHubCenterURL      string   `json:"remote_hubcenter_url"`
	RemoteHubCenterURLs     []string `json:"remote_hubcenter_urls,omitempty"`
	RemoteEmail             string   `json:"remote_email"`
	RemoteMobile            string   `json:"remote_mobile"`
	RemoteSN                string   `json:"remote_sn"`
	RemoteUserID            string   `json:"remote_user_id"`
	RemoteTenantID          string   `json:"remote_tenant_id,omitempty"`
	RemoteTenantName        string   `json:"remote_tenant_name,omitempty"`
	RemoteMachineID         string   `json:"remote_machine_id"`
	RemoteMachineName       string   `json:"remote_machine_name,omitempty"`
	RemoteMachineToken      string   `json:"remote_machine_token"`
	RemoteViewerToken       string   `json:"remote_viewer_token,omitempty"`
	SkillMarketSessionToken string   `json:"skill_market_session_token,omitempty"`
	RemoteHeartbeatSec      int      `json:"remote_heartbeat_sec"`
	RemoteNickname          string   `json:"remote_nickname,omitempty"`
	RemoteClientID          string   `json:"remote_client_id"`
	DefaultLaunchMode       string   `json:"default_launch_mode"`
	// MaClaw LLM configuration
	MaclawLLMUrl             string                     `json:"maclaw_llm_url"`
	MaclawLLMKey             string                     `json:"maclaw_llm_key"`
	MaclawLLMModel           string                     `json:"maclaw_llm_model"`
	MaclawLLMProtocol        string                     `json:"maclaw_llm_protocol,omitempty"`
	MaclawLLMContextLength   int                        `json:"maclaw_llm_context_length,omitempty"`
	MaclawLLMTimeoutSec      int                        `json:"maclaw_llm_timeout_sec,omitempty"`
	AgentResponseTimeoutSec  int                        `json:"agent_response_timeout_sec,omitempty"`
	SkillRunnerTimeoutSec    int                        `json:"skill_runner_timeout_sec,omitempty"`
	MaclawLLMProviders       []MaclawLLMProvider        `json:"maclaw_llm_providers,omitempty"`
	MaclawLLMCurrentProvider string                     `json:"maclaw_llm_current_provider,omitempty"`
	LLMPromptCache           LLMPromptCacheConfig       `json:"llm_prompt_cache,omitempty"`
	ToolCacheMaintenance     ToolCacheMaintenanceConfig `json:"tool_cache_maintenance,omitempty"`
	WebSearchProviders       []WebSearchProvider        `json:"web_search_providers,omitempty"`
	WebSearchCurrentProvider string                     `json:"web_search_current_provider,omitempty"`
	MaclawAgentMaxIterations int                        `json:"maclaw_agent_max_iterations,omitempty"`
	SubAgentConcurrency      int                        `json:"subagent_concurrency,omitempty"`
	SubAgentFullAccess       bool                       `json:"subagent_full_access,omitempty"`
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
	// SkillEvolutionRepairCooldownHours is the min interval between automated
	// self-repair LLM attempts for the same skill. 0 = default (1 hour).
	SkillEvolutionRepairCooldownHours int `json:"skill_evolution_repair_cooldown_hours,omitempty"`
	// SkillEvolutionEnabled controls whether automatic post-run self-repair /
	// optimize / promote is active. Nil means default true. Env kill switch
	// MACLAW_DISABLE_SKILL_EVOLUTION still overrides when set. Manual
	// manage_skill trigger_repair/trigger_optimize remain available.
	SkillEvolutionEnabled *bool `json:"skill_evolution_enabled,omitempty"`
	// Memory
	MemoryAutoCompress bool `json:"memory_auto_compress"`
	MemoryMaxBackups   int  `json:"memory_max_backups"` // 0 means use default (20)
	// Security
	SecurityPolicyMode     string   `json:"security_policy_mode,omitempty"`
	HubSecurityCentralized bool     `json:"hub_security_centralized,omitempty"`
	SandboxMode            string   `json:"sandbox_mode,omitempty"`      // "none" (default), "os", "docker"
	NetworkLevel           string   `json:"network_level,omitempty"`     // "none", "intranet", "allowlist", "full" (default)
	NetworkAllowlist       []string `json:"network_allowlist,omitempty"` // hostnames/IPs allowed when network_level="allowlist"
	YoloModeAllowed        bool     `json:"yolo_mode_allowed"`           // default true
	// ComputerUseEnabled controls desktop Computer Use tools/playbook injection.
	// Nil means default true. Independent of ScreenParsingEnabled (OmniParser YOLO weights).
	ComputerUseEnabled *bool `json:"computer_use_enabled,omitempty"`
	// ComputerUseLogKeepNewest is how many newest diag/csv files to keep per kind when pruning.
	// Nil/0 means default 10.
	ComputerUseLogKeepNewest *int `json:"computer_use_log_keep_newest,omitempty"`
	// ComputerUseLogMaxAgeDays deletes Computer Use log artifacts older than this many days when pruning.
	// Nil/0 means age is ignored (only keep-newest applies).
	ComputerUseLogMaxAgeDays *int `json:"computer_use_log_max_age_days,omitempty"`
	// ComputerUseLogAutoPrune runs prune on GUI startup using keep/max-age policy.
	// Nil/false means off (default).
	ComputerUseLogAutoPrune            *bool                  `json:"computer_use_log_auto_prune,omitempty"`
	SmartRouteEnabled                  bool                   `json:"smart_route_enabled"`             // default true (Hub smart routing allowed)
	GossipEnabled                      bool                   `json:"gossip_enabled"`                  // default true (local preference, overridden by Hub)
	FileOutboundEnabled                bool                   `json:"file_outbound_enabled"`           // default true
	ImageOutboundEnabled               bool                   `json:"image_outbound_enabled"`          // default true
	SkillSourcesAllowed                []string               `json:"skill_sources_allowed,omitempty"` // nil/empty = all; "__none__" = block all; values: "skillhub","clawhub","github","enterprise_hub","local"
	TrustedSkillPackageKeyFingerprints []string               `json:"trusted_skill_package_key_fingerprints,omitempty"`
	CapabilityMarketPolicy             CapabilityMarketPolicy `json:"capability_market_policy,omitempty"`
	MaclawDebugToolCalls               bool                   `json:"maclaw_debug_tool_calls,omitempty"`
	ShowAITraceEntry                   bool                   `json:"show_ai_trace_entry,omitempty"`
	ShowAppEntry                       bool                   `json:"show_app_entry"`
	ShowWorkflowEntry                  *bool                  `json:"show_workflow_entry,omitempty"`
	ShowUtilitiesEntry                 *bool                  `json:"show_utilities_entry,omitempty"`
	SurveyEnabled                      *bool                  `json:"survey_enabled,omitempty"`
	ShowAssistantEntry                 bool                   `json:"show_assistant_entry"`
	ShowHubRanking                     *bool                  `json:"show_hub_ranking,omitempty"`
	PetEnabled                         bool                   `json:"pet_enabled,omitempty"`
	PetSkin                            string                 `json:"pet_skin,omitempty"`
	PetSize                            int                    `json:"pet_size,omitempty"`
	PetMotionEnabled                   *bool                  `json:"pet_motion_enabled,omitempty"`
	PetMotionSound                     *bool                  `json:"pet_motion_sound_enabled,omitempty"`
	PetMotionSoundPreset               string                 `json:"pet_motion_sound_preset,omitempty"`
	PetTextInteraction                 *bool                  `json:"pet_text_interaction_enabled,omitempty"`
	PetVoiceInput                      bool                   `json:"pet_voice_input_enabled,omitempty"`
	PetVoiceReadback                   bool                   `json:"pet_voice_readback_enabled,omitempty"`
	PetFileDropEnabled                 *bool                  `json:"pet_file_drop_enabled,omitempty"`
	PetInteractionMode                 string                 `json:"pet_interaction_mode,omitempty"`
	PetConversationMode                string                 `json:"pet_conversation_mode,omitempty"`
	PetReadbackMode                    string                 `json:"pet_readback_mode,omitempty"`
	PetAutoRetryOnNoHear               bool                   `json:"pet_auto_retry_on_no_hear,omitempty"`
	PetContinuousTimeout               int                    `json:"pet_continuous_timeout_sec,omitempty"`
	PetQuietMode                       bool                   `json:"pet_quiet_mode,omitempty"`
	FloatingBtnX                       int                    `json:"floating_btn_x,omitempty"`
	FloatingBtnY                       int                    `json:"floating_btn_y,omitempty"`
	FloatingBtnPositionSet             bool                   `json:"floating_btn_position_set,omitempty"`
	LogDetailEnabled                   bool                   `json:"log_detail_enabled,omitempty"`
	// IM 闂?per-user QQ Bot (client-side gateway)
	QQBotEnabled   bool   `json:"qqbot_enabled,omitempty"`
	QQBotAppID     string `json:"qqbot_app_id,omitempty"`
	QQBotAppSecret string `json:"qqbot_app_secret,omitempty"`
	// QQBotOwnerOpenID is the fixed C2C user_openid for owner proactive push
	// (盯人 forward / scheduled self). When set, no prior private chat is required.
	QQBotOwnerOpenID string `json:"qqbot_owner_openid,omitempty"`
	// IM 闂?per-user Telegram Bot (client-side gateway)
	TelegramBotEnabled bool   `json:"telegram_bot_enabled,omitempty"`
	TelegramBotToken   string `json:"telegram_bot_token,omitempty"`
	// TelegramBotOwnerChatID is the fixed private chat id for owner proactive
	// push (盯人 / scheduled self). Stored as decimal string for JS-safe JSON.
	// When set, no prior bot message is required.
	TelegramBotOwnerChatID DecimalInt64String `json:"telegram_owner_chat_id,omitempty"`
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
	// LansengerIgnoredGroupIDs lists group IDs the bot should not respond to.
	// The bot remains in the group on the platform; only local handling is suppressed.
	LansengerIgnoredGroupIDs []string `json:"lansenger_ignored_group_ids,omitempty"`
	// Lansenger group-chat policy (aligned with OpenClaw 蓝信频道文档):
	// open | allowlist | disabled. Empty defaults to open.
	LansengerGroupPolicy string `json:"lansenger_group_policy,omitempty"`
	// LansengerAllowedGroupIDs is used when group policy is allowlist.
	LansengerAllowedGroupIDs []string `json:"lansenger_allowed_group_ids,omitempty"`
	// LansengerRequireMention: when true (default), group messages must @ the bot.
	// Nil means default true. Set false to answer every group message.
	LansengerRequireMention *bool `json:"lansenger_require_mention,omitempty"`
	// LansengerRespondToAtAll: when require-mention is on, also treat @所有人 as a trigger.
	LansengerRespondToAtAll bool `json:"lansenger_respond_to_at_all,omitempty"`
	// LansengerAutoMentionReply: replies @ the original sender via platform reminder API.
	LansengerAutoMentionReply bool `json:"lansenger_auto_mention_reply,omitempty"`
	// LansengerAutoQuoteReply: replies attach refMsgId for a native platform quote.
	LansengerAutoQuoteReply bool `json:"lansenger_auto_quote_reply,omitempty"`
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
	// IMProgressNudgeEnabled controls whether IM channels show intermediate
	// progress/nudge messages. Nil means default true. When false, only the
	// first progress message and the final result are sent to the user.
	IMProgressNudgeEnabled *bool `json:"im_progress_nudge_enabled,omitempty"`
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
	// MemoryRecallLogEnabled enables detailed memory recall logging to a
	// dedicated file (~/.maclaw/logs/memory_recall.log). Records every recall
	// operation's query, scores, and returned entries for debugging/improving
	// the memory system. Default: false (disabled).
	MemoryRecallLogEnabled bool `json:"memory_recall_log_enabled,omitempty"`
	// KnowledgeAutoRecallEnabled controls automatic knowledge-base injection
	// into system prompts (desktop IM, VE, TUI, agentservice). Nil means
	// default true (enabled). Set false to disable auto-recall only; manual
	// knowledge_search / knowledge_context_pack tools remain available.
	KnowledgeAutoRecallEnabled *bool `json:"knowledge_auto_recall_enabled,omitempty"`
	// KnowledgeAutoRecallMinScore overrides the minimum FTS score required for
	// auto-recall injection. Zero means use the shared default (0.3).
	KnowledgeAutoRecallMinScore float64 `json:"knowledge_auto_recall_min_score,omitempty"`
	// Trial-and-Reflect setting.
	TrialReflectEnabled bool `json:"trial_reflect_enabled,omitempty"`
	// LocalNeedleEnabled enables the local Needle micro-router. Default false:
	// MacLaw keeps using the existing LLM/rule paths unless explicitly enabled.
	LocalNeedleEnabled bool `json:"local_needle_enabled,omitempty"`
	// LocalNeedleLogEnabled records local, redacted micro-decision events for
	// later Needle fine-tuning. Default false and local-only.
	LocalNeedleLogEnabled bool `json:"local_needle_log_enabled,omitempty"`
	// LocalNeedleTrainingExportEnabled allows exporting collected local Needle
	// events into supervised training datasets. Default false.
	LocalNeedleTrainingExportEnabled bool `json:"local_needle_training_export_enabled,omitempty"`
	// LocalNeedleModelPath points to a local Needle model artifact. Empty means
	// use the packaged/default location when the feature is enabled.
	LocalNeedleModelPath string `json:"local_needle_model_path,omitempty"`
	// LocalNeedleMinConfidence is the minimum confidence required before a local
	// Needle decision can replace the LLM classifier. Zero uses the runtime default.
	LocalNeedleMinConfidence float64 `json:"local_needle_min_confidence,omitempty"`
	// LLM token usage statistics.
	LLMTokenUsage map[string]*TokenUsageStat `json:"llm_token_usage,omitempty"`
	// Onboarding completion flag. Must NOT use omitempty — a false value must
	// be explicitly serialized so that full-config SaveConfig writes do not
	// accidentally drop the field, causing the wizard to reappear on restart.
	OnboardingDone bool `json:"onboarding_done"`
	// Embedding / vector search toggle.
	VectorSearchEnabled bool `json:"vector_search_enabled"`
	// ASR toggle.
	ASREnabled bool `json:"asr_enabled"`
	// Diarization toggle. When enabled, the CAM++ speaker embedding model is
	// downloaded in the background and made available for meeting transcription.
	DiarizationEnabled bool `json:"diarization_enabled"`
	// ASR voice correction: after local ASR, run a light LLM pass to fix
	// obvious homophone/typo errors and (in continuous mode) drop non-commands.
	// Default true via AppConfigDefaults; set false to send raw ASR text.
	ASRVoiceCorrectionEnabled bool `json:"asr_voice_correction_enabled"`
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
	// UI zoom factor (0.5 ~ 2.0 manual). 0 / omit = Auto (DPI-adaptive on the frontend).
	UIZoomFactor float64 `json:"ui_zoom_factor,omitempty"`
	// Chat font size in pixels (12 ~ 24, 0 = default 14).
	ChatFontSize int `json:"chat_font_size,omitempty"`
	// SSH host presets.
	SSHHosts []SSHHostEntry `json:"ssh_hosts,omitempty"`
	// Knowledge Skill token budget.
	KnowledgeSkillTokenBudget int `json:"knowledge_skill_token_budget"`
	// KnowledgeVisionLLM — optional Vision LLM for high-quality image descriptions
	// in the knowledge base. When configured and verified, used instead of OCR for
	// generating image descriptions during import.
	KnowledgeVisionLLM KnowledgeVisionLLMConfig `json:"knowledge_vision_llm,omitempty"`
	// KnowledgeIncludeImages — when true, directory imports include image files
	// (.png/.jpg/.jpeg/.gif/.webp/.bmp). Default false to avoid importing
	// decorative images that pollute the knowledge base.
	KnowledgeIncludeImages bool `json:"knowledge_include_images,omitempty"`
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
	// CodingRoutePref is the default pure-coding workbench model preference
	// (auto|primary|reasoning|vision). Seeded into sticky memory when a session
	// has not set an explicit preference yet.
	CodingRoutePref string `json:"coding_route_pref,omitempty"`
	// CodingCheckpointSidecarMaxMB is the soft global cap for pure-coding
	// checkpoint sidecar files (disk). 0 = default 256 MiB. Min effective 32.
	CodingCheckpointSidecarMaxMB int `json:"coding_checkpoint_sidecar_max_mb,omitempty"`
	// CodingRoutePrefMirror, when true, writing a session route pref also
	// updates CodingRoutePref so new sessions inherit the last choice.
	// Default false (only explicit settings write the default).
	CodingRoutePrefMirror bool `json:"coding_route_pref_mirror,omitempty"`
	// MoA — Mixture of Agents (multi-model council) presets. See docs/moa-mixture-of-agents-design-zh.md.
	MoA MoAConfig `json:"moa,omitempty"`
	// SharedAgentLoopEnabled routes eligible chat/background turns through
	// corelib/agent.RunLoop. Can also be forced via env MACLAW_SHARED_AGENT_LOOP.
	// Workflow doc phases stay on the legacy IM loop unless the workflow pilot
	// env is set. New installs default this true via AppConfigDefaults.
	// No omitempty: default is true and UnmarshalJSON seeds AppConfigDefaults,
	// so false must be written explicitly to survive reload.
	SharedAgentLoopEnabled bool `json:"shared_agent_loop_enabled"`
	// SharedAgentLoopMigrated is set after the one-time upgrade that enables
	// SharedAgentLoopEnabled for existing installs. Once true, the migrator
	// will not re-enable the flag if the user later turns it off.
	// No omitempty for the same default-true reason.
	SharedAgentLoopMigrated bool `json:"shared_agent_loop_migrated"`
	// SharedAgentLoopCanaryPercent is optional sticky canary 0..100 for the
	// shared agent loop. nil = use env MACLAW_SHARED_AGENT_LOOP_PERCENT or 100.
	// Env always wins when set. 0 explicitly means 0% (never divert by canary).
	SharedAgentLoopCanaryPercent *int `json:"shared_agent_loop_canary_percent,omitempty"`
	// SharedAgentLoopWorkflow enables non-doc WorkflowAgentLoop on the shared
	// path (doc phases stay legacy). Env MACLAW_SHARED_AGENT_LOOP_WORKFLOW
	// overrides when set (on/off).
	SharedAgentLoopWorkflow bool `json:"shared_agent_loop_workflow,omitempty"`
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
	// agent loop. Default: false (disabled).
	WorkflowEnabled *bool `json:"workflow_enabled,omitempty"`

	// Coding knowledge base settings (experience accumulation by CodingSubAgent)
	CodingKnowledgeAutoSaveMode  string `json:"coding_knowledge_auto_save_mode,omitempty"`  // observe/auto/off
	CodingKnowledgeSaveStrategy  string `json:"coding_knowledge_save_strategy,omitempty"`   // always/on_success/on_retry_success/off
	CodingKnowledgeMaxPerProject int    `json:"coding_knowledge_max_per_project,omitempty"` // single project limit, default 200
	CodingKnowledgeMaxTotal      int    `json:"coding_knowledge_max_total,omitempty"`       // global limit, default 1000

	// FavoriteEmployees stores the IDs of up to 9 user-configured pinned digital
	// employees shown as quick-access buttons in the sidebar nav rail. Order matters.
	// One additional slot is always reserved for the resident employee (total 10 slots).
	FavoriteEmployees []string `json:"favorite_employees,omitempty"`
	// FavoriteEmployeeNames stores user-defined display names for pinned digital employees.
	FavoriteEmployeeNames map[string]string `json:"favorite_employee_names,omitempty"`
	// ShowCodingToolEntry controls whether the coding tool selector (Claude Code,
	// Optional coding tools can be hidden in the sidebar. Default: false (hidden).
	ShowCodingToolEntry bool `json:"show_coding_tool_entry,omitempty"`
	// VEAllowedDirectories is the list of local filesystem directories
	// that the VE is authorized to access for file operations (list, read, send).
	// Machine-specific, not synced to Hub.
	VEAllowedDirectories []string `json:"ve_allowed_directories,omitempty"`
	// VEApprovalConfigJSON stores the VE approval capability configuration as raw JSON.
	// Parsed by the gui package into VEApprovalConfig struct.
	VEApprovalConfigJSON string `json:"ve_approval_config,omitempty"`
}

const (
	DefaultSubAgentConcurrency = 2
	MinSubAgentConcurrency     = 1
	MaxSubAgentConcurrency     = 10
)

type LLMPromptCacheConfig struct {
	Enabled                      bool   `json:"enabled"`
	OpenAIEnabled                *bool  `json:"openai_enabled,omitempty"`
	AnthropicEnabled             *bool  `json:"anthropic_enabled,omitempty"`
	StreamSynthesisEnabled       *bool  `json:"stream_synthesis_enabled,omitempty"`
	CacheDir                     string `json:"cache_dir,omitempty"`
	TTLSeconds                   int    `json:"ttl_seconds,omitempty"`
	MemoryMaxEntries             int    `json:"memory_max_entries,omitempty"`
	MemoryMaxBytes               int64  `json:"memory_max_bytes,omitempty"`
	DiskMaxBytes                 int64  `json:"disk_max_bytes,omitempty"`
	NormalizeDeterministicParams bool   `json:"normalize_deterministic_params,omitempty"`
	IgnoreModelField             bool   `json:"ignore_model_field,omitempty"`
	IgnoreUserField              bool   `json:"ignore_user_field,omitempty"`
	IgnoreMetadataField          bool   `json:"ignore_metadata_field,omitempty"`
	SingleflightWaitTimeoutMS    int    `json:"singleflight_wait_timeout_ms,omitempty"`
}

type ToolCacheMaintenanceConfig struct {
	Enabled          bool   `json:"enabled"`
	MaxBytes         int64  `json:"max_bytes,omitempty"`
	MinIntervalHours int    `json:"min_interval_hours,omitempty"`
	CleanOnStartup   bool   `json:"clean_on_startup"`
	CleanOnExit      bool   `json:"clean_on_exit"`
	LastCleanupAt    string `json:"last_cleanup_at,omitempty"`
}

func DefaultToolCacheMaintenanceConfig() ToolCacheMaintenanceConfig {
	return ToolCacheMaintenanceConfig{
		Enabled:          true,
		MaxBytes:         512 * 1024 * 1024,
		MinIntervalHours: 24,
		CleanOnStartup:   true,
		CleanOnExit:      true,
	}
}

func (c ToolCacheMaintenanceConfig) WithDefaults() ToolCacheMaintenanceConfig {
	defaults := DefaultToolCacheMaintenanceConfig()
	if c.MaxBytes <= 0 {
		c.MaxBytes = defaults.MaxBytes
	}
	if c.MinIntervalHours <= 0 {
		c.MinIntervalHours = defaults.MinIntervalHours
	}
	return c
}

func DefaultLLMPromptCacheConfig() LLMPromptCacheConfig {
	return LLMPromptCacheConfig{
		Enabled:                      false,
		OpenAIEnabled:                boolPtrValue(true),
		AnthropicEnabled:             boolPtrValue(true),
		StreamSynthesisEnabled:       boolPtrValue(true),
		CacheDir:                     DefaultLLMPromptCacheDir(),
		TTLSeconds:                   1800,
		MemoryMaxEntries:             256,
		MemoryMaxBytes:               8 * 1024 * 1024,
		DiskMaxBytes:                 64 * 1024 * 1024,
		NormalizeDeterministicParams: true,
		IgnoreModelField:             true,
		IgnoreUserField:              true,
		IgnoreMetadataField:          true,
		SingleflightWaitTimeoutMS:    15000,
	}
}

func DefaultLLMPromptCacheDir() string {
	return filepath.Join(MaclawDefaultBaseDir(), "llm_prompt_cache")
}

func boolPtrValue(v bool) *bool {
	return &v
}

func (c LLMPromptCacheConfig) WithDefaults() LLMPromptCacheConfig {
	defaults := DefaultLLMPromptCacheConfig()
	if c.OpenAIEnabled == nil {
		c.OpenAIEnabled = defaults.OpenAIEnabled
	}
	if c.AnthropicEnabled == nil {
		c.AnthropicEnabled = defaults.AnthropicEnabled
	}
	if c.StreamSynthesisEnabled == nil {
		c.StreamSynthesisEnabled = defaults.StreamSynthesisEnabled
	}
	if !c.EffectiveOpenAIEnabled() && !c.EffectiveAnthropicEnabled() && !c.EffectiveStreamSynthesisEnabled() {
		c.Enabled = false
	}
	if strings.TrimSpace(c.CacheDir) == "" {
		c.CacheDir = defaults.CacheDir
	} else {
		c.CacheDir = ExpandLLMPromptCacheDir(c.CacheDir)
	}
	if c.TTLSeconds <= 0 {
		c.TTLSeconds = defaults.TTLSeconds
	}
	if c.MemoryMaxEntries <= 0 {
		c.MemoryMaxEntries = defaults.MemoryMaxEntries
	}
	if c.MemoryMaxBytes <= 0 {
		c.MemoryMaxBytes = defaults.MemoryMaxBytes
	}
	if c.DiskMaxBytes <= 0 {
		c.DiskMaxBytes = defaults.DiskMaxBytes
	}
	if c.SingleflightWaitTimeoutMS <= 0 {
		c.SingleflightWaitTimeoutMS = defaults.SingleflightWaitTimeoutMS
	}
	return c
}

func ExpandLLMPromptCacheDir(dir string) string {
	dir = strings.TrimSpace(os.ExpandEnv(dir))
	if dir == "" {
		return DefaultLLMPromptCacheDir()
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") || strings.HasPrefix(dir, `~\`) {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			if dir == "~" {
				dir = home
			} else {
				dir = filepath.Join(home, strings.TrimLeft(dir[1:], `/\`))
			}
		}
	}
	return filepath.Clean(dir)
}

func (c LLMPromptCacheConfig) EffectiveCacheDir() string {
	return ExpandLLMPromptCacheDir(c.CacheDir)
}

func (c LLMPromptCacheConfig) EffectiveOpenAIEnabled() bool {
	return c.OpenAIEnabled == nil || *c.OpenAIEnabled
}

func (c LLMPromptCacheConfig) EffectiveAnthropicEnabled() bool {
	return c.AnthropicEnabled == nil || *c.AnthropicEnabled
}

func (c LLMPromptCacheConfig) EffectiveStreamSynthesisEnabled() bool {
	return c.StreamSynthesisEnabled == nil || *c.StreamSynthesisEnabled
}

func (c LLMPromptCacheConfig) Options() LLMPromptCacheOptions {
	c = c.WithDefaults()
	return LLMPromptCacheOptions{
		Enabled:                      c.Enabled,
		NormalizeDeterministicParams: c.NormalizeDeterministicParams,
		IgnoreModelField:             c.IgnoreModelField,
		IgnoreUserField:              c.IgnoreUserField,
		IgnoreMetadataField:          c.IgnoreMetadataField,
	}
}

func NormalizeSubAgentConcurrency(n int) int {
	if n <= 0 {
		return DefaultSubAgentConcurrency
	}
	if n < MinSubAgentConcurrency {
		return MinSubAgentConcurrency
	}
	if n > MaxSubAgentConcurrency {
		return MaxSubAgentConcurrency
	}
	return n
}

func (c AppConfig) IsIMProgressNudgeEnabled() bool {
	return c.IMProgressNudgeEnabled == nil || *c.IMProgressNudgeEnabled
}

// IsKnowledgeAutoRecallEnabled reports whether knowledge auto-recall is on.
// Default true when the field has never been set (nil).
func (c AppConfig) IsKnowledgeAutoRecallEnabled() bool {
	return c.KnowledgeAutoRecallEnabled == nil || *c.KnowledgeAutoRecallEnabled
}

// DefaultKnowledgeAutoRecallMinScore matches corelib/agent.KnowledgeAutoRecallScoreThreshold.
const DefaultKnowledgeAutoRecallMinScore = 0.3

// EffectiveKnowledgeAutoRecallMinScore returns the configured min score or default.
func (c AppConfig) EffectiveKnowledgeAutoRecallMinScore() float64 {
	if c.KnowledgeAutoRecallMinScore > 0 {
		return c.KnowledgeAutoRecallMinScore
	}
	return DefaultKnowledgeAutoRecallMinScore
}

// IsSkillEvolutionEnabled returns whether automatic skill evolution after runs
// is allowed. Default true when the field has never been set (nil).
// Does not consult the env kill switch — callers should also check
// skill.EvolutionEnvDisabled() when applicable.
func (c AppConfig) IsSkillEvolutionEnabled() bool {
	return c.SkillEvolutionEnabled == nil || *c.SkillEvolutionEnabled
}

// SetSkillEvolutionEnabled sets the SkillEvolutionEnabled pointer field.
func (c *AppConfig) SetSkillEvolutionEnabled(v bool) {
	if c == nil {
		return
	}
	c.SkillEvolutionEnabled = &v
}

// CapabilityMarketPolicy controls enterprise capability discovery and install behavior.
type CapabilityMarketPolicy struct {
	ViewMode              string                                  `json:"view_mode,omitempty"`
	PreferredUploadTarget string                                  `json:"preferred_upload_target,omitempty"`
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
	enterpriseOnlyInstall := false
	enterpriseOnlySearch := false
	managedEnabled := true
	reinstallIfRemoved := true
	recommendedEnabled := true
	allowUserDismiss := true
	return CapabilityMarketPolicy{
		ViewMode:              "merged",
		PreferredUploadTarget: CapabilitySourceHubCenter,
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
	p.PreferredUploadTarget = NormalizeCapabilityUploadTarget(firstNonEmptyCapabilityPolicyString(p.PreferredUploadTarget, defaults.PreferredUploadTarget))
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

func NormalizeCapabilityUploadTarget(target string) string {
	switch strings.TrimSpace(strings.ToLower(target)) {
	case "hub", "enterprise", "enterprisehub", "enterprise_hub":
		return CapabilitySourceEnterpriseHub
	case "hubcenter", "hub_center", "skillhub", "skill_hub", "market":
		return CapabilitySourceHubCenter
	default:
		return CapabilitySourceHubCenter
	}
}

func (p CapabilityMarketPolicy) EffectivePreferredUploadTarget() string {
	return NormalizeCapabilityUploadTarget(firstNonEmptyCapabilityPolicyString(p.PreferredUploadTarget, CapabilitySourceHubCenter))
}

func (p CapabilityMarketPolicy) UploadTargets(hasEnterpriseHub bool) []string {
	preferred := p.WithDefaults().EffectivePreferredUploadTarget()
	if preferred == CapabilitySourceEnterpriseHub {
		if hasEnterpriseHub {
			return []string{CapabilitySourceEnterpriseHub}
		}
		return nil
	}
	if hasEnterpriseHub {
		return []string{CapabilitySourceHubCenter, CapabilitySourceEnterpriseHub}
	}
	return []string{CapabilitySourceHubCenter}
}

func firstNonEmptyCapabilityPolicyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (p CapabilityMarketPolicy) EffectiveEnterpriseOnlyInstall() bool {
	if p.EnterpriseOnlyInstall == nil {
		return false
	}
	return *p.EnterpriseOnlyInstall
}

// ErrEnterpriseOnlyInstall is the canonical error message returned when
// enterprise-only-install policy blocks a non-enterprise source.
// Frontend localizeHubError matches this string for i18n display.
const ErrEnterpriseOnlyInstall = "enterprise policy only allows installing skills from enterprise Hub"

// RejectNonEnterpriseInstall checks if the enterprise-only-install policy
// should block installation from the given source. Returns ("", false) if
// installation is allowed, or (reason, true) if blocked.
// hubURL is the configured RemoteHubURL — empty means no enterprise Hub configured.
func (p CapabilityMarketPolicy) RejectNonEnterpriseInstall(source, hubURL string) (string, bool) {
	if strings.EqualFold(strings.TrimSpace(source), "enterprise_hub") {
		return "", false
	}
	if strings.TrimSpace(hubURL) == "" {
		return "", false
	}
	if !p.WithDefaults().EffectiveEnterpriseOnlyInstall() {
		return "", false
	}
	return ErrEnterpriseOnlyInstall, true
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
	AuthRequestSoundPreset           string   `json:"auth_request_sound_preset,omitempty"`
	AuthRequestSoundMuted            bool     `json:"auth_request_sound_muted,omitempty"`
}

func (gd GroupDiscussionConfig) CrossAgentExperienceEnabled() bool {
	return gd.UseCrossAgentExperience == nil || *gd.UseCrossAgentExperience
}

func (c AppConfig) MarshalJSON() ([]byte, error) {
	type appConfigAlias AppConfig
	return json.Marshal(appConfigAlias(c))
}

func (c *AppConfig) UnmarshalJSON(data []byte) error {
	// Set defaults first, then let json.Unmarshal overwrite with actual values.
	// Fields present in JSON override defaults; absent fields keep defaults.
	// No *bool tricks, no rawConfig map checks, no dual-layer logic.
	*c = AppConfigDefaults()

	type alias AppConfig
	if err := json.Unmarshal(data, (*alias)(c)); err != nil {
		return err
	}

	// Post-unmarshal normalization (not default-value logic, just clamping/validation).
	c.SubAgentConcurrency = NormalizeSubAgentConcurrency(c.SubAgentConcurrency)
	c.ensureDefaultProject()
	c.applyGroupDiscussionFieldDefaults()
	c.LLMPromptCache = c.LLMPromptCache.WithDefaults()
	c.ToolCacheMaintenance = c.ToolCacheMaintenance.WithDefaults()
	c.CapabilityMarketPolicy = c.CapabilityMarketPolicy.WithDefaults()
	return nil
}

// AppConfigDefaults returns an AppConfig with all default-true booleans and
// struct defaults pre-filled. This is the single source of truth for field
// defaults — no other code path needs to repeat these values.
func AppConfigDefaults() AppConfig {
	return AppConfig{
		MaclawRoleDescription:      DefaultMaclawRoleDescription,
		ShowAssistantEntry:         true,
		ShowCodex:                  true,
		ShowOpenCode:               true,
		ShowCodeBuddy:              true,
		ShowIFlow:                  true,
		ShowKilo:                   true,
		PowerOptimization:          true,
		YoloModeAllowed:            true,
		SmartRouteEnabled:          true,
		GossipEnabled:              true,
		GossipAutoPublish:          true,
		FileOutboundEnabled:        true,
		ImageOutboundEnabled:       true,
		CheckUpdateOnStartup:       true,
		UseWindowsTerminal:         true,
		VectorSearchEnabled:        true,
		ASREnabled:                 true,
		DiarizationEnabled:         true,
		ASRVoiceCorrectionEnabled:  true,
		TTSEnabled:                 true,
		ScreenParsingEnabled:       boolPtrValue(true),
		ComputerUseEnabled:         boolPtrValue(true),
		IMProgressNudgeEnabled:     boolPtrValue(true),
		KnowledgeAutoRecallEnabled: boolPtrValue(true),
		WorkflowEnabled:            boolPtrValue(false),
		// Shared agent loop: new installs divert eligible chat/background turns
		// to corelib/agent.RunLoop. Existing installs are migrated once via
		// ApplySharedAgentLoopMigration (sets SharedAgentLoopMigrated).
		SharedAgentLoopEnabled:  true,
		SharedAgentLoopMigrated: true, // new installs need no later migration
		GroupDiscussion:         defaultGroupDiscussionConfig(),
		LLMPromptCache:          DefaultLLMPromptCacheConfig(),
		ToolCacheMaintenance:    DefaultToolCacheMaintenanceConfig(),
		Projects:                defaultProjects(),
		CurrentProject:          "default",
	}
}

func (c *AppConfig) ensureDefaultProject() {
	if c.Projects != nil {
		return
	}
	c.Projects = defaultProjects()
	if strings.TrimSpace(c.CurrentProject) == "" {
		c.CurrentProject = "default"
	}
}

func defaultProjects() []ProjectConfig {
	home, _ := os.UserHomeDir()
	return []ProjectConfig{
		{Id: "default", Name: "Project 1", Path: home, YoloMode: false},
	}
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
	switch strings.ToLower(strings.TrimSpace(gd.AuthRequestSoundPreset)) {
	case "classic", "soft", "bright", "pulse", "urgent":
		gd.AuthRequestSoundPreset = strings.ToLower(strings.TrimSpace(gd.AuthRequestSoundPreset))
	default:
		gd.AuthRequestSoundPreset = "classic"
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

// KnowledgeVisionLLMConfig is the configuration for the optional Vision LLM
// used by the knowledge base to generate high-quality image descriptions.
// Uses OpenAI-compatible /v1/chat/completions endpoint with image_url content blocks.
//
// Resolution priority:
//  1. Main LLM supports vision (SupportsVision=true) → auto-derive config, no separate setup needed
//  2. This config explicitly set + verified → use it
//  3. Neither available → fall back to OCR + context inference
type KnowledgeVisionLLMConfig struct {
	Enabled     bool   `json:"enabled"`
	BaseURL     string `json:"base_url,omitempty"`
	APIKey      string `json:"api_key,omitempty"`
	Model       string `json:"model,omitempty"`
	MaxTokens   int    `json:"max_tokens,omitempty"`
	TimeoutSec  int    `json:"timeout_sec,omitempty"`
	Verified    bool   `json:"verified,omitempty"`
	FromMainLLM bool   `json:"from_main_llm,omitempty"` // true when auto-derived from main LLM
}

// NewKnowledgeVisionLLMConfigFromMainLLM creates a config derived from the main LLM
// when it supports vision. Auto-verified because the main LLM has already been tested.
func NewKnowledgeVisionLLMConfigFromMainLLM(url, key, model string) KnowledgeVisionLLMConfig {
	return KnowledgeVisionLLMConfig{
		Enabled:     true,
		BaseURL:     url,
		APIKey:      key,
		Model:       model,
		MaxTokens:   500,
		TimeoutSec:  30,
		Verified:    true,
		FromMainLLM: true,
	}
}

// IsConfigured returns true if the Vision LLM has enough configuration to attempt a call.
func (c KnowledgeVisionLLMConfig) IsConfigured() bool {
	return c.Enabled && c.BaseURL != "" && c.APIKey != "" && c.Model != ""
}

// SSHHostEntry describes a preconfigured SSH remote host.
type SSHHostEntry struct {
	Label      string `json:"label"`                 // Human-readable label, e.g. "prod-web-01"
	Host       string `json:"host"`                  // Hostname or IP address
	Port       int    `json:"port,omitempty"`        // Default 22
	User       string `json:"user"`                  // Login username
	AuthMethod string `json:"auth_method,omitempty"` // key/password/agent
	KeyPath    string `json:"key_path,omitempty"`    // Private key path
	// Password is optional in-memory secret for auth_method=password.
	// Prefer not persisting this to disk config; Hub mobile agent injects from vault.
	Password string `json:"password,omitempty"`
	// Passphrase unlocks KeyPath when the private key is encrypted.
	Passphrase string `json:"passphrase,omitempty"`
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

// ApplySharedAgentLoopMigration enables the shared agent loop once for
// existing installs that predate SharedAgentLoopEnabled.
//
// Behavior:
//   - If SharedAgentLoopMigrated is already true → no-op.
//   - Otherwise sets SharedAgentLoopEnabled=true and SharedAgentLoopMigrated=true.
//
// After migration, users who set SharedAgentLoopEnabled=false keep that choice
// because SharedAgentLoopMigrated remains true. Returns true when cfg changed.
func ApplySharedAgentLoopMigration(cfg *AppConfig) bool {
	if cfg == nil || cfg.SharedAgentLoopMigrated {
		return false
	}
	cfg.SharedAgentLoopEnabled = true
	cfg.SharedAgentLoopMigrated = true
	return true
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
// Default is always true (local/单机) regardless of Hub registration status.
// Unlike other IM gateways, Lansenger defaults to local mode because Hub's
// multi-machine routing requires device selection which has poor UX in
// Lansenger (no interactive cards, text-only prompts).
func (c *AppConfig) IsLansengerLocalMode() bool {
	if c.LansengerLocalMode == nil {
		return true
	}
	return *c.LansengerLocalMode
}

// SetLansengerLocal sets the LansengerLocalMode pointer field.
func (c *AppConfig) SetLansengerLocal(v bool) {
	c.LansengerLocalMode = &v
}

// IsLansengerGroupIgnored reports whether groupID is on the ignore list.
// Matching is case-sensitive after trim (Lansenger group IDs are opaque tokens).
func (c *AppConfig) IsLansengerGroupIgnored(groupID string) bool {
	if c == nil {
		return false
	}
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return false
	}
	for _, id := range c.LansengerIgnoredGroupIDs {
		if strings.TrimSpace(id) == groupID {
			return true
		}
	}
	return false
}

// IsLansengerRequireMention returns whether group messages must @ the bot.
// Default true when unset (OpenClaw / 蓝信文档 default).
func (c *AppConfig) IsLansengerRequireMention() bool {
	if c == nil || c.LansengerRequireMention == nil {
		return true
	}
	return *c.LansengerRequireMention
}

// EffectiveLansengerGroupPolicy returns open | allowlist | disabled.
func (c *AppConfig) EffectiveLansengerGroupPolicy() string {
	if c == nil {
		return "open"
	}
	switch strings.ToLower(strings.TrimSpace(c.LansengerGroupPolicy)) {
	case "allowlist", "allow", "whitelist":
		return "allowlist"
	case "disabled", "off", "none":
		return "disabled"
	default:
		return "open"
	}
}

// MaxLansengerIgnoredGroups caps how many groups can be locally silenced / allowlisted.
const MaxLansengerIgnoredGroups = 500

// MaxLansengerAllowedGroups caps the allowlist size (same order of magnitude as ignore).
const MaxLansengerAllowedGroups = MaxLansengerIgnoredGroups

// SetLansengerGroupIgnored adds or removes groupID from the ignore list.
// Returns true when the list changed.
func (c *AppConfig) SetLansengerGroupIgnored(groupID string, ignored bool) bool {
	if c == nil {
		return false
	}
	return setLansengerIDList(&c.LansengerIgnoredGroupIDs, groupID, ignored, MaxLansengerIgnoredGroups)
}

// IsLansengerGroupAllowed reports whether groupID is on the allowlist
// (used when group policy is allowlist).
func (c *AppConfig) IsLansengerGroupAllowed(groupID string) bool {
	if c == nil {
		return false
	}
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return false
	}
	for _, id := range c.LansengerAllowedGroupIDs {
		if strings.TrimSpace(id) == groupID {
			return true
		}
	}
	return false
}

// SetLansengerGroupAllowed adds or removes groupID from the allowlist.
// Returns true when the list changed.
func (c *AppConfig) SetLansengerGroupAllowed(groupID string, allowed bool) bool {
	if c == nil {
		return false
	}
	return setLansengerIDList(&c.LansengerAllowedGroupIDs, groupID, allowed, MaxLansengerAllowedGroups)
}

func setLansengerIDList(list *[]string, groupID string, present bool, maxN int) bool {
	if list == nil {
		return false
	}
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return false
	}
	if present {
		for _, id := range *list {
			if strings.TrimSpace(id) == groupID {
				return false
			}
		}
		cur := *list
		if maxN > 0 && len(cur) >= maxN {
			cur = append([]string(nil), cur[1:]...)
		}
		*list = append(append([]string(nil), cur...), groupID)
		return true
	}
	out := make([]string, 0, len(*list))
	changed := false
	for _, id := range *list {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if id == groupID {
			changed = true
			continue
		}
		out = append(out, id)
	}
	if !changed {
		return false
	}
	if len(out) == 0 {
		*list = nil
	} else {
		*list = out
	}
	return true
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
// Default is false (disabled) when the field has never been explicitly set (nil).
func (c *AppConfig) IsWorkflowEnabled() bool {
	if c.WorkflowEnabled == nil {
		return false
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
	add := func(value string, allowLoopback bool) {
		value = strings.TrimRight(strings.TrimSpace(value), "/")
		if value == "" {
			return
		}
		if !allowLoopback && isConfiguredHubCenterLoopbackURL(value) {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	add(c.RemoteHubCenterURL, false)
	for _, value := range c.RemoteHubCenterURLs {
		add(value, false)
	}
	add(defaultHubCenterURL, true)
	for _, value := range defaultHubCenterURLs {
		add(value, true)
	}
	return out
}

func (c *AppConfig) ConfiguredHubCenterBaseURL() string {
	for _, value := range append([]string{c.RemoteHubCenterURL}, c.RemoteHubCenterURLs...) {
		value = strings.TrimRight(strings.TrimSpace(value), "/")
		if value != "" && !isConfiguredHubCenterLoopbackURL(value) {
			return value
		}
	}
	return ""
}

func (c *AppConfig) SkillHubBaseURL(defaultHubCenterURL string) string {
	return c.ConfiguredHubCenterBaseURL()
}

// SkillMarketBaseURL returns the base URL for SkillMarket APIs (/api/v1/skillmarket/*).
// SkillMarket is hosted on the same HubCenter server as SkillHub.
func (c *AppConfig) SkillMarketBaseURL(defaultHubCenterURL string) string {
	return c.SkillHubBaseURL(defaultHubCenterURL)
}

func isConfiguredHubCenterLoopbackURL(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if ip := net.ParseIP(strings.Trim(value, "[]")); ip != nil {
		return ip.IsLoopback() || ip.IsUnspecified()
	}
	parseValue := value
	if !strings.Contains(parseValue, "://") {
		parseValue = "https://" + parseValue
	}
	host := ""
	if parsed, err := url.Parse(parseValue); err == nil {
		host = strings.TrimSpace(parsed.Hostname())
	}
	if host == "" {
		host = strings.TrimSpace(value)
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.Trim(host, "[]")
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsUnspecified()
	}
	return false
}
