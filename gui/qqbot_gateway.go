package main

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/qqbot"
)

// qqBotGatewayManager manages the client-side QQ Bot WebSocket gateway.
// It starts/stops the gateway based on AppConfig and forwards messages
// between QQ and the Hub via the existing machine WebSocket.
type qqBotGatewayManager struct {
	app        *App
	mu         sync.Mutex
	gateway    *qqbot.Gateway
	status     string // "disconnected", "connecting", "connected", "error", "reconnecting"
	lastAppID  string
	lastSecret string
}

func newQQBotGatewayManager(app *App) *qqBotGatewayManager {
	return &qqBotGatewayManager{
		app:    app,
		status: "disconnected",
	}
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
			m.status = "disconnected"
			m.mu.Unlock()
			_ = gw.Stop() // Stop outside lock to avoid deadlock with onStatusChange
			m.emitStatusEvent()
			return
		}
		m.mu.Unlock()
		return
	}

	// Should be running — check if config actually changed
	if m.gateway != nil && m.lastAppID == cfg.QQBotAppID && m.lastSecret == cfg.QQBotAppSecret {
		m.mu.Unlock()
		return // config unchanged, gateway already running
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
		m.status = "error"
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
	m.status = "disconnected"
	m.lastAppID = ""
	m.lastSecret = ""
	m.mu.Unlock()
	if gw != nil {
		_ = gw.Stop()
	}
}

// Status returns the current connection status.
func (m *qqBotGatewayManager) Status() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// onStatusChange is called by the gateway when connection status changes.
func (m *qqBotGatewayManager) onStatusChange(status string) {
	m.mu.Lock()
	m.status = status
	m.mu.Unlock()
	m.emitStatusEvent()

	// When QQ gateway connects, claim the gateway lock on Hub.
	if status == "connected" {
		hubClient := m.app.hubClient()
		if hubClient != nil && hubClient.IsConnected() {
			hubClient.SendIMGatewayClaim("qqbot_remote")
		}
	}
}

func (m *qqBotGatewayManager) emitStatusEvent() {
	m.app.emitEvent("qqbot-status-changed", m.Status())
}

// onIncomingMessage is called when a C2C message arrives from QQ.
// It forwards the message to Hub as im.gateway_message so Hub's IM Adapter
// can process it (identity binding, /call, @machine, /discuss, agent routing).
func (m *qqBotGatewayManager) onIncomingMessage(msg qqbot.IncomingMessage) {
	hubClient := m.app.hubClient()
	if hubClient == nil || !hubClient.IsConnected() {
		log.Printf("[qqbot-mgr] hub not connected, cannot forward QQ message from %s", msg.OpenID)
		m.mu.Lock()
		gw := m.gateway
		m.mu.Unlock()
		if gw != nil {
			_ = gw.SendText(context.Background(), qqbot.OutgoingText{
				OpenID: msg.OpenID,
				Text:   "⚠️ Hub 未连接，无法处理消息。请确认客户端已连接到 Hub。",
			})
		}
		return
	}

	// Forward to Hub via im.gateway_message — Hub routes through IM Adapter
	// pipeline and sends replies back as im.gateway_reply.
	hubClient.SendIMGatewayMessage("qqbot_remote", map[string]any{
		"platform_uid": msg.OpenID,
		"text":         msg.Text,
		"message_type": "text",
	})
}

// SendQQBotReply sends a text reply to a QQ user. Called when Hub sends
// im.qq_outgoing back to the client.
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
func (a *App) ensureQQBotGateway() {
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
	return a.qqBotGateway.Status()
}

// StopQQBot stops the QQ Bot gateway (Wails binding).
func (a *App) StopQQBot() {
	if a.qqBotGateway != nil {
		a.qqBotGateway.Stop()
	}
}
