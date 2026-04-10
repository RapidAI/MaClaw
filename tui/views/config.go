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
	switch key {
	case "token", "api_key", "secret", "password":
		return true
	}
	return strings.Contains(key, "token") || strings.Contains(key, "secret") ||
		strings.Contains(key, "password") || strings.Contains(key, "_key")
}

// ConfigEntry represents a configuration entry.
type ConfigEntry struct {
	Key     string
	Value   string
	Desc    string
	Section string
}

// ConfigSaveMsg is a config save message, persisted by the outer layer (app.go).
type ConfigSaveMsg struct {
	Section string
	Key     string
	Value   string
}

// ConfigModel is the configuration management view.
type ConfigModel struct {
	entries []ConfigEntry
	cursor  int
	editing bool
	input   textinput.Model
	lang    string
}

// IsEditing returns whether the view is in editing mode (for outer layer to mask global hotkeys).
func (m ConfigModel) IsEditing() bool {
	return m.editing
}

// NewConfigModel creates a new config view.
func NewConfigModel(lang string) ConfigModel {
	lang = i18n.NormalizeLang(lang)
	ti := textinput.New()
	ti.CharLimit = 256
	ti.Width = 40

	return ConfigModel{
		entries: []ConfigEntry{
			{Key: "hub_url", Value: "", Desc: "Hub server URL", Section: "general"},
			{Key: "token", Value: "", Desc: "auth token", Section: "general"},
			{Key: "data_dir", Value: "", Desc: "data directory", Section: "general"},
			{Key: "max_iterations", Value: "300", Desc: "Agent max iterations (30-300)", Section: "general"},
			{Key: "agentnet_enabled", Value: "false", Desc: "enable AgentNet", Section: "general"},
			// LLM config
			{Key: "maclaw_llm_url", Value: "", Desc: "LLM API URL", Section: "maclaw_llm"},
			{Key: "maclaw_llm_key", Value: "", Desc: "LLM API Key", Section: "maclaw_llm"},
			{Key: "maclaw_llm_model", Value: "", Desc: "LLM model name", Section: "maclaw_llm"},
			{Key: "maclaw_llm_protocol", Value: "openai", Desc: "LLM protocol (openai/anthropic)", Section: "maclaw_llm"},
			{Key: "maclaw_llm_context_length", Value: "", Desc: "context length (tokens)", Section: "maclaw_llm"},
			// IM config
			{Key: "qqbot_enabled", Value: "false", Desc: "enable QQ bot", Section: "qqbot"},
			{Key: "qqbot_app_id", Value: "", Desc: "QQ Bot AppID", Section: "qqbot"},
			{Key: "qqbot_app_secret", Value: "", Desc: "QQ Bot AppSecret", Section: "qqbot"},
			{Key: "telegram_bot_enabled", Value: "false", Desc: "enable Telegram bot", Section: "telegram"},
			{Key: "telegram_bot_token", Value: "", Desc: "Telegram Bot Token", Section: "telegram"},
			{Key: "skill_purchase_mode", Value: "auto", Desc: "Skill purchase mode (auto/free_only)", Section: "skillmarket"},
		},
		input: ti,
		lang:  lang,
	}
}

// SetEntries updates config entries.
func (m *ConfigModel) SetLang(lang string) {
	m.lang = i18n.NormalizeLang(lang)
}

// SetEntries updates config entries.
func (m *ConfigModel) SetEntries(entries []ConfigEntry) {
	m.entries = entries
	if m.cursor >= len(entries) {
		m.cursor = max(0, len(entries)-1)
	}
}

// LoadFromAppConfig syncs config values from AppConfig to the view.
func (m *ConfigModel) LoadFromAppConfig(cfg corelib.AppConfig) {
	valMap := map[string]string{
		"hub_url":                  cfg.RemoteHubURL,
		"token":                    cfg.RemoteMachineToken,
		"data_dir":                 "", // determined at runtime, not from config
		"max_iterations":           fmt.Sprintf("%d", cfg.MaclawAgentMaxIterations),
		"agentnet_enabled":           fmt.Sprintf("%v", cfg.AgentNetEnabled),
		"maclaw_llm_url":           cfg.MaclawLLMUrl,
		"maclaw_llm_key":           cfg.MaclawLLMKey,
		"maclaw_llm_model":         cfg.MaclawLLMModel,
		"maclaw_llm_protocol":      cfg.MaclawLLMProtocol,
		"maclaw_llm_context_length": fmt.Sprintf("%d", cfg.MaclawLLMContextLength),
		"qqbot_enabled":            fmt.Sprintf("%v", cfg.QQBotEnabled),
		"qqbot_app_id":             cfg.QQBotAppID,
		"qqbot_app_secret":         cfg.QQBotAppSecret,
		"telegram_bot_enabled":     fmt.Sprintf("%v", cfg.TelegramBotEnabled),
		"telegram_bot_token":       cfg.TelegramBotToken,
		"skill_purchase_mode":      cfg.SkillPurchaseMode,
	}
	for i, e := range m.entries {
		if v, ok := valMap[e.Key]; ok {
			// clear zero-value display
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
			if len(m.entries) > 0 {
				m.editing = true
				m.input.SetValue(m.entries[m.cursor].Value)
				m.input.Focus()
				m.input.CursorEnd()
				return m, textinput.Blink
			}
		}
	}
	return m, nil
}

// updateEditing handles keys in editing mode.
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
	// delegate to textinput for other keys
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// View renders the config view.
func (m ConfigModel) View() string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57"))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	editStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("212"))

	var b strings.Builder
	b.WriteString(headerStyle.Render("  " + i18n.T(i18n.MsgTUIConfigTitle, m.lang)))
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat("-", 60) + "\n")

	for i, e := range m.entries {
		if m.editing && i == m.cursor {
			// editing row: show textinput
			line := fmt.Sprintf("  %-20s = ", e.Key)
			b.WriteString(editStyle.Render(line))
			b.WriteString(m.input.View())
			b.WriteString("\n")
		} else {
			val := e.Value
			if val == "" {
				val = dimStyle.Render(i18n.T(i18n.MsgTUIConfigNotSet, m.lang))
			} else if isSensitiveKey(e.Key) {
				val = "********"
			}
			line := fmt.Sprintf("  %-20s = %-20s  %s", e.Key, val, dimStyle.Render(e.Desc))
			if i == m.cursor {
				b.WriteString(selectedStyle.Render(line))
			} else {
				b.WriteString(normalStyle.Render(line))
			}
			b.WriteString("\n")
		}
	}

	if m.editing {
		b.WriteString("\n  " + i18n.T(i18n.MsgTUIConfigFooterEditing, m.lang))
	} else {
		b.WriteString("\n  " + i18n.T(i18n.MsgTUIConfigFooterNormal, m.lang))
	}
	return b.String()
}
