package views

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// isSensitiveKey checks whether a config key is sensitive.
func isSensitiveKey(key string) bool {
	return strings.Contains(key, "token") || strings.Contains(key, "secret") ||
		strings.Contains(key, "password") || strings.Contains(key, "_key")
}

// configDisplayName returns a human-readable label for a config key.
// The saved key remains unchanged; this is display-only.
func configDisplayName(key string) string {
	return configDisplayNameForLang(key, "zh")
}

// ConfigDisplayNameForLang returns a localized display label for a config key.
func ConfigDisplayNameForLang(key, lang string) string {
	return configDisplayNameForLang(key, lang)
}

func configDisplayNameForLang(key, lang string) string {
	zh := map[string]string{
		"setup_status": "初始化状态", "onboarding": "初始化", "configuration": "配置", "hub_url": "Hub 服务地址", "hubcenter_url": "HubCenter 地址", "token": "认证令牌", "data_dir": "数据目录", "working_directory_profile": "工作目录方案", "working_directory": "自定义工作目录", "language": "界面语言", "max_iterations": "最大轮数", "check_update_on_startup": "启动检查更新",
		"maclaw_llm_provider_preset": "LLM 服务商", "maclaw_llm_model_choice": "模型快选", "maclaw_llm_url": "LLM 地址", "maclaw_llm_key": "LLM 密钥", "maclaw_llm_model": "LLM 模型", "maclaw_llm_protocol": "LLM 协议", "maclaw_llm_context_length": "上下文长度",
		"aux_llm_profile": "辅助 LLM 方案", "aux_llm_url": "辅助 LLM 地址", "aux_llm_key": "辅助 LLM 密钥", "aux_llm_model": "辅助 LLM 模型", "aux_llm_protocol": "辅助 LLM 协议",
		"im_channel_profile": "IM 通道方案", "qqbot_enabled": "QQ 机器人开关", "qqbot_app_id": "QQ AppID", "qqbot_app_secret": "QQ AppSecret", "telegram_bot_enabled": "Telegram 开关", "telegram_bot_token": "Telegram Token", "weixin_enabled": "微信开关", "weixin_token": "微信 Token", "weixin_base_url": "微信地址", "lansenger_enabled": "蓝信开关", "lansenger_app_id": "蓝信 AppID", "lansenger_app_secret": "蓝信密钥", "lansenger_gateway_url": "蓝信网关",
		"default_proxy_profile": "代理方案", "default_proxy_enabled": "代理开关", "default_proxy_protocol": "代理协议", "default_proxy_host": "代理主机", "default_proxy_port": "代理端口", "default_proxy_username": "代理用户", "default_proxy_password": "代理密码", "default_proxy_scope_maclaw": "代理 LLM", "default_proxy_scope_agent": "代理 Agent",
		"security_profile": "安全方案", "security_policy_mode": "安全策略", "sandbox_mode": "沙箱模式", "network_level": "网络级别", "yolo_mode_allowed": "YOLO 模式", "file_outbound_enabled": "文件外发", "image_outbound_enabled": "图片外发",
		"skill_purchase_mode": "技能购买", "ui_mode": "界面模式", "memory_auto_compress": "记忆压缩", "log_detail_enabled": "详细日志", "llm_trajectory_logging": "LLM 轨迹", "maclaw_debug_tool_calls": "工具调试", "gossip_enabled": "Gossip 开关", "trial_reflect_enabled": "试错反思",
	}
	en := map[string]string{
		"setup_status": "Setup status", "onboarding": "Setup", "configuration": "configuration", "hub_url": "Hub URL", "hubcenter_url": "HubCenter URL", "token": "Auth token", "data_dir": "Data dir", "working_directory_profile": "Work dir profile", "working_directory": "Custom work dir", "language": "Language", "max_iterations": "Max rounds", "check_update_on_startup": "Check update",
		"maclaw_llm_provider_preset": "LLM provider", "maclaw_llm_model_choice": "Model quick pick", "maclaw_llm_url": "LLM URL", "maclaw_llm_key": "LLM key", "maclaw_llm_model": "LLM model", "maclaw_llm_protocol": "LLM protocol", "maclaw_llm_context_length": "Context length",
		"aux_llm_profile": "Aux LLM profile", "aux_llm_url": "Aux LLM URL", "aux_llm_key": "Aux LLM key", "aux_llm_model": "Aux LLM model", "aux_llm_protocol": "Aux LLM protocol",
		"im_channel_profile": "IM channel", "qqbot_enabled": "QQ bot", "qqbot_app_id": "QQ AppID", "qqbot_app_secret": "QQ AppSecret", "telegram_bot_enabled": "Telegram", "telegram_bot_token": "Telegram token", "weixin_enabled": "WeChat", "weixin_token": "WeChat token", "weixin_base_url": "WeChat URL", "lansenger_enabled": "Lansenger", "lansenger_app_id": "Lansenger ID", "lansenger_app_secret": "Lansenger secret", "lansenger_gateway_url": "Lansenger gateway",
		"default_proxy_profile": "Proxy profile", "default_proxy_enabled": "Proxy", "default_proxy_protocol": "Proxy protocol", "default_proxy_host": "Proxy host", "default_proxy_port": "Proxy port", "default_proxy_username": "Proxy user", "default_proxy_password": "Proxy password", "default_proxy_scope_maclaw": "Proxy LLM", "default_proxy_scope_agent": "Proxy Agent",
		"security_profile": "Security profile", "security_policy_mode": "Security mode", "sandbox_mode": "Sandbox", "network_level": "Network level", "yolo_mode_allowed": "YOLO mode", "file_outbound_enabled": "File outbound", "image_outbound_enabled": "Image outbound",
		"skill_purchase_mode": "Skill purchase", "ui_mode": "UI mode", "memory_auto_compress": "Memory compress", "log_detail_enabled": "Detail logs", "llm_trajectory_logging": "LLM trajectory", "maclaw_debug_tool_calls": "Debug tools", "gossip_enabled": "Gossip", "trial_reflect_enabled": "Trial reflect",
	}
	zh["smart_route_enabled"] = "智能路由"
	en["smart_route_enabled"] = "Smart route"
	if lang == "en" {
		if label, ok := en[key]; ok {
			return label
		}
	} else if label, ok := zh[key]; ok {
		return label
	}
	return key
}

func configNameWidth(width int) int {
	if width > 0 && width < 48 {
		return max(10, min(18, width/2))
	}
	if width > 0 && width < 72 {
		return 18
	}
	if width < 88 {
		return 24
	}
	if width < 120 {
		return 32
	}
	return 38
}

func configValueWidth(width, nameWidth int) int {
	if width <= 0 {
		return 28
	}
	available := width - nameWidth - 5
	if available < 8 {
		return 8
	}
	if available > 36 {
		return 36
	}
	return available
}

func padDisplay(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s + " "
	}
	return s + strings.Repeat(" ", width-w)
}

func fitDisplay(s string, width int) string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if used+rw > width-1 {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	b.WriteString("…")
	return b.String()
}

func configVisibleRows(height int) int {
	rows := height - 11
	if rows < 7 {
		return 7
	}
	return rows
}

func configSuggestionVisibleRows(height int) int {
	if height > 0 && height < 16 {
		return max(1, height-8)
	}
	rows := height - 15
	if rows < 4 {
		return 4
	}
	if rows > 7 {
		return 7
	}
	return rows
}

func (m ConfigModel) useCompactView() bool {
	return m.height > 0 && m.height < 16
}

func (m ConfigModel) visibleEntryRows(compact bool, hasHint bool) int {
	if !compact {
		return configVisibleRows(m.height)
	}
	available := m.height - 4
	if available < 1 {
		available = 1
	}
	used := 2 // config tabs + footer
	if hasHint {
		used++
	}
	rows := available - used
	if rows < 2 {
		return 2
	}
	return rows
}

func configScrollWindow(total, cursor, visible int) (int, int) {
	if visible <= 0 || total <= visible {
		return 0, total
	}
	start := cursor - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > total {
		start = total - visible
	}
	return start, start + visible
}

