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
	width   int
	height  int
	scroll  int
}

// NewHelpModel creates a new help panel.
func NewHelpModel(lang string) HelpModel {
	return HelpModel{lang: i18n.NormalizeLang(lang)}
}

func (m *HelpModel) SetLang(lang string) {
	m.lang = i18n.NormalizeLang(lang)
}

func (m *HelpModel) SetViewport(width, height int) {
	m.width = width
	m.height = height
	m.clampScroll()
}

// Toggle switches show/hide.
func (m *HelpModel) Toggle() {
	m.visible = !m.visible
	if m.visible {
		m.scroll = 0
	}
}

// Show opens the help panel and resets it to the top.
func (m *HelpModel) Show() {
	m.visible = true
	m.scroll = 0
}

// Hide closes the help panel.
func (m *HelpModel) Hide() {
	m.visible = false
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
	case tea.WindowSizeMsg:
		m.SetViewport(msg.Width, max(1, msg.Height-4))
	case tea.KeyMsg:
		switch msg.String() {
		case "?", "esc":
			m.visible = false
		case "up", "k":
			m.scroll--
		case "down", "j":
			m.scroll++
		case "pgup", "ctrl+u":
			m.scroll -= m.helpPageStep()
		case "pgdown", "ctrl+d":
			m.scroll += m.helpPageStep()
		case "home", "g":
			m.scroll = 0
		case "end", "G":
			m.scroll = m.maxScroll()
		}
		m.clampScroll()
	}
	return m, nil
}

// View renders the full help panel without viewport clipping.
func (m HelpModel) View() string {
	return m.renderContent()
}

// ViewWithSize renders a scrollable help panel for the available content area.
func (m HelpModel) ViewWithSize(height, width int) string {
	content := m.renderContent()
	if height <= 0 {
		return fitRenderedLines(content, width)
	}
	lines := strings.Split(content, "\n")
	if len(lines) <= height {
		return fitRenderedLines(content, width)
	}

	bodyHeight := max(1, height-1)
	maxScroll := max(0, len(lines)-bodyHeight)
	scroll := min(max(m.scroll, 0), maxScroll)
	end := min(len(lines), scroll+bodyHeight)
	visible := append([]string{}, lines[scroll:end]...)
	visible = append(visible, helpScrollFooter(m.lang, scroll, maxScroll, width))
	return fitRenderedLines(strings.Join(visible, "\n"), width)
}

func (m HelpModel) helpPageStep() int {
	if m.height > 3 {
		return m.height - 2
	}
	return 4
}

func (m HelpModel) maxScroll() int {
	if m.height <= 0 {
		return 0
	}
	bodyHeight := max(1, m.height-1)
	return max(0, len(strings.Split(m.renderContent(), "\n"))-bodyHeight)
}

func (m *HelpModel) clampScroll() {
	if m.scroll < 0 {
		m.scroll = 0
	}
	if maxScroll := m.maxScroll(); m.scroll > maxScroll {
		m.scroll = maxScroll
	}
}

