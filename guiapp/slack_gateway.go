package guiapp

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/slack"
	"github.com/RapidAI/CodeClaw/corelib/textutil"
	"github.com/pkg/browser"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// beginSlackLogin is the browser install. Tests replace it so they do not
// open a window or talk to Slack.
var beginSlackLogin = slack.Login

var slackLoginMu sync.Mutex

// slackGatewayManager runs the desktop Slack channel in local mode.
// Replies stay on this machine. The model still uses im_message; Slack is
// only another delivery channel.
type slackGatewayManager struct {
	app          *App
	mu           sync.Mutex
	syncMu       sync.Mutex // SyncFromConfig/Stop; never take while holding mu
	gateway      *slack.Gateway
	status       gatewayConnectionStatus
	lastToken    string
	lastErr      string
	lastChannel  string
	localHandler *IMMessageHandler
}

func newSlackGatewayManager(app *App) *slackGatewayManager {
	return &slackGatewayManager{
		app:    app,
		status: gatewayConnectionStatusDisconnected,
	}
}

func (m *slackGatewayManager) currentLocalHandler() *IMMessageHandler {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.localHandler
}

func (m *slackGatewayManager) SyncFromConfig() {
	m.syncMu.Lock()
	defer m.syncMu.Unlock()

	for {
		cfg, err := m.app.LoadConfig()
		if err != nil {
			return
		}
		m.mu.Lock()
		if !cfg.SlackEnabled || strings.TrimSpace(cfg.SlackBotToken) == "" {
			gw := m.gateway
			m.gateway = nil
			m.lastToken = ""
			m.status = gatewayConnectionStatusDisconnected
			m.mu.Unlock()
			if gw != nil {
				releaseIMSyncLock(&m.syncMu, func() { _ = gw.Stop() })
				m.mu.Lock()
				replaced := m.gateway != nil
				m.mu.Unlock()
				if replaced {
					return
				}
				m.emitStatusEvent()
			}
			return
		}
		if m.gateway != nil && m.lastToken == cfg.SlackBotToken {
			m.mu.Unlock()
			return
		}
		old := m.gateway
		m.gateway = nil
		m.mu.Unlock()
		if old != nil {
			releaseIMSyncLock(&m.syncMu, func() { _ = old.Stop() })
			continue
		}

		gw := slack.NewGateway(cfg.SlackBotToken, cfg.SlackBotUserID, m.onIncomingMessage)
		gw.SetStatusCallback(m.onStatusChange)
		if err := gw.Start(context.Background()); err != nil {
			log.Printf("[slack-mgr] start failed: %v", err)
			m.mu.Lock()
			m.status = gatewayConnectionStatusError
			m.lastToken = ""
			m.lastErr = explainSlackStartError(err)
			m.mu.Unlock()
			m.emitStatusEvent()
			return
		}
		m.mu.Lock()
		m.gateway = gw
		m.lastToken = cfg.SlackBotToken
		m.lastErr = ""
		m.mu.Unlock()
		return
	}
}

func (m *slackGatewayManager) Stop() {
	if m == nil {
		return
	}
	m.syncMu.Lock()
	defer m.syncMu.Unlock()

	m.mu.Lock()
	gw := m.gateway
	m.gateway = nil
	m.lastToken = ""
	m.status = gatewayConnectionStatusDisconnected
	m.localHandler = nil
	m.mu.Unlock()
	if gw != nil {
		releaseIMSyncLock(&m.syncMu, func() { _ = gw.Stop() })
	}
	m.mu.Lock()
	replaced := m.gateway != nil
	m.mu.Unlock()
	if !replaced {
		m.emitStatusEvent()
	}
}

func (m *slackGatewayManager) connectionError() string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	errText := m.lastErr
	gw := m.gateway
	m.mu.Unlock()
	if errText != "" {
		return errText
	}
	if gw == nil {
		return ""
	}
	return gw.LastError()
}

