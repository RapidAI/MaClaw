package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
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
	app       *App
	mu        sync.Mutex
	gateway   *lansenger.Gateway
	status    string
	lastToken string

	localHandler *IMMessageHandler
}

func newLansengerGatewayManager(app *App) *lansengerGatewayManager {
	return &lansengerGatewayManager{
		app:    app,
		status: "disconnected",
	}
}

// SyncFromConfig reads the current AppConfig and starts or stops the gateway.
func (m *lansengerGatewayManager) SyncFromConfig() {
	cfg, err := m.app.LoadConfig()
	if err != nil {
		return
	}

	// Three simple fields: AppID, AppSecret, Gateway URL (with default).
	appID := strings.TrimSpace(cfg.LansengerAppID)
	appSecret := strings.TrimSpace(cfg.LansengerAppSecret)
	gwURL := cfg.LansengerApiGatewayURL()

	m.mu.Lock()
	if !cfg.LansengerEnabled || appID == "" || appSecret == "" {
		gw := m.gateway
		if gw != nil {
			m.gateway = nil
			m.status = "disconnected"
			m.mu.Unlock()
			_ = gw.Stop()
		} else {
			m.mu.Unlock()
		}
		if hubClient := m.app.hubClient(); hubClient != nil && hubClient.IsConnected() {
			_ = hubClient.SendIMGatewayUnclaim("lansenger")
		}
		if gw != nil {
			m.emitStatusEvent()
		}
		return
	}

	// Compose a cache key from all credential fields so any change triggers reconnect.
	cacheKey := appID + "|" + appSecret + "|" + gwURL

	if m.gateway != nil && m.lastToken == cacheKey {
		m.mu.Unlock()
		return
	}

	oldGw := m.gateway
	m.mu.Unlock()

	if oldGw != nil {
		_ = oldGw.Stop()
	}

	gwCfg := lansenger.Config{
		AppID:         appID,
		AppSecret:     appSecret,
		ApiGatewayURL: gwURL,
	}

	gw := lansenger.NewGateway(gwCfg, m.onIncomingMessage)
	gw.SetStatusCallback(m.onStatusChange)

	m.mu.Lock()
	m.gateway = gw
	m.lastToken = cacheKey
	m.mu.Unlock()

	if err := gw.Start(context.Background()); err != nil {
		log.Printf("[lansenger-mgr] start failed: %v", err)
		m.mu.Lock()
		m.gateway = nil
		m.lastToken = ""
		m.status = "error"
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
	m.status = "disconnected"
	m.lastToken = ""
	lh := m.localHandler
	m.localHandler = nil
	m.mu.Unlock()
	if lh != nil {
		lh.memory.stop()
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
	return m.status
}

func (m *lansengerGatewayManager) onStatusChange(status string) {
	m.mu.Lock()
	m.status = status
	if status == "error" {
		// Clear gateway reference so SyncFromConfig can retry on next call.
		m.gateway = nil
		m.lastToken = ""
	}
	m.mu.Unlock()
	m.emitStatusEvent()

	if status == "connected" {
		if cfg, err := m.app.LoadConfig(); err == nil && cfg.IsLansengerLocalMode() {
			return
		}
		hubClient := m.app.hubClient()
		if hubClient != nil && hubClient.IsConnected() {
			hubClient.SendIMGatewayClaim("lansenger")
		}
	}
}

func (m *lansengerGatewayManager) emitStatusEvent() {
	m.app.emitEvent("lansenger-status-changed", m.Status())
}

func (m *lansengerGatewayManager) resetLocalHandler() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.localHandler != nil {
		m.localHandler.memory.stop()
		m.localHandler = nil
	}
}

// ---------------------------------------------------------------------------
// Message routing
// ---------------------------------------------------------------------------

func (m *lansengerGatewayManager) onIncomingMessage(msg lansenger.IncomingMessage) {
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

	if err := hubClient.SendIMGatewayMessage("lansenger", payload); err != nil {
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
		h.memory.archiver = a.conversationArchiver
	}

	m.localHandler = h
	log.Printf("[lansenger-mgr] local IMMessageHandler created")
	return h
}

func (m *lansengerGatewayManager) handleLocalMessage(msg lansenger.IncomingMessage) {
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
		if msg.MediaType == "image" {
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

	var lastProgress time.Time
	var lastProgressText string
	onProgress := func(progressText string) {
		if progressText == "" || progressText == imHeartbeatMsg {
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
			// FormatAskUserForDisplay appends input hints like "请输入：确认 或 取消"
			// or "请输入：选项编号或内容" or "请输入：确认 或 修改意见".
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
	switch reply.ReplyType {
	case "text":
		_ = gw.SendText(ctx, lansenger.OutgoingText{
			ToUserID: reply.PlatformUID,
			Text:     textutil.StripMarkdown(reply.Text),
		})
	case "image":
		data, err := base64.StdEncoding.DecodeString(reply.ImageData)
		if err != nil {
			return
		}
		_ = gw.SendMedia(ctx, lansenger.OutgoingMedia{
			ToUserID:  reply.PlatformUID,
			FileData:  data,
			MediaType: "image",
		})
	case "file":
		data, err := base64.StdEncoding.DecodeString(reply.FileData)
		if err != nil {
			return
		}
		_ = gw.SendMedia(ctx, lansenger.OutgoingMedia{
			ToUserID:  reply.PlatformUID,
			FileData:  data,
			FileName:  reply.FileName,
			MediaType: "file",
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
			hubClient.SendIMGatewayClaim("lansenger")
		}
	}
	return nil
}
