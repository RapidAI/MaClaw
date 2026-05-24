package views

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	qrcode "github.com/skip2/go-qrcode"
)

type OnboardingActivateRemoteMsg struct {
	Email        string
	HubCenterURL string
}
type OnboardingStartWeixinMsg struct{}
type OnboardingPollWeixinMsg struct{ Token string }
type OnboardingWeixinTickMsg struct{ Token string }
type OnboardingFinishMsg struct{}
type OnboardingLanguageChangedMsg struct{ Language string }

type OnboardingRemoteResultMsg struct {
	Success         bool
	Message         string
	HubURL          string
	MachineID       string
	HubServiceReady bool
	MachineReady    bool
}

type OnboardingWeixinQRMsg struct {
	Success bool
	Message string
	QR      string
	Token   string
}

type OnboardingWeixinPollResultMsg struct {
	Token     string
	Status    string
	Message   string
	Success   bool
	Completed bool
	AccountID string
}

type OnboardingModel struct {
	lang             string
	width            int
	height           int
	cursor           int
	emailInput       textinput.Model
	hubCenterInput   textinput.Model
	hubCenterOptions []string
	hubCenterSelect  bool
	hubCenterManual  bool
	hubCenterCursor  int
	hubURL           string
	remoteDone       bool
	remoteBusy       bool
	remoteFailed     bool
	remoteStatus     string
	weixinDone       bool
	weixinBusy       bool
	weixinStatus     string
	weixinQR         string
	weixinToken      string
	weixinElapsed    int
	weixinRefreshes  int
	accountID        string
	done             bool
	message          string
}

const maxOnboardingWeixinRefreshes = 3

func onboardingWeixinTick(token string) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return OnboardingWeixinTickMsg{Token: token}
	})
}

func NewOnboardingModel(lang string) OnboardingModel {
	lang = i18n.NormalizeLang(lang)
	emailInput := textinput.New()
	emailInput.Placeholder = "you@example.com"
	emailInput.CharLimit = 160
	emailInput.Width = 36

	hubCenterInput := textinput.New()
	hubCenterInput.Placeholder = remote.DefaultRemoteHubCenterURL
	hubCenterInput.CharLimit = 256
	hubCenterInput.Width = 46
	hubCenterInput.SetValue(remote.DefaultRemoteHubCenterURL)
	envHubCenterURL := onboardingEnvHubCenterURL()
	if envHubCenterURL != "" {
		hubCenterInput.SetValue(envHubCenterURL)
	}
	if email := onboardingEnvEmail(); email != "" {
		emailInput.SetValue(email)
	}

	m := OnboardingModel{
		lang:             lang,
		width:            80,
		emailInput:       emailInput,
		hubCenterInput:   hubCenterInput,
		hubCenterOptions: cloneOnboardingValues(remote.DefaultRemoteHubCenterURLs...),
		hubCenterManual:  envHubCenterURL != "",
		remoteStatus:     onboardingText(lang, "notActivated"),
		weixinStatus:     onboardingText(lang, "notBound"),
	}
	if strings.TrimSpace(emailInput.Value()) != "" {
		m.cursor = onboardingRowActivate
	}
	m.updateInputWidths()
	m.focusCursor()
	return m
}

