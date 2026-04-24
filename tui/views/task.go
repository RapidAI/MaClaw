package views

// task.go implements the "任务" (Tasks) tab with three sub-tabs:
// - 远程 (Remote): SSH background tasks submitted via ssh tool
// - 后台 (Background): Agent loop background tasks
// - 计划任务 (Scheduled): Cron-like scheduled tasks

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Task sub-tab constants.
const (
	TaskSubRemote = iota
	TaskSubBackground
	TaskSubScheduled
	TaskSubCount
)

// ScheduleItem 计划任务列表项。
type ScheduleItem struct {
	ID     string
	Name   string
	Status string // active, paused
	Time   string // "HH:MM" or "每N小时" etc.
	Runs   int
	Action string
}

// RemoteTaskItem 远程任务列表项。
type RemoteTaskItem struct {
	ID        string
	Host      string
	Command   string
	Status    string // running, completed, failed
	StartedAt string
}

// BackgroundTaskItem 后台任务列表项。
type BackgroundTaskItem struct {
	ID        string
	Name      string
	Status    string // running, stopped
	Rounds    string // e.g. "5/10"
	StartedAt string
}

// TaskModel 任务视图（远程 + 后台 + 计划任务 三个子标签）。
type TaskModel struct {
	subTab int
	lang   string

	// 远程任务 sub-tab
	remoteTasks   []RemoteTaskItem
	remoteCursor  int
	remoteLoading bool

	// 后台任务 sub-tab
	bgTasks   []BackgroundTaskItem
	bgCursor  int
	bgLoading bool

	// 计划任务 sub-tab
	tasks   []ScheduleItem
	cursor  int
	loading bool
}

// NewTaskModel 创建任务视图。
func NewTaskModel(lang string) TaskModel {
	return TaskModel{
		subTab:        TaskSubRemote,
		loading:       true,
		remoteLoading: true,
		bgLoading:     true,
		lang:          i18n.NormalizeLang(lang),
	}
}

func (m *TaskModel) SetLang(lang string) {
	m.lang = i18n.NormalizeLang(lang)
}

// SetTasks 更新计划任务列表。
func (m *TaskModel) SetTasks(tasks []ScheduleItem) {
	m.tasks = tasks
	m.loading = false
	if m.cursor >= len(tasks) {
		m.cursor = max(0, len(tasks)-1)
	}
}

// SetRemoteTasks 更新远程任务列表。
func (m *TaskModel) SetRemoteTasks(tasks []RemoteTaskItem) {
	m.remoteTasks = tasks
	m.remoteLoading = false
	if m.remoteCursor >= len(tasks) {
		m.remoteCursor = max(0, len(tasks)-1)
	}
}

// SetBackgroundTasks 更新后台任务列表。
func (m *TaskModel) SetBackgroundTasks(tasks []BackgroundTaskItem) {
	m.bgTasks = tasks
	m.bgLoading = false
	if m.bgCursor >= len(tasks) {
		m.bgCursor = max(0, len(tasks)-1)
	}
}

// Init 实现 tea.Model。
func (m TaskModel) Init() tea.Cmd { return nil }

// SchedulePauseMsg 请求暂停/恢复计划任务。
type SchedulePauseMsg struct {
	ID     string
	Paused bool // true=当前是 active 要暂停, false=当前是 paused 要恢复
}

// ScheduleDeleteMsg 请求删除计划任务。
type ScheduleDeleteMsg struct{ ID string }

// Update 处理键盘事件。
func (m TaskModel) Update(msg tea.Msg) (TaskModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "1":
			m.subTab = TaskSubRemote
			return m, nil
		case "2":
			m.subTab = TaskSubBackground
			return m, nil
		case "3":
			m.subTab = TaskSubScheduled
			return m, nil
		}

		// Delegate to active sub-tab
		switch m.subTab {
		case TaskSubRemote:
			return m.updateRemote(msg)
		case TaskSubBackground:
			return m.updateBackground(msg)
		case TaskSubScheduled:
			return m.updateScheduled(msg)
		}
	}
	return m, nil
}

func (m TaskModel) updateRemote(msg tea.KeyMsg) (TaskModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.remoteCursor > 0 {
			m.remoteCursor--
		}
	case "down", "j":
		if m.remoteCursor < len(m.remoteTasks)-1 {
			m.remoteCursor++
		}
	}
	return m, nil
}

func (m TaskModel) updateBackground(msg tea.KeyMsg) (TaskModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.bgCursor > 0 {
			m.bgCursor--
		}
	case "down", "j":
		if m.bgCursor < len(m.bgTasks)-1 {
			m.bgCursor++
		}
	}
	return m, nil
}

func (m TaskModel) updateScheduled(msg tea.KeyMsg) (TaskModel, tea.Cmd) {
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
	return m, nil
}