func configOptionDisplay(key, value, lang string) string {
	if key == "hubcenter_url" && value == "" {
		return remote.DefaultRemoteHubCenterURL
	}
	if value == "" {
		if lang == "en" {
			return "Default"
		}
		return "默认"
	}
	switch value {
	case "true":
		if lang == "en" {
			return "On"
		}
		return "开启"
	case "false":
		if lang == "en" {
			return "Off"
		}
		return "关闭"
	}
	if key == "setup_status" {
		switch value {
		case "needs_setup":
			if lang == "en" {
				return "Setup needed"
			}
			return "需要初始化"
		case "needs_llm_key":
			if lang == "en" {
				return "LLM key needed"
			}
			return "需要 LLM 密钥"
		case "needs_redeem":
			if lang == "en" {
				return "Redeem service"
			}
			return "需要服务兑换"
		case "mcp_optional":
			if lang == "en" {
				return "Add MCP templates"
			}
			return "可添加 MCP 模板"
		case "official_ready":
			if lang == "en" {
				return "Official LLM ready"
			}
			return "官方 LLM 可用"
		case "llm_ready":
			if lang == "en" {
				return "LLM ready"
			}
			return "LLM 可用"
		}
	}
	if key == "language" {
		switch value {
		case "zh":
			if lang == "en" {
				return "Chinese"
			}
			return "中文"
		case "en":
			if lang == "en" {
				return "English"
			}
			return "英文"
		}
	}
	if key == "maclaw_llm_provider_preset" {
		return serviceRedeemProviderDisplayName(lang, value)
	}
	if key == "maclaw_llm_model_choice" && value == "auto" {
		if lang == "en" {
			return "Auto"
		}
		return "自动"
	}
	if key == "working_directory_profile" {
		switch value {
		case "default_workspace":
			if lang == "en" {
				return "Default workspace"
			}
			return "默认工作区"
		case "current_directory":
			if lang == "en" {
				return "Current directory"
			}
			return "当前目录"
		case "home_directory":
			if lang == "en" {
				return "Home directory"
			}
			return "用户目录"
		case "custom":
			if lang == "en" {
				return "Custom"
			}
			return "自定义"
		}
	}
	if key == "im_channel_profile" {
		switch value {
		case "off":
			if lang == "en" {
				return "Off"
			}
			return "关闭"
		case "weixin":
			if lang == "en" {
				return "WeChat"
			}
			return "微信"
		case "telegram":
			return "Telegram"
		case "qq":
			return "QQ"
		case "lansenger":
			if lang == "en" {
				return "Lansenger"
			}
			return "蓝信"
		case "custom":
			if lang == "en" {
				return "Custom"
			}
			return "自定义"
		}
	}
	if key == "default_proxy_profile" {
		switch value {
		case "off":
			if lang == "en" {
				return "Off"
			}
			return "关闭"
		case "local_http_7890":
			return "HTTP 127.0.0.1:7890"
		case "local_http_8080":
			return "HTTP 127.0.0.1:8080"
		case "local_socks5_1080":
			return "SOCKS5 127.0.0.1:1080"
		case "custom":
			if lang == "en" {
				return "Custom"
			}
			return "自定义"
		}
	}
	if key == "aux_llm_profile" {
		switch value {
		case "off":
			if lang == "en" {
				return "Off"
			}
			return "关闭"
		case "same_as_primary":
			if lang == "en" {
				return "Same as primary"
			}
			return "跟随主 LLM"
		case "custom":
			if lang == "en" {
				return "Custom"
			}
			return "自定义"
		}
	}
	if key == "security_profile" {
		switch value {
		case "standard":
			if lang == "en" {
				return "Standard"
			}
			return "标准"
		case "strict":
			if lang == "en" {
				return "Strict"
			}
			return "严格"
		case "offline":
			if lang == "en" {
				return "Offline"
			}
			return "离线"
		case "developer":
			if lang == "en" {
				return "Developer"
			}
			return "开发"
		case "custom":
			if lang == "en" {
				return "Custom"
			}
			return "自定义"
		}
	}
	if key == "security_policy_mode" {
		switch value {
		case "standard":
			if lang == "en" {
				return "Standard"
			}
			return "标准"
		case "strict":
			if lang == "en" {
				return "Strict"
			}
			return "严格"
		case "relaxed", "permissive":
			if lang == "en" {
				return "Relaxed"
			}
			return "宽松"
		case "developer":
			if lang == "en" {
				return "Developer"
			}
			return "开发"
		}
	}
	if key == "sandbox_mode" {
		switch value {
		case "none":
			if lang == "en" {
				return "Off"
			}
			return "关闭"
		case "os":
			if lang == "en" {
				return "OS sandbox"
			}
			return "系统沙箱"
		case "docker":
			return "Docker"
		}
	}
	if key == "network_level" {
		switch value {
		case "none":
			if lang == "en" {
				return "Offline"
			}
			return "离线"
		case "intranet":
			if lang == "en" {
				return "Intranet only"
			}
			return "仅内网"
		case "full":
			if lang == "en" {
				return "Full network"
			}
			return "完整网络"
		}
	}
	if key == "skill_purchase_mode" {
		switch value {
		case "auto":
			if lang == "en" {
				return "Auto"
			}
			return "自动"
		case "free_only":
			if lang == "en" {
				return "Free only"
			}
			return "仅免费"
		}
	}
	if key == "ui_mode" {
		switch value {
		case "lite":
			if lang == "en" {
				return "Lite"
			}
			return "简洁"
		case "pro":
			if lang == "en" {
				return "Pro"
			}
			return "专业"
		}
	}
	if key == "maclaw_llm_protocol" || key == "aux_llm_protocol" {
		switch value {
		case "openai":
			if lang == "en" {
				return "OpenAI compatible"
			}
			return "OpenAI 兼容"
		case "anthropic":
			return "Anthropic"
		}
	}
	return value
}

func configSetupHint(status, lang string) string {
	if lang == "en" {
		switch status {
		case "needs_setup":
			return "Next: open Setup, enter email, and activate Hub. Local LLM can also be selected in the LLM tab."
		case "needs_llm_key":
			return "Next: enter the selected provider's API key in the LLM tab, or redeem MaClaw official service."
		case "needs_redeem":
			return "Next: redeem MaClaw official service, or choose a local/custom provider in the LLM tab."
		case "mcp_optional":
			return "Next: add optional MCP capabilities from ready-made Tools templates, or start chatting now."
		case "official_ready":
			return "Ready: MaClaw Official is the default LLM."
		case "llm_ready":
			return "Ready: a default LLM is configured."
		}
		return ""
	}
	switch status {
	case "needs_setup":
		return "下一步：打开初始化，输入邮箱并激活 Hub；也可在 LLM 标签选择本地/自定义模型。"
	case "needs_llm_key":
		return "下一步：在 LLM 标签填写当前服务商密钥；也可兑换 MaClaw 官方服务。"
	case "needs_redeem":
		return "下一步：兑换 MaClaw 官方服务；也可在 LLM 标签选择本地/自定义服务。"
	case "mcp_optional":
		return "下一步：可从工具模板添加 MCP 能力；也可以直接开始聊天。"
	case "official_ready":
		return "已就绪：默认 LLM 使用 MaClaw 官方服务。"
	case "llm_ready":
		return "已就绪：默认 LLM 已配置。"
	}
	return ""
}

// ConfigEntry represents a configuration entry for display.
// Options are strict selector values. Suggestions are quick-fill values for
// free-text fields: Enter opens a chooser, while Space cycles and saves directly.
type ConfigEntry struct {
	Key         string
	Value       string
	Desc        string
	Section     string
	Options     []string // nil = free text input; non-nil = inline selector
	Suggestions []string // quick-fill values for free-text fields
	ReadOnly    bool     // true = display only, cannot edit
	Hidden      bool     // true = hidden from the simplified TUI list
}

func (e ConfigEntry) optionValues() []string {
	return e.Options
}

func (e ConfigEntry) suggestionValues() []string {
	if len(e.Options) > 0 {
		return nil
	}
	return e.Suggestions
}

func (e ConfigEntry) cycleValues() []string {
	if len(e.Options) > 0 {
		return e.Options
	}
	return e.Suggestions
}

// ConfigSaveMsg is a config save message, persisted by the outer layer (app.go).
type ConfigSaveMsg struct {
	Section   string
	Key       string
	Value     string
	Config    corelib.AppConfig
	HasConfig bool
}

type ConfigOpenSetupMsg struct{}

type ConfigOpenServiceRedeemMsg struct{}

type ConfigOpenToolsMsg struct{}

// Config sub-tab constants.
const (
	CfgTabGeneral = iota
	CfgTabLLM
	CfgTabIM
	CfgTabProxy
	CfgTabSecurity
	CfgTabAdvanced
	CfgTabCount
)

// cfgTabNames returns localized tab names.
func cfgTabNames(lang string) [CfgTabCount]string {
	if i18n.NormalizeLang(lang) == "en" {
		return [CfgTabCount]string{"General", "LLM", "IM", "Proxy", "Security", "Advanced"}
	}
	return [CfgTabCount]string{
		"基本",
		"LLM",
		"IM 通道",
		"代理",
		"安全",
		"高级",
	}
}

