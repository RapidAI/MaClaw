package views

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// isSensitiveKey checks whether a config key is sensitive.
func isSensitiveKey(key string) bool {
	return strings.Contains(key, "token") || strings.Contains(key, "secret") ||
		strings.Contains(key, "password") || strings.Contains(key, "_key")
}

// configDisplayName returns a human-readable bilingual label for a config key.
// The saved key remains unchanged; this is display-only.
func configDisplayName(key string) string {
	return configDisplayNameForLang(key, "zh")
}

func configDisplayNameForLang(key, lang string) string {
	zh := map[string]string{
		"hub_url": "Hub 服务地址", "token": "认证令牌", "data_dir": "数据目录", "working_directory": "工作目录", "language": "界面语言", "max_iterations": "最大轮数", "agentnet_enabled": "AgentNet 开关", "check_update_on_startup": "启动检查更新",
		"maclaw_llm_provider_preset": "LLM 服务商", "maclaw_llm_url": "LLM 地址", "maclaw_llm_key": "LLM 密钥", "maclaw_llm_model": "LLM 模型", "maclaw_llm_protocol": "LLM 协议", "maclaw_llm_context_length": "上下文长度",
		"aux_llm_url": "辅助 LLM 地址", "aux_llm_key": "辅助 LLM 密钥", "aux_llm_model": "辅助 LLM 模型", "aux_llm_protocol": "辅助 LLM 协议",
		"qqbot_enabled": "QQ 机器人开关", "qqbot_app_id": "QQ AppID", "qqbot_app_secret": "QQ AppSecret", "telegram_bot_enabled": "Telegram 开关", "telegram_bot_token": "Telegram Token", "weixin_enabled": "微信开关", "weixin_token": "微信 Token", "weixin_base_url": "微信地址", "lansenger_enabled": "蓝信开关", "lansenger_app_id": "蓝信 AppID", "lansenger_app_secret": "蓝信密钥", "lansenger_gateway_url": "蓝信网关",
		"default_proxy_enabled": "代理开关", "default_proxy_protocol": "代理协议", "default_proxy_host": "代理主机", "default_proxy_port": "代理端口", "default_proxy_username": "代理用户", "default_proxy_password": "代理密码", "default_proxy_scope_maclaw": "代理 LLM", "default_proxy_scope_agent": "代理 Agent",
		"security_policy_mode": "安全策略", "sandbox_mode": "沙箱模式", "network_level": "网络级别", "yolo_mode_allowed": "YOLO 模式", "file_outbound_enabled": "文件外发", "image_outbound_enabled": "图片外发",
		"skill_purchase_mode": "技能购买", "ui_mode": "界面模式", "memory_auto_compress": "记忆压缩", "log_detail_enabled": "详细日志", "llm_trajectory_logging": "LLM 轨迹", "maclaw_debug_tool_calls": "工具调试", "gossip_enabled": "Gossip 开关", "trial_reflect_enabled": "试错反思",
	}
	en := map[string]string{
		"hub_url": "Hub URL", "token": "Auth token", "data_dir": "Data dir", "working_directory": "Work dir", "language": "Language", "max_iterations": "Max rounds", "agentnet_enabled": "AgentNet", "check_update_on_startup": "Check update",
		"maclaw_llm_provider_preset": "LLM provider", "maclaw_llm_url": "LLM URL", "maclaw_llm_key": "LLM key", "maclaw_llm_model": "LLM model", "maclaw_llm_protocol": "LLM protocol", "maclaw_llm_context_length": "Context length",
		"aux_llm_url": "Aux LLM URL", "aux_llm_key": "Aux LLM key", "aux_llm_model": "Aux LLM model", "aux_llm_protocol": "Aux LLM protocol",
		"qqbot_enabled": "QQ bot", "qqbot_app_id": "QQ AppID", "qqbot_app_secret": "QQ AppSecret", "telegram_bot_enabled": "Telegram", "telegram_bot_token": "Telegram token", "weixin_enabled": "WeChat", "weixin_token": "WeChat token", "weixin_base_url": "WeChat URL", "lansenger_enabled": "Lansenger", "lansenger_app_id": "Lansenger ID", "lansenger_app_secret": "Lansenger secret", "lansenger_gateway_url": "Lansenger gateway",
		"default_proxy_enabled": "Proxy", "default_proxy_protocol": "Proxy protocol", "default_proxy_host": "Proxy host", "default_proxy_port": "Proxy port", "default_proxy_username": "Proxy user", "default_proxy_password": "Proxy password", "default_proxy_scope_maclaw": "Proxy LLM", "default_proxy_scope_agent": "Proxy Agent",
		"security_policy_mode": "Security mode", "sandbox_mode": "Sandbox", "network_level": "Network level", "yolo_mode_allowed": "YOLO mode", "file_outbound_enabled": "File outbound", "image_outbound_enabled": "Image outbound",
		"skill_purchase_mode": "Skill purchase", "ui_mode": "UI mode", "memory_auto_compress": "Memory compress", "log_detail_enabled": "Detail logs", "llm_trajectory_logging": "LLM trajectory", "maclaw_debug_tool_calls": "Debug tools", "gossip_enabled": "Gossip", "trial_reflect_enabled": "Trial reflect",
	}
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
	if width < 88 {
		return 26
	}
	if width < 120 {
		return 32
	}
	return 38
}

