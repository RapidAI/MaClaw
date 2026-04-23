package views

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// HelpModel is the help overlay panel.
type HelpModel struct {
	visible bool
	lang    string
}

// NewHelpModel creates a new help panel.
func NewHelpModel(lang string) HelpModel {
	return HelpModel{lang: i18n.NormalizeLang(lang)}
}

func (m *HelpModel) SetLang(lang string) {
	m.lang = i18n.NormalizeLang(lang)
}

// Toggle switches show/hide.
func (m *HelpModel) Toggle() {
	m.visible = !m.visible
}

// IsVisible returns whether the panel is visible.
func (m HelpModel) IsVisible() bool {
	return m.visible
}

// Init implements tea.Model.
func (m HelpModel) Init() tea.Cmd { return nil }

// Update handles keyboard events.
func (m HelpModel) Update(msg tea.Msg) (HelpModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "?" || msg.String() == "esc" {
			m.visible = false
		}
	}
	return m, nil
}

// View renders the help panel.
func (m HelpModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229"))
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	var b strings.Builder
	b.WriteString(titleStyle.Render(i18n.T(i18n.MsgTUIHelpTitle, m.lang)))
	b.WriteString("\n\n")

	sections := []struct {
		name string
		keys []struct{ key, desc string }
	}{
		{i18n.T(i18n.MsgTUIHelpSectionGlobal, m.lang), []struct{ key, desc string }{
			{"Tab / ->", i18n.T(i18n.MsgTUIHelpDescNextTab, m.lang)},
			{"Shift+Tab / <-", i18n.T(i18n.MsgTUIHelpDescPreviousTab, m.lang)},
			{"q", i18n.T(i18n.MsgTUIHelpDescQuit, m.lang)},
			{"Ctrl+C", i18n.T(i18n.MsgTUIHelpDescForceQuit, m.lang)},
			{"?", i18n.T(i18n.MsgTUIHelpDescShowCloseHelp, m.lang)},
		}},
		{i18n.T(i18n.MsgTUIHelpSectionListNavigation, m.lang), []struct{ key, desc string }{
			{"Up / k", i18n.T(i18n.MsgTUIHelpDescMoveUp, m.lang)},
			{"Down / j", i18n.T(i18n.MsgTUIHelpDescMoveDown, m.lang)},
			{"g", i18n.T(i18n.MsgTUIHelpDescJumpTop, m.lang)},
			{"G", i18n.T(i18n.MsgTUIHelpDescJumpBottom, m.lang)},
			{"r", i18n.T(i18n.MsgTUIHelpDescRefresh, m.lang)},
		}},
		{i18n.T(i18n.MsgTUIHelpSectionSessions, m.lang), []struct{ key, desc string }{
			{"Enter", i18n.T(i18n.MsgTUIHelpDescViewDetails, m.lang)},
			{"n / c", i18n.T(i18n.MsgTUIHelpDescNewSession, m.lang)},
			{"d / x", i18n.T(i18n.MsgTUIHelpDescTerminateSession, m.lang)},
		}},
		{i18n.T(i18n.MsgTUIHelpSectionScheduledTasks, m.lang), []struct{ key, desc string }{
			{"p", i18n.T(i18n.MsgTUIHelpDescPauseResume, m.lang)},
			{"d", i18n.T(i18n.MsgTUIHelpDescDelete, m.lang)},
		}},
		{i18n.T(i18n.MsgTUIHelpSectionMemory, m.lang), []struct{ key, desc string }{
			{"d", i18n.T(i18n.MsgTUIHelpDescDelete, m.lang)},
		}},
		{i18n.T(i18n.MsgTUIHelpSectionConfig, m.lang), []struct{ key, desc string }{
			{"Enter", i18n.T(i18n.MsgTUIHelpDescEdit, m.lang)},
			{"Esc", i18n.T(i18n.MsgTUIHelpDescCancelEdit, m.lang)},
		}},
		{i18n.T(i18n.MsgTUIHelpSectionSessionDetail, m.lang), []struct{ key, desc string }{
			{"Up/Down", i18n.T(i18n.MsgTUIHelpDescScroll, m.lang)},
			{"g/G", i18n.T(i18n.MsgTUIHelpDescTopBottom, m.lang)},
			{"Esc", i18n.T(i18n.MsgTUIHelpDescBackToList, m.lang)},
		}},
		{i18n.T(i18n.MsgTUIHelpSectionAIAssistant, m.lang), []struct{ key, desc string }{
			{"i", i18n.T(i18n.MsgTUIHelpDescStartInput, m.lang)},
			{"Enter", i18n.T(i18n.MsgTUIHelpDescSendMessage, m.lang)},
			{"Esc", i18n.T(i18n.MsgTUIHelpDescExitInput, m.lang)},
			{"c", i18n.T(i18n.MsgTUIHelpDescClearHistory, m.lang)},
			{"Up/Down", i18n.T(i18n.MsgTUIHelpDescScrollMessages, m.lang)},
		}},
	}

	for _, sec := range sections {
		b.WriteString(sectionStyle.Render("  " + sec.name))
		b.WriteString("\n")
		for _, kv := range sec.keys {
			b.WriteString("    ")
			b.WriteString(keyStyle.Render(fmt.Sprintf("%-16s", kv.key)))
			b.WriteString(descStyle.Render(kv.desc))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(dimStyle.Render("  " + i18n.T(i18n.MsgTUIHelpClose, m.lang)))
	return b.String()
}
