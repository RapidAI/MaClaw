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
	width  int
	height int

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

// ActiveSubTab returns the currently selected Tasks sub-tab.
func (m TaskModel) ActiveSubTab() int { return m.subTab }

// FocusTab switches the Tasks view to a valid sub-tab.
func (m *TaskModel) FocusTab(tab int) {
	if tab < 0 || tab >= TaskSubCount {
		return
	}
	m.subTab = tab
}

// FocusRemote switches the Tasks view to the remote-task sub-tab.
func (m *TaskModel) FocusRemote() {
	m.FocusTab(TaskSubRemote)
}

// FocusBackground switches the Tasks view to the background-task sub-tab.
func (m *TaskModel) FocusBackground() {
	m.FocusTab(TaskSubBackground)
}

// FocusScheduled switches the Tasks view to the scheduled-task sub-tab.
func (m *TaskModel) FocusScheduled() {
	m.FocusTab(TaskSubScheduled)
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

// TaskOpenToolsMsg asks the host app to open Tools from an empty task state.
type TaskOpenToolsMsg struct{}

// TaskOpenChatMsg asks the host app to open Chat from an empty task state.
type TaskOpenChatMsg struct{}

// Update 处理键盘事件。
func (m TaskModel) Update(msg tea.Msg) (TaskModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
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

func (m TaskModel) visibleRows(reserved int) int {
	rows := m.height - reserved
	if rows < 4 {
		return 4
	}
	return rows
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
	case "enter":
		if !m.remoteLoading && len(m.remoteTasks) == 0 {
			return m, func() tea.Msg { return TaskOpenToolsMsg{} }
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
	case "enter":
		if !m.bgLoading && len(m.bgTasks) == 0 {
			return m, func() tea.Msg { return TaskOpenChatMsg{} }
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
	case "enter":
		if !m.loading && len(m.tasks) == 0 {
			return m, func() tea.Msg { return TaskOpenChatMsg{} }
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
	if m.useCompactView() {
		return fitRenderedLines(m.viewCompact(), m.width)
	}

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

	return fitRenderedLines(b.String(), m.width)
}

func (m TaskModel) useCompactView() bool {
	return m.height > 0 && m.height < 14
}

func (m TaskModel) compactContentRows() int {
	if m.height <= 0 {
		return 10
	}
	return max(1, m.height-4)
}

func (m TaskModel) viewCompact() string {
	var b strings.Builder
	b.WriteString(m.renderSubTabs())
	b.WriteString("\n")

	switch m.subTab {
	case TaskSubRemote:
		b.WriteString(m.viewRemoteCompact())
	case TaskSubBackground:
		b.WriteString(m.viewBackgroundCompact())
	case TaskSubScheduled:
		b.WriteString(m.viewScheduledCompact())
	}

	return b.String()
}

func (m TaskModel) compactBodyRows() int {
	return max(1, m.compactContentRows()-2)
}

func (m TaskModel) compactEmpty(text, footer string) string {
	lines := taskCompactTextLines(text)
	if len(lines) == 0 {
		lines = []string{text}
	}
	var b strings.Builder
	available := m.compactBodyRows()
	lineLimit := available
	if strings.TrimSpace(footer) != "" && available > 1 {
		lineLimit = available - 1
	}
	for i := 0; i < min(len(lines), max(1, lineLimit)); i++ {
		b.WriteString("  " + fitDisplay(lines[i], max(10, m.width-2)) + "\n")
	}
	if strings.TrimSpace(footer) != "" {
		b.WriteString("  " + fitDisplay(footer, max(10, m.width-2)))
	}
	return b.String()
}

func taskCompactTextLines(text string) []string {
	parts := strings.Split(text, "\n")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
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
		taskLocalText(m.lang, "id"), taskLocalText(m.lang, "host"), taskLocalText(m.lang, "status"), taskLocalText(m.lang, "started"), taskLocalText(m.lang, "command"))))
	b.WriteString("\n  " + strings.Repeat("─", 70) + "\n")

	start, end := scrollWindow(len(m.remoteTasks), m.remoteCursor, m.visibleRows(8))
	if start > 0 {
		b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(taskLocalFormat(m.lang, "moreAbove", start)) + "\n")
	}
	for i := start; i < end; i++ {
		t := m.remoteTasks[i]
		statusIcon := "● "
		if t.Status == "completed" {
			statusIcon = "✓ "
		} else if t.Status == "failed" {
			statusIcon = "✗ "
		}
		line := fmt.Sprintf("  %-14s %-20s %s%-8s %-12s %s",
			truncate(t.ID, 14), truncate(t.Host, 20), statusIcon, truncate(taskStatusDisplay(t.Status, m.lang), 8),
			truncate(t.StartedAt, 12), truncate(t.Command, 25))
		if i == m.remoteCursor {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}

	if end < len(m.remoteTasks) {
		b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(taskLocalFormat(m.lang, "moreBelow", len(m.remoteTasks)-end)) + "\n")
	}

	b.WriteString("\n  " + i18n.T(i18n.MsgTUITaskRemoteFooter, m.lang))
	return b.String()
}

func (m TaskModel) viewRemoteCompact() string {
	if m.remoteLoading {
		return "  " + i18n.T(i18n.MsgTUIScheduleLoading, m.lang)
	}
	if len(m.remoteTasks) == 0 {
		return m.compactEmpty(i18n.T(i18n.MsgTUITaskRemoteEmpty, m.lang), "")
	}
	return m.compactList(len(m.remoteTasks), m.remoteCursor, i18n.T(i18n.MsgTUITaskRemoteFooter, m.lang), func(i int) string {
		t := m.remoteTasks[i]
		return strings.TrimSpace(fmt.Sprintf("%s  %s  %s  %s",
			truncate(t.ID, 18),
			truncate(t.Host, 18),
			taskStatusDisplay(t.Status, m.lang),
			truncate(t.Command, 28),
		))
	})
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
		taskLocalText(m.lang, "id"), taskLocalText(m.lang, "name"), taskLocalText(m.lang, "status"), taskLocalText(m.lang, "rounds"), taskLocalText(m.lang, "started"))))
	b.WriteString("\n  " + strings.Repeat("─", 70) + "\n")

	start, end := scrollWindow(len(m.bgTasks), m.bgCursor, m.visibleRows(8))
	if start > 0 {
		b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(taskLocalFormat(m.lang, "moreAbove", start)) + "\n")
	}
	for i := start; i < end; i++ {
		t := m.bgTasks[i]
		statusIcon := "● "
		if t.Status == "stopped" {
			statusIcon = "○ "
		}
		line := fmt.Sprintf("  %-14s %-24s %s%-8s %-10s %s",
			truncate(t.ID, 14), truncate(t.Name, 24), statusIcon, truncate(taskStatusDisplay(t.Status, m.lang), 8),
			t.Rounds, truncate(t.StartedAt, 12))
		if i == m.bgCursor {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}

	if end < len(m.bgTasks) {
		b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(taskLocalFormat(m.lang, "moreBelow", len(m.bgTasks)-end)) + "\n")
	}

	b.WriteString("\n  " + i18n.T(i18n.MsgTUITaskBackgroundFooter, m.lang))
	return b.String()
}