// ConfigModel is the configuration management view with tabbed layout.
type ConfigModel struct {
	// All entries grouped by tab index — derived from allConfigFields.
	tabs              [CfgTabCount][]ConfigEntry
	activeTab         int
	cursor            int
	editing           bool
	selectMode        bool // true = inline selector active
	selectSuggestions bool // true = selector uses Suggestions plus manual fallback
	selectCursor      int  // cursor within the active selector list
	input             textinput.Model
	lang              string
	width             int // terminal width for rendering
	height            int // terminal height for rendering
	cfg               corelib.AppConfig
	statusOverview    bool
}

// IsEditing returns whether the view is in editing mode.
func (m ConfigModel) IsEditing() bool {
	return m.editing || m.selectMode
}

func (m ConfigModel) ActiveTab() int { return m.activeTab }

// currentEntries returns entries for the active tab.
func (m ConfigModel) currentEntries() []ConfigEntry {
	if m.activeTab < 0 || m.activeTab >= CfgTabCount {
		return nil
	}
	entries := make([]ConfigEntry, 0, len(m.tabs[m.activeTab]))
	for _, entry := range m.tabs[m.activeTab] {
		if !entry.Hidden {
			entries = append(entries, entry)
		}
	}
	return entries
}

func (m *ConfigModel) clampCursor() {
	entries := m.currentEntries()
	if len(entries) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(entries) {
		m.cursor = len(entries) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// NewConfigModel creates a new config view with tabbed layout.
// All entries are derived from the single source of truth (allConfigFields).
func NewConfigModel(lang string) ConfigModel {
	lang = i18n.NormalizeLang(lang)
	ti := textinput.New()
	ti.CharLimit = 256
	ti.Width = 50
	ti.EchoCharacter = '*'

	m := ConfigModel{
		input: ti,
		lang:  lang,
		width: 80,
	}
	m.rebuildFromDefs()
	return m
}

// rebuildFromDefs rebuilds all tab entries from allConfigFields definitions.
// Values are NOT reset — caller must preserve/restore them if needed.
func (m *ConfigModel) rebuildFromDefs() {
	for t := 0; t < CfgTabCount; t++ {
		m.tabs[t] = nil
	}
	for _, def := range allConfigFields {
		if def.Tab < 0 || def.Tab >= CfgTabCount {
			continue
		}
		entry := ConfigEntry{
			Key:      def.Key,
			Value:    def.Default,
			Desc:     i18n.T(def.DescKey, m.lang),
			Section:  def.Section,
			Options:  def.Options,
			ReadOnly: def.ReadOnly,
		}
		m.tabs[def.Tab] = append(m.tabs[def.Tab], entry)
	}
}

func (m *ConfigModel) beginEditing(e ConfigEntry) {
	m.editing = true
	m.input.SetValue(e.Value)
	m.input.EchoMode = textinput.EchoNormal
	if isSensitiveKey(e.Key) {
		m.input.EchoMode = textinput.EchoPassword
	}
	m.input.Focus()
	m.input.CursorEnd()
}

func (m *ConfigModel) stopEditing() {
	m.editing = false
	m.input.Blur()
	m.input.EchoMode = textinput.EchoNormal
}

func (m *ConfigModel) refreshDynamicOptions(cfg corelib.AppConfig) {
	m.refreshGeneralVisibility(cfg)
	m.refreshIMVisibility(cfg)
	m.refreshProxyVisibility(cfg)
	m.refreshLLMVisibility(cfg)
	m.refreshSecurityVisibility(cfg)
	m.refreshAdvancedVisibility(cfg)
	m.setEntryOptions("working_directory_profile", appendUniqueOption(cloneConfigValues(workDirProfileOpts...), currentWorkingDirectoryProfile(&cfg)))
	m.setEntryOptions("maclaw_llm_provider_preset", llmProviderOptionsFromConfig(&cfg))
	m.setEntryOptions("max_iterations", appendUniqueOption(cloneConfigValues(maxIterationOpts...), intConfigValue(cfg.MaclawAgentMaxIterations)))
	m.setEntryOptions("maclaw_llm_context_length", appendUniqueOption(cloneConfigValues(contextLengthOpts...), intConfigValue(cfg.MaclawLLMContextLength)))
	m.setEntryOptions("maclaw_llm_model_choice", llmModelOptionsFromConfig(&cfg))
	m.setEntryOptions("aux_llm_profile", appendUniqueOption(cloneConfigValues(auxLLMProfileOpts...), currentAuxLLMProfile(&cfg)))
	m.setEntryOptions("im_channel_profile", appendUniqueOption(cloneConfigValues(imChannelProfileOpts...), currentIMChannelProfile(&cfg)))
	m.setEntryOptions("default_proxy_port", appendUniqueOption(cloneConfigValues(proxyPortOpts...), cfg.DefaultProxyPort))
	m.setEntryOptions("default_proxy_profile", appendUniqueOption(cloneConfigValues(proxyProfileOpts...), currentProxyProfile(&cfg)))
	m.setEntryOptions("security_profile", appendUniqueOption(cloneConfigValues(securityProfileOpts...), currentSecurityProfile(&cfg)))

	m.setEntrySuggestions("working_directory", workingDirectorySuggestions(cfg))
	m.setEntrySuggestions("hubcenter_url", hubCenterURLSuggestions(cfg))
	m.setEntrySuggestions("maclaw_llm_url", llmURLSuggestions(cfg))
	m.setEntrySuggestions("maclaw_llm_model", llmModelSuggestions(cfg))
	m.setEntrySuggestions("aux_llm_url", auxLLMURLSuggestions(cfg))
	m.setEntrySuggestions("aux_llm_model", auxLLMModelSuggestions(cfg))
	m.setEntrySuggestions("weixin_base_url", uniqueConfigValues(cfg.WeixinBaseURL, "https://ilinkai.weixin.qq.com"))
	m.setEntrySuggestions("lansenger_gateway_url", uniqueConfigValues(cfg.LansengerGatewayURL, "https://apigw.lx.qianxin.com"))
	m.setEntrySuggestions("default_proxy_host", uniqueConfigValues(cfg.DefaultProxyHost, "127.0.0.1", "localhost"))
}

func (m *ConfigModel) setEntryOptions(key string, options []string) {
	for t := 0; t < CfgTabCount; t++ {
		for i := range m.tabs[t] {
			if m.tabs[t][i].Key == key {
				m.tabs[t][i].Options = cloneConfigValues(options...)
				return
			}
		}
	}
}

func (m *ConfigModel) setEntrySuggestions(key string, suggestions []string) {
	for t := 0; t < CfgTabCount; t++ {
		for i := range m.tabs[t] {
			if m.tabs[t][i].Key == key {
				m.tabs[t][i].Suggestions = cloneConfigValues(suggestions...)
				return
			}
		}
	}
}

func (m *ConfigModel) setEntryHidden(key string, hidden bool) {
	for t := 0; t < CfgTabCount; t++ {
		for i := range m.tabs[t] {
			if m.tabs[t][i].Key == key {
				m.tabs[t][i].Hidden = hidden
				return
			}
		}
	}
}

func (m *ConfigModel) refreshGeneralVisibility(cfg corelib.AppConfig) {
	m.setEntryHidden("hub_url", strings.TrimSpace(cfg.RemoteHubURL) == "")
	m.setEntryHidden("token", strings.TrimSpace(cfg.RemoteMachineToken) == "")
	m.setEntryHidden("working_directory", currentWorkingDirectoryProfile(&cfg) != "custom")
	m.clampCursor()
}

func (m *ConfigModel) refreshLLMVisibility(cfg corelib.AppConfig) {
	provider := currentLLMPresetName(&cfg)
	custom := provider == "Custom"
	hubService := serviceRedeemUsesOfficialLLM(cfg)
	for _, key := range []string{"maclaw_llm_url", "maclaw_llm_model", "maclaw_llm_protocol", "maclaw_llm_context_length"} {
		m.setEntryHidden(key, !custom)
	}
	m.setEntryHidden("maclaw_llm_key", !(custom || llmProviderNeedsKey(&cfg)) || hubService)

	auxCustom := currentAuxLLMProfile(&cfg) == "custom"
	for _, key := range []string{"aux_llm_url", "aux_llm_key", "aux_llm_model", "aux_llm_protocol"} {
		m.setEntryHidden(key, !auxCustom)
	}
	m.clampCursor()
}

func llmProviderNeedsKey(cfg *corelib.AppConfig) bool {
	if cfg == nil {
		return false
	}
	provider := strings.TrimSpace(cfg.MaclawLLMCurrentProvider)
	if provider == "" {
		provider = currentLLMPresetName(cfg)
	}
	if serviceRedeemUsesOfficialLLM(*cfg) {
		return false
	}
	if provider == "Custom" {
		return configLLMURLUsuallyNeedsKey(cfg.MaclawLLMUrl)
	}
	for _, preset := range llmProviderPresets {
		if preset.Name == provider {
			return preset.AuthType != "none"
		}
	}
	for _, saved := range cfg.MaclawLLMProviders {
		if saved.Name == provider {
			authType := strings.TrimSpace(saved.AuthType)
			if authType != "" {
				return !strings.EqualFold(authType, "none")
			}
			break
		}
	}
	return configLLMURLUsuallyNeedsKey(cfg.MaclawLLMUrl)
}

func configLLMURLUsuallyNeedsKey(rawURL string) bool {
	host := configLLMURLHost(rawURL)
	if host == "" {
		return false
	}
	host = strings.Trim(host, "[]")
	lower := strings.ToLower(host)
	if lower == "localhost" || lower == "host.docker.internal" || strings.HasSuffix(lower, ".local") {
		return false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return true
	}
	return !(addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsUnspecified())
}

func configLLMURLHost(rawURL string) string {
	value := strings.TrimSpace(rawURL)
	if value == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		return parsed.Hostname()
	}
	if cut, _, ok := strings.Cut(value, "/"); ok {
		value = cut
	}
	if cut, _, ok := strings.Cut(value, "?"); ok {
		value = cut
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		return host
	}
	if strings.Count(value, ":") == 1 {
		if host, _, ok := strings.Cut(value, ":"); ok {
			return host
		}
	}
	return value
}

func (m *ConfigModel) refreshProxyVisibility(cfg corelib.AppConfig) {
	profile := currentProxyProfile(&cfg)
	for _, key := range []string{
		"default_proxy_enabled", "default_proxy_protocol", "default_proxy_host", "default_proxy_port",
		"default_proxy_username", "default_proxy_password", "default_proxy_scope_maclaw", "default_proxy_scope_agent",
	} {
		m.setEntryHidden(key, profile != "custom")
	}
	m.clampCursor()
}

func (m *ConfigModel) refreshSecurityVisibility(cfg corelib.AppConfig) {
	custom := currentSecurityProfile(&cfg) == "custom"
	for _, key := range []string{
		"security_policy_mode", "sandbox_mode", "network_level",
		"yolo_mode_allowed", "file_outbound_enabled", "image_outbound_enabled",
	} {
		m.setEntryHidden(key, !custom)
	}
	m.clampCursor()
}

func (m *ConfigModel) refreshAdvancedVisibility(cfg corelib.AppConfig) {
	pro := strings.EqualFold(strings.TrimSpace(cfg.UIMode), "pro")
	for _, key := range []string{
		"memory_auto_compress", "log_detail_enabled", "llm_trajectory_logging",
		"maclaw_debug_tool_calls", "gossip_enabled", "trial_reflect_enabled",
	} {
		m.setEntryHidden(key, !pro)
	}
	m.clampCursor()
}

func (m *ConfigModel) refreshIMVisibility(cfg corelib.AppConfig) {
	profile := currentIMChannelProfile(&cfg)
	for _, key := range []string{
		"qqbot_enabled", "qqbot_app_id", "qqbot_app_secret",
		"telegram_bot_enabled", "telegram_bot_token",
		"weixin_enabled", "weixin_token", "weixin_base_url",
		"lansenger_enabled", "lansenger_app_id", "lansenger_app_secret", "lansenger_gateway_url",
	} {
		m.setEntryHidden(key, true)
	}
	show := func(keys ...string) {
		for _, key := range keys {
			m.setEntryHidden(key, false)
		}
	}
	switch profile {
	case "qq":
		show("qqbot_enabled", "qqbot_app_id", "qqbot_app_secret")
	case "telegram":
		show("telegram_bot_enabled", "telegram_bot_token")
	case "weixin":
		show("weixin_enabled", "weixin_token", "weixin_base_url")
	case "lansenger":
		show("lansenger_enabled", "lansenger_app_id", "lansenger_app_secret", "lansenger_gateway_url")
	case "custom":
		show("qqbot_enabled", "qqbot_app_id", "qqbot_app_secret",
			"telegram_bot_enabled", "telegram_bot_token",
			"weixin_enabled", "weixin_token", "weixin_base_url",
			"lansenger_enabled", "lansenger_app_id", "lansenger_app_secret", "lansenger_gateway_url")
	}
	m.setEntryHidden("weixin_token", strings.TrimSpace(cfg.WeixinToken) == "")
	m.clampCursor()
}

func intConfigValue(v int) string {
	if v <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", v)
}

func cloneConfigValues(values ...string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func uniqueConfigValues(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = appendUniqueOption(out, value)
	}
	return out
}

func workingDirectorySuggestions(cfg corelib.AppConfig) []string {
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	return uniqueConfigValues(cfg.WorkingDirectory, corelib.WorkspaceDir(), cwd, home)
}

func hubCenterURLSuggestions(cfg corelib.AppConfig) []string {
	values := []string{cfg.RemoteHubCenterURL}
	values = append(values, cfg.RemoteHubCenterURLs...)
	values = append(values, remote.DefaultRemoteHubCenterURLs...)
	return uniqueConfigValues(values...)
}

func llmURLSuggestions(cfg corelib.AppConfig) []string {
	values := []string{cfg.MaclawLLMUrl}
	for _, p := range llmProviderPresets {
		values = append(values, p.URL)
	}
	for _, p := range cfg.MaclawLLMProviders {
		values = append(values, p.URL)
	}
	values = append(values,
		"http://localhost:11434/v1",
		"http://127.0.0.1:11434/v1",
		"http://localhost:1234/v1",
	)
	return uniqueConfigValues(values...)
}

func llmModelSuggestions(cfg corelib.AppConfig) []string {
	values := []string{cfg.MaclawLLMModel}
	for _, p := range llmProviderPresets {
		values = append(values, p.Model)
	}
	for _, p := range cfg.MaclawLLMProviders {
		values = append(values, p.Model)
	}
	values = append(values,
		"auto",
		"qwen2.5-coder:32b",
		"deepseek-coder-v2",
		"llama3.1",
	)
	return uniqueConfigValues(values...)
}

func auxLLMURLSuggestions(cfg corelib.AppConfig) []string {
	return uniqueConfigValues(
		cfg.AuxiliaryLLM.URL,
		cfg.MaclawLLMUrl,
		"http://localhost:11434/v1",
		"http://127.0.0.1:11434/v1",
		"http://localhost:1234/v1",
	)
}

func auxLLMModelSuggestions(cfg corelib.AppConfig) []string {
	return uniqueConfigValues(
		cfg.AuxiliaryLLM.Model,
		cfg.MaclawLLMModel,
		"auto",
		"qwen2.5-coder:32b",
		"deepseek-coder-v2",
	)
}

// SetLang updates i18n descriptions by rebuilding entries and preserving values.
func (m *ConfigModel) SetLang(lang string) {
	m.lang = i18n.NormalizeLang(lang)
	valMap := m.collectValues()
	m.rebuildFromDefs()
	m.applyValues(valMap)
	m.refreshDynamicOptions(m.cfg)
}

// collectValues snapshots all current values by key.
func (m *ConfigModel) collectValues() map[string]string {
	valMap := make(map[string]string)
	for t := 0; t < CfgTabCount; t++ {
		for _, e := range m.tabs[t] {
			valMap[e.Key] = e.Value
		}
	}
	return valMap
}

// applyValues restores values from a snapshot.
func (m *ConfigModel) applyValues(valMap map[string]string) {
	for t := 0; t < CfgTabCount; t++ {
		for i, e := range m.tabs[t] {
			if v, ok := valMap[e.Key]; ok {
				m.tabs[t][i].Value = v
			}
		}
	}
}

// SetEntries updates config entries (legacy compatibility).
func (m *ConfigModel) SetEntries(entries []ConfigEntry) {
	for _, e := range entries {
		m.setEntryValue(e.Key, e.Value)
	}
}

// setEntryValue sets a value by key across all tabs.
func (m *ConfigModel) setEntryValue(key, value string) {
	for t := 0; t < CfgTabCount; t++ {
		for i, e := range m.tabs[t] {
			if e.Key == key {
				m.tabs[t][i].Value = value
				return
			}
		}
	}
}

func (m *ConfigModel) syncValuesFromConfig() {
	for _, def := range allConfigFields {
		if def.Get == nil {
			continue
		}
		m.setEntryValue(def.Key, def.Get(&m.cfg))
	}
}

func (m *ConfigModel) commitEntryValue(e ConfigEntry, value string) tea.Cmd {
	ApplyConfigValue(&m.cfg, e.Key, value)
	m.refreshDynamicOptions(m.cfg)
	m.syncValuesFromConfig()
	return func() tea.Msg {
		return ConfigSaveMsg{Section: e.Section, Key: e.Key, Value: value, Config: m.cfg, HasConfig: true}
	}
}

// FocusLLMConfig switches to the LLM tab and moves cursor to the first field.
func (m *ConfigModel) FocusLLMConfig() {
	m.FocusTab(CfgTabLLM)
}

// FocusLLMKey switches to the LLM tab and focuses the key field when visible.
func (m *ConfigModel) FocusLLMKey() {
	m.FocusTab(CfgTabLLM)
	entries := m.currentEntries()
	for i, entry := range entries {
		if entry.Key == "maclaw_llm_key" {
			m.cursor = i
			return
		}
	}
}

// FocusSetupStatus switches to General and focuses the setup status row.
func (m *ConfigModel) FocusSetupStatus() {
	m.FocusTab(CfgTabGeneral)
	m.statusOverview = true
	entries := m.currentEntries()
	for i, entry := range entries {
		if entry.Key == "setup_status" {
			m.cursor = i
			return
		}
	}
}

// FocusTab switches to a config tab and moves cursor to the first field.
func (m *ConfigModel) FocusTab(tab int) {
	if tab < 0 || tab >= CfgTabCount {
		return
	}
	m.activeTab = tab
	m.cursor = 0
	m.statusOverview = false
}

// FocusSecurityConfig switches to the Security tab and moves cursor to the first field.
func (m *ConfigModel) FocusSecurityConfig() {
	m.FocusTab(CfgTabSecurity)
}

// LoadFromAppConfig syncs config values from AppConfig to the view.
// Uses the Get accessor from each ConfigFieldDef — no manual key mapping needed.
func (m *ConfigModel) LoadFromAppConfig(cfg corelib.AppConfig) {
	m.cfg = cfg
	m.normalizeImplicitDefaults()
	m.refreshDynamicOptions(m.cfg)
	m.syncValuesFromConfig()
}

func (m *ConfigModel) normalizeImplicitDefaults() {
	if securityFieldsAreBlank(&m.cfg) {
		applySecurityProfile(&m.cfg, "relaxed")
	}
}

// Init implements tea.Model.
func (m ConfigModel) Init() tea.Cmd { return nil }

// Update handles keyboard events.
func (m ConfigModel) Update(msg tea.Msg) (ConfigModel, tea.Cmd) {
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = msg.Width
		m.height = msg.Height
	}
	if m.selectMode {
		return m.updateSelect(msg)
	}
	if m.editing {
		return m.updateEditing(msg)
	}
	return m.updateNormal(msg)
}

