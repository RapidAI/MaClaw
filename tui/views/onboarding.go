package views

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	qrcode "github.com/skip2/go-qrcode"
)

type OnboardingResolveIdentityMsg struct {
	Identity     string
	HubCenterURL string
}
type OnboardingResolveIdentityResultMsg struct {
	Success    bool
	Message    string
	Identity   string
	Method     string // "email" or "phone"
	HubURL     string
	HubID      string
	TenantID   string
	CodeLength int
}
type OnboardingVerifyCodeMsg struct {
	Identity     string
	VerifyCode   string
	Method       string
	HubURL       string
	HubID        string
	TenantID     string
	HubCenterURL string
}
type OnboardingVerifyCodeResultMsg struct {
	Success         bool
	Message         string
	HubURL          string
	MachineID       string
	HubServiceReady bool
	MachineReady    bool
}
type OnboardingSMSTickMsg struct{}
type OnboardingStartSSOMsg struct{ FlowID string }
type OnboardingPollSSOMsg struct {
	FlowID string
	Client *http.Client
}
type OnboardingSubmitSSOInputMsg struct {
	FlowID string
	Input  string
}
type OnboardingCancelSSOMsg struct{ FlowID string }
type OnboardingSSOTickMsg struct{ FlowID string }
type OnboardingStartWeixinMsg struct{}
type OnboardingActivateRemoteMsg struct {
	Email        string
	HubCenterURL string
}
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

type OnboardingSSOQRMsg struct {
	FlowID     string
	Success    bool
	Message    string
	QR         string
	LoginURL   string
	PollClient *http.Client
}

type OnboardingSSOResultMsg struct {
	FlowID        string
	Success       bool
	Message       string
	AccessToken   string
	BaseURL       string
	Email         string
	ModelID       string
	ContextLength int
	KeepOpen      bool
	FromManual    bool
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
	smsCodeInput     textinput.Model
	hubCenterInput   textinput.Model
	ssoInput         textinput.Model
	hubCenterOptions []string
	hubCenterSelect  bool
	hubCenterManual  bool
	hubCenterCursor  int
	hubURL           string
	remoteDone       bool
	remoteBusy       bool
	remoteFailed     bool
	remoteStatus     string
	// Resolve + verify state (identity-first flow)
	resolvedMethod   string // "email" or "phone" — from Hub
	resolvedHubURL   string
	resolvedHubID    string
	resolvedTenantID string
	codeSent         bool // verification code has been sent
	codeCountdown    int
	codeLength       int
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
	tigerClaw        bool
	ssoDone          bool
	ssoBusy          bool
	ssoPolling       bool
	ssoSubmitting    bool
	ssoStatus        string
	ssoQR            string
	ssoLoginURL      string
	ssoFlowID        string
	ssoElapsed       int
	ssoTicking       bool
}

const maxOnboardingWeixinRefreshes = 3

var onboardingTigerClawDefault = strings.EqualFold(brand.Current().ID, "qianxin")

func onboardingWeixinTick(token string) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return OnboardingWeixinTickMsg{Token: token}
	})
}

func onboardingSSOTick(flowID string) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return OnboardingSSOTickMsg{FlowID: flowID}
	})
}

func onboardingCancelSSOCmd(flowID string) tea.Cmd {
	flowID = strings.TrimSpace(flowID)
	if flowID == "" {
		return nil
	}
	return func() tea.Msg { return OnboardingCancelSSOMsg{FlowID: flowID} }
}

func (m *OnboardingModel) ensureSSOTick() tea.Cmd {
	if m.ssoTicking || strings.TrimSpace(m.ssoFlowID) == "" {
		return nil
	}
	m.ssoTicking = true
	return onboardingSSOTick(m.ssoFlowID)
}

