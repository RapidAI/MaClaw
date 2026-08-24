package views

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const maxConfigQQBotQRRefreshes = 3

func (m ConfigModel) qqBotQROverlayActive() bool {
	return m.qqbotOverlay
}

func (m ConfigModel) QQBotQRScanRequested() bool {
	return m.qqbotOverlay && m.qqbotBusy
}

func (m ConfigModel) QQBotQRPollTokenMatches(token string) bool {
	return m.qqBotQRPollTokenMatches(token)
}

func configQQBotQRTick(token string) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return ConfigQQBotTickMsg{Token: token}
	})
}

func (m ConfigModel) startQQBotQROverlay(resetRefreshes bool) (ConfigModel, tea.Cmd) {
	return m, m.beginQQBotQROverlay(resetRefreshes)
}

func (m *ConfigModel) beginQQBotQROverlay(resetRefreshes bool) tea.Cmd {
	prev := strings.TrimSpace(m.qqbotToken)
	if resetRefreshes {
		m.qqbotRefreshes = 0
	}
	m.qqbotOverlay = true
	m.qqbotBusy = true
	m.qqbotQR = ""
	m.qqbotToken = ""
	m.qqbotElapsed = 0
	m.qqbotStatus = configQQBotQRText(m.lang, "requesting")
	var cmds []tea.Cmd
	if prev != "" {
		token := prev
		cmds = append(cmds, func() tea.Msg { return ConfigQQBotCancelMsg{Token: token} })
	}
	cmds = append(cmds, func() tea.Msg { return ConfigQQBotScanMsg{} })
	return tea.Batch(cmds...)
}

func (m *ConfigModel) clearQQBotQROverlay() {
	m.qqbotOverlay = false
	m.qqbotBusy = false
	m.qqbotQR = ""
	m.qqbotToken = ""
	m.qqbotElapsed = 0
	m.qqbotStatus = ""
	m.qqbotRefreshes = 0
}

func (m ConfigModel) qqBotQRPollTokenMatches(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	return strings.TrimSpace(m.qqbotToken) == token
}

func (m *ConfigModel) updateQQBotQR(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case ConfigQQBotQRMsg:
		if !m.qqbotBusy {
			token := strings.TrimSpace(msg.Token)
			if token == "" {
				return nil, true
			}
			return func() tea.Msg { return ConfigQQBotCancelMsg{Token: token} }, true
		}
		m.qqbotBusy = false
		if !msg.Success {
			m.qqbotStatus = strings.TrimSpace(msg.Message)
			if m.qqbotStatus == "" {
				m.qqbotStatus = configQQBotQRText(m.lang, "failed")
			}
			if token := strings.TrimSpace(msg.Token); token != "" {
				return func() tea.Msg { return ConfigQQBotCancelMsg{Token: token} }, true
			}
			return nil, true
		}
		qr := strings.TrimSpace(msg.QR)
		token := strings.TrimSpace(msg.Token)
		if qr == "" || token == "" {
			m.qqbotStatus = configQQBotQRText(m.lang, "failed")
			if token != "" {
				return func() tea.Msg { return ConfigQQBotCancelMsg{Token: token} }, true
			}
			return nil, true
		}
		m.qqbotQR = qr
		m.qqbotToken = token
		m.qqbotElapsed = 0
		m.qqbotStatus = configQQBotQRText(m.lang, "waiting")
		return tea.Batch(
			func() tea.Msg { return ConfigQQBotPollMsg{Token: m.qqbotToken} },
			configQQBotQRTick(m.qqbotToken),
		), true
	case ConfigQQBotPollResultMsg:
		if !m.qqBotQRPollTokenMatches(msg.Token) {
			return nil, true
		}
		m.qqbotStatus = strings.TrimSpace(msg.Message)
		if m.qqbotStatus == "" {
			m.qqbotStatus = strings.TrimSpace(msg.Status)
		}
		if msg.Completed {
			m.qqbotBusy = false
			if msg.Success {
				m.clearQQBotQROverlay()
				return nil, true
			}
			if strings.EqualFold(strings.TrimSpace(msg.Status), "expired") && m.qqbotRefreshes < maxConfigQQBotQRRefreshes {
				m.qqbotRefreshes++
				return m.beginQQBotQROverlay(false), true
			}
			m.qqbotQR = ""
			m.qqbotToken = ""
			m.qqbotElapsed = 0
			return nil, true
		}
		if m.qqbotToken != "" {
			return func() tea.Msg { return ConfigQQBotPollMsg{Token: m.qqbotToken} }, true
		}
		return nil, true
	case ConfigQQBotTickMsg:
		if !m.qqbotOverlay {
			return nil, true
		}
		if strings.TrimSpace(msg.Token) != "" && !m.qqBotQRPollTokenMatches(msg.Token) {
			return nil, true
		}
		if m.qqbotQR == "" && !m.qqbotBusy {
			return nil, true
		}
		m.qqbotElapsed++
		return configQQBotQRTick(m.qqbotToken), true
	case tea.KeyMsg:
		if !m.qqbotOverlay {
			return nil, false
		}
		switch msg.String() {
		case "esc":
			token := strings.TrimSpace(m.qqbotToken)
			m.clearQQBotQROverlay()
			if token == "" {
				return nil, true
			}
			return func() tea.Msg { return ConfigQQBotCancelMsg{Token: token} }, true
		case "enter", "r":
			if m.qqbotBusy {
				return nil, true
			}
			if strings.TrimSpace(m.qqbotToken) == "" || strings.TrimSpace(m.qqbotQR) == "" {
				return m.beginQQBotQROverlay(true), true
			}
			m.qqbotStatus = configQQBotQRText(m.lang, "waiting")
			return func() tea.Msg { return ConfigQQBotPollMsg{Token: m.qqbotToken} }, true
		}
		return nil, true
	}
	return nil, false
}

