package views

// tool_status.go implements the "工具" tab with two sub-tabs: Skill and MCP.
// - Skill: search SkillHub, install, list installed skills
// - MCP: add local/remote MCP servers, list configured servers

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/textutil"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// stripLeadingToolStatusEmoji removes a legacy leading pictograph (+ spaces).
func stripLeadingToolStatusEmoji(s string) string {
	return strings.TrimSpace(textutil.StripLeadingEmojiCluster(strings.TrimSpace(s)))
}

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

// SkillSearchResult represents a search result from SkillHub, ClawHub, or GitHub.
type SkillSearchResult struct {
	ID         string
	Name       string
	Version    string
	Rating     float64
	Downloads  int
	Trust      string
	Source     string // "skillhub", "clawhub", "github"
	InstallRef string // JSON-serialized GitHubSkillCandidate (github only)
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
	// Warning is non-fatal (e.g. partial source failure / degraded search).
	Warning string
}

// ToolSkillInstallMsg triggers a skill install.
type ToolSkillInstallMsg struct {
	SkillID    string
	HubURL     string
	Source     string // "skillhub", "clawhub", or "github"
	InstallRef string // JSON-serialized GitHubSkillCandidate (github only)
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
	width  int
	height int

	// Skill sub-tab state
	skills            []SkillItem
	skillCursor       int
	skillSearch       textinput.Model
	skillPresetIdx    int
	skillSearching    bool
	skillResults      []SkillSearchResult
	skillResultCursor int
	skillMessage      string // status message
	skillConfirming   bool   // true when showing install confirmation dialog
	skillConfirmIdx   int    // index of the result being confirmed

	// MCP sub-tab state
	mcpServers           []MCPItem
	mcpCursor            int
	mcpAdding            bool              // true when in add-MCP form
	mcpAddType           int               // 0=local, 1=remote
	mcpLocalTemplateIdx  int               // selected local MCP preset
	mcpRemoteTemplateIdx int               // selected remote MCP preset
	mcpInputs            []textinput.Model // text input fields
	mcpFocused           int               // which field is focused
	mcpAuthIdx           int               // selected auth type index (remote form only)
	mcpMessage           string            // status message

	// Legacy
	tools   []ToolInfo
	loading bool
}

// NewToolStatusModel 创建工具状态视图。
func NewToolStatusModel(lang string) ToolStatusModel {
	lang = i18n.NormalizeLang(lang)
	si := textinput.New()
	si.Placeholder = toolStatusText(lang, "skillSearchPlaceholder")
	si.CharLimit = 100
	si.Width = 40
	return ToolStatusModel{
		subTab:      ToolSubSkill,
		lang:        lang,
		width:       80,
		skillSearch: si,
		loading:     true,
	}
}

func (m *ToolStatusModel) SetLang(lang string) {
	oldLang := m.lang
	m.lang = i18n.NormalizeLang(lang)
	m.skillSearch.Placeholder = toolStatusText(m.lang, "skillSearchPlaceholder")
	m.skillMessage = translateToolStatusMessage(m.skillMessage, oldLang, m.lang)
	m.mcpMessage = translateToolStatusMessage(m.mcpMessage, oldLang, m.lang)
	if m.mcpAdding {
		m.updateMCPInputPlaceholders()
		m.resizeInputs()
	}
}

func translateToolStatusMessage(message, oldLang, newLang string) string {
	if strings.TrimSpace(message) == "" || i18n.NormalizeLang(oldLang) == i18n.NormalizeLang(newLang) {
		return message
	}

	prefix := ""
	body := message
	// Strip a single leading decorative pictograph cluster (legacy status rows).
	body = stripLeadingToolStatusEmoji(body)

	for _, key := range []string{
		"noSkillResults", "searching", "localRequired", "localArgsInvalid",
		"localEnvInvalid", "remoteRequired", "remoteSecretRequired",
	} {
		for _, lang := range []string{oldLang, "zh", "en"} {
			if body == toolStatusText(lang, key) {
				return prefix + toolStatusText(newLang, key)
			}
		}
	}

	if n, ok := matchToolStatusCount(body, oldLang, "foundSkillResults"); ok {
		return prefix + toolStatusFormat(newLang, "foundSkillResults", n)
	}
	for _, key := range []string{"installedPrefix", "installingPrefix", "addingPrefix"} {
		if tail, ok := matchToolStatusTemplate1(body, oldLang, key); ok {
			return prefix + toolStatusText(newLang, key) + tail
		}
	}
	if tail, ok := matchToolStatusTemplate1(body, oldLang, "searchingPreset"); ok {
		return prefix + toolStatusFormat(newLang, "searchingPreset", tail)
	}
	if name, source, ok := matchToolStatusTemplate2(body, oldLang, "confirmInstall"); ok {
		return prefix + toolStatusFormat(newLang, "confirmInstall", name, source)
	}

	return message
}

func matchToolStatusTemplate1(message, lang, key string) (string, bool) {
	for _, candidateLang := range []string{lang, "zh", "en"} {
		sentinel := "__VALUE__"
		template := toolStatusFormat(candidateLang, key, sentinel)
		parts := strings.Split(template, sentinel)
		if len(parts) != 2 {
			continue
		}
		if strings.HasPrefix(message, parts[0]) && strings.HasSuffix(message, parts[1]) {
			return strings.TrimSuffix(strings.TrimPrefix(message, parts[0]), parts[1]), true
		}
	}
	return "", false
}

func matchToolStatusTemplate2(message, lang, key string) (string, string, bool) {
	for _, candidateLang := range []string{lang, "zh", "en"} {
		left := "__LEFT__"
		right := "__RIGHT__"
		template := toolStatusFormat(candidateLang, key, left, right)
		parts := strings.Split(template, left)
		if len(parts) != 2 {
			continue
		}
		midParts := strings.Split(parts[1], right)
		if len(midParts) != 2 {
			continue
		}
		prefix, middle, suffix := parts[0], midParts[0], midParts[1]
		if !strings.HasPrefix(message, prefix) || !strings.HasSuffix(message, suffix) {
			continue
		}
		trimmed := strings.TrimSuffix(strings.TrimPrefix(message, prefix), suffix)
		values := strings.SplitN(trimmed, middle, 2)
		if len(values) != 2 {
			continue
		}
		return values[0], values[1], true
	}
	return "", "", false
}

