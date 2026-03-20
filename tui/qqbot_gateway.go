// tui/qqbot_gateway.go — TUI-side QQ Bot gateway manager (Hub forwarding mode).
//
// QQ messages are forwarded to Hub via im.gateway_message. Hub processes them
// through the IM Adapter pipeline and sends replies back as im.gateway_reply.
package main

import (
	"context"
	"log"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/qqbot"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// tuiQQBotManager manages the QQ Bot gateway in TUI with Hub forwarding.
type tuiQQBotManager struct {
	mu         sync.Mutex
	gateway    *qqbot.Gateway
	status     string
	lastAppID  string
	lastSecret string
	logger     corelib.Logger
}

func newTUIQQBotManager(logger corelib.Logger) *tuiQQBotManager {
	return &tuiQQBotManager{
		status: "disconnected",
		logger: logger,
	}
}

// SyncFromConfig reads AppConfig and starts/stops the gateway accordingly.
func (m *tuiQQBotManager) SyncFromConfig() {
	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return
	}

	m.mu.Lock()
	if !cfg.QQBotEnabled || cfg.QQBotAppID == "" || cfg.QQBotAppSecret == "" {
		gw := m.gateway
		if gw != nil {
			m.gateway = nil
			m.status = "disconnected"
			m.mu.Unlock()
			_ = gw.Stop()
			return
		}
		m.mu.Unlock()
		return
	}

	if m.gateway != nil && m.lastAppID == cfg.QQBotAppID && m.lastSecret == cfg.QQBotAppSecret {
		m.mu.Unlock()
		return
	}

	oldGw := m.gateway
	m.gateway = nil
	m.mu.Unlock()

	if oldGw != nil {
		_ = oldGw.Stop()
	}

	gwCfg := qqbot.Config{
		AppID:     cfg.QQBotAppID,
		AppSecret: cfg.QQBotAppSecret,
	}
	gw := qqbot.NewGateway(gwCfg, m.onIncomingMessage)
	gw.SetStatusCallback(m.onStatusChange)

	m.mu.Lock()
	m.gateway = gw
	m.lastAppID = cfg.QQBotAppID
	m.lastSecret = cfg.QQBotAppSecret
	m.mu.Unlock()

	if err := gw.Start(context.Background()); err != nil {
		m.logger.Error("[qqbot-tui] gateway start failed: %v", err)
		m.mu.Lock()
		m.status = "error"
		m.mu.Unlock()
	}
}

// Stop shuts down the gateway.
func (m *tuiQQBotManager) Stop() {
	m.mu.Lock()
	gw := m.gateway
	m.gateway = nil
	m.status = "disconnected"
	m.lastAppID = ""
	m.lastSecret = ""
	m.mu.Unlock()
	if gw != nil {
		_ = gw.Stop()
	}
}

// Status returns the current connection status.
func (m *tuiQQBotManager) Status() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *tuiQQBotManager) onStatusChange(status string) {
	m.mu.Lock()
	m.status = status
	m.mu.Unlock()
	// Note: TUI doesn't have Hub client wired yet for gateway claim.
	// Gateway claim will be sent when Hub client integration is added.
}

// onIncomingMessage forwards QQ messages to Hub via im.gateway_message.
// In TUI mode, Hub client is not yet wired, so we log a warning.
// TODO: Wire TUI Hub client for im.gateway_message forwarding.
func (m *tuiQQBotManager) onIncomingMessage(msg qqbot.IncomingMessage) {
	log.Printf("[qqbot-tui] received message from %s (Hub forwarding not yet wired in TUI)", msg.OpenID)
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw != nil {
		_ = gw.SendText(context.Background(), qqbot.OutgoingText{
			OpenID: msg.OpenID,
			Text:   "⚠️ TUI 模式暂不支持 Hub 转发，请使用 GUI 客户端。",
		})
	}
}

// SendReply sends a text reply to a QQ user.
func (m *tuiQQBotManager) SendReply(openID, text string) error {
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		return nil
	}
	return gw.SendText(context.Background(), qqbot.OutgoingText{
		OpenID: openID,
		Text:   text,
	})
}

// SendMedia sends a media message to a QQ user.
func (m *tuiQQBotManager) SendMedia(msg qqbot.OutgoingMedia) error {
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		return nil
	}
	return gw.SendMedia(context.Background(), msg)
}
