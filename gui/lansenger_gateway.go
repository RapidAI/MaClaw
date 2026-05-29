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

	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/lansenger"
	"github.com/RapidAI/CodeClaw/corelib/textutil"
)

// lansengerGatewayManager manages the client-side Lansenger gateway.
// Supports two modes:
//   - Local / 单机 mode (default): routes messages directly to the
//     local MaClaw LLM agent loop, bypassing Hub entirely.
//   - Hub / 多机 mode (LansengerLocalMode=false): forwards messages to Hub
//     via im.gateway_message, receives replies via im.gateway_reply.
type lansengerGatewayManager struct {
	app          *App
	mu           sync.Mutex
	gateway      *lansenger.Gateway
	status       gatewayConnectionStatus
	statusSince  time.Time
	lastRestart  time.Time
	lastToken    string
	healthCancel context.CancelFunc

	localHandler *IMMessageHandler
}

func newLansengerGatewayManager(app *App) *lansengerGatewayManager {
	return &lansengerGatewayManager{
		app:         app,
		status:      gatewayConnectionStatusDisconnected,
		statusSince: time.Now(),
	}
}

// SyncFromConfig reads the current AppConfig and starts or stops the gateway.
func (m *lansengerGatewayManager) SyncFromConfig() {
	m.syncFromConfig(false)
}

// Restart forcefully tears down the current gateway and starts a fresh
// connection from the current AppConfig.
func (m *lansengerGatewayManager) Restart() {
	m.syncFromConfig(true)
}

func (m *lansengerGatewayManager) syncFromConfig(forceRestart bool) {
	cfg, err := m.app.LoadConfig()
	if err != nil {
		return
	}

	appID := strings.TrimSpace(cfg.LansengerAppID)
	appSecret := strings.TrimSpace(cfg.LansengerAppSecret)
	gwURL := cfg.LansengerApiGatewayURL()
	wssURL := cfg.LansengerWebSocketGatewayURL()

	m.mu.Lock()
	if !cfg.LansengerEnabled || appID == "" || appSecret == "" {
		gw := m.gateway
		healthCancel := m.healthCancel
		m.gateway = nil
		m.status = gatewayConnectionStatusDisconnected
		m.statusSince = time.Now()
		m.lastToken = ""
		m.healthCancel = nil
		m.mu.Unlock()
		if healthCancel != nil {
			healthCancel()
		}
		if gw != nil {
			_ = gw.Stop()
		}
		if hubClient := m.app.hubClient(); hubClient != nil && hubClient.IsConnected() {
			_ = hubClient.SendIMGatewayUnclaim(imGatewayPlatformLansenger)
		}
		m.emitStatusEvent()
		return
	}

	cacheKey := appID + "|" + appSecret + "|" + gwURL + "|" + wssURL
	if m.gateway != nil && m.lastToken == cacheKey && !forceRestart && m.gateway.IsRunning() && m.status != gatewayConnectionStatusDisconnected && m.status != gatewayConnectionStatusError {
		m.mu.Unlock()
		return
	}

	oldGw := m.gateway
	m.mu.Unlock()
	if oldGw != nil {
		_ = oldGw.Stop()
	}

	gwCfg := lansenger.Config{
		AppID:            appID,
		AppSecret:        appSecret,
		ApiGatewayURL:    gwURL,
		WebSocketBaseURL: wssURL,
	}
	gw := lansenger.NewGateway(gwCfg, m.onIncomingMessage)
	gw.SetStatusCallback(func(status string) {
		m.onGatewayStatusChange(gw, status)
	})

	m.mu.Lock()
	m.gateway = gw
	m.lastToken = cacheKey
	m.mu.Unlock()
	m.ensureHealthMonitor()

	if err := gw.Start(context.Background()); err != nil {
		log.Printf("[lansenger-mgr] start failed: %v", err)
		m.mu.Lock()
		m.gateway = nil
		m.lastToken = ""
		m.status = gatewayConnectionStatusError
		m.statusSince = time.Now()
		m.mu.Unlock()
		m.emitStatusEvent()
		return
	}
}

// Stop shuts down the gateway.
func (m *lansengerGatewayManager) Stop() {
	m.mu.Lock()
	gw := m.gateway
	m.gateway = nil
	m.status = gatewayConnectionStatusDisconnected
	m.statusSince = time.Now()
	m.lastToken = ""
	healthCancel := m.healthCancel
	m.healthCancel = nil
	lh := m.localHandler
	m.localHandler = nil
	m.mu.Unlock()
	if healthCancel != nil {
		healthCancel()
	}
	if lh != nil {
		lh.memory.Stop()
	}
	if gw != nil {
		_ = gw.Stop()
	}
	m.emitStatusEvent()
}

