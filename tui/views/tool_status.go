package views

// tool_status.go implements the "工具" tab with two sub-tabs: Skill and MCP.
// - Skill: search SkillHub, install, list installed skills
// - MCP: add local/remote MCP servers, list configured servers

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Sub-tab constants.
const (
	ToolSubSkill = iota
	ToolSubMCP
	ToolSubCount
)

// ToolInfo 工具状态信息（legacy, kept for compatibility）。
type ToolInfo struct {
	Name      string
	Available bool
	Version   string
	Path      string
}

// SkillItem represents an installed skill for display.
type SkillItem struct {
	Name        string
	Description string
	Status      string // "active", "disabled", "needs_setup"
	Source      string
	Publisher   string
}

// SkillSearchResult represents a search result from SkillHub.
type SkillSearchResult struct {
	ID        string
	Name      string
	Version   string
	Rating    float64
	Downloads int
	Trust     string
}

// MCPItem represents a configured MCP server for display.
type MCPItem struct {
	ID       string
	Name     string
	Type     string // "local" or "remote"
	Status   string // "running", "stopped", "error"
	Endpoint string // URL for remote, command for local
}

// --- Messages for async operations ---

// ToolSkillSearchMsg triggers a skill search.
type ToolSkillSearchMsg struct{ Query string }

// ToolSkillSearchResultMsg returns search results.
type ToolSkillSearchResultMsg struct {
	Results []SkillSearchResult
	Error   string
}

// ToolSkillInstallMsg triggers a skill install.
type ToolSkillInstallMsg struct {
	SkillID string
	HubURL  string
}

// ToolSkillInstallResultMsg returns install result.
type ToolSkillInstallResultMsg struct {
	Name  string
	Error string
}

// ToolMCPAddMsg triggers adding an MCP server.
type ToolMCPAddMsg struct {
	Entry corelib.LocalMCPServerEntry
}

// ToolMCPAddRemoteMsg triggers adding a remote MCP server.
type ToolMCPAddRemoteMsg struct {
	Entry corelib.MCPServerEntry
}

// ToolRefreshMsg 请求刷新工具状态。
type ToolRefreshMsg struct{}

// ToolStatusModel 工具管理视图（Skill + MCP 子标签）。
type ToolStatusModel struct {
	subTab int
	lang   string

	// Skill sub-tab state
	skills       []SkillItem
	skillCursor  int
	skillSearch  textinput.Model
	skillSearching bool
	skillResults []SkillSearchResult
	skillResultCursor int
	skillMessage string // status message

	// MCP sub-tab state
	mcpServers   []MCPItem
	mcpCursor    int
	mcpAdding    bool   // true when in add-MCP form
	mcpAddType   int    // 0=local, 1=remote
	mcpInputs    []textinput.Model // text input fields
	mcpFocused   int    // which field is focused
	mcpAuthIdx   int    // selected auth type index (remote form only)
	mcpMessage   string // status message

	// Legacy
	tools   []ToolInfo
	loading bool
}

// NewToolStatusModel 创建工具状态视图。
func NewToolStatusModel(lang string) ToolStatusModel {
	lang = i18n.NormalizeLang(lang)
	si := textinput.New()
	si.Placeholder = "搜索 Skill..."
	si.CharLimit = 100
	si.Width = 40
	return ToolStatusModel{
		subTab:      ToolSubSkill,
		lang:        lang,
		skillSearch: si,
		loading:     true,
	}
}

func (m *ToolStatusModel) SetLang(lang string) {
	m.lang = i18n.NormalizeLang(lang)
}

// SetTools 更新工具列表（legacy）。
func (m *ToolStatusModel) SetTools(tools []ToolInfo) {
	m.tools = tools
	m.loading = false
}

// SetSkills updates the installed skill list.
func (m *ToolStatusModel) SetSkills(skills []SkillItem) {
	m.skills = skills
	m.loading = false
}

// SetMCPServers updates the MCP server list.
func (m *ToolStatusModel) SetMCPServers(servers []MCPItem) {
	m.mcpServers = servers
}

// Init 实现 tea.Model。
func (m ToolStatusModel) Init() tea.Cmd { return nil }

