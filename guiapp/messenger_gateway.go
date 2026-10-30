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
	"github.com/RapidAI/CodeClaw/corelib/messenger"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/textutil"
)

// messengerGatewayManager runs the desktop Facebook Messenger webhook.
// Meta calls the local listener (usually through a public HTTPS tunnel).
// Replies go out through the Graph Send API. The model still uses im_message.
type messengerGatewayManager struct {
	app          *App
	mu           sync.Mutex
	syncMu       sync.Mutex
	gateway      *messenger.Gateway
	pending      *messenger.Gateway
	status       gatewayConnectionStatus
	listenKey    string
	lastErr      string
	lastPSID     string
	defaultPSID  string
	localHandler *IMMessageHandler
}

func newMessengerGatewayManager(app *App) *messengerGatewayManager {
	return &messengerGatewayManager{
		app:    app,
		status: gatewayConnectionStatusDisconnected,
	}
}

func (m *messengerGatewayManager) currentLocalHandler() *IMMessageHandler {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.localHandler
}

func messengerAccountFromConfig(cfg corelib.AppConfig) messenger.Account {
	return messenger.Account{
		Enabled:         cfg.MessengerEnabled,
		PageID:          cfg.MessengerPageID,
		PageAccessToken: cfg.MessengerPageAccessToken,
		VerifyToken:     cfg.MessengerVerifyToken,
		AppSecret:       cfg.MessengerAppSecret,
		GraphAPIVersion: cfg.MessengerGraphAPIVersion,
		WebhookPath:     cfg.MessengerWebhookPath,
		ListenHost:      cfg.MessengerListenHost,
		ListenPort:      cfg.MessengerListenPort,
		AllowFrom:       cfg.MessengerAllowFrom,
		DMPolicy:        cfg.MessengerDMPolicy,
		DefaultPSID:     cfg.MessengerDefaultPSID,
		PublicBaseURL:   cfg.MessengerPublicBaseURL,
	}.Normalized()
}

func (m *messengerGatewayManager) SyncFromConfig() {
	if m == nil || m.app == nil {
		return
	}
	m.syncMu.Lock()
	defer m.syncMu.Unlock()

	for {
		cfg, err := m.app.LoadConfig()
		if err != nil {
			return
		}
		account := messengerAccountFromConfig(cfg)
		m.mu.Lock()
		if !account.CanRun() {
			gw := m.gateway
			m.gateway = nil
			m.pending = nil
			m.listenKey = ""
			m.defaultPSID = account.DefaultPSID
			prev := m.status
			m.status = gatewayConnectionStatusDisconnected
			hint := ""
			if account.Enabled {
				hint = "Facebook Messenger needs a Page ID, page access token, app secret, and verify token"
			}
			errChanged := m.lastErr != hint
			m.lastErr = hint
			m.mu.Unlock()
			if prev != gatewayConnectionStatusDisconnected || errChanged || gw != nil {
				m.emitStatusEvent()
			}
			if gw != nil {
				releaseIMSyncLock(&m.syncMu, func() { _ = gw.Stop() })
				m.mu.Lock()
				replaced := m.gateway != nil
				m.mu.Unlock()
				if replaced {
					return
				}
			}
			return
		}
		if m.gateway != nil && m.gateway.Running() && m.listenKey == account.ListenKey() {
			gw := m.gateway
			m.defaultPSID = account.DefaultPSID
			// The socket is still up. That does not mean the last Graph send
			// succeeded. Any later IM sync takes this path, so clearing here
			// hides a send failure until the next reply.
			m.mu.Unlock()
			gw.UpdateAccount(account)
			return
		}
		old := m.gateway
		m.gateway = nil
		m.mu.Unlock()
		if old != nil {
			releaseIMSyncLock(&m.syncMu, func() { _ = old.Stop() })
			continue
		}

		gw := messenger.NewGateway(account, m.onIncomingMessage)
		gw.SetStatusCallback(func(status string) { m.onStatusChange(gw, status) })
		m.mu.Lock()
		m.pending = gw
		m.mu.Unlock()
		if err := gw.Start(context.Background()); err != nil {
			log.Printf("[messenger-mgr] start failed: %v", err)
			m.mu.Lock()
			abandoned := m.pending != gw
			if !abandoned {
				m.pending = nil
				m.status = gatewayConnectionStatusError
				m.listenKey = ""
				m.lastErr = err.Error()
			}
			m.mu.Unlock()
			if !abandoned {
				m.emitStatusEvent()
			}
			return
		}
		m.mu.Lock()
		if m.pending != gw {
			m.mu.Unlock()
			_ = gw.Stop()
			return
		}
		m.gateway = gw
		m.pending = nil
		m.listenKey = account.ListenKey()
		m.defaultPSID = account.DefaultPSID
		m.lastErr = ""
		if m.status != gatewayConnectionStatusConnected {
			m.status = gatewayConnectionStatusConnected
		}
		m.mu.Unlock()
		m.emitStatusEvent()
		return
	}
}