func explainSlackStartError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "invalid_auth"), strings.Contains(msg, "token_revoked"), strings.Contains(msg, "token_expired"), strings.Contains(msg, "not_authed"), strings.Contains(msg, "account_inactive"):
		return "Slack 令牌已失效，请重新登录"
	case strings.Contains(msg, "missing_scope"):
		return "Slack 权限不足，请断开后重新登录"
	default:
		return msg
	}
}

func (m *slackGatewayManager) Status() string {
	if m == nil {
		return gatewayConnectionStatusDisconnected.String()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status == gatewayConnectionStatusUnknown {
		return gatewayConnectionStatusDisconnected.String()
	}
	return m.status.String()
}

func (m *slackGatewayManager) onStatusChange(status string) {
	next := normalizeGatewayConnectionStatus(status)
	if next == gatewayConnectionStatusUnknown {
		next = gatewayConnectionStatusDisconnected
	}
	m.mu.Lock()
	prev := m.status
	m.status = next
	m.mu.Unlock()
	if prev != next {
		m.emitStatusEvent()
	}
}

func (m *slackGatewayManager) emitStatusEvent() {
	if m == nil || m.app == nil {
		return
	}
	m.app.emitEvent("slack-status-changed", m.Status())
}

func (m *slackGatewayManager) noteChannel(channelID string) {
	channelID = strings.TrimSpace(channelID)
	if m == nil || channelID == "" {
		return
	}
	m.mu.Lock()
	m.lastChannel = channelID
	m.mu.Unlock()
}

func (m *slackGatewayManager) lastChannelID() string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastChannel
}

// SendProactiveText posts to channelID. An empty id uses the latest Slack DM.
func (m *slackGatewayManager) SendProactiveText(channelID, text string) (string, error) {
	channelID = strings.TrimSpace(channelID)
	if scheduler.IsSelfPeerID(channelID) {
		channelID = ""
	}
	if channelID == "" {
		channelID = m.lastChannelID()
	}
	if channelID == "" {
		return "", fmt.Errorf("slack target is unavailable")
	}
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		return "", fmt.Errorf("slack gateway unavailable")
	}
	if err := gw.SendText(context.Background(), channelID, text); err != nil {
		return "", err
	}
	m.noteChannel(channelID)
	return channelID, nil
}

func (m *slackGatewayManager) onIncomingMessage(msg slack.IncomingMessage) {
	m.noteChannel(msg.ChannelID)
	if isPassthroughSlashText(msg.Text) {
		m.handleLocalMessage(msg)
		return
	}
	m.handleLocalMessage(msg)
}

func (m *slackGatewayManager) handleLocalMessage(msg slack.IncomingMessage) {
	replyTo := func(text string) {
		m.mu.Lock()
		gw := m.gateway
		m.mu.Unlock()
		if gw == nil || strings.TrimSpace(text) == "" {
			return
		}
		if err := gw.SendThreadText(context.Background(), msg.ChannelID, msg.ThreadTS, text); err != nil {
			log.Printf("[slack-mgr] reply: %v", err)
		}
	}
	if resp, handled := m.app.TryHandlePassthroughSlashCommandWithSource(msg.Text, "slack:"+msg.ChannelID); handled {
		reply := resp.Text
		if reply == "" {
			reply = resp.Error
		}
		if reply == "" {
			reply = "(no output)"
		}
		replyTo(reply)
		return
	}
	if !m.app.isMaclawLLMConfigured() {
		replyTo(i18n.T(i18n.MsgLLMNotConfigured, "zh"))
		return
	}
	handler := m.ensureLocalHandler()
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}
	progressFilter := newIMProgressVisibilityFilter(m.app)
	var lastProgress time.Time
	onProgress := func(progressText string) {
		if !progressFilter.ShouldSendProgress(progressText) {
			return
		}
		now := time.Now()
		if now.Sub(lastProgress) < 5*time.Second {
			return
		}
		lastProgress = now
		replyTo(i18n.T(i18n.MsgProgressPrefix, appUILang(m.app)) + textutil.StripMarkdown(progressText))
	}
	resp := handler.HandleIMMessageWithProgress(IMUserMessage{
		UserID:   msg.ChannelID,
		Platform: "slack_local",
		Text:     text,
		Lang:     appUILang(m.app),
	}, onProgress)
	if resp == nil || resp.Deferred {
		return
	}
	if resp.Text != "" {
		replyTo(textutil.StripMarkdown(resp.Text))
	} else if resp.Error != "" {
		replyTo(textutil.StripMarkdown(resp.Error))
	}
}