// Update 处理键盘事件。
func (m ToolStatusModel) Update(msg tea.Msg) (ToolStatusModel, tea.Cmd) {
	switch msg := msg.(type) {
	case ToolSkillSearchResultMsg:
		m.skillSearching = false
		if msg.Error != "" {
			m.skillMessage = "❌ " + msg.Error
			m.skillResults = nil
		} else {
			m.skillResults = msg.Results
			m.skillResultCursor = 0
			if len(msg.Results) == 0 {
				m.skillMessage = "未找到匹配的 Skill"
			} else {
				m.skillMessage = fmt.Sprintf("找到 %d 个结果", len(msg.Results))
			}
		}
		return m, nil

	case ToolSkillInstallResultMsg:
		if msg.Error != "" {
			m.skillMessage = "❌ " + msg.Error
		} else {
			m.skillMessage = "✅ 已安装: " + msg.Name
		}
		return m, nil

	case ToolOperationResultMsg:
		message := msg.Message
		if !msg.Success {
			message = "❌ " + message
		} else {
			message = "✅ " + message
		}
		if msg.Tab == ToolSubSkill {
			m.skillMessage = message
		} else {
			m.mcpMessage = message
		}
		return m, nil

	case tea.KeyMsg:
		// Sub-tab switching: 1=Skill, 2=MCP
		if !m.skillSearch.Focused() && !m.mcpAdding {
			switch msg.String() {
			case "1":
				m.subTab = ToolSubSkill
				return m, nil
			case "2":
				m.subTab = ToolSubMCP
				return m, nil
			}
		}

		if m.subTab == ToolSubSkill {
			return m.updateSkill(msg)
		}
		return m.updateMCP(msg)
	}
	return m, nil
}

func (m ToolStatusModel) updateSkill(msg tea.KeyMsg) (ToolStatusModel, tea.Cmd) {
	// Search input focused
	if m.skillSearch.Focused() {
		switch msg.String() {
		case "enter":
			query := strings.TrimSpace(m.skillSearch.Value())
			if query != "" {
				m.skillSearching = true
				m.skillMessage = "🔍 搜索中..."
				m.skillSearch.Blur()
				return m, func() tea.Msg { return ToolSkillSearchMsg{Query: query} }
			}
			return m, nil
		case "esc":
			m.skillSearch.Blur()
			return m, nil
		default:
			var cmd tea.Cmd
			m.skillSearch, cmd = m.skillSearch.Update(msg)
			return m, cmd
		}
	}

	// Normal mode
	switch msg.String() {
	case "s", "/":
		m.skillSearch.Focus()
		return m, nil
	case "up", "k":
		if len(m.skillResults) > 0 {
			if m.skillResultCursor > 0 {
				m.skillResultCursor--
			}
		} else if m.skillCursor > 0 {
			m.skillCursor--
		}
	case "down", "j":
		if len(m.skillResults) > 0 {
			if m.skillResultCursor < len(m.skillResults)-1 {
				m.skillResultCursor++
			}
		} else if m.skillCursor < len(m.skills)-1 {
			m.skillCursor++
		}
	case "enter":
		// Install selected search result
		if len(m.skillResults) > 0 && m.skillResultCursor < len(m.skillResults) {
			sr := m.skillResults[m.skillResultCursor]
			m.skillMessage = "📦 安装中: " + sr.Name
			return m, func() tea.Msg {
				return ToolSkillInstallMsg{SkillID: sr.ID}
			}
		}
	case "esc":
		// Clear search results, go back to installed list
		if len(m.skillResults) > 0 {
			m.skillResults = nil
			m.skillMessage = ""
		}
	case "r":
		return m, func() tea.Msg { return ToolRefreshMsg{} }
	}
	return m, nil
}

func (m ToolStatusModel) updateMCP(msg tea.KeyMsg) (ToolStatusModel, tea.Cmd) {
	// In add-MCP form
	if m.mcpAdding {
		return m.updateMCPForm(msg)
	}

	switch msg.String() {
	case "a":
		// Start add-MCP form (local)
		m.mcpAdding = true
		m.mcpAddType = 0
		m.mcpInputs = buildMCPLocalInputs()
		m.mcpFocused = 0
		m.mcpInputs[0].Focus()
		return m, nil
	case "A":
		// Start add-MCP form (remote): name, url, auth_secret (3 text inputs + auth type selector)
		m.mcpAdding = true
		m.mcpAddType = 1
		m.mcpInputs = buildMCPRemoteInputs()
		m.mcpAuthIdx = 0 // default "none"
		m.mcpFocused = 0
		m.mcpInputs[0].Focus()
		return m, nil
	case "up", "k":
		if m.mcpCursor > 0 {
			m.mcpCursor--
		}
	case "down", "j":
		if m.mcpCursor < len(m.mcpServers)-1 {
			m.mcpCursor++
		}
	case "r":
		return m, func() tea.Msg { return ToolRefreshMsg{} }
	}
	return m, nil
}

