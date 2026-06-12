package views

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ServiceRedeemSubmitMsg struct{ Code string }
type ServiceRedeemRefreshMsg struct{}
type ServiceRedeemOpenSetupMsg struct{}

type ServiceRedeemResultMsg struct {
	Success          bool
	Message          string
	ProviderName     string
	CreditsRemaining float64
	ExpiresAt        string
	FromRefresh      bool
	Config           corelib.AppConfig
	HasConfig        bool
}

type ServiceRedeemModel struct {
	lang          string
	width         int
	height        int
	input         textinput.Model
	busy          bool
	status        string
	provider      string
	credits       float64
	expiresAt     string
	email         string
	hubURL        string
	hubCenter     string
	hubReady      bool
	officialReady bool
	mcpReady      bool
	lastSuccess   bool
	lastFailure   bool
}

func NewServiceRedeemModel(lang string) ServiceRedeemModel {
	lang = i18n.NormalizeLang(lang)
	ti := textinput.New()
	ti.Placeholder = serviceRedeemText(lang, "placeholder")
	ti.CharLimit = 128
	ti.Width = 36
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '*'
	ti.Focus()
	m := ServiceRedeemModel{
		lang:   lang,
		width:  80,
		input:  ti,
		status: serviceRedeemText(lang, "ready"),
	}
	m.updateInputWidth()
	return m
}

func (m *ServiceRedeemModel) SetLang(lang string) {
	oldStatus := m.status
	m.lang = i18n.NormalizeLang(lang)
	m.input.Placeholder = serviceRedeemText(m.lang, "placeholder")
	m.updateInputWidth()
	m.status = translateServiceRedeemStatus(oldStatus, m.lang)
	if !m.hubReady && m.statusMatches("ready", "officialReady") {
		m.status = serviceRedeemText(m.lang, "setupRequired")
		return
	}
	if m.hubReady && m.officialReady && m.statusMatches("ready", "setupRequired") {
		m.status = serviceRedeemText(m.lang, "officialReady")
	}
}

func serviceRedeemStatusKeys() []string {
	return []string{
		"ready", "codeReady", "setupRequired", "officialReady", "empty", "redeeming", "checking",
	}
}

func translateServiceRedeemStatus(status, lang string) string {
	for _, key := range serviceRedeemStatusKeys() {
		if status == serviceRedeemText("zh", key) || status == serviceRedeemText("en", key) {
			return serviceRedeemText(lang, key)
		}
	}
	return status
}

func (m *ServiceRedeemModel) LoadFromAppConfig(cfg corelib.AppConfig) {
	m.email = strings.TrimSpace(cfg.RemoteEmail)
	m.hubURL = strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	m.hubCenter = strings.TrimRight(strings.TrimSpace(cfg.RemoteHubCenterURL), "/")
	m.hubReady = m.hubURL != "" && strings.TrimSpace(cfg.RemoteViewerToken) != ""
	m.officialReady = serviceRedeemUsesOfficialLLM(cfg)
	m.mcpReady = len(cfg.MCPServers)+len(cfg.LocalMCPServers) > 0
	if cfg.MaclawLLMCurrentProvider != "" {
		m.provider = cfg.MaclawLLMCurrentProvider
	}
	if m.hubReady {
		if m.officialReady && m.statusMatches("ready", "setupRequired") {
			m.status = serviceRedeemText(m.lang, "officialReady")
		} else if m.statusMatches("setupRequired") {
			m.status = serviceRedeemText(m.lang, "ready")
		}
	} else if !m.lastSuccess {
		m.status = serviceRedeemText(m.lang, "setupRequired")
	}
}

func (m *ServiceRedeemModel) SetInitialCode(code string) {
	code = normalizeServiceRedeemCode(code)
	if code == "" {
		return
	}
	m.input.SetValue(code)
	m.lastSuccess = false
	m.lastFailure = false
	m.status = serviceRedeemText(m.lang, "codeReady")
}