// updateNormal handles keys in non-editing mode.
func (m ConfigModel) updateNormal(msg tea.Msg) (ConfigModel, tea.Cmd) {
	entries := m.currentEntries()
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		// Tab switching: number keys 1-6
		case "1":
			m.statusOverview = false
			m.activeTab = CfgTabGeneral
			m.cursor = 0
		case "2":
			m.statusOverview = false
			m.activeTab = CfgTabLLM
			m.cursor = 0
		case "3":
			m.statusOverview = false
			m.activeTab = CfgTabIM
			m.cursor = 0
		case "4":
			m.statusOverview = false
			m.activeTab = CfgTabProxy
			m.cursor = 0
		case "5":
			m.statusOverview = false
			m.activeTab = CfgTabSecurity
			m.cursor = 0
		case "6":
			m.statusOverview = false
			m.activeTab = CfgTabAdvanced
			m.cursor = 0
		case "tab", "right":
			m.statusOverview = false
			m.activeTab = (m.activeTab + 1) % CfgTabCount
			m.cursor = 0
		case "shift+tab", "left":
			m.statusOverview = false
			m.activeTab = (m.activeTab - 1 + CfgTabCount) % CfgTabCount
			m.cursor = 0
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(entries)-1 {
				m.cursor++
			}
		case "home", "g":
			m.cursor = 0
		case "end", "G":
			if len(entries) > 0 {
				m.cursor = len(entries) - 1
			}
		case "enter":
			if m.cursor >= len(entries) {
				return m, nil
			}
			e := entries[m.cursor]
			if e.ReadOnly {
				if configReadOnlyOpensSetup(e.Key) {
					return m, func() tea.Msg { return ConfigOpenSetupMsg{} }
				}
				if e.Key == "setup_status" {
					switch currentSetupStatus(&m.cfg) {
					case "needs_setup":
						return m, func() tea.Msg { return ConfigOpenSetupMsg{} }
					case "needs_llm_key":
						m.FocusLLMKey()
					case "needs_redeem", "official_ready":
						return m, func() tea.Msg { return ConfigOpenServiceRedeemMsg{} }
					case "mcp_optional":
						return m, func() tea.Msg { return ConfigOpenToolsMsg{} }
					case "llm_ready":
						m.FocusLLMConfig()
					}
				}
				return m, nil
			}
			// Options and suggestions open a selector first; plain text still edits directly.
			if len(e.optionValues()) > 0 {
				m.selectMode = true
				m.selectSuggestions = false
				m.selectCursor = 0
				for i, opt := range e.optionValues() {
					if opt == e.Value {
						m.selectCursor = i
						break
					}
				}
				return m, nil
			}
			if suggestions := e.suggestionValues(); len(suggestions) > 0 {
				m.selectMode = true
				m.selectSuggestions = true
				m.selectCursor = 0
				for i, opt := range suggestions {
					if opt == e.Value {
						m.selectCursor = i
						break
					}
				}
				return m, nil
			}
			m.selectSuggestions = false
			m.beginEditing(e)
			return m, textinput.Blink
		case " ":
			// Space cycles selector options or quick-fill suggestions directly.
			if m.cursor < len(entries) {
				e := entries[m.cursor]
				cycleValues := e.cycleValues()
				if !e.ReadOnly && len(cycleValues) > 0 {
					newVal := nextConfigOption(cycleValues, e.Value)
					return m, m.commitEntryValue(e, newVal)
				}
			}
		}
	}
	return m, nil
}

