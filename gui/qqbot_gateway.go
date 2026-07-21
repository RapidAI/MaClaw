package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/improactive"
	"github.com/RapidAI/CodeClaw/corelib/qqbot"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/textutil"
)

// qqBotGatewayManager manages the client-side QQ Bot WebSocket gateway.
// Supports two modes:
//   - Local / 单机 mode (default): routes messages directly to the
//     local MaClaw LLM agent loop, bypassing Hub entirely.
//   - Hub / 多机 mode (QQBotLocalMode=false): forwards messages to Hub
//     via im.gateway_message, receives replies via im.gateway_reply.
type qqBotGatewayManager struct {
	app        *App
	mu         sync.Mutex
	gateway    *qqbot.Gateway
	status     gatewayConnectionStatus
	lastAppID  string
	lastSecret string
	// lastOpenID is the most recent C2C peer for proactive scheduled delivery.
	lastOpenID string

	// localHandler is a fully-wired IMMessageHandler for local mode.
	// Created lazily on first local-mode message.
	localHandler *IMMessageHandler
}

func newQQBotGatewayManager(app *App) *qqBotGatewayManager {
	peers := improactive.NewStore("").LoadOrEmpty()
	return &qqBotGatewayManager{
		app:        app,
		status:     gatewayConnectionStatusDisconnected,
		lastOpenID: strings.TrimSpace(peers.QQLastOpenID),
	}
}

func (m *qqBotGatewayManager) currentLocalHandler() *IMMessageHandler {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.localHandler
}

// SyncFromConfig reads the current AppConfig and starts or stops the gateway.
func (m *qqBotGatewayManager) SyncFromConfig() {
	cfg, err := m.app.LoadConfig()
	if err != nil {
		return
	}

	m.mu.Lock()

	if !cfg.QQBotEnabled || cfg.QQBotAppID == "" || cfg.QQBotAppSecret == "" {
		// Should be stopped
		gw := m.gateway
		if gw != nil {
			m.gateway = nil
			m.status = gatewayConnectionStatusDisconnected
			m.mu.Unlock()
			_ = gw.Stop() // Stop outside lock to avoid deadlock with onStatusChange
		} else {
			m.mu.Unlock()
		}
		// Always notify Hub to release gateway claim, even if local gateway
		// was already nil — Hub may still hold a stale claim from a previous run.
		if hubClient := m.app.hubClient(); hubClient != nil && hubClient.IsConnected() {
			_ = hubClient.SendIMGatewayUnclaim(imGatewayPlatformQQBotRemote)
			log.Printf("[qqbot-mgr] sent gateway unclaim to hub")
		}
		if gw != nil {
			m.emitStatusEvent()
		}
		return
	}

	// Should be running — check if credentials actually changed.
	// Owner openid can change without restarting the WS gateway (resolved via LoadConfig).
	if m.gateway != nil && m.lastAppID == cfg.QQBotAppID && m.lastSecret == cfg.QQBotAppSecret {
		m.mu.Unlock()
		return
	}

	// Restart with new config
	oldGw := m.gateway
	m.gateway = nil
	m.mu.Unlock()

	// Stop old gateway outside lock to avoid deadlock
	if oldGw != nil {
		_ = oldGw.Stop()
	}

	newCfg := qqbot.Config{
		AppID:     cfg.QQBotAppID,
		AppSecret: cfg.QQBotAppSecret,
	}
	gw := qqbot.NewGateway(newCfg, m.onIncomingMessage)
	gw.SetStatusCallback(m.onStatusChange)

	m.mu.Lock()
	m.gateway = gw
	m.lastAppID = cfg.QQBotAppID
	m.lastSecret = cfg.QQBotAppSecret
	m.mu.Unlock()

	if err := gw.Start(context.Background()); err != nil {
		log.Printf("[qqbot-mgr] start failed: %v", err)
		m.mu.Lock()
		m.gateway = nil
		m.lastAppID = ""
		m.lastSecret = ""
		m.status = gatewayConnectionStatusError
		m.mu.Unlock()
		m.emitStatusEvent()
		return
	}
}

// Stop shuts down the gateway.
func (m *qqBotGatewayManager) Stop() {
	m.mu.Lock()
	gw := m.gateway
	m.gateway = nil
	m.status = gatewayConnectionStatusDisconnected
	m.lastAppID = ""
	m.lastSecret = ""
	lh := m.localHandler
	m.localHandler = nil
	m.mu.Unlock()
	_ = lh // shared App conversation memory remains alive
	if gw != nil {
		_ = gw.Stop()
	}
	m.emitStatusEvent()
}

