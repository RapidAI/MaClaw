package views

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	TabOnboarding = iota
	TabChat
	TabTools
	TabTasks
	TabServiceRedeem
	TabConfig
	TabCount
)

type RootModel struct {
	width  int
	height int
	tab    int
	lang   string

	Onboarding OnboardingModel
	Tools      ToolStatusModel
	Tasks      TaskModel
	Service    ServiceRedeemModel
	Config     ConfigModel
	Chat       ChatModel
	StatusBar  StatusBarModel
	Help       HelpModel
}

func NewRootModel(lang string) RootModel {
	lang = i18n.NormalizeLang(lang)
	chat := NewChatModel(lang)
	chat.FocusInput()
	m := RootModel{
		width:      80,
		height:     24,
		tab:        TabChat,
		lang:       lang,
		Onboarding: NewOnboardingModel(lang),
		Tools:      NewToolStatusModel(lang),
		Tasks:      NewTaskModel(lang),
		Service:    NewServiceRedeemModel(lang),
		Config:     NewConfigModel(lang),
		Chat:       chat,
		StatusBar:  NewStatusBarModel(lang),
		Help:       NewHelpModel(lang),
	}
	_ = m.updateChildSizes(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	return m
}

func (m *RootModel) SetLang(lang string) {
	m.lang = i18n.NormalizeLang(lang)
	m.Onboarding.SetLang(m.lang)
	m.Tools.SetLang(m.lang)
	m.Tasks.SetLang(m.lang)
	m.Service.SetLang(m.lang)
	m.Config.SetLang(m.lang)
	m.Chat.SetLang(m.lang)
	m.Help.SetLang(m.lang)
	m.StatusBar.SetLang(m.lang)
}

func (m RootModel) Lang() string { return m.lang }

func (m RootModel) Init() tea.Cmd { return nil }

func (m RootModel) Update(msg tea.Msg) (RootModel, tea.Cmd) {
	switch msg := msg.(type) {
	case TaskOpenToolsMsg:
		m.SetTab(TabTools)
		m.Tools.FocusMCP()
		return m, nil
	case TaskOpenChatMsg:
		m.SetTab(TabChat)
		return m, nil
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.Help.SetViewport(msg.Width, max(1, msg.Height-4))
		return m, m.updateChildSizes(msg)
	case tea.KeyMsg:
		if tab, ok := rootDirectTabShortcut(msg.String()); ok {
			previousTab := m.tab
			m.Help.visible = false
			m.SetTab(tab)
			if previousTab == TabServiceRedeem && tab == TabTools && m.Service.NeedsMCPNextStep() {
				m.Tools.StartMCPLocalTemplate()
			}
			return m, nil
		}
		switch msg.String() {
		case "ctrl+right", "ctrl+tab":
			m.Help.visible = false
			m.SetTab((m.tab + 1) % TabCount)
			return m, nil
		case "ctrl+left", "ctrl+shift+tab":
			m.Help.visible = false
			m.SetTab((m.tab - 1 + TabCount) % TabCount)
			return m, nil
		}
		if msg.String() == "?" && !m.activeViewIsEditing() {
			m.Help.Toggle()
			return m, nil
		}

		if m.Help.IsVisible() {
			var cmd tea.Cmd
			m.Help, cmd = m.Help.Update(msg)
			return m, cmd
		}
		if m.tab == TabConfig || m.tab == TabOnboarding {
			break
		}
		if m.tab == TabServiceRedeem && m.Service.IsEditing() {
			break
		}
		if m.tab == TabTools && m.Tools.IsEditing() {
			break
		}
		if m.tab == TabChat && m.Chat.IsInputFocused() {
			break
		}

		switch msg.String() {
		case "?":
			m.Help.Toggle()
			return m, nil
		case "tab", "right":
			m.SetTab((m.tab + 1) % TabCount)
			return m, nil
		case "shift+tab", "left":
			m.SetTab((m.tab - 1 + TabCount) % TabCount)
			return m, nil
		}
	}

	var sbCmd tea.Cmd
	m.StatusBar, sbCmd = m.StatusBar.Update(msg)

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

func (m *RootModel) updateChildSizes(msg tea.WindowSizeMsg) tea.Cmd {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	m.Onboarding, cmd = m.Onboarding.Update(msg)
	cmds = append(cmds, cmd)
	m.Tools, cmd = m.Tools.Update(msg)
	cmds = append(cmds, cmd)
	m.Tasks, cmd = m.Tasks.Update(msg)
	cmds = append(cmds, cmd)
	m.Service, cmd = m.Service.Update(msg)
	cmds = append(cmds, cmd)
	m.Config, cmd = m.Config.Update(msg)
	cmds = append(cmds, cmd)
	m.Chat, cmd = m.Chat.Update(msg)
	cmds = append(cmds, cmd)
	return tea.Batch(cmds...)
}

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
		content = m.Help.ViewWithSize(contentHeight, m.width)
	} else {
		switch m.tab {
		case TabOnboarding:
			content = m.Onboarding.View()
		case TabTools:
			content = m.Tools.View()
		case TabTasks:
			content = m.Tasks.View()
		case TabServiceRedeem:
			content = m.Service.View()
		case TabConfig:
			content = m.Config.View()
		case TabChat:
			content = m.Chat.View()
		}
	}

	content = limitRenderedLines(content, contentHeight)
	content = lipgloss.NewStyle().Width(m.width).Height(contentHeight).Render(content)
	statusBar := m.StatusBar.View(m.width)
	return fmt.Sprintf("%s\n%s\n%s", tabBar, content, statusBar)
}