func (m *slackGatewayManager) ensureLocalHandler() *IMMessageHandler {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.localHandler != nil {
		return m.localHandler
	}
	a := m.app
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		a.ensureMemoryStore()
	}
	if a.contextResolver == nil {
		a.ensureContextResolver()
	}
	if a.sessionPrecheck == nil {
		a.ensureSessionPrecheck()
	}
	h := NewIMMessageHandler(a, a.remoteSessions)
	if a.capabilityGapDetector == nil {
		a.ensureCapabilityGapDetector()
	}
	if a.capabilityGapDetector != nil {
		h.SetCapabilityGapDetector(a.capabilityGapDetector)
	}
	if a.toolDefGenerator != nil {
		h.SetToolDefGenerator(a.toolDefGenerator)
	}
	if a.toolRouter != nil {
		h.SetToolRouter(a.toolRouter)
	}
	if a.usageTracker != nil {
		h.SetUsageTracker(a.usageTracker)
	}
	if a.memoryStore != nil {
		h.SetMemoryStore(a.memoryStore)
	}
	h.SetTrajectoryRecorderFactory(a.buildTrajectoryRecorderFactory())
	if a.configManager != nil {
		h.SetConfigManager(a.configManager)
	}
	if a.templateManager != nil {
		h.SetTemplateManager(a.templateManager)
	}
	if a.scheduledTaskManager != nil {
		h.SetScheduledTaskManager(a.scheduledTaskManager)
	}
	if a.contextResolver != nil {
		h.SetContextResolver(a.contextResolver)
	}
	if a.sessionPrecheck != nil {
		h.SetSessionPrecheck(a.sessionPrecheck)
	}
	a.ensureStartupFeedback()
	if a.startupFeedback != nil {
		h.SetStartupFeedback(a.startupFeedback)
	}
	if a.securityFirewall == nil {
		a.ensureSecurityFirewall()
	}
	if a.securityFirewall != nil {
		h.SetSecurityFirewall(a.securityFirewall)
	}
	a.ensureConversationArchiver()
	if a.conversationArchiver != nil {
		h.memory.Archiver = a.conversationArchiver
	}
	m.localHandler = h
	return h
}

func (a *App) ensureSlackGateway() {
	cfg, err := a.LoadConfig()
	if err != nil {
		return
	}
	if !cfg.SlackEnabled || strings.TrimSpace(cfg.SlackBotToken) == "" {
		a.imGatewaySyncMu.Lock()
		m := a.slackGateway
		a.imGatewaySyncMu.Unlock()
		if m != nil {
			m.SyncFromConfig()
		}
		return
	}
	a.imGatewaySyncMu.Lock()
	if a.slackGateway == nil {
		a.slackGateway = newSlackGatewayManager(a)
	}
	m := a.slackGateway
	a.imGatewaySyncMu.Unlock()
	m.SyncFromConfig()
}

// GetSlackStatus reports the desktop Slack connection.
func (a *App) GetSlackStatus() string {
	if a == nil || a.slackGateway == nil {
		return gatewayConnectionStatusDisconnected.String()
	}
	return a.slackGateway.Status()
}

// GetSlackConnectionError is the latest Slack failure to show under the status.
func (a *App) GetSlackConnectionError() string {
	if a == nil || a.slackGateway == nil {
		return ""
	}
	return a.slackGateway.connectionError()
}