func onboardingEnvEmail() string {
	for _, key := range []string{"MACLAW_REMOTE_EMAIL", "MACLAW_EMAIL"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func onboardingEnvHubCenterURL() string {
	for _, key := range []string{"MACLAW_REMOTE_HUBCENTER_URL", "MACLAW_HUBCENTER_URL", "MACLAW_HUB_CENTER_URL"} {
		value := normalizeOnboardingHubCenter(os.Getenv(key))
		if value != remote.DefaultRemoteHubCenterURL && validOnboardingHubCenterURL(value) {
			return value
		}
	}
	return ""
}

func (m *OnboardingModel) SetLang(lang string) {
	oldLang := m.lang
	m.lang = i18n.NormalizeLang(lang)
	m.hubCenterInput.Placeholder = remote.DefaultRemoteHubCenterURL
	m.updateInputWidths()
	m.remoteStatus = translateOnboardingStatus(m.remoteStatus, oldLang, m.lang, onboardingRemoteStatusKeys())
	m.weixinStatus = translateOnboardingStatus(m.weixinStatus, oldLang, m.lang, onboardingWeixinStatusKeys())
}

func (m *OnboardingModel) LoadFromAppConfig(cfg corelib.AppConfig) {
	if strings.TrimSpace(cfg.Language) != "" {
		m.SetLang(cfg.Language)
	}
	m.hubURL = strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	if strings.TrimSpace(m.emailInput.Value()) == "" && cfg.RemoteEmail != "" {
		m.emailInput.SetValue(cfg.RemoteEmail)
	}
	m.hubCenterOptions = hubCenterOnboardingOptions(cfg)
	if strings.TrimSpace(m.hubCenterInput.Value()) == "" || m.hubCenterInput.Value() == remote.DefaultRemoteHubCenterURL {
		if cfg.RemoteHubCenterURL != "" {
			m.hubCenterInput.SetValue(strings.TrimRight(strings.TrimSpace(cfg.RemoteHubCenterURL), "/"))
		} else if len(m.hubCenterOptions) > 0 {
			m.hubCenterInput.SetValue(m.hubCenterOptions[0])
		}
	}
	m.hubCenterManual = !containsOnboardingValue(m.hubCenterOptions, strings.TrimRight(strings.TrimSpace(m.hubCenterInput.Value()), "/"))
	m.focusCursor()
	hubServiceReady := strings.TrimSpace(cfg.RemoteHubURL) != "" && strings.TrimSpace(cfg.RemoteViewerToken) != ""
	machineReady := strings.TrimSpace(cfg.RemoteMachineID) != "" &&
		strings.TrimSpace(cfg.RemoteMachineToken) != "" &&
		strings.TrimSpace(cfg.RemoteViewerToken) != ""
	m.remoteDone = hubServiceReady || machineReady
	m.remoteFailed = false
	if m.remoteDone {
		if machineReady {
			m.remoteStatus = onboardingText(m.lang, "activated")
			m.remoteStatus += ": " + cfg.RemoteMachineID
		} else {
			m.remoteStatus = onboardingText(m.lang, "serviceReady")
		}
		if !cfg.OnboardingDone {
			m.cursor = onboardingRowFinish
		}
	} else if cfg.RemoteEmail != "" {
		m.remoteStatus = onboardingText(m.lang, "emailSaved")
		if m.cursor == onboardingRowLanguage || m.cursor == onboardingRowEmail {
			m.cursor = onboardingRowActivate
		}
	} else {
		m.remoteStatus = onboardingText(m.lang, "notActivated")
	}
	m.weixinDone = cfg.WeixinEnabled && cfg.WeixinToken != ""
	if m.weixinDone {
		m.weixinStatus = onboardingText(m.lang, "bound")
		m.accountID = cfg.WeixinAccountID
	} else {
		m.weixinStatus = onboardingText(m.lang, "notBound")
	}
	m.done = cfg.OnboardingDone
	m.focusCursor()
}

func (m *OnboardingModel) SetInitialEmail(email string) {
	email = normalizeOnboardingEmail(email)
	if email == "" {
		return
	}
	m.emailInput.SetValue(email)
	if validOnboardingEmail(email) && !m.remoteDone {
		m.cursor = onboardingRowActivate
	} else {
		m.cursor = onboardingRowEmail
	}
	m.focusCursor()
}

func (m OnboardingModel) EmailValueForTest() string {
	return m.emailInput.Value()
}

func (m OnboardingModel) RemoteDoneForTest() bool {
	return m.remoteDone
}

func (m OnboardingModel) Init() tea.Cmd { return textinput.Blink }

func (m OnboardingModel) IsEditing() bool {
	return m.emailInput.Focused() || m.hubCenterInput.Focused() || m.hubCenterSelect || m.weixinQR != ""
}

func (m OnboardingModel) Update(msg tea.Msg) (OnboardingModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateInputWidths()
	case OnboardingRemoteResultMsg:
		m.remoteBusy = false
		m.remoteStatus = msg.Message
		m.hubURL = strings.TrimRight(strings.TrimSpace(msg.HubURL), "/")
		if !msg.Success {
			m.remoteDone = false
			m.remoteFailed = true
			m.cursor = onboardingRowActivate
			m.focusCursor()
			return m, nil
		}
		m.remoteFailed = false
		m.remoteDone = msg.HubServiceReady || msg.MachineReady
		switch {
		case msg.MachineReady:
			m.remoteStatus = onboardingText(m.lang, "activated")
			if strings.TrimSpace(msg.MachineID) != "" {
				m.remoteStatus += ": " + strings.TrimSpace(msg.MachineID)
			}
		case msg.HubServiceReady:
			m.remoteStatus = onboardingText(m.lang, "serviceReady")
		default:
			if strings.TrimSpace(m.remoteStatus) == "" {
				m.remoteStatus = onboardingText(m.lang, "serviceIncomplete")
			}
		}
		if m.remoteDone {
			m.cursor = onboardingRowFinish
			m.focusCursor()
		}
		return m, nil
	case OnboardingWeixinQRMsg:
		m.weixinBusy = false
		if !msg.Success {
			m.weixinStatus = msg.Message
			return m, nil
		}
		m.weixinQR = strings.TrimSpace(msg.QR)
		m.weixinToken = strings.TrimSpace(msg.Token)
		m.weixinElapsed = 0
		m.weixinStatus = onboardingText(m.lang, "waitingScan")
		return m, tea.Batch(
			func() tea.Msg { return OnboardingPollWeixinMsg{Token: m.weixinToken} },
			onboardingWeixinTick(m.weixinToken),
		)
	case OnboardingWeixinPollResultMsg:
		if !m.weixinPollTokenMatches(strings.TrimSpace(msg.Token)) {
			return m, nil
		}
		m.weixinStatus = msg.Message
		if m.weixinStatus == "" {
			m.weixinStatus = msg.Status
		}
		if msg.Completed {
			m.weixinBusy = false
			if msg.Success {
				m.weixinDone = true
				m.accountID = msg.AccountID
				m.weixinQR = ""
				m.weixinToken = ""
				m.weixinElapsed = 0
				m.weixinRefreshes = 0
				m.weixinStatus = onboardingText(m.lang, "bound")
			} else if strings.EqualFold(strings.TrimSpace(msg.Status), "expired") && m.weixinRefreshes < maxOnboardingWeixinRefreshes {
				m.weixinRefreshes++
				m.weixinQR = ""
				m.weixinToken = ""
				m.weixinElapsed = 0
				m.weixinBusy = true
				m.weixinStatus = onboardingText(m.lang, "refreshingQR")
				return m, func() tea.Msg { return OnboardingStartWeixinMsg{} }
			} else {
				m.weixinQR = ""
				m.weixinToken = ""
				m.weixinElapsed = 0
			}
			return m, nil
		}
		if m.weixinToken != "" {
			return m, func() tea.Msg { return OnboardingPollWeixinMsg{Token: m.weixinToken} }
		}
		return m, nil
	case OnboardingWeixinTickMsg:
		if !m.weixinPollTokenMatches(strings.TrimSpace(msg.Token)) || m.weixinQR == "" {
			return m, nil
		}
		m.weixinElapsed++
		return m, onboardingWeixinTick(m.weixinToken)
	case tea.KeyMsg:
		if m.weixinQR != "" {
			switch msg.String() {
			case "esc":
				m.weixinQR = ""
				m.weixinToken = ""
				m.weixinElapsed = 0
				if !m.weixinDone {
					m.weixinStatus = onboardingText(m.lang, "notBound")
				}
				return m, nil
			case "enter", "r":
				token := strings.TrimSpace(m.weixinToken)
				if token == "" {
					return m, nil
				}
				m.weixinStatus = onboardingText(m.lang, "waitingScan")
				return m, func() tea.Msg { return OnboardingPollWeixinMsg{Token: token} }
			}
		}
		if m.remoteBusy || m.weixinBusy {
			return m, nil
		}
		if m.hubCenterSelect {
			return m.updateHubCenterSelector(msg)
		}
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
			m.focusCursor()
			return m, nil
		case "down", "j", "tab":
			if m.cursor < onboardingRowCount-1 {
				m.cursor++
			}
			m.focusCursor()
			return m, nil
		case "shift+tab":
			if m.cursor > 0 {
				m.cursor--
			}
			m.focusCursor()
			return m, nil
		case " ":
			if m.cursor == onboardingRowLanguage {
				return m, m.cycleLanguage()
			}
			if m.cursor == onboardingRowHubCenter {
				before := m.hubCenterInput.Value()
				m.cycleHubCenter()
				if m.hubCenterInput.Value() != before {
					m.clearRemoteFailureAfterEdit()
				}
				return m, nil
			}
		case "enter":
			switch m.cursor {
			case onboardingRowLanguage:
				return m, m.cycleLanguage()
			case onboardingRowHubCenter:
				m.openHubCenterSelector()
				return m, nil
			case onboardingRowEmail, onboardingRowActivate:
				if m.remoteDone {
					m.cursor = onboardingRowFinish
					m.focusCursor()
					return m, nil
				}
				return m.startRemoteActivation()
			case onboardingRowWeixin:
				m.weixinBusy = true
				m.weixinRefreshes = 0
				m.weixinElapsed = 0
				m.weixinStatus = onboardingText(m.lang, "requestingQR")
				m.weixinQR = ""
				return m, func() tea.Msg { return OnboardingStartWeixinMsg{} }
			case onboardingRowFinish:
				m.done = true
				return m, func() tea.Msg { return OnboardingFinishMsg{} }
			}
		}
	}

	if m.cursor == onboardingRowEmail && !m.remoteBusy && !m.weixinBusy {
		before := m.emailInput.Value()
		var cmd tea.Cmd
		m.emailInput, cmd = m.emailInput.Update(msg)
		if m.emailInput.Value() != before {
			m.clearRemoteFailureAfterEdit()
		}
		return m, cmd
	}
	if m.cursor == onboardingRowHubCenter && m.hubCenterManual && !m.remoteBusy && !m.weixinBusy {
		before := m.hubCenterInput.Value()
		var cmd tea.Cmd
		m.hubCenterInput, cmd = m.hubCenterInput.Update(msg)
		if m.hubCenterInput.Value() != before {
			m.clearRemoteFailureAfterEdit()
		}
		return m, cmd
	}
	return m, nil
}