func (m ServiceRedeemModel) CodeValueForTest() string {
	return m.input.Value()
}

func (m ServiceRedeemModel) HasPendingCode() bool {
	return normalizeServiceRedeemCode(m.input.Value()) != ""
}

func (m ServiceRedeemModel) IsEditing() bool {
	// The redeem page has one always-focused input. Let root-level Tab/Left/Right
	// navigation stay available; normal text keys still flow into Update below.
	return false
}

func (m *ServiceRedeemModel) updateInputWidth() {
	labelWidth := lipgloss.Width(serviceRedeemText(m.lang, "code"))
	m.input.Width = min(36, max(12, m.width-labelWidth-6))
}

func (m ServiceRedeemModel) Init() tea.Cmd { return textinput.Blink }

func (m ServiceRedeemModel) Update(msg tea.Msg) (ServiceRedeemModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateInputWidth()
	case ServiceRedeemResultMsg:
		if msg.HasConfig {
			m.LoadFromAppConfig(msg.Config)
		}
		m.busy = false
		m.lastSuccess = msg.Success
		m.lastFailure = !msg.Success
		if strings.TrimSpace(msg.Message) != "" {
			m.status = msg.Message
		} else if msg.Success && m.officialReady {
			m.status = serviceRedeemText(m.lang, "officialReady")
		}
		if msg.ProviderName != "" {
			m.provider = msg.ProviderName
		}
		m.credits = msg.CreditsRemaining
		m.expiresAt = msg.ExpiresAt
		if msg.Success && serviceRedeemProviderIsOfficial(msg.ProviderName) {
			m.officialReady = true
		}
		if msg.Success && !msg.FromRefresh {
			m.input.SetValue("")
		}
		return m, nil
	case tea.KeyMsg:
		if m.busy {
			return m, nil
		}
		switch msg.String() {
		case "enter":
			if !m.hubReady {
				m.lastSuccess = false
				m.lastFailure = false
				m.status = serviceRedeemText(m.lang, "setupRequired")
				return m, func() tea.Msg { return ServiceRedeemOpenSetupMsg{} }
			}
			code := normalizeServiceRedeemCode(m.input.Value())
			if code == "" {
				if m.officialReady {
					m.busy = true
					m.lastSuccess = false
					m.lastFailure = false
					m.status = serviceRedeemText(m.lang, "checking")
					return m, func() tea.Msg { return ServiceRedeemRefreshMsg{} }
				}
				m.lastFailure = false
				m.status = serviceRedeemText(m.lang, "empty")
				return m, nil
			}
			m.input.SetValue(code)
			m.busy = true
			m.lastFailure = false
			m.status = serviceRedeemText(m.lang, "redeeming")
			return m, func() tea.Msg { return ServiceRedeemSubmitMsg{Code: code} }
		case "ctrl+r":
			if !m.hubReady {
				m.lastSuccess = false
				m.lastFailure = false
				m.status = serviceRedeemText(m.lang, "setupRequired")
				return m, nil
			}
			m.busy = true
			m.lastSuccess = false
			m.lastFailure = false
			m.status = serviceRedeemText(m.lang, "checking")
			return m, func() tea.Msg { return ServiceRedeemRefreshMsg{} }
		case "ctrl+u":
			m.input.SetValue("")
			m.lastSuccess = false
			m.lastFailure = false
			m.status = serviceRedeemText(m.lang, "ready")
		}
	}
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != before && m.lastFailure {
		m.lastFailure = false
		m.status = serviceRedeemText(m.lang, "codeReady")
	}
	return m, cmd
}