// Status returns the current connection status.
func (m *qqBotGatewayManager) Status() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status.String()
}

// onStatusChange is called by the gateway when connection status changes.
func (m *qqBotGatewayManager) onStatusChange(status string) {
	m.mu.Lock()
	normalized := normalizeGatewayConnectionStatus(status)
	m.status = normalized
	if normalized == gatewayConnectionStatusError {
		m.gateway = nil
		m.lastAppID = ""
		m.lastSecret = ""
	}
	m.mu.Unlock()
	m.emitStatusEvent()

	// When QQ gateway connects, claim the gateway lock on Hub (only in hub mode).
	if normalized == gatewayConnectionStatusConnected {
		if cfg, err := m.app.LoadConfig(); err == nil && cfg.IsQQBotLocalMode() {
			return
		}
		hubClient := m.app.hubClient()
		if hubClient != nil && hubClient.IsConnected() {
			hubClient.SendIMGatewayClaim(imGatewayPlatformQQBotRemote)
		}
	}
}

func (m *qqBotGatewayManager) emitStatusEvent() {
	m.app.emitEvent("qqbot-status-changed", m.Status())
}

// resetLocalHandler invalidates the cached local handler.
func (m *qqBotGatewayManager) resetLocalHandler() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.localHandler = nil
}

// ensureLocalHandler lazily creates a fully-wired IMMessageHandler for local mode.
func (m *qqBotGatewayManager) ensureLocalHandler() *IMMessageHandler {
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
	log.Printf("[qqbot-mgr] local IMMessageHandler created")

	// Wire interrupt handler to gateway for cancel/merge/status during active loops.
	if m.gateway != nil && h.interruptHandler != nil {
		m.gateway.SetInterruptHandler(h.interruptHandler)
	}

	return h
}

func (m *qqBotGatewayManager) noteLastOpenID(openID string) {
	openID = strings.TrimSpace(openID)
	if m == nil || openID == "" {
		return
	}
	m.mu.Lock()
	prev := m.lastOpenID
	m.lastOpenID = openID
	m.mu.Unlock()
	if prev == openID {
		return
	}
	if err := improactive.NewStore("").Patch(func(p *improactive.Peers) {
		p.QQLastOpenID = openID
	}); err != nil {
		log.Printf("[qqbot-mgr] persist last openid: %v", err)
	}
}

// ownerOpenIDFromConfig returns the fixed owner openid from app settings (if any).
func (m *qqBotGatewayManager) ownerOpenIDFromConfig() string {
	if m == nil || m.app == nil {
		return ""
	}
	cfg, err := m.app.LoadConfig()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.QQBotOwnerOpenID)
}

// ResolveProactiveOpenID picks the C2C peer for owner push:
// concrete openid → config qqbot_owner_openid → last private chat openid.
func (m *qqBotGatewayManager) ResolveProactiveOpenID(openID string) string {
	if m == nil {
		return ""
	}
	openID = strings.TrimSpace(openID)
	if openID != "" && !scheduler.IsSelfPeerID(openID) {
		return openID
	}
	if cfgOID := m.ownerOpenIDFromConfig(); cfgOID != "" {
		return cfgOID
	}
	m.mu.Lock()
	last := strings.TrimSpace(m.lastOpenID)
	m.mu.Unlock()
	return last
}

// HasProactiveSession reports whether a QQ C2C peer is known for self-notify
// (configured owner openid or remembered last private chat).
func (m *qqBotGatewayManager) HasProactiveSession() bool {
	return m.ResolveProactiveOpenID("self") != ""
}

// SendProactiveText sends text to openID, or to the owner peer when empty/self.
// Resolution order for self: config qqbot_owner_openid → last private openid.
// On success returns the concrete openid used.
func (m *qqBotGatewayManager) SendProactiveText(openID, text string) (usedOpenID string, err error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("empty proactive text")
	}
	if m == nil {
		return "", fmt.Errorf("qqbot gateway is nil")
	}
	openID = m.ResolveProactiveOpenID(openID)
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		return "", fmt.Errorf("qqbot gateway not running")
	}
	if openID == "" {
		return "", fmt.Errorf("no active qq session (先用 QQ 私聊机器人一次，或在设置中填写 owner openid)")
	}
	if err := gw.SendText(context.Background(), qqbot.OutgoingText{OpenID: openID, Text: text}); err != nil {
		return "", err
	}
	m.noteLastOpenID(openID)
	return openID, nil
}

