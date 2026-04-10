package views

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// AgentNet sub-tab constants.
const (
	AgentNetSubPeers = iota
	AgentNetSubTasks
	AgentNetSubStatus
	AgentNetSubCount
)


// AgentNetPeerItem represents a peer list item.
type AgentNetPeerItem struct {
	PeerID  string
	Addr    string
	Latency string
	Country string
}

// AgentNetTaskItem represents a task list item.
type AgentNetTaskItem struct {
	ID     string
	Status string
	Reward float64
	Title  string
}

// AgentNetStatusInfo holds status information.
type AgentNetStatusInfo struct {
	PeerID   string
	Peers    int
	UnreadDM int
	Version  string
	Uptime   string
	Balance  float64
	Tier     string
	Energy   float64
}

// AgentNetModel is the AgentNet view.
type AgentNetModel struct {
	subTab  int
	peers   []AgentNetPeerItem
	tasks   []AgentNetTaskItem
	status  AgentNetStatusInfo
	cursor  int
	loading bool
	lang    string
}

// NewAgentNetModel creates a new AgentNet view.
func NewAgentNetModel(lang string) AgentNetModel {
	return AgentNetModel{loading: true, lang: i18n.NormalizeLang(lang)}
}

func (m *AgentNetModel) SetLang(lang string) {
	m.lang = i18n.NormalizeLang(lang)
}

// SetPeers updates the peer list.
func (m *AgentNetModel) SetPeers(peers []AgentNetPeerItem) {
	m.peers = peers
	m.loading = false
}

// SetTasks updates the task list.
func (m *AgentNetModel) SetTasks(tasks []AgentNetTaskItem) {
	m.tasks = tasks
	m.loading = false
}

// SetStatus updates the status info.
func (m *AgentNetModel) SetStatus(status AgentNetStatusInfo) {
	m.status = status
	m.loading = false
}

// Init implements tea.Model.
func (m AgentNetModel) Init() tea.Cmd { return nil }

// Update handles keyboard events.
func (m AgentNetModel) Update(msg tea.Msg) (AgentNetModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "1":
			m.subTab = AgentNetSubPeers
			m.cursor = 0
		case "2":
			m.subTab = AgentNetSubTasks
			m.cursor = 0
		case "3":
			m.subTab = AgentNetSubStatus
			m.cursor = 0
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			listLen := m.currentListLen()
			if m.cursor < listLen-1 {
				m.cursor++
			}
		}
	}
	return m, nil
}

func (m AgentNetModel) currentListLen() int {
	switch m.subTab {
	case AgentNetSubPeers:
		return len(m.peers)
	case AgentNetSubTasks:
		return len(m.tasks)
	}
	return 0
}

// View renders the AgentNet view.
func (m AgentNetModel) View() string {
	if m.loading {
		return "  " + i18n.T(i18n.MsgTUIAgentNetLoading, m.lang)
	}

	// sub-tab bar
	subBar := m.renderSubTabs()

	var content string
	switch m.subTab {
	case AgentNetSubPeers:
		content = m.viewPeers()
	case AgentNetSubTasks:
		content = m.viewTasks()
	case AgentNetSubStatus:
		content = m.viewStatus()
	}

	return subBar + "\n" + content
}

func (m AgentNetModel) renderSubTabs() string {
	activeStyle := lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57")).Padding(0, 1)
	inactiveStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).Background(lipgloss.Color("238")).Padding(0, 1)

	tabs := "  "
	for i, name := range [AgentNetSubCount]string{
		i18n.T(i18n.MsgTUIAgentNetTabPeers, m.lang),
		i18n.T(i18n.MsgTUIAgentNetTabTasks, m.lang),
		i18n.T(i18n.MsgTUIAgentNetTabStatus, m.lang),
	} {
		label := fmt.Sprintf("%d:%s", i+1, name)
		if i == m.subTab {
			tabs += activeStyle.Render(label)
		} else {
			tabs += inactiveStyle.Render(label)
		}
	}
	return tabs
}