func (m *messengerGatewayManager) Stop() {
	if m == nil {
		return
	}
	m.syncMu.Lock()
	defer m.syncMu.Unlock()

	m.mu.Lock()
	gw := m.gateway
	pending := m.pending
	m.gateway = nil
	m.pending = nil
	m.listenKey = ""
	m.status = gatewayConnectionStatusDisconnected
	m.localHandler = nil
	m.mu.Unlock()
	m.emitStatusEvent()
	if gw != nil {
		releaseIMSyncLock(&m.syncMu, func() { _ = gw.Stop() })
	}
	if pending != nil && pending != gw {
		releaseIMSyncLock(&m.syncMu, func() { _ = pending.Stop() })
	}
}

func (m *messengerGatewayManager) connectionError() string {
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

func (m *messengerGatewayManager) Status() string {
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

func (m *messengerGatewayManager) onStatusChange(gw *messenger.Gateway, status string) {
	next := normalizeGatewayConnectionStatus(status)
	if next == gatewayConnectionStatusUnknown {
		next = gatewayConnectionStatusDisconnected
	}
	m.mu.Lock()
	if m.gateway != gw && m.pending != gw {
		m.mu.Unlock()
		return
	}
	prev := m.status
	m.status = next
	if next == gatewayConnectionStatusError && gw != nil {
		if errText := gw.LastError(); errText != "" {
			m.lastErr = errText
		}
	}
	m.mu.Unlock()
	if prev != next {
		m.emitStatusEvent()
	}
}

func (m *messengerGatewayManager) emitStatusEvent() {
	if m == nil || m.app == nil {
		return
	}
	m.app.emitEvent("messenger-status-changed", m.Status())
}

func (m *messengerGatewayManager) notePSID(psid string) {
	psid = messenger.NormalizePSID(psid)
	if m == nil || psid == "" {
		return
	}
	m.mu.Lock()
	m.lastPSID = psid
	m.mu.Unlock()
}

func (m *messengerGatewayManager) lastPSIDValue() string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastPSID
}

// SendProactiveText posts to psid. An empty id or "self" uses the latest sender,
// then the configured default PSID.
func (m *messengerGatewayManager) SendProactiveText(psid, text string) (string, error) {
	psid = messenger.NormalizePSID(psid)
	if scheduler.IsSelfPeerID(psid) {
		psid = ""
	}
	if psid == "" {
		psid = m.lastPSIDValue()
	}
	if psid == "" {
		m.mu.Lock()
		psid = m.defaultPSID
		m.mu.Unlock()
	}
	if psid == "" {
		return "", fmt.Errorf("facebook messenger target is unavailable")
	}
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		return "", fmt.Errorf("facebook messenger gateway unavailable")
	}
	_, err := gw.Send(context.Background(), messenger.SendRequest{To: psid, Text: text})
	m.noteSendResult(err)
	if err != nil {
		return "", err
	}
	m.notePSID(psid)
	return psid, nil
}

func (m *messengerGatewayManager) noteSendResult(err error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	prev := m.lastErr
	// A dead listener keeps its bind error. Send failures are visible only while connected.
	if m.status == gatewayConnectionStatusConnected {
		if err != nil {
			m.lastErr = err.Error()
		} else {
			m.lastErr = ""
		}
	}
	changed := m.lastErr != prev
	m.mu.Unlock()
	if changed {
		m.emitStatusEvent()
	}
}

func (m *messengerGatewayManager) onIncomingMessage(msg messenger.IncomingMessage) {
	m.notePSID(msg.PSID)
	m.handleLocalMessage(msg)
}

