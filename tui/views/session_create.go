package views

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SessionCreateSubmitMsg 提交创建会话请求。
type SessionCreateSubmitMsg struct {
	Tool    string
	Project string
}

// SessionCreateModel 创建会话的表单视图（overlay）。
type SessionCreateModel struct {
	tools        []string // 可用工具列表
	toolCursor   int
	projectInput textinput.Model
	focusField   int // 0=tool, 1=project
	width        int
	lang         string
}

// NewSessionCreateModel 创建会话创建表单。
func NewSessionCreateModel(tools []string, lang string) SessionCreateModel {
	lang = i18n.NormalizeLang(lang)
	ti := textinput.New()
	ti.Placeholder = i18n.T(i18n.MsgTUISessionCreateProjectPlaceholder, lang)
	ti.CharLimit = 256
	ti.Width = 40

	if len(tools) == 0 {
		tools = []string{i18n.T(i18n.MsgTUISessionCreateNoTools, lang)}
	}
	return SessionCreateModel{
		tools:        tools,
		projectInput: ti,
		lang:         lang,
	}
}

func (m *SessionCreateModel) SetLang(lang string) {
	m.lang = i18n.NormalizeLang(lang)
	m.projectInput.Placeholder = i18n.T(i18n.MsgTUISessionCreateProjectPlaceholder, m.lang)
	if len(m.tools) == 1 && (m.tools[0] == i18n.T(i18n.MsgTUISessionCreateNoTools, "zh") || m.tools[0] == i18n.T(i18n.MsgTUISessionCreateNoTools, "en")) {
		m.tools[0] = i18n.T(i18n.MsgTUISessionCreateNoTools, m.lang)
	}
}

// Init 实现 tea.Model。
func (m SessionCreateModel) Init() tea.Cmd { return nil }

// Update 处理键盘事件。
func (m SessionCreateModel) Update(msg tea.Msg) (SessionCreateModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			m.focusField = (m.focusField + 1) % 2
			if m.focusField == 1 {
				m.projectInput.Focus()
				return m, textinput.Blink
			}
			m.projectInput.Blur()
			return m, nil
		case "enter":
			if m.focusField == 0 {
				m.focusField = 1
				m.projectInput.Focus()
				return m, textinput.Blink
			}
			tool := ""
			if m.toolCursor < len(m.tools) {
				tool = m.tools[m.toolCursor]
			}
			project := m.projectInput.Value()
			return m, func() tea.Msg {
				return SessionCreateSubmitMsg{Tool: tool, Project: project}
			}
		case "up", "k":
			if m.focusField == 0 && m.toolCursor > 0 {
				m.toolCursor--
			}
		case "down", "j":
			if m.focusField == 0 && m.toolCursor < len(m.tools)-1 {
				m.toolCursor++
			}
		}
	}

	if m.focusField == 1 {
		var cmd tea.Cmd
		m.projectInput, cmd = m.projectInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

// View 渲染创建会话表单。
func (m SessionCreateModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229"))
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	focusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("212"))

	var b strings.Builder
	b.WriteString(titleStyle.Render("  " + i18n.T(i18n.MsgTUISessionCreateTitle, m.lang)))
	b.WriteString("\n\n")

	toolLabel := labelStyle.Render("  " + i18n.T(i18n.MsgTUISessionCreateTool, m.lang))
	if m.focusField == 0 {
		toolLabel = focusStyle.Render("▸ " + i18n.T(i18n.MsgTUISessionCreateTool, m.lang))
	}
	b.WriteString(toolLabel + "\n")

	maxShow := 6
	start := 0
	if m.toolCursor >= maxShow {
		start = m.toolCursor - maxShow + 1
	}
	end := start + maxShow
	if end > len(m.tools) {
		end = len(m.tools)
	}
	for i := start; i < end; i++ {
		prefix := "    "
		if i == m.toolCursor {
			b.WriteString(selectedStyle.Render(fmt.Sprintf("  ▸ %s", m.tools[i])))
		} else {
			b.WriteString(normalStyle.Render(fmt.Sprintf("%s%s", prefix, m.tools[i])))
		}
		b.WriteString("\n")
	}
	if len(m.tools) > maxShow {
		b.WriteString(dimStyle.Render("    " + i18n.Tf(i18n.MsgTUISessionCreateToolCount, m.lang, len(m.tools))))
		b.WriteString("\n")
	}

	b.WriteString("\n")

	projLabel := labelStyle.Render("  " + i18n.T(i18n.MsgTUISessionCreateProject, m.lang))
	if m.focusField == 1 {
		projLabel = focusStyle.Render("▸ " + i18n.T(i18n.MsgTUISessionCreateProject, m.lang))
	}
	b.WriteString(projLabel + "\n")
	b.WriteString("    " + m.projectInput.View() + "\n")

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  " + i18n.T(i18n.MsgTUISessionCreateFooter, m.lang)))
	return b.String()
}
