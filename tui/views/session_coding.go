package views

// session_coding.go provides the coding tool configuration and launch UI.
// Accessed from the Sessions tab when user presses 'n' (new session).
// Shows: tool selector → provider selector → API key input → launch.

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// CodingLaunchMsg is sent when the user confirms a coding tool launch.
type CodingLaunchMsg struct {
	ToolName    string
	Provider    string
	ProjectPath string
}

// CodingConfigSaveMsg is sent when a provider's API key is saved.
// When Launch is true, the tool should be launched after saving.
type CodingConfigSaveMsg struct {
	ToolName    string
	Provider    string
	ApiKey      string
	ProjectPath string
	Launch      bool
}

// CodingSetupModel is the coding tool setup/launch wizard.
type CodingSetupModel struct {
	// Step 0: select tool, Step 1: select provider, Step 2: configure API key, Step 3: select project
	step     int
	tools    []agent.CodingToolInfo
	toolIdx  int
	providers []corelib.ModelConfig
	provIdx  int
	keyInput textinput.Model
	projInput textinput.Model
	appConfig corelib.AppConfig
	message  string
	active   bool // true when this overlay is visible
}

// NewCodingSetupModel creates the coding tool setup wizard.
func NewCodingSetupModel() CodingSetupModel {
	ki := textinput.New()
	ki.Placeholder = "API Key (留空使用已保存的)"
	ki.CharLimit = 200
	ki.Width = 50

	pi := textinput.New()
	pi.Placeholder = "项目路径 (留空使用当前目录)"
	pi.CharLimit = 200
	pi.Width = 50

	return CodingSetupModel{
		tools:    agent.SupportedCodingTools(),
		keyInput: ki,
		projInput: pi,
	}
}

// Show activates the wizard overlay.
func (m *CodingSetupModel) Show(cfg corelib.AppConfig) {
	m.active = true
	m.step = 0
	m.toolIdx = 0
	m.provIdx = 0
	m.message = ""
	m.appConfig = cfg
}

// IsActive returns whether the wizard is visible.
func (m CodingSetupModel) IsActive() bool {
	return m.active
}

// Update handles keyboard events for the wizard.
func (m CodingSetupModel) Update(msg tea.Msg) (CodingSetupModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.step {
		case 0:
			return m.updateToolSelect(msg)
		case 1:
			return m.updateProviderSelect(msg)
		case 2:
			return m.updateKeyInput(msg)
		case 3:
			return m.updateProjectInput(msg)
		}
	}
	return m, nil
}

func (m CodingSetupModel) updateToolSelect(msg tea.KeyMsg) (CodingSetupModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.toolIdx > 0 {
			m.toolIdx--
		}
	case "down", "j":
		if m.toolIdx < len(m.tools)-1 {
			m.toolIdx++
		}
	case "enter":
		// Load providers for selected tool.
		toolName := m.tools[m.toolIdx].Name
		tc := agent.GetToolConfig(m.appConfig, toolName)
		if len(tc.Models) > 0 {
			m.providers = tc.Models
		} else {
			m.providers = agent.DefaultProvidersForTool(toolName)
		}
		// Pre-select current provider.
		m.provIdx = 0
		for i, p := range m.providers {
			if p.ModelName == tc.CurrentModel {
				m.provIdx = i
				break
			}
		}
		m.step = 1
	case "esc":
		m.active = false
	}
	return m, nil
}

func (m CodingSetupModel) updateProviderSelect(msg tea.KeyMsg) (CodingSetupModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.provIdx > 0 {
			m.provIdx--
		}
	case "down", "j":
		if m.provIdx < len(m.providers)-1 {
			m.provIdx++
		}
	case "enter":
		prov := m.providers[m.provIdx]
		if prov.ApiKey != "" {
			// Already has key — skip to project path.
			m.step = 3
			m.projInput.SetValue("")
			m.projInput.Focus()
		} else {
			// Need API key.
			m.step = 2
			m.keyInput.SetValue("")
			m.keyInput.Focus()
		}
	case "esc":
		m.step = 0
	}
	return m, nil
}