func (m TaskModel) viewBackgroundCompact() string {
	if m.bgLoading {
		return "  " + i18n.T(i18n.MsgTUIScheduleLoading, m.lang)
	}
	if len(m.bgTasks) == 0 {
		return m.compactEmpty(i18n.T(i18n.MsgTUITaskBackgroundEmpty, m.lang), "")
	}
	return m.compactList(len(m.bgTasks), m.bgCursor, i18n.T(i18n.MsgTUITaskBackgroundFooter, m.lang), func(i int) string {
		t := m.bgTasks[i]
		return strings.TrimSpace(fmt.Sprintf("%s  %s  %s  %s",
			truncate(t.ID, 18),
			truncate(t.Name, 24),
			taskStatusDisplay(t.Status, m.lang),
			truncate(t.Rounds, 10),
		))
	})
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

	start, end := scrollWindow(len(m.tasks), m.cursor, m.visibleRows(8))
	if start > 0 {
		b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(taskLocalFormat(m.lang, "moreAbove", start)) + "\n")
	}
	for i := start; i < end; i++ {
		t := m.tasks[i]
		action := truncate(t.Action, 25)
		statusIcon := "● "
		if t.Status == "paused" {
			statusIcon = "⏸ "
		}
		line := fmt.Sprintf("  %-18s %s%-6s %-6s %-5d %s",
			truncate(t.Name, 18), statusIcon, truncate(taskStatusDisplay(t.Status, m.lang), 6), t.Time, t.Runs, action)
		if i == m.cursor {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}

	if end < len(m.tasks) {
		b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(taskLocalFormat(m.lang, "moreBelow", len(m.tasks)-end)) + "\n")
	}

	b.WriteString("\n  " + i18n.T(i18n.MsgTUIScheduleFooter, m.lang))
	return b.String()
}

