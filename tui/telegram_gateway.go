// tui/telegram_gateway.go — TUI-side Telegram Bot gateway manager.
//
// Same architecture as QQ Bot: forwards messages to Hub via im.gateway_message.
package main

import (
	"context"
	"log"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/telegram"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// tuiTelegramManager manages the Telegram Bot gateway in TUI.
type tuiTelegramManager struct {
	mu        sync.Mutex
	gateway   *telegram.Gateway
	status    string
	lastToken string
	logger    corelib.Logger
}

func newTUITelegramManager(logger corelib.Logger) *tuiTelegramManager {
	return &tuiTelegramManager{
		status: "disconnected",
		logger: logger,
	}
}

// SyncFromConfig reads AppConfig and starts/stops the gateway accordingly.
func (m *tuiTelegramManager) SyncFromConfig() {
	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
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
		m.logger.Error("[telegram-tui] gateway start failed: %v", err)
		m.mu.Lock()
		m.status = "error"
		m.mu.Unlock()
	}
}

// Stop shuts down the gateway.
func (m *tuiTelegramManager) Stop() {
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
func (m *tuiTelegramManager) Status() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *tuiTelegramManager) onStatusChange(status string) {
	m.mu.Lock()
	m.status = status
	m.mu.Unlock()
}

// onIncomingMessage forwards Telegram messages to Hub.
// TODO: Wire TUI Hub client for im.gateway_message forwarding.
func (m *tuiTelegramManager) onIncomingMessage(msg telegram.IncomingMessage) {
	log.Printf("[telegram-tui] received message from chat=%d (Hub forwarding not yet wired in TUI)", msg.ChatID)
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw != nil {
		_ = gw.SendText(context.Background(), telegram.OutgoingText{
			ChatID: msg.ChatID,
			Text:   "⚠️ TUI 模式暂不支持 Hub 转发，请使用 GUI 客户端。",
		})
	}
}
