package views

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// StatusBarModel 底部状态栏。
type StatusBarModel struct {
	hubStatus string // connected, disconnected, connecting
	modelInfo string // current LLM model name (shown in TUI mode)
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
	oldLang := m.lang
	m.lang = i18n.NormalizeLang(lang)
	m.message = translateStatusBarMessage(m.message, oldLang, m.lang)
}

func translateStatusBarMessage(text, oldLang, newLang string) string {
	for _, lang := range []string{oldLang, "zh", "en"} {
		if text == i18n.T(i18n.MsgTUIReady, lang) {
			return i18n.T(i18n.MsgTUIReady, newLang)
		}
	}
	for _, key := range []string{
		"llmNotConfigured", "llmKeyMissing", "mcpOptionalReady", "incompleteRemoteSetup",
		"serviceRedeemPrompt", "configOpenSetup", "configOpenRedeem", "configOpenTools",
		"serviceOpenSetup", "hubActivationSuccess", "weixinBound", "slashOpenSetup",
		"slashOpenRedeem", "slashOpenChat", "slashOpenTools", "slashOpenMCP",
		"slashOpenMCPList", "slashOpenTasks", "slashOpenConfig", "slashOpenStatus",
		"slashOpenHelp", "onboardingNeedConfig", "officialServiceReady", "redeemSuccessStatus",
	} {
		for _, lang := range []string{oldLang, "zh", "en"} {
			if text == statusBarText(lang, key) {
				return statusBarText(newLang, key)
			}
		}
	}
	if a, b, ok := matchStatusBarTemplate2(text, oldLang, "configSaveFailed"); ok {
		return statusBarFormat(newLang, "configSaveFailed", a, b)
	}
	for _, key := range []string{"configSaved", "configLoadWarning", "mcpAddedReady", "hubActivationFailed", "serviceStatusFailed"} {
		if value, ok := matchStatusBarTemplate1(text, oldLang, key); ok {
			return statusBarFormat(newLang, key, value)
		}
	}
	return text
}

