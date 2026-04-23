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

// ConfigEntry represents a configuration entry.
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

// ConfigModel is the configuration management view.
type ConfigModel struct {
	entries      []ConfigEntry
	cursor       int
	editing      bool
	selectMode   bool // true = inline selector active (Options field)
	selectCursor int  // cursor within Options list
	input        textinput.Model
	lang         string
}

// IsEditing returns whether the view is in editing mode.
func (m ConfigModel) IsEditing() bool {
	return m.editing || m.selectMode
}

// NewConfigModel creates a new config view.
func NewConfigModel(lang string) ConfigModel {
	lang = i18n.NormalizeLang(lang)
	ti := textinput.New()
	ti.CharLimit = 256
	ti.Width = 40

	boolOpts := []string{"true", "false"}

	return ConfigModel{
		entries: []ConfigEntry{
			{Key: "hub_url", Value: "", Desc: i18n.T(i18n.MsgTUIConfigDescHubURL, lang), Section: "general"},
			{Key: "token", Value: "", Desc: i18n.T(i18n.MsgTUIConfigDescToken, lang), Section: "general"},
			{Key: "data_dir", Value: "", Desc: i18n.T(i18n.MsgTUIConfigDescDataDir, lang), Section: "general", ReadOnly: true},
			{Key: "max_iterations", Value: "300", Desc: i18n.T(i18n.MsgTUIConfigDescMaxIterations, lang), Section: "general"},
			{Key: "agentnet_enabled", Value: "false", Desc: i18n.T(i18n.MsgTUIConfigDescAgentNetEnabled, lang), Section: "general", Options: boolOpts},
			// LLM config
			{Key: "maclaw_llm_url", Value: "", Desc: i18n.T(i18n.MsgTUIConfigDescLLMURL, lang), Section: "maclaw_llm"},
			{Key: "maclaw_llm_key", Value: "", Desc: i18n.T(i18n.MsgTUIConfigDescLLMKey, lang), Section: "maclaw_llm"},
			{Key: "maclaw_llm_model", Value: "", Desc: i18n.T(i18n.MsgTUIConfigDescLLMModel, lang), Section: "maclaw_llm"},
			{Key: "maclaw_llm_protocol", Value: "openai", Desc: i18n.T(i18n.MsgTUIConfigDescLLMProtocol, lang), Section: "maclaw_llm",
				Options: []string{"openai", "anthropic"}},
			{Key: "maclaw_llm_context_length", Value: "", Desc: i18n.T(i18n.MsgTUIConfigDescLLMContextLength, lang), Section: "maclaw_llm"},
			// IM config
			{Key: "qqbot_enabled", Value: "false", Desc: i18n.T(i18n.MsgTUIConfigDescQQBotEnabled, lang), Section: "qqbot", Options: boolOpts},
			{Key: "qqbot_app_id", Value: "", Desc: i18n.T(i18n.MsgTUIConfigDescQQBotAppID, lang), Section: "qqbot"},
			{Key: "qqbot_app_secret", Value: "", Desc: i18n.T(i18n.MsgTUIConfigDescQQBotAppSecret, lang), Section: "qqbot"},
			{Key: "telegram_bot_enabled", Value: "false", Desc: i18n.T(i18n.MsgTUIConfigDescTelegramEnabled, lang), Section: "telegram", Options: boolOpts},
			{Key: "telegram_bot_token", Value: "", Desc: i18n.T(i18n.MsgTUIConfigDescTelegramToken, lang), Section: "telegram"},
			{Key: "skill_purchase_mode", Value: "auto", Desc: i18n.T(i18n.MsgTUIConfigDescSkillPurchaseMode, lang), Section: "skillmarket",
				Options: []string{"auto", "free_only"}},
		},
		input: ti,
		lang:  lang,
	}
}