func (m ConfigModel) viewQQBotQROverlay() string {
	var b strings.Builder
	b.WriteString(cfgHeaderStyle.Render("  "+configQQBotQRText(m.lang, "title")) + "\n")
	b.WriteString("  " + cfgDimStyle.Render(fitDisplay(configQQBotQRText(m.lang, "subtitle"), max(10, m.width-4))) + "\n")
	status := strings.TrimSpace(m.qqbotStatus)
	if status == "" {
		status = configQQBotQRText(m.lang, "waiting")
	}
	if m.qqbotElapsed > 0 {
		status = fmt.Sprintf("%s (%ds)", status, m.qqbotElapsed)
	}
	b.WriteString("  " + cfgDimStyle.Render(fitDisplay(status, max(10, m.width-4))) + "\n\n")
	if m.qqbotQR != "" {
		qrRows := 0
		if m.height > 0 {
			qrRows = max(1, m.height-12)
		}
		qrView, qrRendered := renderOnboardingQRWithLimitStatus(m.qqbotQR, m.width, qrRows, m.lang)
		b.WriteString(qrView)
		if !qrRendered {
			prefix := configQQBotQRText(m.lang, "payload")
			b.WriteString("  " + cfgDimStyle.Render(fitDisplay(prefix+m.qqbotQR, max(12, m.width-4))) + "\n")
		}
	}
	b.WriteString("  " + cfgDimStyle.Render(fitDisplay(configQQBotQRText(m.lang, "footer"), max(10, m.width-4))))
	return b.String()
}

func configQQBotQRText(lang, key string) string {
	if strings.EqualFold(strings.TrimSpace(lang), "en") {
		switch key {
		case "title":
			return "QQ Bot scan bind"
		case "subtitle":
			return "Scan this QR code with mobile QQ."
		case "requesting":
			return "Requesting QR code..."
		case "waiting":
			return "Waiting for QQ scan..."
		case "failed":
			return "QQ binding failed. Press Enter to try again."
		case "payload":
			return "URL: "
		case "footer":
			return "Esc cancels. Enter refreshes the QR code."
		}
		return key
	}
	switch key {
	case "title":
		return "QQ 扫码绑定"
	case "subtitle":
		return "请使用手机 QQ 扫描二维码。"
	case "requesting":
		return "正在获取二维码..."
	case "waiting":
		return "等待 QQ 扫码..."
	case "failed":
		return "QQ 绑定失败，请按 Enter 重试。"
	case "payload":
		return "链接："
	case "footer":
		return "Esc 取消，Enter 刷新二维码。"
	}
	return key
}
