package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/telegram"
	"github.com/RapidAI/CodeClaw/corelib/textutil"
)

// telegramGatewayManager manages the client-side Telegram Bot gateway.
// Supports two modes:
//   - Local / 单机 mode (default): routes messages directly to the
//     local MaClaw LLM agent loop, bypassing Hub entirely.
//   - Hub / 多机 mode (TelegramLocalMode=false): forwards messages to Hub
//     via im.gateway_message, receives replies via im.gateway_reply.
type telegramGatewayManager struct {
	app       *App
	mu        sync.Mutex
	gateway   *telegram.Gateway
	status    string
	lastToken string

	// localHandler is a fully-wired IMMessageHandler for local mode.
	localHandler *IMMessageHandler
}

func newTelegramGatewayManager(app *App) *telegramGatewayManager {
	return &telegramGatewayManager{
		app:    app,
		status: "disconnected",
	}
}

// SyncFromConfig reads the current AppConfig and starts or stops the gateway.
func (m *telegramGatewayManager) SyncFromConfig() {
	cfg, err := m.app.LoadConfig()
	if err != nil {
		return
	}

	m.mu.Lock()
	if !cfg.TelegramBotEnabled || cfg.TelegramBotToken == "" {
		gw := m.gateway
		if gw != nil {
			m.gateway = nil
			m.status = "disconnected"
			m.mu.Unlock()
			_ = gw.Stop()
			m.emitStatusEvent()
			return
		}
		m.mu.Unlock()
		return
	}

	if m.gateway != nil && m.lastToken == cfg.TelegramBotToken {
		m.mu.Unlock()
		return
	}

	oldGw := m.gateway
	m.gateway = nil
	m.mu.Unlock()

	if oldGw != nil {
		_ = oldGw.Stop()
	}

	gw := telegram.NewGateway(telegram.Config{BotToken: cfg.TelegramBotToken}, m.onIncomingMessage)
	gw.SetStatusCallback(m.onStatusChange)

	m.mu.Lock()
	m.gateway = gw
	m.lastToken = cfg.TelegramBotToken
	m.mu.Unlock()

	if err := gw.Start(context.Background()); err != nil {
		log.Printf("[telegram-mgr] start failed: %v", err)
		m.mu.Lock()
		m.status = "error"
		m.mu.Unlock()
		m.emitStatusEvent()
		return
	}
}

// Stop shuts down the gateway.
func (m *telegramGatewayManager) Stop() {
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
}

// Status returns the current connection status.
func (m *telegramGatewayManager) Status() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *telegramGatewayManager) onStatusChange(status string) {
	m.mu.Lock()
	m.status = status
	m.mu.Unlock()
	m.emitStatusEvent()

	if status == "connected" {
		// In local mode, skip Hub gateway claim.
		if cfg, err := m.app.LoadConfig(); err == nil && cfg.IsTelegramLocalMode() {
			return
		}
		hubClient := m.app.hubClient()
		if hubClient != nil && hubClient.IsConnected() {
			hubClient.SendIMGatewayClaim("telegram")
		}
	}
}

func (m *telegramGatewayManager) emitStatusEvent() {
	m.app.emitEvent("telegram-status-changed", m.Status())
}

// resetLocalHandler invalidates the cached local handler.
func (m *telegramGatewayManager) resetLocalHandler() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.localHandler != nil {
		m.localHandler.memory.stop()
		m.localHandler = nil
	}
}

// ensureLocalHandler lazily creates a fully-wired IMMessageHandler for local mode.
func (m *telegramGatewayManager) ensureLocalHandler() *IMMessageHandler {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.localHandler != nil {
		return m.localHandler
	}

	a := m.app
	a.ensureRemoteInfra()

	h := NewIMMessageHandler(a, a.remoteSessions)
	if a.capabilityGapDetector != nil {
		h.SetCapabilityGapDetector(a.capabilityGapDetector)
	}
	if a.toolDefGenerator != nil {
		h.SetToolDefGenerator(a.toolDefGenerator)
	}
	if a.toolRouter != nil {
		h.SetToolRouter(a.toolRouter)
	}
	if a.memoryStore != nil {
		h.SetMemoryStore(a.memoryStore)
	}
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
	if a.startupFeedback != nil {
		h.SetStartupFeedback(a.startupFeedback)
	}
	if a.securityFirewall != nil {
		h.SetSecurityFirewall(a.securityFirewall)
	}
	if a.conversationArchiver != nil {
		h.memory.archiver = a.conversationArchiver
	}

	m.localHandler = h
	log.Printf("[telegram-mgr] local IMMessageHandler created")
	return h
}

