package views

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// MemoryItem 记忆列表项。
type MemoryItem struct {
	ID       string
	Category string
	Content  string
	Access   int
}

// MemoryDeleteMsg 请求删除记忆。
type MemoryDeleteMsg struct{ ID string }

// MemoryCompressMsg 请求压缩记忆（dedup）。
type MemoryCompressMsg struct{}

// MemoryBackupListMsg 请求列出备份。
type MemoryBackupListMsg struct{}

// MemoryModel 记忆视图。
type MemoryModel struct {
	entries []MemoryItem
	cursor  int
	loading bool
	lang    string
}

// NewMemoryModel 创建记忆视图。
func NewMemoryModel(lang string) MemoryModel {
	return MemoryModel{loading: true, lang: i18n.NormalizeLang(lang)}
}

func (m *MemoryModel) SetLang(lang string) {
	m.lang = i18n.NormalizeLang(lang)
}

// SetEntries 更新记忆列表。
func (m *MemoryModel) SetEntries(entries []MemoryItem) {
	m.entries = entries
	m.loading = false
	if m.cursor >= len(entries) {
		m.cursor = max(0, len(entries)-1)
	}
}

// Init 实现 tea.Model。
func (m MemoryModel) Init() tea.Cmd { return nil }

// Update 处理键盘事件。
func (m MemoryModel) Update(msg tea.Msg) (MemoryModel, tea.Cmd) {
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
		case "d", "x":
			if m.cursor < len(m.entries) {
				id := m.entries[m.cursor].ID
				return m, func() tea.Msg { return MemoryDeleteMsg{ID: id} }
			}
		case "c":
			return m, func() tea.Msg { return MemoryCompressMsg{} }
		case "b":
			return m, func() tea.Msg { return MemoryBackupListMsg{} }
		}
	}
	return m, nil
}

// View 渲染记忆列表。
func (m MemoryModel) View() string {
	if m.loading {
		return "  " + i18n.T(i18n.MsgTUIMemoryLoading, m.lang)
	}
	if len(m.entries) == 0 {
		return "  " + strings.ReplaceAll(i18n.T(i18n.MsgTUIMemoryEmpty, m.lang), "\n", "\n  ")
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	var b strings.Builder
	b.WriteString(headerStyle.Render(fmt.Sprintf("  %-22s %-16s %-6s %s",
		i18n.T(i18n.MsgTUISessionHeaderID, m.lang),
		i18n.T(i18n.MsgTUIMemoryHeaderCategory, m.lang),
		i18n.T(i18n.MsgTUIMemoryHeaderAccess, m.lang),
		i18n.T(i18n.MsgTUIMemoryHeaderContent, m.lang),
	)))
	b.WriteString("\n  " + strings.Repeat("─", 70) + "\n")

	for i, e := range m.entries {
		content := strings.ReplaceAll(e.Content, "\n", " ")
		if len(content) > 30 {
			content = content[:27] + "..."
		}
		line := fmt.Sprintf("  %-22s %-16s %-6d %s",
			truncate(e.ID, 22), truncate(e.Category, 16), e.Access, content)
		if i == m.cursor {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n  " + i18n.T(i18n.MsgTUIMemoryFooter, m.lang))
	return b.String()
}