func padDisplay(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s + " "
	}
	return s + strings.Repeat(" ", width-w)
}

func configOptionDisplay(key, value, lang string) string {
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
	return value
}

// ConfigEntry represents a configuration entry for display.
// When Options is non-nil, the entry uses an inline selector instead of free text input.
type ConfigEntry struct {
	Key      string
	Value    string
	Desc     string
	Section  string
	Options  []string // nil = free text input; non-nil = inline selector
	ReadOnly bool     // true = display only, cannot edit
}

// ConfigSaveMsg is a config save message, persisted by the outer layer (app.go).
type ConfigSaveMsg struct {
	Section string
	Key     string
	Value   string
}

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
	tabs         [CfgTabCount][]ConfigEntry
	activeTab    int
	cursor       int
	editing      bool
	selectMode   bool // true = inline selector active (Options field)
	selectCursor int  // cursor within Options list
	input        textinput.Model
	lang         string
	width        int // terminal width for rendering
}

// IsEditing returns whether the view is in editing mode.
func (m ConfigModel) IsEditing() bool {
	return m.editing || m.selectMode
}

// currentEntries returns entries for the active tab.
func (m ConfigModel) currentEntries() []ConfigEntry {
	if m.activeTab >= 0 && m.activeTab < CfgTabCount {
		return m.tabs[m.activeTab]
	}
	return nil
}

