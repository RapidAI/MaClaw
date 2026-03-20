package main

import (
	"context"
	"log"
	"strconv"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/telegram"
)

// telegramGatewayManager manages the client-side Telegram Bot gateway.
// Same architecture as qqBotGatewayManager: forwards messages to Hub via
// im.gateway_message, receives replies via im.gateway_reply.
type telegramGatewayManager struct {
	app       *App
	mu        sync.Mutex
	gateway   *telegram.Gateway
	status    string
	lastToken string
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
	m.mu.Unlock()
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
		hubClient := m.app.hubClient()
		if hubClient != nil && hubClient.IsConnected() {
			hubClient.SendIMGatewayClaim("telegram")
		}
	}
}

func (m *telegramGatewayManager) emitStatusEvent() {
	m.app.emitEvent("telegram-status-changed", m.Status())
}

// onIncomingMessage forwards Telegram messages to Hub via im.gateway_message.
func (m *telegramGatewayManager) onIncomingMessage(msg telegram.IncomingMessage) {
	hubClient := m.app.hubClient()
	if hubClient == nil || !hubClient.IsConnected() {
		log.Printf("[telegram-mgr] hub not connected, cannot forward TG message from chat=%d", msg.ChatID)
		m.mu.Lock()
		gw := m.gateway
		m.mu.Unlock()
		if gw != nil {
			_ = gw.SendText(context.Background(), telegram.OutgoingText{
				ChatID: msg.ChatID,
				Text:   "⚠️ Hub 未连接，无法处理消息。",
			})
		}
		return
	}

	// Use chatID as platform_uid (string form)
	hubClient.SendIMGatewayMessage("telegram", map[string]any{
		"platform_uid": strconv.FormatInt(msg.ChatID, 10),
		"text":         msg.Text,
		"message_type": "text",
	})
}

// GatewayReplyPayload holds the fields of an im.gateway_reply from Hub.
type GatewayReplyPayload struct {
	ReplyType   string `json:"reply_type"`
	PlatformUID string `json:"platform_uid"`
	Text        string `json:"text"`
	ImageData   string `json:"image_data"`
	Caption     string `json:"caption"`
	FileData    string `json:"file_data"`
	FileName    string `json:"file_name"`
	MimeType    string `json:"mime_type"`
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