func limitRenderedLines(s string, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		return s
	}
	return strings.Join(lines[:maxLines], "\n")
}

func (m RootModel) renderTabs() string {
	padding := 2
	if m.width < 88 {
		padding = 1
	}
	if m.width < 56 {
		padding = 0
	}
	activeStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57")).Padding(0, padding)
	inactiveStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("236")).Padding(0, padding)

	tabNames := [TabCount]string{
		onboardingTabName(m.lang),
		i18n.T(i18n.MsgTUITabChat, m.lang),
		i18n.T(i18n.MsgTUITabTools, m.lang),
		i18n.T(i18n.MsgTUITabSchedule, m.lang),
		serviceRedeemTabName(m.lang),
		i18n.T(i18n.MsgTUITabConfig, m.lang),
	}

	var tabs strings.Builder
	for i, name := range tabNames {
		label := rootTabLabel(i, name, m.width, m.lang)
		if i == m.tab {
			tabs.WriteString(activeStyle.Render(label))
		} else {
			tabs.WriteString(inactiveStyle.Render(label))
		}
	}
	return fitRenderedLines(tabs.String(), m.width)
}

func rootTabLabel(idx int, name string, width int, lang string) string {
	if width >= 88 {
		return fmt.Sprintf("F%d %s", idx+1, name)
	}
	if width >= 56 {
		return name
	}
	return fmt.Sprintf("F%d%s", idx+1, rootMiniTabName(idx, lang))
}

func rootMiniTabName(idx int, lang string) string {
	if i18n.NormalizeLang(lang) == "en" {
		labels := [TabCount]string{"S", "C", "T", "J", "R", "G"}
		return labels[idx]
	}
	labels := [TabCount]string{"初", "聊", "工", "任", "兑", "设"}
	return labels[idx]
}

func rootDirectTabShortcut(key string) (int, bool) {
	switch key {
	case "f1", "alt+1":
		return TabOnboarding, true
	case "f2", "alt+2":
		return TabChat, true
	case "f3", "alt+3":
		return TabTools, true
	case "f4", "alt+4":
		return TabTasks, true
	case "f5", "alt+5":
		return TabServiceRedeem, true
	case "f6", "alt+6":
		return TabConfig, true
	}
	return 0, false
}

func onboardingTabName(lang string) string {
	if i18n.NormalizeLang(lang) == "en" {
		return "Setup"
	}
	return "初始化"
}

func serviceRedeemTabName(lang string) string {
	if i18n.NormalizeLang(lang) == "en" {
		return "Redeem"
	}
	return "服务兑换"
}

func (m RootModel) ActiveTab() int { return m.tab }

func (m *RootModel) updateActiveTab(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch m.tab {
	case TabOnboarding:
		m.Onboarding, cmd = m.Onboarding.Update(msg)
	case TabTools:
		m.Tools, cmd = m.Tools.Update(msg)
	case TabTasks:
		m.Tasks, cmd = m.Tasks.Update(msg)
	case TabServiceRedeem:
		m.Service, cmd = m.Service.Update(msg)
	case TabConfig:
		m.Config, cmd = m.Config.Update(msg)
	case TabChat:
		m.Chat, cmd = m.Chat.Update(msg)
	}
	return cmd
}

// IsEditing returns true when the active view is in an editing/input mode
// (e.g. chat input focused, config field editing). Used by the top-level
// quit handler to decide whether 'q' should quit or be typed.
func (m RootModel) IsEditing() bool {
	return m.activeViewIsEditing()
}

// AcceptsTextInput returns true when the active view has a focused text input
// that would consume single-character keys. This is broader than IsEditing —
// e.g. ServiceRedeem has an always-focused input but IsEditing returns false
// to allow Tab navigation.
func (m RootModel) AcceptsTextInput() bool {
	switch m.tab {
	case TabOnboarding:
		return m.Onboarding.IsEditing()
	case TabTools:
		return m.Tools.IsEditing()
	case TabServiceRedeem:
		return true // always has a focused code input
	case TabConfig:
		return m.Config.IsEditing()
	case TabChat:
		return m.Chat.IsInputFocused()
	default:
		return false
	}
}

func (m RootModel) activeViewIsEditing() bool {
	switch m.tab {
	case TabOnboarding:
		return m.Onboarding.IsEditing()
	case TabTools:
		return m.Tools.IsEditing()
	case TabServiceRedeem:
		return m.Service.IsEditing()
	case TabConfig:
		return m.Config.IsEditing()
	case TabChat:
		return m.Chat.IsInputFocused()
	default:
		return false
	}
}

func (m *RootModel) SetTab(tab int) {
	if tab >= 0 && tab < TabCount {
		m.tab = tab
		if m.tab == TabChat {
			m.Chat.FocusInput()
		}
	}
}
