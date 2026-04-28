package views

// config_fields.go is the SINGLE SOURCE OF TRUTH for all TUI-editable config fields.
//
// Each ConfigFieldDef declares:
//   - Key:      string key used in UI and ConfigSaveMsg
//   - Tab:      which config tab this field belongs to
//   - Section:  visual grouping within a tab
//   - DescKey:  i18n message key for the description
//   - Options:  nil = free text, non-nil = inline selector
//   - ReadOnly: display only
//   - Default:  default value shown before LoadFromAppConfig
//   - Get:      reads the value from AppConfig -> string
//   - Set:      writes a string value into AppConfig
//
// Adding a new config field = adding ONE entry to this slice.
// No other file needs to change.

import (
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
)

// ConfigFieldDef is the single definition for a TUI-editable config field.
type ConfigFieldDef struct {
	Key      string
	Tab      int
	Section  string
	DescKey  string   // i18n message key
	Options  []string // nil = free text; non-nil = inline selector
	ReadOnly bool
	Default  string
	Get      func(cfg *corelib.AppConfig) string
	Set      func(cfg *corelib.AppConfig, val string)
}

// boolGet / boolSet are helpers for boolean fields.
func boolGet(ptr func(cfg *corelib.AppConfig) bool) func(cfg *corelib.AppConfig) string {
	return func(cfg *corelib.AppConfig) string {
		if ptr(cfg) {
			return "true"
		}
		return "false"
	}
}

func boolSet(ptr func(cfg *corelib.AppConfig, v bool)) func(cfg *corelib.AppConfig, val string) {
	return func(cfg *corelib.AppConfig, val string) {
		ptr(cfg, val == "true")
	}
}

// intGet / intSet are helpers for integer fields (empty string for zero).
func intGet(ptr func(cfg *corelib.AppConfig) int) func(cfg *corelib.AppConfig) string {
	return func(cfg *corelib.AppConfig) string {
		v := ptr(cfg)
		if v == 0 {
			return ""
		}
		return fmt.Sprintf("%d", v)
	}
}

func intSet(ptr func(cfg *corelib.AppConfig, v int)) func(cfg *corelib.AppConfig, val string) {
	return func(cfg *corelib.AppConfig, val string) {
		var v int
		fmt.Sscanf(val, "%d", &v)
		ptr(cfg, v)
	}
}

// boolOpts is the standard boolean option list.
var boolOpts = []string{"true", "false"}