func nextConfigOption(options []string, current string) string {
	if len(options) == 0 {
		return current
	}
	for i, opt := range options {
		if opt == current {
			return options[(i+1)%len(options)]
		}
	}
	return options[0]
}

// updateSelect handles keys in inline selector mode.
const configManualInputSelection = "__manual_input__"

func (m ConfigModel) selectionValues(e ConfigEntry) []string {
	if m.selectSuggestions {
		values := cloneConfigValues(e.suggestionValues()...)
		return append(values, configManualInputSelection)
	}
	return e.optionValues()
}

func configSelectionLabel(e ConfigEntry, value, lang string) string {
	if value == configManualInputSelection {
		return configManualInputLabel(lang)
	}
	return configOptionDisplay(e.Key, value, lang)
}

func configShouldRenderVerticalSelection(values []string, e ConfigEntry, width int, lang string) bool {
	if width > 0 && width < 88 {
		return true
	}
	total := 2 + configNameWidth(width)
	for i, value := range values {
		if i > 0 {
			total += 2
		}
		total += lipgloss.Width(configSelectionLabel(e, value, lang)) + 2
	}
	return width > 0 && total > width
}

func configManualInputLabel(lang string) string {
	if i18n.NormalizeLang(lang) == "en" {
		return "Manual input"
	}
	return "手动输入"
}

// updateSelect handles keys in inline selector mode.
func (m ConfigModel) updateSelect(msg tea.Msg) (ConfigModel, tea.Cmd) {
	entries := m.currentEntries()
	if m.cursor >= len(entries) {
		m.selectMode = false
		m.selectSuggestions = false
		return m, nil
	}
	e := entries[m.cursor]
	options := m.selectionValues(e)
	if len(options) == 0 {
		m.selectMode = false
		m.selectSuggestions = false
		return m, nil
	}
	if m.selectCursor >= len(options) {
		m.selectCursor = len(options) - 1
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "left", "h":
			if m.selectCursor > 0 {
				m.selectCursor--
			}
		case "right", "l":
			if m.selectCursor < len(options)-1 {
				m.selectCursor++
			}
		case "up", "k":
			if m.selectCursor > 0 {
				m.selectCursor--
			}
		case "down", "j":
			if m.selectCursor < len(options)-1 {
				m.selectCursor++
			}
		case "enter":
			newVal := options[m.selectCursor]
			m.selectMode = false
			m.selectSuggestions = false
			if newVal == configManualInputSelection {
				m.beginEditing(e)
				return m, textinput.Blink
			}
			return m, m.commitEntryValue(e, newVal)
		case "esc":
			m.selectMode = false
			m.selectSuggestions = false
			return m, nil
		}
	}
	return m, nil
}