func matchStatusBarTemplate1(message, lang, key string) (string, bool) {
	for _, candidateLang := range []string{lang, "zh", "en"} {
		sentinel := "__VALUE__"
		template := statusBarFormat(candidateLang, key, sentinel)
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

func matchStatusBarTemplate2(message, lang, key string) (string, string, bool) {
	for _, candidateLang := range []string{lang, "zh", "en"} {
		left := "__LEFT__"
		right := "__RIGHT__"
		template := statusBarFormat(candidateLang, key, left, right)
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

func statusBarFormat(lang, key string, args ...interface{}) string {
	return fmt.Sprintf(statusBarText(lang, key), args...)
}

func statusBarText(lang, key string) string {
	if i18n.NormalizeLang(lang) == "en" {
		texts := map[string]string{
			"llmNotConfigured":      "LLM is not configured: complete Setup, Service Redeem, or Config first.",
			"llmKeyMissing":         "LLM provider selected; enter its API key, choose a local provider, or redeem official service.",
			"mcpOptionalReady":      "Chat is ready. Optional: press F3 to add MCP capabilities from templates.",
			"incompleteRemoteSetup": "Remote setup is incomplete. Open Setup to refresh Hub credentials.",
			"serviceRedeemPrompt":   "Use a service code to enable MaClaw Official LLM.",
			"configOpenSetup":       "Opened Setup. Enter email and activate Hub.",
			"configOpenRedeem":      "Opened Service Redeem. Enter a service code or refresh service status.",
			"configOpenTools":       "Opened MCP templates. Left/Right choose local; Enter opens; A remote.",
			"serviceOpenSetup":      "Open Setup and activate Hub before redeeming service codes.",
			"hubActivationSuccess":  "Hub activation succeeded. Continue to Service Redeem.",
			"weixinBound":           "WeChat binding succeeded.",
			"slashOpenSetup":        "Opened Setup. Enter email and choose HubCenter to activate Hub.",
			"slashOpenRedeem":       "Opened Service Redeem. Paste a service code; input is masked.",
			"slashOpenChat":         "Opened Chat. Type a message or /help for commands.",
			"slashOpenTools":        "Opened Tools. Use 1/2 to switch Skill and MCP.",
			"slashOpenMCP":          "Opened MCP templates. Press Enter to add this preset, or Space to choose another.",
			"slashOpenMCPList":      "Opened MCP list. Press a/A to add local or remote templates.",
			"slashOpenTasks":        "Opened Tasks. Use 1/2/3 to switch task lists.",
			"slashOpenConfig":       "Opened Config. Use Enter/Space to choose values.",
			"slashOpenStatus":       "Opened Status. Enter on setup status jumps to the next useful page.",
			"slashOpenHelp":         "Opened Help. Esc closes; Up/Down or PgUp/PgDn scrolls.",
			"onboardingNeedConfig":  "Configure an LLM or complete Hub activation first.",
			"officialServiceReady":  "MaClaw official service is active. Default LLM has switched.",
			"redeemSuccessStatus":   "Redeem succeeded. Default LLM has switched to MaClaw Official.",
			"configSaveFailed":      "Config save failed: %s: %s",
			"configSaved":           "✓ Saved %s",
			"configLoadWarning":     "Config load failed; started with defaults. Error: %s",
			"mcpAddedReady":         "%s Chat is ready; press F2 to return.",
			"hubActivationFailed":   "Hub activation failed: %s",
			"serviceStatusFailed":   "service status check failed: %s",
		}
		if text, ok := texts[key]; ok {
			return text
		}
	}
	texts := map[string]string{
		"llmNotConfigured":      "LLM 未配置：请先在 初始化/服务兑换/设置 中完成配置",
		"llmKeyMissing":         "已选择 LLM 服务商；请填写密钥、切换本地服务商，或兑换官方服务。",
		"mcpOptionalReady":      "聊天已可用。可选：按 F3 从模板添加 MCP 能力。",
		"incompleteRemoteSetup": "远程初始化未完成，请在初始化页刷新 Hub 凭据。",
		"serviceRedeemPrompt":   "请使用服务兑换码启用 MaClaw 官方 LLM",
		"configOpenSetup":       "已打开初始化。请输入邮箱并激活 Hub。",
		"configOpenRedeem":      "已打开服务兑换。请输入服务兑换码，或刷新服务状态。",
		"configOpenTools":       "已打开 MCP 模板。左右键选择本地，Enter 打开，A 打开远程。",
		"serviceOpenSetup":      "请先打开初始化并激活 Hub，然后再兑换服务码。",
		"hubActivationSuccess":  "Hub 激活成功，可以继续服务兑换",
		"weixinBound":           "微信绑定成功",
		"slashOpenSetup":        "已打开初始化。输入邮箱并选择 HubCenter 来激活 Hub。",
		"slashOpenRedeem":       "已打开服务兑换。粘贴服务兑换码；输入会被掩码。",
		"slashOpenChat":         "已打开聊天。输入消息，或输入 /help 查看命令。",
		"slashOpenTools":        "已打开工具。使用 1/2 切换 Skill 与 MCP。",
		"slashOpenMCP":          "已打开 MCP 模板。按 Enter 添加当前预设，或按 Space 切换其它预设。",
		"slashOpenMCPList":      "已打开 MCP 列表。按 a/A 可添加本地或远程模板。",
		"slashOpenTasks":        "已打开任务。使用 1/2/3 切换任务列表。",
		"slashOpenConfig":       "已打开设置。使用 Enter/Space 选择配置值。",
		"slashOpenStatus":       "已打开状态总览。可在初始化状态行按 Enter 跳到下一步。",
		"slashOpenHelp":         "已打开帮助。Esc 关闭，↑↓ 或 PgUp/PgDn 滚动。",
		"onboardingNeedConfig":  "请先配置 LLM 或完成 Hub 激活",
		"officialServiceReady":  "MaClaw 官方服务已生效，默认 LLM 已切换",
		"redeemSuccessStatus":   "兑换成功，默认 LLM 已切换到 MaClaw 官方",
		"configSaveFailed":      "配置保存失败: %s: %s",
		"configSaved":           "✓ 已保存 %s",
		"configLoadWarning":     "配置加载失败，已使用默认值启动。错误: %s",
		"mcpAddedReady":         "%s 聊天已可用；按 F2 返回。",
		"hubActivationFailed":   "Hub 激活失败: %s",
		"serviceStatusFailed":   "服务状态检查失败: %s",
	}
	if text, ok := texts[key]; ok {
		return text
	}
	return key
}

// SetHubStatus 更新 Hub 连接状态。
func (m *StatusBarModel) SetHubStatus(status string) {
	m.hubStatus = status
}

// SetModelInfo sets the current LLM model display string.
func (m *StatusBarModel) SetModelInfo(info string) {
	m.modelInfo = info
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
	if width <= 0 {
		return ""
	}
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Background(lipgloss.Color("236")).
		Width(width)

	line := m.fitLine(width)
	return style.Render(line)
}

func (m StatusBarModel) fitLine(width int) string {
	left := m.leftSection()
	message := strings.TrimSpace(m.message)
	if message == "" {
		message = i18n.T(i18n.MsgTUIReady, m.lang)
	}
	help := i18n.T(i18n.MsgTUIStatusBarHelp, m.lang)

	if line := statusBarJoin(left, message, help); lipgloss.Width(line) <= width {
		return line
	}

	compactHelp := statusBarCompactHelp(m.lang)
	left = fitDisplay(left, max(8, width/4))
	if line, ok := statusBarFitThree(width, left, message, compactHelp); ok {
		return line
	}

	if line, ok := statusBarFitTwo(width, message, compactHelp); ok {
		return line
	}

	tinyHelp := statusBarTinyHelp(m.lang)
	if line, ok := statusBarFitTwo(width, message, tinyHelp); ok {
		return line
	}
	if lipgloss.Width(" "+tinyHelp) <= width {
		return " " + tinyHelp
	}
	return fitDisplay(" "+message, width)
}

func (m StatusBarModel) leftSection() string {
	if strings.TrimSpace(m.modelInfo) != "" {
		return "LLM " + strings.TrimSpace(m.modelInfo)
	}
	label := i18n.T(i18n.MsgTUIStatusDisconnectedHub, m.lang)
	prefix := "Hub:"
	switch m.hubStatus {
	case "connected":
		label = i18n.T(i18n.MsgTUIStatusConnectedHub, m.lang)
		prefix = "Hub:OK"
	case "connecting":
		label = i18n.T(i18n.MsgTUIStatusConnectingHub, m.lang)
		prefix = "Hub:..."
	}
	if i18n.NormalizeLang(m.lang) == "en" {
		return prefix + " " + label
	}
	return prefix + " " + label
}

func statusBarFitThree(width int, left, message, help string) (string, bool) {
	fixed := lipgloss.Width(statusBarJoin(left, "", help))
	messageWidth := width - fixed - lipgloss.Width(" │ ")
	if messageWidth < 8 {
		return "", false
	}
	line := statusBarJoin(left, fitDisplay(message, messageWidth), help)
	return line, lipgloss.Width(line) <= width
}

func statusBarFitTwo(width int, message, help string) (string, bool) {
	fixed := lipgloss.Width(statusBarJoin("", help))
	messageWidth := width - fixed - lipgloss.Width(" │ ")
	if messageWidth < 6 {
		return "", false
	}
	line := statusBarJoin(fitDisplay(message, messageWidth), help)
	return line, lipgloss.Width(line) <= width
}

func statusBarJoin(segments ...string) string {
	kept := make([]string, 0, len(segments))
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment != "" {
			kept = append(kept, segment)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return " " + strings.Join(kept, " │ ")
}

func statusBarCompactHelp(lang string) string {
	if i18n.NormalizeLang(lang) == "en" {
		return "F1-F6 ?:help"
	}
	return "F1-F6 ?:帮助"
}

func statusBarTinyHelp(lang string) string {
	if i18n.NormalizeLang(lang) == "en" {
		return "?:help"
	}
	return "?:帮助"
}