func (m *messengerGatewayManager) handleLocalMessage(msg messenger.IncomingMessage) {
	replyTo := func(text string, quote bool) {
		m.mu.Lock()
		gw := m.gateway
		m.mu.Unlock()
		if gw == nil || strings.TrimSpace(text) == "" {
			return
		}
		req := messenger.SendRequest{To: msg.PSID, Text: text}
		if quote {
			req.ReplyToID = msg.MessageID
		}
		_, err := gw.Send(context.Background(), req)
		if err != nil {
			log.Printf("[messenger-mgr] reply: %v", err)
		}
		m.noteSendResult(err)
	}
	if resp, handled := m.app.TryHandlePassthroughSlashCommandWithSource(msg.Text, "messenger:"+msg.PSID); handled {
		reply := resp.Text
		if reply == "" {
			reply = resp.Error
		}
		if reply == "" {
			reply = "(no output)"
		}
		replyTo(reply, true)
		return
	}
	if !m.app.isMaclawLLMConfigured() {
		replyTo(i18n.T(i18n.MsgLLMNotConfigured, appUILang(m.app)), true)
		return
	}
	handler := m.ensureLocalHandler()
	// Slash commands already matched msg.Text. The quote is only for the agent.
	text := messenger.TextWithReplyQuote(msg.Text, msg.ReplyQuote)
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
		replyTo(i18n.T(i18n.MsgProgressPrefix, appUILang(m.app))+textutil.StripMarkdown(progressText), false)
	}
	resp := handler.HandleIMMessageWithProgress(IMUserMessage{
		UserID:   msg.PSID,
		Platform: "messenger_local",
		Text:     text,
		Lang:     appUILang(m.app),
	}, onProgress)
	if resp == nil || resp.Deferred {
		return
	}
	if resp.Text != "" {
		replyTo(textutil.StripMarkdown(resp.Text), true)
	} else if resp.Error != "" {
		replyTo(textutil.StripMarkdown(resp.Error), true)
	}
}