func matchToolStatusCount(message, lang, key string) (int, bool) {
	for _, candidateLang := range []string{lang, "zh", "en"} {
		prefix := toolStatusFormat(candidateLang, key, 0)
		start := strings.Index(prefix, "0")
		if start < 0 {
			continue
		}
		end := start + 1
		for end < len(prefix) && prefix[end] >= '0' && prefix[end] <= '9' {
			end++
		}
		left, right := prefix[:start], prefix[end:]
		if !strings.HasPrefix(message, left) || !strings.HasSuffix(message, right) {
			continue
		}
		value := strings.TrimSuffix(strings.TrimPrefix(message, left), right)
		if value == "" {
			continue
		}
		n := 0
		for _, r := range value {
			if r < '0' || r > '9' {
				n = -1
				break
			}
			n = n*10 + int(r-'0')
		}
		if n >= 0 {
			return n, true
		}
	}
	return 0, false
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

// ActiveSubTab returns the currently selected Tools sub-tab.
func (m ToolStatusModel) ActiveSubTab() int { return m.subTab }

// MCPAddMode returns the active MCP add flow, or empty when no add form is open.
func (m ToolStatusModel) MCPAddMode() string {
	if !m.mcpAdding {
		return ""
	}
	if m.mcpAddType == 0 {
		return "local"
	}
	if m.mcpAddType == 1 {
		return "remote"
	}
	return ""
}

// FocusTab switches the Tools view to a valid sub-tab.
func (m *ToolStatusModel) FocusTab(tab int) {
	if tab < 0 || tab >= ToolSubCount {
		return
	}
	m.subTab = tab
}

// FocusSkill switches the Tools view to the Skill sub-tab.
func (m *ToolStatusModel) FocusSkill() {
	m.FocusTab(ToolSubSkill)
}

// FocusMCP switches the Tools view to the MCP sub-tab.
func (m *ToolStatusModel) FocusMCP() {
	m.FocusTab(ToolSubMCP)
	m.mcpAdding = false
	m.mcpInputs = nil
	m.mcpFocused = 0
}

// StartMCPLocalTemplate opens the local MCP template flow directly.
func (m *ToolStatusModel) StartMCPLocalTemplate() {
	m.FocusMCP()
	m.startMCPLocalAdd()
}

// StartMCPRemoteTemplate opens the remote MCP template flow directly.
func (m *ToolStatusModel) StartMCPRemoteTemplate() {
	m.FocusMCP()
	m.startMCPRemoteAdd()
}

// Init 实现 tea.Model。
func (m ToolStatusModel) Init() tea.Cmd { return nil }

// Update 处理键盘事件。
func (m ToolStatusModel) Update(msg tea.Msg) (ToolStatusModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeInputs()
		return m, nil

	case ToolSkillSearchResultMsg:
		m.skillSearching = false
		if msg.Error != "" {
			m.skillMessage = msg.Error
			m.skillResults = nil
		} else {
			m.skillResults = msg.Results
			m.skillResultCursor = 0
			if len(msg.Results) == 0 {
				m.skillMessage = toolStatusText(m.lang, "noSkillResults")
			} else {
				m.skillMessage = toolStatusFormat(m.lang, "foundSkillResults", len(msg.Results))
			}
			if strings.TrimSpace(msg.Warning) != "" {
				if m.skillMessage != "" {
					m.skillMessage += "\n"
				}
				m.skillMessage += "⚠️ " + msg.Warning
			}
		}
		return m, nil

	case ToolSkillInstallResultMsg:
		if msg.Error != "" {
			m.skillMessage = msg.Error
		} else {
			m.skillMessage = toolStatusText(m.lang, "installedPrefix") + msg.Name
		}
		return m, nil

	case ToolOperationResultMsg:
		message := msg.Message
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

type skillSearchPreset struct {
	Query string
	EN    string
	ZH    string
}

var skillSearchPresets = []skillSearchPreset{
	{Query: "coding", EN: "coding", ZH: "编程"},
	{Query: "browser", EN: "browser", ZH: "浏览器"},
	{Query: "pdf", EN: "pdf", ZH: "PDF"},
	{Query: "office", EN: "office", ZH: "Office"},
	{Query: "ocr", EN: "ocr", ZH: "OCR"},
	{Query: "database", EN: "database", ZH: "数据库"},
}

func (m ToolStatusModel) visibleRows(reserved int) int {
	rows := m.height - reserved
	if m.useCompactView() {
		if rows < 1 {
			return 1
		}
		return rows
	}
	if rows < 6 {
		return 6
	}
	return rows
}

func (m ToolStatusModel) useCompactView() bool {
	return m.height > 0 && m.height < 14
}

func (m *ToolStatusModel) resizeInputs() {
	width := m.width
	if width <= 0 {
		width = 80
	}
	m.skillSearch.Width = max(16, min(40, width-6))
	inputWidth := max(18, min(46, width-18))
	for i := range m.mcpInputs {
		m.mcpInputs[i].Width = inputWidth
	}
}

func scrollWindow(total, cursor, visible int) (int, int) {
	if visible <= 0 || total <= visible {
		return 0, total
	}
	start := cursor - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > total {
		start = total - visible
	}
	return start, start + visible
}

func (m ToolStatusModel) updateSkill(msg tea.KeyMsg) (ToolStatusModel, tea.Cmd) {
	// Install confirmation dialog
	if m.skillConfirming {
		switch msg.String() {
		case "y", "Y", "enter":
			// Confirmed — proceed with install
			m.skillConfirming = false
			if m.skillConfirmIdx < len(m.skillResults) {
				sr := m.skillResults[m.skillConfirmIdx]
				m.skillMessage = toolStatusText(m.lang, "installingPrefix") + sr.Name
				return m, func() tea.Msg {
					return ToolSkillInstallMsg{
						SkillID:    sr.ID,
						Source:     sr.Source,
						InstallRef: sr.InstallRef,
					}
				}
			}
			return m, nil
		case "n", "N", "esc":
			// Cancelled
			m.skillConfirming = false
			m.skillMessage = ""
			return m, nil
		}
		return m, nil
	}

	// Search input focused
	if m.skillSearch.Focused() {
		switch msg.String() {
		case "enter":
			query := strings.TrimSpace(m.skillSearch.Value())
			if query != "" {
				m.skillSearching = true
				m.skillMessage = toolStatusText(m.lang, "searching")
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
	case " ", "space":
		return m.startSkillPresetSearch()
	case "left", "h":
		if len(m.skillResults) == 0 {
			m.cycleSkillSearchPreset(-1)
		}
	case "right", "l":
		if len(m.skillResults) == 0 {
			m.cycleSkillSearchPreset(1)
		}
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
		// Show confirmation dialog before installing
		if len(m.skillResults) > 0 && m.skillResultCursor < len(m.skillResults) {
			sr := m.skillResults[m.skillResultCursor]
			m.skillConfirming = true
			m.skillConfirmIdx = m.skillResultCursor
			sourceLabel := "SkillHub"
			switch sr.Source {
			case "clawhub":
				sourceLabel = "ClawHub"
			case "github":
				sourceLabel = "GitHub"
			}
			m.skillMessage = toolStatusFormat(m.lang, "confirmInstall", sr.Name, sourceLabel)
			return m, nil
		}
		if len(m.skillResults) == 0 && !m.skillSearching {
			return m.startSkillPresetSearch()
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

func (m ToolStatusModel) startSkillPresetSearch() (ToolStatusModel, tea.Cmd) {
	query := m.currentSkillSearchPresetQuery()
	if query == "" {
		return m, nil
	}
	label := m.currentSkillSearchPresetLabel()
	m.skillSearch.SetValue(query)
	m.skillSearching = true
	m.skillMessage = toolStatusFormat(m.lang, "searchingPreset", label)
	m.cycleSkillSearchPreset(1)
	return m, func() tea.Msg { return ToolSkillSearchMsg{Query: query} }
}

func (m *ToolStatusModel) cycleSkillSearchPreset(delta int) {
	if len(skillSearchPresets) == 0 {
		return
	}
	if m.skillPresetIdx < 0 || m.skillPresetIdx >= len(skillSearchPresets) {
		m.skillPresetIdx = 0
	}
	m.skillPresetIdx = (m.skillPresetIdx + delta + len(skillSearchPresets)) % len(skillSearchPresets)
}

func (m ToolStatusModel) currentSkillSearchPresetQuery() string {
	if len(skillSearchPresets) == 0 {
		return ""
	}
	idx := m.skillPresetIdx
	if idx < 0 || idx >= len(skillSearchPresets) {
		idx = 0
	}
	return skillSearchPresets[idx].Query
}

func (m ToolStatusModel) updateMCP(msg tea.KeyMsg) (ToolStatusModel, tea.Cmd) {
	// In add-MCP form
	if m.mcpAdding {
		return m.updateMCPForm(msg)
	}

	switch msg.String() {
	case "a":
		// Start add-MCP form (local) with a preset selector first.
		m.startMCPLocalAdd()
		return m, nil
	case "A":
		// Start add-MCP form (remote) with a preset selector first.
		m.startMCPRemoteAdd()
		return m, nil
	case "enter":
		if len(m.mcpServers) == 0 {
			m.startMCPLocalAddWithTemplate(m.mcpLocalTemplateIdx)
			return m, nil
		}
	case " ", "space":
		if len(m.mcpServers) == 0 {
			m.startMCPLocalAddWithTemplate(m.mcpLocalTemplateIdx)
			return m, nil
		}
	case "left", "h":
		if len(m.mcpServers) == 0 {
			m.cycleMCPEmptyLocalTemplate(-1)
			return m, nil
		}
	case "up", "k":
		if m.mcpCursor > 0 {
			m.mcpCursor--
		}
	case "right", "l":
		if len(m.mcpServers) == 0 {
			m.cycleMCPEmptyLocalTemplate(1)
			return m, nil
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

func (m *ToolStatusModel) startMCPLocalAdd() {
	m.startMCPLocalAddWithTemplate(0)
}

func (m *ToolStatusModel) startMCPLocalAddWithTemplate(idx int) {
	m.mcpAdding = true
	m.mcpAddType = 0
	m.mcpLocalTemplateIdx = normalizedMCPLocalTemplateIndex(idx)
	m.mcpInputs = buildMCPLocalInputs(m.lang)
	m.mcpFocused = 0
	m.applyMCPLocalTemplate()
	m.resizeInputs()
}

func (m *ToolStatusModel) startMCPRemoteAdd() {
	m.mcpAdding = true
	m.mcpAddType = 1
	m.mcpRemoteTemplateIdx = 0
	m.mcpInputs = buildMCPRemoteInputs(m.lang)
	m.mcpAuthIdx = 0
	m.mcpFocused = 0
	m.applyMCPRemoteTemplate()
	m.resizeInputs()
}

var mcpAuthTypes = []string{"none", "api_key", "bearer"}

type mcpLocalTemplate struct {
	Name    string
	Command string
	Args    string
	Env     string
	DescZH  string
	DescEN  string
	Manual  bool
}

var mcpLocalTemplates = []mcpLocalTemplate{
	{
		Name:    "filesystem",
		Command: "npx",
		Args:    "-y @modelcontextprotocol/server-filesystem .",
		DescEN:  "share the current directory with the model",
		DescZH:  "把当前目录提供给模型访问",
	},
	{
		Name:    "playwright",
		Command: "npx",
		Args:    "-y @playwright/mcp@latest",
		DescEN:  "browser automation for local testing",
		DescZH:  "用于本地测试的浏览器自动化",
	},
	{
		Name:    "sequential-thinking",
		Command: "npx",
		Args:    "-y @modelcontextprotocol/server-sequential-thinking",
		DescEN:  "structured reasoning scratchpad",
		DescZH:  "结构化推理草稿能力",
	},
	{
		Name:    "fetch",
		Command: "uvx",
		Args:    "mcp-server-fetch",
		DescEN:  "web fetch helper via uvx",
		DescZH:  "通过 uvx 使用网页抓取辅助能力",
	},
	{
		Name:   "manual",
		DescEN: "custom command and arguments",
		DescZH: "自定义命令和参数",
		Manual: true,
	},
}

type mcpRemoteTemplate struct {
	Name       string
	URL        string
	AuthType   string
	AuthSecret string
	DescZH     string
	DescEN     string
	Manual     bool
}

var mcpRemoteTemplates = []mcpRemoteTemplate{
	{
		Name:     "local-http-mcp",
		URL:      "http://127.0.0.1:3000/mcp",
		AuthType: "none",
		DescEN:   "local streamable HTTP MCP endpoint",
		DescZH:   "本机 Streamable HTTP MCP 端点",
	},
	{
		Name:     "local-sse-mcp",
		URL:      "http://127.0.0.1:3000/sse",
		AuthType: "none",
		DescEN:   "local SSE MCP endpoint",
		DescZH:   "本机 SSE MCP 端点",
	},
	{
		Name:     "remote-bearer-mcp",
		AuthType: "bearer",
		DescEN:   "remote MCP endpoint with bearer token",
		DescZH:   "使用 Bearer Token 的远程 MCP 端点",
	},
	{
		Name:     "manual",
		AuthType: "none",
		DescEN:   "custom endpoint and auth",
		DescZH:   "自定义端点和认证",
		Manual:   true,
	},
}

func (m ToolStatusModel) updateMCPForm(msg tea.KeyMsg) (ToolStatusModel, tea.Cmd) {
	if m.mcpAddType == 0 {
		return m.updateMCPLocalForm(msg)
	}
	if m.mcpAddType == 1 {
		return m.updateMCPRemoteForm(msg)
	}
	return m, nil
}

func (m ToolStatusModel) updateMCPLocalForm(msg tea.KeyMsg) (ToolStatusModel, tea.Cmd) {
	totalFields := len(m.mcpInputs) + 1 // template selector + text fields
	if totalFields <= 1 {
		m.mcpAdding = false
		return m, nil
	}

	if m.mcpFocused == 0 && (msg.String() == " " || msg.String() == "space") {
		m.cycleMCPLocalTemplate(1)
		return m, nil
	}

	switch msg.String() {
	case "esc":
		m.mcpAdding = false
		m.mcpInputs = nil
		return m, nil
	case "tab", "down":
		m.blurMCPLocalFocusedInput()
		m.mcpFocused = (m.mcpFocused + 1) % totalFields
		m.focusMCPLocalFocusedInput()
		return m, nil
	case "shift+tab", "up":
		m.blurMCPLocalFocusedInput()
		m.mcpFocused = (m.mcpFocused - 1 + totalFields) % totalFields
		m.focusMCPLocalFocusedInput()
		return m, nil
	case "left", "h":
		if m.mcpFocused == 0 {
			m.cycleMCPLocalTemplate(-1)
			return m, nil
		}
	case "right", "l":
		if m.mcpFocused == 0 {
			m.cycleMCPLocalTemplate(1)
			return m, nil
		}
	case "enter":
		return m.submitMCPLocal()
	}

	if m.mcpFocused == 0 {
		return m, nil
	}
	inputIdx := m.mcpFocused - 1
	if inputIdx < len(m.mcpInputs) {
		var cmd tea.Cmd
		m.mcpInputs[inputIdx], cmd = m.mcpInputs[inputIdx].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *ToolStatusModel) cycleMCPLocalTemplate(delta int) {
	if len(mcpLocalTemplates) == 0 {
		return
	}
	m.mcpLocalTemplateIdx = (m.mcpLocalTemplateIdx + delta + len(mcpLocalTemplates)) % len(mcpLocalTemplates)
	m.applyMCPLocalTemplate()
}

func (m *ToolStatusModel) cycleMCPEmptyLocalTemplate(delta int) {
	if len(mcpLocalTemplates) == 0 {
		return
	}
	m.mcpLocalTemplateIdx = normalizedMCPLocalTemplateIndex(m.mcpLocalTemplateIdx + delta)
}

func normalizedMCPLocalTemplateIndex(idx int) int {
	if len(mcpLocalTemplates) == 0 {
		return 0
	}
	return (idx%len(mcpLocalTemplates) + len(mcpLocalTemplates)) % len(mcpLocalTemplates)
}

func (m *ToolStatusModel) applyMCPLocalTemplate() {
	if len(m.mcpInputs) < 4 || len(mcpLocalTemplates) == 0 {
		return
	}
	if m.mcpLocalTemplateIdx < 0 || m.mcpLocalTemplateIdx >= len(mcpLocalTemplates) {
		m.mcpLocalTemplateIdx = 0
	}
	tpl := mcpLocalTemplates[m.mcpLocalTemplateIdx]
	values := []string{tpl.Name, tpl.Command, tpl.Args, tpl.Env}
	if tpl.Manual {
		values = []string{"", "", "", ""}
	}
	for i, value := range values {
		m.mcpInputs[i].SetValue(value)
	}
}

func (m *ToolStatusModel) blurMCPLocalFocusedInput() {
	if m.mcpFocused <= 0 {
		return
	}
	inputIdx := m.mcpFocused - 1
	if inputIdx < len(m.mcpInputs) {
		m.mcpInputs[inputIdx].Blur()
	}
}

func (m *ToolStatusModel) focusMCPLocalFocusedInput() {
	if m.mcpFocused <= 0 {
		return
	}
	inputIdx := m.mcpFocused - 1
	if inputIdx < len(m.mcpInputs) {
		m.mcpInputs[inputIdx].Focus()
	}
}

func mcpLocalTemplateLabel(idx int, lang string) string {
	if idx < 0 || idx >= len(mcpLocalTemplates) {
		idx = 0
	}
	tpl := mcpLocalTemplates[idx]
	if tpl.Manual {
		if i18n.NormalizeLang(lang) == "en" {
			return "Manual"
		}
		return "手动"
	}
	return tpl.Name
}

func mcpLocalTemplateDesc(idx int, lang string) string {
	if idx < 0 || idx >= len(mcpLocalTemplates) {
		idx = 0
	}
	tpl := mcpLocalTemplates[idx]
	if i18n.NormalizeLang(lang) == "en" {
		return tpl.DescEN
	}
	return tpl.DescZH
}

func (m ToolStatusModel) updateMCPRemoteForm(msg tea.KeyMsg) (ToolStatusModel, tea.Cmd) {
	totalFields := len(m.mcpInputs) + 2 // template selector + text fields + auth selector
	if totalFields <= 2 {
		m.mcpAdding = false
		return m, nil
	}

	if (m.mcpFocused == 0 || m.mcpFocused == 3) && (msg.String() == " " || msg.String() == "space") {
		if m.mcpFocused == 0 {
			m.cycleMCPRemoteTemplate(1)
		} else {
			m.mcpAuthIdx = (m.mcpAuthIdx + 1) % len(mcpAuthTypes)
		}
		return m, nil
	}

	switch msg.String() {
	case "esc":
		m.mcpAdding = false
		m.mcpInputs = nil
		return m, nil
	case "tab", "down":
		m.blurMCPRemoteFocusedInput()
		m.mcpFocused = (m.mcpFocused + 1) % totalFields
		m.focusMCPRemoteFocusedInput()
		return m, nil
	case "shift+tab", "up":
		m.blurMCPRemoteFocusedInput()
		m.mcpFocused = (m.mcpFocused - 1 + totalFields) % totalFields
		m.focusMCPRemoteFocusedInput()
		return m, nil
	case "left", "h":
		if m.mcpFocused == 0 {
			m.cycleMCPRemoteTemplate(-1)
			return m, nil
		}
		if m.mcpFocused == 3 {
			m.mcpAuthIdx = (m.mcpAuthIdx - 1 + len(mcpAuthTypes)) % len(mcpAuthTypes)
			return m, nil
		}
	case "right", "l":
		if m.mcpFocused == 0 {
			m.cycleMCPRemoteTemplate(1)
			return m, nil
		}
		if m.mcpFocused == 3 {
			m.mcpAuthIdx = (m.mcpAuthIdx + 1) % len(mcpAuthTypes)
			return m, nil
		}
	case "enter":
		return m.submitMCPRemote()
	}

	if inputIdx := mcpRemoteFocusedInputIndex(m.mcpFocused); inputIdx >= 0 && inputIdx < len(m.mcpInputs) {
		var cmd tea.Cmd
		m.mcpInputs[inputIdx], cmd = m.mcpInputs[inputIdx].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *ToolStatusModel) cycleMCPRemoteTemplate(delta int) {
	if len(mcpRemoteTemplates) == 0 {
		return
	}
	m.mcpRemoteTemplateIdx = (m.mcpRemoteTemplateIdx + delta + len(mcpRemoteTemplates)) % len(mcpRemoteTemplates)
	m.applyMCPRemoteTemplate()
}

func (m *ToolStatusModel) applyMCPRemoteTemplate() {
	if len(m.mcpInputs) < 3 || len(mcpRemoteTemplates) == 0 {
		return
	}
	if m.mcpRemoteTemplateIdx < 0 || m.mcpRemoteTemplateIdx >= len(mcpRemoteTemplates) {
		m.mcpRemoteTemplateIdx = 0
	}
	tpl := mcpRemoteTemplates[m.mcpRemoteTemplateIdx]
	values := []string{tpl.Name, tpl.URL, tpl.AuthSecret}
	if tpl.Manual {
		values = []string{"", "", ""}
	}
	for i, value := range values {
		m.mcpInputs[i].SetValue(value)
	}
	m.mcpAuthIdx = mcpAuthTypeIndex(tpl.AuthType)
}

func (m *ToolStatusModel) blurMCPRemoteFocusedInput() {
	if inputIdx := mcpRemoteFocusedInputIndex(m.mcpFocused); inputIdx >= 0 && inputIdx < len(m.mcpInputs) {
		m.mcpInputs[inputIdx].Blur()
	}
}

func (m *ToolStatusModel) focusMCPRemoteFocusedInput() {
	if inputIdx := mcpRemoteFocusedInputIndex(m.mcpFocused); inputIdx >= 0 && inputIdx < len(m.mcpInputs) {
		m.mcpInputs[inputIdx].Focus()
	}
}

func mcpRemoteFocusedInputIndex(focused int) int {
	switch focused {
	case 1:
		return 0
	case 2:
		return 1
	case 4:
		return 2
	default:
		return -1
	}
}

func mcpAuthTypeIndex(authType string) int {
	for i, candidate := range mcpAuthTypes {
		if candidate == authType {
			return i
		}
	}
	return 0
}

func mcpRemoteTemplateLabel(idx int, lang string) string {
	if idx < 0 || idx >= len(mcpRemoteTemplates) {
		idx = 0
	}
	tpl := mcpRemoteTemplates[idx]
	if tpl.Manual {
		if i18n.NormalizeLang(lang) == "en" {
			return "Manual"
		}
		return "手动"
	}
	return tpl.Name
}

func mcpRemoteTemplateDesc(idx int, lang string) string {
	if idx < 0 || idx >= len(mcpRemoteTemplates) {
		idx = 0
	}
	tpl := mcpRemoteTemplates[idx]
	if i18n.NormalizeLang(lang) == "en" {
		return tpl.DescEN
	}
	return tpl.DescZH
}

func (m ToolStatusModel) mcpLocalDetailsVisible() bool {
	if m.mcpFocused > 0 {
		return true
	}
	if m.mcpLocalTemplateIdx < 0 || m.mcpLocalTemplateIdx >= len(mcpLocalTemplates) {
		return true
	}
	return mcpLocalTemplates[m.mcpLocalTemplateIdx].Manual
}

func (m ToolStatusModel) mcpRemoteDetailsVisible() bool {
	if m.mcpFocused > 0 {
		return true
	}
	if m.mcpRemoteTemplateIdx < 0 || m.mcpRemoteTemplateIdx >= len(mcpRemoteTemplates) {
		return true
	}
	tpl := mcpRemoteTemplates[m.mcpRemoteTemplateIdx]
	if tpl.Manual {
		return true
	}
	return strings.TrimSpace(tpl.URL) == ""
}

func (m ToolStatusModel) submitMCPLocal() (ToolStatusModel, tea.Cmd) {
	name := strings.TrimSpace(m.mcpInputs[0].Value())
	command := strings.TrimSpace(m.mcpInputs[1].Value())
	argsStr := strings.TrimSpace(m.mcpInputs[2].Value())
	envStr := strings.TrimSpace(m.mcpInputs[3].Value())

	if name == "" || command == "" {
		m.blurMCPLocalFocusedInput()
		if name == "" {
			m.mcpFocused = 1
		} else {
			m.mcpFocused = 2
		}
		m.focusMCPLocalFocusedInput()
		m.mcpMessage = toolStatusText(m.lang, "localRequired")
		return m, nil
	}

	var args []string
	if argsStr != "" {
		var err error
		args, err = splitMCPWords(argsStr)
		if err != nil {
			m.blurMCPLocalFocusedInput()
			m.mcpFocused = 3
			m.focusMCPLocalFocusedInput()
			m.mcpMessage = toolStatusText(m.lang, "localArgsInvalid")
			return m, nil
		}
	}
	env, err := parseEnvString(envStr)
	if err != nil {
		m.blurMCPLocalFocusedInput()
		m.mcpFocused = 4
		m.focusMCPLocalFocusedInput()
		m.mcpMessage = toolStatusText(m.lang, "localEnvInvalid")
		return m, nil
	}

	entry := corelib.LocalMCPServerEntry{
		Name:    name,
		Command: command,
		Args:    args,
		Env:     env,
	}

	m.mcpAdding = false
	m.mcpInputs = nil
	m.mcpMessage = toolStatusText(m.lang, "addingPrefix") + name
	return m, func() tea.Msg { return ToolMCPAddMsg{Entry: entry} }
}

func (m ToolStatusModel) submitMCPRemote() (ToolStatusModel, tea.Cmd) {
	name := strings.TrimSpace(m.mcpInputs[0].Value())
	url := strings.TrimSpace(m.mcpInputs[1].Value())
	authType := mcpAuthTypes[m.mcpAuthIdx]
	authSecret := strings.TrimSpace(m.mcpInputs[2].Value()) // index 2 in inputs = secret (3rd text field)

	if name == "" || url == "" {
		m.blurMCPRemoteFocusedInput()
		if name == "" {
			m.mcpFocused = 1
		} else {
			m.mcpFocused = 2
		}
		m.focusMCPRemoteFocusedInput()
		m.mcpMessage = toolStatusText(m.lang, "remoteRequired")
		return m, nil
	}
	if authType != "none" && authSecret == "" {
		m.blurMCPRemoteFocusedInput()
		m.mcpFocused = 4
		m.focusMCPRemoteFocusedInput()
		m.mcpMessage = toolStatusText(m.lang, "remoteSecretRequired")
		return m, nil
	}
	if authType == "none" {
		authSecret = ""
	}

	entry := corelib.MCPServerEntry{
		Name:        name,
		EndpointURL: url,
		AuthType:    authType,
		AuthSecret:  authSecret,
	}

	m.mcpAdding = false
	m.mcpInputs = nil
	m.mcpMessage = toolStatusText(m.lang, "addingPrefix") + name
	return m, func() tea.Msg { return ToolMCPAddRemoteMsg{Entry: entry} }
}

// --- View ---

func (m ToolStatusModel) View() string {
	var b strings.Builder
	compact := m.useCompactView()

	// Sub-tab bar
	b.WriteString(m.renderSubTabs())
	if compact {
		b.WriteString("\n")
	} else {
		b.WriteString("\n\n")
	}

	if m.subTab == ToolSubSkill {
		b.WriteString(m.viewSkill())
	} else {
		b.WriteString(m.viewMCP())
	}

	return fitRenderedLines(b.String(), m.width)
}

func (m ToolStatusModel) renderSubTabs() string {
	active := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57")).Padding(0, 1)
	inactive := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("238")).Padding(0, 1)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	names := [ToolSubCount]string{toolStatusText(m.lang, "subSkill"), toolStatusText(m.lang, "subMCP")}
	var tabs string
	for i, name := range names {
		if i == m.subTab {
			tabs += active.Render(name)
		} else {
			tabs += inactive.Render(name)
		}
		tabs += " "
	}
	hint := ""
	if m.subTab == ToolSubSkill && m.width >= 52 {
		hint = dim.Render("  " + toolStatusText(m.lang, "skillHint"))
	}
	return "  " + tabs + hint
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
		b.WriteString("  " + fitDisplay(m.skillMessage, max(12, m.width-2)) + "\n")
	}
	b.WriteString("\n")

	// Search results (if any)
	if len(m.skillResults) > 0 {
		b.WriteString(dim.Render("  "+toolStatusText(m.lang, "searchResultsHeader")) + "\n")
		start, end := scrollWindow(len(m.skillResults), m.skillResultCursor, m.visibleRows(10))
		if start > 0 {
			b.WriteString(dim.Render("  "+toolStatusFormat(m.lang, "moreAbove", start)) + "\n")
		}
		nameWidth := max(10, min(22, m.width-32))
		for i := start; i < end; i++ {
			sr := m.skillResults[i]
			sourceTag := "hub"
			switch sr.Source {
			case "clawhub":
				sourceTag = "claw"
			case "github":
				sourceTag = "gh"
			}
			// For GitHub, Downloads field holds star count.
			metric := fmt.Sprintf("%d↓", sr.Downloads)
			if sr.Source == "github" {
				metric = fmt.Sprintf("*%d", sr.Downloads)
			}
			line := fmt.Sprintf("  %s %s %s %s",
				padDisplay(fitDisplay(sr.Name, nameWidth), nameWidth),
				padDisplay(fitDisplay(sr.Version, 8), 8),
				padDisplay(fitDisplay(sourceTag+" "+sr.Trust, 10), 10),
				fitDisplay(metric, 8))
			if i == m.skillResultCursor {
				b.WriteString(sel.Render(line))
			} else {
				b.WriteString(line)
			}
			b.WriteString("\n")
		}
		if end < len(m.skillResults) {
			b.WriteString(dim.Render("  "+toolStatusFormat(m.lang, "moreBelow", len(m.skillResults)-end)) + "\n")
		}
		return b.String()
	}

	// Installed skills list
	if len(m.skills) == 0 {
		b.WriteString(dim.Render("  "+fitDisplay(toolStatusText(m.lang, "noInstalledSkills"), max(12, m.width-2))) + "\n")
		b.WriteString(dim.Render("  "+fitDisplay(toolStatusFormat(m.lang, "skillQuickSearch", m.currentSkillSearchPresetLabel()), max(12, m.width-2))) + "\n")
		b.WriteString(dim.Render("  "+fitDisplay(toolStatusFormat(m.lang, "skillPresetChoices", m.skillPresetChoicesText()), max(12, m.width-2))) + "\n")
		return b.String()
	}

	b.WriteString(dim.Render("  "+toolStatusFormat(m.lang, "installedSkillCount", len(m.skills))) + "\n\n")
	start, end := scrollWindow(len(m.skills), m.skillCursor, m.visibleRows(11))
	if start > 0 {
		b.WriteString(dim.Render("  "+toolStatusFormat(m.lang, "moreAbove", start)) + "\n")
	}
	nameWidth := max(10, min(28, m.width-24))
	descWidth := max(8, m.width-nameWidth-9)
	for i := start; i < end; i++ {
		sk := m.skills[i]
		status := ok.Render("*")
		if sk.Status == "disabled" {
			status = dim.Render("-")
		} else if sk.Status == "needs_setup" {
			status = warn.Render("?")
		}
		name := sk.Name
		if sk.Publisher != "" {
			name = sk.Publisher + ":" + sk.Name
		}
		line := fmt.Sprintf("  %s %s %s",
			status,
			padDisplay(fitDisplay(name, nameWidth), nameWidth),
			fitDisplay(sk.Description, descWidth))
		if i == m.skillCursor {
			b.WriteString(sel.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	if end < len(m.skills) {
		b.WriteString(dim.Render("  "+toolStatusFormat(m.lang, "moreBelow", len(m.skills)-end)) + "\n")
	}

	b.WriteString("\n" + dim.Render("  "+fitDisplay(toolStatusFormat(m.lang, "skillFooter", m.currentSkillSearchPresetLabel()), max(12, m.width-2))))
	return b.String()
}

func (m ToolStatusModel) currentSkillSearchPresetLabel() string {
	if len(skillSearchPresets) == 0 {
		return ""
	}
	idx := m.skillPresetIdx
	if idx < 0 || idx >= len(skillSearchPresets) {
		idx = 0
	}
	return skillSearchPresetLabel(skillSearchPresets[idx], m.lang)
}

func (m ToolStatusModel) skillPresetChoicesText() string {
	if len(skillSearchPresets) == 0 {
		return ""
	}
	idx := m.skillPresetIdx
	if idx < 0 || idx >= len(skillSearchPresets) {
		idx = 0
	}
	labels := make([]string, 0, len(skillSearchPresets))
	for i, preset := range skillSearchPresets {
		label := skillSearchPresetLabel(preset, m.lang)
		if i == idx {
			label = "[" + label + "]"
		}
		labels = append(labels, label)
	}
	return strings.Join(labels, "  ")
}

func skillSearchPresetLabel(preset skillSearchPreset, lang string) string {
	if i18n.NormalizeLang(lang) == "en" {
		if preset.EN != "" {
			return preset.EN
		}
		return preset.Query
	}
	if preset.ZH != "" {
		return preset.ZH
	}
	return preset.Query
}

func (m ToolStatusModel) viewMCP() string {
	var b strings.Builder
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sel := lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	ok := lipgloss.NewStyle().Foreground(lipgloss.Color("82"))

	if m.mcpAdding {
		if m.useCompactView() {
			return m.viewMCPFormCompact()
		}
		return m.viewMCPForm()
	}

	if m.mcpMessage != "" {
		b.WriteString("  " + fitDisplay(m.mcpMessage, max(12, m.width-2)) + "\n\n")
	}

	if len(m.mcpServers) == 0 {
		b.WriteString(m.viewMCPEmptyState(dim))
		return b.String()
	}

	b.WriteString(dim.Render("  "+toolStatusFormat(m.lang, "mcpServerCount", len(m.mcpServers))) + "\n\n")
	start, end := scrollWindow(len(m.mcpServers), m.mcpCursor, m.visibleRows(8))
	if start > 0 {
		b.WriteString(dim.Render("  "+toolStatusFormat(m.lang, "moreAbove", start)) + "\n")
	}
	nameWidth := max(10, min(20, m.width-28))
	endpointWidth := max(8, m.width-nameWidth-17)
	for i := start; i < end; i++ {
		srv := m.mcpServers[i]
		typeLabel := toolStatusText(m.lang, "mcpLocal")
		if srv.Type == "remote" {
			typeLabel = toolStatusText(m.lang, "mcpRemote")
		}
		status := ok.Render("*")
		if srv.Status == "stopped" {
			status = dim.Render("-")
		}
		line := fmt.Sprintf("  %s [%s] %s %s",
			status,
			fitDisplay(typeLabel, 6),
			padDisplay(fitDisplay(srv.Name, nameWidth), nameWidth),
			fitDisplay(srv.Endpoint, endpointWidth))
		if i == m.mcpCursor {
			b.WriteString(sel.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	if end < len(m.mcpServers) {
		b.WriteString(dim.Render("  "+toolStatusFormat(m.lang, "moreBelow", len(m.mcpServers)-end)) + "\n")
	}

	b.WriteString("\n" + dim.Render("  "+fitDisplay(toolStatusText(m.lang, "mcpFooter"), max(12, m.width-2))))
	return b.String()
}

func (m ToolStatusModel) viewMCPEmptyState(dim lipgloss.Style) string {
	var b strings.Builder
	width := max(12, m.width-2)
	b.WriteString(dim.Render("  "+fitDisplay(toolStatusText(m.lang, "noMCPServers"), width)) + "\n")
	b.WriteString(dim.Render("  "+fitDisplay(toolStatusFormat(m.lang, "mcpNextLocalTemplate", m.currentMCPLocalTemplateSummary()), width)) + "\n")
	b.WriteString(dim.Render("  "+fitDisplay(toolStatusFormat(m.lang, "mcpLocalTemplateChoices", m.mcpLocalTemplateChoicesText()), width)) + "\n")
	b.WriteString(dim.Render("  " + fitDisplay(toolStatusText(m.lang, "mcpEmptyFooter"), width)))
	return b.String()
}

func (m ToolStatusModel) currentMCPLocalTemplateSummary() string {
	idx := normalizedMCPLocalTemplateIndex(m.mcpLocalTemplateIdx)
	label := mcpLocalTemplateLabel(idx, m.lang)
	desc := mcpLocalTemplateDesc(idx, m.lang)
	if desc == "" {
		return label
	}
	return label + " - " + desc
}

func (m ToolStatusModel) mcpLocalTemplateChoicesText() string {
	if len(mcpLocalTemplates) == 0 {
		return ""
	}
	idx := normalizedMCPLocalTemplateIndex(m.mcpLocalTemplateIdx)
	labels := make([]string, 0, len(mcpLocalTemplates))
	for i := range mcpLocalTemplates {
		label := mcpLocalTemplateLabel(i, m.lang)
		if i == idx {
			label = "[" + label + "]"
		}
		labels = append(labels, label)
	}
	return strings.Join(labels, "  ")
}

func (m ToolStatusModel) viewMCPForm() string {
	var b strings.Builder
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("117"))

	if m.mcpAddType == 0 {
		b.WriteString(title.Render("  "+toolStatusText(m.lang, "addLocalMCP")) + "\n\n")
		if m.mcpMessage != "" {
			b.WriteString("  " + fitDisplay(m.mcpMessage, max(12, m.width-2)) + "\n\n")
		}
		optActive := lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
		optNormal := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
		label := toolStatusText(m.lang, "fieldTemplate")
		selected := mcpLocalTemplateLabel(m.mcpLocalTemplateIdx, m.lang)
		if m.mcpFocused == 0 {
			selected = optActive.Render(" " + selected + " ")
		} else {
			selected = optNormal.Render(" " + selected + " ")
		}
		dim := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		b.WriteString(fmt.Sprintf("  %-10s %s\n", label, selected))
		b.WriteString("  " + dim.Render(fitDisplay(mcpLocalTemplateDesc(m.mcpLocalTemplateIdx, m.lang), max(12, m.width-2))) + "\n")

		if !m.mcpLocalDetailsVisible() {
			summary := strings.TrimSpace(strings.Join([]string{m.mcpInputs[1].Value(), m.mcpInputs[2].Value()}, " "))
			b.WriteString("\n  " + dim.Render(toolStatusText(m.lang, "mcpPresetReady")) + " " + fitDisplay(summary, max(12, m.width-displayWidth(toolStatusText(m.lang, "mcpPresetReady"))-4)) + "\n")
			if env := strings.TrimSpace(m.mcpInputs[3].Value()); env != "" {
				maskedEnv := maskMCPEnvForDisplay(env)
				b.WriteString("  " + dim.Render(toolStatusText(m.lang, "fieldEnv")) + " " + fitDisplay(maskedEnv, max(12, m.width-displayWidth(toolStatusText(m.lang, "fieldEnv"))-4)) + "\n")
			}
			b.WriteString("\n  " + dim.Render(toolStatusText(m.lang, "mcpPresetQuickHelp")))
		} else {
			labels := []string{toolStatusText(m.lang, "fieldName"), toolStatusText(m.lang, "fieldCommand"), toolStatusText(m.lang, "fieldArgs"), toolStatusText(m.lang, "fieldEnv")}
			for i, input := range m.mcpInputs {
				b.WriteString(fmt.Sprintf("  %-10s %s\n", labels[i], input.View()))
			}
			b.WriteString("\n  " + dim.Render(
				toolStatusText(m.lang, "localMCPPresetHelp")))
		}
	} else {
		b.WriteString(title.Render("  "+toolStatusText(m.lang, "addRemoteMCP")) + "\n\n")
		if m.mcpMessage != "" {
			b.WriteString("  " + fitDisplay(m.mcpMessage, max(12, m.width-2)) + "\n\n")
		}
		optActive := lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
		optNormal := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
		label := toolStatusText(m.lang, "fieldTemplate")
		selected := mcpRemoteTemplateLabel(m.mcpRemoteTemplateIdx, m.lang)
		if m.mcpFocused == 0 {
			selected = optActive.Render(" " + selected + " ")
		} else {
			selected = optNormal.Render(" " + selected + " ")
		}
		dim := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		b.WriteString(fmt.Sprintf("  %-10s %s\n", label, selected))
		b.WriteString("  " + dim.Render(fitDisplay(mcpRemoteTemplateDesc(m.mcpRemoteTemplateIdx, m.lang), max(12, m.width-2))) + "\n")

		if !m.mcpRemoteDetailsVisible() {
			url := strings.TrimSpace(m.mcpInputs[1].Value())
			if url == "" {
				url = toolStatusText(m.lang, "mcpPresetNeedsURL")
			}
			b.WriteString("\n  " + dim.Render(toolStatusText(m.lang, "mcpPresetReady")) + " " + fitDisplay(url, max(12, m.width-displayWidth(toolStatusText(m.lang, "mcpPresetReady"))-4)) + "\n")
			b.WriteString("  " + dim.Render(toolStatusText(m.lang, "fieldAuthType")) + " " + mcpAuthTypeDisplay(mcpAuthTypes[m.mcpAuthIdx], m.lang) + "\n")
			b.WriteString("\n  " + dim.Render(toolStatusText(m.lang, "mcpPresetQuickHelp")))
		} else {
			labels := []string{toolStatusText(m.lang, "fieldName"), "URL:", toolStatusText(m.lang, "fieldAuthType"), toolStatusText(m.lang, "fieldSecret")}
			for i, label := range labels {
				b.WriteString(fmt.Sprintf("  %-10s ", label))
				fieldIdx := i + 1
				if fieldIdx == 3 {
					// Auth type selector.
					for j, opt := range mcpAuthTypes {
						label := mcpAuthTypeDisplay(opt, m.lang)
						if j > 0 {
							b.WriteString("  ")
						}
						if j == m.mcpAuthIdx {
							b.WriteString(optActive.Render(" " + label + " "))
						} else {
							b.WriteString(optNormal.Render(" " + label + " "))
						}
					}
					if m.mcpFocused == 3 {
						b.WriteString("  <>")
					}
					b.WriteString("\n")
				} else {
					if inputIdx := mcpRemoteFocusedInputIndex(fieldIdx); inputIdx >= 0 && inputIdx < len(m.mcpInputs) {
						b.WriteString(m.mcpInputs[inputIdx].View())
					}
					b.WriteString("\n")
				}
			}
			b.WriteString("\n  " + dim.Render(
				toolStatusText(m.lang, "remoteMCPPresetHelp")))
		}
	}

	b.WriteString("\n\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(
		toolStatusText(m.lang, "mcpFormFooter")))
	return b.String()
}

func (m ToolStatusModel) viewMCPFormCompact() string {
	var b strings.Builder
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("117"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	optActive := lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	optNormal := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	if m.mcpAddType == 0 {
		b.WriteString(title.Render("  "+toolStatusText(m.lang, "addLocalMCP")) + "\n")
		if m.mcpMessage != "" {
			b.WriteString("  " + fitDisplay(m.mcpMessage, max(12, m.width-2)) + "\n")
		}
		selected := mcpLocalTemplateLabel(m.mcpLocalTemplateIdx, m.lang)
		if m.mcpFocused == 0 {
			selected = optActive.Render(" " + selected + " ")
		} else {
			selected = optNormal.Render(" " + selected + " ")
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", toolStatusText(m.lang, "fieldTemplate"), selected))
		if m.mcpLocalDetailsVisible() {
			b.WriteString(m.renderCompactLocalMCPFocusedField())
		} else {
			desc := fitDisplay(mcpLocalTemplateDesc(m.mcpLocalTemplateIdx, m.lang), max(12, m.width-2))
			summary := strings.TrimSpace(strings.Join([]string{m.mcpInputs[1].Value(), m.mcpInputs[2].Value()}, " "))
			b.WriteString("  " + dim.Render(desc) + "\n")
			b.WriteString("  " + dim.Render(toolStatusText(m.lang, "mcpPresetReady")) + " " + fitDisplay(summary, max(12, m.width-displayWidth(toolStatusText(m.lang, "mcpPresetReady"))-4)) + "\n")
		}
	} else {
		b.WriteString(title.Render("  "+toolStatusText(m.lang, "addRemoteMCP")) + "\n")
		if m.mcpMessage != "" {
			b.WriteString("  " + fitDisplay(m.mcpMessage, max(12, m.width-2)) + "\n")
		}
		selected := mcpRemoteTemplateLabel(m.mcpRemoteTemplateIdx, m.lang)
		if m.mcpFocused == 0 {
			selected = optActive.Render(" " + selected + " ")
		} else {
			selected = optNormal.Render(" " + selected + " ")
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", toolStatusText(m.lang, "fieldTemplate"), selected))
		if m.mcpRemoteDetailsVisible() {
			b.WriteString(m.renderCompactRemoteMCPFocusedField())
		} else {
			url := strings.TrimSpace(m.mcpInputs[1].Value())
			if url == "" {
				url = toolStatusText(m.lang, "mcpPresetNeedsURL")
			}
			desc := fitDisplay(mcpRemoteTemplateDesc(m.mcpRemoteTemplateIdx, m.lang), max(12, m.width-2))
			ready := toolStatusText(m.lang, "mcpPresetReady") + " " + url
			auth := toolStatusText(m.lang, "fieldAuthType") + " " + mcpAuthTypeDisplay(mcpAuthTypes[m.mcpAuthIdx], m.lang)
			b.WriteString("  " + dim.Render(desc) + "\n")
			b.WriteString("  " + dim.Render(fitDisplay(ready+"  "+auth, max(12, m.width-2))) + "\n")
		}
	}
	b.WriteString("  " + dim.Render(fitDisplay(toolStatusText(m.lang, "mcpPresetQuickHelp"), max(12, m.width-2))))
	return b.String()
}

func (m ToolStatusModel) renderCompactLocalMCPFocusedField() string {
	labels := []string{toolStatusText(m.lang, "fieldName"), toolStatusText(m.lang, "fieldCommand"), toolStatusText(m.lang, "fieldArgs"), toolStatusText(m.lang, "fieldEnv")}
	inputIdx := m.mcpFocused - 1
	if inputIdx < 0 || inputIdx >= len(m.mcpInputs) || inputIdx >= len(labels) {
		return ""
	}
	return fmt.Sprintf("  %s %s\n", labels[inputIdx], m.mcpInputs[inputIdx].View())
}

func (m ToolStatusModel) renderCompactRemoteMCPFocusedField() string {
	optActive := lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
	optNormal := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	if m.mcpFocused == 3 {
		var b strings.Builder
		b.WriteString("  " + toolStatusText(m.lang, "fieldAuthType") + " ")
		for j, opt := range mcpAuthTypes {
			if j > 0 {
				b.WriteString(" ")
			}
			label := mcpAuthTypeDisplay(opt, m.lang)
			if j == m.mcpAuthIdx {
				b.WriteString(optActive.Render(" " + label + " "))
			} else {
				b.WriteString(optNormal.Render(" " + label + " "))
			}
		}
		b.WriteString("\n")
		return b.String()
	}
	labels := map[int]string{
		1: toolStatusText(m.lang, "fieldName"),
		2: "URL:",
		4: toolStatusText(m.lang, "fieldSecret"),
	}
	inputIdx := mcpRemoteFocusedInputIndex(m.mcpFocused)
	label := labels[m.mcpFocused]
	if inputIdx < 0 || inputIdx >= len(m.mcpInputs) || label == "" {
		return ""
	}
	return fmt.Sprintf("  %s %s\n", label, m.mcpInputs[inputIdx].View())
}

// --- Helpers ---

func toolStatusText(lang, key string) string {
	if i18n.NormalizeLang(lang) == "en" {
		texts := map[string]string{
			"skillSearchPlaceholder":  "Search Skill...",
			"noSkillResults":          "No matching Skill found",
			"foundSkillResults":       "Found %d results",
			"installedPrefix":         "Installed: ",
			"installingPrefix":        "Installing: ",
			"searching":               "Searching...",
			"searchingPreset":         "Searching preset: %s",
			"confirmInstall":          "Install %s from %s? [Y/n]",
			"localRequired":           "name and command are required",
			"localArgsInvalid":        "args contain an unfinished quote",
			"localEnvInvalid":         "env contains an unfinished quote",
			"remoteRequired":          "name and URL are required",
			"remoteSecretRequired":    "selected auth type needs a secret or token",
			"addingPrefix":            "Adding: ",
			"subSkill":                "1:Skill",
			"subMCP":                  "2:MCP",
			"skillHint":               "←→:preset  Enter/Space:search  s:type  r:refresh",
			"searchResultsHeader":     "Search results (Enter installs, Esc returns):",
			"moreAbove":               "... %d more above",
			"moreBelow":               "... %d more below",
			"noInstalledSkills":       "No installed Skills. Left/Right chooses a common search; Enter or Space runs it. Press s to type one.",
			"skillQuickSearch":        "Next quick search preset: %s",
			"skillPresetChoices":      "Quick presets: %s",
			"installedSkillCount":     "%d installed Skills:",
			"skillFooter":             "Up/Down:select  Left/Right:preset(%s)  Enter/Space:search  s:type  r:refresh",
			"noMCPServers":            "No MCP servers yet.",
			"mcpNextLocalTemplate":    "Selected local template: %s",
			"mcpLocalTemplateChoices": "Local templates: %s",
			"mcpEmptyFooter":          "Left/Right chooses local template. Enter/Space opens it. A opens remote templates.",
			"mcpServerCount":          "%d configured MCP servers:",
			"mcpLocal":                "local",
			"mcpRemote":               "remote",
			"mcpFooter":               "a:add local  A:add remote  r:refresh  1/2:switch sub-tab",
			"addLocalMCP":             "Add local MCP server",
			"addRemoteMCP":            "Add remote MCP server",
			"fieldName":               "Name:",
			"fieldCommand":            "Command:",
			"fieldArgs":               "Args:",
			"fieldEnv":                "Env:",
			"fieldTemplate":           "Template:",
			"fieldAuthType":           "Auth type:",
			"fieldSecret":             "Secret/Token:",
			"localMCPHelp":            "Split args by spaces; env format: KEY=VALUE KEY2=VALUE2",
			"localMCPPresetHelp":      "Left/Right or Space changes template; Enter confirms. Tab adjusts details only if needed.",
			"remoteMCPPresetHelp":     "Left/Right or Space changes endpoint template; Tab adjusts URL/auth only if needed.",
			"mcpPresetReady":          "Ready to add:",
			"mcpPresetQuickHelp":      "Enter confirms this preset. Tab adjusts details if needed. Space changes template.",
			"mcpPresetNeedsURL":       "URL required before submit",
			"mcpFormFooter":           "Tab:next  Enter:confirm  Esc:cancel",
			"placeholderName":         "Name",
			"placeholderCommand":      "Command (e.g. uvx, npx)",
			"placeholderArgs":         "Args",
			"placeholderEnv":          "Env (KEY=VAL ...)",
			"placeholderEndpoint":     "Endpoint URL",
			"placeholderSecret":       "Secret or Token",
		}
		if text, ok := texts[key]; ok {
			return text
		}
	}
	texts := map[string]string{
		"skillSearchPlaceholder":  "搜索 Skill...",
		"noSkillResults":          "未找到匹配的 Skill",
		"foundSkillResults":       "找到 %d 个结果",
		"installedPrefix":         "已安装: ",
		"installingPrefix":        "安装中: ",
		"searching":               "搜索中...",
		"searchingPreset":         "按预设搜索: %s",
		"confirmInstall":          "确认安装 %s（来源: %s）？ [Y/n]",
		"localRequired":           "名称和命令为必填项",
		"localArgsInvalid":        "参数中有未闭合的引号",
		"localEnvInvalid":         "环境变量中有未闭合的引号",
		"remoteRequired":          "名称和 URL 为必填项",
		"remoteSecretRequired":    "当前认证类型需要填写密钥或 Token",
		"addingPrefix":            "添加中: ",
		"subSkill":                "1:技能",
		"subMCP":                  "2:MCP 服务",
		"skillHint":               "←→:预设  Enter/Space:搜索  s:输入  r:刷新",
		"searchResultsHeader":     "搜索结果（Enter 安装，Esc 返回）:",
		"moreAbove":               "... 上方还有 %d 项",
		"moreBelow":               "... 下方还有 %d 项",
		"noInstalledSkills":       "暂无已安装的 Skill。左右选择常用搜索，Enter 或 Space 执行；按 s 可手动搜索。",
		"skillQuickSearch":        "下一个快速搜索预设：%s",
		"skillPresetChoices":      "常用预设：%s",
		"installedSkillCount":     "已安装 %d 个 Skill:",
		"skillFooter":             "↑↓:选择  ←→:预设(%s)  Enter/Space:搜索  s:输入  r:刷新",
		"noMCPServers":            "暂无 MCP 服务器。",
		"mcpNextLocalTemplate":    "当前本地模板：%s",
		"mcpLocalTemplateChoices": "本地模板：%s",
		"mcpEmptyFooter":          "左右键选择本地模板，Enter/Space 打开；A 打开远程模板。",
		"mcpServerCount":          "已配置 %d 个 MCP 服务器:",
		"mcpLocal":                "本地",
		"mcpRemote":               "远程",
		"mcpFooter":               "a:添加本地  A:添加远程  r:刷新  1/2:切换子标签",
		"addLocalMCP":             "添加本地 MCP 服务器",
		"addRemoteMCP":            "添加远程 MCP 服务器",
		"fieldName":               "名称:",
		"fieldCommand":            "命令:",
		"fieldArgs":               "参数:",
		"fieldEnv":                "环境变量:",
		"fieldTemplate":           "模板:",
		"fieldAuthType":           "认证类型:",
		"fieldSecret":             "密钥/Token:",
		"localMCPHelp":            "参数用空格分隔，环境变量格式: KEY=VALUE KEY2=VALUE2",
		"localMCPPresetHelp":      "左右键或 Space 切换模板；Enter 确认。必要时按 Tab 调整细节。",
		"remoteMCPPresetHelp":     "左右键或 Space 切换端点模板；必要时按 Tab 调整 URL/认证。",
		"mcpPresetReady":          "将添加:",
		"mcpPresetQuickHelp":      "Enter 使用该预设。必要时 Tab 调整细节。Space 切换模板。",
		"mcpPresetNeedsURL":       "提交前需要填写 URL",
		"mcpFormFooter":           "Tab:下一项  Enter:确认  Esc:取消",
		"placeholderName":         "名称",
		"placeholderCommand":      "命令 (如 uvx, npx)",
		"placeholderArgs":         "参数",
		"placeholderEnv":          "环境变量 (KEY=VAL ...)",
		"placeholderEndpoint":     "端点 URL",
		"placeholderSecret":       "密钥或 Token",
	}
	if text, ok := texts[key]; ok {
		return text
	}
	return key
}

func toolStatusFormat(lang, key string, args ...interface{}) string {
	return fmt.Sprintf(toolStatusText(lang, key), args...)
}

func (m *ToolStatusModel) updateMCPInputPlaceholders() {
	var placeholders []string
	if m.mcpAddType == 1 {
		placeholders = []string{toolStatusText(m.lang, "placeholderName"), toolStatusText(m.lang, "placeholderEndpoint"), toolStatusText(m.lang, "placeholderSecret")}
	} else {
		placeholders = []string{toolStatusText(m.lang, "placeholderName"), toolStatusText(m.lang, "placeholderCommand"), toolStatusText(m.lang, "placeholderArgs"), toolStatusText(m.lang, "placeholderEnv")}
	}
	for i := range m.mcpInputs {
		if i < len(placeholders) {
			m.mcpInputs[i].Placeholder = placeholders[i]
		}
	}
}

func buildMCPLocalInputs(lang string) []textinput.Model {
	names := []string{toolStatusText(lang, "placeholderName"), toolStatusText(lang, "placeholderCommand"), toolStatusText(lang, "placeholderArgs"), toolStatusText(lang, "placeholderEnv")}
	inputs := make([]textinput.Model, 4)
	for i, ph := range names {
		ti := textinput.New()
		ti.Placeholder = ph
		ti.CharLimit = 200
		ti.Width = 50
		ti.EchoCharacter = '*'
		if i == 3 {
			ti.EchoMode = textinput.EchoPassword
		}
		inputs[i] = ti
	}
	return inputs
}

func buildMCPRemoteInputs(lang string) []textinput.Model {
	names := []string{toolStatusText(lang, "placeholderName"), toolStatusText(lang, "placeholderEndpoint"), toolStatusText(lang, "placeholderSecret")}
	inputs := make([]textinput.Model, 3)
	for i, ph := range names {
		ti := textinput.New()
		ti.Placeholder = ph
		ti.CharLimit = 200
		ti.Width = 50
		ti.EchoCharacter = '*'
		if i == 2 {
			ti.EchoMode = textinput.EchoPassword
		}
		inputs[i] = ti
	}
	return inputs
}

func mcpAuthTypeDisplay(value, lang string) string {
	switch value {
	case "none":
		if i18n.NormalizeLang(lang) == "en" {
			return "No auth"
		}
		return "不认证"
	case "api_key":
		return "API Key"
	case "bearer":
		return "Bearer Token"
	}
	return value
}

func maskMCPEnvForDisplay(s string) string {
	parts, err := splitMCPWords(s)
	if err != nil {
		parts = strings.Fields(strings.TrimSpace(s))
	}
	if len(parts) == 0 {
		return ""
	}
	masked := make([]string, 0, len(parts))
	for _, part := range parts {
		idx := strings.Index(part, "=")
		if idx <= 0 {
			masked = append(masked, "********")
			continue
		}
		key := part[:idx]
		if part[idx+1:] == "" {
			masked = append(masked, key+"=")
			continue
		}
		masked = append(masked, key+"=********")
	}
	return strings.Join(masked, " ")
}

func parseEnvString(s string) (map[string]string, error) {
	if s == "" {
		return nil, nil
	}
	parts, err := splitMCPWords(s)
	if err != nil {
		return nil, err
	}
	env := make(map[string]string)
	for _, part := range parts {
		if idx := strings.Index(part, "="); idx > 0 {
			env[part[:idx]] = part[idx+1:]
		}
	}
	if len(env) == 0 {
		return nil, nil
	}
	return env, nil
}

func splitMCPWords(s string) ([]string, error) {
	var words []string
	var b strings.Builder
	var quote rune
	escaped := false
	inWord := false

	flush := func() {
		if !inWord {
			return
		}
		words = append(words, b.String())
		b.Reset()
		inWord = false
	}

	for _, r := range strings.TrimSpace(s) {
		if escaped {
			b.WriteRune(r)
			inWord = true
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			inWord = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				inWord = true
				continue
			}
			b.WriteRune(r)
			inWord = true
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			inWord = true
			continue
		}
		if unicode.IsSpace(r) {
			flush()
			continue
		}
		b.WriteRune(r)
		inWord = true
	}
	if escaped {
		b.WriteRune('\\')
	}
	if quote != 0 {
		return nil, fmt.Errorf("unfinished quote")
	}
	flush()
	return words, nil
}

// IsEditing returns true when a text input is focused or a confirmation
// dialog is active (blocks tab navigation).
func (m ToolStatusModel) IsEditing() bool {
	return m.skillSearch.Focused() || m.mcpAdding || m.skillConfirming
}