func (m ServiceRedeemModel) View() string {
	if m.useCompactView() {
		return m.viewCompact()
	}

	var b strings.Builder
	b.WriteString(serviceTitle.Render("  "+serviceRedeemText(m.lang, "title")) + "\n")
	b.WriteString("  " + serviceDim.Render(fitServiceText(serviceRedeemText(m.lang, "subtitle"), max(10, m.width-2))) + "\n\n")
	b.WriteString(m.renderHubState())
	if m.hubReady && m.officialReady {
		b.WriteString("  " + serviceOK.Render(fitServiceText(serviceRedeemText(m.lang, "officialReady"), max(10, m.width-2))) + "\n")
	}
	if !m.hubReady {
		b.WriteString("\n")
		b.WriteString(m.renderSavedCodeBeforeSetup())
		b.WriteString("  " + serviceDim.Render(fitServiceText(serviceRedeemText(m.lang, "setupAction"), max(10, m.width-2))) + "\n")
		b.WriteString("\n  " + serviceDim.Render(fitServiceText(serviceRedeemText(m.lang, "footerMissing"), max(10, m.width-2))))
		return b.String()
	}
	b.WriteString("\n")
	b.WriteString("  " + serviceLabel.Render(serviceRedeemText(m.lang, "code")) + "  ")
	b.WriteString(m.input.View())
	b.WriteString("\n\n")

	statusStyle := serviceDim
	if m.lastSuccess {
		statusStyle = serviceOK
	} else if m.lastFailure {
		statusStyle = serviceErr
	}
	statusWidth := max(8, m.width-lipgloss.Width(serviceRedeemText(m.lang, "status"))-6)
	b.WriteString("  " + serviceLabel.Render(serviceRedeemText(m.lang, "status")) + "  " + statusStyle.Render(fitServiceText(m.status, statusWidth)) + "\n")
	if m.provider != "" {
		b.WriteString("  " + serviceDim.Render(serviceRedeemText(m.lang, "provider")+": "+serviceRedeemProviderDisplayName(m.lang, m.provider)) + "\n")
	}
	if m.credits > 0 {
		b.WriteString("  " + serviceDim.Render(serviceRedeemText(m.lang, "credits")+": "+formatServiceCredits(m.credits)) + "\n")
	}
	if m.expiresAt != "" {
		b.WriteString("  " + serviceDim.Render(serviceRedeemText(m.lang, "expires")+": "+m.expiresAt) + "\n")
	}
	if next := m.nextStepHint(); next != "" {
		b.WriteString("  " + serviceDim.Render(fitServiceText(next, max(10, m.width-2))) + "\n")
	}

	b.WriteString("\n  " + serviceDim.Render(fitServiceText(m.footerText(), max(10, m.width-2))))
	return fitRenderedLines(b.String(), m.width)
}

func (m ServiceRedeemModel) useCompactView() bool {
	return m.height > 0 && m.height < 14
}

func (m ServiceRedeemModel) viewCompact() string {
	var b strings.Builder
	b.WriteString(serviceTitle.Render("  "+serviceRedeemText(m.lang, "title")) + "\n")
	b.WriteString(m.renderHubStateCompact())
	if !m.hubReady {
		b.WriteString(m.renderSavedCodeBeforeSetup())
		b.WriteString("  " + serviceDim.Render(fitServiceText(serviceRedeemText(m.lang, "setupAction"), max(10, m.width-2))) + "\n")
		b.WriteString("  " + serviceDim.Render(fitServiceText(serviceRedeemText(m.lang, "footerMissing"), max(10, m.width-2))))
		return fitRenderedLines(b.String(), m.width)
	}
	if m.officialReady {
		b.WriteString("  " + serviceOK.Render(fitServiceText(serviceRedeemText(m.lang, "officialReady"), max(10, m.width-2))) + "\n")
	}
	b.WriteString("  " + serviceLabel.Render(serviceRedeemText(m.lang, "code")) + "  ")
	b.WriteString(m.input.View())
	b.WriteString("\n")

	statusStyle := serviceDim
	if m.lastSuccess {
		statusStyle = serviceOK
	} else if m.lastFailure {
		statusStyle = serviceErr
	}
	statusWidth := max(8, m.width-lipgloss.Width(serviceRedeemText(m.lang, "status"))-6)
	b.WriteString("  " + serviceLabel.Render(serviceRedeemText(m.lang, "status")) + "  " + statusStyle.Render(fitServiceText(m.status, statusWidth)) + "\n")
	if m.provider != "" {
		b.WriteString("  " + serviceDim.Render(fitServiceText(serviceRedeemText(m.lang, "provider")+": "+serviceRedeemProviderDisplayName(m.lang, m.provider), max(10, m.width-2))) + "\n")
	}
	if next := m.nextStepHint(); next != "" {
		b.WriteString("  " + serviceDim.Render(fitServiceText(next, max(10, m.width-2))) + "\n")
	}
	b.WriteString("  " + serviceDim.Render(fitServiceText(m.footerCompactText(), max(10, m.width-2))))
	return fitRenderedLines(b.String(), m.width)
}

