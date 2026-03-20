package views

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SessionDetailModel 会话详情视图（实时输出流）。
type SessionDetailModel struct {
	sessionID string
	tool      string
	title     string
	status    string
	lines     []string // 输出行缓冲
	maxLines  int
	scroll    int // 滚动偏移
	height    int
}

// NewSessionDetailModel 创建会话详情视图。
func NewSessionDetailModel(id, tool, title string) SessionDetailModel {
	return SessionDetailModel{
		sessionID: id,
		tool:      tool,
		title:     title,
		maxLines:  1000,
	}
}

// AppendOutput 追加输出行。
func (m *SessionDetailModel) AppendOutput(line string) {
	m.lines = append(m.lines, line)
	if len(m.lines) > m.maxLines {
		m.lines = m.lines[len(m.lines)-m.maxLines:]
	}
}

// GetLines 返回当前输出行数（用于增量追加）。
func (m *SessionDetailModel) GetLines() []string {
	return m.lines
}

// SetStatus 更新会话状态。
func (m *SessionDetailModel) SetStatus(status string) {
	m.status = status
}

// Init 实现 tea.Model。
func (m SessionDetailModel) Init() tea.Cmd { return nil }

// Update 处理键盘事件。
func (m SessionDetailModel) Update(msg tea.Msg) (SessionDetailModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height - 6 // 留出 header + footer
		if m.height < 1 {
			m.height = 1
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.scroll > 0 {
				m.scroll--
			}
		case "down", "j":
			maxScroll := len(m.lines) - m.height
			if maxScroll < 0 {
				maxScroll = 0
			}
			if m.scroll < maxScroll {
				m.scroll++
			}
		case "G": // 跳到底部
			maxScroll := len(m.lines) - m.height
			if maxScroll < 0 {
				maxScroll = 0
			}
			m.scroll = maxScroll
		case "g": // 跳到顶部
			m.scroll = 0
		}
	}
	return m, nil
}

// View 渲染会话详情。
func (m SessionDetailModel) View() string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	var b strings.Builder
	b.WriteString(headerStyle.Render(fmt.Sprintf("  会话: %s  [%s]  %s", m.sessionID, m.tool, statusIcon(m.status))))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(fmt.Sprintf("  %s", m.title)))
	b.WriteString("\n")
	b.WriteString("  " + strings.Repeat("─", 60) + "\n")

	// 输出区域
	viewHeight := m.height
	if viewHeight <= 0 {
		viewHeight = 10
	}

	start := m.scroll
	end := start + viewHeight
	if end > len(m.lines) {
		end = len(m.lines)
	}
	if start > end {
		start = end
	}

	for _, line := range m.lines[start:end] {
		b.WriteString("  " + line + "\n")
	}

	// 填充空行
	for i := end - start; i < viewHeight; i++ {
		b.WriteString("\n")
	}

	b.WriteString(dimStyle.Render(fmt.Sprintf("  行 %d/%d  ↑↓:滚动  g/G:首/尾  Esc:返回", m.scroll+1, len(m.lines))))
	return b.String()
}