// SetLang updates i18n descriptions.
func (m *ConfigModel) SetLang(lang string) {
	m.lang = i18n.NormalizeLang(lang)
	for i, e := range m.entries {
		switch e.Key {
		case "hub_url":
			m.entries[i].Desc = i18n.T(i18n.MsgTUIConfigDescHubURL, m.lang)
		case "token":
			m.entries[i].Desc = i18n.T(i18n.MsgTUIConfigDescToken, m.lang)
		case "data_dir":
			m.entries[i].Desc = i18n.T(i18n.MsgTUIConfigDescDataDir, m.lang)
		case "max_iterations":
			m.entries[i].Desc = i18n.T(i18n.MsgTUIConfigDescMaxIterations, m.lang)
		case "agentnet_enabled":
			m.entries[i].Desc = i18n.T(i18n.MsgTUIConfigDescAgentNetEnabled, m.lang)
		case "maclaw_llm_url":
			m.entries[i].Desc = i18n.T(i18n.MsgTUIConfigDescLLMURL, m.lang)
		case "maclaw_llm_key":
			m.entries[i].Desc = i18n.T(i18n.MsgTUIConfigDescLLMKey, m.lang)
		case "maclaw_llm_model":
			m.entries[i].Desc = i18n.T(i18n.MsgTUIConfigDescLLMModel, m.lang)
		case "maclaw_llm_protocol":
			m.entries[i].Desc = i18n.T(i18n.MsgTUIConfigDescLLMProtocol, m.lang)
		case "maclaw_llm_context_length":
			m.entries[i].Desc = i18n.T(i18n.MsgTUIConfigDescLLMContextLength, m.lang)
		case "qqbot_enabled":
			m.entries[i].Desc = i18n.T(i18n.MsgTUIConfigDescQQBotEnabled, m.lang)
		case "qqbot_app_id":
			m.entries[i].Desc = i18n.T(i18n.MsgTUIConfigDescQQBotAppID, m.lang)
		case "qqbot_app_secret":
			m.entries[i].Desc = i18n.T(i18n.MsgTUIConfigDescQQBotAppSecret, m.lang)
		case "telegram_bot_enabled":
			m.entries[i].Desc = i18n.T(i18n.MsgTUIConfigDescTelegramEnabled, m.lang)
		case "telegram_bot_token":
			m.entries[i].Desc = i18n.T(i18n.MsgTUIConfigDescTelegramToken, m.lang)
		case "skill_purchase_mode":
			m.entries[i].Desc = i18n.T(i18n.MsgTUIConfigDescSkillPurchaseMode, m.lang)
		}
	}
}

// SetEntries updates config entries.
func (m *ConfigModel) SetEntries(entries []ConfigEntry) {
	m.entries = entries
	if m.cursor >= len(entries) {
		m.cursor = max(0, len(entries)-1)
	}
}

// FocusLLMConfig moves cursor to the first LLM config field.
func (m *ConfigModel) FocusLLMConfig() {
	for i, e := range m.entries {
		if e.Key == "maclaw_llm_url" {
			m.cursor = i
			return
		}
	}
}

// LoadFromAppConfig syncs config values from AppConfig to the view.
func (m *ConfigModel) LoadFromAppConfig(cfg corelib.AppConfig) {
	valMap := map[string]string{
		"hub_url":                   cfg.RemoteHubURL,
		"token":                     cfg.RemoteMachineToken,
		"data_dir":                  "",
		"max_iterations":            fmt.Sprintf("%d", cfg.MaclawAgentMaxIterations),
		"agentnet_enabled":          fmt.Sprintf("%v", cfg.AgentNetEnabled),
		"maclaw_llm_url":            cfg.MaclawLLMUrl,
		"maclaw_llm_key":            cfg.MaclawLLMKey,
		"maclaw_llm_model":          cfg.MaclawLLMModel,
		"maclaw_llm_protocol":       cfg.MaclawLLMProtocol,
		"maclaw_llm_context_length": fmt.Sprintf("%d", cfg.MaclawLLMContextLength),
		"qqbot_enabled":             fmt.Sprintf("%v", cfg.QQBotEnabled),
		"qqbot_app_id":              cfg.QQBotAppID,
		"qqbot_app_secret":          cfg.QQBotAppSecret,
		"telegram_bot_enabled":      fmt.Sprintf("%v", cfg.TelegramBotEnabled),
		"telegram_bot_token":        cfg.TelegramBotToken,
		"skill_purchase_mode":       cfg.SkillPurchaseMode,
	}
	for i, e := range m.entries {
		if v, ok := valMap[e.Key]; ok {
			if v == "0" && (e.Key == "max_iterations" || e.Key == "maclaw_llm_context_length") {
				v = ""
			}
			m.entries[i].Value = v
		}
	}
}