func (m ServiceRedeemModel) nextStepHint() string {
	if m.lastFailure {
		return serviceRedeemText(m.lang, "nextRetry")
	}
	if m.NeedsMCPNextStep() {
		return serviceRedeemText(m.lang, "nextMCP")
	}
	if m.hubReady && m.officialReady {
		return serviceRedeemText(m.lang, "nextChat")
	}
	return ""
}

func (m ServiceRedeemModel) NeedsMCPNextStep() bool {
	return m.hubReady && m.officialReady && !m.mcpReady
}

func (m ServiceRedeemModel) footerText() string {
	if m.NeedsMCPNextStep() {
		return serviceRedeemText(m.lang, "footerMCP")
	}
	if m.hubReady && m.officialReady {
		return serviceRedeemText(m.lang, "footerChat")
	}
	return serviceRedeemText(m.lang, "footer")
}

func (m ServiceRedeemModel) footerCompactText() string {
	if m.NeedsMCPNextStep() {
		return serviceRedeemText(m.lang, "footerCompactMCP")
	}
	if m.hubReady && m.officialReady {
		return serviceRedeemText(m.lang, "footerCompactChat")
	}
	return serviceRedeemText(m.lang, "footerCompact")
}

func (m ServiceRedeemModel) renderSavedCodeBeforeSetup() string {
	if strings.TrimSpace(m.input.Value()) == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("  " + serviceLabel.Render(serviceRedeemText(m.lang, "code")) + "  ")
	b.WriteString(m.input.View())
	b.WriteString("\n")
	b.WriteString("  " + serviceDim.Render(fitServiceText(serviceRedeemText(m.lang, "codeSavedBeforeSetup"), max(10, m.width-2))) + "\n\n")
	return b.String()
}

func (m ServiceRedeemModel) renderHubState() string {
	var b strings.Builder
	state := serviceErr.Render(serviceRedeemText(m.lang, "hubMissing"))
	if m.hubReady {
		state = serviceOK.Render(serviceRedeemText(m.lang, "hubReady"))
	}
	b.WriteString("  " + serviceLabel.Render(serviceRedeemText(m.lang, "hub")) + "  " + state + "\n")
	if m.email != "" {
		b.WriteString("  " + serviceDim.Render(serviceRedeemText(m.lang, "email")+": "+fitServiceText(m.email, max(20, m.width-14))) + "\n")
	}
	if m.hubCenter != "" {
		b.WriteString("  " + serviceDim.Render(serviceRedeemText(m.lang, "hubCenter")+": "+fitServiceText(m.hubCenter, max(20, m.width-19))) + "\n")
	}
	if m.hubURL != "" {
		label := serviceRedeemText(m.lang, "hubURL") + ": "
		b.WriteString("  " + serviceDim.Render(label+fitServiceText(m.hubURL, max(20, m.width-lipgloss.Width(label)-2))) + "\n")
	}
	return b.String()
}