// Status returns the current connection status.
func (m *lansengerGatewayManager) Status() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status.String()
}

func (m *lansengerGatewayManager) ensureHealthMonitor() {
	m.mu.Lock()
	if m.healthCancel != nil {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.healthCancel = cancel
	m.mu.Unlock()

	go m.healthMonitorLoop(ctx)
}

func (m *lansengerGatewayManager) healthMonitorLoop(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.Lock()
			gw := m.gateway
			status := m.status
			statusSince := m.statusSince
			lastRestart := m.lastRestart
			m.mu.Unlock()

			if lansengerRestartInCooldown(lastRestart, time.Now()) {
				continue
			}

			if status == gatewayConnectionStatusError || (gw != nil && !gw.IsRunning()) {
				running := gw != nil && gw.IsRunning()
				log.Printf("[lansenger-mgr] health monitor restarting gateway: status=%s running=%v", status, running)
				m.restartFromHealthMonitor("not_running_or_error")
				continue
			}
			if gw == nil || status == gatewayConnectionStatusDisconnected {
				continue
			}
			if lansengerStatusNeedsWatchdogRestart(status) && time.Since(statusSince) > 5*time.Minute {
				log.Printf("[lansenger-mgr] health monitor restarting stale gateway status: status=%s since=%v", status, statusSince.Format(time.RFC3339))
				m.restartFromHealthMonitor("stale_status_" + status.String())
				continue
			}
		}
	}
}

func lansengerStatusNeedsWatchdogRestart(status gatewayConnectionStatus) bool {
	switch status {
	case gatewayConnectionStatusConnecting, gatewayConnectionStatusReconnecting, gatewayConnectionStatusUnknown:
		return true
	default:
		return false
	}
}

func lansengerRestartInCooldown(lastRestart, now time.Time) bool {
	return !lastRestart.IsZero() && now.Sub(lastRestart) < time.Minute
}

func (m *lansengerGatewayManager) onGatewayStatusChange(gw *lansenger.Gateway, status string) {
	m.mu.Lock()
	if m.gateway != gw {
		m.mu.Unlock()
		log.Printf("[lansenger-mgr] ignoring stale gateway status: %s", status)
		return
	}
	normalized := normalizeGatewayConnectionStatus(status)
	if normalized != m.status {
		m.statusSince = time.Now()
	}
	m.status = normalized
	if normalized == gatewayConnectionStatusError {
		// Clear gateway reference so SyncFromConfig can retry on next call.
		m.gateway = nil
		m.lastToken = ""
	}
	m.mu.Unlock()
	m.emitStatusEvent()

	if normalized == gatewayConnectionStatusConnected {
		if cfg, err := m.app.LoadConfig(); err == nil && cfg.IsLansengerLocalMode() {
			return
		}
		hubClient := m.app.hubClient()
		if hubClient != nil && hubClient.IsConnected() {
			hubClient.SendIMGatewayClaim(imGatewayPlatformLansenger)
		}
	}
}

func (m *lansengerGatewayManager) restartFromHealthMonitor(reason string) {
	m.mu.Lock()
	if lansengerRestartInCooldown(m.lastRestart, time.Now()) {
		m.mu.Unlock()
		return
	}
	m.lastRestart = time.Now()
	m.mu.Unlock()
	m.app.emitEvent("lansenger-auto-restarting", reason)
	m.Restart()
}

func (m *lansengerGatewayManager) emitStatusEvent() {
	m.app.emitEvent("lansenger-status-changed", m.Status())
}

func (m *lansengerGatewayManager) resetLocalHandler() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.localHandler != nil {
		m.localHandler.memory.Stop()
		m.localHandler = nil
	}
}

// ---------------------------------------------------------------------------
// Message routing
// ---------------------------------------------------------------------------