// allConfigFields is the single source of truth.
// Order within each tab determines display order.
//
// String fields use raw closures for Get/Set (no type conversion needed).
// Boolean fields use boolGet/boolSet (string<->bool conversion).
// Integer fields use intGet/intSet (string<->int conversion).
var allConfigFields = []ConfigFieldDef{
	// ---- Tab 0: General ----
	{
		Key: "hub_url", Tab: CfgTabGeneral, Section: "general",
		DescKey: i18n.MsgTUIConfigDescHubURL,
		Get: func(c *corelib.AppConfig) string { return c.RemoteHubURL },
		Set: func(c *corelib.AppConfig, v string) { c.RemoteHubURL = v },
	},
	{
		Key: "token", Tab: CfgTabGeneral, Section: "general",
		DescKey: i18n.MsgTUIConfigDescToken,
		Get: func(c *corelib.AppConfig) string { return c.RemoteMachineToken },
		Set: func(c *corelib.AppConfig, v string) { c.RemoteMachineToken = v },
	},
	{
		Key: "data_dir", Tab: CfgTabGeneral, Section: "general",
		DescKey: i18n.MsgTUIConfigDescDataDir, ReadOnly: true,
		Get: func(_ *corelib.AppConfig) string { return "" },
		Set: func(_ *corelib.AppConfig, _ string) {},
	},
	{
		Key: "working_directory", Tab: CfgTabGeneral, Section: "general",
		DescKey: i18n.MsgTUIConfigDescWorkDir,
		Get: func(c *corelib.AppConfig) string { return c.WorkingDirectory },
		Set: func(c *corelib.AppConfig, v string) { c.WorkingDirectory = v },
	},
	{
		Key: "language", Tab: CfgTabGeneral, Section: "general",
		DescKey: i18n.MsgTUIConfigDescLanguage, Options: []string{"zh", "en"},
		Get: func(c *corelib.AppConfig) string { return c.Language },
		Set: func(c *corelib.AppConfig, v string) { c.Language = v },
	},
	{
		Key: "max_iterations", Tab: CfgTabGeneral, Section: "general",
		DescKey: i18n.MsgTUIConfigDescMaxIterations, Default: fmt.Sprintf("%d", config.MaxAgentIterationsCap),
		Get: intGet(func(c *corelib.AppConfig) int { return c.MaclawAgentMaxIterations }),
		Set: intSet(func(c *corelib.AppConfig, v int) { c.MaclawAgentMaxIterations = v }),
	},
	{
		Key: "agentnet_enabled", Tab: CfgTabGeneral, Section: "general",
		DescKey: i18n.MsgTUIConfigDescAgentNetEnabled, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.AgentNetEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.AgentNetEnabled = v }),
	},
	{
		Key: "check_update_on_startup", Tab: CfgTabGeneral, Section: "general",
		DescKey: i18n.MsgTUIConfigDescCheckUpdate, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.CheckUpdateOnStartup }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.CheckUpdateOnStartup = v }),
	},

	// ---- Tab 1: LLM ----
	{
		Key: "maclaw_llm_url", Tab: CfgTabLLM, Section: "maclaw_llm",
		DescKey: i18n.MsgTUIConfigDescLLMURL,
		Get: func(c *corelib.AppConfig) string { return c.MaclawLLMUrl },
		Set: func(c *corelib.AppConfig, v string) { c.MaclawLLMUrl = v },
	},
	{
		Key: "maclaw_llm_key", Tab: CfgTabLLM, Section: "maclaw_llm",
		DescKey: i18n.MsgTUIConfigDescLLMKey,
		Get: func(c *corelib.AppConfig) string { return c.MaclawLLMKey },
		Set: func(c *corelib.AppConfig, v string) { c.MaclawLLMKey = v },
	},
	{
		Key: "maclaw_llm_model", Tab: CfgTabLLM, Section: "maclaw_llm",
		DescKey: i18n.MsgTUIConfigDescLLMModel,
		Get: func(c *corelib.AppConfig) string { return c.MaclawLLMModel },
		Set: func(c *corelib.AppConfig, v string) { c.MaclawLLMModel = v },
	},
	{
		Key: "maclaw_llm_protocol", Tab: CfgTabLLM, Section: "maclaw_llm",
		DescKey: i18n.MsgTUIConfigDescLLMProtocol, Options: []string{"openai", "anthropic"}, Default: "openai",
		Get: func(c *corelib.AppConfig) string { return c.MaclawLLMProtocol },
		Set: func(c *corelib.AppConfig, v string) { c.MaclawLLMProtocol = v },
	},
	{
		Key: "maclaw_llm_context_length", Tab: CfgTabLLM, Section: "maclaw_llm",
		DescKey: i18n.MsgTUIConfigDescLLMContextLength,
		Get: intGet(func(c *corelib.AppConfig) int { return c.MaclawLLMContextLength }),
		Set: intSet(func(c *corelib.AppConfig, v int) { c.MaclawLLMContextLength = v }),
	},
	// Auxiliary LLM
	{
		Key: "aux_llm_url", Tab: CfgTabLLM, Section: "aux_llm",
		DescKey: i18n.MsgTUIConfigDescAuxLLMURL,
		Get: func(c *corelib.AppConfig) string { return c.AuxiliaryLLM.URL },
		Set: func(c *corelib.AppConfig, v string) { c.AuxiliaryLLM.URL = v },
	},
	{
		Key: "aux_llm_key", Tab: CfgTabLLM, Section: "aux_llm",
		DescKey: i18n.MsgTUIConfigDescAuxLLMKey,
		Get: func(c *corelib.AppConfig) string { return c.AuxiliaryLLM.Key },
		Set: func(c *corelib.AppConfig, v string) { c.AuxiliaryLLM.Key = v },
	},
	{
		Key: "aux_llm_model", Tab: CfgTabLLM, Section: "aux_llm",
		DescKey: i18n.MsgTUIConfigDescAuxLLMModel,
		Get: func(c *corelib.AppConfig) string { return c.AuxiliaryLLM.Model },
		Set: func(c *corelib.AppConfig, v string) { c.AuxiliaryLLM.Model = v },
	},
	{
		Key: "aux_llm_protocol", Tab: CfgTabLLM, Section: "aux_llm",
		DescKey: i18n.MsgTUIConfigDescAuxLLMProtocol, Options: []string{"openai", "anthropic"}, Default: "openai",
		Get: func(c *corelib.AppConfig) string { return c.AuxiliaryLLM.Protocol },
		Set: func(c *corelib.AppConfig, v string) { c.AuxiliaryLLM.Protocol = v },
	},

	// ---- Tab 2: IM ----
	{
		Key: "qqbot_enabled", Tab: CfgTabIM, Section: "qqbot",
		DescKey: i18n.MsgTUIConfigDescQQBotEnabled, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.QQBotEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.QQBotEnabled = v }),
	},
	{
		Key: "qqbot_app_id", Tab: CfgTabIM, Section: "qqbot",
		DescKey: i18n.MsgTUIConfigDescQQBotAppID,
		Get: func(c *corelib.AppConfig) string { return c.QQBotAppID },
		Set: func(c *corelib.AppConfig, v string) { c.QQBotAppID = v },
	},
	{
		Key: "qqbot_app_secret", Tab: CfgTabIM, Section: "qqbot",
		DescKey: i18n.MsgTUIConfigDescQQBotAppSecret,
		Get: func(c *corelib.AppConfig) string { return c.QQBotAppSecret },
		Set: func(c *corelib.AppConfig, v string) { c.QQBotAppSecret = v },
	},
	{
		Key: "telegram_bot_enabled", Tab: CfgTabIM, Section: "telegram",
		DescKey: i18n.MsgTUIConfigDescTelegramEnabled, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.TelegramBotEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.TelegramBotEnabled = v }),
	},
	{
		Key: "telegram_bot_token", Tab: CfgTabIM, Section: "telegram",
		DescKey: i18n.MsgTUIConfigDescTelegramToken,
		Get: func(c *corelib.AppConfig) string { return c.TelegramBotToken },
		Set: func(c *corelib.AppConfig, v string) { c.TelegramBotToken = v },
	},
	{
		Key: "weixin_enabled", Tab: CfgTabIM, Section: "weixin",
		DescKey: i18n.MsgTUIConfigDescWeixinEnabled, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.WeixinEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.WeixinEnabled = v }),
	},
	{
		Key: "weixin_token", Tab: CfgTabIM, Section: "weixin",
		DescKey: i18n.MsgTUIConfigDescWeixinToken,
		Get: func(c *corelib.AppConfig) string { return c.WeixinToken },
		Set: func(c *corelib.AppConfig, v string) { c.WeixinToken = v },
	},
	{
		Key: "weixin_base_url", Tab: CfgTabIM, Section: "weixin",
		DescKey: i18n.MsgTUIConfigDescWeixinBaseURL,
		Get: func(c *corelib.AppConfig) string { return c.WeixinBaseURL },
		Set: func(c *corelib.AppConfig, v string) { c.WeixinBaseURL = v },
	},
	{
		Key: "lansenger_enabled", Tab: CfgTabIM, Section: "lansenger",
		DescKey: i18n.MsgTUIConfigDescLansengerEnabled, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.LansengerEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.LansengerEnabled = v }),
	},
	{
		Key: "lansenger_app_id", Tab: CfgTabIM, Section: "lansenger",
		DescKey: i18n.MsgTUIConfigDescLansengerAppID,
		Get: func(c *corelib.AppConfig) string { return c.LansengerAppID },
		Set: func(c *corelib.AppConfig, v string) { c.LansengerAppID = v },
	},
	{
		Key: "lansenger_app_secret", Tab: CfgTabIM, Section: "lansenger",
		DescKey: i18n.MsgTUIConfigDescLansengerAppSecret,
		Get: func(c *corelib.AppConfig) string { return c.LansengerAppSecret },
		Set: func(c *corelib.AppConfig, v string) { c.LansengerAppSecret = v },
	},
	{
		Key: "lansenger_gateway_url", Tab: CfgTabIM, Section: "lansenger",
		DescKey: i18n.MsgTUIConfigDescLansengerGateway,
		Get: func(c *corelib.AppConfig) string { return c.LansengerGatewayURL },
		Set: func(c *corelib.AppConfig, v string) { c.LansengerGatewayURL = v },
	},

	// ---- Tab 3: Proxy ----
	{
		Key: "default_proxy_enabled", Tab: CfgTabProxy, Section: "proxy",
		DescKey: i18n.MsgTUIConfigDescProxyEnabled, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.DefaultProxyEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.DefaultProxyEnabled = v }),
	},
	{
		Key: "default_proxy_protocol", Tab: CfgTabProxy, Section: "proxy",
		DescKey: i18n.MsgTUIConfigDescProxyProtocol, Options: []string{"http", "https", "socks5"}, Default: "http",
		Get: func(c *corelib.AppConfig) string { return c.DefaultProxyProtocol },
		Set: func(c *corelib.AppConfig, v string) { c.DefaultProxyProtocol = v },
	},
	{
		Key: "default_proxy_host", Tab: CfgTabProxy, Section: "proxy",
		DescKey: i18n.MsgTUIConfigDescProxyHost,
		Get: func(c *corelib.AppConfig) string { return c.DefaultProxyHost },
		Set: func(c *corelib.AppConfig, v string) { c.DefaultProxyHost = v },
	},
	{
		Key: "default_proxy_port", Tab: CfgTabProxy, Section: "proxy",
		DescKey: i18n.MsgTUIConfigDescProxyPort,
		Get: func(c *corelib.AppConfig) string { return c.DefaultProxyPort },
		Set: func(c *corelib.AppConfig, v string) { c.DefaultProxyPort = v },
	},
	{
		Key: "default_proxy_username", Tab: CfgTabProxy, Section: "proxy",
		DescKey: i18n.MsgTUIConfigDescProxyUser,
		Get: func(c *corelib.AppConfig) string { return c.DefaultProxyUsername },
		Set: func(c *corelib.AppConfig, v string) { c.DefaultProxyUsername = v },
	},
	{
		Key: "default_proxy_password", Tab: CfgTabProxy, Section: "proxy",
		DescKey: i18n.MsgTUIConfigDescProxyPass,
		Get: func(c *corelib.AppConfig) string { return c.DefaultProxyPassword },
		Set: func(c *corelib.AppConfig, v string) { c.DefaultProxyPassword = v },
	},
	{
		Key: "default_proxy_scope_maclaw", Tab: CfgTabProxy, Section: "proxy",
		DescKey: i18n.MsgTUIConfigDescProxyScopeLLM, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.DefaultProxyScopeMaclaw }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.DefaultProxyScopeMaclaw = v }),
	},
	{
		Key: "default_proxy_scope_agent", Tab: CfgTabProxy, Section: "proxy",
		DescKey: i18n.MsgTUIConfigDescProxyScopeAgent, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.DefaultProxyScopeAgent }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.DefaultProxyScopeAgent = v }),
	},

	// ---- Tab 4: Security ----
	{
		Key: "security_policy_mode", Tab: CfgTabSecurity, Section: "security",
		DescKey: i18n.MsgTUIConfigDescSecurityMode, Options: []string{"standard", "strict", "permissive", "developer"}, Default: "standard",
		Get: func(c *corelib.AppConfig) string { return c.SecurityPolicyMode },
		Set: func(c *corelib.AppConfig, v string) { c.SecurityPolicyMode = v },
	},
	{
		Key: "sandbox_mode", Tab: CfgTabSecurity, Section: "security",
		DescKey: i18n.MsgTUIConfigDescSandbox, Options: []string{"none", "os", "docker"}, Default: "none",
		Get: func(c *corelib.AppConfig) string { return c.SandboxMode },
		Set: func(c *corelib.AppConfig, v string) { c.SandboxMode = v },
	},
	{
		Key: "network_level", Tab: CfgTabSecurity, Section: "security",
		DescKey: i18n.MsgTUIConfigDescNetworkLevel, Options: []string{"none", "intranet", "full"}, Default: "full",
		Get: func(c *corelib.AppConfig) string { return c.NetworkLevel },
		Set: func(c *corelib.AppConfig, v string) { c.NetworkLevel = v },
	},
	{
		Key: "yolo_mode_allowed", Tab: CfgTabSecurity, Section: "security",
		DescKey: i18n.MsgTUIConfigDescYoloMode, Options: boolOpts, Default: "true",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.YoloModeAllowed }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.YoloModeAllowed = v }),
	},
	{
		Key: "file_outbound_enabled", Tab: CfgTabSecurity, Section: "security",
		DescKey: i18n.MsgTUIConfigDescFileOutbound, Options: boolOpts, Default: "true",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.FileOutboundEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.FileOutboundEnabled = v }),
	},
	{
		Key: "image_outbound_enabled", Tab: CfgTabSecurity, Section: "security",
		DescKey: i18n.MsgTUIConfigDescImageOutbound, Options: boolOpts, Default: "true",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.ImageOutboundEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.ImageOutboundEnabled = v }),
	},

	// ---- Tab 5: Advanced ----
	{
		Key: "skill_purchase_mode", Tab: CfgTabAdvanced, Section: "skillmarket",
		DescKey: i18n.MsgTUIConfigDescSkillPurchaseMode, Options: []string{"auto", "free_only"}, Default: "auto",
		Get: func(c *corelib.AppConfig) string { return c.SkillPurchaseMode },
		Set: func(c *corelib.AppConfig, v string) { c.SkillPurchaseMode = v },
	},
	{
		Key: "ui_mode", Tab: CfgTabAdvanced, Section: "advanced",
		DescKey: i18n.MsgTUIConfigDescUIMode, Options: []string{"pro", "lite"}, Default: "lite",
		Get: func(c *corelib.AppConfig) string { return c.UIMode },
		Set: func(c *corelib.AppConfig, v string) { c.UIMode = v },
	},
	{
		Key: "memory_auto_compress", Tab: CfgTabAdvanced, Section: "advanced",
		DescKey: i18n.MsgTUIConfigDescMemoryCompress, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.MemoryAutoCompress }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.MemoryAutoCompress = v }),
	},
	{
		Key: "log_detail_enabled", Tab: CfgTabAdvanced, Section: "advanced",
		DescKey: i18n.MsgTUIConfigDescLogDetail, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.LogDetailEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.LogDetailEnabled = v }),
	},
	{
		Key: "llm_trajectory_logging", Tab: CfgTabAdvanced, Section: "advanced",
		DescKey: i18n.MsgTUIConfigDescTrajectory, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.LLMTrajectoryLogging }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.LLMTrajectoryLogging = v }),
	},
	{
		Key: "maclaw_debug_tool_calls", Tab: CfgTabAdvanced, Section: "advanced",
		DescKey: i18n.MsgTUIConfigDescDebugTools, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.MaclawDebugToolCalls }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.MaclawDebugToolCalls = v }),
	},
	{
		Key: "gossip_enabled", Tab: CfgTabAdvanced, Section: "advanced",
		DescKey: i18n.MsgTUIConfigDescGossip, Options: boolOpts, Default: "true",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.GossipEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.GossipEnabled = v }),
	},
	{
		Key: "trial_reflect_enabled", Tab: CfgTabAdvanced, Section: "advanced",
		DescKey: i18n.MsgTUIConfigDescTrialReflect, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.TrialReflectEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.TrialReflectEnabled = v }),
	},
}

// configFieldIndex builds a key->*ConfigFieldDef lookup map (built once, reused).
var configFieldIndex map[string]*ConfigFieldDef

func init() {
	configFieldIndex = make(map[string]*ConfigFieldDef, len(allConfigFields))
	for i := range allConfigFields {
		configFieldIndex[allConfigFields[i].Key] = &allConfigFields[i]
	}
}

// ApplyConfigValue applies a TUI config key+value to an AppConfig.
// This is the ONLY write path -- called by app.go's saveConfig.
func ApplyConfigValue(cfg *corelib.AppConfig, key, value string) {
	if def, ok := configFieldIndex[key]; ok && def.Set != nil {
		def.Set(cfg, value)
	}
}

// LoadConfigValue reads a TUI config key from an AppConfig.
// Returns ("", false) if the key is unknown.
func LoadConfigValue(cfg *corelib.AppConfig, key string) (string, bool) {
	if def, ok := configFieldIndex[key]; ok && def.Get != nil {
		return def.Get(cfg), true
	}
	return "", false
}