// onIncomingMessage routes Telegram messages to local handler or Hub.
func (m *telegramGatewayManager) onIncomingMessage(msg telegram.IncomingMessage) {
	cfg, err := m.app.LoadConfig()
	if err != nil {
		log.Printf("[telegram-mgr] LoadConfig error: %v", err)
		return
	}

	isLocal := cfg.IsTelegramLocalMode()
	hubClient := m.app.hubClient()
	hubConnected := hubClient != nil && hubClient.IsConnected()
	log.Printf("[telegram-mgr] onIncomingMessage: chat=%d local_mode=%v hub_connected=%v", msg.ChatID, isLocal, hubConnected)

	if isLocal {
		m.handleLocalMessage(msg)
		return
	}

	// Hub mode — try forwarding; fall back to local if Hub unavailable.
	if !hubConnected {
		log.Printf("[telegram-mgr] Hub mode but Hub unavailable, falling back to local: chat=%d", msg.ChatID)
		m.notifyHubUnavailable(msg)
		m.handleLocalMessage(msg)
		return
	}

	m.forwardToHub(msg)
}

func (m *telegramGatewayManager) notifyHubUnavailable(msg telegram.IncomingMessage) {
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		return
	}
	_ = gw.SendText(context.Background(), telegram.OutgoingText{
		ChatID: msg.ChatID,
		Text:   "⚠️ 当前为多机模式，但 Hub 未连接。消息已回退到本地处理。\n请检查 Hub 连接状态，或切换回单机模式。",
	})
}

func (m *telegramGatewayManager) forwardToHub(msg telegram.IncomingMessage) {
	hubClient := m.app.hubClient()
	if hubClient == nil || !hubClient.IsConnected() {
		log.Printf("[telegram-mgr] forwardToHub FAILED: hub unavailable chat=%d", msg.ChatID)
		m.notifyHubUnavailable(msg)
		m.handleLocalMessage(msg)
		return
	}

	if err := hubClient.SendIMGatewayMessage("telegram", map[string]any{
		"platform_uid": strconv.FormatInt(msg.ChatID, 10),
		"text":         msg.Text,
		"message_type": "text",
	}); err != nil {
		log.Printf("[telegram-mgr] forwardToHub SendIMGatewayMessage error: %v", err)
	}
}