func (m TaskModel) viewScheduledCompact() string {
	if m.loading {
		return "  " + i18n.T(i18n.MsgTUIScheduleLoading, m.lang)
	}
	if len(m.tasks) == 0 {
		return m.compactEmpty(i18n.T(i18n.MsgTUIScheduleEmpty, m.lang), "")
	}
	return m.compactList(len(m.tasks), m.cursor, i18n.T(i18n.MsgTUIScheduleFooter, m.lang), func(i int) string {
		t := m.tasks[i]
		return strings.TrimSpace(fmt.Sprintf("%s  %s  %s  %d  %s",
			truncate(t.Name, 22),
			taskStatusDisplay(t.Status, m.lang),
			truncate(t.Time, 12),
			t.Runs,
			truncate(t.Action, 28),
		))
	})
}

func (m TaskModel) compactList(total, cursor int, footer string, render func(int) string) string {
	var b strings.Builder
	rowBudget := max(1, m.compactBodyRows()-1)
	start, end := scrollWindow(total, cursor, rowBudget)
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	if start > 0 && rowBudget >= 3 {
		b.WriteString("  " + dim.Render(taskLocalFormat(m.lang, "moreAbove", start)) + "\n")
		start++
	}
	for i := start; i < end; i++ {
		prefix := "  "
		if i == cursor {
			prefix = "> "
		}
		line := prefix + fitDisplay(render(i), max(8, m.width-lipgloss.Width(prefix)))
		if i == cursor {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	if end < total && rowBudget >= 3 {
		b.WriteString("  " + dim.Render(taskLocalFormat(m.lang, "moreBelow", total-end)) + "\n")
	}
	b.WriteString("  " + dim.Render(fitDisplay(footer, max(10, m.width-2))))
	return b.String()
}

func taskLocalText(lang, key string) string {
	if i18n.NormalizeLang(lang) == "en" {
		texts := map[string]string{
			"id": "ID", "host": "HOST", "status": "STATUS", "started": "STARTED", "command": "COMMAND",
			"name": "NAME", "rounds": "ROUNDS", "moreAbove": "... %d more above", "moreBelow": "... %d more below",
		}
		if text, ok := texts[key]; ok {
			return text
		}
	}
	texts := map[string]string{
		"id": "ID", "host": "主机", "status": "状态", "started": "开始时间", "command": "命令",
		"name": "名称", "rounds": "轮次", "moreAbove": "... 上方还有 %d 项", "moreBelow": "... 下方还有 %d 项",
	}
	if text, ok := texts[key]; ok {
		return text
	}
	return key
}

func taskStatusDisplay(status, lang string) string {
	if i18n.NormalizeLang(lang) == "en" {
		switch status {
		case "running":
			return "running"
		case "completed":
			return "done"
		case "failed":
			return "failed"
		case "stopped":
			return "stopped"
		case "active":
			return "active"
		case "paused":
			return "paused"
		}
		return status
	}
	switch status {
	case "running":
		return "运行中"
	case "completed":
		return "已完成"
	case "failed":
		return "失败"
	case "stopped":
		return "已停止"
	case "active":
		return "启用"
	case "paused":
		return "暂停"
	}
	return status
}

func taskLocalFormat(lang, key string, args ...interface{}) string {
	return fmt.Sprintf(taskLocalText(lang, key), args...)
}