// onIncomingMessage is called when a C2C message arrives from QQ.
// Routes to local handler or Hub depending on mode.
func (m *qqBotGatewayManager) onIncomingMessage(msg qqbot.IncomingMessage) {
	m.noteLastOpenID(msg.OpenID)
	if isPassthroughSlashText(msg.Text) {
		log.Printf("[qqbot-mgr] routing passthrough command locally: user=%s", msg.OpenID)
		m.handleLocalMessage(msg)
		return
	}
	cfg, err := m.app.LoadConfig()
	if err != nil {
		log.Printf("[qqbot-mgr] LoadConfig error: %v", err)
		return
	}

	isLocal := cfg.IsQQBotLocalMode()
	hubClient := m.app.hubClient()
	hubConnected := hubClient != nil && hubClient.IsConnected()
	log.Printf("[qqbot-mgr] onIncomingMessage: user=%s local_mode=%v hub_connected=%v", msg.OpenID, isLocal, hubConnected)

	if isLocal {
		m.handleLocalMessage(msg)
		return
	}

	// Hub mode — try forwarding; fall back to local if Hub unavailable.
	if !hubConnected {
		log.Printf("[qqbot-mgr] Hub mode but Hub unavailable, falling back to local: user=%s", msg.OpenID)
		m.notifyHubUnavailable(msg)
		m.handleLocalMessage(msg)
		return
	}

	m.forwardToHub(msg)
}

func (m *qqBotGatewayManager) notifyHubUnavailable(msg qqbot.IncomingMessage) {
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		return
	}
	_ = gw.SendText(context.Background(), qqbot.OutgoingText{
		OpenID: msg.OpenID,
		Text:   i18n.T(i18n.MsgHubUnavailable, "zh"),
	})
}

func (m *qqBotGatewayManager) forwardToHub(msg qqbot.IncomingMessage) {
	hubClient := m.app.hubClient()
	if hubClient == nil || !hubClient.IsConnected() {
		log.Printf("[qqbot-mgr] forwardToHub FAILED: hub unavailable user=%s", msg.OpenID)
		m.notifyHubUnavailable(msg)
		m.handleLocalMessage(msg)
		return
	}

	msgType := "text"
	if msg.MediaType != "" && len(msg.MediaData) > 0 {
		msgType = msg.MediaType
	}

	payload := map[string]any{
		"platform_uid": msg.OpenID,
		"text":         msg.Text,
		"message_type": msgType,
	}

	if att := buildMediaAttachment(msg.MediaType, msg.MediaData, msg.MediaName, msg.MimeType); att != nil {
		payload["attachments"] = []map[string]any{att}
	}

	if err := hubClient.SendIMGatewayMessage(imGatewayPlatformQQBotRemote, payload); err != nil {
		log.Printf("[qqbot-mgr] forwardToHub SendIMGatewayMessage error: %v", err)
	}
}