func (m *telegramGatewayManager) handleLocalMessage(msg telegram.IncomingMessage) {
	if !m.app.isMaclawLLMConfigured() {
		m.mu.Lock()
		gw := m.gateway
		m.mu.Unlock()
		if gw != nil {
			_ = gw.SendText(context.Background(), telegram.OutgoingText{
				ChatID: msg.ChatID,
				Text:   "⚠️ 本地 LLM 未配置，请先在设置中配置 MaClaw LLM。",
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
	if text == "" {
		return
	}

	var lastProgress time.Time
	onProgress := func(progressText string) {
		if progressText == "" {
			return
		}
		now := time.Now()
		if now.Sub(lastProgress) < 5*time.Second {
			return
		}
		lastProgress = now
		_ = gw.SendText(context.Background(), telegram.OutgoingText{
			ChatID: msg.ChatID,
			Text:   "⏳ " + textutil.StripMarkdown(progressText),
		})
	}

	resp := handler.HandleIMMessageWithProgress(IMUserMessage{
		UserID:   strconv.FormatInt(msg.ChatID, 10),
		Platform: "telegram_local",
		Text:     text,
	}, onProgress)

	if resp == nil {
		return
	}

	m.sendAgentResponse(gw, msg.ChatID, resp)
}

func (m *telegramGatewayManager) sendAgentResponse(gw *telegram.Gateway, chatID int64, resp *IMAgentResponse) {
	ctx := context.Background()

	if resp.Text != "" {
		text := textutil.StripMarkdown(resp.Text)
		if err := gw.SendText(ctx, telegram.OutgoingText{
			ChatID: chatID,
			Text:   text,
		}); err != nil {
			log.Printf("[telegram-mgr] local SendText error (to=%d): %v", chatID, err)
		}
	}

	if resp.Error != "" && resp.Text == "" {
		_ = gw.SendText(ctx, telegram.OutgoingText{
			ChatID: chatID,
			Text:   "❌ " + textutil.StripMarkdown(resp.Error),
		})
	}

	if resp.ImageKey != "" {
		_ = gw.SendMedia(ctx, telegram.OutgoingMedia{
			ChatID:   chatID,
			FileType: "photo",
			FileData: resp.ImageKey,
		})
	}

	if resp.FileData != "" {
		mimeType := resp.FileMimeType
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		_ = gw.SendMedia(ctx, telegram.OutgoingMedia{
			ChatID:   chatID,
			FileType: "document",
			FileData: resp.FileData,
			FileName: resp.FileName,
			MimeType: mimeType,
		})
	}
}

// GatewayReplyPayload holds the fields of an im.gateway_reply from Hub.
type GatewayReplyPayload struct {
	ReplyType    string         `json:"reply_type"`
	PlatformUID  string         `json:"platform_uid"`
	Text         string         `json:"text"`
	ImageData    string         `json:"image_data"`
	Caption      string         `json:"caption"`
	FileData     string         `json:"file_data"`
	FileName     string         `json:"file_name"`
	MimeType     string         `json:"mime_type"`
	ContextToken string         `json:"context_token,omitempty"`
	Extra        map[string]any `json:"extra,omitempty"`
}

// HandleGatewayReply dispatches a reply from Hub to the Telegram API.
func (m *telegramGatewayManager) HandleGatewayReply(reply GatewayReplyPayload) {
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		return
	}

	chatID, err := strconv.ParseInt(reply.PlatformUID, 10, 64)
	if err != nil {
		log.Printf("[telegram-mgr] invalid chatID %q: %v", reply.PlatformUID, err)
		return
	}

	switch reply.ReplyType {
	case "text":
		_ = gw.SendText(context.Background(), telegram.OutgoingText{
			ChatID: chatID,
			Text:   reply.Text,
		})
	case "image":
		_ = gw.SendMedia(context.Background(), telegram.OutgoingMedia{
			ChatID:   chatID,
			FileType: "photo",
			FileData: reply.ImageData,
			Caption:  reply.Caption,
		})
	case "file":
		_ = gw.SendMedia(context.Background(), telegram.OutgoingMedia{
			ChatID:   chatID,
			FileType: "document",
			FileData: reply.FileData,
			FileName: reply.FileName,
			MimeType: reply.MimeType,
		})
	}
}

// ---------------------------------------------------------------------------
// App integration — Wails bindings and lifecycle
// ---------------------------------------------------------------------------

func (a *App) ensureTelegramGateway() {
	if a.telegramGateway == nil {
		a.telegramGateway = newTelegramGatewayManager(a)
	}
	a.telegramGateway.SyncFromConfig()
}

func (a *App) GetTelegramStatus() string {
	if a.telegramGateway == nil {
		return "disconnected"
	}
	return a.telegramGateway.Status()
}

func (a *App) RestartTelegram() string {
	a.ensureTelegramGateway()
	return a.telegramGateway.Status()
}

func (a *App) StopTelegram() {
	if a.telegramGateway != nil {
		a.telegramGateway.Stop()
	}
}

// GetTelegramLocalMode returns whether Telegram local mode is enabled.
func (a *App) GetTelegramLocalMode() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return true
	}
	return cfg.IsTelegramLocalMode()
}

// SetTelegramLocalMode enables or disables Telegram local mode.
func (a *App) SetTelegramLocalMode(enabled bool) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	// Switching to hub mode requires prior Hub registration.
	if !enabled && cfg.RemoteMachineID == "" {
		return fmt.Errorf("请先注册到 Hub（设置 Hub 地址并完成注册），再开启多机模式")
	}
	cfg.SetTelegramLocal(enabled)
	if err := a.SaveConfig(cfg); err != nil {
		return err
	}
	log.Printf("[telegram-mgr] SetTelegramLocalMode: enabled=%v", enabled)

	if a.telegramGateway != nil {
		a.telegramGateway.resetLocalHandler()
	}

	if !enabled {
		hubClient := a.hubClient()
		if hubClient != nil && hubClient.IsConnected() {
			hubClient.SendIMGatewayClaim("telegram")
			log.Printf("[telegram-mgr] sent gateway claim after switching to hub mode")
		}
	}
	return nil
}
