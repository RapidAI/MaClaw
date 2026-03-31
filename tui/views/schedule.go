package views

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ScheduleItem 定时任务列表项。
type ScheduleItem struct {
	ID       string
	Name     string
	Status   string // active, paused
	Time     string // "HH:MM" or "每N小时" etc.
	Runs     int
	Action   string
}

// ScheduleModel 定时任务视图。
type ScheduleModel struct {
	tasks   []ScheduleItem
	cursor  int
	loading bool
}

// NewScheduleModel 创建定时任务视图。
func NewScheduleModel() ScheduleModel {
	return ScheduleModel{loading: true}
}

// SetTasks 更新任务列表。
func (m *ScheduleModel) SetTasks(tasks []ScheduleItem) {
	m.tasks = tasks
	m.loading = false
	if m.cursor >= len(tasks) {
		m.cursor = max(0, len(tasks)-1)
	}
}

// Init 实现 tea.Model。
func (m ScheduleModel) Init() tea.Cmd { return nil }

// SchedulePauseMsg 请求暂停/恢复任务。
type SchedulePauseMsg struct {
	ID     string
	Paused bool // true=当前是 active 要暂停, false=当前是 paused 要恢复
}

// ScheduleDeleteMsg 请求删除任务。
type ScheduleDeleteMsg struct{ ID string }

// Update 处理键盘事件。
func (m ScheduleModel) Update(msg tea.Msg) (ScheduleModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.tasks)-1 {
				m.cursor++
			}
		case "p":
			if m.cursor < len(m.tasks) {
				t := m.tasks[m.cursor]
				return m, func() tea.Msg {
					return SchedulePauseMsg{ID: t.ID, Paused: t.Status == "active"}
				}
			}
		case "d", "x":
			if m.cursor < len(m.tasks) {
				id := m.tasks[m.cursor].ID
				return m, func() tea.Msg { return ScheduleDeleteMsg{ID: id} }
			}
		}
	}
	return m, nil
}

// View 渲染定时任务列表。
func (m ScheduleModel) View() string {
	if m.loading {
		return "  正在加载定时任务..."
	}
	if len(m.tasks) == 0 {
		return "  暂无定时任务\n\n  使用 CLI 创建: maclaw-tui schedule create --name <name> --action <text>"
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	var b strings.Builder
	b.WriteString(headerStyle.Render(fmt.Sprintf("  %-18s %-8s %-6s %-5s %s", "NAME", "STATUS", "TIME", "RUNS", "ACTION")))
	b.WriteString("\n  " + strings.Repeat("─", 65) + "\n")

	for i, t := range m.tasks {
		action := t.Action
		if len(action) > 25 {
			action = action[:22] + "..."
		}
		statusIcon := "● "
		if t.Status == "paused" {
			statusIcon = "⏸ "
		}
		line := fmt.Sprintf("  %-18s %s%-6s %-6s %-5d %s",
			truncate(t.Name, 18), statusIcon, t.Status, t.Time, t.Runs, action)
		if i == m.cursor {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n  ↑↓:选择  p:暂停/恢复  d:删除")
	return b.String()
}
