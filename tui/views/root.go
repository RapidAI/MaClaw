package views

import (
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Tab 索引常量。
// Chat is first — it's the primary use case for TUI.
// AgentNet removed — not relevant for standalone TUI.
const (
	TabChat = iota
	TabSessions
	TabTools
	TabSchedule
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
	appConfig corelib.AppConfig // shared config for coding tool wizard

	// 子视图
	Sessions      SessionListModel
	SessionDetail *SessionDetailModel // nil = 不显示详情
	SessionCreate *SessionCreateModel // nil = 不显示创建表单
	CodingSetup   CodingSetupModel   // coding tool launch wizard
	Tools         ToolStatusModel
	Schedule      ScheduleModel
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
		Sessions:    NewSessionListModel(lang),
		CodingSetup: NewCodingSetupModel(),
		Tools:       NewToolStatusModel(lang),
		Schedule: NewScheduleModel(lang),
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
	m.Sessions.SetLang(m.lang)
	m.Tools.SetLang(m.lang)
	m.Schedule.SetLang(m.lang)
	m.Memory.SetLang(m.lang)
	m.Audit.SetLang(m.lang)
	m.Config.SetLang(m.lang)
	m.Chat.SetLang(m.lang)
	m.Help.SetLang(m.lang)
	m.StatusBar.SetLang(m.lang)
	if m.SessionDetail != nil {
		m.SessionDetail.SetLang(m.lang)
	}
	if m.SessionCreate != nil {
		m.SessionCreate.SetLang(m.lang)
	}
}

func (m RootModel) Lang() string {
	return m.lang
}

// SetAppConfig updates the shared config used by the coding tool wizard.
func (m *RootModel) SetAppConfig(cfg corelib.AppConfig) {
	m.appConfig = cfg
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
		if m.SessionDetail != nil {
			d := *m.SessionDetail
			d, _ = d.Update(msg)
			m.SessionDetail = &d
		}
		if m.SessionCreate != nil {
			c := *m.SessionCreate
			c, _ = c.Update(msg)
			m.SessionCreate = &c
		}
	case SessionOpenMsg:
		detail := NewSessionDetailModel(msg.ID, msg.Tool, msg.Title, m.lang)
		m.SessionDetail = &detail
		return m, nil
	case SessionCreateMsg:
		// Show coding tool launch wizard instead of the old form.
		m.CodingSetup.Show(m.appConfig)
		return m, nil
	case SessionCreateSubmitMsg:
		m.SessionCreate = nil
		m.StatusBar.SetMessage(i18n.Tf(i18n.MsgTUISessionCreated, m.lang, msg.Tool, msg.Project))
		return m, nil
	case tea.KeyMsg:
		if m.Help.IsVisible() {
			var cmd tea.Cmd
			m.Help, cmd = m.Help.Update(msg)
			return m, cmd
		}
		if m.SessionCreate != nil {
			if msg.String() == "esc" {
				m.SessionCreate = nil
				return m, nil
			}
			var cmd tea.Cmd
			c := *m.SessionCreate
			c, cmd = c.Update(msg)
			m.SessionCreate = &c
			return m, cmd
		}
		if m.CodingSetup.IsActive() {
			var cmd tea.Cmd
			m.CodingSetup, cmd = m.CodingSetup.Update(msg)
			return m, cmd
		}
		if m.SessionDetail != nil {
			if msg.String() == "esc" {
				m.SessionDetail = nil
				return m, nil
			}
			var cmd tea.Cmd
			d := *m.SessionDetail
			d, cmd = d.Update(msg)
			m.SessionDetail = &d
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
	} else if m.SessionCreate != nil {
		content = m.SessionCreate.View()
	} else if m.CodingSetup.IsActive() {
		content = m.CodingSetup.View()
	} else if m.tab == TabSessions && m.SessionDetail != nil {
		content = m.SessionDetail.View()
	} else {
		switch m.tab {
		case TabSessions:
			content = m.Sessions.View()
		case TabTools:
			content = m.Tools.View()
		case TabSchedule:
			content = m.Schedule.View()
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
		i18n.T(i18n.MsgTUITabSessions, m.lang),
		i18n.T(i18n.MsgTUITabTools, m.lang),
		i18n.T(i18n.MsgTUITabSchedule, m.lang),
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
	case TabSessions:
		m.Sessions, cmd = m.Sessions.Update(msg)
	case TabTools:
		m.Tools, cmd = m.Tools.Update(msg)
	case TabSchedule:
		m.Schedule, cmd = m.Schedule.Update(msg)
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