// updateEditing handles keys in text editing mode.
func (m ConfigModel) updateEditing(msg tea.Msg) (ConfigModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			entries := m.currentEntries()
			if m.cursor >= len(entries) {
				m.stopEditing()
				return m, nil
			}
			newVal := m.input.Value()
			e := entries[m.cursor]
			m.stopEditing()
			return m, m.commitEntryValue(e, newVal)
		case "esc":
			m.stopEditing()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// ---- Styles (allocated once, reused across renders) ----

var (
	cfgHeaderStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	cfgSelectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	cfgNormalStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	cfgDimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	cfgEditStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	cfgOptActive     = lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	cfgOptNormal     = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	cfgReadOnly      = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)
	cfgSectionStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
	cfgToggleOn      = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	cfgToggleOff     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	cfgTabActive     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57")).Padding(0, 1)
	cfgTabInactive   = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("238")).Padding(0, 1)
)

// View renders the config view with tabs.
func (m ConfigModel) View() string {
	var b strings.Builder
	compact := m.useCompactView()

	// Title
	if !compact {
		b.WriteString(cfgHeaderStyle.Render("  "+i18n.T(i18n.MsgTUIConfigTitle, m.lang)) + "\n")
	}

	// Tab bar
	b.WriteString(m.renderTabs())
	b.WriteString("\n")
	if !compact {
		b.WriteString("  " + strings.Repeat("─", max(0, min(60, m.width-4))) + "\n")
	}
	hint := configSetupHint(currentSetupStatus(&m.cfg), m.lang)
	if hint != "" {
		b.WriteString("  " + cfgDimStyle.Render(fitDisplay(hint, max(20, m.width-4))) + "\n")
	}

	entries := m.currentEntries()
	if compact && m.statusOverview && !m.selectMode && !m.editing && m.cursor < len(entries) && entries[m.cursor].Key == "setup_status" {
		for _, row := range configSetupCompactChecklistRows(m.cfg, m.lang, m.width) {
			b.WriteString("  " + cfgDimStyle.Render(row) + "\n")
		}
		b.WriteString("  " + cfgDimStyle.Render(i18n.T(i18n.MsgTUIConfigFooterNormal, m.lang)))
		return fitRenderedLines(b.String(), m.width)
	}

	visible := m.visibleEntryRows(compact, hint != "")
	start, end := configScrollWindow(len(entries), m.cursor, visible)
	if compact && m.selectMode && m.cursor < len(entries) {
		start, end = m.cursor, m.cursor+1
	}
	if start > 0 && !compact {
		b.WriteString("  " + cfgDimStyle.Render(fmt.Sprintf("↑ %d more", start)) + "\n")
	}

	for i := start; i < end; i++ {
		e := entries[i]
		if !compact && (i == 0 || entries[i-1].Section != e.Section) {
			label := sectionLabel(e.Section, m.lang)
			if label != "" {
				b.WriteString(cfgSectionStyle.Render("  ▸ "+label) + "\n")
			}
		}

		// Inline selector mode for this row.
		name := configDisplayNameForLang(e.Key, m.lang)
		nameWidth := configNameWidth(m.width)
		displayName := fitDisplay(name, nameWidth)
		valueWidth := configValueWidth(m.width, nameWidth)

		if m.selectMode && i == m.cursor {
			line := "  " + padDisplay(displayName, nameWidth)
			b.WriteString(cfgEditStyle.Render(line))
			selection := m.selectionValues(e)
			if m.selectSuggestions || configShouldRenderVerticalSelection(selection, e, m.width, m.lang) {
				b.WriteString("\n")
				labelWidth := max(16, m.width-8)
				visibleSuggestions := configSuggestionVisibleRows(m.height)
				sStart, sEnd := configScrollWindow(len(selection), m.selectCursor, visibleSuggestions)
				if sStart > 0 {
					b.WriteString(cfgDimStyle.Render(fmt.Sprintf("    ↑ %d more", sStart)) + "\n")
				}
				for j := sStart; j < sEnd; j++ {
					opt := selection[j]
					marker := " "
					if j == m.selectCursor {
						marker = ">"
					}
					label := fitDisplay(configSelectionLabel(e, opt, m.lang), labelWidth)
					row := fmt.Sprintf("    %s %s", marker, label)
					if j == m.selectCursor {
						b.WriteString(cfgOptActive.Render(row))
					} else {
						b.WriteString(cfgOptNormal.Render(row))
					}
					b.WriteString("\n")
				}
				if sEnd < len(selection) {
					b.WriteString(cfgDimStyle.Render(fmt.Sprintf("    ↓ %d more", len(selection)-sEnd)) + "\n")
				}
				continue
			}
			for j, opt := range selection {
				if j > 0 {
					b.WriteString("  ")
				}
				label := configSelectionLabel(e, opt, m.lang)
				if j == m.selectCursor {
					b.WriteString(cfgOptActive.Render(" " + label + " "))
				} else {
					b.WriteString(cfgOptNormal.Render(" " + label + " "))
				}
			}
			b.WriteString("\n")
			continue
		}

		// Text editing mode for this row.
		if m.editing && i == m.cursor {
			line := "  " + padDisplay(displayName, nameWidth)
			b.WriteString(cfgEditStyle.Render(line))
			b.WriteString(m.input.View())
			b.WriteString("\n")
			continue
		}

		// Normal display.
		val := e.Value
		options := e.optionValues()
		isBoolField := len(options) == 2 && options[0] == "true" && options[1] == "false"

		if isBoolField {
			if val == "true" {
				val = cfgToggleOn.Render("● " + configOptionDisplay(e.Key, val, m.lang))
			} else {
				val = cfgToggleOff.Render("○ " + configOptionDisplay(e.Key, val, m.lang))
			}
		} else if len(options) > 0 {
			val = configOptionDisplay(e.Key, val, m.lang)
		} else if val == "" {
			val = cfgDimStyle.Render(i18n.T(i18n.MsgTUIConfigNotSet, m.lang))
		} else if isSensitiveKey(e.Key) {
			val = "********"
		}
		if action := configEntryActionLabel(e, m.cfg, m.lang); action != "" {
			val = configValueWithAction(val, action, valueWidth)
		}

		line := fmt.Sprintf("  %s %s", padDisplay(displayName, nameWidth), padDisplay(fitDisplay(val, valueWidth), valueWidth))
		if i == m.cursor {
			b.WriteString(cfgSelectedStyle.Render(line))
		} else {
			b.WriteString(cfgNormalStyle.Render(line))
		}
		b.WriteString("\n")
	}
	if end < len(entries) && !compact {
		b.WriteString("  " + cfgDimStyle.Render(fmt.Sprintf("↓ %d more", len(entries)-end)) + "\n")
	}

	if m.cursor < len(entries) && !m.selectMode && !compact {
		b.WriteString("\n")
		b.WriteString(m.renderDetails(entries[m.cursor]))
	}

	// Footer
	if !compact {
		b.WriteString("\n")
	}
	if m.selectMode {
		b.WriteString("  " + cfgDimStyle.Render(i18n.T(i18n.MsgTUIConfigFooterSelect, m.lang)))
	} else if m.editing {
		b.WriteString("  " + cfgDimStyle.Render(i18n.T(i18n.MsgTUIConfigFooterEditing, m.lang)))
	} else {
		b.WriteString("  " + cfgDimStyle.Render(i18n.T(i18n.MsgTUIConfigFooterNormal, m.lang)))
	}
	return fitRenderedLines(b.String(), m.width)
}

func configSetupActionHint(status, lang string) string {
	if i18n.NormalizeLang(lang) == "en" {
		switch status {
		case "needs_setup":
			return "Enter opens Setup."
		case "needs_llm_key":
			return "Enter jumps to the LLM key field."
		case "needs_redeem":
			return "Enter opens Service Redeem."
		case "mcp_optional":
			return "Enter opens Tools/MCP templates."
		case "official_ready":
			return "Enter opens Service Redeem details."
		case "llm_ready":
			return "Enter jumps to the LLM tab."
		}
		return ""
	}
	switch status {
	case "needs_setup":
		return "Enter 打开初始化。"
	case "needs_llm_key":
		return "Enter 跳到 LLM 密钥。"
	case "needs_redeem":
		return "Enter 打开服务兑换。"
	case "mcp_optional":
		return "Enter 打开工具/MCP 模板。"
	case "official_ready":
		return "Enter 打开服务兑换详情。"
	case "llm_ready":
		return "Enter 跳到 LLM 标签。"
	}
	return ""
}