func (m CodingSetupModel) updateKeyInput(msg tea.KeyMsg) (CodingSetupModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		key := strings.TrimSpace(m.keyInput.Value())
		if key != "" {
			m.providers[m.provIdx].ApiKey = key
		}
		m.keyInput.Blur()
		m.step = 3
		m.projInput.SetValue("")
		m.projInput.Focus()
		return m, nil
	case "esc":
		m.keyInput.Blur()
		m.step = 1
		return m, nil
	default:
		var cmd tea.Cmd
		m.keyInput, cmd = m.keyInput.Update(msg)
		return m, cmd
	}
}

func (m CodingSetupModel) updateProjectInput(msg tea.KeyMsg) (CodingSetupModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		toolName := m.tools[m.toolIdx].Name
		provName := m.providers[m.provIdx].ModelName
		projPath := strings.TrimSpace(m.projInput.Value())
		m.projInput.Blur()
		m.active = false

		// Save first, then launch. The launch is triggered by app.go
		// when it receives CodingConfigSaveMsg and sees a pending launch.
		return m, func() tea.Msg {
			return CodingConfigSaveMsg{
				ToolName:    toolName,
				Provider:    provName,
				ApiKey:      m.providers[m.provIdx].ApiKey,
				ProjectPath: projPath,
				Launch:      true,
			}
		}
	case "esc":
		m.projInput.Blur()
		m.step = 2
		m.keyInput.Focus()
		return m, nil
	default:
		var cmd tea.Cmd
		m.projInput, cmd = m.projInput.Update(msg)
		return m, cmd
	}
}

// View renders the wizard.
func (m CodingSetupModel) View() string {
	if !m.active {
		return ""
	}

	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("117"))
	sel := lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	normal := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("82"))

	var b strings.Builder

	switch m.step {
	case 0:
		b.WriteString(title.Render("  启动编程工具 — 选择工具") + "\n\n")
		for i, t := range m.tools {
			line := fmt.Sprintf("  %-12s %s", t.Display, t.Binary)
			if i == m.toolIdx {
				b.WriteString(sel.Render(line))
			} else {
				b.WriteString(normal.Render(line))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n" + dim.Render("  ↑↓:选择  Enter:确认  Esc:取消"))

	case 1:
		toolName := m.tools[m.toolIdx].Display
		b.WriteString(title.Render(fmt.Sprintf("  启动 %s — 选择服务商", toolName)) + "\n\n")
		for i, p := range m.providers {
			hasKey := "  "
			if p.ApiKey != "" {
				hasKey = keyStyle.Render("🔑")
			}
			url := p.ModelUrl
			if len(url) > 40 {
				url = url[:37] + "..."
			}
			if p.IsCustom && p.ModelUrl == "" {
				url = dim.Render("(自定义)")
			}
			line := fmt.Sprintf("  %s %-14s %-14s %s", hasKey, p.ModelName, p.ModelId, url)
			if i == m.provIdx {
				b.WriteString(sel.Render(line))
			} else {
				b.WriteString(normal.Render(line))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n" + dim.Render("  ↑↓:选择  Enter:确认  Esc:返回  🔑=已配置 API Key"))

	case 2:
		provName := m.providers[m.provIdx].ModelName
		b.WriteString(title.Render(fmt.Sprintf("  配置 %s API Key", provName)) + "\n\n")
		b.WriteString("  API Key: " + m.keyInput.View() + "\n")
		if m.providers[m.provIdx].ModelUrl != "" {
			b.WriteString("\n  " + dim.Render("URL: "+m.providers[m.provIdx].ModelUrl))
		}
		b.WriteString("\n\n" + dim.Render("  Enter:确认  Esc:返回"))

	case 3:
		b.WriteString(title.Render("  选择项目路径") + "\n\n")
		b.WriteString("  路径: " + m.projInput.View() + "\n")
		b.WriteString("\n" + dim.Render("  留空使用当前目录  Enter:启动  Esc:返回"))
	}

	if m.message != "" {
		b.WriteString("\n\n  " + m.message)
	}

	return b.String()
}
