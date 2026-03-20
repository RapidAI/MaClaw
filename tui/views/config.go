package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// isSensitiveKey 判断配置项是否为敏感字段。
func isSensitiveKey(key string) bool {
	switch key {
	case "token", "api_key", "secret", "password":
		return true
	}
	return strings.Contains(key, "token") || strings.Contains(key, "secret") || strings.Contains(key, "password")
}

// ConfigEntry 配置项。
type ConfigEntry struct {
	Key     string
	Value   string
	Desc    string
	Section string
}

// ConfigSaveMsg 配置保存消息，由外层（app.go）处理持久化。
type ConfigSaveMsg struct {
	Section string
	Key     string
	Value   string
}

// ConfigModel 配置管理视图。
type ConfigModel struct {
	entries []ConfigEntry
	cursor  int
	editing bool
	input   textinput.Model
}

// IsEditing 返回是否处于编辑模式（供外层屏蔽全局快捷键）。
func (m ConfigModel) IsEditing() bool {
	return m.editing
}

// NewConfigModel 创建配置视图。
func NewConfigModel() ConfigModel {
	ti := textinput.New()
	ti.CharLimit = 256
	ti.Width = 40

	return ConfigModel{
		entries: []ConfigEntry{
			{Key: "hub_url", Value: "", Desc: "Hub 服务器地址", Section: "general"},
			{Key: "token", Value: "", Desc: "认证令牌", Section: "general"},
			{Key: "data_dir", Value: "", Desc: "数据目录", Section: "general"},
			{Key: "max_iterations", Value: "12", Desc: "Agent 最大迭代次数", Section: "general"},
			{Key: "clawnet_enabled", Value: "false", Desc: "启用 ClawNet", Section: "general"},
			{Key: "qqbot_enabled", Value: "false", Desc: "启用 QQ 机器人", Section: "qqbot"},
			{Key: "qqbot_app_id", Value: "", Desc: "QQ Bot AppID", Section: "qqbot"},
			{Key: "qqbot_app_secret", Value: "", Desc: "QQ Bot AppSecret", Section: "qqbot"},
			{Key: "telegram_bot_enabled", Value: "false", Desc: "启用 Telegram 机器人", Section: "telegram"},
			{Key: "telegram_bot_token", Value: "", Desc: "Telegram Bot Token", Section: "telegram"},
			{Key: "skill_purchase_mode", Value: "auto", Desc: "Skill获取策略 (auto/free_only)", Section: "skillmarket"},
		},
		input: ti,
	}
}

// SetEntries 更新配置项。
func (m *ConfigModel) SetEntries(entries []ConfigEntry) {
	m.entries = entries
	if m.cursor >= len(entries) {
		m.cursor = max(0, len(entries)-1)
	}
}

// Init 实现 tea.Model。
func (m ConfigModel) Init() tea.Cmd { return nil }

// Update 处理键盘事件。
func (m ConfigModel) Update(msg tea.Msg) (ConfigModel, tea.Cmd) {
	if m.editing {
		return m.updateEditing(msg)
	}
	return m.updateNormal(msg)
}

// updateNormal 非编辑模式下的按键处理。
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

// updateEditing 编辑模式下的按键处理。
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
	// 委托给 textinput 处理其他按键
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// View 渲染配置视图。
func (m ConfigModel) View() string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57"))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	editStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("212"))

	var b strings.Builder
	b.WriteString(headerStyle.Render("  配置管理"))
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat("─", 60) + "\n")

	for i, e := range m.entries {
		if m.editing && i == m.cursor {
			// 编辑行：显示 textinput
			line := fmt.Sprintf("  %-20s = ", e.Key)
			b.WriteString(editStyle.Render(line))
			b.WriteString(m.input.View())
			b.WriteString("\n")
		} else {
			val := e.Value
			if val == "" {
				val = dimStyle.Render("(未设置)")
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
		b.WriteString("\n  Enter:确认  Esc:取消")
	} else {
		b.WriteString("\n  ↑↓:选择  Enter:编辑  r:刷新")
	}
	return b.String()
}