func (m OnboardingModel) weixinPollTokenMatches(token string) bool {
	token = strings.TrimSpace(token)
	if m.weixinToken != "" {
		return token == m.weixinToken
	}
	return token == ""
}

func (m *OnboardingModel) clearRemoteFailureAfterEdit() {
	if !m.remoteFailed {
		return
	}
	m.remoteFailed = false
	if strings.TrimSpace(m.emailInput.Value()) != "" {
		m.remoteStatus = onboardingText(m.lang, "emailSaved")
	} else {
		m.remoteStatus = onboardingText(m.lang, "notActivated")
	}
}

func (m OnboardingModel) View() string {
	if m.weixinQR != "" {
		return m.viewWeixinQR()
	}
	if m.useCompactView() {
		return m.viewCompact()
	}

	var b strings.Builder
	b.WriteString(onboardingTitle.Render("  "+onboardingText(m.lang, "title")) + "\n")
	b.WriteString("  " + onboardingDim.Render(fitOnboarding(onboardingText(m.lang, "subtitle"), max(10, m.width-2))) + "\n")
	b.WriteString("  " + onboardingNext.Render(fitOnboarding(m.nextStepText(), max(10, m.width-2))) + "\n\n")

	b.WriteString(m.renderRow(onboardingRowLanguage, onboardingText(m.lang, "language"), onboardingLanguageDisplay(m.lang), onboardingText(m.lang, "languageHint")))
	b.WriteString(m.renderRow(onboardingRowEmail, onboardingText(m.lang, "email"), m.emailInput.View(), ""))
	b.WriteString(m.renderRow(onboardingRowHubCenter, onboardingText(m.lang, "hubCenter"), m.hubCenterInput.View(), onboardingText(m.lang, "hubCenterHint")))
	if m.hubCenterSelect {
		b.WriteString(m.renderHubCenterSelector())
	}
	b.WriteString(m.renderRow(onboardingRowActivate, onboardingText(m.lang, "remote"), m.remoteStatus, onboardingText(m.lang, "remoteHint")))
	if m.hubURL != "" {
		prefix := onboardingText(m.lang, "hubURL") + ": "
		b.WriteString("  " + onboardingDim.Render(prefix+fitOnboarding(m.hubURL, max(8, m.width-lipgloss.Width(prefix)-2))) + "\n")
	}
	b.WriteString(m.renderRow(onboardingRowWeixin, onboardingText(m.lang, "weixin"), m.weixinStatus, onboardingText(m.lang, "weixinHint")))
	b.WriteString(m.renderRow(onboardingRowFinish, onboardingText(m.lang, "finish"), m.finishValue(), m.finishHint()))

	b.WriteString("\n  " + onboardingDim.Render(fitOnboarding(onboardingText(m.lang, "footer"), max(10, m.width-2))))
	return b.String()
}