// NewConfigModel creates a new config view with tabbed layout.
// All entries are derived from the single source of truth (allConfigFields).
func NewConfigModel(lang string) ConfigModel {
	lang = i18n.NormalizeLang(lang)
	ti := textinput.New()
	ti.CharLimit = 256
	ti.Width = 50

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

// SetLang updates i18n descriptions by rebuilding entries and preserving values.
func (m *ConfigModel) SetLang(lang string) {
	m.lang = i18n.NormalizeLang(lang)
	// Save current values.
	valMap := m.collectValues()
	// Rebuild with new lang.
	m.rebuildFromDefs()
	// Restore values.
	m.applyValues(valMap)
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

// FocusLLMConfig switches to the LLM tab and moves cursor to the first field.
func (m *ConfigModel) FocusLLMConfig() {
	m.activeTab = CfgTabLLM
	m.cursor = 0
}

// LoadFromAppConfig syncs config values from AppConfig to the view.
// Uses the Get accessor from each ConfigFieldDef — no manual key mapping needed.
func (m *ConfigModel) LoadFromAppConfig(cfg corelib.AppConfig) {
	for _, def := range allConfigFields {
		if def.Get == nil {
			continue
		}
		val := def.Get(&cfg)
		m.setEntryValue(def.Key, val)
	}
}

// Init implements tea.Model.
func (m ConfigModel) Init() tea.Cmd { return nil }

// Update handles keyboard events.
func (m ConfigModel) Update(msg tea.Msg) (ConfigModel, tea.Cmd) {
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = msg.Width
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
			m.activeTab = CfgTabGeneral
			m.cursor = 0
		case "2":
			m.activeTab = CfgTabLLM
			m.cursor = 0
		case "3":
			m.activeTab = CfgTabIM
			m.cursor = 0
		case "4":
			m.activeTab = CfgTabProxy
			m.cursor = 0
		case "5":
			m.activeTab = CfgTabSecurity
			m.cursor = 0
		case "6":
			m.activeTab = CfgTabAdvanced
			m.cursor = 0
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(entries)-1 {
				m.cursor++
			}
		case "enter":
			if m.cursor >= len(entries) {
				return m, nil
			}
			e := entries[m.cursor]
			if e.ReadOnly {
				return m, nil
			}
			// Options field → inline selector; otherwise → text input.
			if len(e.Options) > 0 {
				m.selectMode = true
				m.selectCursor = 0
				for i, opt := range e.Options {
					if opt == e.Value {
						m.selectCursor = i
						break
					}
				}
				return m, nil
			}
			m.editing = true
			m.input.SetValue(e.Value)
			m.input.Focus()
			m.input.CursorEnd()
			return m, textinput.Blink
		case " ":
			// Space on a boolean field toggles it directly.
			if m.cursor < len(entries) {
				e := entries[m.cursor]
				if !e.ReadOnly && len(e.Options) == 2 && e.Options[0] == "true" && e.Options[1] == "false" {
					newVal := "true"
					if e.Value == "true" {
						newVal = "false"
					}
					m.tabs[m.activeTab][m.cursor].Value = newVal
					return m, func() tea.Msg {
						return ConfigSaveMsg{Section: e.Section, Key: e.Key, Value: newVal}
					}
				}
			}
		}
	}
	return m, nil
}