func configSetupChecklistRows(cfg corelib.AppConfig, lang string) []string {
	isEN := i18n.NormalizeLang(lang) == "en"
	llmReady := configLLMReady(&cfg)
	hubReady := strings.TrimSpace(cfg.RemoteHubURL) != "" && strings.TrimSpace(cfg.RemoteViewerToken) != ""
	remoteMachineReady := strings.TrimSpace(cfg.RemoteMachineID) != "" && strings.TrimSpace(cfg.RemoteMachineToken) != ""
	officialReady := serviceRedeemUsesOfficialLLM(cfg) && llmReady
	llmNeedsKey := configLLMNeedsKey(&cfg)
	mcpCount := len(cfg.MCPServers) + len(cfg.LocalMCPServers)
	llmEndpointSet := strings.TrimSpace(cfg.MaclawLLMUrl) != "" && strings.TrimSpace(cfg.MaclawLLMModel) != ""
	setupReady := cfg.OnboardingDone || hubReady || llmReady || llmEndpointSet

	llmName := strings.TrimSpace(cfg.MaclawLLMModel)
	if officialReady {
		llmName = serviceRedeemProviderDisplayName(lang, serviceRedeemOfficialProviderName)
	} else if provider := strings.TrimSpace(cfg.MaclawLLMCurrentProvider); provider != "" && provider != "Custom" {
		llmName = serviceRedeemProviderDisplayName(lang, provider)
	}

	mark := func(ok bool) string {
		if ok {
			return "[x]"
		}
		return "[ ]"
	}
	setupLabel := configSetupLabel(lang, "setup")
	hubLabel := configSetupLabel(lang, "hub")
	machineLabel := configSetupLabel(lang, "machine")
	officialLabel := configSetupLabel(lang, "official")
	llmLabel := configSetupLabel(lang, "llm")
	mcpLabel := configSetupLabel(lang, "mcp")

	if isEN {
		setupText := "open guided Setup"
		switch {
		case cfg.OnboardingDone:
			setupText = "guided setup complete"
		case hubReady:
			setupText = "Hub activation complete"
		case llmNeedsKey:
			setupText = "LLM provider selected; key needed"
		case llmReady:
			setupText = "LLM path configured"
		}
		hubText := "email + HubCenter activation needed"
		if hubReady {
			hubText = "activated; Hub auto-selected"
		} else if strings.TrimSpace(cfg.RemoteHubCenterURL) != "" || strings.TrimSpace(cfg.RemoteEmail) != "" {
			hubText = "HubCenter/email saved; activate Hub"
		}
		machineText := "optional for remote tasks"
		if remoteMachineReady {
			machineText = "activated for remote tasks"
		} else if hubReady {
			machineText = "not required for service redeem"
		}
		officialText := "redeem code optional"
		if officialReady {
			officialText = "active; default LLM is MaClaw Official"
		} else if hubReady {
			officialText = "ready to redeem"
		}
		llmText := "choose provider or redeem service"
		if llmNeedsKey {
			llmText = "API key required for selected provider"
		}
		if llmReady {
			if llmName == "" {
				llmName = "configured"
			}
			llmText = llmName
		}
		mcpText := "optional templates available"
		if mcpCount > 0 {
			mcpText = fmt.Sprintf("%d configured", mcpCount)
		}
		return []string{
			fmt.Sprintf("%s %s: %s", mark(setupReady), setupLabel, setupText),
			fmt.Sprintf("%s %s: %s", mark(hubReady), hubLabel, hubText),
			fmt.Sprintf("%s %s: %s", mark(remoteMachineReady), machineLabel, machineText),
			fmt.Sprintf("%s %s: %s", mark(officialReady), officialLabel, officialText),
			fmt.Sprintf("%s %s: %s", mark(llmReady), llmLabel, llmText),
			fmt.Sprintf("%s %s: %s", mark(mcpCount > 0), mcpLabel, mcpText),
		}
	}

	setupText := "打开引导式初始化"
	switch {
	case cfg.OnboardingDone:
		setupText = "引导已完成"
	case hubReady:
		setupText = "Hub 激活已完成"
	case llmNeedsKey:
		setupText = "已选择 LLM 服务商；还需密钥"
	case llmReady:
		setupText = "LLM 路径已配置"
	}
	hubText := "需要邮箱 + HubCenter 激活"
	if hubReady {
		hubText = "已激活；Hub 已自动选择"
	} else if strings.TrimSpace(cfg.RemoteHubCenterURL) != "" || strings.TrimSpace(cfg.RemoteEmail) != "" {
		hubText = "已保存 HubCenter/邮箱；继续激活"
	}
	machineText := "远程任务可选"
	if remoteMachineReady {
		machineText = "远程任务已激活"
	} else if hubReady {
		machineText = "兑换官方服务不需要"
	}
	officialText := "可选兑换服务码"
	if officialReady {
		officialText = "已启用；默认 LLM 为 MaClaw 官方"
	} else if hubReady {
		officialText = "可直接兑换"
	}
	llmText := "选择服务商或兑换官方服务"
	if llmNeedsKey {
		llmText = "当前服务商需要填写密钥"
	}
	if llmReady {
		if llmName == "" {
			llmName = "已配置"
		}
		llmText = llmName
	}
	mcpText := "可选使用模板添加"
	if mcpCount > 0 {
		mcpText = fmt.Sprintf("已配置 %d 个", mcpCount)
	}
	return []string{
		fmt.Sprintf("%s 初始化: %s", mark(setupReady), setupText),
		fmt.Sprintf("%s Hub: %s", mark(hubReady), hubText),
		fmt.Sprintf("%s 远程机器: %s", mark(remoteMachineReady), machineText),
		fmt.Sprintf("%s 官方服务: %s", mark(officialReady), officialText),
		fmt.Sprintf("%s LLM: %s", mark(llmReady), llmText),
		fmt.Sprintf("%s MCP: %s", mark(mcpCount > 0), mcpText),
	}
}

func configSetupCompactChecklistRows(cfg corelib.AppConfig, lang string, width int) []string {
	isEN := i18n.NormalizeLang(lang) == "en"
	llmReady := configLLMReady(&cfg)
	hubReady := strings.TrimSpace(cfg.RemoteHubURL) != "" && strings.TrimSpace(cfg.RemoteViewerToken) != ""
	remoteMachineReady := strings.TrimSpace(cfg.RemoteMachineID) != "" && strings.TrimSpace(cfg.RemoteMachineToken) != ""
	officialReady := serviceRedeemUsesOfficialLLM(cfg) && llmReady
	llmEndpointSet := strings.TrimSpace(cfg.MaclawLLMUrl) != "" && strings.TrimSpace(cfg.MaclawLLMModel) != ""
	setupReady := cfg.OnboardingDone || hubReady || llmReady || llmEndpointSet
	mcpReady := len(cfg.MCPServers)+len(cfg.LocalMCPServers) > 0

	mark := func(ok bool) string {
		if ok {
			return "[x]"
		}
		return "[ ]"
	}
	labels := []string{"Setup", "Hub", "Machine", "Official", "LLM", "MCP"}
	if !isEN {
		labels = []string{"初始化", "Hub", "机器", "官方", "LLM", "MCP"}
	}
	items := []string{
		labels[0] + ":" + mark(setupReady),
		labels[1] + ":" + mark(hubReady),
		labels[2] + ":" + mark(remoteMachineReady),
		labels[3] + ":" + mark(officialReady),
		labels[4] + ":" + mark(llmReady),
		labels[5] + ":" + mark(mcpReady),
	}

	prefix := "Status: "
	if !isEN {
		prefix = "状态: "
	}
	first := prefix + items[0] + "  " + items[1] + "  " + items[2]
	second := strings.Repeat(" ", lipgloss.Width(prefix)) + items[3] + "  " + items[4] + "  " + items[5]
	return []string{
		fitDisplay(first, max(12, width-4)),
		fitDisplay(second, max(12, width-4)),
	}
}