func (m OnboardingModel) useCompactView() bool {
	return m.height > 0 && m.height < 16
}

func (m OnboardingModel) viewCompact() string {
	var b strings.Builder
	b.WriteString(onboardingTitle.Render("  "+onboardingText(m.lang, "title")) + "\n")
	b.WriteString("  " + onboardingNext.Render(fitOnboarding(m.nextStepText(), max(10, m.width-2))) + "\n")

	if m.hubCenterSelect {
		b.WriteString(m.renderRow(onboardingRowHubCenter, onboardingText(m.lang, "hubCenter"), m.hubCenterInput.View(), ""))
		b.WriteString(m.renderHubCenterSelector())
		b.WriteString("  " + onboardingDim.Render(fitOnboarding(onboardingText(m.lang, "footerCompact"), max(10, m.width-2))))
		return fitRenderedLines(b.String(), m.width)
	}

	rows := []string{
		m.renderRow(onboardingRowLanguage, onboardingText(m.lang, "language"), onboardingLanguageDisplay(m.lang), ""),
		m.renderRow(onboardingRowEmail, onboardingText(m.lang, "email"), m.emailInput.View(), ""),
		m.renderRow(onboardingRowHubCenter, onboardingText(m.lang, "hubCenter"), m.hubCenterInput.View(), ""),
		m.renderRow(onboardingRowActivate, onboardingText(m.lang, "remote"), m.remoteStatus, ""),
		m.renderRow(onboardingRowWeixin, onboardingText(m.lang, "weixin"), m.weixinStatus, ""),
		m.renderRow(onboardingRowFinish, onboardingText(m.lang, "finish"), m.finishValue(), ""),
	}
	visibleRows := min(onboardingRowCount, max(3, m.height-9))
	start, end := scrollWindow(len(rows), m.cursor, visibleRows)
	showMarkers := m.height >= 12
	if showMarkers && start > 0 {
		b.WriteString("  " + onboardingDim.Render(onboardingFormat(m.lang, "moreAbove", start)) + "\n")
	}
	for i := start; i < end; i++ {
		b.WriteString(rows[i])
	}
	if showMarkers && end < len(rows) {
		b.WriteString("  " + onboardingDim.Render(onboardingFormat(m.lang, "moreBelow", len(rows)-end)) + "\n")
	}
	b.WriteString("  " + onboardingDim.Render(fitOnboarding(onboardingText(m.lang, "footerCompact"), max(10, m.width-2))))
	return fitRenderedLines(b.String(), m.width)
}

func (m OnboardingModel) viewWeixinQR() string {
	var b strings.Builder
	b.WriteString(onboardingTitle.Render("  "+onboardingText(m.lang, "scan")) + "\n")
	b.WriteString("  " + onboardingDim.Render(fitOnboarding(onboardingText(m.lang, "scanSubtitle"), max(10, m.width-2))) + "\n")
	status := m.weixinStatus
	if m.weixinElapsed > 0 {
		status = fmt.Sprintf("%s (%ds)", status, m.weixinElapsed)
	}
	b.WriteString("  " + onboardingDim.Render(fitOnboarding(onboardingText(m.lang, "weixin")+": "+status, max(10, m.width-2))) + "\n\n")
	qrRows := 0
	if m.height > 0 {
		qrRows = max(1, m.height-10)
	}
	qrView, qrRendered := renderOnboardingQRWithLimitStatus(m.weixinQR, m.width, qrRows, m.lang)
	b.WriteString(qrView)
	if !qrRendered {
		payloadPrefix := onboardingText(m.lang, "payloadPrefix")
		payloadWidth := onboardingContentWidth(m.width, lipgloss.Width("  "+payloadPrefix))
		b.WriteString("  " + onboardingDim.Render(payloadPrefix+fitOnboarding(m.weixinQR, payloadWidth)) + "\n")
	}
	b.WriteString("  " + onboardingDim.Render(fitOnboarding(onboardingText(m.lang, "scanFooter"), onboardingContentWidth(m.width, 2))))
	return fitRenderedLines(b.String(), m.width)
}

