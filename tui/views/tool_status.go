package views

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ToolInfo 工具状态信息。
type ToolInfo struct {
	Name      string
	Available bool
	Version   string
	Path      string
}

// ToolStatusModel 工具状态视图。
type ToolStatusModel struct {
	tools   []ToolInfo
	cursor  int
	loading bool
	lang    string
}

// NewToolStatusModel 创建工具状态视图。
func NewToolStatusModel(lang string) ToolStatusModel {
	return ToolStatusModel{loading: true, lang: i18n.NormalizeLang(lang)}
}

func (m *ToolStatusModel) SetLang(lang string) {
	m.lang = i18n.NormalizeLang(lang)
}

// SetTools 更新工具列表。
func (m *ToolStatusModel) SetTools(tools []ToolInfo) {
	m.tools = tools
	m.loading = false
}

// Init 实现 tea.Model。
func (m ToolStatusModel) Init() tea.Cmd { return nil }

// ToolRefreshMsg 请求刷新工具状态。
type ToolRefreshMsg struct{}

// Update 处理键盘事件。
func (m ToolStatusModel) Update(msg tea.Msg) (ToolStatusModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.tools)-1 {
				m.cursor++
			}
		case "r":
			return m, func() tea.Msg { return ToolRefreshMsg{} }
		}
	}
	return m, nil
}

// View 渲染工具状态列表。
func (m ToolStatusModel) View() string {
	if m.loading {
		return "  " + i18n.T(i18n.MsgTUIToolDetecting, m.lang)
	}
	if len(m.tools) == 0 {
		return "  " + i18n.T(i18n.MsgTUIToolNone, m.lang)
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57"))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

	var b strings.Builder
	b.WriteString(headerStyle.Render(fmt.Sprintf("  %-15s %-8s %-12s %s",
		i18n.T(i18n.MsgTUIToolHeaderName, m.lang),
		i18n.T(i18n.MsgTUIToolHeaderStatus, m.lang),
		i18n.T(i18n.MsgTUIToolHeaderVersion, m.lang),
		i18n.T(i18n.MsgTUIToolHeaderPath, m.lang),
	)))
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat("─", 60) + "\n")

	for i, t := range m.tools {
		status := errStyle.Render(i18n.T(i18n.MsgTUIToolNotInstalled, m.lang))
		if t.Available {
			status = okStyle.Render(i18n.T(i18n.MsgTUIToolReady, m.lang))
		}
		line := fmt.Sprintf("  %-15s %s %-12s %s", t.Name, status, t.Version, t.Path)
		if i == m.cursor {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n  " + i18n.T(i18n.MsgTUIToolFooter, m.lang))
	return b.String()
}