func (m *messengerGatewayManager) ensureLocalHandler() *IMMessageHandler {
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

func (a *App) ensureMessengerGateway() {
	cfg, err := a.LoadConfig()
	if err != nil {
		return
	}
	account := messengerAccountFromConfig(cfg)
	a.imGatewaySyncMu.Lock()
	if a.messengerGateway == nil && !account.Enabled {
		a.imGatewaySyncMu.Unlock()
		return
	}
	if a.messengerGateway == nil {
		a.messengerGateway = newMessengerGatewayManager(a)
	}
	m := a.messengerGateway
	a.imGatewaySyncMu.Unlock()
	m.SyncFromConfig()
}

// GetMessengerStatus reports the desktop Facebook Messenger webhook.
func (a *App) GetMessengerStatus() string {
	if a == nil || a.messengerGateway == nil {
		return gatewayConnectionStatusDisconnected.String()
	}
	return a.messengerGateway.Status()
}

// GetMessengerConnectionError is the latest webhook or Graph failure.
func (a *App) GetMessengerConnectionError() string {
	if a == nil || a.messengerGateway == nil {
		return ""
	}
	return a.messengerGateway.connectionError()
}

// RestartMessenger rebinds the local webhook from the saved configuration.
func (a *App) RestartMessenger() string {
	if a == nil {
		return gatewayConnectionStatusDisconnected.String()
	}
	if a.messengerGateway != nil {
		a.messengerGateway.Stop()
	}
	a.ensureMessengerGateway()
	return a.GetMessengerStatus()
}

func (a *App) deliverMessengerScheduledTarget(_ context.Context, target scheduler.DeliveryTarget, text string) (string, error) {
	id := strings.TrimSpace(target.UserID)
	if id == "" {
		id = strings.TrimSpace(target.GroupID)
	}
	if store := a.deliveryStateStore(); store != nil {
		id = store.ResolveSelfPeer(scheduler.DeliveryChannelMessenger, id)
	}
	a.ensureMessengerGateway()
	if a.messengerGateway == nil {
		return "", fmt.Errorf("facebook messenger gateway unavailable")
	}
	return a.messengerGateway.SendProactiveText(id, text)
}

func (a *App) listMessengerDeliveryTargets(_ context.Context, query string) ([]scheduler.TargetRef, error) {
	refs := []scheduler.TargetRef{{
		Kind:    scheduler.DeliveryKindUser,
		ID:      "self",
		Name:    "Messenger 最近会话 (self)",
		Channel: scheduler.DeliveryChannelMessenger,
	}}
	seen := map[string]struct{}{"self": {}}
	add := func(id, name string) {
		id = messenger.NormalizePSID(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		refs = append(refs, scheduler.TargetRef{
			Kind:    scheduler.DeliveryKindUser,
			ID:      id,
			Name:    name,
			Channel: scheduler.DeliveryChannelMessenger,
		})
	}
	if a != nil && a.messengerGateway != nil {
		add(a.messengerGateway.lastPSIDValue(), "Messenger "+a.messengerGateway.lastPSIDValue())
		a.messengerGateway.mu.Lock()
		defaultPSID := a.messengerGateway.defaultPSID
		a.messengerGateway.mu.Unlock()
		add(defaultPSID, "Messenger 默认 PSID")
	}
	return scheduler.FilterTargetRefs(refs, query), nil
}

// applyMessengerConfigField writes one whitelisted Messenger setting.
// changed is true when the live webhook should reload.
func applyMessengerConfigField(cfg *corelib.AppConfig, key string, value interface{}) error {
	if cfg == nil {
		return fmt.Errorf("config unavailable")
	}
	switch key {
	case "messenger_enabled":
		v, ok := value.(bool)
		if !ok {
			return fmt.Errorf("config field %q must be boolean", key)
		}
		cfg.MessengerEnabled = v
	case "messenger_page_id":
		v, err := messengerString(key, value)
		if err != nil {
			return err
		}
		cfg.MessengerPageID = strings.TrimSpace(v)
	case "messenger_page_access_token":
		v, err := messengerString(key, value)
		if err != nil {
			return err
		}
		cfg.MessengerPageAccessToken = strings.TrimSpace(v)
	case "messenger_app_secret":
		v, err := messengerString(key, value)
		if err != nil {
			return err
		}
		cfg.MessengerAppSecret = strings.TrimSpace(v)
	case "messenger_verify_token":
		v, err := messengerString(key, value)
		if err != nil {
			return err
		}
		cfg.MessengerVerifyToken = strings.TrimSpace(v)
	case "messenger_graph_api_version":
		v, err := messengerString(key, value)
		if err != nil {
			return err
		}
		version, err := messenger.NormalizeGraphAPIVersion(v)
		if err != nil {
			return err
		}
		cfg.MessengerGraphAPIVersion = version
	case "messenger_webhook_path":
		v, err := messengerString(key, value)
		if err != nil {
			return err
		}
		path, err := messenger.NormalizeWebhookPath(v)
		if err != nil {
			return err
		}
		if strings.TrimSpace(v) == "" {
			path = ""
		}
		cfg.MessengerWebhookPath = path
	case "messenger_listen_host":
		v, err := messengerString(key, value)
		if err != nil {
			return err
		}
		host, err := messenger.NormalizeListenHost(v)
		if err != nil {
			return err
		}
		cfg.MessengerListenHost = host
	case "messenger_listen_port":
		if value == nil || value == "" {
			cfg.MessengerListenPort = 0
			return nil
		}
		switch v := value.(type) {
		case int:
			if v != 0 && (v < 1 || v > 65535) {
				return fmt.Errorf("messenger_listen_port must be between 1 and 65535")
			}
			cfg.MessengerListenPort = v
		case int64:
			if v != 0 && (v < 1 || v > 65535) {
				return fmt.Errorf("messenger_listen_port must be between 1 and 65535")
			}
			cfg.MessengerListenPort = int(v)
		case float64:
			port := int(v)
			if port != 0 && (port < 1 || port > 65535) {
				return fmt.Errorf("messenger_listen_port must be between 1 and 65535")
			}
			cfg.MessengerListenPort = port
		default:
			return fmt.Errorf("config field %q must be number", key)
		}
	case "messenger_allow_from":
		ids, err := parseMessengerAllowFrom(value)
		if err != nil {
			return err
		}
		cfg.MessengerAllowFrom = ids
	case "messenger_dm_policy":
		v, err := messengerString(key, value)
		if err != nil {
			return err
		}
		policy, err := messenger.NormalizeDMPolicy(v)
		if err != nil {
			return err
		}
		if strings.TrimSpace(v) == "" {
			policy = ""
		}
		cfg.MessengerDMPolicy = policy
	case "messenger_default_psid":
		v, err := messengerString(key, value)
		if err != nil {
			return err
		}
		cfg.MessengerDefaultPSID = messenger.NormalizePSID(v)
	case "messenger_public_base_url":
		v, err := messengerString(key, value)
		if err != nil {
			return err
		}
		base, err := messenger.NormalizePublicBaseURL(v)
		if err != nil {
			return err
		}
		cfg.MessengerPublicBaseURL = base
	default:
		return fmt.Errorf("config field %q is not a messenger setting", key)
	}
	return nil
}

func messengerString(key string, value interface{}) (string, error) {
	v, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("config field %q must be string", key)
	}
	return v, nil
}

func parseMessengerAllowFrom(value interface{}) ([]string, error) {
	switch raw := value.(type) {
	case nil:
		return nil, nil
	case string:
		return messenger.NormalizeAllowFrom(strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
		})), nil
	case []string:
		return messenger.NormalizeAllowFrom(raw), nil
	case []any:
		ids := make([]string, 0, len(raw))
		for _, item := range raw {
			id, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("messenger_allow_from must be a string array")
			}
			ids = append(ids, id)
		}
		return messenger.NormalizeAllowFrom(ids), nil
	default:
		return nil, fmt.Errorf("messenger_allow_from must be a string array")
	}
}