func onboardingRemoteStatusKeys() []string {
	return []string{
		"notActivated", "emailSaved", "activated", "serviceReady", "serviceIncomplete", "emailRequired", "emailInvalid",
		"hubCenterInvalid", "activating",
	}
}

func onboardingWeixinStatusKeys() []string {
	return []string{"notBound", "bound", "waitingScan", "requestingQR", "refreshingQR"}
}

func translateOnboardingStatus(status, oldLang, newLang string, keys []string) string {
	for _, key := range keys {
		for _, lang := range []string{oldLang, "zh", "en"} {
			oldText := onboardingText(lang, key)
			if status == oldText {
				return onboardingText(newLang, key)
			}
			if strings.HasPrefix(status, oldText+": ") {
				return onboardingText(newLang, key) + strings.TrimPrefix(status, oldText)
			}
		}
	}
	return status
}

func (m OnboardingModel) startRemoteActivation() (OnboardingModel, tea.Cmd) {
	email := normalizeOnboardingEmail(m.emailInput.Value())
	if email == "" {
		m.remoteStatus = onboardingText(m.lang, "emailRequired")
		m.cursor = onboardingRowEmail
		m.focusCursor()
		return m, nil
	}
	if !validOnboardingEmail(email) {
		m.remoteStatus = onboardingText(m.lang, "emailInvalid")
		m.cursor = onboardingRowEmail
		m.focusCursor()
		return m, nil
	}
	hubCenterURL := normalizeOnboardingHubCenter(m.hubCenterInput.Value())
	if !validOnboardingHubCenterURL(hubCenterURL) {
		m.remoteStatus = onboardingText(m.lang, "hubCenterInvalid")
		m.cursor = onboardingRowHubCenter
		m.focusCursor()
		return m, nil
	}
	m.emailInput.SetValue(email)
	m.hubCenterInput.SetValue(hubCenterURL)
	m.remoteBusy = true
	m.remoteFailed = false
	m.remoteStatus = onboardingText(m.lang, "activating")
	return m, func() tea.Msg { return OnboardingActivateRemoteMsg{Email: email, HubCenterURL: hubCenterURL} }
}

func normalizeOnboardingEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validOnboardingEmail(email string) bool {
	if email == "" || strings.ContainsAny(email, " \t\r\n") {
		return false
	}
	at := strings.LastIndex(email, "@")
	if at <= 0 || at >= len(email)-1 {
		return false
	}
	domain := email[at+1:]
	return strings.Contains(domain, ".") && !strings.HasPrefix(domain, ".") && !strings.HasSuffix(domain, ".")
}

func normalizeOnboardingHubCenter(value string) string {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" {
		return remote.DefaultRemoteHubCenterURL
	}
	return value
}

func validOnboardingHubCenterURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func (m OnboardingModel) finishValue() string {
	if m.remoteDone {
		return onboardingText(m.lang, "finishRedeem")
	}
	return onboardingText(m.lang, "finishConfig")
}

func (m OnboardingModel) finishHint() string {
	if m.remoteDone {
		return onboardingText(m.lang, "finishHintRedeem")
	}
	return onboardingText(m.lang, "finishHintConfig")
}

func (m OnboardingModel) nextStepText() string {
	if !m.remoteDone {
		if m.remoteFailed {
			return onboardingText(m.lang, "nextActivateFailed")
		}
		if strings.TrimSpace(m.emailInput.Value()) == "" {
			return onboardingText(m.lang, "nextEmail")
		}
		return onboardingText(m.lang, "nextActivate")
	}
	if !m.weixinDone {
		return onboardingText(m.lang, "nextRedeemOptionalWeixin")
	}
	return onboardingText(m.lang, "nextRedeem")
}

func (m OnboardingModel) renderRow(idx int, label, value, hint string) string {
	prefix := "  "
	if idx == m.cursor {
		prefix = "> "
	}
	labelWidth := m.rowLabelWidth()
	showHint := hint != "" && m.width >= 76
	displayValue := value
	if !strings.Contains(value, "\x1b") {
		displayValue = fitOnboarding(value, m.rowValueWidth(showHint, hint))
	}
	line := fmt.Sprintf("%s%s  %s", prefix, padOnboarding(label, labelWidth), displayValue)
	if showHint {
		hintWidth := max(8, m.width-lipgloss.Width(line)-2)
		line += "  " + onboardingDim.Render(fitOnboarding(hint, hintWidth))
	}
	if idx == m.cursor {
		line = onboardingSelected.Render(line)
	}
	return line + "\n"
}

func renderOnboardingQR(content string, width int, lang ...string) string {
	return renderOnboardingQRWithLimit(content, width, 0, lang...)
}

func renderOnboardingQRWithLimit(content string, width, maxRows int, lang ...string) string {
	rendered, _ := renderOnboardingQRWithLimitStatus(content, width, maxRows, lang...)
	return rendered
}

