package views

import (
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// StatusBarModel 底部状态栏。
type StatusBarModel struct {
	hubStatus string // connected, disconnected, connecting
	message   string // 最近的日志/事件消息
	lang      string
}

// NewStatusBarModel 创建状态栏。
func NewStatusBarModel(lang string) StatusBarModel {
	lang = i18n.NormalizeLang(lang)
	return StatusBarModel{
		hubStatus: "disconnected",
		message:   i18n.T(i18n.MsgTUIReady, lang),
		lang:      lang,
	}
}

func (m *StatusBarModel) SetLang(lang string) {
	m.lang = i18n.NormalizeLang(lang)
}

// SetHubStatus 更新 Hub 连接状态。
func (m *StatusBarModel) SetHubStatus(status string) {
	m.hubStatus = status
}

// SetMessage 更新状态消息。
func (m *StatusBarModel) SetMessage(msg string) {
	m.message = msg
}

// Init 实现 tea.Model。
func (m StatusBarModel) Init() tea.Cmd { return nil }

// Update 处理消息。
func (m StatusBarModel) Update(msg tea.Msg) (StatusBarModel, tea.Cmd) {
	return m, nil
}

// View 渲染状态栏（需要宽度参数）。
func (m StatusBarModel) View(width int) string {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Background(lipgloss.Color("236")).
		Width(width)

	hubIcon := "○"
	hubColor := lipgloss.Color("196") // red
	hubLabel := i18n.T(i18n.MsgTUIStatusDisconnectedHub, m.lang)
	switch m.hubStatus {
	case "connected":
		hubIcon = "●"
		hubColor = lipgloss.Color("82") // green
		hubLabel = i18n.T(i18n.MsgTUIStatusConnectedHub, m.lang)
	case "connecting":
		hubIcon = "◌"
		hubColor = lipgloss.Color("226") // yellow
		hubLabel = i18n.T(i18n.MsgTUIStatusConnectingHub, m.lang)
	}

	hubStyle := lipgloss.NewStyle().Foreground(hubColor)
	hub := hubStyle.Render(hubIcon) + " " + hubLabel

	bar := fmt.Sprintf(" %s │ %s │ %s", hub, m.message, i18n.T(i18n.MsgTUIStatusBarHelp, m.lang))
	return style.Render(bar)
}