// GetSlackLoginHint tells the settings page which redirect URL and bot scopes
// to register on the Slack app before login.
func (a *App) GetSlackLoginHint() map[string]string {
	return map[string]string{
		"redirect_uri": slack.DefaultRedirectURI,
		"scopes":       slack.BotScopes,
	}
}

// StartSlackLogin opens Slack in the browser, saves the workspace install,
// and starts the local gateway. The Slack app credentials are built in.
func (a *App) StartSlackLogin() (string, error) {
	if a == nil {
		return "", fmt.Errorf("应用未初始化")
	}
	if !slackLoginMu.TryLock() {
		return "", fmt.Errorf("Slack 登录正在进行")
	}
	defer slackLoginMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	session, err := beginSlackLogin(ctx, slack.ProductClientID, slack.ProductClientSecret, func(raw string) error {
		return openSlackLoginURL(a.ctx, raw)
	}, slack.LoginOptions{})
	if err != nil {
		return "", err
	}
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.SlackEnabled = true
		cfg.SlackBotToken = session.BotToken
		cfg.SlackTeamID = session.TeamID
		cfg.SlackTeamName = session.TeamName
		cfg.SlackBotUserID = session.BotUserID
	}); err != nil {
		return "", err
	}
	a.ensureSlackGateway()
	name := strings.TrimSpace(session.TeamName)
	if name == "" {
		name = "Slack"
	}
	if a.GetSlackStatus() == gatewayConnectionStatusError.String() {
		if detail := a.GetSlackConnectionError(); detail != "" {
			return "", fmt.Errorf("已登录 %s，但连接失败：%s", name, detail)
		}
		return "", fmt.Errorf("已登录 %s，但连接失败", name)
	}
	return fmt.Sprintf("已连接 Slack 工作区 %s", name), nil
}

// DisconnectSlack stops the gateway and forgets the workspace install.
// The next login uses the built-in Slack app.
func (a *App) DisconnectSlack() error {
	if a == nil {
		return fmt.Errorf("应用未初始化")
	}
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.SlackEnabled = false
		cfg.SlackBotToken = ""
		cfg.SlackTeamID = ""
		cfg.SlackTeamName = ""
		cfg.SlackBotUserID = ""
	}); err != nil {
		return err
	}
	a.ensureSlackGateway()
	return nil
}

func openSlackLoginURL(wailsCtx context.Context, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("无法打开浏览器")
	}
	if err := browser.OpenURL(raw); err != nil {
		if wailsCtx != nil {
			runtime.BrowserOpenURL(wailsCtx, raw)
			return nil
		}
		return fmt.Errorf("无法打开浏览器")
	}
	return nil
}

func (a *App) deliverSlackScheduledTarget(_ context.Context, target scheduler.DeliveryTarget, text string) (string, error) {
	id := strings.TrimSpace(target.UserID)
	if id == "" {
		id = strings.TrimSpace(target.GroupID)
	}
	if store := a.deliveryStateStore(); store != nil {
		id = store.ResolveSelfPeer(scheduler.DeliveryChannelSlack, id)
	}
	a.ensureSlackGateway()
	if a.slackGateway == nil {
		return "", fmt.Errorf("slack gateway unavailable")
	}
	return a.slackGateway.SendProactiveText(id, text)
}

func (a *App) listSlackDeliveryTargets(_ context.Context, query string) ([]scheduler.TargetRef, error) {
	refs := []scheduler.TargetRef{{
		Kind:    scheduler.DeliveryKindUser,
		ID:      "self",
		Name:    "Slack 最近会话 (self)",
		Channel: scheduler.DeliveryChannelSlack,
	}}
	if a != nil && a.slackGateway != nil {
		if id := strings.TrimSpace(a.slackGateway.lastChannelID()); id != "" {
			refs = append(refs, scheduler.TargetRef{
				Kind:    scheduler.DeliveryKindUser,
				ID:      id,
				Name:    "Slack " + id,
				Channel: scheduler.DeliveryChannelSlack,
			})
		}
	}
	return scheduler.FilterTargetRefs(refs, query), nil
}