func (m ServiceRedeemModel) renderHubStateCompact() string {
	state := serviceErr.Render(serviceRedeemText(m.lang, "hubMissing"))
	if m.hubReady {
		state = serviceOK.Render(serviceRedeemText(m.lang, "hubReady"))
	}
	return "  " + serviceLabel.Render(serviceRedeemText(m.lang, "hub")) + "  " + state + "\n"
}

const serviceRedeemOfficialProviderName = "MaClaw官方"

func (m ServiceRedeemModel) statusMatches(keys ...string) bool {
	if m.status == "" {
		return true
	}
	for _, key := range keys {
		if m.status == serviceRedeemText("zh", key) || m.status == serviceRedeemText("en", key) {
			return true
		}
	}
	return false
}

func serviceRedeemProviderDisplayName(lang, provider string) string {
	if provider == serviceRedeemOfficialProviderName {
		if i18n.NormalizeLang(lang) == "en" {
			return "MaClaw Official"
		}
		return "MaClaw 官方"
	}
	if provider == "Custom" {
		if i18n.NormalizeLang(lang) == "en" {
			return "Custom"
		}
		return "自定义"
	}
	if i18n.NormalizeLang(lang) != "en" {
		switch provider {
		case "Zhipu GLM Coding":
			return "智谱 GLM Coding"
		case "Xfyun Astron":
			return "讯飞星辰"
		case "Ollama Local":
			return "Ollama 本地"
		case "LM Studio Local":
			return "LM Studio 本地"
		case "OpenAI API Key":
			return "OpenAI API Key"
		}
	}
	return provider
}

func serviceRedeemUsesOfficialLLM(cfg corelib.AppConfig) bool {
	current := strings.TrimSpace(cfg.MaclawLLMCurrentProvider)
	if serviceRedeemProviderIsOfficial(current) {
		return true
	}
	for _, provider := range cfg.MaclawLLMProviders {
		if provider.Name != current {
			continue
		}
		return provider.IsHubService || serviceRedeemProviderIsOfficial(provider.Name)
	}
	return false
}

func serviceRedeemProviderIsOfficial(name string) bool {
	name = strings.TrimSpace(name)
	return name == serviceRedeemOfficialProviderName ||
		(strings.Contains(name, "MaClaw") &&
			(strings.Contains(name, "官方") || strings.Contains(strings.ToLower(name), "official")))
}

