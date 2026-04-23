package views

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SessionItem 会话列表中的一项。
type SessionItem struct {
	ID     string
	Tool   string
	Title  string
	Status string // running, stopped, error
}

// SessionCreateMsg 请求创建新会话。
type SessionCreateMsg struct{}

// SessionKillMsg 请求终止选中的会话。
type SessionKillMsg struct {
	ID string
}

// SessionOpenMsg 请求打开会话详情。
type SessionOpenMsg struct {
	ID    string
	Tool  string
	Title string
}

// SessionListModel 会话列表视图。
type SessionListModel struct {
	sessions []SessionItem
	cursor   int
	loading  bool
	lang     string
}

// NewSessionListModel 创建会话列表视图。
func NewSessionListModel(lang string) SessionListModel {
	return SessionListModel{loading: true, lang: i18n.NormalizeLang(lang)}
}

func (m *SessionListModel) SetLang(lang string) {
	m.lang = i18n.NormalizeLang(lang)
}

// SetSessions 更新会话列表数据。
func (m *SessionListModel) SetSessions(sessions []SessionItem) {
	m.sessions = sessions
	m.loading = false
	if m.cursor >= len(sessions) {
		m.cursor = max(0, len(sessions)-1)
	}
}

// Init 实现 tea.Model。
func (m SessionListModel) Init() tea.Cmd { return nil }

// Update 处理键盘事件。
func (m SessionListModel) Update(msg tea.Msg) (SessionListModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.sessions)-1 {
				m.cursor++
			}
		case "enter":
			if len(m.sessions) == 0 {
				return m, func() tea.Msg { return SessionCreateMsg{} }
			}
			if m.cursor < len(m.sessions) {
				s := m.sessions[m.cursor]
				return m, func() tea.Msg {
					return SessionOpenMsg{ID: s.ID, Tool: s.Tool, Title: s.Title}
				}
			}
		case "n", "c":
			return m, func() tea.Msg { return SessionCreateMsg{} }
		case "d", "x":
			if m.cursor < len(m.sessions) {
				id := m.sessions[m.cursor].ID
				return m, func() tea.Msg { return SessionKillMsg{ID: id} }
			}
		}
	}
	return m, nil
}

// View 渲染会话列表。
func (m SessionListModel) View() string {
	if m.loading {
		return "  " + i18n.T(i18n.MsgTUISessionLoading, m.lang)
	}
	if len(m.sessions) == 0 {
		return m.renderWelcome()
	}

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("252"))

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57"))

	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	var b strings.Builder
	b.WriteString(headerStyle.Render(fmt.Sprintf("  %-16s %-10s %-10s %s",
		i18n.T(i18n.MsgTUISessionHeaderID, m.lang),
		i18n.T(i18n.MsgTUISessionHeaderTool, m.lang),
		i18n.T(i18n.MsgTUISessionHeaderStatus, m.lang),
		i18n.T(i18n.MsgTUISessionHeaderTitle, m.lang),
	)))
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat("─", 60) + "\n")

	for i, s := range m.sessions {
		line := fmt.Sprintf("  %-16s %-10s %-10s %s",
			truncate(s.ID, 16), s.Tool, statusIcon(s.Status, m.lang), truncate(s.Title, 30))
		if i == m.cursor {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n  " + i18n.T(i18n.MsgTUISessionFooter, m.lang))
	return b.String()
}

func statusIcon(status string, lang string) string {
	switch status {
	case "running":
		return i18n.T(i18n.MsgTUIStatusRunning, lang)
	case "stopped":
		return i18n.T(i18n.MsgTUIStatusStopped, lang)
	case "error":
		return i18n.T(i18n.MsgTUIStatusError, lang)
	default:
		return "? " + status
	}
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(runes[:n-1]) + "…"
}

// renderWelcome renders the ASCII art logo and welcome message.
func (m SessionListModel) renderWelcome() string {
	logoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("39")). // bright cyan
		Bold(true)

	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")) // dim gray

	logo := "" +
		"  ███╗   ███╗ █████╗  ██████╗██╗      █████╗ ██╗    ██╗\n" +
		"  ████╗ ████║██╔══██╗██╔════╝██║     ██╔══██╗██║    ██║\n" +
		"  ██╔████╔██║███████║██║     ██║     ███████║██║ █╗ ██║\n" +
		"  ██║╚██╔╝██║██╔══██║██║     ██║     ██╔══██║██║███╗██║\n" +
		"  ██║ ╚═╝ ██║██║  ██║╚██████╗███████╗██║  ██║╚███╔███╔╝\n" +
		"  ╚═╝     ╚═╝╚═╝  ╚═╝ ╚═════╝╚══════╝╚═╝  ╚═╝ ╚══╝╚══╝"

	hint := i18n.T(i18n.MsgTUISessionEmpty, m.lang)

	var b strings.Builder
	b.WriteString(logoStyle.Render(logo))
	b.WriteString("\n\n")
	b.WriteString("  " + hintStyle.Render(strings.ReplaceAll(hint, "\n", "\n  ")))
	return b.String()
}