func (m AgentNetModel) viewPeers() string {
	if len(m.peers) == 0 {
		return "  " + i18n.T(i18n.MsgTUIAgentNetNoPeers, m.lang)
	}
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	var b strings.Builder
	b.WriteString(headerStyle.Render(fmt.Sprintf("  %-20s %-20s %-10s %s",
		i18n.T(i18n.MsgTUIAgentNetHeaderPeerID, m.lang),
		i18n.T(i18n.MsgTUIAgentNetHeaderAddr, m.lang),
		i18n.T(i18n.MsgTUIAgentNetHeaderLatency, m.lang),
		i18n.T(i18n.MsgTUIAgentNetHeaderCountry, m.lang))))
	b.WriteString("\n  " + strings.Repeat("-", 65) + "\n")

	for i, p := range m.peers {
		line := fmt.Sprintf("  %-20s %-20s %-10s %s",
			truncate(p.PeerID, 20), truncate(p.Addr, 20), p.Latency, p.Country)
		if i == m.cursor {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n  " + i18n.Tf(i18n.MsgTUIAgentNetFooterPeers, m.lang, len(m.peers)))
	return b.String()
}

func (m AgentNetModel) viewTasks() string {
	if len(m.tasks) == 0 {
		return "  " + i18n.T(i18n.MsgTUIAgentNetNoTasks, m.lang)
	}
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	var b strings.Builder
	b.WriteString(headerStyle.Render(fmt.Sprintf("  %-20s %-10s %-8s %s",
		i18n.T(i18n.MsgTUISessionHeaderID, m.lang),
		i18n.T(i18n.MsgTUISessionHeaderStatus, m.lang),
		i18n.T(i18n.MsgTUIAgentNetHeaderReward, m.lang),
		i18n.T(i18n.MsgTUISessionHeaderTitle, m.lang))))
	b.WriteString("\n  " + strings.Repeat("-", 65) + "\n")

	for i, t := range m.tasks {
		line := fmt.Sprintf("  %-20s %-10s %-8.1f %s",
			truncate(t.ID, 20), t.Status, t.Reward, truncate(t.Title, 30))
		if i == m.cursor {
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n  " + i18n.Tf(i18n.MsgTUIAgentNetFooterTasks, m.lang, len(m.tasks)))
	return b.String()
}

func (m AgentNetModel) viewStatus() string {
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	valStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("229"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	s := m.status
	var b strings.Builder
	b.WriteString(labelStyle.Render("  " + i18n.T(i18n.MsgTUIAgentNetStatusTitle, m.lang)))
	b.WriteString("\n  " + strings.Repeat("-", 40) + "\n")
	b.WriteString(fmt.Sprintf("  %s:    %s\n", i18n.T(i18n.MsgTUIAgentNetStatusPeerID, m.lang), valStyle.Render(s.PeerID)))
	b.WriteString(fmt.Sprintf("  %s:     %s\n", i18n.T(i18n.MsgTUIAgentNetStatusPeers, m.lang), valStyle.Render(fmt.Sprintf("%d", s.Peers))))
	b.WriteString(fmt.Sprintf("  %s:    %s\n", i18n.T(i18n.MsgTUIAgentNetStatusUnread, m.lang), valStyle.Render(fmt.Sprintf("%d", s.UnreadDM))))
	b.WriteString(fmt.Sprintf("  %s:   %s\n", i18n.T(i18n.MsgTUIAgentNetStatusVersion, m.lang), valStyle.Render(s.Version)))
	b.WriteString(fmt.Sprintf("  %s:    %s\n", i18n.T(i18n.MsgTUIAgentNetStatusUptime, m.lang), valStyle.Render(s.Uptime)))
	b.WriteString("\n")
	b.WriteString(labelStyle.Render("  " + i18n.T(i18n.MsgTUIAgentNetCreditsTitle, m.lang)))
	b.WriteString("\n  " + strings.Repeat("-", 40) + "\n")
	b.WriteString(fmt.Sprintf("  %s:   %s\n", i18n.T(i18n.MsgTUIAgentNetStatusBalance, m.lang), valStyle.Render(fmt.Sprintf("%.2f", s.Balance))))
	b.WriteString(fmt.Sprintf("  %s:      %s\n", i18n.T(i18n.MsgTUIAgentNetStatusTier, m.lang), valStyle.Render(s.Tier)))
	b.WriteString(fmt.Sprintf("  %s:    %s\n", i18n.T(i18n.MsgTUIAgentNetStatusEnergy, m.lang), valStyle.Render(fmt.Sprintf("%.2f", s.Energy))))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  " + i18n.T(i18n.MsgTUIAgentNetFooterStatus, m.lang)))
	return b.String()
}