// updateSelect handles keys in inline selector mode.
func (m ConfigModel) updateSelect(msg tea.Msg) (ConfigModel, tea.Cmd) {
	entries := m.currentEntries()
	if m.cursor >= len(entries) {
		m.selectMode = false
		return m, nil
	}
	e := entries[m.cursor]
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "left", "h":
			if m.selectCursor > 0 {
				m.selectCursor--
			}
		case "right", "l":
			if m.selectCursor < len(e.Options)-1 {
				m.selectCursor++
			}
		case "up", "k":
			if m.selectCursor > 0 {
				m.selectCursor--
			}
		case "down", "j":
			if m.selectCursor < len(e.Options)-1 {
				m.selectCursor++
			}
		case "enter":
			newVal := e.Options[m.selectCursor]
			m.tabs[m.activeTab][m.cursor].Value = newVal
			m.selectMode = false
			return m, func() tea.Msg {
				return ConfigSaveMsg{Section: e.Section, Key: e.Key, Value: newVal}
			}
		case "esc":
			m.selectMode = false
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
				m.editing = false
				m.input.Blur()
				return m, nil
			}
			newVal := m.input.Value()
			e := entries[m.cursor]
			m.tabs[m.activeTab][m.cursor].Value = newVal
			m.editing = false
			m.input.Blur()
			return m, func() tea.Msg {
				return ConfigSaveMsg{Section: e.Section, Key: e.Key, Value: newVal}
			}
		case "esc":
			m.editing = false
			m.input.Blur()
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

	// Title
	b.WriteString(cfgHeaderStyle.Render("  "+i18n.T(i18n.MsgTUIConfigTitle, m.lang)) + "\n")

	// Tab bar
	b.WriteString(m.renderTabs())
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat("─", min(60, m.width-4)) + "\n")

	entries := m.currentEntries()
	prevSection := ""

	for i, e := range entries {
		// Section separator
		if e.Section != prevSection {
			prevSection = e.Section
			if i > 0 {
				b.WriteString("\n")
			}
			label := sectionLabel(e.Section, m.lang)
			if label != "" {
				b.WriteString(cfgSectionStyle.Render("  ▸ "+label) + "\n")
			}
		}

		// Inline selector mode for this row.
		name := configDisplayNameForLang(e.Key, m.lang)
		nameWidth := configNameWidth(m.width)

		if m.selectMode && i == m.cursor {
			line := "  " + padDisplay(name, nameWidth)
			b.WriteString(cfgEditStyle.Render(line))
			for j, opt := range e.Options {
				if j > 0 {
					b.WriteString("  ")
				}
				label := configOptionDisplay(e.Key, opt, m.lang)
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
			line := "  " + padDisplay(name, nameWidth)
			b.WriteString(cfgEditStyle.Render(line))
			b.WriteString(m.input.View())
			b.WriteString("\n")
			continue
		}

		// Normal display.
		val := e.Value
		isBoolField := len(e.Options) == 2 && e.Options[0] == "true" && e.Options[1] == "false"

		if isBoolField {
			if val == "true" {
				val = cfgToggleOn.Render("● ON")
			} else {
				val = cfgToggleOff.Render("○ OFF")
			}
		} else if len(e.Options) > 0 {
			val = configOptionDisplay(e.Key, val, m.lang)
		} else if val == "" {
			val = cfgDimStyle.Render(i18n.T(i18n.MsgTUIConfigNotSet, m.lang))
		} else if isSensitiveKey(e.Key) {
			val = "********"
		}

		descStr := cfgDimStyle.Render(e.Desc)
		if e.ReadOnly {
			descStr = cfgReadOnly.Render("(只读) " + e.Desc)
		}

		// Show options hint for non-boolean selector fields.
		optHint := ""
		if len(e.Options) > 0 && !isBoolField {
			labels := make([]string, 0, len(e.Options))
			for _, opt := range e.Options {
				labels = append(labels, configOptionDisplay(e.Key, opt, m.lang))
			}
			optHint = cfgDimStyle.Render(" [" + strings.Join(labels, "|") + "]")
		}

		line := fmt.Sprintf("  %s %-16s%s  %s", padDisplay(name, nameWidth), val, optHint, descStr)
		if i == m.cursor {
			b.WriteString(cfgSelectedStyle.Render(line))
		} else {
			b.WriteString(cfgNormalStyle.Render(line))
		}
		b.WriteString("\n")
	}

	// Footer
	b.WriteString("\n")
	if m.selectMode {
		b.WriteString("  " + cfgDimStyle.Render(i18n.T(i18n.MsgTUIConfigFooterSelect, m.lang)))
	} else if m.editing {
		b.WriteString("  " + cfgDimStyle.Render(i18n.T(i18n.MsgTUIConfigFooterEditing, m.lang)))
	} else {
		b.WriteString("  " + cfgDimStyle.Render(i18n.T(i18n.MsgTUIConfigFooterNormal, m.lang)))
	}
	return b.String()
}

// renderTabs renders the config sub-tab bar.
func (m ConfigModel) renderTabs() string {
	names := cfgTabNames(m.lang)
	var tabs string
	for i, name := range names {
		label := fmt.Sprintf("%d:%s", i+1, name)
		if i == m.activeTab {
			tabs += cfgTabActive.Render(label)
		} else {
			tabs += cfgTabInactive.Render(label)
		}
		tabs += " "
	}
	return "  " + tabs
}

// sectionLabel returns a human-readable section header.
func sectionLabel(section, lang string) string {
	if i18n.NormalizeLang(lang) == "en" {
		labels := map[string]string{
			"general":     "General",
			"maclaw_llm":  "Primary LLM",
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