var mcpAuthTypes = []string{"none", "api_key", "bearer"}

func (m ToolStatusModel) updateMCPForm(msg tea.KeyMsg) (ToolStatusModel, tea.Cmd) {
	totalFields := len(m.mcpInputs)
	if m.mcpAddType == 1 {
		totalFields = 4 // name, url, auth_type(selector), auth_secret
	}

	switch msg.String() {
	case "esc":
		m.mcpAdding = false
		m.mcpInputs = nil
		return m, nil
	case "tab", "down":
		// Blur current text input if applicable.
		if m.mcpAddType == 1 && m.mcpFocused == 2 {
			// auth_type selector — no textinput to blur
		} else {
			inputIdx := m.mcpFocused
			if m.mcpAddType == 1 && m.mcpFocused > 2 {
				inputIdx = m.mcpFocused - 1 // skip selector
			}
			if inputIdx < len(m.mcpInputs) {
				m.mcpInputs[inputIdx].Blur()
			}
		}
		m.mcpFocused = (m.mcpFocused + 1) % totalFields
		// Focus new text input if applicable.
		if m.mcpAddType == 1 && m.mcpFocused == 2 {
			// auth_type selector — no textinput to focus
		} else {
			inputIdx := m.mcpFocused
			if m.mcpAddType == 1 && m.mcpFocused > 2 {
				inputIdx = m.mcpFocused - 1
			}
			if inputIdx < len(m.mcpInputs) {
				m.mcpInputs[inputIdx].Focus()
			}
		}
		return m, nil
	case "shift+tab", "up":
		if m.mcpAddType == 1 && m.mcpFocused == 2 {
			// leaving selector
		} else {
			inputIdx := m.mcpFocused
			if m.mcpAddType == 1 && m.mcpFocused > 2 {
				inputIdx = m.mcpFocused - 1
			}
			if inputIdx < len(m.mcpInputs) {
				m.mcpInputs[inputIdx].Blur()
			}
		}
		m.mcpFocused = (m.mcpFocused - 1 + totalFields) % totalFields
		if m.mcpAddType == 1 && m.mcpFocused == 2 {
			// entering selector
		} else {
			inputIdx := m.mcpFocused
			if m.mcpAddType == 1 && m.mcpFocused > 2 {
				inputIdx = m.mcpFocused - 1
			}
			if inputIdx < len(m.mcpInputs) {
				m.mcpInputs[inputIdx].Focus()
			}
		}
		return m, nil
	case "left":
		// Cycle auth type selector left.
		if m.mcpAddType == 1 && m.mcpFocused == 2 {
			m.mcpAuthIdx = (m.mcpAuthIdx - 1 + len(mcpAuthTypes)) % len(mcpAuthTypes)
			return m, nil
		}
	case "right":
		if m.mcpAddType == 1 && m.mcpFocused == 2 {
			m.mcpAuthIdx = (m.mcpAuthIdx + 1) % len(mcpAuthTypes)
			return m, nil
		}
	case "enter":
		if m.mcpAddType == 0 {
			return m.submitMCPLocal()
		}
		return m.submitMCPRemote()
	}

	// Delegate to the focused textinput (skip if on selector field).
	if m.mcpAddType == 1 && m.mcpFocused == 2 {
		return m, nil
	}
	inputIdx := m.mcpFocused
	if m.mcpAddType == 1 && m.mcpFocused > 2 {
		inputIdx = m.mcpFocused - 1
	}
	if inputIdx < len(m.mcpInputs) {
		var cmd tea.Cmd
		m.mcpInputs[inputIdx], cmd = m.mcpInputs[inputIdx].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m ToolStatusModel) submitMCPLocal() (ToolStatusModel, tea.Cmd) {
	name := strings.TrimSpace(m.mcpInputs[0].Value())
	command := strings.TrimSpace(m.mcpInputs[1].Value())
	argsStr := strings.TrimSpace(m.mcpInputs[2].Value())
	envStr := strings.TrimSpace(m.mcpInputs[3].Value())

	if name == "" || command == "" {
		m.mcpMessage = "❌ 名称和命令为必填项"
		return m, nil
	}

	var args []string
	if argsStr != "" {
		args = strings.Fields(argsStr)
	}
	env := parseEnvString(envStr)

	entry := corelib.LocalMCPServerEntry{
		Name:    name,
		Command: command,
		Args:    args,
		Env:     env,
	}

	m.mcpAdding = false
	m.mcpInputs = nil
	m.mcpMessage = "📦 添加中: " + name
	return m, func() tea.Msg { return ToolMCPAddMsg{Entry: entry} }
}

func (m ToolStatusModel) submitMCPRemote() (ToolStatusModel, tea.Cmd) {
	name := strings.TrimSpace(m.mcpInputs[0].Value())
	url := strings.TrimSpace(m.mcpInputs[1].Value())
	authType := mcpAuthTypes[m.mcpAuthIdx]
	authSecret := strings.TrimSpace(m.mcpInputs[2].Value()) // index 2 in inputs = secret (3rd text field)

	if name == "" || url == "" {
		m.mcpMessage = "❌ 名称和 URL 为必填项"
		return m, nil
	}

	entry := corelib.MCPServerEntry{
		Name:        name,
		EndpointURL: url,
		AuthType:    authType,
		AuthSecret:  authSecret,
	}

	m.mcpAdding = false
	m.mcpInputs = nil
	m.mcpMessage = "📦 添加中: " + name
	return m, func() tea.Msg { return ToolMCPAddRemoteMsg{Entry: entry} }
}

// --- View ---

func (m ToolStatusModel) View() string {
	var b strings.Builder

	// Sub-tab bar
	b.WriteString(m.renderSubTabs())
	b.WriteString("\n\n")

	if m.subTab == ToolSubSkill {
		b.WriteString(m.viewSkill())
	} else {
		b.WriteString(m.viewMCP())
	}

	return b.String()
}

func (m ToolStatusModel) renderSubTabs() string {
	active := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57")).Padding(0, 1)
	inactive := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("238")).Padding(0, 1)

	names := [ToolSubCount]string{"Skill", "MCP"}
	var tabs string
	for i, name := range names {
		if i == m.subTab {
			tabs += active.Render(name)
		} else {
			tabs += inactive.Render(name)
		}
		tabs += " "
	}
	return "  " + tabs
}