// View 渲染任务视图。
func (m TaskModel) View() string {
	var b strings.Builder
	b.WriteString(m.renderSubTabs())
	b.WriteString("\n\n")

	switch m.subTab {
	case TaskSubRemote:
		b.WriteString(m.viewRemote())
	case TaskSubBackground:
		b.WriteString(m.viewBackground())
	case TaskSubScheduled:
		b.WriteString(m.viewScheduled())
	}

	return b.String()
}

func (m TaskModel) renderSubTabs() string {
	active := lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57")).Padding(0, 1)
	inactive := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).Background(lipgloss.Color("238")).Padding(0, 1)

	names := [TaskSubCount]string{
		i18n.T(i18n.MsgTUITaskSubRemote, m.lang),
		i18n.T(i18n.MsgTUITaskSubBackground, m.lang),
		i18n.T(i18n.MsgTUITaskSubScheduled, m.lang),
	}
	var tabs string
	for i, name := range names {
		label := fmt.Sprintf("%d:%s", i+1, name)
		if i == m.subTab {
			tabs += active.Render(label)
		} else {
			tabs += inactive.Render(label)
		}
		tabs += " "
	}
	return "  " + tabs
}

func (m TaskModel) viewRemote() string {
	if m.remoteLoading {
		return "  " + i18n.T(i18n.MsgTUIScheduleLoading, m.lang)
	}
	if len(m.remoteTasks) == 0 {
		return "  " + strings.ReplaceAll(i18n.T(i18n.MsgTUITaskRemoteEmpty, m.lang), "\n", "\n  ")
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	var b strings.Builder
	b.WriteString(headerStyle.Render(fmt.Sprintf("  %-14s %-20s %-10s %-12s %s",
		"ID", "HOST", "STATUS", "STARTED", "COMMAND")))
	b.WriteString("\n  " + strings.Repeat("─", 70) + "\n")

	for i, t := range m.remoteTasks {
		statusIcon := "● "
		if t.Status == "completed" {
			statusIcon = "✓ "
		} else if t.Status == "failed" {
			statusIcon = "✗ "
		}
		line := fmt.Sprintf("  %-14s %-20s %s%-8s %-12s %s",
			truncate(t.ID, 14), truncate(t.Host, 20), statusIcon, t.Status,
			truncate(t.StartedAt, 12), truncate(t.Command, 25))
		if i == m.remoteCursor {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n  " + i18n.T(i18n.MsgTUITaskRemoteFooter, m.lang))
	return b.String()
}

func (m TaskModel) viewBackground() string {
	if m.bgLoading {
		return "  " + i18n.T(i18n.MsgTUIScheduleLoading, m.lang)
	}
	if len(m.bgTasks) == 0 {
		return "  " + strings.ReplaceAll(i18n.T(i18n.MsgTUITaskBackgroundEmpty, m.lang), "\n", "\n  ")
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	var b strings.Builder
	b.WriteString(headerStyle.Render(fmt.Sprintf("  %-14s %-24s %-10s %-10s %s",
		"ID", "NAME", "STATUS", "ROUNDS", "STARTED")))
	b.WriteString("\n  " + strings.Repeat("─", 70) + "\n")

	for i, t := range m.bgTasks {
		statusIcon := "● "
		if t.Status == "stopped" {
			statusIcon = "○ "
		}
		line := fmt.Sprintf("  %-14s %-24s %s%-8s %-10s %s",
			truncate(t.ID, 14), truncate(t.Name, 24), statusIcon, t.Status,
			t.Rounds, truncate(t.StartedAt, 12))
		if i == m.bgCursor {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n  " + i18n.T(i18n.MsgTUITaskBackgroundFooter, m.lang))
	return b.String()
}

func (m TaskModel) viewScheduled() string {
	if m.loading {
		return "  " + i18n.T(i18n.MsgTUIScheduleLoading, m.lang)
	}
	if len(m.tasks) == 0 {
		return "  " + strings.ReplaceAll(i18n.T(i18n.MsgTUIScheduleEmpty, m.lang), "\n", "\n  ")
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	var b strings.Builder
	b.WriteString(headerStyle.Render(fmt.Sprintf("  %-18s %-8s %-6s %-5s %s",
		i18n.T(i18n.MsgTUIScheduleHeaderName, m.lang),
		i18n.T(i18n.MsgTUIScheduleHeaderStatus, m.lang),
		i18n.T(i18n.MsgTUIScheduleHeaderTime, m.lang),
		i18n.T(i18n.MsgTUIScheduleHeaderRuns, m.lang),
		i18n.T(i18n.MsgTUIScheduleHeaderAction, m.lang),
	)))
	b.WriteString("\n  " + strings.Repeat("─", 65) + "\n")

	for i, t := range m.tasks {
		action := truncate(t.Action, 25)
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

	b.WriteString("\n  " + i18n.T(i18n.MsgTUIScheduleFooter, m.lang))
	return b.String()
}