func helpScrollFooter(lang string, scroll, maxScroll, width int) string {
	var text string
	if i18n.NormalizeLang(lang) == "en" {
		text = fmt.Sprintf("↑/↓ scroll %d/%d  PgUp/PgDn page  ?/Esc close", scroll+1, maxScroll+1)
	} else {
		text = fmt.Sprintf("↑/↓ 滚动 %d/%d  PgUp/PgDn 翻页  ?/Esc 关闭", scroll+1, maxScroll+1)
	}
	if width > 8 {
		text = fitDisplay(text, width-4)
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("  " + text)
}

func (m HelpModel) renderContent() string {
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
			{"F1-F6 / Alt+1-6", helpLocalText(m.lang, "directTabs")},
			{"Ctrl+Left/Right", helpLocalText(m.lang, "cycleTabs")},
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
		{helpLocalText(m.lang, "toolsSection"), []struct{ key, desc string }{
			{"1 / 2", helpLocalText(m.lang, "toolsSubTabs")},
			{"s / /", helpLocalText(m.lang, "toolsSearch")},
			{"Space", helpLocalText(m.lang, "toolsQuick")},
			{"a / A", helpLocalText(m.lang, "toolsMCP")},
			{"Tab", helpLocalText(m.lang, "toolsMCPDetails")},
		}},
		{i18n.T(i18n.MsgTUIHelpSectionScheduledTasks, m.lang), []struct{ key, desc string }{
			{"1 / 2 / 3", i18n.T(i18n.MsgTUIHelpDescSwitchSubTab, m.lang)},
			{"Enter", helpLocalText(m.lang, "tasksEnter")},
			{"p", i18n.T(i18n.MsgTUIHelpDescPauseResume, m.lang)},
			{"d", i18n.T(i18n.MsgTUIHelpDescDelete, m.lang)},
		}},
		{helpLocalText(m.lang, "setupSection"), []struct{ key, desc string }{
			{"Enter", helpLocalText(m.lang, "setupEnter")},
			{"Space", helpLocalText(m.lang, "setupSpace")},
		}},
		{helpLocalText(m.lang, "redeemSection"), []struct{ key, desc string }{
			{"Enter", helpLocalText(m.lang, "redeemEnter")},
			{"F3", helpLocalText(m.lang, "redeemMCP")},
			{"Ctrl+R", helpLocalText(m.lang, "redeemRefresh")},
			{"Ctrl+U", helpLocalText(m.lang, "redeemClear")},
		}},
		{i18n.T(i18n.MsgTUIHelpSectionConfig, m.lang), []struct{ key, desc string }{
			{"Space", helpLocalText(m.lang, "configSpace")},
			{"Enter", helpLocalText(m.lang, "configEnter")},
			{"Esc", i18n.T(i18n.MsgTUIHelpDescCancelEdit, m.lang)},
		}},
		{i18n.T(i18n.MsgTUIHelpSectionAIAssistant, m.lang), []struct{ key, desc string }{
			{"i", i18n.T(i18n.MsgTUIHelpDescStartInput, m.lang)},
			{"Enter", i18n.T(i18n.MsgTUIHelpDescSendMessage, m.lang)},
			{"Esc", i18n.T(i18n.MsgTUIHelpDescExitInput, m.lang)},
			{"c", i18n.T(i18n.MsgTUIHelpDescClearHistory, m.lang)},
			{"Up/Down", i18n.T(i18n.MsgTUIHelpDescScrollMessages, m.lang)},
		}},
		{helpLocalText(m.lang, "slashSection"), []struct{ key, desc string }{
			{"/setup", helpLocalText(m.lang, "slashSetup")},
			{"/redeem", helpLocalText(m.lang, "slashRedeem")},
			{"/chat", helpLocalText(m.lang, "slashChat")},
			{"/tools", helpLocalText(m.lang, "slashTools")},
			{"/mcp [remote]", helpLocalText(m.lang, "slashMCP")},
			{"/skill", helpLocalText(m.lang, "slashSkill")},
			{"/tasks", helpLocalText(m.lang, "slashTasks")},
			{"/schedule", helpLocalText(m.lang, "slashSchedule")},
			{"/config", helpLocalText(m.lang, "slashConfig")},
			{"/status /doctor /health", helpLocalText(m.lang, "slashStatus")},
			{"/llm /security", helpLocalText(m.lang, "slashConfigDirect")},
			{"/help", helpLocalText(m.lang, "slashHelp")},
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

func helpLocalText(lang, key string) string {
	if i18n.NormalizeLang(lang) == "en" {
		texts := map[string]string{
			"directTabs":        "jump to a top tab from any page",
			"cycleTabs":         "cycle top tabs from any page",
			"setupSection":      "Setup",
			"setupEnter":        "run the selected setup action",
			"setupSpace":        "switch language or cycle HubCenter choices",
			"redeemSection":     "Service Redeem",
			"redeemEnter":       "redeem the service code",
			"redeemMCP":         "after official service is ready, open MCP templates",
			"redeemRefresh":     "refresh existing official service",
			"redeemClear":       "clear code input",
			"configSpace":       "cycle and save choices/suggestions",
			"configEnter":       "select from choices/actions; manual input appears only when needed",
			"slashSection":      "Slash commands",
			"slashSetup":        "open first-run Setup; /setup EMAIL prefills it",
			"slashRedeem":       "open Service Redeem; /redeem CODE prefills it",
			"slashChat":         "open Chat",
			"slashTools":        "open Tools; mcp shows the MCP list or template choices when empty",
			"slashMCP":          "open MCP; template choices when empty, remote opens remote templates",
			"slashSkill":        "open Skill tools",
			"slashTasks":        "open Tasks",
			"slashSchedule":     "open scheduled tasks",
			"slashConfig":       "open Config",
			"slashStatus":       "show setup status and the next action",
			"slashConfigDirect": "open the matching Config sub-page",
			"slashHelp":         "open this help overlay",
			"tasksEnter":        "on empty lists, open the next useful page",
			"toolsSection":      "Tools",
			"toolsSubTabs":      "switch Skill and MCP",
			"toolsSearch":       "type a SkillHub search",
			"toolsQuick":        "choose and search common Skill presets",
			"toolsMCP":          "on empty MCP, Enter opens the selected template; a/A adds templates",
			"toolsMCPDetails":   "adjust MCP template details only when needed",
		}
		return texts[key]
	}
	texts := map[string]string{
		"directTabs":        "从任意页面直达顶层标签",
		"cycleTabs":         "从任意页面切换顶层标签",
		"setupSection":      "初始化",
		"setupEnter":        "执行当前设置动作",
		"setupSpace":        "切换语言或 HubCenter 选项",
		"redeemSection":     "服务兑换",
		"redeemEnter":       "兑换服务码",
		"redeemMCP":         "官方服务就绪后打开 MCP 模板",
		"redeemRefresh":     "刷新已有官方服务",
		"redeemClear":       "清空兑换码输入",
		"configSpace":       "切换并保存选项/建议值",
		"configEnter":       "选择选项/执行动作；必要时才打开手动输入",
		"slashSection":      "斜杠命令",
		"slashSetup":        "打开首次设置；/setup 邮箱 可预填",
		"slashRedeem":       "打开服务兑换；/redeem 兑换码 可预填",
		"slashChat":         "打开聊天",
		"slashTools":        "打开工具；mcp 显示 MCP 列表，未配置时显示模板选择",
		"slashMCP":          "打开 MCP；未配置时显示模板选择，remote 打开远程模板",
		"slashSkill":        "打开 Skill 工具",
		"slashTasks":        "打开任务",
		"slashSchedule":     "打开定时任务",
		"slashConfig":       "打开设置",
		"slashStatus":       "查看初始化状态和下一步",
		"slashConfigDirect": "打开对应设置子页",
		"slashHelp":         "打开当前帮助层",
		"tasksEnter":        "空列表时打开下一步可操作页面",
		"toolsSection":      "工具",
		"toolsSubTabs":      "切换 Skill 与 MCP",
		"toolsSearch":       "输入 SkillHub 搜索",
		"toolsQuick":        "选择并搜索常用 Skill 预设",
		"toolsMCP":          "空 MCP 时 Enter 打开所选模板；a/A 添加模板",
		"toolsMCPDetails":   "必要时调整 MCP 模板细节",
	}
	return texts[key]
}