func (m *qqBotGatewayManager) handleLocalMessage(msg qqbot.IncomingMessage) {
	if resp, handled := m.app.TryHandlePassthroughSlashCommandWithSource(msg.Text, "qqbot:"+msg.OpenID); handled {
		m.mu.Lock()
		gw := m.gateway
		m.mu.Unlock()
		if gw != nil {
			reply := resp.Text
			if reply == "" {
				reply = resp.Error
			}
			if reply == "" {
				reply = "(no output)"
			}
			_ = gw.SendText(context.Background(), qqbot.OutgoingText{
				OpenID: msg.OpenID,
				Text:   reply,
			})
		}
		return
	}
	if !m.app.isMaclawLLMConfigured() {
		m.mu.Lock()
		gw := m.gateway
		m.mu.Unlock()
		if gw != nil {
			_ = gw.SendText(context.Background(), qqbot.OutgoingText{
				OpenID: msg.OpenID,
				Text:   i18n.T(i18n.MsgLLMNotConfigured, "zh"),
			})
		}
		return
	}

	handler := m.ensureLocalHandler()

	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		return
	}

	text := msg.Text

	// Pass images as multimodal attachments so the LLM can "see" them.
	var attachments []MessageAttachment
	if msg.MediaType != "" && len(msg.MediaData) > 0 {
		if normalizeIMMediaKind(msg.MediaType).IsImage() {
			attachments = append(attachments, buildLocalImageAttachment(msg.MediaData, msg.MediaName, msg.MimeType))
		} else {
			mediaPath, err := saveQQMediaToTemp(msg)
			if err != nil {
				log.Printf("[qqbot-mgr] save media error: %v", err)
			} else {
				prefix := "[收到" + mediaLabel(msg.MediaType) + ": " + mediaPath + "]\n"
				text = prefix + text
			}
		}
	}

	if text == "" && len(attachments) == 0 {
		return
	}

	progressFilter := newIMProgressVisibilityFilter(m.app)
	var lastProgress time.Time
	var lastProgressText string
	onProgress := func(progressText string) {
		if !progressFilter.ShouldSendProgress(progressText) {
			return
		}
		now := time.Now()
		if now.Sub(lastProgress) < 5*time.Second {
			return
		}
		stripped := textutil.StripMarkdown(progressText)
		if stripped == lastProgressText {
			return
		}
		lastProgress = now
		lastProgressText = stripped
		_ = gw.SendText(context.Background(), qqbot.OutgoingText{
			OpenID: msg.OpenID,
			Text:   i18n.T(i18n.MsgProgressPrefix, appUILang(m.app)) + textutil.StripMarkdown(progressText),
		})
	}

	resp := handler.HandleIMMessageWithProgress(IMUserMessage{
		UserID:      msg.OpenID,
		Platform:    "qqbot_local",
		Text:        text,
		Lang:        appUILang(m.app),
		Attachments: attachments,
	}, onProgress)

	if resp == nil || resp.Deferred {
		return
	}

	m.sendAgentResponse(gw, msg.OpenID, resp)
}

func (m *qqBotGatewayManager) sendAgentResponse(gw *qqbot.Gateway, openID string, resp *IMAgentResponse) {
	ctx := context.Background()

	if resp.Text != "" {
		text := textutil.StripMarkdown(resp.Text)
		if err := gw.SendText(ctx, qqbot.OutgoingText{
			OpenID: openID,
			Text:   text,
		}); err != nil {
			log.Printf("[qqbot-mgr] local SendText error (to=%s): %v", openID, err)
		}
	}

	if resp.Error != "" && resp.Text == "" {
		_ = gw.SendText(ctx, qqbot.OutgoingText{
			OpenID: openID,
			Text:   "" + textutil.StripMarkdown(resp.Error),
		})
	}

	if resp.ImageKey != "" {
		_ = gw.SendMedia(ctx, qqbot.OutgoingMedia{
			OpenID:   openID,
			FileType: 1, // image
			FileData: resp.ImageKey,
			MimeType: "image/png",
		})
	}

	if resp.FileData != "" {
		mimeType := resp.FileMimeType
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		_ = gw.SendMedia(ctx, qqbot.OutgoingMedia{
			OpenID:   openID,
			FileType: 4, // file
			FileData: resp.FileData,
			FileName: resp.FileName,
			MimeType: mimeType,
		})
	}

	if resp.VoiceData != "" {
		if err := gw.SendMedia(ctx, qqbot.OutgoingMedia{
			OpenID:   openID,
			FileType: 3, // voice
			FileData: resp.VoiceData,
			FileName: resp.VoiceFileName,
			MimeType: resp.VoiceMimeType,
		}); err != nil {
			log.Printf("[qqbot-mgr] SendMedia voice failed (to=%s): %v", openID, err)
		}
	}

	m.sendLocalFiles(gw, openID, resp)
}

func (m *qqBotGatewayManager) sendLocalFiles(gw *qqbot.Gateway, openID string, resp *IMAgentResponse) {
	paths := resp.LocalFilePaths
	if resp.LocalFilePath != "" && !containsString(paths, resp.LocalFilePath) {
		paths = append([]string{resp.LocalFilePath}, paths...)
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			log.Printf("[qqbot-mgr] read local file %s error: %v", p, err)
			continue
		}
		name := filepath.Base(p)
		mediaType := mediaTypeFromFileName(name)
		fileType := 4
		switch normalizeIMMediaKind(mediaType) {
		case imMediaImage:
			fileType = 1
		case imMediaVideo:
			fileType = 2
		case imMediaVoice:
			fileType = 3
		case imMediaAudio:
			mediaType = imMediaFile.String()
		}
		if err := gw.SendMedia(context.Background(), qqbot.OutgoingMedia{
			OpenID:   openID,
			FileType: fileType,
			FileData: base64.StdEncoding.EncodeToString(data),
			FileName: name,
			MimeType: guessMimeFromMedia(mediaType, name),
		}); err != nil {
			log.Printf("[qqbot-mgr] SendMedia local file failed (to=%s file=%s): %v", openID, p, err)
		}
	}
}