func renderOnboardingQRWithLimitStatus(content string, width, maxRows int, lang ...string) (string, bool) {
	uiLang := "en"
	if len(lang) > 0 {
		uiLang = lang[0]
	}
	qr, err := qrcode.New(content, qrcode.Low)
	if err != nil {
		return "  " + onboardingDim.Render(fitOnboarding(onboardingText(uiLang, "qrFailed")+": "+err.Error(), onboardingContentWidth(width, 2))) + "\n", false
	}
	bitmap := qr.Bitmap()
	if len(bitmap) == 0 {
		return "", false
	}
	maxModules := (width - 4) / 2
	if maxModules > 0 && len(bitmap) > maxModules {
		return "  " + onboardingDim.Render(fitOnboarding(onboardingText(uiLang, "qrTooNarrow"), onboardingContentWidth(width, 2))) + "\n", false
	}
	if maxRows > 0 && (len(bitmap)+1)/2 > maxRows {
		return "  " + onboardingDim.Render(fitOnboarding(onboardingText(uiLang, "qrTooSmall"), onboardingContentWidth(width, 2))) + "\n", false
	}
	var b strings.Builder
	for y := 0; y < len(bitmap); y += 2 {
		b.WriteString("  ")
		for x := range bitmap[y] {
			top := bitmap[y][x]
			bottom := false
			if y+1 < len(bitmap) && x < len(bitmap[y+1]) {
				bottom = bitmap[y+1][x]
			}
			b.WriteString(onboardingQRHalfBlock(top, bottom))
		}
		b.WriteByte('\n')
	}
	return b.String(), true
}

func onboardingQRHalfBlock(topBlack, bottomBlack bool) string {
	fg := 37
	if topBlack {
		fg = 30
	}
	bg := 47
	if bottomBlack {
		bg = 40
	}
	return fmt.Sprintf("\x1b[%d;%dm\u2580\u2580\x1b[0m", fg, bg)
}

const (
	onboardingRowLanguage = iota
	onboardingRowEmail
	onboardingRowHubCenter
	onboardingRowActivate
	onboardingRowWeixin
	onboardingRowFinish
	onboardingRowCount
)

func (m *OnboardingModel) updateInputWidths() {
	valueWidth := m.rowValueWidth(false, "")
	m.emailInput.Width = min(36, max(12, valueWidth))
	m.hubCenterInput.Width = min(46, max(14, valueWidth))
}

func (m OnboardingModel) rowLabelWidth() int {
	switch {
	case m.width > 0 && m.width < 48:
		return 10
	case m.width > 0 && m.width < 64:
		return 14
	default:
		return 18
	}
}

func (m OnboardingModel) rowValueWidth(showHint bool, hint string) int {
	width := m.width
	if width <= 0 {
		width = 80
	}
	available := width - 2 - m.rowLabelWidth() - 2
	if showHint {
		available -= 2 + lipgloss.Width(hint)
	}
	return max(8, available)
}

func (m *OnboardingModel) focusCursor() {
	if m.cursor == onboardingRowEmail {
		m.emailInput.Focus()
	} else {
		m.emailInput.Blur()
	}
	if m.cursor == onboardingRowHubCenter && m.hubCenterManual && !m.hubCenterSelect {
		m.hubCenterInput.Focus()
	} else {
		m.hubCenterInput.Blur()
	}
}

func (m *OnboardingModel) cycleLanguage() tea.Cmd {
	next := "en"
	if i18n.NormalizeLang(m.lang) == "en" {
		next = "zh"
	}
	m.SetLang(next)
	return func() tea.Msg { return OnboardingLanguageChangedMsg{Language: next} }
}

func onboardingLanguageDisplay(lang string) string {
	if i18n.NormalizeLang(lang) == "en" {
		return "English"
	}
	return "中文"
}

func (m *OnboardingModel) cycleHubCenter() {
	if len(m.hubCenterOptions) == 0 {
		m.hubCenterOptions = cloneOnboardingValues(remote.DefaultRemoteHubCenterURLs...)
	}
	current := strings.TrimRight(strings.TrimSpace(m.hubCenterInput.Value()), "/")
	for i, opt := range m.hubCenterOptions {
		if opt == current {
			m.hubCenterInput.SetValue(m.hubCenterOptions[(i+1)%len(m.hubCenterOptions)])
			m.hubCenterManual = false
			m.hubCenterInput.CursorEnd()
			return
		}
	}
	m.hubCenterInput.SetValue(m.hubCenterOptions[0])
	m.hubCenterManual = false
	m.hubCenterInput.CursorEnd()
}

func (m OnboardingModel) updateHubCenterSelector(msg tea.KeyMsg) (OnboardingModel, tea.Cmd) {
	choiceCount := m.hubCenterChoiceCount()
	if choiceCount == 0 {
		m.hubCenterSelect = false
		m.focusCursor()
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.hubCenterSelect = false
	case "up", "k", "shift+tab":
		if m.hubCenterCursor > 0 {
			m.hubCenterCursor--
		}
	case "down", "j", "tab":
		if m.hubCenterCursor < choiceCount-1 {
			m.hubCenterCursor++
		}
	case "home", "g":
		m.hubCenterCursor = 0
	case "end", "G":
		m.hubCenterCursor = choiceCount - 1
	case " ", "enter":
		before := m.hubCenterInput.Value()
		if m.hubCenterCursor < len(m.hubCenterOptions) {
			m.hubCenterInput.SetValue(m.hubCenterOptions[m.hubCenterCursor])
			m.hubCenterManual = false
			m.hubCenterInput.CursorEnd()
		} else {
			m.hubCenterManual = true
			m.hubCenterInput.CursorEnd()
		}
		if m.hubCenterInput.Value() != before || m.hubCenterManual {
			m.clearRemoteFailureAfterEdit()
		}
		m.hubCenterSelect = false
	}
	m.focusCursor()
	return m, nil
}

