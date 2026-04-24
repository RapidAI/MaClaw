package views

import (
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Tab 索引常量。
// Chat is first — it's the primary use case for TUI.
// AgentNet removed — not relevant for standalone TUI.
// Coding sessions removed — TUI codes directly via bash/write_file/edit_file.
const (
	TabChat = iota
	TabTools
	TabTasks    // 任务（远程 + 后台 + 计划任务）
	TabMemory
	TabAudit
	TabConfig
	TabCount
)

// RootModel 是 TUI 的根 Model，管理 Tab 切换和子视图。
type RootModel struct {
	width     int
	height    int
	tab       int
	lang      string

	// 子视图
	Tools         ToolStatusModel
	Tasks         TaskModel
	Memory        MemoryModel
	Audit         AuditModel
	Config        ConfigModel
	Chat          ChatModel
	StatusBar     StatusBarModel
	Help          HelpModel
}

// NewRootModel 创建根 Model。
func NewRootModel(lang string) RootModel {
	lang = i18n.NormalizeLang(lang)
	chat := NewChatModel(lang)
	chat.FocusInput() // 启动后直接聚焦输入框
	return RootModel{
		tab:         TabChat,
		lang:        lang,
		Tools:       NewToolStatusModel(lang),
		Tasks:    NewTaskModel(lang),
		Memory:   NewMemoryModel(lang),
		Audit:    NewAuditModel(lang),
		Config:   NewConfigModel(lang),
		Chat:     chat,
		StatusBar: NewStatusBarModel(lang),
		Help:     NewHelpModel(lang),
	}
}

func (m *RootModel) SetLang(lang string) {
	m.lang = i18n.NormalizeLang(lang)
	m.Tools.SetLang(m.lang)
	m.Tasks.SetLang(m.lang)
	m.Memory.SetLang(m.lang)
	m.Audit.SetLang(m.lang)
	m.Config.SetLang(m.lang)
	m.Chat.SetLang(m.lang)
	m.Help.SetLang(m.lang)
	m.StatusBar.SetLang(m.lang)
}

func (m RootModel) Lang() string {
	return m.lang
}

// Init 实现 tea.Model。
func (m RootModel) Init() tea.Cmd {
	return nil
}

// Update 处理键盘导航和子视图更新。
func (m RootModel) Update(msg tea.Msg) (RootModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		if m.Help.IsVisible() {
			var cmd tea.Cmd
			m.Help, cmd = m.Help.Update(msg)
			return m, cmd
		}
		if m.tab == TabConfig && m.Config.IsEditing() {
			break
		}
		if m.tab == TabAudit && m.Audit.IsFiltering() {
			break
		}
		if m.tab == TabTools && m.Tools.IsEditing() {
			break
		}
		if m.tab == TabChat && m.Chat.IsInputFocused() {
			break // let chat handle all keys when input is focused
		}

		switch msg.String() {
		case "?":
			m.Help.Toggle()
			return m, nil
		case "tab", "right":
			m.tab = (m.tab + 1) % TabCount
			if m.tab == TabChat {
				m.Chat.FocusInput()
			}
			return m, nil
		case "shift+tab", "left":
			m.tab = (m.tab - 1 + TabCount) % TabCount
			if m.tab == TabChat {
				m.Chat.FocusInput()
			}
			return m, nil
		}
	}

	// Status bar always gets all messages.
	var sbCmd tea.Cmd
	m.StatusBar, sbCmd = m.StatusBar.Update(msg)

	// Route async result messages to their target view regardless of active tab.
	switch msg.(type) {
	case ToolSkillSearchResultMsg, ToolSkillInstallResultMsg, ToolOperationResultMsg:
		var toolCmd tea.Cmd
		m.Tools, toolCmd = m.Tools.Update(msg)
		if m.tab == TabTools {
			return m, tea.Batch(toolCmd, sbCmd)
		}
		activeCmd := m.updateActiveTab(msg)
		return m, tea.Batch(toolCmd, activeCmd, sbCmd)
	case ChatResponseMsg, ChatStreamMsg, chatTickMsg:
		var chatCmd tea.Cmd
		m.Chat, chatCmd = m.Chat.Update(msg)
		if m.tab == TabChat {
			return m, tea.Batch(chatCmd, sbCmd)
		}
		activeCmd := m.updateActiveTab(msg)
		return m, tea.Batch(chatCmd, activeCmd, sbCmd)
	}

	cmd := m.updateActiveTab(msg)
	return m, tea.Batch(cmd, sbCmd)
}

// View 渲染完整 TUI 界面。
func (m RootModel) View() string {
	if m.width == 0 {
		return i18n.T(i18n.MsgTUIInitializing, m.lang) + "\n"
	}

	tabBar := m.renderTabs()

	contentHeight := m.height - 4
	if contentHeight < 1 {
		contentHeight = 1
	}

	content := ""
	if m.Help.IsVisible() {
		content = m.Help.View()
	} else {
		switch m.tab {
		case TabTools:
			content = m.Tools.View()
		case TabTasks:
			content = m.Tasks.View()
		case TabMemory:
			content = m.Memory.View()
		case TabAudit:
			content = m.Audit.View()
		case TabConfig:
			content = m.Config.View()
		case TabChat:
			content = m.Chat.View()
		}
	}

	contentStyle := lipgloss.NewStyle().
		Width(m.width).
		Height(contentHeight)
	content = contentStyle.Render(content)

	statusBar := m.StatusBar.View(m.width)
	return fmt.Sprintf("%s\n%s\n%s", tabBar, content, statusBar)
}

// renderTabs 渲染 Tab 栏。
func (m RootModel) renderTabs() string {
	activeStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Padding(0, 2)

	inactiveStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Background(lipgloss.Color("236")).
		Padding(0, 2)

	tabNames := [TabCount]string{
		i18n.T(i18n.MsgTUITabChat, m.lang),
		i18n.T(i18n.MsgTUITabTools, m.lang),
		i18n.T(i18n.MsgTUITabSchedule, m.lang),  // "任务" / "Tasks"
		i18n.T(i18n.MsgTUITabMemory, m.lang),
		i18n.T(i18n.MsgTUITabAudit, m.lang),
		i18n.T(i18n.MsgTUITabConfig, m.lang),
	}

	tabs := ""
	for i, name := range tabNames {
		if i == m.tab {
			tabs += activeStyle.Render(name)
		} else {
			tabs += inactiveStyle.Render(name)
		}
	}
	return tabs
}

// ActiveTab 返回当前活跃的 Tab 索引。
func (m RootModel) ActiveTab() int {
	return m.tab
}

// updateActiveTab dispatches a message to the currently active tab's view.
func (m *RootModel) updateActiveTab(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch m.tab {
	case TabTools:
		m.Tools, cmd = m.Tools.Update(msg)
	case TabTasks:
		m.Tasks, cmd = m.Tasks.Update(msg)
	case TabMemory:
		m.Memory, cmd = m.Memory.Update(msg)
	case TabAudit:
		m.Audit, cmd = m.Audit.Update(msg)
	case TabConfig:
		m.Config, cmd = m.Config.Update(msg)
	case TabChat:
		m.Chat, cmd = m.Chat.Update(msg)
	}
	return cmd
}

// SetTab 切换到指定 Tab。
func (m *RootModel) SetTab(tab int) {
	if tab >= 0 && tab < TabCount {
		m.tab = tab
	}
}