// SendQQBotReply sends a text reply to a QQ user. Called when Hub sends
// im.gateway_reply back to the client.
func (m *qqBotGatewayManager) SendQQBotReply(openID, text string) error {
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		return fmt.Errorf("qqbot gateway not running")
	}
	return gw.SendText(context.Background(), qqbot.OutgoingText{
		OpenID: openID,
		Text:   text,
	})
}

// SendQQBotMedia sends a media message to a QQ user.
func (m *qqBotGatewayManager) SendQQBotMedia(msg qqbot.OutgoingMedia) error {
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		return fmt.Errorf("qqbot gateway not running")
	}
	return gw.SendMedia(context.Background(), msg)
}

// ---------------------------------------------------------------------------
// App integration — Wails bindings and lifecycle
// ---------------------------------------------------------------------------

// ensureQQBotGateway lazily creates the gateway manager and syncs from config.
// If QQ Bot is not enabled in config, skips entirely to avoid unnecessary work.
func (a *App) ensureQQBotGateway() {
	cfg, err := a.LoadConfig()
	if err != nil {
		return
	}
	if !cfg.QQBotEnabled || cfg.QQBotAppID == "" || cfg.QQBotAppSecret == "" {
		// Not configured — skip creating the manager entirely.
		// If there's an existing manager (user just disabled), let it sync to stop.
		if a.qqBotGateway != nil {
			a.qqBotGateway.SyncFromConfig()
		}
		return
	}
	if a.qqBotGateway == nil {
		a.qqBotGateway = newQQBotGatewayManager(a)
	}
	a.qqBotGateway.SyncFromConfig()
}

// GetQQBotStatus returns the current QQ Bot gateway status (Wails binding).
func (a *App) GetQQBotStatus() string {
	if a.qqBotGateway == nil {
		return "disconnected"
	}
	return a.qqBotGateway.Status()
}

// RestartQQBot restarts the QQ Bot gateway with current config (Wails binding).
func (a *App) RestartQQBot() string {
	a.ensureQQBotGateway()
	if a.qqBotGateway == nil {
		return gatewayConnectionStatusDisconnected.String()
	}
	return a.qqBotGateway.Status()
}

// StopQQBot stops the QQ Bot gateway (Wails binding).
func (a *App) StopQQBot() {
	if a.qqBotGateway != nil {
		a.qqBotGateway.Stop()
	}
}

// GetQQBotLocalMode returns whether QQ Bot local mode is enabled.
func (a *App) GetQQBotLocalMode() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return true
	}
	return cfg.IsQQBotLocalMode()
}

// SetQQBotLocalMode enables or disables QQ Bot local mode.
func (a *App) SetQQBotLocalMode(enabled bool) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	// Switching to hub mode requires prior Hub registration.
	if !enabled && cfg.RemoteMachineID == "" {
		return fmt.Errorf("请先注册到 Hub（设置 Hub 地址并完成注册），再开启多机模式")
	}
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.SetQQBotLocal(enabled)
	}); err != nil {
		return err
	}
	log.Printf("[qqbot-mgr] SetQQBotLocalMode: enabled=%v", enabled)

	if a.qqBotGateway != nil {
		a.qqBotGateway.resetLocalHandler()
	}

	if !enabled {
		hubClient := a.hubClient()
		if hubClient != nil && hubClient.IsConnected() {
			hubClient.SendIMGatewayClaim(imGatewayPlatformQQBotRemote)
			log.Printf("[qqbot-mgr] sent gateway claim after switching to hub mode")
		}
	}
	return nil
}

// saveQQMediaToTemp saves media from a QQ message to a temp file.
func saveQQMediaToTemp(msg qqbot.IncomingMessage) (string, error) {
	return saveMediaToTempDir("qq", "qq_", msg.OpenID, msg.MediaType, msg.MediaData, msg.MediaName)
}