func (m *OnboardingModel) openHubCenterSelector() {
	if len(m.hubCenterOptions) == 0 {
		m.hubCenterOptions = cloneOnboardingValues(remote.DefaultRemoteHubCenterURLs...)
	}
	current := strings.TrimRight(strings.TrimSpace(m.hubCenterInput.Value()), "/")
	m.hubCenterCursor = len(m.hubCenterOptions)
	for i, opt := range m.hubCenterOptions {
		if opt == current {
			m.hubCenterCursor = i
			break
		}
	}
	m.hubCenterSelect = true
	m.hubCenterManual = !containsOnboardingValue(m.hubCenterOptions, current)
	m.focusCursor()
}

func (m OnboardingModel) hubCenterChoiceCount() int {
	if len(m.hubCenterOptions) == 0 {
		return 0
	}
	return len(m.hubCenterOptions) + 1
}

func (m OnboardingModel) renderHubCenterSelector() string {
	if len(m.hubCenterOptions) == 0 {
		return ""
	}
	choices := append(cloneOnboardingValues(m.hubCenterOptions...), onboardingText(m.lang, "manualInput"))
	visibleCount := 5
	if m.height > 0 {
		visibleCount = min(visibleCount, max(3, m.height-10))
	}
	start := 0
	if m.hubCenterCursor >= visibleCount {
		start = m.hubCenterCursor - visibleCount + 1
	}
	end := min(len(choices), start+visibleCount)

	var b strings.Builder
	for i := start; i < end; i++ {
		prefix := "    "
		if i == m.hubCenterCursor {
			prefix = "  > "
		}
		value := choices[i]
		if i == len(m.hubCenterOptions) {
			value = onboardingText(m.lang, "manualInput")
		}
		b.WriteString(prefix + onboardingDim.Render(fitOnboarding(value, max(10, m.width-lipgloss.Width(prefix)-2))) + "\n")
	}
	b.WriteString("    " + onboardingDim.Render(fitOnboarding(onboardingText(m.lang, "hubCenterSelectFooter"), max(10, m.width-6))) + "\n")
	return b.String()
}

func hubCenterOnboardingOptions(cfg corelib.AppConfig) []string {
	values := []string{cfg.RemoteHubCenterURL}
	values = append(values, cfg.RemoteHubCenterURLs...)
	values = append(values, remote.DefaultRemoteHubCenterURLs...)
	return uniqueOnboardingValues(values...)
}

func uniqueOnboardingValues(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimRight(strings.TrimSpace(value), "/")
		if value == "" {
			continue
		}
		found := false
		for _, existing := range out {
			if existing == value {
				found = true
				break
			}
		}
		if !found {
			out = append(out, value)
		}
	}
	return out
}

func containsOnboardingValue(values []string, target string) bool {
	target = strings.TrimRight(strings.TrimSpace(target), "/")
	for _, value := range values {
		if strings.TrimRight(strings.TrimSpace(value), "/") == target {
			return true
		}
	}
	return false
}