func (m ToolStatusModel) viewSkill() string {
	var b strings.Builder
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sel := lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	ok := lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	// Search bar
	b.WriteString("  " + m.skillSearch.View() + "\n")
	if m.skillMessage != "" {
		b.WriteString("  " + m.skillMessage + "\n")
	}
	b.WriteString("\n")

	// Search results (if any)
	if len(m.skillResults) > 0 {
		b.WriteString(dim.Render("  搜索结果（Enter 安装，Esc 返回）:") + "\n")
		for i, sr := range m.skillResults {
			rating := fmt.Sprintf("★%.1f", sr.Rating)
			line := fmt.Sprintf("  %-22s %-8s %-6s %-5s %s",
				truncate(sr.Name, 22), sr.Version, sr.Trust, rating, fmt.Sprintf("%d↓", sr.Downloads))
			if i == m.skillResultCursor {
				b.WriteString(sel.Render(line))
			} else {
				b.WriteString(line)
			}
			b.WriteString("\n")
		}
		return b.String()
	}

	// Installed skills list
	if len(m.skills) == 0 {
		b.WriteString(dim.Render("  暂无已安装的 Skill。按 s 搜索 SkillHub。") + "\n")
		return b.String()
	}

	b.WriteString(dim.Render(fmt.Sprintf("  已安装 %d 个 Skill:", len(m.skills))) + "\n\n")
	for i, sk := range m.skills {
		status := ok.Render("●")
		if sk.Status == "disabled" {
			status = dim.Render("○")
		} else if sk.Status == "needs_setup" {
			status = warn.Render("◌")
		}
		name := sk.Name
		if sk.Publisher != "" {
			name = sk.Publisher + ":" + sk.Name
		}
		desc := truncate(sk.Description, 40)
		line := fmt.Sprintf("  %s %-28s %s", status, name, desc)
		if i == m.skillCursor {
			b.WriteString(sel.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	b.WriteString("\n" + dim.Render("  s:搜索  r:刷新  1/2:切换子标签"))
	return b.String()
}

func (m ToolStatusModel) viewMCP() string {
	var b strings.Builder
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sel := lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	ok := lipgloss.NewStyle().Foreground(lipgloss.Color("82"))

	if m.mcpAdding {
		return m.viewMCPForm()
	}

	if m.mcpMessage != "" {
		b.WriteString("  " + m.mcpMessage + "\n\n")
	}

	if len(m.mcpServers) == 0 {
		b.WriteString(dim.Render("  暂无 MCP 服务器。按 a 添加本地，A 添加远程。") + "\n")
		return b.String()
	}

	b.WriteString(dim.Render(fmt.Sprintf("  已配置 %d 个 MCP 服务器:", len(m.mcpServers))) + "\n\n")
	for i, srv := range m.mcpServers {
		typeLabel := "本地"
		if srv.Type == "remote" {
			typeLabel = "远程"
		}
		status := ok.Render("●")
		if srv.Status == "stopped" {
			status = dim.Render("○")
		}
		line := fmt.Sprintf("  %s [%s] %-20s %s", status, typeLabel, srv.Name, truncate(srv.Endpoint, 40))
		if i == m.mcpCursor {
			b.WriteString(sel.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	b.WriteString("\n" + dim.Render("  a:添加本地  A:添加远程  r:刷新  1/2:切换子标签"))
	return b.String()
}

func (m ToolStatusModel) viewMCPForm() string {
	var b strings.Builder
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("117"))

	if m.mcpAddType == 0 {
		b.WriteString(title.Render("  添加本地 MCP 服务器") + "\n\n")
		labels := []string{"名称:", "命令:", "参数:", "环境变量:"}
		for i, input := range m.mcpInputs {
			b.WriteString(fmt.Sprintf("  %-10s %s\n", labels[i], input.View()))
		}
		b.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(
			"参数用空格分隔，环境变量格式: KEY=VALUE KEY2=VALUE2"))
	} else {
		b.WriteString(title.Render("  添加远程 MCP 服务器") + "\n\n")
		labels := []string{"名称:", "URL:", "认证类型:", "密钥/Token:"}
		optActive := lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
		optNormal := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
		for i, label := range labels {
			b.WriteString(fmt.Sprintf("  %-10s ", label))
			if i == 2 {
				// Auth type selector.
				for j, opt := range mcpAuthTypes {
					if j > 0 {
						b.WriteString("  ")
					}
					if j == m.mcpAuthIdx {
						b.WriteString(optActive.Render(" " + opt + " "))
					} else {
						b.WriteString(optNormal.Render(" " + opt + " "))
					}
				}
				if m.mcpFocused == 2 {
					b.WriteString("  ◀▶")
				}
				b.WriteString("\n")
			} else {
				// Text input. Map logical field index to mcpInputs index.
				inputIdx := i
				if i > 2 {
					inputIdx = i - 1 // skip selector
				}
				if inputIdx < len(m.mcpInputs) {
					b.WriteString(m.mcpInputs[inputIdx].View())
				}
				b.WriteString("\n")
			}
		}
	}

	b.WriteString("\n\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(
		"Tab:下一项  Enter:确认  Esc:取消"))
	return b.String()
}

// --- Helpers ---

func buildMCPLocalInputs() []textinput.Model {
	names := []string{"名称", "命令 (如 uvx, npx)", "参数", "环境变量 (KEY=VAL ...)"}
	inputs := make([]textinput.Model, 4)
	for i, ph := range names {
		ti := textinput.New()
		ti.Placeholder = ph
		ti.CharLimit = 200
		ti.Width = 50
		inputs[i] = ti
	}
	return inputs
}

func buildMCPRemoteInputs() []textinput.Model {
	names := []string{"名称", "Endpoint URL", "密钥或 Token"}
	inputs := make([]textinput.Model, 3)
	for i, ph := range names {
		ti := textinput.New()
		ti.Placeholder = ph
		ti.CharLimit = 200
		ti.Width = 50
		inputs[i] = ti
	}
	return inputs
}

func parseEnvString(s string) map[string]string {
	if s == "" {
		return nil
	}
	env := make(map[string]string)
	for _, part := range strings.Fields(s) {
		if idx := strings.Index(part, "="); idx > 0 {
			env[part[:idx]] = part[idx+1:]
		}
	}
	if len(env) == 0 {
		return nil
	}
	return env
}

// IsEditing returns true when a text input is focused (blocks tab navigation).
func (m ToolStatusModel) IsEditing() bool {
	return m.skillSearch.Focused() || m.mcpAdding
}