func NewOnboardingModel(lang string) OnboardingModel {
	lang = i18n.NormalizeLang(lang)
	emailInput := textinput.New()
	emailInput.Placeholder = "you@example.com / 13800138000"
	emailInput.CharLimit = 160
	emailInput.Width = 36
	smsCodeInput := textinput.New()
	smsCodeInput.Placeholder = "000000"
	smsCodeInput.CharLimit = 8
	smsCodeInput.Width = 12
	ssoInput := textinput.New()
	ssoInput.Placeholder = "paste returned URL or token"
	ssoInput.CharLimit = 4096
	ssoInput.Width = 46

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
		smsCodeInput:     smsCodeInput,
		hubCenterInput:   hubCenterInput,
		ssoInput:         ssoInput,
		hubCenterOptions: cloneOnboardingValues(remote.DefaultRemoteHubCenterURLs...),
		hubCenterManual:  envHubCenterURL != "",
		remoteStatus:     onboardingText(lang, "notActivated"),
		weixinStatus:     onboardingText(lang, "notBound"),
		tigerClaw:        onboardingTigerClawDefault,
		ssoStatus:        onboardingText(lang, "ssoNotSignedIn"),
		codeLength:       6,
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
	m.ssoStatus = translateOnboardingStatus(m.ssoStatus, oldLang, m.lang, onboardingSSOStatusKeys())
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
	if strings.TrimSpace(m.emailInput.Value()) == "" && cfg.RemoteMobile != "" {
		m.emailInput.SetValue(cfg.RemoteMobile)
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
	m.ssoDone = codeGenSSOReady(cfg)
	if m.ssoDone {
		m.ssoStatus = onboardingText(m.lang, "ssoSignedIn")
	} else {
		m.ssoStatus = onboardingText(m.lang, "ssoNotSignedIn")
	}
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
	if m.tigerClaw && !m.ssoDone && !cfg.OnboardingDone {
		m.cursor = onboardingRowSSO
	}
	m.focusCursor()
}

func codeGenSSOReady(cfg corelib.AppConfig) bool {
	for _, p := range cfg.MaclawLLMProviders {
		if strings.EqualFold(strings.TrimSpace(p.Name), "CodeGen") && strings.EqualFold(strings.TrimSpace(p.AuthType), "sso") {
			return strings.TrimSpace(p.Key) != "" && strings.TrimSpace(p.URL) != "" && strings.TrimSpace(p.Model) != ""
		}
	}
	return false
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
	return m.emailInput.Focused() || m.smsCodeInput.Focused() || m.hubCenterInput.Focused() || m.hubCenterSelect || m.ssoQR != "" || m.weixinQR != ""
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
	case OnboardingResolveIdentityResultMsg:
		m.remoteBusy = false
		if !msg.Success {
			m.remoteStatus = msg.Message
			m.remoteFailed = true
			return m, nil
		}
		m.resolvedMethod = msg.Method
		m.resolvedHubURL = msg.HubURL
		m.resolvedHubID = msg.HubID
		m.resolvedTenantID = msg.TenantID
		if msg.CodeLength > 0 {
			m.codeLength = msg.CodeLength
		}
		if msg.Method == "phone" {
			// Phone: code was sent, show verification input
			m.codeSent = true
			m.codeCountdown = 60
			m.remoteFailed = false
			m.remoteStatus = onboardingText(m.lang, "codeSent")
			m.cursor = onboardingRowSMSCode
			m.focusCursor()
			return m, onboardingSMSTick()
		}
		// Email: no verification code needed, enroll directly
		m.remoteBusy = true
		m.remoteFailed = false
		m.remoteStatus = onboardingText(m.lang, "activating")
		identity := strings.TrimSpace(m.emailInput.Value())
		hubCenterURL := normalizeOnboardingHubCenter(m.hubCenterInput.Value())
		return m, func() tea.Msg {
			return OnboardingVerifyCodeMsg{
				Identity:     identity,
				Method:       "email",
				HubURL:       msg.HubURL,
				HubID:        msg.HubID,
				TenantID:     msg.TenantID,
				HubCenterURL: hubCenterURL,
			}
		}
	case OnboardingSMSTickMsg:
		if m.codeCountdown > 0 {
			m.codeCountdown--
			if m.codeCountdown > 0 {
				return m, onboardingSMSTick()
			}
		}
		return m, nil
	case OnboardingVerifyCodeResultMsg:
		m.remoteBusy = false
		m.remoteStatus = msg.Message
		m.hubURL = strings.TrimRight(strings.TrimSpace(msg.HubURL), "/")
		if !msg.Success {
			m.remoteDone = false
			m.remoteFailed = true
			if m.codeSent {
				m.cursor = onboardingRowSMSCode
			} else {
				m.cursor = onboardingRowEmail
			}
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
	case OnboardingSSOQRMsg:
		if !m.ssoBusy || strings.TrimSpace(msg.FlowID) != strings.TrimSpace(m.ssoFlowID) {
			return m, nil
		}
		if !msg.Success {
			m.ssoBusy = false
			m.ssoPolling = false
			m.ssoSubmitting = false
			m.ssoTicking = false
			m.ssoQR = ""
			m.ssoFlowID = ""
			m.ssoInput.SetValue("")
			m.ssoInput.Blur()
			m.ssoStatus = msg.Message
			return m, nil
		}
		m.ssoQR = strings.TrimSpace(msg.QR)
		m.ssoLoginURL = strings.TrimSpace(msg.LoginURL)
		m.ssoElapsed = 0
		m.ssoStatus = strings.TrimSpace(msg.Message)
		if m.ssoStatus == "" {
			m.ssoStatus = onboardingText(m.lang, "ssoWaitingScan")
		}
		m.ssoInput.SetValue("")
		m.ssoInput.Focus()
		if msg.PollClient == nil {
			m.ssoBusy = false
			m.ssoPolling = false
			m.ssoSubmitting = false
			m.ssoTicking = false
			return m, nil
		}
		m.ssoBusy = true
		m.ssoPolling = true
		return m, tea.Batch(func() tea.Msg { return OnboardingPollSSOMsg{FlowID: m.ssoFlowID, Client: msg.PollClient} }, m.ensureSSOTick())
	case OnboardingSSOResultMsg:
		if strings.TrimSpace(msg.FlowID) != strings.TrimSpace(m.ssoFlowID) {
			return m, nil
		}
		if !msg.Success {
			if msg.FromManual {
				m.ssoSubmitting = false
			} else {
				m.ssoPolling = false
			}
			m.ssoBusy = m.ssoPolling || m.ssoSubmitting
			if !m.ssoBusy {
				m.ssoElapsed = 0
				m.ssoTicking = false
			}
			if !m.ssoSubmitting || msg.FromManual {
				m.ssoStatus = msg.Message
			}
			if !msg.KeepOpen {
				m.ssoBusy = false
				m.ssoPolling = false
				m.ssoSubmitting = false
				m.ssoTicking = false
				m.ssoQR = ""
				m.ssoFlowID = ""
				m.ssoInput.SetValue("")
				m.ssoInput.Blur()
			}
			return m, nil
		}
		m.ssoBusy = false
		m.ssoPolling = false
		m.ssoSubmitting = false
		m.ssoTicking = false
		m.ssoElapsed = 0
		m.ssoDone = true
		m.ssoQR = ""
		m.ssoFlowID = ""
		m.ssoInput.SetValue("")
		m.ssoInput.Blur()
		m.ssoStatus = onboardingText(m.lang, "ssoSignedIn")
		if strings.TrimSpace(msg.Email) != "" {
			m.ssoStatus += ": " + strings.TrimSpace(msg.Email)
		} else if strings.TrimSpace(msg.ModelID) != "" {
			m.ssoStatus += ": " + strings.TrimSpace(msg.ModelID)
		}
		m.cursor = onboardingRowWeixin
		m.focusCursor()
		return m, nil
	case OnboardingSSOTickMsg:
		if !m.ssoBusy || !m.ssoPollFlowMatches(strings.TrimSpace(msg.FlowID)) {
			return m, nil
		}
		m.ssoElapsed++
		m.ssoTicking = false
		return m, m.ensureSSOTick()
	case OnboardingWeixinQRMsg:
		if !m.weixinBusy {
			return m, nil
		}
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
				return m, tea.Batch(
					func() tea.Msg { return OnboardingStartWeixinMsg{} },
					onboardingWeixinTick(""),
				)
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
		if !m.weixinPollTokenMatches(strings.TrimSpace(msg.Token)) || (m.weixinQR == "" && !m.weixinBusy) {
			return m, nil
		}
		m.weixinElapsed++
		return m, onboardingWeixinTick(m.weixinToken)
	case tea.KeyMsg:
		if m.ssoBusy && msg.String() == "esc" {
			flowID := m.ssoFlowID
			m.ssoBusy = false
			m.ssoPolling = false
			m.ssoSubmitting = false
			m.ssoTicking = false
			m.ssoQR = ""
			m.ssoFlowID = ""
			m.ssoElapsed = 0
			m.ssoInput.SetValue("")
			m.ssoInput.Blur()
			if !m.ssoDone {
				m.ssoStatus = onboardingText(m.lang, "ssoNotSignedIn")
			}
			return m, onboardingCancelSSOCmd(flowID)
		}
		if m.ssoQR != "" {
			switch msg.String() {
			case "esc":
				flowID := m.ssoFlowID
				m.ssoBusy = false
				m.ssoPolling = false
				m.ssoSubmitting = false
				m.ssoTicking = false
				m.ssoQR = ""
				m.ssoFlowID = ""
				m.ssoElapsed = 0
				m.ssoInput.SetValue("")
				m.ssoInput.Blur()
				if !m.ssoDone {
					m.ssoStatus = onboardingText(m.lang, "ssoNotSignedIn")
				}
				return m, onboardingCancelSSOCmd(flowID)
			case "enter":
				if m.ssoSubmitting {
					return m, nil
				}
				input := strings.TrimSpace(m.ssoInput.Value())
				if input == "" {
					m.ssoStatus = onboardingText(m.lang, "ssoPasteRequired")
					return m, nil
				}
				m.ssoBusy = true
				m.ssoSubmitting = true
				m.ssoElapsed = 0
				m.ssoStatus = onboardingText(m.lang, "ssoValidating")
				return m, tea.Batch(
					func() tea.Msg { return OnboardingSubmitSSOInputMsg{FlowID: m.ssoFlowID, Input: input} },
					m.ensureSSOTick(),
				)
			}
			var cmd tea.Cmd
			m.ssoInput, cmd = m.ssoInput.Update(msg)
			return m, cmd
		}
		if m.weixinBusy && msg.String() == "esc" {
			m.weixinBusy = false
			m.weixinQR = ""
			m.weixinToken = ""
			m.weixinElapsed = 0
			if !m.weixinDone {
				m.weixinStatus = onboardingText(m.lang, "notBound")
			}
			return m, nil
		}
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
		if m.remoteBusy || m.ssoBusy || m.weixinBusy {
			return m, nil
		}
		if m.hubCenterSelect {
			return m.updateHubCenterSelector(msg)
		}
		switch msg.String() {
		case "up", "k":
			m.moveCursor(-1)
			m.focusCursor()
			return m, nil
		case "down", "j", "tab":
			m.moveCursor(1)
			m.focusCursor()
			return m, nil
		case "shift+tab":
			m.moveCursor(-1)
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
			case onboardingRowSSO:
				flowID := fmt.Sprintf("%d", time.Now().UnixNano())
				m.ssoBusy = true
				m.ssoPolling = false
				m.ssoSubmitting = false
				m.ssoTicking = false
				m.ssoDone = false
				m.ssoQR = ""
				m.ssoLoginURL = ""
				m.ssoFlowID = flowID
				m.ssoElapsed = 0
				m.ssoStatus = onboardingText(m.lang, "ssoRequestingQR")
				return m, tea.Batch(
					func() tea.Msg { return OnboardingStartSSOMsg{FlowID: flowID} },
					m.ensureSSOTick(),
				)
			case onboardingRowEmail, onboardingRowActivate:
				if m.remoteDone {
					m.cursor = onboardingRowFinish
					m.focusCursor()
					return m, nil
				}
				if m.codeSent && m.codeCountdown > 0 {
					// Code recently sent, verify it
					return m.startVerifyCode()
				}
				if m.codeSent && m.codeCountdown <= 0 {
					// Countdown expired, resend code
					m.codeSent = false
					m.resolvedMethod = ""
					m.resolvedHubURL = ""
					m.smsCodeInput.SetValue("")
					m.cursor = onboardingRowEmail
					m.focusCursor()
				}
				// Resolve identity + send code
				return m.startResolveIdentity()
			case onboardingRowSMSCode:
				if m.remoteDone {
					m.cursor = onboardingRowFinish
					m.focusCursor()
					return m, nil
				}
				return m.startVerifyCode()
			case onboardingRowWeixin:
				m.weixinBusy = true
				m.weixinRefreshes = 0
				m.weixinElapsed = 0
				m.weixinStatus = onboardingText(m.lang, "requestingQR")
				m.weixinQR = ""
				return m, tea.Batch(
					func() tea.Msg { return OnboardingStartWeixinMsg{} },
					onboardingWeixinTick(""),
				)
			case onboardingRowFinish:
				m.done = true
				return m, func() tea.Msg { return OnboardingFinishMsg{} }
			}
		}
	}

	if m.cursor == onboardingRowEmail && !m.remoteBusy && !m.ssoBusy && !m.weixinBusy {
		before := m.emailInput.Value()
		var cmd tea.Cmd
		m.emailInput, cmd = m.emailInput.Update(msg)
		if m.emailInput.Value() != before {
			m.clearRemoteFailureAfterEdit()
			// Reset resolve state if identity changed
			if m.codeSent {
				m.codeSent = false
				m.resolvedMethod = ""
				m.resolvedHubURL = ""
				m.smsCodeInput.SetValue("")
			}
		}
		return m, cmd
	}
	if m.cursor == onboardingRowSMSCode && !m.remoteBusy && !m.ssoBusy && !m.weixinBusy {
		var cmd tea.Cmd
		m.smsCodeInput, cmd = m.smsCodeInput.Update(msg)
		return m, cmd
	}
	if m.cursor == onboardingRowHubCenter && m.hubCenterManual && !m.remoteBusy && !m.ssoBusy && !m.weixinBusy {
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

func (m OnboardingModel) ssoPollFlowMatches(flowID string) bool {
	flowID = strings.TrimSpace(flowID)
	if m.ssoFlowID != "" {
		return flowID == m.ssoFlowID
	}
	return flowID == ""
}

func (m OnboardingModel) AcceptsSSOFlow(flowID string) bool {
	return m.ssoPollFlowMatches(flowID)
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
	if m.ssoQR != "" {
		return m.viewSSOQR()
	}
	if m.weixinQR != "" {
		return m.viewWeixinQR()
	}
	if m.hubCenterSelect {
		// HubCenter picker is available from Enter/Space on the hub row and must
		// render in both compact and full layouts.
		var b strings.Builder
		b.WriteString(onboardingTitle.Render("  "+onboardingText(m.lang, "title")) + "\n")
		b.WriteString("  " + onboardingNext.Render(fitOnboarding(m.nextStepText(), max(10, m.width-2))) + "\n")
		b.WriteString(m.renderRow(onboardingRowHubCenter, onboardingText(m.lang, "hubCenter"), m.hubCenterInput.View(), ""))
		b.WriteString(m.renderHubCenterSelector())
		b.WriteString("  " + onboardingDim.Render(fitOnboarding(onboardingText(m.lang, "footerCompact"), max(10, m.width-2))))
		return fitRenderedLines(b.String(), m.width)
	}
	if m.useCompactView() {
		return m.viewCompact()
	}

	var b strings.Builder
	b.WriteString(onboardingTitle.Render("  "+onboardingText(m.lang, "title")) + "\n")
	b.WriteString("  " + onboardingDim.Render(fitOnboarding(onboardingText(m.lang, "subtitle"), max(10, m.width-2))) + "\n")
	b.WriteString("  " + onboardingNext.Render(fitOnboarding(m.nextStepText(), max(10, m.width-2))) + "\n\n")

	b.WriteString(m.renderRow(onboardingRowLanguage, onboardingText(m.lang, "language"), onboardingLanguageDisplay(m.lang), onboardingText(m.lang, "languageHint")))
	if m.tigerClaw {
		b.WriteString(m.renderRow(onboardingRowSSO, onboardingText(m.lang, "sso"), m.ssoStatusText(), onboardingText(m.lang, "ssoHint")))
	} else {
		b.WriteString(m.renderRow(onboardingRowEmail, onboardingText(m.lang, "identity"), m.emailInput.View(), onboardingText(m.lang, "identityHint")))
		if m.codeSent {
			b.WriteString(m.renderRow(onboardingRowSMSCode, onboardingText(m.lang, "verifyCode"), m.smsCodeInput.View(), m.codeHint()))
		}
		b.WriteString(m.renderRow(onboardingRowActivate, onboardingText(m.lang, "remote"), m.remoteStatus, onboardingText(m.lang, "remoteHint")))
	}
	b.WriteString(m.renderRow(onboardingRowWeixin, onboardingText(m.lang, "weixin"), m.weixinStatusText(), onboardingText(m.lang, "weixinHint")))
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

	rows := m.compactRows()
	visibleRows := min(len(rows), max(3, m.height-9))
	start, end := scrollWindow(len(rows), m.compactRowIndex(m.cursor), visibleRows)
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

func (m OnboardingModel) compactRows() []string {
	if m.tigerClaw {
		return []string{
			m.renderRow(onboardingRowLanguage, onboardingText(m.lang, "language"), onboardingLanguageDisplay(m.lang), ""),
			m.renderRow(onboardingRowSSO, onboardingText(m.lang, "sso"), m.ssoStatusText(), ""),
			m.renderRow(onboardingRowWeixin, onboardingText(m.lang, "weixin"), m.weixinStatusText(), ""),
			m.renderRow(onboardingRowFinish, onboardingText(m.lang, "finish"), m.finishValue(), ""),
		}
	}
	rows := []string{
		m.renderRow(onboardingRowLanguage, onboardingText(m.lang, "language"), onboardingLanguageDisplay(m.lang), ""),
		m.renderRow(onboardingRowEmail, onboardingText(m.lang, "identity"), m.emailInput.View(), ""),
	}
	if m.codeSent {
		rows = append(rows, m.renderRow(onboardingRowSMSCode, onboardingText(m.lang, "verifyCode"), m.smsCodeInput.View(), ""))
	}
	rows = append(rows,
		m.renderRow(onboardingRowActivate, onboardingText(m.lang, "remote"), m.remoteStatus, ""),
		m.renderRow(onboardingRowWeixin, onboardingText(m.lang, "weixin"), m.weixinStatusText(), ""),
		m.renderRow(onboardingRowFinish, onboardingText(m.lang, "finish"), m.finishValue(), ""),
	)
	return rows
}

func (m OnboardingModel) compactRowIndex(row int) int {
	for i, candidate := range m.activeRows() {
		if candidate == row {
			return i
		}
	}
	return 0
}

func (m OnboardingModel) viewSSOQR() string {
	var b strings.Builder
	b.WriteString(onboardingTitle.Render("  "+onboardingText(m.lang, "ssoScan")) + "\n")
	b.WriteString("  " + onboardingDim.Render(fitOnboarding(onboardingText(m.lang, "ssoScanSubtitle"), max(10, m.width-2))) + "\n")
	b.WriteString("  " + onboardingDim.Render(fitOnboarding(onboardingText(m.lang, "sso")+": "+m.ssoStatusText(), max(10, m.width-2))) + "\n\n")
	qrRows := 0
	if m.height > 0 {
		qrRows = max(1, m.height-10)
	}
	qrView, qrRendered := renderOnboardingQRWithLimitStatus(m.ssoQR, m.width, qrRows, m.lang)
	b.WriteString(qrView)
	if !qrRendered {
		payloadPrefix := onboardingText(m.lang, "payloadPrefix")
		payloadWidth := onboardingContentWidth(m.width, lipgloss.Width("  "+payloadPrefix))
		b.WriteString("  " + onboardingDim.Render(payloadPrefix+fitOnboarding(m.ssoQR, payloadWidth)) + "\n")
	}
	if m.ssoLoginURL != "" {
		prefix := onboardingText(m.lang, "ssoLoginURL") + ": "
		b.WriteString("  " + onboardingDim.Render(prefix+fitOnboarding(m.ssoLoginURL, onboardingContentWidth(m.width, lipgloss.Width("  "+prefix)))) + "\n")
	}
	inputPrefix := onboardingText(m.lang, "ssoPastePrompt") + ": "
	inputWidth := onboardingContentWidth(m.width, lipgloss.Width("  "+inputPrefix))
	m.ssoInput.Width = min(72, max(16, inputWidth))
	b.WriteString("  " + inputPrefix + m.ssoInput.View() + "\n")
	b.WriteString("  " + onboardingDim.Render(fitOnboarding(onboardingText(m.lang, "ssoScanFooter"), onboardingContentWidth(m.width, 2))))
	return fitRenderedLines(b.String(), m.width)
}

func (m OnboardingModel) viewWeixinQR() string {
	var b strings.Builder
	b.WriteString(onboardingTitle.Render("  "+onboardingText(m.lang, "scan")) + "\n")
	b.WriteString("  " + onboardingDim.Render(fitOnboarding(onboardingText(m.lang, "scanSubtitle"), max(10, m.width-2))) + "\n")
	status := m.weixinStatusText()
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

func (m OnboardingModel) ssoStatusText() string {
	status := strings.TrimSpace(m.ssoStatus)
	if status == "" {
		status = onboardingText(m.lang, "ssoNotSignedIn")
	}
	if m.ssoElapsed > 0 {
		status = fmt.Sprintf("%s (%ds)", status, m.ssoElapsed)
	}
	return status
}

func (m OnboardingModel) weixinStatusText() string {
	status := strings.TrimSpace(m.weixinStatus)
	if status == "" {
		status = onboardingText(m.lang, "notBound")
	}
	if m.weixinElapsed > 0 {
		status = fmt.Sprintf("%s (%ds)", status, m.weixinElapsed)
	}
	return status
}

func onboardingRemoteStatusKeys() []string {
	return []string{
		"notActivated", "emailSaved", "activated", "serviceReady", "serviceIncomplete", "emailRequired", "emailInvalid",
		"hubCenterInvalid", "activating", "identityRequired", "codeRequired", "codeSent", "resolving",
	}
}

func onboardingSSOStatusKeys() []string {
	return []string{"ssoNotSignedIn", "ssoSignedIn", "ssoRequestingQR", "ssoWaitingScan", "ssoValidating", "ssoPasteRequired"}
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

func (m OnboardingModel) startResolveIdentity() (OnboardingModel, tea.Cmd) {
	identity := strings.TrimSpace(m.emailInput.Value())
	if identity == "" {
		m.remoteStatus = onboardingText(m.lang, "identityRequired")
		m.cursor = onboardingRowEmail
		m.focusCursor()
		return m, nil
	}
	// Client-side guard: reject values that are neither a plausible email nor phone.
	// Server still re-validates; this avoids starting a resolve round-trip on typos.
	if strings.Contains(identity, "@") {
		if !validOnboardingEmail(normalizeOnboardingEmail(identity)) {
			m.remoteStatus = onboardingText(m.lang, "emailInvalid")
			m.cursor = onboardingRowEmail
			m.focusCursor()
			return m, nil
		}
	} else {
		phone := normalizeOnboardingPhone(identity)
		if len(phone) < 8 || len(phone) > 15 {
			m.remoteStatus = onboardingText(m.lang, "emailInvalid")
			m.cursor = onboardingRowEmail
			m.focusCursor()
			return m, nil
		}
	}
	hubCenterURL := normalizeOnboardingHubCenter(m.hubCenterInput.Value())
	m.remoteBusy = true
	m.remoteFailed = false
	m.remoteStatus = onboardingText(m.lang, "resolving")
	return m, func() tea.Msg { return OnboardingResolveIdentityMsg{Identity: identity, HubCenterURL: hubCenterURL} }
}

func (m OnboardingModel) startVerifyCode() (OnboardingModel, tea.Cmd) {
	code := strings.TrimSpace(m.smsCodeInput.Value())
	if code == "" {
		m.remoteStatus = onboardingText(m.lang, "codeRequired")
		m.cursor = onboardingRowSMSCode
		m.focusCursor()
		return m, nil
	}
	identity := strings.TrimSpace(m.emailInput.Value())
	hubCenterURL := normalizeOnboardingHubCenter(m.hubCenterInput.Value())
	m.remoteBusy = true
	m.remoteFailed = false
	m.remoteStatus = onboardingText(m.lang, "activating")
	return m, func() tea.Msg {
		return OnboardingVerifyCodeMsg{
			Identity:     identity,
			VerifyCode:   code,
			Method:       m.resolvedMethod,
			HubURL:       m.resolvedHubURL,
			HubID:        m.resolvedHubID,
			TenantID:     m.resolvedTenantID,
			HubCenterURL: hubCenterURL,
		}
	}
}

func (m OnboardingModel) codeHint() string {
	if m.codeCountdown > 0 {
		return fmt.Sprintf(onboardingText(m.lang, "codeCountdown"), m.codeLength, m.codeCountdown)
	}
	return onboardingText(m.lang, "codeExpiredHint")
}

func normalizeOnboardingPhone(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func onboardingSMSTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return OnboardingSMSTickMsg{}
	})
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
	if m.tigerClaw {
		return onboardingText(m.lang, "finishValue")
	}
	if m.remoteDone {
		return onboardingText(m.lang, "finishRedeem")
	}
	return onboardingText(m.lang, "finishConfig")
}

func (m OnboardingModel) finishHint() string {
	if m.tigerClaw {
		return onboardingText(m.lang, "finishHintConfig")
	}
	if m.remoteDone {
		return onboardingText(m.lang, "finishHintRedeem")
	}
	return onboardingText(m.lang, "finishHintConfig")
}

func (m OnboardingModel) nextStepText() string {
	if m.tigerClaw {
		if !m.ssoDone {
			return onboardingText(m.lang, "nextSSO")
		}
		if !m.weixinDone {
			return onboardingText(m.lang, "nextTigerWeixin")
		}
		return onboardingText(m.lang, "nextDone")
	}
	if !m.remoteDone {
		if m.remoteFailed {
			return onboardingText(m.lang, "nextActivateFailed")
		}
		if m.codeSent {
			return onboardingText(m.lang, "nextSMSCode")
		}
		return onboardingText(m.lang, "nextEmail")
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
	onboardingRowSMSCode
	onboardingRowHubCenter
	onboardingRowActivate
	onboardingRowSSO
	onboardingRowWeixin
	onboardingRowFinish
	onboardingRowCount
)

func (m OnboardingModel) activeRows() []int {
	if m.tigerClaw {
		return []int{onboardingRowLanguage, onboardingRowSSO, onboardingRowWeixin, onboardingRowFinish}
	}
	rows := []int{onboardingRowLanguage, onboardingRowEmail}
	if m.codeSent {
		rows = append(rows, onboardingRowSMSCode)
	}
	rows = append(rows, onboardingRowActivate, onboardingRowWeixin, onboardingRowFinish)
	return rows
}

func (m *OnboardingModel) moveCursor(delta int) {
	rows := m.activeRows()
	if len(rows) == 0 || delta == 0 {
		return
	}
	idx := 0
	for i, row := range rows {
		if row == m.cursor {
			idx = i
			break
		}
	}
	idx += delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(rows) {
		idx = len(rows) - 1
	}
	m.cursor = rows[idx]
}

func (m *OnboardingModel) updateInputWidths() {
	valueWidth := m.rowValueWidth(false, "")
	m.emailInput.Width = min(36, max(12, valueWidth))
	m.smsCodeInput.Width = min(12, max(8, valueWidth))
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
	if m.cursor == onboardingRowSMSCode {
		m.smsCodeInput.Focus()
	} else {
		m.smsCodeInput.Blur()
	}
	if m.cursor == onboardingRowHubCenter && m.hubCenterManual && !m.hubCenterSelect {
		m.hubCenterInput.Focus()
	} else {
		m.hubCenterInput.Blur()
	}
	if m.ssoQR == "" {
		m.ssoInput.Blur()
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
			"subtitle":                 "Register with email or phone to activate service. WeChat binding is optional.",
			"language":                 "Language",
			"languageHint":             "Space switches UI language",
			"identity":                 "User ID",
			"identityHint":             "email or phone; Enter to continue",
			"identityRequired":         "enter email or phone number",
			"verifyCode":               "Verify code",
			"codeHint":                 "enter %d-digit code, then Enter",
			"codeCountdown":            "%d-digit code; resend in %ds",
			"codeResend":               "resend in %ds",
			"codeExpiredHint":          "Enter on User ID row to resend",
			"codeSent":                 "code sent, enter below",
			"codeRequired":             "enter the verification code",
			"resolving":                "resolving hub...",
			"hubCenter":                "HubCenter",
			"hubCenterHint":            "Enter chooses; Space cycles",
			"hubCenterSelectFooter":    "Enter chooses, Esc closes; choose Manual input only for a private center.",
			"manualInput":              "Manual input",
			"hubURL":                   "Selected Hub",
			"remote":                   "Status",
			"remoteHint":               "shows activation status",
			"sso":                      "Enterprise SSO",
			"ssoHint":                  "Enter signs in; Esc cancels",
			"ssoNotSignedIn":           "not signed in",
			"ssoSignedIn":              "signed in",
			"ssoRequestingQR":          "requesting SSO QR code...",
			"ssoWaitingScan":           "waiting for SSO scan",
			"ssoValidating":            "validating SSO token...",
			"ssoPasteRequired":         "paste returned URL or token first",
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
			"nextSSO":                  "Next: press Enter to sign in with enterprise SSO.",
			"nextTigerWeixin":          "Next: WeChat binding is optional; finish when ready.",
			"nextDone":                 "Next: finish onboarding and start using TigerClaw.",
			"nextEmail":                "Next: enter email or phone, then Enter to receive verification code.",
			"nextPhone":                "Next: enter email or phone, then Enter to receive verification code.",
			"nextSMSCode":              "Next: enter the verification code, then press Enter to activate.",
			"nextActivate":             "Next: press Enter to activate.",
			"nextActivateFailed":       "Activation failed. Check your input and press Enter to retry.",
			"nextRedeemOptionalWeixin": "Next: finish to redeem MaClaw official service. WeChat can be bound later.",
			"nextRedeem":               "Next: finish to redeem MaClaw official service.",
			"scan":                     "Scan with WeChat",
			"scanSubtitle":             "Scan and confirm on the phone. Polling continues while this screen is open.",
			"scanFooter":               "Esc returns; Enter checks now. Payload appears only when QR cannot fit; use phone WeChat, not a desktop browser.",
			"ssoScan":                  "Enterprise SSO Login",
			"ssoScanSubtitle":          "Scan with enterprise mobile SSO. Polling continues in the background.",
			"ssoScanFooter":            "Esc cancels. If QR cannot fit, use the payload or login URL.",
			"ssoLoginURL":              "login URL",
			"ssoPastePrompt":           "returned URL/token",
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
		"subtitle":                 "输入邮箱或手机号注册激活服务；微信绑定可选。",
		"language":                 "界面语言",
		"languageHint":             "Space 切换界面语言",
		"identity":                 "用户ID",
		"identityHint":             "邮箱或手机号；Enter 继续",
		"identityRequired":         "请输入邮箱或手机号",
		"verifyCode":               "验证码",
		"codeHint":                 "输入 %d 位验证码，Enter 确认",
		"codeCountdown":            "%d 位验证码；%d 秒后可重发",
		"codeResend":               "%d 秒后可重发",
		"codeExpiredHint":          "在用户ID行按 Enter 重发",
		"codeSent":                 "验证码已发送，在下方输入",
		"codeRequired":             "请输入验证码",
		"resolving":                "正在连接服务...",
		"hubCenter":                "HubCenter",
		"hubCenterHint":            "Enter 选择；Space 切换",
		"hubCenterSelectFooter":    "Enter 选中，Esc 关闭；只有私有中心才使用手动输入。",
		"manualInput":              "手动输入",
		"hubURL":                   "已选择 Hub",
		"remote":                   "状态",
		"remoteHint":               "显示激活状态",
		"sso":                      "企业 SSO",
		"ssoHint":                  "Enter 登录；Esc 取消",
		"ssoNotSignedIn":           "未登录",
		"ssoSignedIn":              "已登录",
		"ssoRequestingQR":          "正在请求 SSO 二维码...",
		"ssoWaitingScan":           "等待 SSO 扫码",
		"ssoValidating":            "正在验证 SSO token...",
		"ssoPasteRequired":         "请先粘贴返回 URL 或 token",
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
		"nextSSO":                  "下一步：按 Enter 进行企业 SSO 登录。",
		"nextTigerWeixin":          "下一步：微信绑定可选；准备好后完成。",
		"nextDone":                 "下一步：完成 onboarding，进入 TigerClaw。",
		"nextEmail":                "下一步：输入邮箱或手机号，按 Enter 接收验证码。",
		"nextPhone":                "下一步：输入邮箱或手机号，按 Enter 接收验证码。",
		"nextSMSCode":              "下一步：输入验证码，按 Enter 激活。",
		"nextActivate":             "下一步：按 Enter 激活。",
		"nextActivateFailed":       "激活失败。检查输入后按 Enter 重试。",
		"nextRedeemOptionalWeixin": "下一步：完成后去服务兑换。微信可稍后再绑定。",
		"nextRedeem":               "下一步：完成后去服务兑换。",
		"scan":                     "用微信扫码",
		"scanSubtitle":             "扫码后在手机上确认；停留在此页时会自动轮询。",
		"scanFooter":               "Esc 返回；Enter 立即检查。只有二维码放不下时才显示载荷；请用手机微信打开/扫描。",
		"ssoScan":                  "企业 SSO 登录",
		"ssoScanSubtitle":          "用企业移动端扫码确认；TUI 会在后台轮询。",
		"ssoScanFooter":            "Esc 取消。如果二维码放不下，使用 payload 或登录 URL。",
		"ssoLoginURL":              "登录 URL",
		"ssoPastePrompt":           "返回 URL/token",
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