func (m *lansengerGatewayManager) onIncomingMessage(msg lansenger.IncomingMessage) {
	if isPassthroughSlashText(msg.Text) {
		log.Printf("[lansenger-mgr] routing passthrough command locally: user=%s", msg.FromUserID)
		m.handleLocalMessage(msg)
		return
	}
	cfg, err := m.app.LoadConfig()
	if err != nil {
		log.Printf("[lansenger-mgr] LoadConfig error: %v", err)
		return
	}

	isLocal := cfg.IsLansengerLocalMode()
	hubClient := m.app.hubClient()
	hubNil := hubClient == nil
	hubConn := !hubNil && hubClient.IsConnected()

	log.Printf("[lansenger-mgr] incoming: user=%s local=%v hub_nil=%v hub_conn=%v text_len=%d",
		msg.FromUserID, isLocal, hubNil, hubConn, len(msg.Text))

	if isLocal {
		m.handleLocalMessage(msg)
		return
	}

	if hubNil || !hubConn {
		log.Printf("[lansenger-mgr] Hub unavailable, falling back to local")
		m.notifyHubUnavailable(msg)
		m.handleLocalMessage(msg)
		return
	}

	m.forwardToHub(msg)
}

func (m *lansengerGatewayManager) notifyHubUnavailable(msg lansenger.IncomingMessage) {
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		return
	}
	_ = gw.SendText(context.Background(), lansenger.OutgoingText{
		ToUserID: msg.FromUserID,
		Text:     "⚠️ 当前为多机模式，但 Hub 未连接。消息已回退到本地处理。",
	})
}

func (m *lansengerGatewayManager) forwardToHub(msg lansenger.IncomingMessage) {
	hubClient := m.app.hubClient()
	if hubClient == nil || !hubClient.IsConnected() {
		m.notifyHubUnavailable(msg)
		m.handleLocalMessage(msg)
		return
	}

	payload := map[string]any{
		"platform_uid": msg.FromUserID,
		"text":         msg.Text,
		"message_type": "text",
	}

	if err := hubClient.SendIMGatewayMessage(imGatewayPlatformLansenger, payload); err != nil {
		log.Printf("[lansenger-mgr] forwardToHub error: %v, falling back to local", err)
		m.handleLocalMessage(msg)
	}
}

// ---------------------------------------------------------------------------
// Local mode — direct LLM agent loop
// ---------------------------------------------------------------------------

func (m *lansengerGatewayManager) ensureLocalHandler() *IMMessageHandler {
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
	log.Printf("[lansenger-mgr] local IMMessageHandler created")
	return h
}

