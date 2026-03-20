package views

import (
	"fmt"
	"strings"

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
	ID   string
	Tool string
	Title string
}

// SessionListModel 会话列表视图。
type SessionListModel struct {
	sessions []SessionItem
	cursor   int
	loading  bool
}

// NewSessionListModel 创建会话列表视图。
func NewSessionListModel() SessionListModel {
	return SessionListModel{loading: true}
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
		return "  正在加载会话列表..."
	}
	if len(m.sessions) == 0 {
		return "  暂无活跃会话\n\n  按 Enter 创建新会话"
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
	b.WriteString(headerStyle.Render(fmt.Sprintf("  %-16s %-10s %-10s %s", "ID", "工具", "状态", "标题")))
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat("─", 60) + "\n")

	for i, s := range m.sessions {
		line := fmt.Sprintf("  %-16s %-10s %-10s %s",
			truncate(s.ID, 16), s.Tool, statusIcon(s.Status), truncate(s.Title, 30))
		if i == m.cursor {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n  ↑↓:选择  Enter:详情  n:新建  d:终止")
	return b.String()
}

func statusIcon(status string) string {
	switch status {
	case "running":
		return "● 运行中"
	case "stopped":
		return "○ 已停止"
	case "error":
		return "✗ 错误"
	default:
		return "? " + status
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