func cloneOnboardingValues(values ...string) []string {
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func onboardingText(lang, key string) string {
	if i18n.NormalizeLang(lang) == "en" {
		texts := map[string]string{
			"title":                    "First-run Setup",
			"subtitle":                 "Activate Hub, or continue to local LLM settings. WeChat binding is optional.",
			"language":                 "Language",
			"languageHint":             "Space switches UI language",
			"email":                    "Email",
			"hubCenter":                "HubCenter",
			"hubCenterHint":            "Enter chooses; Space cycles",
			"hubCenterSelectFooter":    "Enter chooses, Esc closes; choose Manual input only for a private center.",
			"manualInput":              "Manual input",
			"hubURL":                   "Selected Hub",
			"remote":                   "Hub activation",
			"remoteHint":               "Enter activates; Done skips",
			"emailRequired":            "email is required",
			"emailInvalid":             "enter a valid email address",
			"hubCenterInvalid":         "HubCenter must be a valid http(s) URL",
			"activating":               "activating...",
			"requestingQR":             "requesting QR code...",
			"refreshingQR":             "QR expired; refreshing...",
			"weixin":                   "WeChat binding (optional)",
			"weixinHint":               "optional; Enter to show QR",
			"finish":                   "Done",
			"finishValue":              "mark onboarding complete",
			"finishRedeem":             "continue to Service Redeem",
			"finishConfig":             "finish and open LLM settings",
			"finishHintRedeem":         "Enter continues",
			"finishHintConfig":         "Enter opens settings",
			"notActivated":             "not activated",
			"emailSaved":               "email saved",
			"activated":                "activated",
			"serviceReady":             "Hub service ready",
			"serviceIncomplete":        "Hub activation incomplete; run activation again",
			"notBound":                 "not bound",
			"bound":                    "bound",
			"waitingScan":              "waiting for scan",
			"nextEmail":                "Next: enter email to activate Hub, or choose Done for local LLM settings.",
			"nextActivate":             "Next: press Enter to activate Hub; Hub URL is selected automatically.",
			"nextActivateFailed":       "Activation failed. Check email/HubCenter, then press Enter to retry.",
			"nextRedeemOptionalWeixin": "Next: finish to redeem MaClaw official service. WeChat can be bound later.",
			"nextRedeem":               "Next: finish to redeem MaClaw official service.",
			"scan":                     "Scan with WeChat",
			"scanSubtitle":             "Scan and confirm on the phone. Polling continues while this screen is open.",
			"scanFooter":               "Esc returns; Enter checks now. Payload appears only when QR cannot fit; use phone WeChat, not a desktop browser.",
			"payloadPrefix":            "payload: ",
			"qrFailed":                 "QR render failed",
			"qrTooNarrow":              "Terminal is too narrow for QR. Use the payload below or enlarge the window.",
			"qrTooSmall":               "Terminal is too small for QR. Use the payload below or enlarge the window.",
			"footer":                   "Up/Down selects, Enter chooses/actions, Space cycles language or HubCenter.",
			"footerCompact":            "Up/Down scrolls rows, Enter chooses/actions, Space cycles choices.",
			"moreAbove":                "... %d more above",
			"moreBelow":                "... %d more below",
		}
		return texts[key]
	}
	texts := map[string]string{
		"title":                    "首次设置",
		"subtitle":                 "可在 TUI 内激活 Hub，也可直接进入本地 LLM 设置；微信绑定可选。",
		"language":                 "界面语言",
		"languageHint":             "Space 切换界面语言",
		"email":                    "邮箱",
		"hubCenter":                "HubCenter",
		"hubCenterHint":            "Enter 选择；Space 切换",
		"hubCenterSelectFooter":    "Enter 选中，Esc 关闭；只有私有中心才使用手动输入。",
		"manualInput":              "手动输入",
		"hubURL":                   "已选择 Hub",
		"remote":                   "Hub 激活",
		"remoteHint":               "Enter 激活；完成可跳过",
		"emailRequired":            "邮箱不能为空",
		"emailInvalid":             "请输入有效邮箱地址",
		"hubCenterInvalid":         "HubCenter 必须是有效的 http(s) 地址",
		"activating":               "正在激活...",
		"requestingQR":             "正在请求二维码...",
		"refreshingQR":             "二维码已过期，正在刷新...",
		"weixin":                   "微信绑定（可选）",
		"weixinHint":               "可选；Enter 显示二维码",
		"finish":                   "完成",
		"finishValue":              "标记首次设置完成",
		"finishRedeem":             "继续服务兑换",
		"finishConfig":             "完成并打开 LLM 设置",
		"finishHintRedeem":         "Enter 继续",
		"finishHintConfig":         "Enter 打开设置",
		"notActivated":             "未激活",
		"emailSaved":               "邮箱已保存",
		"activated":                "已激活",
		"serviceReady":             "Hub 服务可用",
		"serviceIncomplete":        "Hub 激活不完整，请重新激活",
		"notBound":                 "未绑定",
		"bound":                    "已绑定",
		"waitingScan":              "等待扫码",
		"nextEmail":                "下一步：输入邮箱激活 Hub，或选择完成进入本地 LLM 设置。",
		"nextActivate":             "下一步：在 Hub 激活行按 Enter；Hub 地址会自动选择。",
		"nextActivateFailed":       "激活失败。检查邮箱/HubCenter 后，按 Enter 重试。",
		"nextRedeemOptionalWeixin": "下一步：完成后去服务兑换。微信可稍后再绑定。",
		"nextRedeem":               "下一步：完成后去服务兑换。",
		"scan":                     "用微信扫码",
		"scanSubtitle":             "扫码后在手机上确认；停留在此页时会自动轮询。",
		"scanFooter":               "Esc 返回；Enter 立即检查。只有二维码放不下时才显示载荷；请用手机微信打开/扫描。",
		"payloadPrefix":            "载荷: ",
		"qrFailed":                 "二维码渲染失败",
		"qrTooNarrow":              "终端太窄，无法完整显示二维码。请使用下方 payload，或放大窗口。",
		"qrTooSmall":               "终端太小，无法完整显示二维码。请使用下方 payload，或放大窗口。",
		"footer":                   "上下选择，Enter 选择或执行动作，Space 切换语言或 HubCenter。",
		"footerCompact":            "上下滚动设置项，Enter 选择或执行，Space 切换选项。",
		"moreAbove":                "... 上方还有 %d 项",
		"moreBelow":                "... 下方还有 %d 项",
	}
	return texts[key]
}

func onboardingFormat(lang, key string, args ...interface{}) string {
	return fmt.Sprintf(onboardingText(lang, key), args...)
}

func padOnboarding(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

func fitOnboarding(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 3 {
		return strings.Repeat(".", width)
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+3 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "..."
}

func onboardingContentWidth(width, used int) int {
	if width <= 0 {
		return 78
	}
	return max(1, width-used)
}

var (
	onboardingTitle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	onboardingDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	onboardingNext     = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	onboardingSelected = lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
)