// Init implements tea.Model.
func (m ConfigModel) Init() tea.Cmd { return nil }

// Update handles keyboard events.
func (m ConfigModel) Update(msg tea.Msg) (ConfigModel, tea.Cmd) {
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
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.entries)-1 {
				m.cursor++
			}
		case "enter":
			if m.cursor >= len(m.entries) {
				return m, nil
			}
			e := m.entries[m.cursor]
			if e.ReadOnly {
				return m, nil
			}
			// Options field → inline selector; otherwise → text input.
			if len(e.Options) > 0 {
				m.selectMode = true
				m.selectCursor = 0
				// Pre-select current value.
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
			if m.cursor < len(m.entries) {
				e := m.entries[m.cursor]
				if !e.ReadOnly && len(e.Options) == 2 && e.Options[0] == "true" && e.Options[1] == "false" {
					newVal := "true"
					if e.Value == "true" {
						newVal = "false"
					}
					m.entries[m.cursor].Value = newVal
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
	e := m.entries[m.cursor]
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
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
			m.entries[m.cursor].Value = newVal
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
			newVal := m.input.Value()
			e := m.entries[m.cursor]
			m.entries[m.cursor].Value = newVal
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

// View renders the config view.
func (m ConfigModel) View() string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	editStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	optActiveStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	optNormalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	readOnlyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)

	var b strings.Builder
	b.WriteString(headerStyle.Render("  " + i18n.T(i18n.MsgTUIConfigTitle, m.lang)))
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat("─", 60) + "\n")

	for i, e := range m.entries {
		// Inline selector mode for this row.
		if m.selectMode && i == m.cursor {
			line := fmt.Sprintf("  %-20s = ", e.Key)
			b.WriteString(editStyle.Render(line))
			for j, opt := range e.Options {
				if j > 0 {
					b.WriteString("  ")
				}
				if j == m.selectCursor {
					b.WriteString(optActiveStyle.Render(" " + opt + " "))
				} else {
					b.WriteString(optNormalStyle.Render(" " + opt + " "))
				}
			}
			b.WriteString("\n")
			continue
		}

		// Text editing mode for this row.
		if m.editing && i == m.cursor {
			line := fmt.Sprintf("  %-20s = ", e.Key)
			b.WriteString(editStyle.Render(line))
			b.WriteString(m.input.View())
			b.WriteString("\n")
			continue
		}

		// Normal display.
		val := e.Value
		if val == "" {
			val = dimStyle.Render(i18n.T(i18n.MsgTUIConfigNotSet, m.lang))
		} else if isSensitiveKey(e.Key) {
			val = "********"
		}

		descStr := dimStyle.Render(e.Desc)
		if e.ReadOnly {
			descStr = readOnlyStyle.Render("(只读) " + e.Desc)
		}

		// Show options hint for selector fields.
		optHint := ""
		if len(e.Options) > 0 {
			optHint = dimStyle.Render(" [" + strings.Join(e.Options, "/") + "]")
		}

		line := fmt.Sprintf("  %-20s = %-20s%s  %s", e.Key, val, optHint, descStr)
		if i == m.cursor {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}

	if m.selectMode {
		b.WriteString("\n  " + dimStyle.Render("↑↓:选择  Enter:确认  Esc:取消"))
	} else if m.editing {
		b.WriteString("\n  " + i18n.T(i18n.MsgTUIConfigFooterEditing, m.lang))
	} else {
		b.WriteString("\n  " + dimStyle.Render("Enter:编辑  Space:切换布尔值  ↑↓:移动"))
	}
	return b.String()
}