func serviceRedeemText(lang, key string) string {
	if i18n.NormalizeLang(lang) == "en" {
		texts := map[string]string{
			"title":                "Service Redeem",
			"subtitle":             "Redeem a MaClaw official service code and switch the default LLM to MaClaw Official.",
			"placeholder":          "service code",
			"ready":                "Enter a service code to redeem.",
			"codeReady":            "Service code is ready. Press Enter to redeem.",
			"code":                 "Code",
			"codeSavedBeforeSetup": "Code saved. Complete Setup first; the code stays ready for redeem.",
			"status":               "Status",
			"empty":                "Please enter a service code.",
			"redeeming":            "Redeeming...",
			"checking":             "Checking official service status...",
			"officialReady":        "MaClaw official LLM is configured.",
			"provider":             "LLM provider",
			"credits":              "Credits remaining",
			"expires":              "Expires at",
			"hub":                  "Hub state",
			"hubReady":             "ready",
			"hubMissing":           "not activated",
			"email":                "Email",
			"hubCenter":            "HubCenter",
			"hubURL":               "Selected Hub",
			"setupRequired":        "Complete Setup first: email activation selects Hub automatically and obtains the service token.",
			"setupAction":          "Press Enter to open Setup, enter email, and activate Hub before redeeming.",
			"footerMissing":        "Enter opens Setup; F1/Alt+1 also jumps there.",
			"footer":               "Enter redeems, or refreshes when already ready. Ctrl+R refreshes, Ctrl+U clears. Hub is selected by HubCenter during Setup.",
			"nextMCP":              "Next: F3 opens Tools/MCP templates; Chat is ready now.",
			"nextChat":             "Ready: press F2 to start chatting.",
			"nextRetry":            "Redeem failed. Check the code, then press Enter to retry; Ctrl+U clears it.",
			"footerMCP":            "Enter refreshes service status. F3 opens Tools/MCP templates, F2 opens Chat.",
			"footerChat":           "Enter refreshes service status. F2 opens Chat, Ctrl+R refreshes.",
			"footerCompact":        "Enter redeems/refreshes, Ctrl+U clears.",
			"footerCompactMCP":     "F3 MCP templates, F2 Chat.",
			"footerCompactChat":    "F2 Chat, Enter refreshes.",
		}
		return texts[key]
	}
	texts := map[string]string{
		"title":                "服务兑换",
		"subtitle":             "输入服务兑换码，兑换 MaClaw 官方服务，并默认使用 MaClaw 官方 LLM。",
		"placeholder":          "服务兑换码",
		"ready":                "请输入服务兑换码。",
		"codeReady":            "服务兑换码已就绪，按 Enter 兑换。",
		"code":                 "兑换码",
		"codeSavedBeforeSetup": "兑换码已保留。请先完成初始化，之后可直接兑换。",
		"status":               "状态",
		"empty":                "请输入服务兑换码。",
		"redeeming":            "正在兑换...",
		"checking":             "正在检查官方服务状态...",
		"officialReady":        "MaClaw 官方 LLM 已配置。",
		"provider":             "LLM 服务",
		"credits":              "剩余额度",
		"expires":              "到期时间",
		"hub":                  "Hub 状态",
		"hubReady":             "已就绪",
		"hubMissing":           "未激活",
		"email":                "邮箱",
		"hubCenter":            "HubCenter",
		"hubURL":               "已选择 Hub",
		"setupRequired":        "请先在 初始化 完成邮箱激活；Hub 会由 HubCenter 自动选择，并获取服务令牌。",
		"setupAction":          "按 Enter 打开初始化，输入邮箱并激活 Hub 后再兑换。",
		"footerMissing":        "Enter 打开初始化；F1/Alt+1 也可直接跳转。",
		"footer":               "Enter 兑换；已就绪时刷新。Ctrl+R 刷新，Ctrl+U 清空。Hub 由初始化时的 HubCenter 自动选择。",
		"nextMCP":              "下一步：F3 打开工具/MCP 模板；也可以现在开始聊天。",
		"nextChat":             "已就绪：按 F2 开始聊天。",
		"nextRetry":            "兑换失败。检查兑换码后按 Enter 重试；Ctrl+U 清空。",
		"footerMCP":            "Enter 刷新服务状态。F3 打开工具/MCP 模板，F2 打开聊天。",
		"footerChat":           "Enter 刷新服务状态。F2 打开聊天，Ctrl+R 刷新。",
		"footerCompact":        "Enter 兑换/刷新，Ctrl+U 清空。",
		"footerCompactMCP":     "F3 MCP 模板，F2 聊天。",
		"footerCompactChat":    "F2 聊天，Enter 刷新。",
	}
	return texts[key]
}

func normalizeServiceRedeemCode(value string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(value) {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func NormalizeServiceRedeemCodeForInput(value string) string {
	return normalizeServiceRedeemCode(value)
}

func fitServiceText(s string, width int) string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return "..."
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+3 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "..."
}

func formatServiceCredits(v float64) string {
	s := strings.TrimRight(strings.TrimRight(fmtFloat(v), "0"), ".")
	if s == "" {
		return "0"
	}
	return s
}

func fmtFloat(v float64) string { return fmt.Sprintf("%.2f", v) }

var (
	serviceTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	serviceDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	serviceLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
	serviceOK    = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	serviceErr   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)