func (m *lansengerGatewayManager) handleLocalMessage(msg lansenger.IncomingMessage) {
	if resp, handled := m.app.TryHandlePassthroughSlashCommandWithSource(msg.Text, "lansenger:"+msg.FromUserID); handled {
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
			_ = gw.SendText(context.Background(), lansenger.OutgoingText{
				ToUserID: msg.FromUserID,
				Text:     reply,
			})
		}
		return
	}
	if !m.app.isMaclawLLMConfigured() {
		m.mu.Lock()
		gw := m.gateway
		m.mu.Unlock()
		if gw != nil {
			_ = gw.SendText(context.Background(), lansenger.OutgoingText{
				ToUserID: msg.FromUserID,
				Text:     i18n.T(i18n.MsgLLMNotConfigured, "zh"),
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

	// Pass images/files as attachments, matching the pattern used by
	// WeChat, Telegram and QQ gateways.
	var attachments []MessageAttachment
	if msg.MediaType != "" && len(msg.MediaData) > 0 {
		if normalizeIMMediaKind(msg.MediaType).IsImage() {
			// Image → multimodal attachment for LLM vision.
			// If the LLM doesn't support vision, buildUserContent will
			// save it to a local file and tell the LLM accordingly.
			attachments = append(attachments, buildLocalImageAttachment(msg.MediaData, msg.MediaName, ""))
		} else {
			// Non-image media (file, voice, video) → save to local temp
			// and prepend the path to the text so the agent can read it.
			mediaPath, err := saveMediaToTempDir("lansenger", "ls_", msg.FromUserID, msg.MediaType, msg.MediaData, msg.MediaName)
			if err != nil {
				log.Printf("[lansenger-mgr] save media error: %v", err)
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
		if now.Sub(lastProgress) < 2*time.Second {
			return
		}
		stripped := textutil.StripMarkdown(progressText)
		if stripped == lastProgressText {
			return
		}
		lastProgress = now
		lastProgressText = stripped
		_ = gw.SendText(context.Background(), lansenger.OutgoingText{
			ToUserID: msg.FromUserID,
			Text:     i18n.T(i18n.MsgProgressPrefix, "zh") + stripped,
		})
	}

	resp := handler.HandleIMMessageWithProgress(IMUserMessage{
		UserID:      msg.FromUserID,
		Platform:    "lansenger_local",
		Text:        text,
		Lang:        "zh",
		Attachments: attachments,
	}, onProgress)

	if resp == nil || resp.Deferred {
		return
	}

	m.sendAgentResponse(gw, msg.FromUserID, resp)
}

func (m *lansengerGatewayManager) sendAgentResponse(gw *lansenger.Gateway, toUserID string, resp *IMAgentResponse) {
	ctx := context.Background()

	if resp.Text != "" {
		text := textutil.StripMarkdown(resp.Text)
		// Lanxin (蓝信) does not support interactive buttons/cards.
		// Degrade resp.Actions to numbered text options appended to the message.
		if len(resp.Actions) > 0 {
			// FormatAskUserForDisplay may append generic input hints.
			// Strip those hints and replace them with concrete numbered options.
			// Strip these generic hints and replace with the concrete numbered
			// options so the user sees one clear instruction.
			for _, hint := range []string{
				"请输入：确认 或 取消",
				"请输入：选项编号或内容",
				"请输入：确认 或 修改意见",
				"请直接输入您的回复",
				"请回复「确认」或「取消」", // legacy
			} {
				text = strings.TrimSuffix(strings.TrimSpace(text), hint)
			}
			text = strings.TrimSpace(text)
			text += "\n\n请回复对应选项："
			for i, action := range resp.Actions {
				text += fmt.Sprintf("\n%d. %s", i+1, action.Label)
			}
		}
		if err := gw.SendText(ctx, lansenger.OutgoingText{
			ToUserID: toUserID,
			Text:     text,
		}); err != nil {
			log.Printf("[lansenger-mgr] SendText error: %v", err)
		}
	} else if len(resp.Actions) > 0 {
		// Actions without text body — send as standalone options message.
		text := "请回复对应选项："
		for i, action := range resp.Actions {
			text += fmt.Sprintf("\n%d. %s", i+1, action.Label)
		}
		_ = gw.SendText(ctx, lansenger.OutgoingText{
			ToUserID: toUserID,
			Text:     text,
		})
	}

	if resp.Error != "" && resp.Text == "" && len(resp.Actions) == 0 {
		_ = gw.SendText(ctx, lansenger.OutgoingText{
			ToUserID: toUserID,
			Text:     "❌ " + textutil.StripMarkdown(resp.Error),
		})
	}

	if resp.ImageKey != "" {
		imgData, err := base64.StdEncoding.DecodeString(resp.ImageKey)
		if err == nil && len(imgData) > 0 {
			_ = gw.SendMedia(ctx, lansenger.OutgoingMedia{
				ToUserID:  toUserID,
				FileData:  imgData,
				MediaType: "image",
			})
		}
	}

	if resp.FileData != "" {
		fileBytes, err := base64.StdEncoding.DecodeString(resp.FileData)
		if err == nil && len(fileBytes) > 0 {
			_ = gw.SendMedia(ctx, lansenger.OutgoingMedia{
				ToUserID:  toUserID,
				FileData:  fileBytes,
				FileName:  resp.FileName,
				MediaType: "file",
			})
		}
	}

	// Send voice message as a file because Lansenger does not expose a native voice type.
	if resp.VoiceData != "" {
		voiceBytes, err := base64.StdEncoding.DecodeString(resp.VoiceData)
		if err != nil || len(voiceBytes) == 0 {
			log.Printf("[lansenger-mgr] decode voice data failed (to=%s): %v", toUserID, err)
		} else if err := gw.SendMedia(ctx, lansenger.OutgoingMedia{
			ToUserID:  toUserID,
			FileData:  voiceBytes,
			FileName:  resp.VoiceFileName,
			MediaType: "file",
		}); err != nil {
			log.Printf("[lansenger-mgr] SendMedia voice file failed (to=%s): %v", toUserID, err)
		}
	}

	m.sendLocalFiles(gw, toUserID, resp)
}

func (m *lansengerGatewayManager) sendLocalFiles(gw *lansenger.Gateway, toUserID string, resp *IMAgentResponse) {
	paths := resp.LocalFilePaths
	if resp.LocalFilePath != "" && !containsString(paths, resp.LocalFilePath) {
		paths = append([]string{resp.LocalFilePath}, paths...)
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			log.Printf("[lansenger-mgr] read local file %s error: %v", p, err)
			continue
		}
		name := filepath.Base(p)
		mediaType := mediaTypeFromFileName(name)
		mediaKind := normalizeIMMediaKind(mediaType)
		if mediaKind.IsVoice() || mediaKind.IsAudio() {
			mediaType = imMediaFile.String()
		}
		if err := gw.SendMedia(context.Background(), lansenger.OutgoingMedia{
			ToUserID:  toUserID,
			FileData:  data,
			FileName:  name,
			MediaType: mediaType,
		}); err != nil {
			log.Printf("[lansenger-mgr] SendMedia local file failed (to=%s file=%s): %v", toUserID, p, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Hub reply handling
// ---------------------------------------------------------------------------

// HandleGatewayReply dispatches a reply from Hub to the Lansenger API.
func (m *lansengerGatewayManager) HandleGatewayReply(reply GatewayReplyPayload) {
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		log.Printf("[lansenger-mgr] HandleGatewayReply: gateway is nil, dropping")
		return
	}

	ctx := context.Background()
	switch normalizeGatewayReplyTypeKind(reply.ReplyType) {
	case gatewayReplyTypeText:
		_ = gw.SendText(ctx, lansenger.OutgoingText{
			ToUserID: reply.PlatformUID,
			Text:     textutil.StripMarkdown(reply.Text),
		})
	case gatewayReplyTypeImage:
		data, err := base64.StdEncoding.DecodeString(reply.ImageData)
		if err != nil {
			return
		}
		_ = gw.SendMedia(ctx, lansenger.OutgoingMedia{
			ToUserID:  reply.PlatformUID,
			FileData:  data,
			MediaType: imMediaImage.String(),
		})
	case gatewayReplyTypeFile:
		data, err := base64.StdEncoding.DecodeString(reply.FileData)
		if err != nil {
			return
		}
		_ = gw.SendMedia(ctx, lansenger.OutgoingMedia{
			ToUserID:  reply.PlatformUID,
			FileData:  data,
			FileName:  reply.FileName,
			MediaType: imMediaFile.String(),
		})
	case gatewayReplyTypeVoice:
		data, err := base64.StdEncoding.DecodeString(reply.FileData)
		if err != nil {
			return
		}
		_ = gw.SendMedia(ctx, lansenger.OutgoingMedia{
			ToUserID:  reply.PlatformUID,
			FileData:  data,
			FileName:  reply.FileName,
			MediaType: imMediaFile.String(),
		})
	}
}

// ---------------------------------------------------------------------------
// App integration — Wails bindings
// ---------------------------------------------------------------------------

func (a *App) ensureLansengerGateway() {
	cfg, err := a.LoadConfig()
	if err != nil {
		return
	}
	if !cfg.LansengerEnabled || strings.TrimSpace(cfg.LansengerAppID) == "" || strings.TrimSpace(cfg.LansengerAppSecret) == "" {
		if a.lansengerGateway != nil {
			a.lansengerGateway.SyncFromConfig()
		}
		return
	}
	if a.lansengerGateway == nil {
		a.lansengerGateway = newLansengerGatewayManager(a)
	}
	a.lansengerGateway.SyncFromConfig()
}

func (a *App) GetLansengerStatus() string {
	if a.lansengerGateway == nil {
		return "disconnected"
	}
	return a.lansengerGateway.Status()
}

func (a *App) RestartLansenger() string {
	a.ensureLansengerGateway()
	if a.lansengerGateway == nil {
		return "disconnected"
	}
	a.lansengerGateway.Restart()
	return a.lansengerGateway.Status()
}

func (a *App) StopLansenger() {
	if a.lansengerGateway != nil {
		a.lansengerGateway.Stop()
	}
}

func (a *App) GetLansengerLocalMode() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return true
	}
	return cfg.IsLansengerLocalMode()
}

func (a *App) SetLansengerLocalMode(enabled bool) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	if !enabled && cfg.RemoteMachineID == "" {
		return fmt.Errorf("请先注册到 Hub（设置 Hub 地址并完成注册），再开启多机模式")
	}
	cfg.SetLansengerLocal(enabled)
	if err := a.SaveConfig(cfg); err != nil {
		return err
	}

	if a.lansengerGateway != nil {
		a.lansengerGateway.resetLocalHandler()
	}

	if !enabled {
		hubClient := a.hubClient()
		if hubClient != nil && hubClient.IsConnected() {
			hubClient.SendIMGatewayClaim(imGatewayPlatformLansenger)
		}
	}
	return nil
}