func configSetupLabel(lang, key string) string {
	if i18n.NormalizeLang(lang) == "en" {
		switch key {
		case "setup":
			return "Setup"
		case "hub":
			return "Hub"
		case "machine":
			return "Remote machine"
		case "official":
			return "Official service"
		case "llm":
			return "LLM"
		case "mcp":
			return "MCP"
		}
		return key
	}
	switch key {
	case "setup":
		return "鍒濆鍖?"
	case "hub":
		return "Hub"
	case "machine":
		return "杩滅▼鏈哄櫒"
	case "official":
		return "瀹樻柟鏈嶅姟"
	case "llm":
		return "LLM"
	case "mcp":
		return "MCP"
	}
	return key
}

func configEntryActionLabel(e ConfigEntry, cfg corelib.AppConfig, lang string) string {
	if e.Key == "setup_status" {
		switch currentSetupStatus(&cfg) {
		case "needs_setup":
			if i18n.NormalizeLang(lang) == "en" {
				return "Enter->Setup"
			}
			return "Enter->初始化"
		case "needs_llm_key":
			return "Enter->LLM"
		case "needs_redeem", "official_ready":
			if i18n.NormalizeLang(lang) == "en" {
				return "Enter->Redeem"
			}
			return "Enter->兑换"
		case "mcp_optional":
			if i18n.NormalizeLang(lang) == "en" {
				return "Enter->Tools"
			}
			return "Enter->工具"
		case "llm_ready":
			return "Enter->LLM"
		}
	}
	if configReadOnlyOpensSetup(e.Key) {
		if i18n.NormalizeLang(lang) == "en" {
			return "Enter->Setup"
		}
		return "Enter->初始化"
	}
	return ""
}

func configValueWithAction(value, action string, width int) string {
	suffix := " [" + action + "]"
	suffixWidth := lipgloss.Width(suffix)
	if width <= suffixWidth+4 {
		return fitDisplay("["+action+"]", width)
	}
	return fitDisplay(value, width-suffixWidth) + suffix
}

func configReadOnlyActionHint(key, lang string) string {
	if i18n.NormalizeLang(lang) == "en" {
		switch key {
		case "hub_url":
			return "Enter opens Setup; Hub is selected automatically from HubCenter and email."
		case "token":
			return "Enter opens Setup to activate Hub and refresh this token."
		case "weixin_token":
			return "Enter opens Setup to bind WeChat by QR code."
		}
		return ""
	}
	switch key {
	case "hub_url":
		return "Enter 打开初始化；Hub 会根据 HubCenter 和邮箱自动选择。"
	case "token":
		return "Enter 打开初始化，激活 Hub 并刷新该令牌。"
	case "weixin_token":
		return "Enter 打开初始化，通过二维码绑定微信。"
	}
	return ""
}

func configReadOnlyOpensSetup(key string) bool {
	switch key {
	case "hub_url", "token", "weixin_token":
		return true
	default:
		return false
	}
}

func (m ConfigModel) renderDetails(e ConfigEntry) string {
	var b strings.Builder
	name := configDisplayNameForLang(e.Key, m.lang)
	b.WriteString(cfgSectionStyle.Render("  "+name) + "\n")
	if e.ReadOnly {
		label := "read-only"
		if m.lang != "en" {
			label = "只读"
		}
		b.WriteString("  " + cfgReadOnly.Render(label) + "\n")
		if strings.TrimSpace(e.Value) != "" {
			value := e.Value
			if isSensitiveKey(e.Key) {
				value = "********"
			} else if len(e.optionValues()) > 0 {
				value = configOptionDisplay(e.Key, value, m.lang)
			}
			prefix := "current: "
			if m.lang != "en" {
				prefix = "当前值："
			}
			b.WriteString("  " + cfgDimStyle.Render(prefix+fitDisplay(value, max(12, m.width-lipgloss.Width(prefix)-4))) + "\n")
		}
	}
	if e.Desc != "" {
		b.WriteString("  " + cfgDimStyle.Render(fitDisplay(e.Desc, max(20, m.width-4))) + "\n")
	}
	if e.Key == "setup_status" {
		if hint := configSetupActionHint(currentSetupStatus(&m.cfg), m.lang); hint != "" {
			b.WriteString("  " + cfgDimStyle.Render(hint) + "\n")
		}
		for _, row := range configSetupChecklistRows(m.cfg, m.lang) {
			b.WriteString("  " + cfgDimStyle.Render(fitDisplay(row, max(20, m.width-4))) + "\n")
		}
	}
	if hint := configReadOnlyActionHint(e.Key, m.lang); hint != "" {
		b.WriteString("  " + cfgDimStyle.Render(hint) + "\n")
	}
	showInternal := strings.EqualFold(strings.TrimSpace(m.cfg.UIMode), "pro")
	detailRows := make([]string, 0, 4)
	if showInternal {
		detailRows = append(detailRows, "key: "+e.Key)
	}
	if options := e.optionValues(); len(options) > 0 {
		labels := make([]string, 0, len(options))
		for _, opt := range options {
			labels = append(labels, configOptionDisplay(e.Key, opt, m.lang))
		}
		prefix := "options: "
		if m.lang != "en" {
			prefix = "选项："
		}
		detailRows = append(detailRows, prefix+fitDisplay(strings.Join(labels, " | "), max(20, m.width-lipgloss.Width(prefix)-4)))
		if !e.ReadOnly {
			if m.lang == "en" {
				detailRows = append(detailRows, "tip: Space cycles and saves; Enter opens the selector.")
			} else {
				detailRows = append(detailRows, "提示：Space 直接切换并保存，Enter 打开选择列表。")
			}
		}
	} else if suggestions := e.suggestionValues(); len(suggestions) > 0 {
		prefix := "suggestions: "
		if m.lang != "en" {
			prefix = "建议："
		}
		detailRows = append(detailRows, prefix+fitDisplay(strings.Join(suggestions, " | "), max(20, m.width-lipgloss.Width(prefix)-4)))
		if m.lang == "en" {
			detailRows = append(detailRows, "tip: Enter opens choices; Space uses the next suggestion and saves.")
		} else {
			detailRows = append(detailRows, "提示：Enter 打开选择，Space 使用下一个建议并保存。")
		}
	}
	for _, row := range detailRows {
		b.WriteString("  " + cfgDimStyle.Render(fitDisplay(row, max(20, m.width-4))) + "\n")
	}
	return b.String()
}

// renderTabs renders the config sub-tab bar.
func (m ConfigModel) renderTabs() string {
	names := cfgTabNames(m.lang)
	active := cfgTabActive
	inactive := cfgTabInactive
	if m.width < 64 {
		active = active.Padding(0, 0)
		inactive = inactive.Padding(0, 0)
	}
	var tabs strings.Builder
	for i, name := range names {
		label := configTabLabel(i, name, m.width, m.lang)
		if i == m.activeTab {
			tabs.WriteString(active.Render(label))
		} else {
			tabs.WriteString(inactive.Render(label))
		}
		if m.width >= 64 {
			tabs.WriteByte(' ')
		}
	}
	return "  " + tabs.String()
}

func configTabLabel(idx int, name string, width int, lang string) string {
	if width >= 72 {
		return fmt.Sprintf("%d:%s", idx+1, name)
	}
	if i18n.NormalizeLang(lang) == "en" {
		labels := [CfgTabCount]string{"Gen", "LLM", "IM", "Prx", "Sec", "Adv"}
		return fmt.Sprintf("%d:%s", idx+1, labels[idx])
	}
	labels := [CfgTabCount]string{"基", "LLM", "IM", "代", "安", "高"}
	return fmt.Sprintf("%d:%s", idx+1, labels[idx])
}

// sectionLabel returns a human-readable section header.
func sectionLabel(section, lang string) string {
	if i18n.NormalizeLang(lang) == "en" {
		labels := map[string]string{
			"general":     "General",
			"maclaw_llm":  "Primary LLM",
			"im_overview": "IM Overview",
			"aux_llm":     "Auxiliary LLM",
			"qqbot":       "QQ Bot",
			"telegram":    "Telegram Bot",
			"weixin":      "WeChat",
			"lansenger":   "Lansenger",
			"proxy":       "Proxy",
			"security":    "Security",
			"skillmarket": "Skill Market",
			"advanced":    "Advanced",
		}
		return labels[section]
	}
	labels := map[string]string{
		"general":     "基本设置",
		"maclaw_llm":  "主 LLM",
		"im_overview": "IM 总览",
		"aux_llm":     "辅助 LLM",
		"qqbot":       "QQ 机器人",
		"telegram":    "Telegram 机器人",
		"weixin":      "微信",
		"lansenger":   "蓝信",
		"proxy":       "代理设置",
		"security":    "安全策略",
		"skillmarket": "技能市场",
		"advanced":    "高级选项",
	}
	return labels[section]
}
